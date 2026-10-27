# 显式字体选面：集成断点

基线为 `27b2e1905828bda48a4f7f448d6ade68af2a4629`。DeepSeek 实现 R1.1，经 ZCode 修复差异复审后，六份固定源码已在 `codex/night-font-integration` 集成验证。主仓库已有未提交内容不得覆盖；本轮未推送，也未升级 FONT ACCEPT/WBS 正式状态。

实现入口是 `theme.LoadSelectedFaces` 和 `theme.NewLazyWithSelection`。后者接受源与索引，在既有 worker 内执行读取、解析和最终主题构建，App 继续通过原 pending/Take 链在帧内交换。默认 NewLazy/Provider 路径保持原行为。

同步调用取得的 `SelectedFaces` 可用于显式 NewApp；异步调用将 `NewLazyWithSelection` 的结果交给 `app.NewLazyApp`。不得先同步加载，再用 LazyOptions.Temp 冒充异步最终加载。Path 每次显式失败重试重新读取；Bytes 在构造时复制冻结，Ready 后释放源快照。字体对象仍可持有表级缓冲，尚无常驻内存预算结论。

两项修复已闭合：普通缺表错误为 parse，真正 recover 的解析器 panic 为 parser-panic；Lazy.Report 每次读取复制自有 SelectionError 与诊断切片，默认路径不经过新增快照分支。结构化错误仍支持 errors.Is(ErrFontNotFound) 与 errors.As(*SelectionError)。

集成验证：全仓测试及覆盖率、全仓 vet、Windows amd64/arm64 构建、gofmt 与 diff 检查通过。新 facesel.go/lazy_facesel.go 语句覆盖率分别为 86.2%/85.6%。没有 race、跨平台运行、真实窗口或生产性能通过结论。

下一步由 DeepSeek 扩展既有 `pkg/runtime/app/testdata/m1nightprobe`，新增独立 `face-selection-window-v1` 协议；原 font-init-v1 强制 ProbeRan=true，不适用于合法无 Probe 的显式选面路径，原守卫不得放宽。工具通过复核后，由 WorkBuddy 执行真实窗口 smoke，再按固定资源、独立进程、随机配对、环境分层采样。首帧、Ready、帧内最终交接均需实际观测；headless parseTotal 的收益不替代 M1 或进程启动指标。

本机证据与任务书：`D:/Codes/Goxml-agent-rounds/2026-10-27/face-selection/production-integration-r1/coordinator/`。下一任务书为 `deepseek-window-probe-prompt.md`；历史交付在同根目录 production-impl-r1.1 下。后续提交需在 dev-log 登记本次已经产生的实现提交号，避免旧提交离开 HEAD 豁免后触发 docgate。提交必须逐次覆盖为空 hooksPath，防止环境钩子推送。
