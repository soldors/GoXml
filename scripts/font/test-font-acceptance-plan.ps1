#Requires -Version 5.1
<#
    test-font-acceptance-plan.ps1 - R24-A repair-r1 synthetic regression (real summarizer)

    Uses ONLY a temporary mock COLLECTOR that emits real-shaped records (with the observed.*
    fields summarize-m1.ps1 actually consumes, including a legal zero and a legal invalid),
    plus the REAL scripts/font/summarize-m1.ps1 (not a mock). No Go build, no window, no
    sampling, no real font. Mock lives only in this test's temp dir; not a product flow.

    Covers F-A1..F-A4:
      - healthy alternating batch: entry rc0, real summary accepted==2 rejected==0 groups==2
      - legal invalid (collector rc=5) preserved: summary invalid counted, NOT rejected, entry rc0
      - forged record (valid=true but duration mismatch): REAL summarizer rejects -> entry fails
      - non-alternating (sync,sync) order rejected
      - wrong-mode (record.load != preregistered) rejected before summary
      - wrong/unknown baseSHA rejected
      - missing id / rows!=1 / existing dir / non-full ExpectedHead rejected
      - prepare (no -Execute) rc=2, no samples
      - two behavior mutations (continue-after-fatal, no-namespace duplicate id) run the SAME
        health guard to real FAIL (bounded, script executable), then restore PASS.
#>
param([string]$OutDir = (Join-Path $env:TEMP ('font-accept-repair-' + [guid]::NewGuid().ToString('N'))))
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (Test-Path -LiteralPath $OutDir) { throw 'test output exists; refusing to overwrite' }
$null = New-Item -ItemType Directory -Path $OutDir
$psExe = Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe'
$runnerDir = $PSScriptRoot
$runner = Join-Path $runnerDir 'run-font-acceptance.ps1'
$realSummary = Join-Path $runnerDir 'summarize-m1.ps1'
$check = 0
function Assert-Check([bool]$c, [string]$m) { if (-not $c) { throw $m }; $script:check++ }

# ---- real-shaped record builder (fields summarize-m1.ps1 consumes) ----
function New-Record([string]$id, [string]$load, [string]$head, [bool]$valid, [bool]$zero, [string]$verdict='OK') {
    $m1 = if ($zero) { 0 } else { 20 }
    $fr = if ($zero) { 0 } else { 100 }
    $first = if ($zero) { 100 } else { 150 }
    [ordered]@{
        id=$id; measurementContract='font-init-v1'; runtimeProcsPolicy='inherit'; runtimeProcsObserved=2;
        agent='MOCK'; baseSHA=$head; load=$load; requestedFrames=3; args='MOCK';
        exeSha256='fixture'; sourceSha256='fixture'; pid=0; handleCached=$false; killConfirmName='';
        start='2026-11-06T01:00:00.000+08:00'; end='2026-11-06T01:00:00.004+08:00'; durationMs=4;
        exitCode=0; exitCodeKnown=$true; terminatedByCollector=$false; probeTimeout=$false; timedOut=$false;
        verdict=$verdict; stdoutEmpty=$false; stdoutPath=''; stderrPath='';
        stdoutTail=@('RESULT verdict='+$verdict); stderrTail=@();
        observed=[ordered]@{
            fontComparisonContract='font-init-v1'; fontComparisonMeasured='true'; fontComparisonColdProvider='true';
            fontInitAtNs=100; fontReadyAtNs=200; fontReadyElapsedNs=$fr; fontToFirstFrameElapsedNs=50;
            m1ElapsedNs=$m1; m1ElapsedNsPresent=$true; m1Measured=$true;
            layoutFirstEntryNs=151; layoutFirstEntryNsPresent=$true; layoutFirstAtMeasured=$true;
            framesObserved=3; frameWindowNs=$null; procStartNs=$null; ctorEntryNs=$null; ctorDoneNs=$null;
            createdAtNs=$null; firstFrameAtNs=$first; lazyState=$null; lazyTempElapsedNs=$null;
            lazyProbeElapsedNs=$null; lazyLoadElapsedNs=$null; lazyProbeRan=$null
        };
        valid=$valid; belowGranularity=$false; invalidReasons=@(); verificationRun=$false
    }
}

# ---- mock collector: emits exactly one real-shaped record; behavior via env switches ----
$mockDir = Join-Path $OutDir 'mock'
$null = New-Item -ItemType Directory -Path $mockDir
$mockCollector = Join-Path $mockDir 'mock-collect.ps1'
$mockBody = @'
param([string]$RepoDir,[string]$ExpectedHead,[string]$OutDir,[string]$Load,[switch]$CompareFonts,[int]$Runs,[switch]$Strict,[string]$LockFile)
$ErrorActionPreference='Stop'
$null = New-Item -ItemType Directory -Path $OutDir -Force
function New-Record([string]$id,[string]$load,[string]$head,[bool]$valid,[bool]$zero) {
  $m1 = if ($zero) { 0 } else { 20 }; $fr = if ($zero) { 0 } else { 100 }; $first = if ($zero) { 100 } else { 150 }
  [ordered]@{ id=$id; measurementContract='font-init-v1'; runtimeProcsPolicy='inherit'; runtimeProcsObserved=2; agent='MOCK'; baseSHA=$head; load=$load; requestedFrames=3; args='MOCK'; exeSha256='fixture'; sourceSha256='fixture'; pid=0; handleCached=$false; killConfirmName=''; start='2026-11-06T01:00:00+08:00'; end='2026-11-06T01:00:00+08:00'; durationMs=4; exitCode=0; exitCodeKnown=$true; terminatedByCollector=$false; probeTimeout=$false; timedOut=$false; verdict='OK'; stdoutEmpty=$false; stdoutPath=''; stderrPath=''; stdoutTail=@('RESULT verdict=OK'); stderrTail=@(); observed=[ordered]@{ fontComparisonContract='font-init-v1'; fontComparisonMeasured='true'; fontComparisonColdProvider='true'; fontInitAtNs=100; fontReadyAtNs=200; fontReadyElapsedNs=$fr; fontToFirstFrameElapsedNs=50; m1ElapsedNs=$m1; m1ElapsedNsPresent=$true; m1Measured=$true; layoutFirstEntryNs=151; layoutFirstEntryNsPresent=$true; layoutFirstAtMeasured=$true; framesObserved=3; frameWindowNs=$null; procStartNs=$null; ctorEntryNs=$null; ctorDoneNs=$null; createdAtNs=$null; firstFrameAtNs=$first; lazyState=$null; lazyTempElapsedNs=$null; lazyProbeElapsedNs=$null; lazyLoadElapsedNs=$null; lazyProbeRan=$null }; valid=$valid; belowGranularity=$false; invalidReasons=@(); verificationRun=$false }
}
if ($env:MOCK_FATAL_ON -eq $Load -and -not $env:MOCK_FATAL_DONE) { $env:MOCK_FATAL_DONE='1'; exit 7 }
if ($env:MOCK_MISSING -eq $Load) { exit 0 }   # no samples.jsonl
if ($env:MOCK_ROWS2 -eq $Load) { $l = @((New-Record '0001' $Load $ExpectedHead $true $false | ConvertTo-Json -Compress),(New-Record '0002' $Load $ExpectedHead $true $false | ConvertTo-Json -Compress)); [System.IO.File]::WriteAllLines((Join-Path $OutDir 'samples.jsonl'), $l, (New-Object System.Text.UTF8Encoding($false))); exit 0 }
$id = if ($env:MOCK_NOID -eq $Load) { '' } else { '0001' }
$valid = ($env:MOCK_INVALID -ne $Load)
$zero = ($env:MOCK_ZERO -eq $Load)
$r = New-Record $id $Load $ExpectedHead $valid $zero
if ($env:MOCK_WRONGMODE -eq $Load) { $r.load='sync' }
if ($env:MOCK_BADBASE -eq $Load) { $r.baseSHA='deadbeefdeadbeefdeadbeefdeadbeefdeadbeef' }
$r | ConvertTo-Json -Depth 20 -Compress | Set-Content -LiteralPath (Join-Path $OutDir 'samples.jsonl') -Encoding UTF8
if ($valid) { exit 0 } else { exit 5 }
'@
Set-Content -LiteralPath $mockCollector -Value $mockBody -Encoding UTF8

function Invoke-Runner([string]$id, [string[]]$Argv, [int]$ExpectedRc) {
    $saveFat = $env:MOCK_FATAL_DONE; $env:MOCK_FATAL_DONE = $null
    & $psExe -NoProfile -NonInteractive -File $runner @Argv *> (Join-Path $OutDir ($id + '.log'))
    $rc = $LASTEXITCODE
    [ordered]@{id=$id;argv=$Argv;rc=$rc;expected=$ExpectedRc} | ConvertTo-Json -Depth 6 -Compress |
        Add-Content -LiteralPath (Join-Path $OutDir 'results.jsonl') -Encoding UTF8
    $env:MOCK_FATAL_DONE = $saveFat
    return $rc
}
function Base-Args([string]$ev, [string]$order) {
    @('-RepoDir', $runnerDir, '-ExpectedHead', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      '-OutDir', $ev, '-Order', $order, '-Execute',
      '-CollectorScript', $mockCollector, '-SummarizerScript', $realSummary)
}
function Clear-Switches() { $env:MOCK_FATAL_ON=$null; $env:MOCK_INVALID=$null; $env:MOCK_MISSING=$null; $env:MOCK_ROWS2=$null; $env:MOCK_NOID=$null; $env:MOCK_ZERO=$null; $env:MOCK_WRONGMODE=$null; $env:MOCK_BADBASE=$null }

# 1) healthy alternating 2 (sync,lazy), real summary accepted=2 rejected=0 groups=2, entry rc0
Clear-Switches
$ev = Join-Path $OutDir 'healthy'
$rc = Invoke-Runner 'healthy' (Base-Args $ev 'sync,lazy') 0
Assert-Check ($rc -eq 0) "healthy rc=$rc expected 0 (see healthy.log)"
$sj = Get-Content -LiteralPath (Join-Path $ev 'summary\summary.json') -Raw | ConvertFrom-Json
Assert-Check ([int]$sj.accepted -eq 2) "healthy accepted=$($sj.accepted) expected 2"
Assert-Check ([int]$sj.rejected -eq 0) "healthy rejected=$($sj.rejected) expected 0"
Assert-Check ([int]$sj.groupCount -eq 2) "healthy groupCount=$($sj.groupCount) expected 2"

# 2) legal-invalid (collector rc=5 on lazy): preserved, summary invalid counted, NOT rejected, entry rc0
Clear-Switches; $env:MOCK_INVALID='lazy'
$ev = Join-Path $OutDir 'legal-invalid'
$rc = Invoke-Runner 'legal-invalid' (Base-Args $ev 'sync,lazy') 0
$sj = Get-Content -LiteralPath (Join-Path $ev 'summary\summary.json') -Raw | ConvertFrom-Json
Clear-Switches
Assert-Check ($rc -eq 0) "legal-invalid rc=$rc expected 0 (invalid kept, not fatal)"
Assert-Check ([int]$sj.rejected -eq 0) "legal-invalid rejected=$($sj.rejected) expected 0 (invalid != forged)"
Assert-Check ([int]$sj.invalid -eq 1) "legal-invalid invalid=$($sj.invalid) expected 1 (preserved)"

# 3) F-A1 forged (valid=true but duration mismatch) -> real summarizer rejects -> entry fails
$forgedCollector = Join-Path $mockDir 'mock-forged.ps1'
$fb = Get-Content -LiteralPath $mockCollector -Raw
$fb = $fb -replace "if \(\`$env:MOCK_BADBASE -eq \`$Load\) \{ \`$r.baseSHA='deadbeefdeadbeefdeadbeefdeadbeefdeadbeef' \}",
                   "if (`$env:MOCK_BADBASE -eq `$Load) { `$r.baseSHA='deadbeefdeadbeefdeadbeefdeadbeefdeadbeef' }`nif (`$env:MOCK_FORGE -eq `$Load) { `$r.observed.fontReadyElapsedNs = 999 }"
Set-Content -LiteralPath $forgedCollector -Value $fb -Encoding UTF8
Clear-Switches; $env:MOCK_FORGE='lazy'
$ev = Join-Path $OutDir 'forged'
$rc = Invoke-Runner 'forged' (@('-RepoDir',$runnerDir,'-ExpectedHead','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','-OutDir',$ev,'-Order','sync,lazy','-Execute','-CollectorScript',$forgedCollector,'-SummarizerScript',$realSummary)) 1
Clear-Switches
Assert-Check ($rc -ne 0) 'forged record must be rejected by real summarizer -> entry fails (F-A1)'

# 4) non-alternating (sync,sync) rejected before any scheduling
Clear-Switches
$ev = Join-Path $OutDir 'nonalternating'
$rc = Invoke-Runner 'nonalternating' (Base-Args $ev 'sync,sync') 1
Assert-Check ($rc -ne 0) 'non-alternating order must be rejected (F-A2)'

# 5) wrong-mode record (load=sync while preregistered lazy) rejected
Clear-Switches; $env:MOCK_WRONGMODE='lazy'
$ev = Join-Path $OutDir 'wrongmode'
$rc = Invoke-Runner 'wrongmode' (Base-Args $ev 'sync,lazy') 1
Clear-Switches
Assert-Check ($rc -ne 0) 'record load != preregistered mode must be rejected (F-A3)'

# 6) bad baseSHA rejected
Clear-Switches; $env:MOCK_BADBASE='lazy'
$ev = Join-Path $OutDir 'badbase'
$rc = Invoke-Runner 'badbase' (Base-Args $ev 'sync,lazy') 1
Clear-Switches
Assert-Check ($rc -ne 0) 'record baseSHA != ExpectedHead must be rejected (F-A3)'

# 7) missing id / rows!=1 / non-full ExpectedHead rejected
Clear-Switches; $env:MOCK_NOID='sync'
$ev = Join-Path $OutDir 'noid'; $rc = Invoke-Runner 'noid' (Base-Args $ev 'sync,lazy') 1; Clear-Switches
Assert-Check ($rc -ne 0) 'missing original id must be rejected'
$env:MOCK_ROWS2='sync'; $ev = Join-Path $OutDir 'rows2'; $rc = Invoke-Runner 'rows2' (Base-Args $ev 'sync,lazy') 1; Clear-Switches
Assert-Check ($rc -ne 0) 'rows!=1 must be rejected'
$ev = Join-Path $OutDir 'badhead'
$rc = Invoke-Runner 'badhead' (@('-RepoDir',$runnerDir,'-ExpectedHead','deadbeef','-OutDir',$ev,'-Order','sync,lazy','-Execute','-CollectorScript',$mockCollector,'-SummarizerScript',$realSummary)) 1
Assert-Check ($rc -ne 0) 'non-full ExpectedHead must be rejected (F-A3)'

# 8) existing dir rejected; prepare rc=2 no samples
Clear-Switches
$ev = Join-Path $OutDir 'existing'; $null = New-Item -ItemType Directory -Path $ev
$rc = Invoke-Runner 'olddir' (Base-Args $ev 'sync,lazy') 1
Assert-Check ($rc -ne 0) 'existing evidence dir must be rejected'
$ev = Join-Path $OutDir 'prepare'
$rc = Invoke-Runner 'prepare' (@('-RepoDir',$runnerDir,'-ExpectedHead','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','-OutDir',$ev,'-Order','sync,lazy','-CollectorScript',$mockCollector,'-SummarizerScript',$realSummary)) 2
$prepSamples = @(Get-ChildItem -LiteralPath $ev -Directory | Where-Object { $_.Name -like 'sample-*' })
Assert-Check ($rc -eq 2) "prepare rc=$rc expected 2"
Assert-Check ($prepSamples.Count -eq 0) 'prepare must not produce sample dirs'
Assert-Check (Test-Path (Join-Path $ev 'prepare-plan.json')) 'prepare writes prepare-plan.json'

# 9) two behavior mutations run the SAME health guard to real FAIL (bounded), then restore PASS
function Clone-Runner([string]$name, [scriptblock]$edit) {
    $dst = Join-Path $mockDir ($name + '.ps1')
    (Get-Content -LiteralPath $runner -Raw | ForEach-Object { & $edit $_ }) | Set-Content -LiteralPath $dst -Encoding UTF8
    return $dst
}
$mutA = Clone-Runner 'runner-mut-continue' { param($t) $t -replace 'throw "collector fault rc=\$rc \(\$caseName\); stop scheduling, keep all recorded evidence"', 'Write-Host "MUT: continue"; continue' }
$mutB = Clone-Runner 'runner-mut-dupid' { param($t) $t -replace '\$derivedId = \$caseName \+ ''/'' \+ \$originalId', '$derivedId = $originalId' }

# Mutation A: fatal in middle -> healthy entry stops at 2 samples; mutated continues past (real behavior diff).
Clear-Switches; $env:MOCK_FATAL_ON='lazy'
$ev = Join-Path $OutDir 'mut-continue'
$env:MOCK_FATAL_DONE=$null
& $psExe -NoProfile -NonInteractive -File $mutA @('-RepoDir',$runnerDir,'-ExpectedHead','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','-OutDir',$ev,'-Order','sync,lazy,sync,lazy','-Execute','-CollectorScript',$mockCollector,'-SummarizerScript',$realSummary) *> (Join-Path $OutDir 'mut-continue.log')
$samplesA = @(Get-ChildItem -LiteralPath $ev -Directory -ErrorAction SilentlyContinue | Where-Object { $_.Name -like 'sample-*' })
Clear-Switches; $env:MOCK_FATAL_DONE=$null
Assert-Check ($samplesA.Count -gt 2) "mutA(continue-after-fatal) must run >2 samples, actual $($samplesA.Count)"

# Mutation B: no namespace on 4 balanced samples -> duplicate derived ids in summary/derived
Clear-Switches
$ev = Join-Path $OutDir 'mut-dupid'
& $psExe -NoProfile -NonInteractive -File $mutB @('-RepoDir',$runnerDir,'-ExpectedHead','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','-OutDir',$ev,'-Order','sync,lazy,sync,lazy','-Execute','-CollectorScript',$mockCollector,'-SummarizerScript',$realSummary) *> (Join-Path $OutDir 'mut-dupid.log')
$idsB = @(Get-Content -LiteralPath (Join-Path $ev 'derived\samples.jsonl') | Where-Object { $_.Trim() } | ForEach-Object { ($_ | ConvertFrom-Json).id })
$uniqB = @($idsB | Sort-Object -Unique)
Assert-Check ($uniqB.Count -lt $idsB.Count) "mutB(no-namespace) must produce duplicate derived ids; unique=$($uniqB.Count)/total=$($idsB.Count)"

Write-Host ("PASS: {0} checks; healthy->mutation-FAIL->restore, real summarizer end-to-end. Evidence: {1}" -f $check, $OutDir)
exit 0
