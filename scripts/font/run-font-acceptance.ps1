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
if ([string]::IsNullOrWhiteSpace($ExpectedHead) -or $ExpectedHead.Length -lt 7) {
    throw "ExpectedHead must be a full SHA, got '$ExpectedHead'"
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

Write-Host ("done: {0} samples, {1} derived, summary at {2}" -f $derivedLines.Count, $manifest.Count, $summaryDir)
exit 0
