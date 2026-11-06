# R25-B 最终组合提案（待负责人批准）

组合父基线 `76f0669`，作者 candidate `7375468`、协调者补正 `e3ecfd6`。
本分支只用于最终测试与可审查批准，未批准前不合主库、不推送。

基础范围拟接纳：Row/Column/Page 流中的单子节点 Expanded，缺省权重 1，
自身显式尺寸优先，子节点尺寸保留；If/For/Def/Slot 走已有规则。
Grid/显式权重/自身新装饰不在本次批准范围，继续明确拒绝。
输出契约 §4.9 与 CHANGELOG 的补丁属于本提案，语言规范用户 WIP 未改。

组合时统一将 generatorOutputRevision 从 gen-3 提至 gen-4，K4 一次性失效，
缓存格式 v8 与语言版本 1 不变。专属回归以 gen-3 真实热缓存验证安全未命中，
再验证字节/mtime/Manifest 真命中；旧 R12 失败保旧守卫继续保留。

技术证据包含恢复遗漏探针、拒绝内部 benchmark 失败、匹配完整页面与轮询的
Layout-only/Frame 分开对照、真实无界阈值及渲染缓存断言。R25-R3 作者原始数字
只作历史记录，不用带 Source 却不提交 Frame 的累积状态作稳定帧性能依据。

原始证据与最终门禁在外部 `product-r25/coordinator/review-repair-r3/`。
未运行原生窗口或真实显示器采样，不扩大跨平台与性能全局结论。
主库 9 项 WIP 保持原字节，A/C 发布不回滚；Router 预算、ThemeRef 与字体未触碰。
