# 安装 post-commit 钩子：每次提交成功后自动推送到 origin。
#
# 用法：.\scripts\install-hooks.ps1
# 卸载：Remove-Item .git/hooks/post-commit
#
# 注意：推送失败时提交已经产生（本地领先远端）。钩子会在下次提交时
# 再次尝试推送当前分支；离线环境建议改用 commit-push.ps1 显式推送。

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot

$hooksDir = Join-Path $repoRoot '.git\hooks'
if (-not (Test-Path $hooksDir)) {
    Write-Host "✗ 未找到 .git/hooks，确认这是 Git 仓库根目录" -ForegroundColor Red
    exit 1
}

$hookPath = Join-Path $hooksDir 'post-commit'

$content = @'
#!/bin/sh
# 由 scripts/install-hooks.ps1 生成：提交后自动推送当前分支。
# 卸载：删除本文件。

branch=$(git branch --show-current)
if [ -z "$branch" ]; then
    echo "[hook] detached HEAD，跳过推送"
    exit 0
fi

# 非交互：凭据缺失时立即失败而不是挂起
GIT_TERMINAL_PROMPT=0 git push origin "$branch" >/dev/null 2>&1
if [ $? -ne 0 ]; then
    echo ""
    echo "[hook] ⚠ 自动推送失败（提交已在本地产生）"
    echo "[hook]   修复后可手动执行： git push origin $branch"
    echo ""
fi
exit 0
'@

# 写为 LF 行尾（Git 钩子在 Windows 上由 sh 执行，CRLF 会报 bad interpreter）
$content = $content -replace "`r`n", "`n"
[System.IO.File]::WriteAllText($hookPath, $content, (New-Object System.Text.UTF8Encoding $false))

Write-Host "✓ 已安装 post-commit 钩子：$hookPath" -ForegroundColor Green
Write-Host ""
Write-Host "行为：git commit 成功后自动 git push origin <当前分支>" -ForegroundColor Cyan
Write-Host "卸载：Remove-Item .git/hooks/post-commit" -ForegroundColor Gray
Write-Host ""
Write-Host "提示：钩子里的推送失败不会阻止提交；离线时请用 scripts/commit-push.ps1" -ForegroundColor Yellow
