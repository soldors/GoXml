# 显式字体选面：集成断点

基线为 `27b2e1905828bda48a4f7f448d6ade68af2a4629`。DeepSeek 实现 R1.1，经 ZCode 修复差异复审后，六份固定源码已在 `codex/night-font-integration` 集成验证。主仓库已有未提交内容不得覆盖；本轮未推送，也未升级 FONT ACCEPT/WBS 正式状态。

实现入口是 `theme.LoadSelectedFaces` 和 `theme.NewLazyWithSelection`。后者接受源与索引，在既有 worker 内执行读取、解析和最终主题构建，App 继续通过原 pending/Take 链在帧内交换。默认 NewLazy/Provider 路径保持原行为。

同步调用取得的 `SelectedFaces` 可用于显式 NewApp；异步调用将 `NewLazyWithSelection` 的结果交给 `app.NewLazyApp`。不得先同步加载，再用 LazyOptions.Temp 冒充异步最终加载。Path 每次显式失败重试重新读取；Bytes 在构造时复制冻结，Ready 后释放源快照。字体对象仍可持有表级缓冲，尚无常驻内存预算结论。

两项修复已闭合：普通缺表错误为 parse，真正 recover 的解析器 panic 为 parser-panic；Lazy.Report 每次读取复制自有 SelectionError 与诊断切片，默认路径不经过新增快照分支。结构化错误仍支持 errors.Is(ErrFontNotFound) 与 errors.As(*SelectionError)。

集成验证：全仓测试及覆盖率、全仓 vet、Windows amd64/arm64 构建、gofmt 与 diff 检查通过。新 facesel.go/lazy_facesel.go 语句覆盖率分别为 86.2%/85.6%。没有 race、跨平台运行、真实窗口或生产性能通过结论。

真实窗口探针已收口至 DeepSeek WINDOW-PROBE-R1.2 三文件固定候选，沿用独立 `face-selection-window-v1`，旧 font-init-v1 守卫不变。协调者复跑 28 顶层/71 子测试、vet/build 通过；M1 直接取 App.FirstFrame.CreatedAt/FirstFrameAt，加载失败分类、测量标志、原始错误单行编码和可复算端点已经发射。

下一步完成 collector 的最终冻结门禁和来源绑定后再安排真实窗口 smoke，不能仅把 FROZEN 改为 true 就跑正式采样。R2 的 115 项无窗口自检已复跑通过；作者成功 fixture 缺构造完成时刻，协调者重新生成并逐条先做 producer.validate 的 8 份 synthetic fixtures 可作为有效对照，未冒称真实窗口。R2 原始命令台账与 manifest 不一致需保留证据并登记更正。

生产六文件保持实现提交 `97c0f7b645f1b2bb596df8382fd4693cc09e8d11` 的哈希；该提交已在 dev-log 登记。主仓库仍保留既有未提交内容，尚未合入显式选面实现。没有真实窗口/M1性能/240样本/race/跨平台运行/RSS结论，不升级正式验收。

本机证据：`D:/Codes/Goxml-agent-rounds/2026-10-27/face-selection/window-final-validation/coordinator/`；后续以该处复核和最终 source snapshot 为准。提交逐次临时覆盖空 hooksPath，禁止 push。用户当前手工派发 Agent，协调者不自行发送 GUI 消息。
