# 提交并推送到 GitHub（origin/master）。
#
# 用法：
#   .\scripts\commit-push.ps1 -Message "feat: xxx" -Files "a.go","b.md"
#   .\scripts\commit-push.ps1 -Message "docs: xxx" -All -Yes
#
# 设计约束（来自本仓库两次提交事故）：
#   - 默认必须显式列文件，避免 -A 把并发会话内容/临时垃圾带入提交；
#   - 提交前打印实际暂存清单，供人工核对；
#   - 推送失败必须报错退出，不得假装成功。

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Message,

    # 显式文件列表（相对仓库根）。
    [string[]]$Files,

    # 暂存全部改动（危险：会带上并发会话内容）。
    [switch]$All,

    # 跳过提交前的确认。
    [switch]$Yes,

    # 提交前跑门禁。
    [switch]$SkipGates
)

$ErrorActionPreference = 'Stop'
Set-Location (Split-Path -Parent $PSScriptRoot)

function Fail($msg) {
    Write-Host "✗ $msg" -ForegroundColor Red
    exit 1
}

# ---- 1. 校验参数组合 ----
if (-not $All -and -not $Files) {
    Fail "必须指定 -Files（推荐）或显式使用 -All"
}
if ($All -and $Files) {
    Fail "-All 与 -Files 不能同时使用"
}
if ($All) {
    Write-Host "⚠ -All 会暂存工作区全部改动，可能带入并发会话的内容。" -ForegroundColor Yellow
}

# ---- 2. 门禁 ----
if (-not $SkipGates) {
    Write-Host "→ 门禁检查..." -ForegroundColor Cyan
    $env:CGO_ENABLED = '0'
    $env:GOPROXY = 'off'

    $fmt = gofmt -l . 2>&1
    if ($fmt) { Fail "gofmt 未通过：`n$($fmt -join "`n")" }

    go build ./... 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "go build 失败" }

    go vet ./... 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "go vet 失败" }

    go test ./... 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "go test 失败" }

    go test ./internal/compiler/diagdoc/ ./internal/docgate/ 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "diagdoc/docgate 失败" }

    Write-Host "  ✓ 门禁通过" -ForegroundColor Green
}

# 记录本次运行**之前**索引里已有的条目：失败时要能只撤销自己加的那部分。
$preStaged = @(git -c core.quotePath=false diff --cached --name-only)

# ---- 3. 暂存 ----
if ($All) {
    git add -A
} else {
    foreach ($f in $Files) {
        if (-not (Test-Path -LiteralPath $f)) {
            # 允许删除：文件不存在但 git 知道它 → 记录删除
            $known = git ls-files --error-unmatch -- $f 2>&1
            if ($LASTEXITCODE -ne 0) { Fail "文件不存在且未被跟踪：$f" }
            git rm --cached --ignore-unmatch -- $f | Out-Null
        } else {
            git add -- $f
        }
    }
}

# ---- 4. 打印实际暂存清单（核对点）----
# 共享工作树下，索引里可能存在**别人**已暂存的文件。`git commit` 会提交整个索引，
# 于是「只提交我声明的文件」会悄悄变成「顺手提交别人的在制品」（2026-10-21 已发生一次）。
# 因此这里必须硬校验：声明了 -Files 时，索引里不允许出现清单外的条目。
if ($All) {
    $staged = git diff --cached --name-status
} else {
    # core.quotePath=false：否则非 ASCII 路径会被输出成 \345\274\200 形式，
    # 与 -Files 里的中文路径比对必然失败（首版护栏因此把自家文件误判为「清单外」）。
    $stagedPaths = @(git -c core.quotePath=false diff --cached --name-only)
    $wanted = @{}
    foreach ($f in $Files) { $wanted[($f -replace '\\', '/')] = $true }

    $foreign = @()
    foreach ($p in $stagedPaths) {
        if (-not $wanted.ContainsKey(($p -replace '\\', '/'))) { $foreign += $p }
    }
    if ($foreign.Count -gt 0) {
        Write-Host "✗ 索引里有本次清单之外的文件（可能来自并发会话）：" -ForegroundColor Red
        $foreign | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
        # 失败即撤销**本次**新加的暂存，避免"失败一次就把文件留在索引里"。
        # 2026-10-21 事故：护栏调试失败后未撤销，随后被并发会话的提交卷走。
        foreach ($f in $Files) {
            $norm = $f -replace '\\', '/'
            if ($preStaged -notcontains $norm) { git restore --staged -- $f 2>&1 | Out-Null }
        }
        Fail "拒绝提交：先 `git restore --staged <上述文件>`，或确认要整体提交时改用 -All"
    }

    # 只取本次声明的文件（-- 之后是 pathspec）。
    $staged = git -c core.quotePath=false diff --cached --name-status -- $Files
}
if (-not $staged) { Fail "暂存区为空，没有可提交的内容" }

Write-Host "→ 即将提交以下文件：" -ForegroundColor Cyan
$staged | ForEach-Object { Write-Host "  $_" }

# ---- 5. 确认 ----
if (-not $Yes) {
    $ans = Read-Host "确认提交并推送？(y/N)"
    if ($ans -notmatch '^[Yy]') { Write-Host "已取消"; exit 0 }
}

# ---- 6. 提交 ----
# 同样按 pathspec 提交：即使索引里还有别的条目，也只提交声明过的文件。
if ($All) {
    git commit -m $Message | Out-Null
} else {
    git commit -m $Message -- $Files | Out-Null
}
if ($LASTEXITCODE -ne 0) { Fail "git commit 失败" }
$sha = (git rev-parse --short HEAD).Trim()
Write-Host "  ✓ 提交 $sha" -ForegroundColor Green

# ---- 7. 推送 ----
$branch = (git branch --show-current).Trim()
if (-not $branch) { Fail "当前处于 detached HEAD，无法推送" }

Write-Host "→ 推送 $branch → origin..." -ForegroundColor Cyan
$env:GIT_TERMINAL_PROMPT = '0'   # 避免凭据提示卡死
$pushOut = git push origin $branch 2>&1
$pushCode = $LASTEXITCODE
if ($pushCode -ne 0) {
    Write-Host ($pushOut | Select-Object -Last 8) -ForegroundColor Red
    Write-Host "✗ 提交已产生（$sha），但推送失败。" -ForegroundColor Red
    Write-Host "  修复网络/凭据后执行：git push origin $branch" -ForegroundColor Yellow
    exit 1
}

Write-Host "  ✓ 已推送 origin/$branch" -ForegroundColor Green
Write-Host ""
Write-Host "完成：$sha 已同步到 GitHub" -ForegroundColor Green
