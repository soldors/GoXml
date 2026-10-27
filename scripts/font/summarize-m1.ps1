#Requires -Version 5.1
<#
    summarize-m1.ps1 - R2-WB-SAMPLER-HARDEN / R3-WB-SAMPLER-CLOSE
    Summarises samples produced by collect-m1.ps1 into JSON / CSV / Markdown.

    Statistics contract (quote together with every number):
      * median  = middle value; for even n the arithmetic mean of the two middles.
      * p95     = nearest-rank without interpolation: rank = ceil(0.95 * n),
                  index = rank - 1 (1-based), clamped to n. For n < 20 this is
                  very close to max and must not be read as a stable tail.
      * Data from different baseSHA / load / exeSha256 / sourceSha256 is NEVER
        merged into one median. The default is to reject the input set; with
        -AllowMixed the output carries one section per consistent group.
      * A record whose valid=true is not backed by usable numeric fields is
        treated as FORGED and is rejected, not silently trusted.
      * failed / missing / zero / timeout / terminated samples are counted
        separately and are NEVER dropped. Zero readings are reported as
        "below clock granularity / not observed", never as "cost 0".
      * if valid samples < -MinSamplesForThreshold the output states that no
        threshold is frozen. This script never sets a threshold.

    R3 fix (false green, reproduced by the coordinator):
      A record claiming valid=true is now re-checked against the COMPLETE
      validity condition instead of being trusted:
        exitCodeKnown really true; exitCode == 0; verdict == OK;
        verificationRun == false; terminatedByCollector / timedOut /
        probeTimeout / stdoutEmpty all false; framesObserved >= requestedFrames;
        observed.m1ElapsedNs present, FINITE and >= 0 (a legal zero is kept);
        observed.m1Measured and observed.layoutFirstAtMeasured really true;
        observed.layoutFirstEntryNs present, finite and > 0.
      A record failing any of these is REJECTED with every reason recorded and
      never enters the statistics. A genuine valid=false record is kept and
      counted as invalid.

      Two PowerShell semantics this depends on, both measured on this host
      (r3 diag-ps-semantics.ps1):
        [bool]'false' is True   -> booleans must be normalised, never cast blind.
        [double]::TryParse('NaN') is True -> a numeric gate must also require
                                             IsNaN/IsInfinity to be false.
      R2 defect fixed here as well: `continue` inside the provenance foreach
      advanced to the next FIELD, not the next RECORD, so a record with missing
      provenance still reached the statistics.

    Exit codes:
      0 = summary written
      2 = usage error, no input, output-overwrite refusal,
          or mixed provenance without -AllowMixed
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$InputDir,

    [string]$OutDir,

    [int]$MinSamplesForThreshold = 30,

    [switch]$AllowMixed,

    [switch]$Overwrite
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

. (Join-Path $PSScriptRoot 'font-comparison.ps1')

if (-not (Test-Path -LiteralPath $InputDir)) { Write-Host "REJECT InputDir not found: $InputDir"; exit 2 }
if (-not $OutDir -or $OutDir -eq '') { $OutDir = $InputDir }

$rejections = New-Object System.Collections.Generic.List[object]
function Add-Rejection {
    param([string]$Source, [string]$Reason)
    $rejections.Add([ordered]@{ source = $Source; reason = $Reason })
}

$jsonl = Join-Path $InputDir 'samples.jsonl'
$raw = New-Object System.Collections.Generic.List[object]
if (Test-Path -LiteralPath $jsonl) {
    # -Encoding UTF8 is mandatory: GBK decoding of Chinese probe output swallows
    # line breaks and corrupts records.
    $lines = Get-Content -LiteralPath $jsonl -Encoding UTF8 | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    foreach ($l in $lines) { $raw.Add(($l | ConvertFrom-Json)) }
} else {
    $sampleDir = Join-Path $InputDir 'samples'
    if (-not (Test-Path -LiteralPath $sampleDir)) { Write-Host 'REJECT no samples.jsonl and no samples dir'; exit 2 }
    foreach ($f in (Get-ChildItem -LiteralPath $sampleDir -Filter 'sample-*.json' -File)) {
        $raw.Add((Get-Content -LiteralPath $f.FullName -Raw -Encoding UTF8 | ConvertFrom-Json))
    }
}
if ($raw.Count -eq 0) { Write-Host 'REJECT no sample records parsed'; exit 2 }

# Output uniqueness is checked BEFORE the first write.
$jsonPath = Join-Path $OutDir 'summary.json'
$csvPath = Join-Path $OutDir 'summary.csv'
$mdPath = Join-Path $OutDir 'summary.md'
$rejPath = Join-Path $OutDir 'rejections.json'
if (-not $Overwrite) {
    foreach ($p in @($jsonPath, $csvPath, $mdPath, $rejPath)) {
        if (Test-Path -LiteralPath $p) { Write-Host ("REJECT output overwrite refused, path exists: " + $p); exit 2 }
    }
}
# Created only after the refusal checks, so a rejection never leaves a directory behind.
if (-not (Test-Path -LiteralPath $OutDir)) { New-Item -ItemType Directory -Path $OutDir -Force | Out-Null }

# ---------------------------------------------------------------- validation
# A field counts as numeric only when it parses AND is finite.
# Measured on this host (r3 diag-ps-semantics.ps1 Q3):
#   [double]::TryParse('NaN') = True, IsNaN = True
# so a plain "parses" test would let NaN through the gate.
function Test-NumericProp {
    param([object]$Obj, [string]$Name)
    if ($null -eq $Obj) { return $false }
    $prop = $Obj.PSObject.Properties[$Name]
    if ($null -eq $prop -or $null -eq $prop.Value) { return $false }
    $d = 0.0
    if (-not [double]::TryParse([string]$prop.Value, [ref]$d)) { return $false }
    return -not ([double]::IsNaN($d) -or [double]::IsInfinity($d))
}

function Get-NumericProp {
    param([object]$Obj, [string]$Name)
    $d = 0.0
    [void][double]::TryParse([string]$Obj.PSObject.Properties[$Name].Value, [ref]$d)
    return $d
}

# Why a numeric gate failed. Distinguishing "absent", "unparseable" and
# "parses but is NaN/Infinity" matters: the third one is only reachable because
# [double]::TryParse('NaN') returns True (measured r3 Q3), so a plain
# "is it numeric" test would have let it through.
function Get-NumericFailReason {
    param([object]$Obj, [string]$Name)
    if ($null -eq $Obj) { return ('observed.' + $Name + ' absent (no observed block)') }
    $prop = $Obj.PSObject.Properties[$Name]
    if ($null -eq $prop -or $null -eq $prop.Value) { return ('observed.' + $Name + ' missing') }
    $raw = [string]$prop.Value
    $d = 0.0
    if (-not [double]::TryParse($raw, [ref]$d)) { return ('observed.' + $Name + ' non-numeric:' + $raw) }
    if ([double]::IsNaN($d)) { return ('observed.' + $Name + ' is NaN') }
    if ([double]::IsInfinity($d)) { return ('observed.' + $Name + ' is Infinity') }
    return ('observed.' + $Name + ' unusable')
}

# Safe property read: under Set-StrictMode -Version Latest, dereferencing
# .Value on a missing property throws PropertyNotFoundStrict, which would abort
# the whole summary instead of rejecting one record.
function Get-PropValue {
    param([object]$Obj, [string]$Name)
    if ($null -eq $Obj) { return $null }
    $p = $Obj.PSObject.Properties[$Name]
    if ($null -eq $p) { return $null }
    return $p.Value
}

# PowerShell truthiness trap, measured on this host (r3 diag-ps-semantics.ps1 Q2):
#   [bool]'false' = True, [bool]'0' = True, only '' is False.
# A record carrying the STRING 'false' would therefore be counted as valid.
# Real booleans pass through; strings are compared literally; null is False.
function ConvertTo-RealBool {
    param([object]$Value)
    if ($null -eq $Value) { return $false }
    if ($Value -is [bool]) { return $Value }
    $s = [string]$Value
    if ($s -eq '1' -or $s -eq 'true' -or $s -eq 'True' -or $s -eq 'TRUE') { return $true }
    return $false
}

$accepted = New-Object System.Collections.Generic.List[object]
$seenIds = @{}
$index = 0
foreach ($r in $raw) {
    $index++
    $src = 'record#' + $index
    $id = $null
    if ($null -ne $r.PSObject.Properties['id'] -and $null -ne $r.id) { $id = [string]$r.id }
    if ([string]::IsNullOrWhiteSpace($id)) {
        Add-Rejection -Source $src -Reason 'missing-id'
        continue
    }
    $src = $id
    if ($seenIds.ContainsKey($id)) {
        Add-Rejection -Source $src -Reason 'duplicate-id'
        continue
    }
    $seenIds[$id] = $true

    # Provenance is collected FIRST and the record is skipped afterwards.
    # R2 defect: `continue` inside this loop only advanced to the NEXT FIELD,
    # so a record missing baseSHA was still accepted and still reached the
    # statistics. The whole record must be dropped.
    $missingProv = New-Object System.Collections.Generic.List[string]
    foreach ($prov in @('baseSHA', 'load', 'exeSha256', 'sourceSha256')) {
        $p = $r.PSObject.Properties[$prov]
        if ($null -eq $p -or $null -eq $p.Value -or [string]::IsNullOrWhiteSpace([string]$p.Value)) {
            $missingProv.Add($prov)
        }
    }
    if ($missingProv.Count -gt 0) {
        Add-Rejection -Source $src -Reason ('missing-provenance:' + ($missingProv -join ','))
        continue
    }

    if ($null -eq $r.PSObject.Properties['valid'] -or $null -eq $r.PSObject.Properties['valid'].Value) {
        Add-Rejection -Source $src -Reason 'missing-valid'
        continue
    }
    $isValid = ConvertTo-RealBool -Value $r.valid
    $measurementContract = [string](Get-PropValue -Obj $r -Name 'measurementContract')
    if ($measurementContract -eq '') { $measurementContract = 'app-m1-v1' }
    if ($measurementContract -notin @('app-m1-v1', 'font-init-v1')) {
        Add-Rejection -Source $src -Reason 'unsupported-measurement-contract'
        continue
    }
    if (($r.valid -isnot [bool]) -and -not ($r.valid -is [string])) {
        Add-Rejection -Source $src -Reason ('non-boolean-valid:' + [string]$r.valid)
        continue
    }

    if ($isValid) {
        # R3: a record that CLAIMS valid=true is re-checked against the complete
        # validity condition. R2 trusted the flag and only looked at two numeric
        # fields, so exitCode=9, verificationRun=true and framesObserved=1<3 all
        # sailed through as valid=3 / rejected=0 (coordinator fixture
        # integration-v2/sampler-review-fixture-1).
        $forged = New-Object System.Collections.Generic.List[string]

        if ($null -ne $r.PSObject.Properties['runtimeProcsPolicy'] -or $null -ne $r.PSObject.Properties['runtimeProcsObserved']) {
            $runtimeValue = 0
            $observedProcs = Get-PropValue -Obj $r -Name 'runtimeProcsObserved'
            if (-not [int]::TryParse([string]$observedProcs,[ref]$runtimeValue) -or $runtimeValue -lt 1) {
                $forged.Add('runtime-procs-not-observed')
            }
            $policy = [string](Get-PropValue -Obj $r -Name 'runtimeProcsPolicy')
            $requested = 0
            if ($policy -ne 'inherit' -and $policy -ne 'auto') {
                if (-not [int]::TryParse($policy,[ref]$requested) -or $requested -lt 1 -or $requested -ne $runtimeValue) {
                    $forged.Add('runtime-procs-policy-mismatch')
                }
            }
        }

        if (-not (ConvertTo-RealBool -Value $r.exitCodeKnown)) { $forged.Add('exitCodeKnown-not-true') }

        $ecProp = $r.PSObject.Properties['exitCode']
        if ($null -eq $ecProp -or $null -eq $ecProp.Value) { $forged.Add('exitCode-missing') }
        else {
            $ec = 0.0
            if ([double]::TryParse([string]$ecProp.Value, [ref]$ec)) {
                if ($ec -ne 0) { $forged.Add('exitCode=' + [string]$ecProp.Value) }
            } else { $forged.Add('exitCode-non-numeric:' + [string]$ecProp.Value) }
        }

        $vProp = $r.PSObject.Properties['verdict']
        $verdictText = $(if ($null -eq $vProp -or $null -eq $vProp.Value) { '' } else { [string]$vProp.Value })
        if ($verdictText -ne 'OK') { $forged.Add('verdict=' + $(if ($verdictText -eq '') { '<missing>' } else { $verdictText })) }

        # A verification run is driven by a helper process (exit5/ok0/hang) to
        # prove the kill path. It is NEVER a probe measurement.
        if (ConvertTo-RealBool -Value $r.verificationRun) { $forged.Add('verificationRun=true') }

        foreach ($f in @('terminatedByCollector', 'timedOut', 'probeTimeout', 'stdoutEmpty')) {
            if (ConvertTo-RealBool -Value (Get-PropValue -Obj $r -Name $f)) { $forged.Add($f + '=true') }
        }

        # Frames: the record must carry what was asked for and must have seen at
        # least that many. Missing requiredFrames cannot be compared, so it is a
        # rejection, not a silent pass.
        $reqProp = $r.PSObject.Properties['requestedFrames']
        $reqFrames = -1
        if ($null -ne $reqProp -and $null -ne $reqProp.Value) {
            $rf = 0
            if ([int]::TryParse([string]$reqProp.Value, [ref]$rf)) { $reqFrames = $rf }
        }
        if ($reqFrames -lt 1) {
            $forged.Add('requestedFrames-missing-or-nonpositive')
        } elseif (-not (Test-NumericProp -Obj $r.observed -Name 'framesObserved')) {
            $forged.Add((Get-NumericFailReason -Obj $r.observed -Name 'framesObserved'))
        } else {
            $frVal = Get-NumericProp -Obj $r.observed -Name 'framesObserved'
            if ($frVal -lt $reqFrames) { $forged.Add('framesObserved=' + $frVal + '<required=' + $reqFrames) }
        }

        # M1: present, finite, and >= 0. A legal zero is KEPT (below clock
        # granularity), a negative value is impossible and is rejected.
        if (-not (Test-NumericProp -Obj $r.observed -Name 'm1ElapsedNs')) {
            $forged.Add((Get-NumericFailReason -Obj $r.observed -Name 'm1ElapsedNs'))
        } else {
            $m1v = Get-NumericProp -Obj $r.observed -Name 'm1ElapsedNs'
            if ($m1v -lt 0) { $forged.Add('m1ElapsedNs-negative:' + $m1v) }
        }
        if (-not (ConvertTo-RealBool -Value $r.observed.m1Measured)) { $forged.Add('observed.m1Measured is false') }

        if (-not (ConvertTo-RealBool -Value $r.observed.layoutFirstAtMeasured)) { $forged.Add('observed.layoutFirstAtMeasured is false') }
        $layoutOk = $false
        if ((Test-NumericProp -Obj $r.observed -Name 'layoutFirstEntryNs')) {
            if ((Get-NumericProp -Obj $r.observed -Name 'layoutFirstEntryNs') -gt 0) { $layoutOk = $true }
        }
        if (-not $layoutOk) {
            if (Test-NumericProp -Obj $r.observed -Name 'layoutFirstEntryNs') {
                $forged.Add('observed.layoutFirstEntryNs non-positive:' + (Get-NumericProp -Obj $r.observed -Name 'layoutFirstEntryNs'))
            } else {
                $forged.Add((Get-NumericFailReason -Obj $r.observed -Name 'layoutFirstEntryNs'))
            }
        }

        if ($measurementContract -eq 'font-init-v1') {
            foreach ($reason in @(Get-FontComparisonErrors -Observed $r.observed -LoadMode $r.load)) {
                $forged.Add($reason)
            }
        } elseif ($null -ne (Get-PropValue -Obj $r.observed -Name 'fontComparisonContract')) {
            $forged.Add('comparison-observations-without-contract')
        }

        if ($forged.Count -gt 0) {
            foreach ($f in $forged) { Add-Rejection -Source $src -Reason ('forged-valid:' + $f) }
            continue
        }
    }
    $accepted.Add($r)
}

$validRecs = @($accepted | Where-Object { ConvertTo-RealBool -Value $_.valid })
$invalidRecs = @($accepted | Where-Object { -not (ConvertTo-RealBool -Value $_.valid) })

# ---------------------------------------------------------------- grouping
function Get-GroupKey {
    param([object]$R)
    # Built from separate locals: an inline @([string]$x, ...) form trips the
    # PowerShell 5.1 parser here ("unexpected token ]").
    $gk = [string]$R.baseSHA
    $gl = [string]$R.load
    $ge = [string]$R.exeSha256
    $gs = [string]$R.sourceSha256
    $gc = [string](Get-PropValue -Obj $R -Name 'measurementContract')
    if ($gc -eq '') { $gc = 'app-m1-v1' }
    $gp = [string](Get-PropValue -Obj $R -Name 'runtimeProcsObserved')
    $gpolicy = [string](Get-PropValue -Obj $R -Name 'runtimeProcsPolicy')
    if ($gp -eq '') { $gp = 'legacy-unobserved' }
    if ($gpolicy -eq '') { $gpolicy = 'legacy' }
    return ($gk + '|' + $gl + '|' + $ge + '|' + $gs + '|' + $gc + '|procs=' + $gp + '|policy=' + $gpolicy)
}

$groups = @{}
foreach ($r in $validRecs) {
    $k = Get-GroupKey -R $r
    if (-not $groups.ContainsKey($k)) { $groups[$k] = New-Object System.Collections.Generic.List[object] }
    $groups[$k].Add($r)
}

if ($groups.Count -gt 1 -and -not $AllowMixed) {
    foreach ($k in $groups.Keys) {
        Add-Rejection -Source ('group:' + $k) -Reason 'mixed-provenance (baseSHA/load/exeSha256/sourceSha256); use -AllowMixed to emit per-group sections'
    }
    ($rejections | ConvertTo-Json -Depth 6) | Set-Content -LiteralPath $rejPath -Encoding UTF8
    Write-Host ("REJECT mixed provenance: " + $groups.Count + " distinct groups; see " + $rejPath)
    Write-Host ("GROUPS " + (($groups.Keys | Sort-Object) -join ' ;; '))
    exit 2
}

# ---------------------------------------------------------------- statistics
function Get-Values {
    param([object[]]$Records, [string]$Prop)
    $vals = New-Object System.Collections.Generic.List[double]
    foreach ($r in $Records) {
        if (Test-NumericProp -Obj $r.observed -Name $Prop) { $vals.Add((Get-NumericProp -Obj $r.observed -Name $Prop)) }
    }
    return , $vals
}

function Get-Median {
    param([double[]]$Values)
    if ($null -eq $Values) { return $null }
    $s = @($Values | Sort-Object)
    if ($s.Count -eq 0) { return $null }
    $n = $s.Count
    if ($n % 2 -eq 1) { return [double]$s[($n - 1) / 2] }
    return ([double]$s[($n / 2) - 1] + [double]$s[$n / 2]) / 2.0
}

function Get-P95NearestRank {
    param([double[]]$Values)
    if ($null -eq $Values) { return $null }
    $s = @($Values | Sort-Object)
    if ($s.Count -eq 0) { return $null }
    $n = $s.Count
    $rank = [int][math]::Ceiling(0.95 * $n)
    if ($rank -lt 1) { $rank = 1 }
    if ($rank -gt $n) { $rank = $n }
    return [double]$s[$rank - 1]
}

function New-Stat {
    param([string]$Name, [double[]]$Values, [int]$ZeroCount)
    if ($null -eq $Values) { $Values = @() }
    $min = $null; $max = $null
    if ($Values.Count -gt 0) { $min = ($Values | Measure-Object -Minimum).Minimum; $max = ($Values | Measure-Object -Maximum).Maximum }
    return [ordered]@{
        metric         = $Name
        n              = $Values.Count
        min            = $min
        median         = (Get-Median -Values $Values)
        p95NearestRank = (Get-P95NearestRank -Values $Values)
        max            = $max
        zeroOrBelow    = $ZeroCount
    }
}

function New-GroupStats {
    param([string]$Key, [object[]]$Records, [int]$MinSamples)
    $m1 = Get-Values -Records $Records -Prop 'm1ElapsedNs'
    $m1Zero = @($m1 | Where-Object { $_ -le 0 })
    $m1Pos = @($m1 | Where-Object { $_ -gt 0 })
    $fr = Get-Values -Records $Records -Prop 'framesObserved'
    $win = Get-Values -Records $Records -Prop 'frameWindowNs'
    # A List, not @() += : '+=' with an IDictionary on the right is ambiguous in
    # PowerShell 5.1 and raised "argument type mismatch" here.
    $stats = New-Object System.Collections.Generic.List[object]
    $stats.Add((New-Stat -Name 'm1ElapsedNs' -Values $m1 -ZeroCount $m1Zero.Count))
    $stats.Add((New-Stat -Name 'm1ElapsedNs(positive only)' -Values $m1Pos -ZeroCount 0))
    $stats.Add((New-Stat -Name 'framesObserved' -Values $fr -ZeroCount 0))
    $stats.Add((New-Stat -Name 'frameWindowNs' -Values $win -ZeroCount 0))
    if ((Get-PropValue -Obj $Records[0] -Name 'measurementContract') -eq 'font-init-v1') {
        foreach ($metric in @('fontReadyElapsedNs', 'fontToFirstFrameElapsedNs')) {
            $values = Get-Values -Records $Records -Prop $metric
            $zeros = @($values | Where-Object { $_ -eq 0 })
            $stats.Add((New-Stat -Name $metric -Values $values -ZeroCount $zeros.Count))
        }
    }
    $note = 'insufficient samples; no threshold frozen'
    if ($Records.Count -ge $MinSamples) { $note = ("valid samples >= " + $MinSamples + "; threshold still requires owner decision") }
    return [ordered]@{
        groupKey = $Key
        valid    = $Records.Count
        stats    = $stats
        threshold = [ordered]@{ minSamples = $MinSamples; note = $note }
    }
}

$groupSections = New-Object System.Collections.Generic.List[object]
foreach ($k in ($groups.Keys | Sort-Object)) {
    # The List is passed directly, NOT wrapped in @(...) : in PowerShell 5.1
    # "@($genericList)" fails parameter binding with "argument type mismatch"
    # while the bare List (and .ToArray()) bind correctly. Verified case by case.
    $groupSections.Add((New-GroupStats -Key $k -Records $groups[$k] -MinSamples $MinSamplesForThreshold))
}

$summary = [ordered]@{
    task        = 'R2-WB-SAMPLER-HARDEN'
    script      = 'scripts/font/summarize-m1.ps1'
    generatedAt = (Get-Date).ToString('o')
    inputDir    = $InputDir
    totalRecords = $raw.Count
    accepted    = $accepted.Count
    rejected    = $rejections.Count
    valid       = $validRecs.Count
    invalid     = $invalidRecs.Count
    timedOut    = @($accepted | Where-Object { ConvertTo-RealBool -Value (Get-PropValue -Obj $_ -Name 'timedOut') }).Count
    terminatedByCollector = @($accepted | Where-Object { ConvertTo-RealBool -Value (Get-PropValue -Obj $_ -Name 'terminatedByCollector') }).Count
    probeTimeout = @($accepted | Where-Object { ConvertTo-RealBool -Value (Get-PropValue -Obj $_ -Name 'probeTimeout') }).Count
    emptyStdout = @($accepted | Where-Object { ConvertTo-RealBool -Value (Get-PropValue -Obj $_ -Name 'stdoutEmpty') }).Count
    exitCodeUnknown = @($accepted | Where-Object { -not (ConvertTo-RealBool -Value (Get-PropValue -Obj $_ -Name 'exitCodeKnown')) }).Count
    verificationRuns = @($accepted | Where-Object { ConvertTo-RealBool -Value (Get-PropValue -Obj $_ -Name 'verificationRun') }).Count
    belowGranularity = @($validRecs | Where-Object { ConvertTo-RealBool -Value (Get-PropValue -Obj $_ -Name 'belowGranularity') }).Count
    groupCount  = $groups.Count
    groups      = $groupSections
    p95Method   = 'nearest-rank ceil(0.95*n), no interpolation'
    medianMethod = 'even n = mean of two middles'
    rejections  = $rejections.ToArray()
    contract    = @(
        'M1 = App construction start -> first frame processing start, not process startup',
        'zero elapsed means not observed / below clock granularity, never cost 0',
        'different baseSHA/load/exeSha256/sourceSha256 are never merged into one median',
        'font-init-v1 comparisons are separate from constructor-only app-m1-v1; none is not synchronous font loading',
        'failed, timed-out, terminated and zero samples are preserved and counted separately',
        'no threshold is set by this script'
    )
}

($summary | ConvertTo-Json -Depth 10) | Set-Content -LiteralPath $jsonPath -Encoding UTF8

$csvLines = New-Object System.Collections.Generic.List[string]
$csvLines.Add('groupKey,metric,n,min,median,p95NearestRank,max,zeroOrBelow')
foreach ($g in $groupSections) {
    foreach ($s in $g.stats) {
        $csvLines.Add(("{0},{1},{2},{3},{4},{5},{6},{7}" -f $g.groupKey, $s.metric, $s.n,
            $(if ($null -eq $s.min) { '' } else { $s.min }),
            $(if ($null -eq $s.median) { '' } else { $s.median }),
            $(if ($null -eq $s.p95NearestRank) { '' } else { $s.p95NearestRank }),
            $(if ($null -eq $s.max) { '' } else { $s.max }), $s.zeroOrBelow))
    }
}
Set-Content -LiteralPath $csvPath -Value $csvLines -Encoding UTF8

$md = New-Object System.Collections.Generic.List[string]
$md.Add('# M1 window sampling summary')
$md.Add('')
$md.Add(('generatedAt: ' + $summary.generatedAt))
$md.Add('')
$md.Add(('records={0} accepted={1} rejected={2} valid={3} invalid={4}' -f $raw.Count, $accepted.Count, $rejections.Count, $validRecs.Count, $invalidRecs.Count))
$md.Add(('timedOut={0} terminatedByCollector={1} probeTimeout={2} emptyStdout={3} exitCodeUnknown={4} belowGranularity={5} verificationRuns={6}' -f `
    $summary.timedOut, $summary.terminatedByCollector, $summary.probeTimeout, $summary.emptyStdout, $summary.exitCodeUnknown, $summary.belowGranularity, $summary.verificationRuns))
$md.Add('')
foreach ($g in $groupSections) {
    $md.Add(('## group: ' + $g.groupKey))
    $md.Add('')
    $md.Add('| metric | n | min | median | p95(nearest-rank) | max | zero/below |')
    $md.Add('|---|---|---|---|---|---|---|')
    foreach ($s in $g.stats) {
        $md.Add(('| {0} | {1} | {2} | {3} | {4} | {5} | {6} |' -f $s.metric, $s.n,
            $(if ($null -eq $s.min) { 'n/a' } else { $s.min }),
            $(if ($null -eq $s.median) { 'n/a' } else { $s.median }),
            $(if ($null -eq $s.p95NearestRank) { 'n/a' } else { $s.p95NearestRank }),
            $(if ($null -eq $s.max) { 'n/a' } else { $s.max }), $s.zeroOrBelow))
    }
    $md.Add('')
    $md.Add(('threshold: ' + $g.threshold.note))
    $md.Add('')
}
$md.Add('units: all timing metrics are nanoseconds (1 ms = 1,000,000 ns).')
$md.Add('')
if ($rejections.Count -gt 0) {
    $md.Add('## rejections')
    $md.Add('')
    foreach ($rj in $rejections) { $md.Add(('- ' + $rj.source + ' : ' + $rj.reason)) }
    $md.Add('')
}
Set-Content -LiteralPath $mdPath -Value $md -Encoding UTF8

if ($rejections.Count -gt 0) {
    ($rejections | ConvertTo-Json -Depth 6) | Set-Content -LiteralPath $rejPath -Encoding UTF8
}

Write-Host ("SUMMARY_OK records=" + $raw.Count + " accepted=" + $accepted.Count + " rejected=" + $rejections.Count +
    " valid=" + $validRecs.Count + " invalid=" + $invalidRecs.Count + " groups=" + $groups.Count)
Write-Host ("WROTE " + $jsonPath)
Write-Host ("WROTE " + $csvPath)
Write-Host ("WROTE " + $mdPath)
if ($rejections.Count -gt 0) { Write-Host ("WROTE " + $rejPath) }
exit 0
