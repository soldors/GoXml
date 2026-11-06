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
            fontInitAtNs=100; fontReadyAtNs=$(if ($zero) { 100 } else { 200 }); fontReadyElapsedNs=$fr; fontToFirstFrameElapsedNs=$(if ($zero) { 0 } else { 50 });
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
if ($Runs -ne 1 -or -not $CompareFonts) { throw 'argv-invalid' }
$null = New-Item -ItemType Directory -Path $OutDir -Force
function New-Record([string]$id,[string]$load,[string]$head,[bool]$valid,[bool]$zero) {
  $m1 = if ($zero) { 0 } else { 20 }; $fr = if ($zero) { 0 } else { 100 }; $first = if ($zero) { 100 } else { 150 }
  [ordered]@{ id=$id; measurementContract='font-init-v1'; runtimeProcsPolicy='inherit'; runtimeProcsObserved=2; agent='MOCK'; baseSHA=$head; load=$load; requestedFrames=3; args='MOCK'; exeSha256='fixture'; sourceSha256='fixture'; pid=0; handleCached=$false; killConfirmName=''; start='2026-11-06T01:00:00+08:00'; end='2026-11-06T01:00:00+08:00'; durationMs=4; exitCode=0; exitCodeKnown=$true; terminatedByCollector=$false; probeTimeout=$false; timedOut=$false; verdict='OK'; stdoutEmpty=$false; stdoutPath=''; stderrPath=''; stdoutTail=@('RESULT verdict=OK'); stderrTail=@(); observed=[ordered]@{ fontComparisonContract='font-init-v1'; fontComparisonMeasured='true'; fontComparisonColdProvider='true'; fontInitAtNs=100; fontReadyAtNs=$(if ($zero) { 100 } else { 200 }); fontReadyElapsedNs=$fr; fontToFirstFrameElapsedNs=$(if ($zero) { 0 } else { 50 }); m1ElapsedNs=$m1; m1ElapsedNsPresent=$true; m1Measured=$true; layoutFirstEntryNs=151; layoutFirstEntryNsPresent=$true; layoutFirstAtMeasured=$true; framesObserved=3; frameWindowNs=$null; procStartNs=$null; ctorEntryNs=$null; ctorDoneNs=$null; createdAtNs=$null; firstFrameAtNs=$first; lazyState=$null; lazyTempElapsedNs=$null; lazyProbeElapsedNs=$null; lazyLoadElapsedNs=$null; lazyProbeRan=$null }; valid=$valid; belowGranularity=$false; invalidReasons=@(); verificationRun=$false }
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
function Clear-Switches() { $env:MOCK_FATAL_ON=$null; $env:MOCK_INVALID=$null; $env:MOCK_MISSING=$null; $env:MOCK_ROWS2=$null; $env:MOCK_NOID=$null; $env:MOCK_ZERO=$null; $env:MOCK_WRONGMODE=$null; $env:MOCK_BADBASE=$null; $env:MOCK_FORGE=$null }

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

# Additional direct contracts: zero measurements, order-file, and missing records.
Clear-Switches; $env:MOCK_ZERO='sync'
$ev = Join-Path $OutDir 'legal-zero'
$rc = Invoke-Runner 'legal-zero' (Base-Args $ev 'sync,lazy') 0
Clear-Switches
$sj = Get-Content -LiteralPath (Join-Path $ev 'summary/summary.json') -Raw | ConvertFrom-Json
$zeroRecord = Get-Content -LiteralPath (Join-Path $ev 'sample-00-sync/samples.jsonl') -Raw | ConvertFrom-Json
Assert-Check ($rc -eq 0 -and $sj.valid -eq 2 -and $sj.rejected -eq 0) 'legal measured zero must reach the real summary'
Assert-Check ($zeroRecord.observed.m1ElapsedNs -eq 0 -and $zeroRecord.observed.fontReadyElapsedNs -eq 0) 'zero fixture must actually contain zero measurements'
$zeroStats = @($sj.groups | Where-Object { $_.groupKey -like '*|sync|*' } | ForEach-Object { $_.stats } | Where-Object { $_.metric -eq 'm1ElapsedNs' })
Assert-Check ($zeroStats.Count -eq 1 -and $zeroStats[0].median -eq 0) 'real summary must preserve zero median'

$orderPath = Join-Path $OutDir 'order-input.txt'
Set-Content -LiteralPath $orderPath -Value @('sync','lazy') -Encoding UTF8
$ev = Join-Path $OutDir 'order-file'
$rc = Invoke-Runner 'order-file' (@('-RepoDir',$runnerDir,'-ExpectedHead','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','-OutDir',$ev,'-OrderFile',$orderPath,'-Execute','-CollectorScript',$mockCollector,'-SummarizerScript',$realSummary)) 0
Assert-Check ($rc -eq 0) 'OrderFile alternating pair must succeed'
foreach ($badOrder in @('sync,lazy,sync','sync,sync,lazy,lazy')) {
    $caseId = 'bad-order-' + $badOrder.Replace(',','-')
    $ev = Join-Path $OutDir $caseId
    $rc = Invoke-Runner $caseId (Base-Args $ev $badOrder) 1
    Assert-Check ($rc -ne 0 -and -not (Test-Path -LiteralPath $ev)) 'invalid order must fail before creating evidence'
}
Clear-Switches; $env:MOCK_MISSING='sync'
$ev = Join-Path $OutDir 'missing-record'
$rc = Invoke-Runner 'missing-record' (Base-Args $ev 'sync,lazy') 1
Clear-Switches
Assert-Check ($rc -ne 0 -and -not (Test-Path (Join-Path $ev 'derived'))) 'missing record must stop before derivation'

# Real collector rejects a nonexistent audited SHA before lock/build/window launch.
$ev = Join-Path $OutDir 'unknown-head'
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $runnerDir '../..'))
$rc = Invoke-Runner 'unknown-head' (@('-RepoDir',$repoRoot,'-ExpectedHead',('0'*40),'-OutDir',$ev,'-Order','sync,lazy','-Execute')) 1
$collectorLog = Get-Content -LiteralPath (Join-Path $ev 'sample-00-sync.log') -Raw
Assert-Check ($rc -ne 0 -and $collectorLog -match 'baseline mismatch') 'unknown actual SHA must be rejected by real collector precheck'
Assert-Check (-not (Test-Path (Join-Path $ev 'sample-00-sync'))) 'identity precheck must not create a sample/build directory'

# Same executable health contract for healthy -> mutant FAIL -> restored PASS.
$guard = Join-Path $mockDir 'health-contract.ps1'
@'
param([string]$Runner,[string]$Collector,[string]$Summary,[string]$Repo,[string]$Ev,[string]$Kind)
$ErrorActionPreference='Stop'
$ps = Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe'
$env:MOCK_FATAL_ON = if ($Kind -eq 'fatal') { 'lazy' } else { $null }
$env:MOCK_FATAL_DONE=$null
# Native stderr is expected for the fatal health case; inspect rc explicitly.
$ErrorActionPreference='Continue'
& $ps -NoProfile -NonInteractive -File $Runner -RepoDir $Repo -ExpectedHead aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa -OutDir $Ev -Order sync,lazy,sync,lazy -Execute -CollectorScript $Collector -SummarizerScript $Summary *> ($Ev+'.runner.log')
$rc=$LASTEXITCODE
$ErrorActionPreference='Stop'
if ($Kind -eq 'fatal') {
    $dirs=@(Get-ChildItem -LiteralPath $Ev -Directory | Where-Object { $_.Name -like 'sample-*' })
    if ($rc -eq 0 -or $dirs.Count -ne 2 -or (Test-Path (Join-Path $Ev 'derived'))) { throw "CONTRACT-FAIL fatal: rc=$rc scheduled=$($dirs.Count), expected stop at 2" }
} else {
    if ($rc -ne 0) { throw "CONTRACT-FAIL healthy runner rc=$rc" }
    $ids=@(Get-Content -LiteralPath (Join-Path $Ev 'derived/samples.jsonl') | ForEach-Object { ($_ | ConvertFrom-Json).id })
    if ($ids.Count -ne 4 -or @($ids | Sort-Object -Unique).Count -ne 4) { throw 'CONTRACT-FAIL unique: four distinct derived IDs required' }
}
Write-Host "CONTRACT-PASS $Kind"
exit 0
'@ | Set-Content -LiteralPath $guard -Encoding UTF8

$source = Get-Content -LiteralPath $runner -Raw
$mutA = Join-Path $mockDir 'runner-mut-continue.ps1'
$textA = $source.Replace('throw "collector fault rc=$rc ($caseName); stop scheduling, keep all recorded evidence"','Write-Host "MUT: continue"; continue')
Assert-Check ($textA -ne $source) 'fatal mutation anchor must match'
$textA | Set-Content -LiteralPath $mutA -Encoding UTF8
$mutB = Join-Path $mockDir 'runner-mut-dupid.ps1'
$textB = $source.Replace('$derivedId = $caseName + ''/'' + $originalId','$derivedId = $originalId')
Assert-Check ($textB -ne $source) 'ID mutation anchor must match'
$textB | Set-Content -LiteralPath $mutB -Encoding UTF8
foreach ($contract in @(@{kind='fatal'; mutant=$mutA},@{kind='unique'; mutant=$mutB})) {
    foreach ($phase in @('healthy','mutant','restored')) {
        Clear-Switches
        $id = 'contract-' + $contract.kind + '-' + $phase
        $selected = if ($phase -eq 'mutant') { $contract.mutant } else { $runner }
        $ev = Join-Path $OutDir $id
        $log = Join-Path $OutDir ($id+'.log')
        & $psExe -NoProfile -NonInteractive -File $guard -Runner $selected -Collector $mockCollector -Summary $realSummary -Repo $runnerDir -Ev $ev -Kind $contract.kind *> $log
        $rc=$LASTEXITCODE
        [ordered]@{id=$id;runner=$selected;guard=$guard;rc=$rc} | ConvertTo-Json -Compress | Add-Content -LiteralPath (Join-Path $OutDir 'results.jsonl') -Encoding UTF8
        $guardText=Get-Content -LiteralPath $log -Raw
        if ($phase -eq 'mutant') {
            Assert-Check ($rc -ne 0 -and $guardText -match 'CONTRACT-FAIL') "$id must actually fail the same executable health guard"
        } else {
            Assert-Check ($rc -eq 0 -and $guardText -match 'CONTRACT-PASS') "$id must pass the same executable health guard"
        }
    }
}
Clear-Switches
Write-Host ("PASS: {0} checks; real summarizer, actual health-guard FAIL on both mutants, healthy/restored PASS. Evidence: {1}" -f $check, $OutDir)
exit 0
