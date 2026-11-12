# 2026-11-12 · R28-C2 关窗就绪发布与行为证明（HY4）

遵守 `coordinator/review-r1-2026-11-12/r2-hy4.md` 与继承的
`coordinator/review-2026-11-12/r1-common.md` 共用约束。范围只含
`pkg/runtime/app/testdata/lifecyclenative/`、它的验收测试、手册与本 dev-log；
Owner/Router/App 生产行为不改。不开窗、不控制桌面、不执行原生程序、不安装依赖。

| 项 | 值 |
|---|---|
| 完整父（R1 冻结） | `d4f8b156af74377cfed78f5a9711d1b878b414e3`，父 `d0e2a79eaf710e6522576fa800f2e6a4a817c57c` |
| 组合基线 / 主库 | `611948d49525744cbb4127265983fd24236e6bf5` / `255c39251127c39d1a684baa881767a5fb7677b9`（只读） |
| 分支 / 独立树 | `codex/r28-release-lifecycle-r2` / `C:/Users/admin/.codex/worktrees/r28-release-lifecycle-r2/Goxml` |
| 代码提交 | `bdf26c84a12aee256b351c2fb3c5ca5ab8a0c322`（6 个文件，父 `d4f8b15`） |
| 本提交 | dev-log 维护提交（按 docgate 自引用规则排除 HEAD）；冻结 readyRef 见交付 `handoff.md` |
| 交付 | `D:/Codes/Goxml-agent-rounds/2026-11-11/product-r28-release-followup/r2-delivery-c/` |

## 协调者 C1-01 / C1-02 的闭合

1. **唯一就绪发布点**：R1 曾「先置 ready flag、再另一次持锁写 marker」，
   协调者受控探针在两者之间调用关窗检查得 `early=false` 且磁盘无 ready。
   本轮 `publishReady()` 在**同一 `mu` 临界区**内先成功把 `ready-to-close`
   写入 `marker.txt`、再置 `readyPublished`；写盘失败返回 error、记录
   `ready_publish_err` 并写 `ready-refused`，**绝不发布**；不再有可提前置位的
   flag（`setReadyWritten()` 为显式 no-op，仅兼容协调者探针）。
   `checkCloseArrival()` 在同一把锁里只读已落盘发布态；`closeArrivedEarly`
   一旦置位**不可逆**，`publishReady()` 不清除它，随后补写 marker 也不能
   让 `Judge()` 翻案（且 ready 会排在 callback 之后）。
   四类受控回归：未发布/已发布正对照、真实写盘失败、早到后补写仍不通过。
2. **非隐式 Shutdown 的可观察证明**：R1 的 `shutdown_calls` 只统计 fixture
   自己调了几次，不能观察 Router 内部。本轮用公开 `router.Context()` 记录
   `LifecycleFinalize` 前后 Router 根 Context 仍活跃；真实 `r.Shutdown()`
   取消根的可区分对照（`r2_contract_test.go` B1/B2）；树外 overlay M4 给生产
   `Router.LifecycleFinalize()` 注入隐式 Shutdown 能令守卫变红。
   `shutdown_calls*` 与 `fixture-shutdown-call-count-changed` 判据**据实收窄**
   为「fixture 日志自洽，不作为 Router 内部行为证据」，手册 §4 与
   `result.json` 的 `evidence_scope` 明确写出。Judge 新增
   `ready-not-published` 与 `router-root-canceled-after-finalize` 判据。
3. 保留原轨迹 `callback → unmount:home → finalize 完成 → observer`、三证明、
   确定性帧请求与既有 17 项无窗口套件；新增 10 项共 **28 项受控用例**。
   **Image v1 对账已独立通过 18/18，本线不重做资产管线、不关闭整卡。**

## 证据纪律（先立红、首败保留）

- 协调者 `lifecycle-readiness-probe_test.go` 复制为 `r2_readiness_probe_test.go`：
  修前 `r2-red-probe` rc=1（`close accepted before ready-to-close was recorded`，
  精确选择器命中真实用例、无 SKIP、无编译失败）；修后同一选择器
  `r2-red-probe-green` rc=0。
- **6 条外部 Go overlay 变异**（树外副本，不改冻结源）：M1 写盘失败仍发布、
  M2 关窗忽略发布、M3 发布清除早到判定、M4 生产 Router 注入隐式 Shutdown、
  M5 删 Router 根判据、M6 删 ready-published 判据。每条 `go test -c -overlay`
  编译 rc=0 → `-run` 精确选择器守卫 rc=1（命中 `--- FAIL`、无编译失败、
  无 SKIP）→ 去掉 overlay 恢复 rc=0；恢复判据为树内目标文件**字节 SHA-256**
  六条全一致。M4 只在树外副本注入，**树内生产文件字节不变**。
- **全树测试环境限制（如实登记，不写成通过）**：`r2-full-tree`
  (`go test -count=1 -timeout 30m ./...`) rc=1，`elapsedSec=1026`、`activeAfter=0`、
  无超时；唯一失败包 `internal/devx/preview`，症状为
  `taskkill /PID … /T /F 失败: exit status 1`（Windows 进程终止环境限制）。
  3 次定向复跑 3/3 rc=1 且均伴随同一 taskkill 失败；主库只读对照
  （HEAD `255c3925`，不含本线任何改动）同包同样 rc=1 + taskkill 失败，
  确证**与本线改动无关**（本线只改 `//go:build nativewindow` 的 fixture，
  默认 `go list ./...` 不选中该包）。按纪律**不重复跑到绿**、不删首败、
  不弱化判据；全树 rc=1 登记为「环境限制/未通过」。见
  `raw/gates/full-tree-first-fail.json`、`preview-recheck.json`、
  `main-control.json`。冻结态门禁不含全树，只跑本线受影响范围。
- 阶段 1 其余门禁：红转绿 rc=0；受控回归 28 PASS / 0 FAIL / 0 SKIP；
  MinGW race 0 DATA RACE；默认/显式标签边界；vet、gofmt、受影响包窄测
  230 PASS / 0 FAIL / 1 SKIP 全 rc=0。
- 冻结态门禁（提交后复跑）：`raw/gates/final-report.json` 与交付 `handoff.md`。

## 固定可复核身份

从**干净冻结树**构建普通/race 二进制，**只编译不运行**。
`raw/fixture/source-manifest.json` 记录冻结 commit / tree SHA、`go.mod`/`go.sum`、
fixture 全部文件、`go list -deps` 得到的本模块构建输入逐文件 SHA-256 与聚合
SHA-256；`raw/fixture/build-identity.json` 记录每条构建的 argv/env/observed rc、
二进制字节与 SHA-256、`go version -m` 原文。若 `go version -m` **缺
`vcs.revision`**（本环境只见 `vcs.modified=true`），以可复核的源码/构建绑定
补证，不把 VCS 元数据当唯一证明、不据 `vcs.modified` 推断提交状态、
**不声称编译即原生通过**。`runner_fields_required` 全部留空。

## 五态与未验证边界

| 状态 | 验收结论 | 复核状态 |
|---|---|---|
| 实现 | 完成（fixture/判据/手册修正；生产零改动） | 待独立复核 |
| 自测 | 通过（**仅限**本线受影响范围 / 无窗口受控集合 / 静态与编译门禁；全树为环境限制未通过） | 待独立复核 |
| 独立复核 | 未验证（协调者对本线冻结 SHA 的再复核尚未进行） | 待独立复核 |
| 主库 | 未合入（HEAD 仍 `255c3925`） | 未要求 |
| 远端 | 未 push；本轮未执行 `ls-remote`，远端状态无法核对 | 未要求 |

**验收结论：未验证 / 复核状态：未要求（待授权后转待独立复核）** 单列：
来自 Gio 真实窗口的 `DestroyEvent`、真实窗口显示与原生退出、真实 FrameEvent
按请求到达、真实进程中的就绪 marker 写盘时序、人工标题栏关闭 8 步、
`runner_observed_rc`、`no_residual_process`、真实 native race、Linux/macOS
实机运行（需 `CGO_ENABLED=1` 与系统库）、IME/DPI/多窗口、人工视觉验收。
`runner_fields_required` 中只能由授权执行者填写的字段本轮**全部未填**。
编译 ≠ 执行，受控回放 / 真实 Router 组合守卫 ≠ 原生 `DestroyEvent`。

冻结后停止，不接下一线；由协调者独立复核、组合验收并决定发布。
