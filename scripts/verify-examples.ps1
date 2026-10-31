#Requires -Version 5.1
<#
.SYNOPSIS
    用真实 CLI 生成 + 真实 Go 构建来验证 examples/gxml 下的入门工程（starter / tasks）。

.DESCRIPTION
    本脚本面向"用户直接运行"：从任意当前目录都可调用，内部按 $PSScriptRoot 定位仓库根。
    它对每个示例做三件事，且**从不执行窗口**：

      1. 把 testdata/<示例> 的**同一份源材料**复制到脚本自己创建的隔离输出目录；
      2. 用 go 构建 ./cmd/goxml，再真实执行 `goxml generate --no-cache`；
      3. `go build` 窗口入口（cmd/<示例>/main.go）——只构建，不运行产物。

    两种模块形态：
      -Mode isolated（默认）：输出目录里写独立 go.mod（module example.com/goxml-<示例>）
        并 replace 到本地仓库，复用仓库的 go.sum；GOPROXY=off，全程不联网。
      -Mode checkout：不写 go.mod，直接在仓库内的子目录构建（模块 github.com/goxml/goxml），
        并把 main 模板里的 `example.com/goxml-<示例>` 替换成本地包路径
        （对应 R2-REVIEW-DOC-01：模板是独立 module，复制到 checkout 必须做导入替换）。

    关于子进程输出：本脚本**不重定向**子进程的 stdout/stderr，让 go / goxml 的输出
    直通控制台。原因见文件内注释（同步重定向在双流大输出下实测死锁；PS 5.1 的
    DataReceived 异步事件实测收不到数据）。这样用户看到的是工具的原始输出，
    本脚本不做任何二次编码转换。

    锁与可迁移性：锁助手 `verify-examples-lock.py` 与同目录，只依赖 Python 标准库，
    默认锁路径是**仓库内** `<仓库根>/tmp/verify-examples/heavy.lock`（由助手自身位置
    推导），因此换机器 checkout 后无需任何历史目录即可运行。要共用夜间公共锁时
    必须**显式**传 -LockPath；默认值不会替代、也不得绕过公共锁。
    已有 Python 依赖是明确前提：缺 Python、Python 是 Microsoft Store 应用执行别名、
    或锁助手缺失，都会以 rc 2 明确失败（不弹商店、不静默绕锁）。

    每次调用的 runId 唯一（时间戳 + PID + 随机段，不只用 PID），并写入输出目录的
    marker；输出目录必须由本轮新建；已有目录与重解析路径均按他人目录处理（rc 5，零新增）。

    退出码（子进程的真实 rc 会原样打印在原因行里）：
      0  成功
      2  前置条件/用法（缺 Go、缺 Python 或 Python 不可用、锁助手缺失、未知示例、
         checkout 输出目录不在仓库内、-SourceDir 与 -Example all 同时使用、
         -GoxmlExe 指向的文件不存在）
      3  生成失败（goxml generate 非 0，或 rc=0 但产物缺失/为空）
      4  Go 构建失败（CLI 或窗口入口，或 rc=0 但产物缺失）
      5  输出目录已存在（包括空目录、已有 marker 或重解析路径）（拒绝覆盖、不删用户文件、
         不写入运行日志）
      6  锁被他人持有（不等待、不删他人锁）
      7  示例源材料缺失（目录不存在、.gxml 不唯一、vm.go / main.go.txt 缺失）
      8  子进程超时（按 -TimeoutSec，默认 900 秒）
      99 未处理异常

    清理：finally 中只释放**自己 token** 的锁；失败时保留全部证据目录（不删除），
    不因为清理成功而吞掉原始失败码。

    注：本文件必须保存为 **UTF-8 with BOM**。Windows PowerShell 5.1（Desktop）把无 BOM
    的 .ps1 按系统 ANSI 代码页（简体中文环境为 GBK）解码，中文会变成乱码。

.EXAMPLE
    pwsh -File scripts/verify-examples.ps1
    # 默认：两个示例，输出到 <仓库>/tmp/verify-examples/<时间戳>

.EXAMPLE
    scripts\verify-examples.ps1 -Example tasks -OutputRoot "D:\我的 构建\验证"
#>
[CmdletBinding()]
param(
    [string]$Example = 'all',
    [string]$RepoRoot = '',
    [string]$OutputRoot = '',
    [string]$GoxmlExe = '',
    [string]$GoExe = 'go',
    [ValidateSet('isolated', 'checkout')]
    [string]$Mode = 'isolated',
    [string]$SourceDir = '',
    [string]$LockPath = '',
    [string]$PythonExe = 'python',
    [int]$TimeoutSec = 900,
    [switch]$SkipLock
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

# ---------------------------------------------------------------- 全局状态

$script:ExitCode = 0
$script:LockToken = $null
$script:LockPathUsed = ''
$script:RunLog = New-Object System.Collections.ArrayList

# runId：同一 PowerShell 宿主连续两次调用也必须唯一，因此**不能只用 PID**
# （同进程内两次调用 PID 相同）。组成：时间戳 + PID + 随机段。
$script:RunId = 'verify-examples-' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-p' + $PID + '-' + ([guid]::NewGuid().ToString('N').Substring(0, 8))
# markerToken：本次调用独有的秘密值，写进输出目录的 marker。
# 判定"这个目录是不是本轮创建的"必须同时比对 runId 与 markerToken——
# 只看 marker 文件存在会把上一条/他人留下的同名 marker 误认成自己的，
# 从而在拒绝覆盖的同时又往里写运行日志。
$script:MarkerToken = [guid]::NewGuid().ToString('N')

function Add-Log {
    param([string]$Message)
    [void]$script:RunLog.Add($Message)
}

function Write-Step {
    param([string]$Message)
    $line = '[verify-examples] ' + $Message
    Write-Host $line
    Add-Log $line
}

function Stop-Run {
    param([int]$Code, [string]$Message)
    $line = '[verify-examples] 失败[rc=' + $Code + '] ' + $Message
    Write-Host $line
    Add-Log $line
    $script:ExitCode = $Code
    throw 'STOP-RUN'
}

# ---------------------------------------------------------------- UTF-8（无 BOM）读写
# 只用于本脚本写出的 Go / JSON / go.mod 文件；本 .ps1 自身必须带 BOM，见文件头注释。

function Get-Utf8Encoding {
    return (New-Object System.Text.UTF8Encoding($false))
}

function Read-Utf8File {
    param([string]$Path)
    return [System.IO.File]::ReadAllText($Path, (Get-Utf8Encoding))
}

function Write-Utf8File {
    param([string]$Path, [string]$Content)
    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path -LiteralPath $dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }
    [System.IO.File]::WriteAllText($Path, $Content, (Get-Utf8Encoding))
}

# 输出目录归属判定：marker 存在**且**它属于本次调用。
# 只用"marker 文件存在"当判据是错的：另一个人/上一次运行留下的同名 marker
# 会被认成本轮的，于是脚本一边拒绝覆盖、一边把运行日志写进那个目录。
function Test-OwnMarker {
    param([string]$Dir)
    if ([string]::IsNullOrWhiteSpace($Dir)) { return $false }
    $m = Join-Path $Dir '.verify-examples-marker.json'
    if (-not (Test-Path -LiteralPath $m)) { return $false }
    $json = $null
    try { $json = (Read-Utf8File -Path $m) | ConvertFrom-Json } catch { $json = $null }
    if ($null -eq $json) { return $false }
    $names = @($json.PSObject.Properties.Name)
    if (-not ($names -contains 'runId')) { return $false }
    if (-not ($names -contains 'markerToken')) { return $false }
    if ($json.runId -ne $script:RunId) { return $false }
    if ($json.markerToken -ne $script:MarkerToken) { return $false }
    return $true
}

# Python 解析：必须先确认拿到的是**可用的解释器**，再执行。
# Windows 10/11 的 Microsoft Store 应用执行别名也叫 python.exe，直接执行会
# 打开商店页面（不是锁助手能用的解释器）。这里在解析阶段识别并明确失败，
# 不执行它、不弹商店、不静默绕锁。
function Resolve-Python {
    param([string]$Name)
    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if (-not $cmd) {
        Stop-Run 2 ("找不到 Python 可执行文件 '$Name'（锁助手需要；如调用方已持共享锁可加 -SkipLock）。" +
            "前提：本机预置 Python 3 且在 PATH 中，或用 -PythonExe 指定解释器的完整路径。")
    }
    $src = ''
    try { $src = [string]$cmd.Source } catch { $src = '' }
    if ($src -and ($src -like '*\WindowsApps\*')) {
        Stop-Run 2 ("'$Name' 解析到 Microsoft Store 应用执行别名（$src），不是可用的 Python 解释器；" +
            "请用 -PythonExe 指定真实解释器路径，或先安装 Python。本工具不会打开商店，也不会绕开锁直接继续。")
    }
    return $src
}

# ---------------------------------------------------------------- 子进程

function Format-Arg {
    param([string]$Value)
    # Windows CRT 参数规则：引号前已有反斜杠要加倍；末尾反斜杠也要加倍，
    # 否则会把结束引号转义掉（"D:\" 这种路径正是反例）。
    $v = [regex]::Replace($Value, '(\\*)"', '$1$1\"')
    $v = [regex]::Replace($v, '(\\+)$', '$1$1')
    return '"' + $v + '"'
}

function Invoke-Real {
    param(
        [string]$Exe,
        [string[]]$Arguments,
        [string]$WorkingDirectory,
        [string]$Label
    )

    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Exe
    $psi.Arguments = (($Arguments | ForEach-Object { Format-Arg $_ }) -join ' ')
    $psi.WorkingDirectory = $WorkingDirectory
    $psi.UseShellExecute = $false
    # 实测（Windows PowerShell 5.1 / .NET Framework 4.8）：第一次读 $psi.Environment
    # 恒返回 $null，从第二次读起才带上继承来的父进程环境（打点取证：D0=NULL、
    # D1..D3=162 项）。早先以为是"UseShellExecute=false 之后才可用"，那是错的：
    # 当时探针里其实读了两次。这里显式做一次丢弃式预热读，再判断是否可用。
    $null = $psi.Environment
    if ($null -ne $psi.Environment) {
        $psi.Environment['CGO_ENABLED'] = '0'
        $psi.Environment['GOMAXPROCS'] = '2'
        $psi.Environment['GOPROXY'] = 'off'
        $psi.Environment['GOSUMDB'] = 'off'
        $psi.Environment['GOTOOLCHAIN'] = 'local'
        $psi.Environment['GOFLAGS'] = '-mod=readonly'
    }
    else {
        Write-Step ('警告：无法向子进程注入环境（Environment 不可用），沿用当前环境：' + $Label)
    }

    # 刻意**不**重定向 stdout/stderr：
    #   1) 实测：重定向后先 ReadToEnd(stdout) 再 ReadToEnd(stderr)，子进程 stderr 管道
    #      写满时会阻塞在写 stdout 之前 -> 死锁（200KB 双流取证脚本停在 stdout 读取）；
    #   2) 实测：改用 BeginOutputReadLine/BeginErrorReadLine 的 DataReceived 事件，
    #      PS 5.1 下 stringbuilder 长度恒为 0，收不到任何数据；
    # 因此让子进程输出直通控制台：不截断、不转码、不会死锁。
    $p = [System.Diagnostics.Process]::Start($psi)
    $done = $p.WaitForExit($TimeoutSec * 1000)
    if (-not $done) {
        try { $p.Kill() } catch { }
        Stop-Run 8 ('子进程超时（' + $TimeoutSec + 's）未退出：' + $Label)
    }
    return $p.ExitCode
}

# ---------------------------------------------------------------- 主流程

try {
    # 1) 仓库根（可从任意当前目录运行）
    if ([string]::IsNullOrWhiteSpace($RepoRoot)) {
        $RepoRoot = Split-Path -Parent $PSScriptRoot
    }
    if (-not (Test-Path -LiteralPath $RepoRoot)) { Stop-Run 2 "仓库根不存在: $RepoRoot" }
    $RepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path.TrimEnd('\')
    if (-not (Test-Path -LiteralPath (Join-Path $RepoRoot 'go.mod'))) {
        Stop-Run 2 "仓库根下没有 go.mod: $RepoRoot"
    }
    Write-Step "仓库根: $RepoRoot"

    # 2) 示例选择
    if ($Example -eq 'all') {
        if ($SourceDir -ne '') { Stop-Run 2 "-SourceDir 只能用于单个示例，不能与 -Example all 同时使用" }
        $names = @('starter', 'tasks')
    }
    elseif (@('starter', 'tasks') -contains $Example) {
        $names = @($Example)
    }
    else {
        Stop-Run 2 "未知示例 '$Example'（可选 starter / tasks / all）"
    }
    Write-Step ("示例: " + ($names -join ', ') + "；模块形态: $Mode")

    # 3) 前置：Go
    $goCmd = Get-Command $GoExe -ErrorAction SilentlyContinue
    if (-not $goCmd) { Stop-Run 2 "找不到 Go 可执行文件 '$GoExe'（请用 -GoExe 指定，例如 C:\Program Files\Go\bin\go.exe）" }

    # 4) 锁（调用方已持锁时用 -SkipLock）
    #    默认锁路径是**仓库内**的 tmp/verify-examples/heavy.lock（由锁助手按自身位置推导）：
    #    随 checkout 迁移，另一台机器不需要任何历史目录。需要共用夜间公共锁时
    #    必须显式传 -LockPath —— 默认值不会替代、也不得绕过公共锁。
    if (-not $SkipLock) {
        $null = Resolve-Python -Name $PythonExe
        $helper = Join-Path $PSScriptRoot 'verify-examples-lock.py'
        if (-not (Test-Path -LiteralPath $helper)) {
            Stop-Run 2 ("锁助手缺失: " + $helper + "（它随仓库提供，位于 scripts/ 下；若只单独拷贝了本 .ps1，请把 verify-examples-lock.py 一起拷上，或用 -SkipLock 由调用方自行持锁）")
        }
        if ([string]::IsNullOrWhiteSpace($LockPath)) {
            $LockPath = Join-Path $RepoRoot 'tmp/verify-examples/heavy.lock'
        }
        $lockArgs = @('acquire', '--lock-path', $LockPath, '--run-id', $script:RunId)
        $lockOut = & $PythonExe $helper $lockArgs 2>$null
        $lockJson = $null
        try { $lockJson = (($lockOut | Out-String).Trim()) | ConvertFrom-Json } catch { $lockJson = $null }
        if (-not $lockJson) { Stop-Run 2 ("锁助手输出无法解析（python 退出码 " + $LASTEXITCODE + "）: " + ($lockOut | Out-String)) }
        if (-not $lockJson.ok) {
            if ($lockJson.busy) {
                Stop-Run 6 ("锁被他人持有: " + $lockJson.path + "；详情 " + ($lockOut | Out-String))
            }
            Stop-Run 2 ("取锁失败: " + ($lockOut | Out-String))
        }
        $script:LockToken = $lockJson.token
        $script:LockPathUsed = $LockPath
        Write-Step ("已取得锁: " + $script:LockPathUsed + "（run id " + $script:RunId + "）")
    }
    else {
        Write-Step '-SkipLock：调用方声明已持有共享锁，本脚本不再取锁'
    }

    # 5) 输出目录（只写自己创建的隔离目录）
    if ([string]::IsNullOrWhiteSpace($OutputRoot)) {
        $OutputRoot = Join-Path $RepoRoot ('tmp/verify-examples/' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
    }
    if (-not [System.IO.Path]::IsPathRooted($OutputRoot)) {
        $OutputRoot = Join-Path (Get-Location).Path $OutputRoot
    }
    $OutputRoot = [System.IO.Path]::GetFullPath($OutputRoot)
    $repoPrefix = $RepoRoot.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
    if ($Mode -eq 'checkout' -and -not $OutputRoot.StartsWith($repoPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        Stop-Run 2 "checkout 模式要求输出目录位于仓库内（当前: $OutputRoot）"
    }
    # Claim a new leaf atomically: neither empty user directories nor reparse points
    # are owned by this invocation. New-Item without -Force also rejects a creator race.
    $probe = [System.IO.Path]::GetFullPath($OutputRoot)
    while ($probe) {
        if (Test-Path -LiteralPath $probe) {
            $item = Get-Item -LiteralPath $probe -Force
            if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
                Stop-Run 5 "输出路径含重解析目录，拒绝写入: $probe"
            }
        }
        $parent = Split-Path -Parent $probe
        if ($parent -eq $probe) { break }
        $probe = $parent
    }
    if (Test-Path -LiteralPath $OutputRoot) {
        Stop-Run 5 "输出目录已存在，拒绝接管（包括空目录或已有 marker）: $OutputRoot"
    }
    try {
        New-Item -ItemType Directory -Path $OutputRoot -ErrorAction Stop | Out-Null
    }
    catch {
        if (Test-Path -LiteralPath $OutputRoot) {
            Stop-Run 5 "输出目录被其它创建者占用，拒绝接管: $OutputRoot"
        }
        throw
    }
    $OutputRoot = (Resolve-Path -LiteralPath $OutputRoot).Path.TrimEnd('\')
    Write-Utf8File -Path (Join-Path $OutputRoot '.verify-examples-marker.json') -Content (@{
        tool        = 'verify-examples.ps1'
        createdAt   = (Get-Date).ToString('s')
        repoRoot    = $RepoRoot
        mode        = $Mode
        examples    = $names
        pid         = $PID
        runId       = $script:RunId
        markerToken = $script:MarkerToken
        lockToken   = $script:LockToken
        lockPath    = $script:LockPathUsed
    } | ConvertTo-Json)
    Write-Step "输出目录: $OutputRoot"

    # 6) 构建 CLI（可用 -GoxmlExe 复用已有二进制）
    if ([string]::IsNullOrWhiteSpace($GoxmlExe)) {
        $binDir = Join-Path $OutputRoot 'bin'
        New-Item -ItemType Directory -Force -Path $binDir | Out-Null
        $GoxmlExe = Join-Path $binDir 'goxml.exe'
        Write-Step "构建 CLI -> $GoxmlExe"
        $rc = Invoke-Real -Exe $GoExe -Arguments @('build', '-o', $GoxmlExe, './cmd/goxml') `
            -WorkingDirectory $RepoRoot -Label 'go build ./cmd/goxml'
        if ($rc -ne 0) { Stop-Run 4 ("构建 goxml CLI 失败，go 真实 rc=" + $rc + "（原始输出见上方控制台）") }
    }
    elseif (-not (Test-Path -LiteralPath $GoxmlExe)) {
        Stop-Run 2 "-GoxmlExe 指向的文件不存在: $GoxmlExe"
    }

    # 7) 每个示例：生成 + 构建窗口入口（不运行）
    foreach ($name in $names) {
        Write-Step "=== 示例 $name ==="
        $srcDir = if ($SourceDir -ne '') { $SourceDir } else { Join-Path $RepoRoot ('examples/gxml/testdata/' + $name) }
        if (-not (Test-Path -LiteralPath $srcDir)) { Stop-Run 7 "示例源目录不存在: $srcDir" }
        foreach ($f in @('vm.go', 'main.go.txt')) {
            if (-not (Test-Path -LiteralPath (Join-Path $srcDir $f))) { Stop-Run 7 "示例源材料缺失: $srcDir\$f" }
        }
        # .gxml 文件名不按目录名推导：starter 是 home.gxml，tasks 是 tasks.gxml。
        $gxmlFiles = @(Get-ChildItem -LiteralPath $srcDir -Filter '*.gxml' -File)
        if ($gxmlFiles.Count -ne 1) {
            Stop-Run 7 ("示例源目录内 .gxml 文件应恰好 1 个，实际 " + $gxmlFiles.Count + " 个: " + $srcDir)
        }
        $gxmlName = $gxmlFiles[0].Name
        $gxmlBase = [System.IO.Path]::GetFileNameWithoutExtension($gxmlName)
        Write-Step ("源材料: " + $srcDir + "（页面 " + $gxmlName + "）")

        $moduleDir = Join-Path $OutputRoot $name
        New-Item -ItemType Directory -Force -Path $moduleDir | Out-Null

        if ($Mode -eq 'isolated') {
            $repoMod = Read-Utf8File -Path (Join-Path $RepoRoot 'go.mod')
            $moduleName = 'example.com/goxml-' + $name
            $mod = [regex]::Replace($repoMod, '(?m)^module\s+\S+', 'module ' + $moduleName)
            $mod = $mod + "`nrequire github.com/goxml/goxml v0.0.0`nreplace github.com/goxml/goxml => `"" + ($RepoRoot -replace '\\', '/') + "`"`n"
            Write-Utf8File -Path (Join-Path $moduleDir 'go.mod') -Content $mod
            $repoSum = Join-Path $RepoRoot 'go.sum'
            if (Test-Path -LiteralPath $repoSum) {
                Copy-Item -LiteralPath $repoSum -Destination (Join-Path $moduleDir 'go.sum') -Force
            }
            else {
                Write-Step '警告：仓库根没有 go.sum，隔离模块将不带校验和（离线构建可能失败）'
            }
            Write-Step ("隔离模块: " + $moduleName + " -> replace " + $RepoRoot)
        }

        Copy-Item -LiteralPath (Join-Path $srcDir 'vm.go') -Destination (Join-Path $moduleDir 'vm.go') -Force
        Copy-Item -LiteralPath (Join-Path $srcDir $gxmlName) -Destination (Join-Path $moduleDir $gxmlName) -Force

        # main 模板 -> cmd/<name>/main.go（checkout 模式做本地导入替换）
        $mainText = Read-Utf8File -Path (Join-Path $srcDir 'main.go.txt')
        $mainText = $mainText.Replace("`r`n", "`n")
        if ($Mode -eq 'checkout') {
            $rel = $moduleDir.Substring($RepoRoot.Length).TrimStart('\', '/') -replace '\\', '/'
            $importPath = 'github.com/goxml/goxml/' + $rel
            $before = $mainText
            $mainText = $mainText.Replace('example.com/goxml-' + $name, $importPath)
            if ($before -eq $mainText) { Stop-Run 7 "main 模板未包含预期导入 example.com/goxml-$name，无法替换" }
            Write-Step ("导入替换: example.com/goxml-" + $name + " -> " + $importPath)
        }
        $cmdDir = Join-Path $moduleDir ('cmd/' + $name)
        New-Item -ItemType Directory -Force -Path $cmdDir | Out-Null
        Write-Utf8File -Path (Join-Path $cmdDir 'main.go') -Content $mainText

        # 生成（--no-cache：每次都真实生成，不复用缓存）
        Write-Step ("generate: " + (Join-Path $moduleDir $gxmlName))
        $rc = Invoke-Real -Exe $GoxmlExe -Arguments @('generate', (Join-Path $moduleDir $gxmlName), '--no-cache') `
            -WorkingDirectory $moduleDir -Label ('goxml generate ' + $gxmlName)
        if ($rc -ne 0) { Stop-Run 3 ("生成失败，goxml 真实 rc=" + $rc + "（原始输出见上方控制台）") }
        $genFile = Join-Path $moduleDir ($gxmlBase + '_gen.go')
        if (-not (Test-Path -LiteralPath $genFile)) {
            Stop-Run 3 ("生成 rc=0 但产物缺失: " + $genFile)
        }
        $genLen = (Get-Item -LiteralPath $genFile).Length
        if ($genLen -le 0) { Stop-Run 3 ("生成产物为空: " + $genFile) }
        Write-Step ("产物: " + $genFile + "（" + $genLen + " 字节）")

        # 构建窗口入口（只构建，不执行）
        $entry = Join-Path $OutputRoot ($name + '-entry.exe')
        Write-Step ("build 窗口入口 -> " + $entry)
        $rc = Invoke-Real -Exe $GoExe -Arguments @('build', '-o', $entry, ('./cmd/' + $name)) `
            -WorkingDirectory $moduleDir -Label ('go build ./cmd/' + $name)
        if ($rc -ne 0) { Stop-Run 4 ("构建窗口入口失败，go 真实 rc=" + $rc + "（原始输出见上方控制台）") }
        if (-not (Test-Path -LiteralPath $entry)) { Stop-Run 4 ("构建 rc=0 但产物缺失: " + $entry) }
        Write-Step ("窗口入口已构建（不执行）: " + $entry + "，" + (Get-Item -LiteralPath $entry).Length + " 字节")
    }

    Write-Step '全部通过：真实生成 + 真实构建完成；未执行任何窗口，未产生点击/呈现证据。'
    $script:ExitCode = 0
}
catch {
    if ($_.Exception.Message -ne 'STOP-RUN') {
        $msg = '[verify-examples] 未处理异常: ' + $_.Exception.Message
        Write-Host $msg
        Add-Log $msg
        Add-Log ($_.ScriptStackTrace)
        if ($script:ExitCode -eq 0) { $script:ExitCode = 99 }
    }
}
finally {
    # 运行日志：只在**本轮创建的**目录里写。
    # 判据演进：初版"只看目录存在就写"会向被判定为占用的用户目录塞日志；
    # 二版改成"marker 存在就写"，但仍会把**上一次/他人留下的同名 marker** 认成自己的。
    # 现在必须 Test-OwnMarker（runId + markerToken 同时匹配）才写。
    $logPath = ''
    try {
    if (-not [string]::IsNullOrWhiteSpace($OutputRoot)) {
        if (Test-OwnMarker -Dir $OutputRoot) {
            $logPath = Join-Path $OutputRoot 'verify-examples-run.log'
            $text = ($script:RunLog -join "`n") + "`n[verify-examples] 退出码 " + $script:ExitCode + "`n"
            [System.IO.File]::WriteAllText($logPath, $text, (Get-Utf8Encoding))
        }
    }
    } catch {
        Write-Host ('[verify-examples] 警告：运行日志写入失败: ' + $_.Exception.Message)
        if ($script:ExitCode -eq 0) { $script:ExitCode = 99 }
    }
    if ($logPath -ne '') { Write-Host ('[verify-examples] 运行日志: ' + $logPath) }
    if ($script:LockToken) {
        $helper = Join-Path $PSScriptRoot 'verify-examples-lock.py'
        $relArgs = @('release', $script:LockToken)
        if ($script:LockPathUsed -ne '') {
            $relArgs = @('release', '--lock-path', $script:LockPathUsed, $script:LockToken)
        }
        try {
            $relOut = & $PythonExe $helper $relArgs 2>$null
            $relJson = (($relOut | Out-String).Trim()) | ConvertFrom-Json
            if ($relJson.released) {
                Write-Step ("已释放自己的锁（token " + $script:LockToken.Substring(0, 8) + "…）")
            }
            else {
                Write-Host ('[verify-examples] 警告：锁未释放（' + $relJson.error + '）；路径 ' + $script:LockPathUsed + '，token ' + $script:LockToken + ' —— 属本工具持有，可凭 token 核验后删除')
            }
        }
        catch {
            Write-Host ('[verify-examples] 警告：释放锁时异常: ' + $_.Exception.Message + '；token ' + $script:LockToken)
        }
    }
    if ($script:ExitCode -ne 0) {
        Write-Host ('[verify-examples] 失败证据保留在: ' + $OutputRoot + '（本工具不删除失败产物）')
    }
    Write-Host ('[verify-examples] 退出码 ' + $script:ExitCode)
}

exit $script:ExitCode
