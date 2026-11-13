# R29-C S1：多窗口宿主建模与基线实证（HY4，2026-11-13）

- 切片：S1（建模并实证）；起点 `ddef3a4d52588c3edebcb0dfa999c048addecd15`。
- 只读盘点 `app.Main`、旧 `Run`/`RunWith`/`RunWindow*`、单窗进程入口
  `RunSingleWindowProcess`、真实 `DestroyEvent` 与 `WithOnDestroy` 回调；
  ADR-0018 的 UI owner 串行点、CancelOnly 与 Finalize 不隐式 Shutdown 保持。
- 受控两窗探针（`pkg/runtime/app/r29_s1_baseline_probe_test.go`，
  `!nativewindow`，纯内存事件序列）：
  1. `TestS1SingleWindowProcessOwnsWholeProcess`：注入 mainFunc/opts.next/
     opts.exit，两次单窗进程入口各恰好一次进程级 exit(code=0) 且先通知
     Observer → 两窗共用旧入口时先关者终止全进程，另一窗无法保持服务
     = 基线缺口。
  2. `TestS1IndependentRouterOwnersSurviveSiblingFinalize`：两个真实独立
     Router/Owner，A Finalize 后 B 根活跃、未卸载、仍可 Layout；B 后台
     Shutdown 只取消，随后由 B 的 UI owner 关闭路径卸载恰一次；A 不被
     重复清理 = 健康对照。
- 决定表与所有权（宿主/窗口/Router）、正常关闭、后台取消、打开失败、
  重复关窗、宿主退出；推荐最小显式 API、兼容影响、回滚点见
  `delivery-c/s1-model.md`。多窗口 API 与退出策略为**候选**，待协调者验收；
  本切片未改生产代码、未开窗、未改已批准合同。
- 定向门禁：`CGO_ENABLED=0 go test -count=1 -v -run TestS1 -timeout 5m
  ./pkg/runtime/app/` rc=0（2 PASS / 0 FAIL）。
