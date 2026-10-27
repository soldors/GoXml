#Requires -Version 5.1
# Parser/summary regressions only: synthetic fixtures, no Go build or window.
param([string]$OutDir = (Join-Path $env:TEMP ('font-compare-tests-' + [guid]::NewGuid().ToString('N'))))
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (Test-Path -LiteralPath $OutDir) { throw 'Test output already exists; evidence will not be overwritten' }
$null = New-Item -ItemType Directory -Path $OutDir
$psExe = Join-Path $env:WINDIR 'System32/WindowsPowerShell/v1.0/powershell.exe'
$collector = Join-Path $PSScriptRoot 'collect-m1.ps1'
$summary = Join-Path $PSScriptRoot 'summarize-m1.ps1'
$ledger = Join-Path $OutDir 'results.jsonl'
$script:checks = 0
function Assert-Check([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
    $script:checks++
}
function Invoke-TestCommand([string]$Id, [string]$Script, [string[]]$Argv, [int]$ExpectedExit = 0) {
    $start = Get-Date
    & $psExe -NoProfile -NonInteractive -File $Script @Argv *> (Join-Path $OutDir ($Id + '.log'))
    $rc = $LASTEXITCODE
    [ordered]@{id=$Id;script=$Script;argv=$Argv;start=$start.ToString('o');end=(Get-Date).ToString('o');rc=$rc} |
        ConvertTo-Json -Depth 5 -Compress | Add-Content -LiteralPath $ledger -Encoding UTF8
    Assert-Check ($rc -eq $ExpectedExit) "$Id exit=$rc, expected=$ExpectedExit; see retained log"
}
$baseFields = [ordered]@{
    m1ElapsedNs='20';m1Measured='true';layoutFirstEntryNs='151';layoutFirstAtMeasured='true';framesObserved='3'
    firstFrameAtNs='150';fontComparisonContract='font-init-v1';fontComparisonMeasured='true'
    fontComparisonColdProvider='true';fontInitAtNs='100';fontReadyAtNs='200'
    fontReadyElapsedNs='100';fontToFirstFrameElapsedNs='50'
}
$cases = @(
    @{id='ready-after-frame';valid=$true;changes=@{};remove=@()},
    @{id='ready-before-frame';valid=$true;changes=@{fontReadyAtNs='120';fontReadyElapsedNs='20'};remove=@()},
    @{id='measured-zero';valid=$true;changes=@{m1ElapsedNs='0';firstFrameAtNs='100';fontReadyAtNs='100';fontReadyElapsedNs='0';fontToFirstFrameElapsedNs='0'};remove=@()},
    @{id='missing-ready';valid=$false;changes=@{};remove=@('fontReadyAtNs')},
    @{id='duration-mismatch';valid=$false;changes=@{fontReadyElapsedNs='999'};remove=@()},
    @{id='not-ready';valid=$false;changes=@{fontComparisonMeasured='false'};remove=@()},
    @{id='warm-provider';valid=$false;changes=@{fontComparisonColdProvider='false'};remove=@()},
    @{id='noninteger';valid=$false;changes=@{fontReadyElapsedNs='NaN'};remove=@()},
    @{id='reversed';valid=$false;changes=@{fontReadyAtNs='99';fontReadyElapsedNs='0'};remove=@()},
    @{id='unknown-contract';valid=$false;changes=@{fontComparisonContract='future'};remove=@()}
    @{id='missing-runtime';valid=$false;changes=@{};remove=@()}
)
$good = $null
foreach ($case in $cases) {
    $fields = [ordered]@{}
    foreach ($key in $baseFields.Keys) { $fields[$key] = $baseFields[$key] }
    foreach ($key in $case.changes.Keys) { $fields[$key] = $case.changes[$key] }
    foreach ($key in $case.remove) { $fields.Remove($key) }
    $stdout = @($fields.Keys | ForEach-Object { 'RESULT_DATA ' + $_ + '=' + $fields[$_] }) + @('RESULT verdict=OK')
    if ($case.id -ne 'missing-runtime') { $stdout = @('ENV go=fixture os=windows arch=amd64 gomaxprocs=2') + $stdout }
    $inputPath = Join-Path $OutDir ($case.id + '.stdout.txt')
    $recordPath = Join-Path $OutDir ($case.id + '.json')
    Set-Content -LiteralPath $inputPath -Value $stdout -Encoding UTF8
    Invoke-TestCommand $case.id $collector @('-ParseFixture',$inputPath,'-FixtureOut',$recordPath,'-Load','lazy','-CompareFonts')
    $record = Get-Content -LiteralPath $recordPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-Check ($record.valid -eq $case.valid) ($case.id + ': wrong validity classification')
    Assert-Check ($record.measurementContract -eq 'font-init-v1') 'Comparison provenance lost'
    if ($case.id -eq 'ready-after-frame') { $good = $record }
    if ($case.id -eq 'measured-zero') {
        Assert-Check ($record.observed.fontReadyElapsedNs -eq 0 -and $record.observed.fontToFirstFrameElapsedNs -eq 0) 'Measured zeros lost'
    }
}
# Synthetic, labelled provenance is supplied only to exercise the summary, never
# submitted as an actual process measurement.
foreach ($requested in @(2,8)) {
    $recordPath = Join-Path $OutDir ("runtime-request-" + $requested + '.json')
    Invoke-TestCommand ("runtime-request-" + $requested) $collector @('-ParseFixture',(Join-Path $OutDir 'ready-after-frame.stdout.txt'),'-FixtureOut',$recordPath,'-Load','lazy','-CompareFonts','-RuntimeProcs',([string]$requested))
    $requestedRecord = Get-Content -LiteralPath $recordPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-Check ($requestedRecord.valid -eq ($requested -eq 2)) 'Explicit runtime request was not checked against observed ENV'
}
$good.id = 'synthetic-comparison'
$good.baseSHA = 'fixture'
$good.exeSha256 = 'fixture'
$good.sourceSha256 = 'fixture'
$good | Add-Member -NotePropertyName verificationRun -NotePropertyValue $false
$positiveDir = Join-Path $OutDir 'positive'
$null = New-Item -ItemType Directory -Path $positiveDir
# samples.jsonl requires one JSON object per line.
$good | ConvertTo-Json -Depth 12 -Compress | Set-Content -LiteralPath (Join-Path $positiveDir 'samples.jsonl') -Encoding UTF8
Invoke-TestCommand 'summary-positive' $summary @('-InputDir',$positiveDir)
$result = Get-Content -LiteralPath (Join-Path $positiveDir 'summary.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Assert-Check ($result.valid -eq 1 -and $result.rejected -eq 0) 'Good comparison was rejected'
$readyStat = @($result.groups[0].stats | Where-Object metric -eq 'fontReadyElapsedNs')
Assert-Check ($readyStat.Count -eq 1 -and $readyStat[0].median -eq 100) 'Readiness metric missing or wrong'

$forgedDir = Join-Path $OutDir 'forged'
$null = New-Item -ItemType Directory -Path $forgedDir
$forged = $good | ConvertTo-Json -Depth 12 | ConvertFrom-Json
$forged.observed.fontReadyElapsedNs = 999
$forged | ConvertTo-Json -Depth 12 -Compress | Set-Content -LiteralPath (Join-Path $forgedDir 'samples.jsonl') -Encoding UTF8
Invoke-TestCommand 'summary-forged-valid' $summary @('-InputDir',$forgedDir)
$result = Get-Content -LiteralPath (Join-Path $forgedDir 'summary.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Assert-Check ($result.valid -eq 0 -and $result.rejected -gt 0) 'Forged valid=true bypassed summary revalidation'

$legacy = $good | ConvertTo-Json -Depth 12 | ConvertFrom-Json
$legacy.id = 'synthetic-legacy-m1'
$legacy.PSObject.Properties.Remove('measurementContract')
$legacy.PSObject.Properties.Remove('runtimeProcsPolicy')
$legacy.PSObject.Properties.Remove('runtimeProcsObserved')
foreach ($name in @('fontComparisonContract','fontComparisonMeasured','fontComparisonColdProvider','fontInitAtNs','fontReadyAtNs','fontReadyElapsedNs','fontToFirstFrameElapsedNs')) {
    $legacy.observed.PSObject.Properties.Remove($name)
}
$mixedDir = Join-Path $OutDir 'mixed'
$null = New-Item -ItemType Directory -Path $mixedDir
@(($good | ConvertTo-Json -Depth 12 -Compress), ($legacy | ConvertTo-Json -Depth 12 -Compress)) |
    Set-Content -LiteralPath (Join-Path $mixedDir 'samples.jsonl') -Encoding UTF8
Invoke-TestCommand 'summary-mixed-rejected' $summary @('-InputDir',$mixedDir) 2
Invoke-TestCommand 'summary-mixed-separated' $summary @('-InputDir',$mixedDir,'-OutDir',(Join-Path $OutDir 'separated'),'-AllowMixed')
$result = Get-Content -LiteralPath (Join-Path $OutDir 'separated/summary.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Assert-Check ($result.groupCount -eq 2 -and $result.valid -eq 2) 'Legacy M1 and comparison merged or old M1 rejected'

$runtimeBadDir = Join-Path $OutDir 'runtime-forged'
$null = New-Item -ItemType Directory -Path $runtimeBadDir
$runtimeBad = $good | ConvertTo-Json -Depth 12 | ConvertFrom-Json
$runtimeBad.runtimeProcsObserved = 0
$runtimeBad | ConvertTo-Json -Depth 12 -Compress | Set-Content -LiteralPath (Join-Path $runtimeBadDir 'samples.jsonl') -Encoding UTF8
Invoke-TestCommand 'summary-runtime-forged' $summary @('-InputDir',$runtimeBadDir)
$result = Get-Content -LiteralPath (Join-Path $runtimeBadDir 'summary.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Assert-Check ($result.valid -eq 0 -and $result.rejected -gt 0) 'Unknown runtime accepted as valid'

$runtimeMixedDir = Join-Path $OutDir 'runtime-mixed'
$null = New-Item -ItemType Directory -Path $runtimeMixedDir
$runtimeOther = $good | ConvertTo-Json -Depth 12 | ConvertFrom-Json
$runtimeOther.id = 'synthetic-auto-runtime'
$runtimeOther.runtimeProcsPolicy = 'auto'
$runtimeOther.runtimeProcsObserved = 20
@(($good | ConvertTo-Json -Depth 12 -Compress), ($runtimeOther | ConvertTo-Json -Depth 12 -Compress)) |
    Set-Content -LiteralPath (Join-Path $runtimeMixedDir 'samples.jsonl') -Encoding UTF8
Invoke-TestCommand 'summary-runtime-mixed-rejected' $summary @('-InputDir',$runtimeMixedDir) 2
Invoke-TestCommand 'summary-runtime-separated' $summary @('-InputDir',$runtimeMixedDir,'-OutDir',(Join-Path $OutDir 'runtime-separated'),'-AllowMixed')
$result = Get-Content -LiteralPath (Join-Path $OutDir 'runtime-separated/summary.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Assert-Check ($result.groupCount -eq 2 -and $result.valid -eq 2) 'Different runtime limits merged'
Invoke-TestCommand 'reject-none-comparison' $collector @('-CompareFonts','-Load','none') 2
Write-Host "PASS checks=$script:checks evidence=$OutDir"
