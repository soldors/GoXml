#Requires -Version 5.1
<#
    collect-m1.ps1 - R2-WB-SAMPLER-HARDEN
    Orchestrates repeated, traceable m1nightprobe window samples.

    Contract notes (must be quoted together with any numbers produced):
      * M1 measures App construction start -> first frame processing start.
        It is NOT process startup. procStartNs is recorded for context only and
        must never be reported as the M1 start.
      * One sample == one independent OS process. In-process repetition is never
        used to inflate sample counts.
      * A sample is VALID only when all of these hold: the process was not
        terminated by this collector, its exit code was actually observed
        (null => invalid, never substituted), exit code 0, RESULT verdict=OK,
        m1Measured=true with a PRESENT and NUMERIC m1ElapsedNs,
        layoutFirstAtMeasured=true with a PRESENT and POSITIVE
        layoutFirstEntryNs, and framesObserved >= -Frames.
        A legal zero m1ElapsedNs stays valid and is flagged belowGranularity.
        Failures, probe timeouts and collector terminations are all preserved
        and counted separately; none is dropped.
      * Difference between sync and lazy construction intervals is NOT a "net
        gain" and is never computed as such by this script.
      * This script does not set, freeze or upgrade any threshold.

    Launch mechanism (R2, verified - see report diag-handle.ps1):
      Start-Process -PassThru, then READ $proc.Handle, then WaitForExit(ms).
      Reading .Handle caches the OS handle; without it ExitCode stays $null for
      every wait pattern (measured: exit-5 helper -> null without Handle, 5 with
      Handle). The bounded wait keeps the kill path available, so a hanging
      child is terminated by the collector instead of blocking forever.

    R3 fixes (the three gaps the coordinator closed):
      1. Build: WaitForExit(ms) now has a parameterised ceiling (BuildTimeoutSec)
         and its RETURN VALUE is used. A `go` that outlives the window is
         terminated after re-confirming PID + process name, then given a grace
         period; if it is still alive the run aborts with exit 3 instead of
         continuing. Same for every sample: a cleanup failure breaks the loop
         (exit 7) so no sample starts while a probe is still running.
      2. Lock: ONE outer try/finally wraps the entire lock-holding section, and
         the sampling finally has a nested try/finally so a Write-RunState
         failure can no longer skip the release. `exit` inside try{} does run
         finally{} (measured with a real child: exitCode 7, "CHILD_FINALLY").
      3. Arguments: Start-Process does NOT quote -ArgumentList elements. Every
         launch now goes through Invoke-BoundedProcess, which applies Windows
         argv quoting (Format-WinArg). The raw argv array is preserved in
         run.json / each sample alongside the quoted command line.

    Exit codes:
      0 = completed; every requested sample was attempted and recorded
      2 = usage / baseline / build-input / output-uniqueness rejection
         (raised before any file is written)
      3 = probe build failure, or a build child that could not be cleaned up
      4 = resource lock wait timeout (sampling not started)
      5 = -Strict was set and at least one sample was invalid
      6 = baseline or source file changed between pre-check and post-build
      7 = a sample child outlived its bounded cleanup; the loop was aborted
      8 = run evidence could not be persisted; raw logs may be incomplete
#>
[CmdletBinding()]
param(
    # Not [Parameter(Mandatory)] on purpose: -ParseFixture must be able to run
    # without a repository. Normal mode re-checks them explicitly below.
    [string]$RepoDir,

    [string]$ExpectedHead,

    [ValidateSet('none', 'lazy', 'sync')]
    [string]$Load = 'none',

    # Adds font-init-v1 intervals and waits for the final font theme to be ready.
    # Ordinary M1 collection keeps its original, non-waiting behavior.
    [switch]$CompareFonts,

    # -1: inherit caller; 0: Go automatic default; positive: explicit runtime P.
    # Build parallelism stays bounded independently and never leaks to samples.
    [ValidateRange(-1, 1024)]
    [int]$RuntimeProcs = -1,

    [ValidateRange(1, 10000)]
    [int]$Runs = 1,

    [string]$OutDir,

    [ValidateRange(1, 10000)]
    [int]$Frames = 3,

    [ValidateRange(1, 3600)]
    [int]$TimeoutSec = 20,

    [ValidateRange(1, 3600)]
    [int]$HardTimeoutSec = 45,

    [ValidateRange(1, 600)]
    [int]$KillGraceSec = 10,

    # Wall-clock ceiling for the Go build. R3: the old code called
    # WaitForExit(300000) and DISCARDED the result, so a wedged `go` could
    # survive the script. The ceiling is now a parameter and the bounded wait's
    # return value drives a targeted cleanup.
    [ValidateRange(10, 3600)]
    [int]$BuildTimeoutSec = 300,

    [string]$LockFile = 'D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock',

    [ValidateRange(0, 20)]
    [int]$LockWaitMinutes = 20,

    [switch]$SkipLock,

    [switch]$Strict,

    # Fixture mode: parse a captured stdout text file and exit. No git check,
    # no build, no window. Used only to prove how the parser classifies stdout.
    [string]$ParseFixture,

    [int]$FixtureExitCode = 0,

    [switch]$FixtureExitCodeUnknown,

    [switch]$FixtureTimedOut,

    [switch]$FixtureTerminated,

    [string]$FixtureOut = 'fixture-record.json',

    [switch]$FixtureOverwrite,

    # VERIFICATION HOOK - not for measurement runs.
    # Lets the kill / exit-code path be driven by a real helper process
    # (exit-5, success, hanging) instead of the probe. When set, the Go build is
    # skipped and every artifact is flagged verificationRun=true so such records
    # can never be mistaken for probe samples or merged with them.
    [string]$Executable,

    # Arguments are passed as ONE '|'-delimited string: when this script is run
    # through "powershell.exe -File", a [string[]] parameter binds only a single
    # value and the remaining tokens leak into the next parameters. Verified by
    # the VER-01 failure ("-Load exit5" from a ValidateSet error).
    [string]$ExecutableArgLine = '',

    [string]$ExecutableLabel = '',

    [string]$Agent = 'WorkBuddy'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

. (Join-Path $PSScriptRoot 'font-comparison.ps1')
if ($CompareFonts -and $Load -notin @('sync', 'lazy')) {
    Write-Host 'REJECT CompareFonts requires Load sync or lazy'
    exit 2
}

$script:LockHeld = $false
$script:RunPath = $null
$script:ExitCode = 0
$runtimeProcsPolicy = $(if ($RuntimeProcs -lt 0) { 'inherit' } elseif ($RuntimeProcs -eq 0) { 'auto' } else { [string]$RuntimeProcs })

function Write-Line { param([string]$Message) Write-Host $Message }

# Rejections raised here happen BEFORE any artifact is written.
function Stop-Precheck {
    param([string]$Message, [int]$Code = 2)
    Write-Line ("REJECT " + $Message)
    exit $Code
}

function Get-FileSha256 {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return $null }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

# Workaround for hosts whose environment contains case-duplicate variable names
# (e.g. both https_proxy and HTTPS_PROXY): Start-Process builds a case-sensitive
# dictionary from the environment and throws "item with the same key has already
# been added". Removing the lowercase duplicate affects THIS process only.
function Repair-EnvDuplicates {
    foreach ($n in @('https_proxy', 'http_proxy', 'all_proxy', 'no_proxy', 'ftp_proxy')) {
        $low = [System.Environment]::GetEnvironmentVariable($n)
        $up = [System.Environment]::GetEnvironmentVariable($n.ToUpperInvariant())
        if (($null -ne $low) -and ($null -ne $up)) {
            Remove-Item ("Env:" + $n) -ErrorAction SilentlyContinue
            Write-Line ("ENV_DEDUP removed process-local duplicate: " + $n)
        }
    }
}

# RESULT_DATA values are plain text; parse defensively instead of casting.
function Get-LongFieldOrNull {
    param([hashtable]$Fields, [string]$Key)
    if (-not $Fields.ContainsKey($Key)) { return $null }
    $tmp = 0L
    if ([long]::TryParse($Fields[$Key], [ref]$tmp)) { return $tmp }
    return $null
}

function Get-StringFieldOrNull {
    param([hashtable]$Fields, [string]$Key)
    if (-not $Fields.ContainsKey($Key)) { return $null }
    return $Fields[$Key]
}

function Test-LongFieldPresent {
    param([hashtable]$Fields, [string]$Key)
    return $Fields.ContainsKey($Key)
}

# ---------------------------------------------------------------- lock
function Try-AcquireLock {
    param([string]$Path, [string]$AgentName)
    try {
        $fs = [System.IO.File]::Open($Path, [System.IO.FileMode]::CreateNew,
            [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
    } catch {
        return $false
    }
    $payload = @{ agent = $AgentName; pid = $PID; startedAt = (Get-Date).ToString('o'); purpose = 'R2-WB-SAMPLER-HARDEN' } | ConvertTo-Json -Compress
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($payload)
    try { $fs.Write($bytes, 0, $bytes.Length); $fs.Flush() } finally { $fs.Dispose() }
    return $true
}

function Release-Lock {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return }
    $mine = $false
    try {
        $raw = Get-Content -LiteralPath $Path -Raw -Encoding UTF8 -ErrorAction Stop
        if ($raw) {
            $doc = $raw | ConvertFrom-Json
            if ($doc.agent -eq $Agent -and [int]$doc.pid -eq $PID) { $mine = $true }
        }
    } catch { $mine = $false }
    if (-not $mine) {
        Write-Line ("LOCK_NOT_MINE skipped-release path=" + $Path)
        return
    }
    Remove-Item -LiteralPath $Path -Force
    $script:LockHeld = $false
}

# ---------------------------------------------------------------- process launch
# Windows argv quoting. Start-Process does NOT quote -ArgumentList elements:
# measured on this host (r3 diag-ps-semantics.ps1 Q4) a value containing spaces
# reached the child as FOUR separate arguments (ARGC=7 instead of 4). Go build
# -o <path with spaces> therefore silently produced the wrong output path.
function Format-WinArg {
    param([string]$Value)
    if ($null -eq $Value -or $Value -eq '') { return '""' }
    if ($Value -match '[\s"]') {
        # Double the backslashes that precede a quote, then escape the quote;
        # double any trailing backslashes so they do not escape our closing quote.
        $esc = $Value -replace '(\\*)"', '$1$1\"'
        $esc = $esc -replace '(\\+)$', '$1$1'
        return '"' + $esc + '"'
    }
    return $Value
}

function ConvertTo-WindowsArgv {
    param([string[]]$Argv)
    $parts = New-Object System.Collections.Generic.List[string]
    foreach ($a in $Argv) { $parts.Add((Format-WinArg -Value $a)) }
    return ($parts -join ' ')
}

<#
    Invoke-BoundedProcess - the ONE launch path used by both the Go build and
    every sample.

    R2 proved that reading $proc.Handle before waiting is what makes ExitCode
    observable; R3 adds what R2 left open: the bounded wait's return value is
    now USED, a child that outlives the window is terminated (after re-confirming
    it is still the process we started), the Process object is disposed, and a
    failed cleanup is reported as a hard failure instead of being ignored.

    Returns: exited, terminated, rc, rcKnown, pid, handleCached, killConfirmName,
             cleanupError, cleanupFailed
#>
function Invoke-BoundedProcess {
    param(
        [string]$FilePath, [string[]]$Argv, [string]$WorkingDirectory,
        [string]$StdoutPath, [string]$StderrPath,
        [int]$WaitMs, [int]$KillGraceMs, [string]$ExpectedName
    )
    $res = [ordered]@{
        exited = $false; terminated = $false; rc = $null; rcKnown = $false
        pid = 0; handleCached = $false; killConfirmName = $null
        cleanupError = $null; cleanupFailed = $false; argv = @($Argv)
        argvQuoted = (ConvertTo-WindowsArgv -Argv $Argv)
    }
    $proc = $null
    try {
        $proc = Start-Process -FilePath $FilePath -ArgumentList (ConvertTo-WindowsArgv -Argv $Argv) `
            -WorkingDirectory $WorkingDirectory -RedirectStandardOutput $StdoutPath `
            -RedirectStandardError $StderrPath -NoNewWindow -PassThru
        try { $null = $proc.Handle; $res.handleCached = $true } catch { $res.handleCached = $false }
        $res.pid = $proc.Id

        # The bounded wait's result is what keeps the kill path reachable.
        $res.exited = $proc.WaitForExit($WaitMs)
        if (-not $res.exited) {
            $confirm = Get-Process -Id $res.pid -ErrorAction SilentlyContinue
            if ($null -ne $confirm) { $res.killConfirmName = $confirm.ProcessName }
            if ($null -ne $confirm -and ($ExpectedName -eq '' -or $res.killConfirmName -eq $ExpectedName)) {
                try { $proc.Kill(); $res.terminated = $true }
                catch { $res.cleanupError = ('kill threw: ' + $_.Exception.Message); $res.cleanupFailed = $true }
            } else {
                # The PID is gone (already exited) or now names a different
                # process: never kill something we did not start.
                $res.terminated = $false
                if ($null -eq $confirm) { $res.cleanupError = 'child vanished before kill' }
                else { $res.cleanupError = ('kill refused: name=' + $res.killConfirmName + ' expected=' + $ExpectedName); $res.cleanupFailed = $true }
            }
            $goneAfterGrace = $proc.WaitForExit($KillGraceMs)
            if (-not $goneAfterGrace) {
                # Still running after the grace period. APPEND to any earlier
                # reason: a refused kill followed by "still alive" is two facts,
                # and overwriting the first one hid why the kill was refused.
                $still = Get-Process -Id $res.pid -ErrorAction SilentlyContinue
                if ($null -ne $still) {
                    $extra = ('still alive after grace: pid=' + $res.pid + ' name=' + $still.ProcessName)
                    if ([string]::IsNullOrWhiteSpace($res.cleanupError)) { $res.cleanupError = $extra }
                    else { $res.cleanupError = ($res.cleanupError + '; ' + $extra) }
                    $res.cleanupFailed = $true
                }
            }
        }
        try { $proc.Refresh(); $res.rc = $proc.ExitCode; $res.rcKnown = ($null -ne $res.rc) }
        catch { $res.rc = $null; $res.rcKnown = $false }
    } finally {
        if ($null -ne $proc) { try { $proc.Dispose() } catch { } }
    }
    return $res
}

# ---------------------------------------------------------------- parsing
function Parse-ProbeStdout {
    param([string]$StdoutText)
    $fields = @{}
    $verdict = $null
    foreach ($line in ($StdoutText -split "`r?`n")) {
        # The probe's timeout path prints a combined line
        #   RESULT_DATA framesObserved=%d m1Measured=%v
        # so each RESULT_DATA line is tokenised instead of treated as one key.
        if ($line -match '^RESULT_DATA\s+(.*)$') {
            foreach ($tok in ($Matches[1] -split '\s+')) {
                if ($tok -match '^([A-Za-z0-9_]+)=(.*)$') { $fields[$Matches[1]] = $Matches[2] }
            }
        } elseif ($line -match '^ENV\s+.*\bgomaxprocs=([0-9]+)\b') {
            $fields['runtimeProcsObserved'] = $Matches[1]
        } elseif ($line -match '^RESULT\s+verdict=([A-Za-z_]+)') {
            if ($null -eq $verdict) { $verdict = $Matches[1] }
        }
    }
    return @{ fields = $fields; verdict = $verdict }
}

function Get-TailLines {
    param([string]$Text, [int]$Count)
    if ([string]::IsNullOrWhiteSpace($Text)) { return @() }
    $lines = @($Text -split "`r?`n" | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($lines.Count -le $Count) { return $lines }
    return @($lines[($lines.Count - $Count)..($lines.Count - 1)])
}

function New-SampleRecord {
    param(
        [string]$Id, [object]$Rc, [bool]$RcKnown, [bool]$TerminatedByCollector,
        [bool]$TimedOut, [string]$StdoutText, [string]$StderrText,
        [string]$OutFile, [string]$ErrFile, [string]$ArgsText, [string]$LoadMode,
        [int]$RequiredFrames, [string]$ExeHash, [string]$SrcHash, [string]$BaseSha,
        [string]$AgentName, [datetime]$Start, [datetime]$End, [int]$PidValue,
        [bool]$HandleCached, [string]$KillConfirmName
    )
    $parsed = Parse-ProbeStdout -StdoutText $StdoutText
    $fields = $parsed.fields
    $verdict = $parsed.verdict

    $m1Measured = $false
    if ($fields.ContainsKey('m1Measured')) { $m1Measured = ($fields['m1Measured'] -eq 'true') }
    $layoutMeasured = $false
    if ($fields.ContainsKey('layoutFirstAtMeasured')) { $layoutMeasured = ($fields['layoutFirstAtMeasured'] -eq 'true') }
    $framesObserved = 0
    if ($fields.ContainsKey('framesObserved')) { [int]::TryParse($fields['framesObserved'], [ref]$framesObserved) | Out-Null }

    # Field validity is judged on PRESENCE and PARSEABILITY, not on a bare null.
    $m1Present = (Test-LongFieldPresent -Fields $fields -Key 'm1ElapsedNs')
    $m1Ns = Get-LongFieldOrNull -Fields $fields -Key 'm1ElapsedNs'
    $m1Parsed = ($null -ne $m1Ns)
    $layoutPresent = (Test-LongFieldPresent -Fields $fields -Key 'layoutFirstEntryNs')
    $layoutRaw = Get-LongFieldOrNull -Fields $fields -Key 'layoutFirstEntryNs'
    # A zero-value time.Time serialises to a huge negative; non-positive means
    # "not observed", which is a MISSING value, not a valid timestamp.
    $layoutNs = $null
    if ($null -ne $layoutRaw -and $layoutRaw -gt 0) { $layoutNs = $layoutRaw }

    $reasons = New-Object System.Collections.Generic.List[string]
    $runtimeObserved = Get-LongFieldOrNull -Fields $fields -Key 'runtimeProcsObserved'
    if ($null -eq $runtimeObserved -or $runtimeObserved -lt 1) { $reasons.Add('runtime-procs-not-observed') }
    elseif ($RuntimeProcs -gt 0 -and $runtimeObserved -ne $RuntimeProcs) { $reasons.Add('runtime-procs-request-mismatch') }
    if ($TerminatedByCollector) { $reasons.Add('terminated-by-collector-after-bounded-wait') }
    if ($TimedOut -and -not $TerminatedByCollector) { $reasons.Add('wait-window-exceeded') }
    if (-not $RcKnown) { $reasons.Add('exitCode-unknown') }
    else { if ($Rc -ne 0) { $reasons.Add("exitCode=$Rc") } }
    if ([string]::IsNullOrWhiteSpace($StdoutText)) { $reasons.Add('empty-stdout') }
    if ($verdict -ne 'OK') { $reasons.Add("verdict=$verdict") }
    if (-not $m1Measured) { $reasons.Add('m1Measured=false') }
    if (-not $layoutMeasured) { $reasons.Add('layoutFirstAtMeasured=false') }

    # Numeric fields: a missing or unparseable value can never be valid, even
    # when the measured flags claim success (a forged/partial stdout).
    if (-not $m1Present) { $reasons.Add('m1ElapsedNs-missing') }
    elseif (-not $m1Parsed) { $reasons.Add('m1ElapsedNs-unparseable') }
    elseif ($m1Ns -lt 0) { $reasons.Add('m1ElapsedNs-negative') }
    if ($m1Measured -and -not $m1Parsed) { $reasons.Add('m1Measured-but-elapsed-unusable') }
    if (-not $layoutPresent) { $reasons.Add('layoutFirstEntryNs-missing') }
    elseif ($null -eq $layoutNs) { $reasons.Add('layoutFirstEntryNs-unparseable-or-nonpositive') }
    if ($layoutMeasured -and ($null -eq $layoutNs)) { $reasons.Add('layoutMeasured-but-timestamp-unusable') }
    if ($framesObserved -lt $RequiredFrames) { $reasons.Add("framesObserved=$framesObserved<required=$RequiredFrames") }

    # A zero m1ElapsedNs is NOT invalid: time.Now() advances in ~0.52 ms steps on
    # this host, so sub-millisecond intervals read as 0. Flagged, not dropped.
    $belowGranularity = ($m1Parsed -and $m1Ns -le 0)

    $comparisonObserved = [pscustomobject]@{
        fontComparisonContract = (Get-StringFieldOrNull -Fields $fields -Key 'fontComparisonContract')
        fontComparisonMeasured = (Get-StringFieldOrNull -Fields $fields -Key 'fontComparisonMeasured')
        fontComparisonColdProvider = (Get-StringFieldOrNull -Fields $fields -Key 'fontComparisonColdProvider')
        fontInitAtNs = (Get-LongFieldOrNull -Fields $fields -Key 'fontInitAtNs')
        fontReadyAtNs = (Get-LongFieldOrNull -Fields $fields -Key 'fontReadyAtNs')
        firstFrameAtNs = (Get-LongFieldOrNull -Fields $fields -Key 'firstFrameAtNs')
        fontReadyElapsedNs = (Get-LongFieldOrNull -Fields $fields -Key 'fontReadyElapsedNs')
        fontToFirstFrameElapsedNs = (Get-LongFieldOrNull -Fields $fields -Key 'fontToFirstFrameElapsedNs')
    }
    if ($CompareFonts) {
        foreach ($reason in @(Get-FontComparisonErrors -Observed $comparisonObserved -LoadMode $LoadMode)) {
            $reasons.Add($reason)
        }
    } elseif ($null -ne $comparisonObserved.fontComparisonContract) {
        $reasons.Add('comparison-output-requires-CompareFonts')
    }

    return [ordered]@{
        id            = $Id
        measurementContract = $(if ($CompareFonts) { 'font-init-v1' } else { 'app-m1-v1' })
        runtimeProcsPolicy = $runtimeProcsPolicy
        runtimeProcsObserved = $runtimeObserved
        agent         = $AgentName
        baseSHA       = $BaseSha
        load          = $LoadMode
        requestedFrames = $RequiredFrames
        args          = $ArgsText
        exeSha256     = $ExeHash
        sourceSha256  = $SrcHash
        pid           = $PidValue
        handleCached  = $HandleCached
        killConfirmName = $KillConfirmName
        start         = $Start.ToString('o')
        end           = $End.ToString('o')
        durationMs    = [int](($End - $Start).TotalMilliseconds)
        exitCode      = $Rc
        exitCodeKnown = $RcKnown
        terminatedByCollector = $TerminatedByCollector
        probeTimeout  = ($verdict -eq 'ENV_TIMEOUT' -or $verdict -eq 'HARD_TIMEOUT')
        timedOut      = $TimedOut
        verdict       = $verdict
        stdoutEmpty   = [string]::IsNullOrWhiteSpace($StdoutText)
        stdoutPath    = $OutFile
        stderrPath    = $ErrFile
        stdoutTail    = @(Get-TailLines -Text $StdoutText -Count 8)
        stderrTail    = @(Get-TailLines -Text $StderrText -Count 20)
        observed      = [ordered]@{
            fontComparisonContract = $comparisonObserved.fontComparisonContract
            fontComparisonMeasured = $comparisonObserved.fontComparisonMeasured
            fontComparisonColdProvider = $comparisonObserved.fontComparisonColdProvider
            fontInitAtNs = $comparisonObserved.fontInitAtNs
            fontReadyAtNs = $comparisonObserved.fontReadyAtNs
            fontReadyElapsedNs = $comparisonObserved.fontReadyElapsedNs
            fontToFirstFrameElapsedNs = $comparisonObserved.fontToFirstFrameElapsedNs
            m1ElapsedNs          = $m1Ns
            m1ElapsedNsPresent   = $m1Present
            m1Measured           = $m1Measured
            layoutFirstEntryNs   = $layoutNs
            layoutFirstEntryNsPresent = $layoutPresent
            layoutFirstAtMeasured = $layoutMeasured
            framesObserved       = $framesObserved
            frameWindowNs        = (Get-LongFieldOrNull -Fields $fields -Key 'frameWindowNs')
            procStartNs          = (Get-LongFieldOrNull -Fields $fields -Key 'procStartNs')
            ctorEntryNs          = (Get-LongFieldOrNull -Fields $fields -Key 'ctorEntryNs')
            ctorDoneNs           = (Get-LongFieldOrNull -Fields $fields -Key 'ctorDoneNs')
            createdAtNs          = (Get-LongFieldOrNull -Fields $fields -Key 'createdAtNs')
            firstFrameAtNs       = (Get-LongFieldOrNull -Fields $fields -Key 'firstFrameAtNs')
            lazyState            = (Get-StringFieldOrNull -Fields $fields -Key 'lazyState')
            lazyTempElapsedNs    = (Get-LongFieldOrNull -Fields $fields -Key 'lazyTempElapsedNs')
            lazyProbeElapsedNs   = (Get-LongFieldOrNull -Fields $fields -Key 'lazyProbeElapsedNs')
            lazyLoadElapsedNs    = (Get-LongFieldOrNull -Fields $fields -Key 'lazyLoadElapsedNs')
            lazyProbeRan         = (Get-StringFieldOrNull -Fields $fields -Key 'lazyProbeRan')
        }
        valid            = ($reasons.Count -eq 0)
        belowGranularity = $belowGranularity
        invalidReasons   = $reasons.ToArray()
    }
}

# ---------------------------------------------------------------- fixture mode
if ($ParseFixture) {
    if (-not (Test-Path -LiteralPath $ParseFixture)) { Stop-Precheck ("fixture not found: " + $ParseFixture) }
    if (-not [System.IO.Path]::IsPathRooted($FixtureOut)) { Stop-Precheck ("FixtureOut must be absolute, got: " + $FixtureOut) }
    if ((Test-Path -LiteralPath $FixtureOut) -and -not $FixtureOverwrite) { Stop-Precheck ("fixture output exists: " + $FixtureOut) }
    # -Encoding UTF8 is mandatory: GBK decoding of Chinese probe output swallows
    # line breaks and merges the following RESULT_DATA line.
    $fxText = Get-Content -LiteralPath $ParseFixture -Raw -Encoding UTF8
    $fxStart = Get-Date
    $fxRc = $null
    $fxKnown = -not $FixtureExitCodeUnknown
    if ($fxKnown) { $fxRc = $FixtureExitCode }
    $rec = New-SampleRecord -Id 'fixture' -Rc $fxRc -RcKnown $fxKnown `
        -TerminatedByCollector ([bool]$FixtureTerminated) -TimedOut ([bool]$FixtureTimedOut) `
        -StdoutText $fxText -StderrText '' -OutFile $ParseFixture -ErrFile '' `
        -ArgsText 'FIXTURE (no process launched)' -LoadMode $Load -RequiredFrames $Frames `
        -ExeHash $null -SrcHash $null -BaseSha 'fixture' -AgentName $Agent `
        -Start $fxStart -End (Get-Date) -PidValue 0 -HandleCached $false -KillConfirmName $null
    Write-Line ("FIXTURE_ID " + (Split-Path -Leaf $ParseFixture))
    Write-Line ("FIXTURE_VALID " + $rec.valid)
    Write-Line ("FIXTURE_BELOW_GRANULARITY " + $rec.belowGranularity)
    Write-Line ("FIXTURE_REASONS " + ($rec.invalidReasons -join '; '))
    ($rec | ConvertTo-Json -Depth 8) | Set-Content -LiteralPath $FixtureOut -Encoding UTF8
    Write-Line ("FIXTURE_WROTE " + $FixtureOut)
    exit 0
}

# ---------------------------------------------------------------- pre-flight
Repair-EnvDuplicates

if ([string]::IsNullOrWhiteSpace($RepoDir)) { Stop-Precheck 'RepoDir is required in normal mode' }
if ([string]::IsNullOrWhiteSpace($ExpectedHead)) { Stop-Precheck 'ExpectedHead is required in normal mode' }
if ([string]::IsNullOrWhiteSpace($OutDir)) { Stop-Precheck 'OutDir is required in normal mode' }
if (-not (Test-Path -LiteralPath $RepoDir)) { Stop-Precheck "RepoDir not found: $RepoDir" }
$probeSrc = Join-Path $RepoDir 'pkg/runtime/app/testdata/m1nightprobe/main.go'
if (-not (Test-Path -LiteralPath $probeSrc)) { Stop-Precheck "probe source not found: $probeSrc" }
$appSrc = Join-Path $RepoDir 'pkg/runtime/app/app.go'
if (-not (Test-Path -LiteralPath $appSrc)) { Stop-Precheck "app.go not found: $appSrc" }
if (-not (Test-Path -LiteralPath (Join-Path $RepoDir 'go.mod'))) { Stop-Precheck "go.mod not found under RepoDir" }

try {
    $head = (git -C $RepoDir rev-parse HEAD).Trim()
    $porcelain = @(git -C $RepoDir status --porcelain)
} catch {
    Stop-Precheck ("git query failed under RepoDir: " + $_.Exception.Message)
}
if ($LASTEXITCODE -ne 0) { Stop-Precheck "git rev-parse failed" }
if ($head -ne $ExpectedHead) { Stop-Precheck ("baseline mismatch: HEAD=" + $head + " expected=" + $ExpectedHead) }

# Build-input purity: no tracked file may differ from HEAD, and the ONLY
# untracked paths tolerated are this script and the summary script. Any
# untracked .go file, go.mod or go.sum would silently change the probe binary.
$trackedDirty = @($porcelain | Where-Object { $_ -notmatch '^\?\?' })
$untracked = @($porcelain | Where-Object { $_ -match '^\?\?' } | ForEach-Object {
    ($_ -replace '^\?\?\s+', '').Trim().Trim('"')
})
if ($trackedDirty.Count -gt 0) {
    Stop-Precheck ("tracked changes vs baseline (" + $trackedDirty.Count + " paths: " + ($trackedDirty -join '; ') + ")")
}
$allowedUntracked = @('scripts/font/collect-m1.ps1', 'scripts/font/summarize-m1.ps1')
$foreignUntracked = @($untracked | Where-Object {
    $p = $_ -replace '\\', '/'
    -not ($allowedUntracked -contains $p)
})
if ($foreignUntracked.Count -gt 0) {
    Stop-Precheck ("untracked files would pollute the build (" + $foreignUntracked.Count + "): " + ($foreignUntracked -join '; '))
}
$buildInputUntracked = @($untracked | Where-Object { $_ -match '(\.go|go\.mod|go\.sum)$' })
if ($buildInputUntracked.Count -gt 0) {
    Stop-Precheck ("untracked Go/module files present: " + ($buildInputUntracked -join '; '))
}

# Output uniqueness: every artifact path is checked BEFORE the first write.
$exePath = Join-Path $OutDir 'm1nightprobe.exe'
$buildOutLog = Join-Path $OutDir 'build.stdout.log'
$buildErrLog = Join-Path $OutDir 'build.stderr.log'
$runJsonPath = Join-Path $OutDir 'run.json'
$samplesJsonlPath = Join-Path $OutDir 'samples.jsonl'
$sampleDir = Join-Path $OutDir 'samples'
$reserved = @($exePath, $buildOutLog, $buildErrLog, $runJsonPath, $samplesJsonlPath)
foreach ($p in $reserved) {
    if (Test-Path -LiteralPath $p) { Stop-Precheck ("output overwrite refused, path exists: " + $p) }
}
if (Test-Path -LiteralPath $sampleDir) {
    $leftover = @(Get-ChildItem -LiteralPath $sampleDir -Filter 'sample-*' -File -ErrorAction SilentlyContinue)
    if ($leftover.Count -gt 0) { Stop-Precheck ("output overwrite refused: " + $leftover.Count + " sample-* files exist in " + $sampleDir) }
}
for ($i = 1; $i -le $Runs; $i++) {
    $stamp = $i.ToString('0000')
    foreach ($p in @((Join-Path $sampleDir ("sample-" + $stamp + ".json")),
                     (Join-Path $sampleDir ("sample-" + $stamp + ".stdout.log")),
                     (Join-Path $sampleDir ("sample-" + $stamp + ".stderr.log")))) {
        if (Test-Path -LiteralPath $p) { Stop-Precheck ("output overwrite refused, path exists: " + $p) }
    }
}

$useExternal = -not [string]::IsNullOrWhiteSpace($Executable)
$ExecutableArgs = @()
if ($useExternal) {
    if (-not (Test-Path -LiteralPath $Executable)) { Stop-Precheck ("Executable not found: " + $Executable) }
    if ([string]::IsNullOrWhiteSpace($ExecutableArgLine)) { Stop-Precheck 'ExecutableArgLine must be supplied explicitly with -Executable' }
    $ExecutableArgs = @($ExecutableArgLine -split '\|' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($ExecutableArgs.Count -eq 0) { Stop-Precheck 'ExecutableArgLine produced no arguments' }
}

# ---------------------------------------------------------------- env snapshot
$os = Get-CimInstance Win32_OperatingSystem
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$gpu = Get-CimInstance Win32_VideoController | Select-Object -First 1
$lockScreen = @(Get-Process -Name LogonUI -ErrorAction SilentlyContinue)
$envInfo = [ordered]@{
    collectedAt    = (Get-Date).ToString('o')
    osCaption      = $os.Caption
    cpuName        = $cpu.Name
    cpuLogical     = [int]$env:NUMBER_OF_PROCESSORS
    gpuName        = $(if ($gpu) { $gpu.Name } else { 'unknown' })
    logonUiPresent = ($lockScreen.Count -gt 0)
    psVersion      = $PSVersionTable.PSVersion.ToString()
    note           = 'No user identity, no personal data collected on purpose.'
}

# ---------------------------------------------------------------- lock (build + sampling)
if (-not $SkipLock) {
    $deadline = (Get-Date).AddMinutes($LockWaitMinutes)
    while (-not (Try-AcquireLock -Path $LockFile -AgentName $Agent)) {
        if ((Get-Date) -ge $deadline) {
            Write-Line ("LOCK_TIMEOUT waited " + $LockWaitMinutes + " minutes; nothing started")
            exit 4
        }
        Write-Line ("LOCK_BUSY waiting path=" + $LockFile)
        Start-Sleep -Seconds 15
    }
    $script:LockHeld = $true
    Write-Line ("LOCK_ACQUIRED pid=" + $PID)
}

# R3: EVERYTHING from here to the end of the script runs while the heavy lock is
# held, so it is wrapped in ONE try/finally. Previously the release was repeated
# by hand at each early exit and was therefore missing on several paths:
#   * an exception while hashing sources or creating the output directory
#   * a Write-RunState failure inside the sampling finally (the release was the
#     statement AFTER it, so a throw skipped it)
#   * the post-build check and build-exception branches
# `exit` inside a try{} DOES run the finally{} - measured on this host with a
# real child process (r3 diag-ps-semantics.ps1 Q1: exitCode 7, "CHILD_FINALLY"
# printed). The lock is thus released on every path, and only by the process
# that acquired it.
try {

# The holder must be this live collector process, verified right after acquiring.
$lockVerification = [ordered]@{ verified = $false; lockPid = $null; collectorPid = $PID; reason = $null }
if ($script:LockHeld) {
    try {
        $raw = Get-Content -LiteralPath $LockFile -Raw -Encoding UTF8
        $doc = $raw | ConvertFrom-Json
        $holder = Get-Process -Id ([int]$doc.pid) -ErrorAction Stop
        $lockVerification.lockPid = [int]$doc.pid
        $lockVerification.verified = (($doc.agent -eq $Agent) -and ([int]$doc.pid -eq $PID) -and ($null -ne $holder))
        if (-not $lockVerification.verified) { $lockVerification.reason = 'lock payload does not match this live process' }
    } catch {
        $lockVerification.reason = ('verification failed: ' + $_.Exception.Message)
    }
}
Write-Line ("LOCK_VERIFIED " + $lockVerification.verified)

# ---------------------------------------------------------------- source hashes
$sourceFiles = [ordered]@{}
foreach ($rel in @('pkg/runtime/app/app.go',
                   'pkg/runtime/app/testdata/m1nightprobe/main.go',
                   'pkg/runtime/app/testdata/m1nightprobe/comparison.go',
                   'pkg/runtime/theme/theme.go',
                   'pkg/runtime/theme/font.go',
                   'pkg/runtime/theme/lazy.go',
                   'scripts/font/font-comparison.ps1',
                   'scripts/font/collect-m1.ps1',
                   'scripts/font/summarize-m1.ps1')) {
    $sourceFiles[$rel] = (Get-FileSha256 -Path (Join-Path $RepoDir $rel))
}

$script:RunPath = $runJsonPath
$samples = New-Object System.Collections.Generic.List[object]
$script:RunState = [ordered]@{
    task      = 'R2-WB-SAMPLER-HARDEN'
    script    = 'scripts/font/collect-m1.ps1'
    baseSHA   = $head
    repoDir   = $RepoDir
    outDir    = $OutDir
    load      = $Load
    measurementContract = $(if ($CompareFonts) { 'font-init-v1' } else { 'app-m1-v1' })
    runtimeProcsPolicy = $runtimeProcsPolicy
    runtimeProcsInheritedEnv = [Environment]::GetEnvironmentVariable('GOMAXPROCS','Process')
    runs      = $Runs
    frames    = $Frames
    timeoutSec = $TimeoutSec
    hardTimeoutSec = $HardTimeoutSec
    killGraceSec = $KillGraceSec
    collectorPid = $PID
    agent     = $Agent
    verificationRun = $useExternal
    executableLabel = $(if ($useExternal) { $ExecutableLabel } else { 'm1nightprobe (project probe)' })
    env       = $envInfo
    git       = [ordered]@{ trackedChanges = $trackedDirty; untrackedAtStart = $untracked }
    sourceFiles = $sourceFiles
    lock      = $lockVerification
    status    = 'running'
    startedAt = (Get-Date).ToString('o')
    build     = $null
    totals    = [ordered]@{ attempted = 0; valid = 0; invalid = 0; belowGranularity = 0; terminated = 0; probeTimeout = 0; emptyStdout = 0; exitCodeUnknown = 0 }
    contract  = @(
        'M1 = App construction start -> first frame processing start; NOT process startup',
        'one sample == one independent process',
        'null exit code is recorded as null and makes the sample invalid; never substituted',
        'sync/lazy construction interval differences are never reported as net gain',
        'no threshold is set or frozen by this script'
    )
}

function Write-RunState {
    param([string]$Status)
    if ($null -eq $script:RunPath) { return }
    $script:RunState.status = $Status
    $script:RunState.updatedAt = (Get-Date).ToString('o')
    $tmp = $script:RunPath + '.tmp'
    ($script:RunState | ConvertTo-Json -Depth 10) | Set-Content -LiteralPath $tmp -Encoding UTF8
    if (Test-Path -LiteralPath $script:RunPath) { Remove-Item -LiteralPath $script:RunPath -Force }
    Move-Item -LiteralPath $tmp -Destination $script:RunPath -Force
}

if (-not (Test-Path -LiteralPath $OutDir)) { New-Item -ItemType Directory -Path $OutDir -Force | Out-Null }
if (-not (Test-Path -LiteralPath $sampleDir)) { New-Item -ItemType Directory -Path $sampleDir -Force | Out-Null }

# ---------------------------------------------------------------- build probe
if ($useExternal) {
    # Verification run: no Go build, the supplied helper stands in for the probe.
    $script:RunState.build = [ordered]@{
        skipped  = $true
        reason   = 'external verification executable supplied via -Executable'
        exePath  = $Executable
        exeArgs  = ($ExecutableArgs -join ' ')
        label    = $ExecutableLabel
    }
    Write-RunState -Status 'building'
    Write-Line ("RUN_JSON_WROTE " + $script:RunPath)
    $exeHash = Get-FileSha256 -Path $Executable
    $srcHash = $null
    $script:RunState.sourceSha256 = $null
    $script:RunState.build.exeSha256 = $exeHash
    Write-Line ("BUILD_SKIPPED exeSha256=" + $exeHash)
} else {
try {
    # run.json is persisted BEFORE the build so an interrupted run still shows
    # what was attempted, with the real build stdout/stderr/rc once it returns.
    Write-RunState -Status 'building'
    Write-Line ("RUN_JSON_WROTE " + $script:RunPath)

    # Raw argv is kept as an ARRAY and logged element by element, and separately
    # as the quoted command line that is actually handed to Start-Process.
    # R3 defect: Start-Process does not quote -ArgumentList elements, so an
    # OutDir containing spaces produced a different -o target than intended
    # (measured: ARGC=7 instead of 4). The R2 "path with spaces" case only ever
    # exercised -Executable, which skipped the Go build entirely.
    $buildArgv = @('build', '-p=1', '-o', $exePath, './pkg/runtime/app/testdata/m1nightprobe')
    Write-Line ("BUILD go " + (ConvertTo-WindowsArgv -Argv $buildArgv))
    $buildStart = Get-Date
    $buildProcsBefore = [Environment]::GetEnvironmentVariable('GOMAXPROCS','Process')
    try {
        $env:GOMAXPROCS = '2'
        $bp = Invoke-BoundedProcess -FilePath 'go' -Argv $buildArgv -WorkingDirectory $RepoDir `
            -StdoutPath $buildOutLog -StderrPath $buildErrLog `
            -WaitMs ([int]$BuildTimeoutSec * 1000) -KillGraceMs ([int]$KillGraceSec * 1000) `
            -ExpectedName 'go'
    } finally { [Environment]::SetEnvironmentVariable('GOMAXPROCS',$buildProcsBefore,'Process') }
    $buildRc = $bp.rc
    if (-not $bp.rcKnown) { $buildRc = $null }
    $buildEnd = Get-Date
    if (-not $bp.exited) { Write-Line ("BUILD_WAIT_EXCEEDED pid=" + $bp.pid + " terminated=" + $bp.terminated) }
    if ($bp.cleanupFailed) { Write-Line ("BUILD_CLEANUP_FAILED " + $bp.cleanupError) }
    # [string] cast is required: Get-Content attaches FileSystem provider ETS
    # properties (PSPath, PSDrive, PSProvider, ReadCount ...) to the returned
    # string and ConvertTo-Json serialises ALL of them, so an empty build output
    # was written as {} instead of "". Verified in diag-emptyfield.ps1.
    $buildOut = ''
    if (Test-Path -LiteralPath $buildOutLog) {
        $rawOut = Get-Content -LiteralPath $buildOutLog -Raw -Encoding UTF8
        if ($null -ne $rawOut) { $buildOut = [string]$rawOut }
    }
    $buildErr = ''
    if (Test-Path -LiteralPath $buildErrLog) {
        $rawErr = Get-Content -LiteralPath $buildErrLog -Raw -Encoding UTF8
        if ($null -ne $rawErr) { $buildErr = [string]$rawErr }
    }
    $goVersion = 'unknown'
    try { $goVersion = (& go version) } catch { }
    $script:RunState.build = [ordered]@{
        command      = 'go build -p=1 -o <out> ./pkg/runtime/app/testdata/m1nightprobe'
        argv         = $buildArgv
        argvQuoted   = (ConvertTo-WindowsArgv -Argv $buildArgv)
        gomaxprocs   = 2
        goVersion    = "$goVersion"
        buildTimeoutSec = $BuildTimeoutSec
        startedAt    = $buildStart.ToString('o')
        endedAt      = $buildEnd.ToString('o')
        durationMs   = [int](($buildEnd - $buildStart).TotalMilliseconds)
        pid          = $bp.pid
        exited       = $bp.exited
        terminatedByCollector = $bp.terminated
        exitCode     = $buildRc
        exitCodeKnown = $bp.rcKnown
        waitExceeded = (-not $bp.exited)
        cleanupFailed = $bp.cleanupFailed
        cleanupError = $bp.cleanupError
        stdoutPath   = $buildOutLog
        stderrPath   = $buildErrLog
        stdout       = $buildOut
        stderr       = $buildErr
        exeSha256    = $null
    }
    Write-RunState -Status 'building'
    # A build child that outlived its window or could not be cleaned up is a
    # hard failure: nothing may proceed while a compiler is still running.
    if ($bp.cleanupFailed) {
        Write-Line ("BUILD_FAILED cleanup-failed: " + $bp.cleanupError)
        Write-RunState -Status 'build-cleanup-failed'
        exit 3
    }
    if ($null -eq $buildRc -or $buildRc -ne 0 -or -not (Test-Path -LiteralPath $exePath)) {
        Write-Line ("BUILD_FAILED rc=" + $buildRc)
        Write-RunState -Status 'build-failed'
        exit 3
    }
    $srcHash = Get-FileSha256 -Path $probeSrc
    $exeHash = Get-FileSha256 -Path $exePath
    $script:RunState.build.exeSha256 = $exeHash
    $script:RunState.sourceSha256 = $srcHash
    Write-Line ("BUILD_OK exeSha256=" + $exeHash)

    # Re-verify the baseline and every source hash after the build: a build must
    # never move the tree it was measured from.
    $postHead = (git -C $RepoDir rev-parse HEAD).Trim()
    $postDirty = @((git -C $RepoDir status --porcelain) | Where-Object { $_ -notmatch '^\?\?' })
    $postSources = [ordered]@{}
    foreach ($rel in $sourceFiles.Keys) { $postSources[$rel] = (Get-FileSha256 -Path (Join-Path $RepoDir $rel)) }
    $changed = @()
    if ($postHead -ne $head) { $changed += ('HEAD ' + $head + ' -> ' + $postHead) }
    if ($postDirty.Count -gt 0) { $changed += ('trackedDirty ' + $postDirty.Count) }
    foreach ($rel in $sourceFiles.Keys) {
        if ($postSources[$rel] -ne $sourceFiles[$rel]) { $changed += ('sourceChanged ' + $rel) }
    }
    $script:RunState.postBuildCheck = [ordered]@{ changed = $changed; head = $postHead; sources = $postSources }
    Write-RunState -Status $(if ($changed.Count -gt 0) { 'post-build-check-failed' } else { 'building' })
    if ($changed.Count -gt 0) {
        Write-Line ("POST_BUILD_CHECK_FAILED " + ($changed -join '; '))
        exit 6
    }
} catch {
    Write-Line ("BUILD_EXCEPTION " + $_.Exception.Message)
    try { Write-RunState -Status 'build-exception' } catch { }
    exit 3
}
}

# ---------------------------------------------------------------- sampling
if ($useExternal) {
    $targetExe = $Executable
    $targetExeName = [System.IO.Path]::GetFileNameWithoutExtension($Executable)
} else {
    $targetExe = $exePath
    $targetExeName = [System.IO.Path]::GetFileNameWithoutExtension($exePath)
}
$exeName = $targetExeName
$validCount = 0
$invalidCount = 0
$belowCount = 0
$terminatedCount = 0
$probeTimeoutCount = 0
$emptyCount = 0
$unknownCount = 0
# Set when a child outlived its bounded cleanup. The loop stops immediately so
# no further sample starts while a probe is still running.
$abortReason = $null
$persistFailure = $false

try {
    for ($i = 1; $i -le $Runs; $i++) {
        $stamp = $i.ToString('0000')
        $outFile = Join-Path $sampleDir ("sample-" + $stamp + ".stdout.log")
        $errFile = Join-Path $sampleDir ("sample-" + $stamp + ".stderr.log")
        # Flag and value are SEPARATE array elements: in PowerShell array
        # literals the comma binds tighter than '+', so '-timeout=' + 20 + 's'
        # leaks out of the array and reaches the child as three arguments.
        $argList = @(
            '-mode', 'window',
            '-load', $Load,
            '-frames', "$Frames",
            '-timeout', "$($TimeoutSec)s",
            '-hardtimeout', "$($HardTimeoutSec)s"
        )
        if ($CompareFonts) { $argList += '-compare-fonts' }
        if ($useExternal) { $argList = $ExecutableArgs }

        $start = Get-Date
        $script:RunState.totals.attempted = $i
        # Same bounded launch path as the Go build (R3): -PassThru, cache .Handle
        # so ExitCode is observable, bounded wait, targeted kill of the confirmed
        # child, grace wait, dispose. The result carries whether cleanup failed.
        $waitMs = [int](($HardTimeoutSec + 15) * 1000)
        $sampleProcsBefore = [Environment]::GetEnvironmentVariable('GOMAXPROCS','Process')
        try {
            if ($RuntimeProcs -eq 0) { Remove-Item Env:GOMAXPROCS -ErrorAction SilentlyContinue }
            elseif ($RuntimeProcs -gt 0) { $env:GOMAXPROCS = [string]$RuntimeProcs }
            $pr = Invoke-BoundedProcess -FilePath $targetExe -Argv $argList -WorkingDirectory $RepoDir `
                -StdoutPath $outFile -StderrPath $errFile -WaitMs $waitMs `
                -KillGraceMs ([int]$KillGraceSec * 1000) -ExpectedName $exeName
        } finally { [Environment]::SetEnvironmentVariable('GOMAXPROCS',$sampleProcsBefore,'Process') }
        $handleCached = $pr.handleCached
        $childPid = $pr.pid
        $exited = $pr.exited
        $terminated = $pr.terminated
        $killConfirmName = $pr.killConfirmName
        $rc = $pr.rc
        $rcKnown = $pr.rcKnown
        if ($killConfirmName -ne $null -and $killConfirmName -ne $exeName -and -not $exited) {
            Write-Line ("KILL_REFUSED pid=" + $childPid + " name=" + $killConfirmName + " expected=" + $exeName)
        }
        $end = Get-Date
        if ($pr.cleanupFailed) {
            # Never start the next sample while a child of ours is still alive.
            Write-Line ("SAMPLE_CLEANUP_FAILED id=" + $stamp + " pid=" + $childPid + " : " + $pr.cleanupError)
            $abortReason = ('sample ' + $stamp + ' cleanup failed: ' + $pr.cleanupError)
        }

        # -Encoding UTF8 is mandatory: GBK decoding swallows line breaks in the
        # probe's Chinese output and merges the next RESULT_DATA line.
        $stdoutText = ''
        if (Test-Path -LiteralPath $outFile) {
            $rawS = Get-Content -LiteralPath $outFile -Raw -Encoding UTF8
            if ($null -ne $rawS) { $stdoutText = [string]$rawS }
        }
        $stderrText = ''
        if (Test-Path -LiteralPath $errFile) {
            $rawE = Get-Content -LiteralPath $errFile -Raw -Encoding UTF8
            if ($null -ne $rawE) { $stderrText = [string]$rawE }
        }

        $rec = New-SampleRecord -Id $stamp -Rc $rc -RcKnown $rcKnown `
            -TerminatedByCollector $terminated -TimedOut (-not $exited) `
            -StdoutText $stdoutText -StderrText $stderrText `
            -OutFile $outFile -ErrFile $errFile -ArgsText ($argList -join ' ') `
            -LoadMode $Load -RequiredFrames $Frames -ExeHash $exeHash -SrcHash $srcHash `
            -BaseSha $head -AgentName $Agent -Start $start -End $end `
            -PidValue $childPid -HandleCached $handleCached -KillConfirmName $killConfirmName
        $rec.verificationRun = $useExternal
        $rec.cleanupFailed = $pr.cleanupFailed
        $rec.cleanupError = $pr.cleanupError
        if ($pr.cleanupFailed) {
            $rec.valid = $false
            $rec.invalidReasons = @($rec.invalidReasons) + @('cleanup-failed:' + $pr.cleanupError)
        }
        # Raw argv array AND the quoted command line actually handed to
        # Start-Process: a joined string alone cannot show how a spaced path was
        # passed, which is exactly what hid R3 defect 3.
        $rec.argv = @($argList)
        $rec.argvQuoted = $pr.argvQuoted

        if ($rec.valid) { $validCount++ } else { $invalidCount++ }
        if ($rec.belowGranularity) { $belowCount++ }
        if ($terminated) { $terminatedCount++ }
        if ($rec.probeTimeout) { $probeTimeoutCount++ }
        if ($rec.stdoutEmpty) { $emptyCount++ }
        if (-not $rcKnown) { $unknownCount++ }

        $samples.Add($rec)
        ($rec | ConvertTo-Json -Depth 8) | Set-Content -LiteralPath (Join-Path $sampleDir ("sample-" + $stamp + ".json")) -Encoding UTF8
        $script:RunState.totals.attempted = $i
        $script:RunState.totals.valid = $validCount
        $script:RunState.totals.invalid = $invalidCount
        $script:RunState.totals.belowGranularity = $belowCount
        $script:RunState.totals.terminated = $terminatedCount
        $script:RunState.totals.probeTimeout = $probeTimeoutCount
        $script:RunState.totals.emptyStdout = $emptyCount
        $script:RunState.totals.exitCodeUnknown = $unknownCount
        Write-RunState -Status 'sampling'
        Write-Line ("SAMPLE id=" + $stamp + " pid=" + $childPid + " exitCode=" + $(if ($rcKnown) { $rc } else { 'null' }) +
            " terminated=" + $terminated + " verdict=" + $rec.verdict + " m1Ns=" + $rec.observed.m1ElapsedNs +
            " frames=" + $rec.observed.framesObserved + " valid=" + $rec.valid)
        if ($null -ne $abortReason) { break }
    }
} finally {
    # NESTED try/finally: persistence must never be the reason the lock is not
    # released. In R2 the release was the statement AFTER Write-RunState, so a
    # throw while persisting skipped it and leaked the heavy lock.
    try {
        $finalStatus = 'completed'
        if ($null -ne $abortReason) { $finalStatus = 'aborted-cleanup-failure' }
        elseif ($script:RunState.totals.attempted -lt $Runs) { $finalStatus = 'interrupted' }
        if ($null -ne $abortReason) { $script:RunState.abortReason = $abortReason }
        Write-RunState -Status $finalStatus
        $samples | ForEach-Object { ($_ | ConvertTo-Json -Depth 8 -Compress) } |
            Set-Content -LiteralPath $samplesJsonlPath -Encoding UTF8
    } catch {
        $persistFailure = $true
        Write-Line ("PERSIST_FAILED " + $_.Exception.Message)
    } finally {
        if ($script:LockHeld) { Release-Lock -Path $LockFile; Write-Line 'LOCK_RELEASED' }
    }
}

Write-Line ("DONE attempted=" + $script:RunState.totals.attempted + " valid=" + $validCount + " invalid=" + $invalidCount +
    " belowGranularity=" + $belowCount + " terminated=" + $terminatedCount +
    " probeTimeout=" + $probeTimeoutCount + " emptyStdout=" + $emptyCount + " exitCodeUnknown=" + $unknownCount)
if ($null -ne $abortReason) {
    Write-Line ("ABORTED " + $abortReason)
    exit 7
}
if ($persistFailure) { exit 8 }
if ($Strict -and $invalidCount -gt 0) { exit 5 }
exit 0

} finally {
    # Outer guard for the whole lock-holding section: source hashing, output
    # directory creation, build (exit 3 / exit 6 / exception) and sampling all
    # release the lock here. Releasing is idempotent-safe: Release-Lock checks
    # that the payload still names this agent and PID.
    if ($script:LockHeld) {
        try { Release-Lock -Path $LockFile; Write-Line 'LOCK_RELEASED_OUTER' }
        catch { Write-Line ("LOCK_RELEASE_FAILED " + $_.Exception.Message) }
    }
}
