# Shared validation for probe-only font-init-v1 records (PowerShell 5.1).
function Get-FontComparisonErrors {
    param([object]$Observed, [string]$LoadMode)
    if ($LoadMode -ne 'sync' -and $LoadMode -ne 'lazy') { 'comparison-load-must-be-sync-or-lazy' }
    $values = @{}
    foreach ($key in @('fontComparisonContract', 'fontComparisonMeasured', 'fontComparisonColdProvider',
                       'fontInitAtNs', 'fontReadyAtNs', 'firstFrameAtNs',
                       'fontReadyElapsedNs', 'fontToFirstFrameElapsedNs')) {
        $p = if ($null -eq $Observed) { $null } else { $Observed.PSObject.Properties[$key] }
        $values[$key] = if ($null -eq $p) { $null } else { $p.Value }
    }
    if ($values.fontComparisonContract -ne 'font-init-v1') { 'comparison-contract-missing-or-unsupported' }
    foreach ($key in @('fontComparisonMeasured', 'fontComparisonColdProvider')) {
        if ([string]$values[$key] -ne 'true') { "$key-not-true" }
    }
    $numbers = @{}
    foreach ($key in @('fontInitAtNs', 'fontReadyAtNs', 'firstFrameAtNs',
                       'fontReadyElapsedNs', 'fontToFirstFrameElapsedNs')) {
        $n = 0L
        if ($null -eq $values[$key] -or -not [long]::TryParse([string]$values[$key], [ref]$n)) {
            "$key-missing-or-noninteger"
        } elseif ($n -lt 0 -or ($key -like '*AtNs' -and $n -eq 0)) {
            "$key-negative-or-zero-timestamp"
        } else { $numbers[$key] = $n }
    }
    if ($numbers.Count -eq 5) {
        if ($numbers.fontReadyAtNs -lt $numbers.fontInitAtNs -or
            $numbers.firstFrameAtNs -lt $numbers.fontInitAtNs) { 'comparison-reversed-interval' }
        if (($numbers.fontReadyAtNs - $numbers.fontInitAtNs) -ne $numbers.fontReadyElapsedNs) {
            'comparison-readiness-duration-mismatch'
        }
        if (($numbers.firstFrameAtNs - $numbers.fontInitAtNs) -ne $numbers.fontToFirstFrameElapsedNs) {
            'comparison-first-frame-duration-mismatch'
        }
    }
}
