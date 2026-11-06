#Requires -Version 5.1
<#
    run-font-acceptance.ps1 - R24-A FONT-ACCEPTANCE-RUNNER

    Ship the reviewed alternating-sample / derived-summary template as a repo entry.
    Real sampling needs separate authorization; this script only ORCHESTRATES and
    reuses the existing collect-m1.ps1 / summarize-m1.ps1 (no second collector).

    Behavior (matches the R23-R1 reviewed template; see docs plan):
      * Pre-registered alternating order via -Order or -OrderFile; frozen to order.json
        before running. One independent single-run (-Runs 1) sample per entry; do not
        run two whole batches and call it alternating.
      * All samples share ONE public lock -LockFile (default public heavy.lock); a single
        lock layer is held by the collector.
      * Originals untouched: each sample has its own output dir; derived samples.jsonl
        carries aggregationSource{path,sha256,originalId} and a namespaced derived id
        (original id unchanged).
      * Derived summary uses explicit -AllowMixed grouped output; rejection => fail.
      * fail-closed: existing OutDir rejected; per-sample samples.jsonl must be exactly
        1 line; missing original id rejected; collector rc not in {0,5} stops scheduling
        immediately (keeps recorded evidence); rc=5 = invalid sample, keep and continue.
      * Real run needs -Execute. Without -Execute it only validates/prepares (writes
        order.json, dry-run argv) and exits 2 (prepare state is NOT a pass); it does not
        call the collector and produces no samples.
* Prepared vs execute validation scope (F-A3): prepare validates argv/order/ExpectedHead
        format/dir uniqueness only and does NOT claim provenance or summary validity; execute
        additionally binds each record's load/baseSHA to the preregistered mode and ExpectedHead,
        then enforces summary.json conservation. Any provenance failure is rejected before the
        derived summary is written and stops further scheduling; originals are preserved.
      * No thresholds, no sampling substitutes, no fabricated samples.

    Read-only: existing collector/summarizer are read-only by default; theme/app
    product source untouched.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$RepoDir,
    [Parameter(Mandatory = $true)][string]$ExpectedHead,
    [Parameter(Mandatory = $true)][string]$OutDir,
    [string]$Order = 'sync,lazy,sync,lazy,sync,lazy',
    [string]$OrderFile,
    [string]$LockFile = 'D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock',
    [switch]$Execute,
    [string]$CollectorScript,
    [string]$SummarizerScript
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not $CollectorScript)  { $CollectorScript  = Join-Path $PSScriptRoot 'collect-m1.ps1' }
if (-not $SummarizerScript) { $SummarizerScript = Join-Path $PSScriptRoot 'summarize-m1.ps1' }
foreach ($s in @($CollectorScript, $SummarizerScript)) {
    if (-not (Test-Path -LiteralPath $s)) { throw "missing script: $s" }
}
if ($ExpectedHead -notmatch '^[0-9a-f]{40}$') {
    throw "ExpectedHead must be a full 40-hex SHA, got '$ExpectedHead'"
}
if (-not (Test-Path -LiteralPath $RepoDir)) { throw "RepoDir does not exist: $RepoDir" }

$orderSeq = New-Object 'System.Collections.Generic.List[string]'
if ($OrderFile) {
    if (-not (Test-Path -LiteralPath $OrderFile)) { throw "OrderFile does not exist: $OrderFile" }
    foreach ($ln in (Get-Content -LiteralPath $OrderFile -Encoding UTF8)) {
        $v = $ln.Trim(); if ($v) { $orderSeq.Add($v) }
    }
} else {
    foreach ($seg in $Order.Split(',')) {
        $v = $seg.Trim(); if ($v) { $orderSeq.Add($v) }
    }
}
if ($orderSeq.Count -eq 0) { throw 'empty pre-registered order' }
foreach ($m in $orderSeq) {
    if ($m -notin @('sync', 'lazy')) { throw "invalid mode '$m' (only sync/lazy; none is not a comparison sample)" }
}
# F-A2: strictly alternating (adjacent differ) + balanced (equal sync/lazy), even length.
# Odd / unbalanced batches are rejected as NOT a balanced comparison (do not silently treat
# a lone trailing mode as a balanced sample). To run an odd design, preregister a balanced set.
if ($orderSeq.Count -lt 2) { throw "order needs at least 2 samples, got $($orderSeq.Count)" }
if ($orderSeq.Count % 2 -ne 0) { throw "order length $($orderSeq.Count) is odd; a balanced alternating batch must be even" }
$nSync = @($orderSeq | Where-Object { $_ -eq 'sync' }).Count
$nLazy = @($orderSeq | Where-Object { $_ -eq 'lazy' }).Count
if ($nSync -ne $nLazy) { throw "order not balanced: sync=$nSync lazy=$nLazy (must be equal)" }
for ($k = 1; $k -lt $orderSeq.Count; $k++) {
    if ($orderSeq[$k] -eq $orderSeq[$k - 1]) {
        throw "order not alternating at index ${k}: '$($orderSeq[$k - 1])' repeats '$($orderSeq[$k])'"
    }
}

if (Test-Path -LiteralPath $OutDir) { throw "evidence dir already exists, refusing to overwrite: $OutDir" }

function Plan-Argv([int]$i, [string]$mode) {
    $caseName = 'sample-{0:D2}-{1}' -f $i, $mode
    $out = Join-Path $OutDir $caseName
    @{
        caseName = $caseName
        outDir   = $out
        argv     = @(
            '-RepoDir', $RepoDir, '-ExpectedHead', $ExpectedHead, '-OutDir', $out,
            '-Load', $mode, '-CompareFonts', '-Runs', '1', '-Strict', '-LockFile', $LockFile
        )
    }
}

if (-not $Execute) {
    $null = New-Item -ItemType Directory -Path $OutDir
    $orderSeq | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $OutDir 'order.json') -Encoding UTF8
    $plan = New-Object 'System.Collections.Generic.List[object]'
    for ($i = 0; $i -lt $orderSeq.Count; $i++) {
        $p = Plan-Argv $i $orderSeq[$i]
        $plan.Add([ordered]@{ index = $i; mode = $orderSeq[$i]; caseName = $p.caseName; argv = $p.argv })
        Write-Host ("[prepare] {0} {1} {2}" -f $i, $orderSeq[$i], ($p.argv -join ' '))
    }
    $plan | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $OutDir 'prepare-plan.json') -Encoding UTF8
    Write-Host 'prepare state done: collector not called, no samples; real sampling needs -Execute and authorization.'
    exit 2
}

$null = New-Item -ItemType Directory -Path $OutDir
$orderSeq | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $OutDir 'order.json') -Encoding UTF8
$psExe = Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe'
$derivedLines = New-Object 'System.Collections.Generic.List[string]'
$manifest = New-Object 'System.Collections.Generic.List[object]'
$expectedAccepted = 0
$expectedInvalid = 0

for ($i = 0; $i -lt $orderSeq.Count; $i++) {
    $mode = $orderSeq[$i]
    $p = Plan-Argv $i $mode
    $caseName = $p.caseName
    $out = $p.outDir
    & $psExe -NoProfile -NonInteractive -File $CollectorScript @($p.argv) *> (Join-Path $OutDir ($caseName + '.log'))
    $rc = $LASTEXITCODE
    [ordered]@{ index = $i; mode = $mode; caseName = $caseName; argv = $p.argv; rc = $rc } |
        ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $OutDir ($caseName + '-exit.json')) -Encoding UTF8

    if ($rc -notin @(0, 5)) {
        throw "collector fault rc=$rc ($caseName); stop scheduling, keep all recorded evidence"
    }

    $source = Join-Path $out 'samples.jsonl'
    if (-not (Test-Path -LiteralPath $source)) { throw "$caseName missing samples.jsonl (rc=$rc)" }
    $rows = @(Get-Content -LiteralPath $source -Encoding UTF8 | Where-Object { $_.Trim() })
    if ($rows.Count -ne 1) {
        throw "$caseName samples.jsonl must be exactly 1 line (Runs=1), got $($rows.Count); refuse missing/extra rows"
    }
    $record = $rows[0] | ConvertFrom-Json
    $originalId = [string]$record.id
    if ([string]::IsNullOrWhiteSpace($originalId)) { throw "$caseName original record missing id" }

    # F-A3: bind real provenance from the record itself, not the directory name.
    # Expected load is the preregistered mode; baseSHA must equal the audited full SHA.
    $recLoad = [string]$record.load
    if ($recLoad -ne $mode) {
        throw "$caseName record load='$recLoad' != preregistered mode '$mode' (source/provenance mismatch)"
    }
    $recBase = [string]$record.baseSHA
    if ($recBase -ne $ExpectedHead) {
        throw "$caseName record baseSHA='$recBase' != ExpectedHead '$ExpectedHead' (source/provenance mismatch)"
    }
    if ($rc -eq 0) { $expectedAccepted++ } else { $expectedInvalid++ }

    $sourceHash = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant()
    $derivedId = $caseName + '/' + $originalId
    $record | Add-Member -NotePropertyName aggregationSource -NotePropertyValue ([ordered]@{
        path = $source; sha256 = $sourceHash; originalId = $originalId
    }) -Force
    $record.id = $derivedId
    $derivedLines.Add(($record | ConvertTo-Json -Depth 20 -Compress))
    $manifest.Add([ordered]@{ derivedId = $derivedId; path = $source; sha256 = $sourceHash; originalId = $originalId })
}

$derived = Join-Path $OutDir 'derived'
$null = New-Item -ItemType Directory -Path $derived
[System.IO.File]::WriteAllLines((Join-Path $derived 'samples.jsonl'), $derivedLines.ToArray(),
    (New-Object System.Text.UTF8Encoding($false)))
$manifest | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $derived 'manifest.json') -Encoding UTF8

$summaryDir = Join-Path $OutDir 'summary'
& $psExe -NoProfile -NonInteractive -File $SummarizerScript -InputDir $derived -OutDir $summaryDir -AllowMixed *> (Join-Path $OutDir 'summary.log')
if ($LASTEXITCODE -ne 0) { throw 'derived summary rejected (-AllowMixed); do not record as sampling pass' }

# F-A1: summarizer exits 0 even when it rejects records; rc is NOT the business gate.
# Read the real summary.json and enforce conservation: no rejections, accepted==valid==
# expected-accepted (rc==0 samples), total==derived count, invalid==expected-invalid (rc==5
# legit-invalid kept, not a failure). Preserve originals; do not cast invalid as forged.
$summaryJsonPath = Join-Path $summaryDir 'summary.json'
if (-not (Test-Path -LiteralPath $summaryJsonPath)) { throw 'summary.json not produced; cannot verify summary, do not record as pass' }
$sj = Get-Content -LiteralPath $summaryJsonPath -Raw | ConvertFrom-Json
$totalRecords = [int]$sj.totalRecords
$accepted     = [int]$sj.accepted
$rejected     = [int]$sj.rejected
$validN       = [int]$sj.valid
$invalidN     = [int]$sj.invalid
$nDerived     = $derivedLines.Count
if ($rejected -ne 0) {
    throw "summary rejected $rejected record(s) (must be 0); see summary.json rejections; do not record as pass"
}
if ($totalRecords -ne $nDerived) {
    throw "summary totalRecords=$totalRecords != derived=$nDerived (record conservation broken)"
}
# summarize-m1.ps1 semantics: accepted = valid + invalid (invalid is a flagged subset of
# accepted, NOT rejected). So accepted must equal all derived records; valid excludes the
# legal-invalid; invalid equals the rc=5 samples. rejected must be 0 (forged/duration-mismatch).
if ($accepted -ne $nDerived) {
    throw "summary accepted=$accepted != derived=$nDerived (all recorded samples must be accepted, none rejected)"
}
if ($invalidN -ne $expectedInvalid) {
    throw "summary invalid=$invalidN != expected-invalid(rc=5)=$expectedInvalid (legal invalid must be preserved, not dropped)"
}
if ($validN -ne ($nDerived - $expectedInvalid)) {
    throw "summary valid=$validN != derived-invalid=$($nDerived - $expectedInvalid)"
}

Write-Host ("done: {0} samples ({1} accepted, {2} legal-invalid), rejected={3}, groups={4}, summary at {5}" -f `
    $nDerived, $accepted, $invalidN, $rejected, [int]$sj.groupCount, $summaryDir)
exit 0
