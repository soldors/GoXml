#Requires -Version 5.1
<#
    test-font-acceptance-plan.ps1 - R24-A synthetic regression

    Uses ONLY temporary mock collector/summarizer and synthetic samples to verify the
    orchestration contract of run-font-acceptance.ps1. No Go build, no window, no
    sampling, no real font. The mock lives only in this test's temp dir; it is not the
    product flow.

    Covers:
      1) pre-registered alternating order, per-sample independent single-run (argv fidelity)
      2) legal-zero sample kept, invalid sample (rc=5) kept then continue
      3) fault code (e.g. rc=7) stops further scheduling immediately
      4) source samples.jsonl hash not rewritten; derived ids unique with original path/id/SHA
      5) missing file / duplicate original id handling / existing dir rejected / bad arg rejected
      6) prepare state (no -Execute) exits 2, does not call collector
      7) two behavior mutations: continue-after-fatal, concat-duplicate-original-id (cloned
         script broken) => target assertions must actually FAIL; healthy => PASS.
         Mutations are only in temp copies; repo scripts unchanged.
#>
param([string]$OutDir = (Join-Path $env:TEMP ('font-accept-plan-' + [guid]::NewGuid().ToString('N'))))
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (Test-Path -LiteralPath $OutDir) { throw 'test output exists; refusing to overwrite' }
$null = New-Item -ItemType Directory -Path $OutDir
$psExe = Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe'
$runner = Join-Path $PSScriptRoot 'run-font-acceptance.ps1'
$check = 0
function Assert-Check([bool]$c, [string]$m) { if (-not $c) { throw $m }; $script:check++ }

$mockDir = Join-Path $OutDir 'mock'
$null = New-Item -ItemType Directory -Path $mockDir
$mockCollector = Join-Path $mockDir 'mock-collect.ps1'
@'
param([string]$RepoDir,[string]$ExpectedHead,[string]$OutDir,[string]$Load,[switch]$CompareFonts,[int]$Runs,[switch]$Strict,[string]$LockFile)
$ErrorActionPreference='Stop'
if ($Runs -ne 1 -or -not $CompareFonts) { throw 'argv-invalid' }
$null = New-Item -ItemType Directory -Path $OutDir -Force
if ($env:MOCK_FATAL_ON -eq $Load -and -not $env:MOCK_FATAL_DONE) { $env:MOCK_FATAL_DONE='1'; exit 7 }
if ($env:MOCK_MISSING -eq $Load) { exit 0 }
$valid = if ($env:MOCK_INVALID -eq $Load) { 'false' } else { 'true' }
$rec = [ordered]@{ id='0001'; load=$Load; valid=[bool]::Parse($valid); baseSHA='deadbeef'; exeSha256='aa' }
$rec | ConvertTo-Json -Compress | Set-Content -LiteralPath (Join-Path $OutDir 'samples.jsonl') -Encoding UTF8
if ($valid -eq 'false') { exit 5 } else { exit 0 }
'@ | Set-Content -LiteralPath $mockCollector -Encoding UTF8

$mockSummary = Join-Path $mockDir 'mock-summarize.ps1'
@'
param([string]$InputDir,[string]$OutDir,[int]$MinSamplesForThreshold=30,[switch]$AllowMixed,[switch]$Overwrite)
$ErrorActionPreference='Stop'
if (-not $AllowMixed) { exit 3 }
$null = New-Item -ItemType Directory -Path $OutDir -Force
$n = (Get-Content -LiteralPath (Join-Path $InputDir 'samples.jsonl') | Where-Object { $_.Trim() }).Count
Set-Content -LiteralPath (Join-Path $OutDir 'summary.json') -Value ('{ "records": ' + $n + ' }') -Encoding UTF8
exit 0
'@ | Set-Content -LiteralPath $mockSummary -Encoding UTF8

function Invoke-Runner([string]$id, [string[]]$Argv, [int]$ExpectedRc) {
    $saved = $env:MOCK_FATAL_DONE
    $env:MOCK_FATAL_DONE = $null
    & $psExe -NoProfile -NonInteractive -File $runner @Argv *> (Join-Path $OutDir ($id + '.log'))
    $rc = $LASTEXITCODE
    [ordered]@{id=$id;argv=$Argv;rc=$rc;expected=$ExpectedRc} | ConvertTo-Json -Depth 6 -Compress |
        Add-Content -LiteralPath (Join-Path $OutDir 'results.jsonl') -Encoding UTF8
    $env:MOCK_FATAL_DONE = $saved
    return $rc
}
function Base-Args([string]$ev, [string]$order) {
    @('-RepoDir', $PSScriptRoot, '-ExpectedHead', 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeef',
      '-OutDir', $ev, '-Order', $order, '-Execute',
      '-CollectorScript', $mockCollector, '-SummarizerScript', $mockSummary)
}

# 1) healthy: alternating 6 samples, all 0 -> rc0; check order/derived/manifest/ids/no-overwrite
$ev = Join-Path $OutDir 'healthy'; $env:MOCK_FATAL_ON=$null; $env:MOCK_INVALID=$null; $env:MOCK_MISSING=$null
$rc = Invoke-Runner 'healthy' (Base-Args $ev 'sync,lazy,sync,lazy,sync,lazy') 0
Assert-Check ($rc -eq 0) "healthy rc=$rc expected 0 (see log)"
$order = (Get-Content -LiteralPath (Join-Path $ev 'order.json') -Raw | ConvertFrom-Json)
Assert-Check ((@($order) -join ',') -eq 'sync,lazy,sync,lazy,sync,lazy') 'order not frozen as pre-registered'
$derived = @(Get-Content -LiteralPath (Join-Path $ev 'derived\samples.jsonl') | Where-Object { $_.Trim() })
Assert-Check ($derived.Count -eq 6) "derived count=$($derived.Count) expected 6"
$manifest = Get-Content -LiteralPath (Join-Path $ev 'derived\manifest.json') -Raw | ConvertFrom-Json
Assert-Check (@($manifest).Count -eq 6) 'manifest entries expected 6'
$ids = @($manifest | ForEach-Object { $_.derivedId }) | Sort-Object -Unique
Assert-Check ($ids.Count -eq 6) 'derived ids must be unique (all original 0001 namespaced by dir)'
$first = $derived[0] | ConvertFrom-Json
Assert-Check ($first.aggregationSource.sha256.Length -eq 64) 'derived record needs original path/SHA/originalId'
Assert-Check ($first.aggregationSource.originalId -eq '0001') 'derived must keep originalId=0001'

# 2) source hash not rewritten
$src = Join-Path $ev 'sample-00-sync\samples.jsonl'
$h = (Get-FileHash -LiteralPath $src -Algorithm SHA256).Hash.ToLowerInvariant()
$m0 = @($manifest | Where-Object { $_.path -like '*sample-00-sync*' })[0]
Assert-Check ($m0.sha256 -eq $h) 'manifest.sha256 must equal original file hash (original untouched)'

# 3) invalid sample (rc=5) kept then continue
$ev2 = Join-Path $OutDir 'invalid'; $env:MOCK_INVALID='lazy'; $env:MOCK_FATAL_ON=$null; $env:MOCK_MISSING=$null
$rc = Invoke-Runner 'invalid' (Base-Args $ev2 'sync,lazy,sync,lazy') 0
$env:MOCK_INVALID=$null
Assert-Check ($rc -eq 0) "rc=5 invalid sample kept-continue, overall rc=$rc expected 0"
Assert-Check (Test-Path (Join-Path $ev2 'derived\samples.jsonl')) 'invalid sample still produces derived (kept, not dropped)'

# 4) fault rc=7 stops: pre-registered 6, lazy faults, only index0..1 run then stop
$ev3 = Join-Path $OutDir 'fatal'; $env:MOCK_FATAL_ON='lazy'; $env:MOCK_MISSING=$null
$rc = Invoke-Runner 'fatal' (Base-Args $ev3 'sync,lazy,sync,lazy,sync,lazy') 1
$env:MOCK_FATAL_ON=$null
Assert-Check ($rc -ne 0) "fault must be non-zero rc=$rc"
$samples = @(Get-ChildItem -LiteralPath $ev3 -Directory -ErrorAction SilentlyContinue | Where-Object { $_.Name -like 'sample-*' })
Assert-Check ($samples.Count -eq 2) "fault must stop after index1 (2 samples run), actual $($samples.Count)"

# 5) missing file -> fail-closed
$ev4 = Join-Path $OutDir 'missing'; $env:MOCK_MISSING='sync'; $env:MOCK_FATAL_ON=$null
$rc = Invoke-Runner 'missing' (Base-Args $ev4 'sync') 1
$env:MOCK_MISSING=$null
Assert-Check ($rc -ne 0) 'missing samples.jsonl must fail-closed'

# 6) existing dir rejected + bad mode rejected
$ev5 = Join-Path $OutDir 'existing'; $null = New-Item -ItemType Directory -Path $ev5
$rc = Invoke-Runner 'olddir' (Base-Args $ev5 'sync') 1
Assert-Check ($rc -ne 0) 'existing evidence dir must be rejected'
$rc = Invoke-Runner 'badmode' (@('-RepoDir',$PSScriptRoot,'-ExpectedHead','deadbeefdeadbeefdeadbeefdeadbeefdeadbeef','-OutDir',(Join-Path $OutDir 'badmode'),'-Order','sync,none','-Execute','-CollectorScript',$mockCollector,'-SummarizerScript',$mockSummary)) 1
Assert-Check ($rc -ne 0) 'none mode must be rejected (not a comparison sample)'

# 7) prepare state: without -Execute -> rc2, no sample dirs
$ev6 = Join-Path $OutDir 'prepare'; $env:MOCK_FATAL_DONE=$null
$rc = Invoke-Runner 'prepare' (@('-RepoDir',$PSScriptRoot,'-ExpectedHead','deadbeefdeadbeefdeadbeefdeadbeefdeadbeef','-OutDir',$ev6,'-Order','sync,lazy','-CollectorScript',$mockCollector,'-SummarizerScript',$mockSummary)) 2
Assert-Check ($rc -eq 2) "prepare rc=$rc expected 2 (prepare != pass)"
$prepSamples = @(Get-ChildItem -LiteralPath $ev6 -Directory | Where-Object { $_.Name -like 'sample-*' })
Assert-Check ($prepSamples.Count -eq 0) 'prepare must not produce sample dirs'
Assert-Check (Test-Path (Join-Path $ev6 'prepare-plan.json')) 'prepare should write prepare-plan.json'

# 8) two behavior mutations on cloned runner; target assertions must FAIL; healthy passes
function Clone-Runner([string]$name, [scriptblock]$edit) {
    $dst = Join-Path $mockDir ($name + '.ps1')
    $txt = Get-Content -LiteralPath $runner -Raw
    $txt = & $edit $txt
    Set-Content -LiteralPath $dst -Value $txt -Encoding UTF8
    return $dst
}
$mutA = Clone-Runner 'runner-mut-continue' {
    param($t)
    $t -replace 'throw "collector fault rc=\$rc \(\$caseName\); stop scheduling, keep all recorded evidence"',
                 'Write-Host "MUT: continue after fault"; continue'
}
$mutB = Clone-Runner 'runner-mut-dupid' {
    param($t)
    $t -replace '\$derivedId = \$caseName \+ ''/'' \+ \$originalId',
                 '$derivedId = $originalId'
}

$evA = Join-Path $OutDir 'mut-continue'; $env:MOCK_FATAL_ON='lazy'; $env:MOCK_MISSING=$null; $env:MOCK_FATAL_DONE=$null
& $psExe -NoProfile -NonInteractive -File $mutA @('-RepoDir',$PSScriptRoot,'-ExpectedHead','deadbeefdeadbeefdeadbeefdeadbeefdeadbeef','-OutDir',$evA,'-Order','sync,lazy,sync,lazy,sync,lazy','-Execute','-CollectorScript',$mockCollector,'-SummarizerScript',$mockSummary) *> (Join-Path $OutDir 'mut-continue.log')
$env:MOCK_FATAL_ON=$null; $env:MOCK_FATAL_DONE=$null
$samplesA = @(Get-ChildItem -LiteralPath $evA -Directory -ErrorAction SilentlyContinue | Where-Object { $_.Name -like 'sample-*' })
Assert-Check ($samplesA.Count -gt 2) "mutA(continue-after-fatal) must run >2 samples to expose defect, actual $($samplesA.Count) (==2 means mutation ineffective)"

$evB = Join-Path $OutDir 'mut-dupid'; $env:MOCK_FATAL_ON=$null; $env:MOCK_FATAL_DONE=$null; $env:MOCK_MISSING=$null
& $psExe -NoProfile -NonInteractive -File $mutB @('-RepoDir',$PSScriptRoot,'-ExpectedHead','deadbeefdeadbeefdeadbeefdeadbeefdeadbeef','-OutDir',$evB,'-Order','sync,lazy,sync,lazy','-Execute','-CollectorScript',$mockCollector,'-SummarizerScript',$mockSummary) *> (Join-Path $OutDir 'mut-dupid.log')
$idsB = @(Get-Content -LiteralPath (Join-Path $evB 'derived\samples.jsonl') | Where-Object { $_.Trim() } | ForEach-Object { ($_ | ConvertFrom-Json).id })
$uniqB = @($idsB | Sort-Object -Unique)
Assert-Check ($uniqB.Count -lt $idsB.Count) "mutB(no-namespace) must produce duplicate derived ids so uniqueness assertion fails; unique=$($uniqB.Count)/total=$($idsB.Count) (equal means mutation ineffective)"

Write-Host ("PASS: {0} checks; healthy->mutation-FAIL->restore covered. Evidence: {1}" -f $check, $OutDir)
exit 0
