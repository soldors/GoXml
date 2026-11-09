# 2026-11-09 R27-A HYDRATION-PROTOCOL（DeepSeek）开发日志

固定基线 `d6b1fc063bb5671ab6340181cd8b16160c5b676c`（开工核验：主库 HEAD 同、porcelain 0、索引空）。分支 `codex/r27-deepseek-hydration`，树 `r27-deepseek-hydration`。所有权：`pkg/runtime/hydration/`、`internal/devx/hydration/`、`internal/devx/watch`（opt-in 最小钩子）、本 dev-log。
**候选**：显式 DTO 状态协议为候选（未批准默认策略）；不反射任意 VM、不改 App/公共 CLI。
提交：`d6b1fc0`（父）→ `ce050fb`（A1）→ `e94932f`（A2）→ `cb23570`（A3 测试）→ 本 dev-log。

## A1 协议/线程边界/不变量（`ce050fb`）
- `pkg/runtime/hydration`：显式 `Envelope`（version/schema/appId/sessionId/generation/createdAt/checksum/size上限），`NewEnvelope/Encode/DecodeAndVerify/DecodePayload`，错误分类（corrupt/checksum/too-large/schema/app/session/version/stale）。**只依赖标准库**，Runtime 层不 import devx；无反射扫描任意 VM。
- `internal/devx/hydration`：`Store`（`.goxml/hydration/<session>.state`，原子 temp+fsync+rename，只写自己 session 文件、不覆盖他人）+ `Protocol`（Snapshot：所有者在 CaptureTimeout 内有界捕获→序列化→原子提交；失败/超时/取消不提交（保旧）。Restore：全量校验（schema/app/session/version/checksum/generation）后交所有者 apply，拒绝不部分应用；ErrNoState=冷启动）。
- 线程边界：Capture 在所有者串行点执行（唯一读活 VM）；worker 只处理不可变字节+原子写；恢复在新代 UI 所有者应用。快照时点=捕获串行点；不承诺停止前后续编辑已保存、不宣称无损/零停机。
- 会话/所有权：token=sessionID 证明“自有”范围，checksum 只证内容一致不称鉴权；`.goxml` 已在 Watch 快照排除，状态写入不自激重建。
- 策略（逐项，见 scope-matrix）：旧程序不支持协议/损坏/版本-schema-session-gen 不符/超大/截断/恢复拒绝→不部分应用不静默成功；无状态→冷启动；**建议快照失败保旧不切换**（已实现）。

## A2 核心 + 真实两进程整链（`e94932f`）
- Watch opt-in：`Config.Hydration *HydrationHook`（nil 时行为与 R26 逐字节一致：无额外 I/O/线程/顺序/失败保旧/清理变化）。非 nil：新代构建成功、停旧前 `BeforeSwap` 请求旧代快照并原子提交；失败/超时/取消→保旧不切换。`ProcessManager.EnvFor(gen)` 为新代注入恢复状态文件/会话。
- 真实两进程 e2e（`TestTwoProcessHydrationRestore`）：旧代设非默认 DTO（count=7/items=a,b,c/nonDefault=OLD-NON-DEFAULT）→ 经协议快照 → 停旧（PID 18984）→ 新代（PID 31176）从状态文件恢复**精确值**（gen=2，打印 RESTORED 对齐）。真实 console helper + 真实文件 + 真实进程，不 mock 整链。
- Watch opt-in 判别：默认 nil 不变；capture 成功才 swap；capture 失败/超时保旧不 swap。R26 watch 全量回归绿。

## A3 判别力/成本/交付（`cb23570` 测试）
- **两条可编译行为变异**（独立副本 + Go overlay）：
  - MUT-1 跳过 checksum+schema 校验（`hydration.DecodeAndVerify`）→ `TestChecksumMismatch`+`TestSchemaAppSessionMismatch` **FAIL**。
  - MUT-2 忽略捕获失败仍提交（`devx.Protocol.Snapshot`）→ `TestSnapshotCaptureFailureKeepsOld` **FAIL**。
  - 恢复（无 overlay）→ 三包全 PASS。真红非编译/flags。证据 raw/mutations-*.txt。
- 成本单列（见 closure-matrix）：快照捕获=一次 owner 串行捕获 + JSON 序列化（payload ≤4MiB、Envelope ≤8MiB）；worker 一次原子写（temp+fsync+rename）；恢复=一次读+全校验+apply。未启用钩子：稳定路径**零新增每帧 I/O/线程**。

## 门禁（Windows/amd64，CGO=0；race MinGW）
`pkg/runtime/hydration` + `internal/devx/hydration` + `internal/devx/watch` test 全绿；`-race` 三包 rc0/0 DATA RACE；`go build ./...` rc0；**GOOS=linux CGO=0 build 三包 rc0**（交叉编译，非运行）；vet rc0；gofmt 空；diagdoc ok；docgate ok；覆盖 runtime **85.0%** / devx **83.5%**（devx 略低于 85%，余量为跨平台难触发的 os 失败分支，**如实登记**不凑数）/ watch 88.1%。

## 未证边界
无原生窗口/视觉/IME；无跨平台运行（仅 GOOS=linux 交叉编译）；无性能采样；Hydration 仅候选，不关闭完整 DEVX-001/LIFE；通用 Lifecycle 未实现；真实“UI 线程应用”以所有者回调/e2e 新进程日志证明，非 gio 原生渲染；helper 仅自有 console、有界、不开窗、不跑用户 main/init。

## 五态
实现 done(`cb23570`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
