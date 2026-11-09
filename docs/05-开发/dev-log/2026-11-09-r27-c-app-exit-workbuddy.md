# R27-C APP-EXIT-CONTRACT（WorkBuddy）

日期：2026-11-09。固定基线 `d6b1fc063bb5671ab6340181cd8b16160c5b676c`，
分支 `codex/r27-workbuddy-app-exit`，独立树
`C:/Users/admin/.codex/worktrees/r27-workbuddy-app-exit/Goxml`。主库全程只读，
本线未合主库、未 push。交付在 `D:/Codes/Goxml-agent-rounds/2026-11-09/product-r27/workbuddy/delivery/`。

本线三个提交（真实 SHA，供门禁登记）：

- C1 `c26f51c511e90ed7a0b07ee74e9503f7441c2acb`：三层退出分离、入口矩阵、方案比较与三条受控实证探针。
- C2 `7da6b45325e4080a98e6800982d1c1b4fcfed360`：交出 `DestroyEvent.Err`、并存错误模型、显式单窗进程入口与 console helper。
- C3 `c4a675324291465723f6cdfc66846c28ec2c791a`：三条行为变异、完整门禁、原生验收包与 C1 探针竞态修复。
- 本文件所在的日志维护提交为冻结 HEAD；门禁按设计排除 HEAD，故不在此自引用。

## 契约结论

库函数返回、事件循环结束、整个进程退出三层不能互相冒充。Gio v0.10.3 的
Windows `osMain()` 是 `select {}`，`app.Main()` 永不返回，因此 `(*App).Run`
的 `return` 在生产形态不可达；注入空 `mainFunc` 的测试只证明控制形态，
不是原生证据。R26 Preview 专用宿主的 rc2 只证明该宿主退出形态有问题，
不能推断所有 `App.Run` 调用方都失败，Preview 的局部修复也不关闭 BUG-028。

方案 A（契约内 ready 候选）：`loopEvents` 与 `runWindowEvents` 把
`DestroyEvent.Err` 交出，`WindowExitError`/`ErrWindowDestroy` 可双判别，
`combineExitErrors` 用 `errors.Join` 保留窗口原因与严格字体诊断；不改
`Main` 位置、不引入 `os.Exit`。方案 B（新策略/新公共 API，candidate）：
opt-in `RunSingleWindowProcess`，只有显式调用它的 main 才让出终止整个进程的
所有权；通用 `Run`/`RunWith`/`(*App).Run`/`RunWindow*` 一律不接该入口。
两者技术完成，但**新公共语义与进程退出策略未获批准**，不写成已发布。

## 判别力与门禁

C2 先立红探针（首败 rc=1 原件保留）：`DestroyEvent.Err 未交出：err=<nil>`；
修复后 `r27c-c2-suite` rc=0。C3 三条可编译行为变异（丢 Destroy 原因、
跳过销毁回调、在证据写出前提前以 rc0 退出）在同一守卫上真实翻红，
每条先 `go build` rc=0 证明可编译，再按字节 SHA-256 恢复、同一守卫转绿；
不删健康检查、不改生产退化开关、不改 Skip。

完整门禁：C3 修复后全量 app+theme `279 PASS / 0 FAIL / 1 SKIP`；
app 覆盖 `91.1%`、theme `86.7%`（基线 app 为 `97.9%`，下降来自新增生产分支
在无窗口受控测试中不可达、分母变大；不混源码版本 profile）；
CGO=0 全量 build、vet、gofmt（空输出）、带标签测试仅编译、默认选择器不含
原生测试、diagdoc 均通过。原生验收二进制只构建（SHA 见交付
`native-acceptance.md`），**作者未执行任何原生窗口**。

race 有两处真实首败，均保留不改写：全量 app+theme race 检出既有 Gio 窗口
初始化竞态（`watchWindowStop` → `w.Perform` vs `callbacks.SetDriver`），
在只读导出的冻结基线树上重放同一用例得到同一栈，确认非本线引入；
本线 C1 探针自身的 `mainFunc` 全局注入竞态则是本轮引入的测试缺陷，
已改为局部捕获注入值 + cleanup 显式等待 goroutine 收口，行为判据不变，
复跑转绿。排除已证既有用例后的全量 race 0 DATA RACE。**既有竞态仍 Open**，
交协调者裁决，不自行豁免、不把 rc=1 压成通过。

`verify-evidence.py` 记录完整性 PASS 只说明形态/自洽/原始双通道/锁释放，
不等于业务全绿；台账里两条 `business.pass=false` 是真实首败。

## 交付与边界

`integration-proposal.diff` 覆盖 6 份共享正式文档的最小措辞/影响登记，
`git apply --check` rc=0、**未应用**；正式正文、ADR 决策、BUG-028 状态行
未直接改。冻结 `font_lazy_strict_test.go`、字体包/策略/时钟/采样器、
公共 CLI/Compiler/依赖/共享进程助手均未改。

未验证：原生窗口显示与标题栏关闭、真实 Gio premature close、跨平台、
多窗口、真实字体探测失败；不执行用户业务 main/init、不安装、不真实采样。
原生组合验收与方案 B 语义裁决由协调者完成；技术自测通过不自动批准新公共
语义，BUG-028 保持 Open。五态、冻结 SHA 与最小复跑入口见交付 `handoff.md`、
`slices.json`、`resume.md`、`closure-matrix.md`、`known-boundaries.md`。
