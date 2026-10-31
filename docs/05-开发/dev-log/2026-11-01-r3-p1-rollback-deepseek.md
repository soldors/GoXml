# 2026-11-01 R3 P1 回滚修复（DeepSeek 接管）

任务：R3-P1-HANDOFF-DEEPSEEK。树 `product-r3-deepseek-rollback`，分支 `codex/product-r3-deepseek-rollback-20261101`，基线 `ad3317d`（已含 N1/N2/N4 与 GOFLAGS，不重做）。

## 修复（commit `010deca`）：E1/E4/E5 三入口统一收口

`internal/cli/commit.go` `commitBatchOps` 用**单个 `fail()` 闭包**统一三个失败入口，均**回滚此前已提交项（committed）** + 清理本批 tmp，而非只清 tmp 就返回：

- **E1**（已有普通目标 `target→backup` rename 失败）：旧目标仍在原地、当前项未覆盖；此前 committed 已被新内容覆盖 → `fail(path, "备份原目标: …", committed)` 恢复之。
- **E4**（`Lstat` 返回非 `IsNotExist` 错误）：当前目标不动；`fail(path, statErr, committed)` 回滚此前 committed。（`os.Lstat` 经包级 `osLstatHook`（默认 `os.Lstat`）调用，仅测试受控注入，未改全局文件系统函数。）
- **E5**（`Lstat` 识别**非普通目标**：symlink/目录/junction/reparse）：**显式拒绝**并 `fail(..., committed)`，不备份/不替换/不删除用户对象；不再"靠 rename 自然失败"。
- `rename(tmp→target)` 失败入口改用同一 `fail()`（保持原行为：把当前已备份旧目标并入回滚列表）。

统一语义：`rollbackCommit` 恢复普通目标（旧字节/权限/mtime 经 rename 原样）、撤回**确由本批创建**的新建目标；当前触发失败项**不进 committed**，故既不 restored 也不 withdrawn（不会把未安装项误列成新建撤回）。恢复失败 → `unrestored`+`residue` 如实、备份保留可定位。

## 回归（`commit_p1_rollback_test.go`，7 条，`TestP1*`）
E1 后序失败恢复先前 committed（含字节/无 tmp/bak 残留、restored 断言）；E1 首项失败（committed 空，无 restored/withdrawn）；E4 Lstat 错误回滚先前且当前目标不建、不误撤回；E5 符号链接拒绝 + 回滚先前 + 链接本体保留/Readlink 不变/第三方字节不变/不进 withdrawn；E5 单独链接；成功覆盖+新建仍工作（无 .bak 残留）；空批次 noop。

## 判别红（overlay 还原旧实现）
`raw/mutation/` 外部副本把三入口还原为"只清 tmp 不回滚 committed"：`TestP1E1…`/`TestP1E4…`/`TestP1E5…` **各自 rc=1 变红**（`raw/p1-red-E1/E4/E5` 日志）；恢复后 `TestP1*` 全绿。原源码字节未改（overlay 仅构建期替换）。

## 真实 CLI 证据（新目录 `repro/reallicli`，不覆盖旧 `review-wave-r2/.../fail-dir`）
1-Text→2-Text 结构变更 + `--no-cache` 强制 miss + 父进程持 `pg2/zeta_gen.go` 只读共享（无 DELETE）句柄：
- `exit=2`，stderr 来自**提交阶段**（`提交失败…备份原目标: rename … being used by another process`；非读取/前端）。
- **`page/home_gen.go` 旧字节、权限、mtime 全部恢复**（`homePreservedByte/Stat=true`）；`pg2/zeta_gen.go` 未动；**Manifest 未变**；**tmp/bak 零残留**；错误如实报"已恢复原目标 1"。
- 对照（不持句柄同输入）rc=0 且两页生成物改变（确为内容驱动 miss）。

## 检查
`go test ./internal/cli`（受影响包全量）ok；`go vet ./internal/cli` rc=0；`gofmt -l` 空。Go 重负载均持 heavy.lock 自有 token 并 finally 实测释放。

## 状态与边界
**修复自测通过、待 ZCode 独立复核**（不自行解除 BLOCK）。只改本新树 `internal/cli/commit.go` 与该 P1 回归 + 本 dev-log；未改 pipeline.go/packageDirName/GOFLAGS；未改 qcode 原树/主仓/冻结证据；未 push/合主仓。
