# R27-C 集中返修 R1（WorkBuddy）

日期：2026-11-09。审查基线 `01bf3447205e52b6226a55157903f924a978b14d`（协调者
结论 BLOCK）。新分支 `codex/r27-r1-workbuddy-app-exit`，独立树
`C:/Users/admin/.codex/worktrees/r27-r1-workbuddy-app-exit/Goxml`；原冻结树、
原 delivery/raw、审查 probes/raw 全程只读。交付在
`D:/Codes/Goxml-agent-rounds/2026-11-09/product-r27/workbuddy/repair-r1/`。

本线提交（真实 SHA，供 docgate 登记）：

- S1 `dc485e2e14a162292850029c45553efb4d85c123`：F-C1 懒加载入口按主题来源分流。
- S2 `f0966ee7865ecaa1edf273fac6309bf7816a0f5a`：F-C2 初始化期 stop 同步与
  F-C3 默认选择器隔离 / 执行事实更正。
- S3/冻结 = 本文件所在的日志维护提交（HEAD，门禁按设计排除，故不在此自引用）。

## 原红与新绿

在 `01bf344` 上逐字复制协调者探针
`coord_r27_review_test.go`（SHA `f166cde1…`），
`r27c-r1-probes-red` rc=1，两条断言与审查报告逐字一致：
`lazy process consulted synchronous font path before construction: calls=1
result={Code:1 … Steps:[validate theme observer exit]}` 与
`stop watcher called Perform while Event was still initializing driver`；
健康对照 `r27c-r1-healthy-controls` rc=0。

F-C1：`RunSingleWindowProcess` 旧实现无条件先调 `resolveProcessTheme`
（同步默认 Provider），有效 Lazy 也被无关 Provider 失败在 `NewLazyApp`
之前截断。改为**先按主题来源分流**：Lazy 走 `NewLazyApp`，不读/不等同步
Provider；同步默认与显式 Theme 路径 B6 不变（开窗前失败仍返回错误）；
显式 ThemeErr 两条路径都保留。Strict true 仍由 `App.fontFailure()` 交出
`contracts.ErrFontNotFound`。分流策略（Lazy 优先于显式 Theme）是待裁决候选。

F-C2：Gio v0.10.3 首次 `Window.Event()` 内部 `w.init()`，Windows 后端在另一
goroutine 里 `callbacks.SetDriver`；此前 `watchWindowStop` 直接 `Perform` 与之
竞争。修复把 watcher 启动**推迟到首次 Event() 返回之后**（driver 就绪），
首次事件即 Destroy 时根本不启动（Destroy 优先）。提前 stop 不丢、就绪后
有界执行一次 ActionClose、回调一次、错误透传、无遗留 watcher；
`watchWindowStop` 三参数签名与 `TestWindowStopWatcherLifecycle` 原断言不变。
不用 sleep / busy wait / 改 Gio 缓存 / 挪 Main。

F-C3 **事实更正**：R27-C 原批次默认选择器**确实**通过 `RunWindow*` /
`a.Run()` 进入过真实 `Window.Event` → Win32 init → `callbacks.SetDriver`
（旧 `r27c-c3-race` 首败栈即证据）。旧措辞「未执行任何原生路径」不成立；
窗口可见性、渲染效果、原生退出仍**未验证**，不升级为验收通过，也不追加
作者原生执行。旧 delivery/raw 与首败一字不改。
修复：`app.go` 增加**包内** `windowEventSource` 注入点（非公开 API，
生产默认值仍是 `a.w.Event`）；默认测试集合由
`controlled_window_test.go`（`//go:build !nativewindow`）的 `TestMain`
换成受控事件源；真实窗口测试按原名原断言移到
`native_window_test.go` / `state_e2e_cases_test.go`（`//go:build nativewindow`），
默认新增等价受控回归。冻结 `font_lazy_strict_test.go` 不改。

## 判别力与门禁

两条针对性可编译变异 M1（重引同步 Provider 前置）/ M2（重引初始化期未就绪
Perform）在**同一守卫** `^Test(CoordR27|R1)` 上真实翻红（先 `go build` rc=0
证明可编译），按**字节 SHA-256** 恢复后同一守卫转绿：
`process_entry.go b0f0a70f…`、`app.go edd58fdf…`。

当前源码态门禁：无窗口 app+theme 完整回归 **290 PASS / 0 FAIL / 1 既有 SKIP**
（`TestNightM1CollectSample`，无采样授权），app 覆盖 **90.5%**、theme **86.7%**
（均 ≥85%）；必要测试集合 42 PASS；vet、CGO=0 全量 build、gofmt 空输出、
原生标签集合仅编译 + vet、diagdoc 全 rc=0。
**全量 race（不使用 -skip）rc=0、290 PASS、0 DATA RACE**，专项 race 13 PASS
0 DATA RACE —— F-C2 的基线竞态已在本封装层闭合；真实 native race 仍留
协调者授权阶段。冻结态 docgate 见交付 `closure-matrix.md`。

`verify-evidence.py` 记录完整性 PASS 只说明形态/自洽/原始双通道/锁释放，
不等于业务全绿；台账里 `r27c-r1-s1-probes-green`（F-C2 未修时联合探针 rc=1）
与 `r27c-r1-s2-probes-green`（`theme.LoadIdle` 编译失败）两条
`business.pass=false` 是真实首败，保留不改写；另有
`r27c-r1-s1-fc1-probe-green` 因公共锁被 R27-B 返修线持有而被记录器拒绝，
**未启动任何子进程**，按纪律不删他人锁、不重试到绿。

## 未验证与候选边界

原生窗口、真实 Gio premature close、跨平台、多窗口、真实字体探测失败均未
验证；作者返修阶段不开新原生窗口、不安装、不采样、不跑用户 main/init。
三层退出仍分开：库函数返回 / 事件循环结束 / 整个进程退出；Windows 真实
`Main` 下通用 `(*App).Run` 仍不可返回；BUG-028 保持 Open；
`RunSingleWindowProcess` 新公共 API、进程退出策略、Lazy/Theme 组合策略、
`ProcessOptions` 自检通道与 `Steps` 收敛方案（交付 `api-convergence.md`）
**全部是候选，不替负责人批准**。主库全程只读，未合主库、未 push。
