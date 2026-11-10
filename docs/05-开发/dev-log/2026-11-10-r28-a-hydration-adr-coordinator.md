# 2026-11-10 R28-A HYDRATION-ADR-COORDINATOR 开发日志

基线为组合分支最终提交 `474dfb2a047ab19bc56a6093b13c32f7a0bbd204`，本线分支 `codex/r28-a-hydration-adr` 从该 SHA 新建，工作树开工时 clean。本轮是文档/ADR 收口，不修改生产 Go、测试、公共 CLI、共享索引或 CHANGELOG，不开原生窗口，不推送、不合主库。

## 负责人裁决落地

本轮新增 `ADR-0016-Hydration开发态跨进程状态恢复范围.md`，并在 `TASK-DEVX-001` 追加 R28 范围补记。文档明确：

- 开发态 Windows 首发使用显式回环 TCP `127.0.0.1:0` 与显式 DTO Envelope；不反射扫描任意 VM。
- Hydration 显式 opt-in、默认关闭；未启用时无监听、线程和额外状态 I/O。
- `.goxml/hydration/<session>.state`、payload/Envelope 大小、超时与并发限制写成开发态推荐默认；数值、清理与发布策略仍可配置/待验收。
- `ServiceObserver.ServingGeneration()` 与请求 `ServingGen`、响应 `Gen`、客户端旧代校验使用同一 Watch 编号域。错配返回 `ErrMismatch` 且不返回或接受 payload。
- token 只用于会话绑定、请求关联和重放拒绝，不视为认证/授权。
- 明确措辞：**负责人已批准开发态 Windows 首发范围，原生/跨平台发布等级待验收。**

## 边界与未完成项

本轮没有把候选传输/API/默认参数写成通用发布合同。CLI 自动恢复参数、最终公开 API 命名、正式限制数值、认证/加密、状态保留与迁移、原生窗口/IME、多窗口、Linux/macOS 运行和正式发布门禁仍未完成。共享 `docs/00-文档索引.md`、CHANGELOG 与跨线总览由协调者在三线合并后统一更新，避免并行冲突。

## 证据口径

generation 双侧守卫及历史红绿证据沿用组合基线与既有 R28-A 交付，不重写历史 raw。当前提交仅包含本 ADR、TASK-DEVX-001 补记和本 dev-log；门禁只验证文档链接、诊断文档与工作树完整性。
