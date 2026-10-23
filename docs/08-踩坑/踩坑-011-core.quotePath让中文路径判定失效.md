# PIT-011：`core.quotePath` 会把中文路径变成转义串，让脚本判定永久失效

- 分类：工具链 / Git / 中文路径
- 关联：`internal/docgate/docgate_test.go`（第 109-145 行的判定与注释）

## 现象

解析 `git show --name-only` 输出的脚本，在含中文路径的仓库里**永远匹配不上**：

```text
实际拿到的路径（默认 core.quotePath=true）：
docs/05-\346\274\200\345\217\221/dev-log/2026-10-23.md

脚本里写的判定：
strings.Contains(f, "docs/05-开发/dev-log/")   → 恒为 false
```

后果是**方向性的错判**：只改 dev-log / `CHANGELOG.md` 的"日志维护提交"被误判为普通提交，
于是它必须引用自身哈希；而这类提交会先经历一段"免检期"——它作为 HEAD 时被门禁窗口排除，
长期不被检查，等 HEAD 前移、它进入窗口之后门禁才突然变红，看起来像"今天新引入的问题"。

（`a02edd6` 就是这样被抓出来的：它此前一直是 HEAD，被门禁窗口排除，直到 HEAD 前移。）

## 根因

git 默认 `core.quotePath=true`：非 ASCII 字节会被转写成 `\NNN` 八进制转义。
**这不影响 git 自身使用**（传回命令时仍能正确解析），只影响"按字符串解析路径"的下游脚本。

## 解决

解析 git 路径输出的**每一处**都要显式关闭：

```bash
git -c core.quotePath=false show --name-only --format= <sha>
git -c core.quotePath=false diff --name-only
git -c core.quotePath=false status --short
```

`-c core.quotePath=false` 在 `internal/docgate` 里**不是可选项**，
去掉它就等于让中文路径上的所有日志维护提交判定失效。

## 预防

- 任何用 `strings.Contains` / 正则去匹配 **git 输出路径** 的脚本，先关 `quotePath`。
- 反过来也成立：如果某个和中文路径有关的门禁**永远绿**，先怀疑它其实一直在假绿。
- 临时命令也建议带上（`git -c core.quotePath=false status --short`），省得看 `\346\274\200`。

## 数据来源

- `internal/docgate/docgate_test.go` 第 109-145 行（`isLogMaintenance` 与其注释，明确记载 `a02edd6`）
- `internal/docgate/docgate_test.go` 第 23-24 行（`recentCommits = 10`，窗口不含 HEAD）
