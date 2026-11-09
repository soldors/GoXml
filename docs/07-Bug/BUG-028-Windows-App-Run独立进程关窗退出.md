# BUG-028：Windows App.Run 独立进程关窗后的退出策略

- 发现：2026-11-08，R26 原生 Preview 验收。
- 状态：Open（限定替代路径已解决）。R27 显式单窗入口已批准，API 收敛/无窗口/race和本机原生退出通过；旧 App.Run、跨平台及多窗口的原边界未因此全局关闭。
- 平台：本机 Windows/amd64、Go 1.26、Gio 0.10.3；不外推其它平台。

## 实证与边界

旧预览宿主直接 `app.Run(v, cfg)`，窗口显示、最大化重绘正常，标题栏关闭后
退出码 2。直接执行同一宿主并保留 stderr，得到 Go `all goroutines are asleep`
诊断，栈停在 Gio `osMain` 的无限阻塞与 Goxml `App.Run`。公共 Preview 启动器当时
未转发宿主 stderr，导致只看到非零退出码；现已补转发。

该证据只证明旧独立宿主的退出形态有问题，不能推断所有 App.Run 调用方均失败。
有显式进程退出策略的应用、集成入口、尚有其它 goroutine 的进程条件不同。
未改字体冻结文件、App 公共签名、生产事件循环或多窗口策略。

## 本轮处置

Preview 是自有单窗子进程，采用标准 Gio 生命周期：主线程执行 Gio Main，窗口事件
goroutine 在 DestroyEvent 后退出该宿主；正常退出 0，原生错误保留并退出 1。
仍使用同源 runtime View/字体策略与已校验生成视图，不运行用户业务代码。
真实公共 CLI 复验：显示、最大化、标题栏关闭、rc0、activeAfter0、目录清理均成立。
生成宿主本体另有假事件源测试，验证 Frame/Layout 一次及 DestroyEvent.Err 透传。

Widgets/Watch 自有场景在销毁回调中明确结束单窗进程；这属于测试场景退出策略，
不能冒充底座 App.Run 返回行为已被验证或修复。

## 后续

2026-11-09：新增显式 `RunSingleWindowProcess`，仅调用它的单窗 main 让出进程所有权。公开选项限 Theme/Lazy/Observer，两种主题同时给定拒绝；销毁和严格字体原因合并保留，回调/清理在 Observer 与退出之前。测试注入及步骤字段收为包内，四种真实 console 退出断言保留。DataGrid 原生标题栏关闭 rc0；无桌面控制的普通/显式失败 Provider 场景在真实帧后向自有窗口发正常关闭请求，rc0/1、回调清理标志及严格诊断均正确，零 DATA RACE/残留。曾被 Escape 中断的旧尝试不算通过。自动场景未做人工视觉、未伪装系统字体真实故障；旧 App.Run 不隐式 os.Exit，多窗口与 Windows Main 不返回的边界不变。见 [R27 组合验收](../05-开发/2026-11-09-R27-基础范围与组合验收.md)。

需要按 App.Run 的跨平台/单窗/多窗契约单独决定进程退出与返回语义，不应在通用库
偷偷加入 os.Exit。保留旧路径/旧 stderr/原始 rc，不能把 Preview 的局部修复关闭此项。
