# ADR-0016：Hydration 开发态跨进程状态恢复范围

- 状态：已批准（开发态 Windows 首发范围；原生与跨平台发布等级待验收）
- 冻结日期：2026-11-10
- 关联需求：TASK-DEVX-001-3、REQ-005
- 关联文档：[ADR-0004-双进程 Live Reload 与独立预览器方案](ADR-0004-双进程Live-Reload与独立预览器方案.md)、[运行时线程模型](../运行时线程模型.md)、[TASK-DEVX-001](../../04-WBS/任务卡/TASK-DEVX-001.md)

## 1. 背景

R27 的显式 DTO、Watch opt-in 与原子状态保存已经具备无窗口技术证据。R28 补充了旧代与新代之间的回环通道，以及请求方、服务方和客户端之间的 generation 关联合同。此前材料将传输方式、状态目录和限制写成候选，容易把开发态范围误读成已批准的发布合同。本 ADR 只冻结负责人已批准的开发态 Windows 首发范围，不宣称原生窗口、跨平台运行或最终公开 API 已完成。

## 2. 决策

### 2.1 传输与端点

1. 开发态首发使用显式的回环 TCP 端点 `127.0.0.1:0`。端口由系统分配，端点信息只通过显式启动参数或受控的临时文件传递。
2. 默认关闭时不得监听端口、创建传输线程或执行额外状态 I/O；未启用 Hydration 的应用保持既有行为。
3. 端点只用于同一开发会话内的状态捕获/应用协作，不向公共网络暴露；TCP 可达不等于身份认证。

### 2.2 数据合同

1. 跨进程交换使用显式 DTO `Envelope`，包含 version、schema、appId、sessionId、generation、checksum 和大小边界等字段。
2. 不通过反射扫描任意 ViewModel，不把 Gio 操作对象、平台句柄、指针或 goroutine 放入 payload。
3. 新进程必须在 owner 串行点完成完整校验后再 Apply；校验失败、取消、过期或无状态时不得部分应用。

### 2.3 默认策略与状态目录

1. Hydration 是显式 opt-in，默认关闭；Watch 的 Hook、端点和恢复均不得隐式开启。
2. 开发态推荐状态目录为 `.goxml/hydration/<session>.state`。写入采用临时文件、刷新并原子替换，且必须证明当前 session 的所有权；非本会话既有文件不得被覆盖。
3. payload、Envelope、捕获超时和在途捕获数量均为可配置限制。当前限制是开发态推荐默认，不构成跨平台或发布级固定合同；正式数值、保留周期与清理策略仍需发布验收裁决。

### 2.4 Generation 关联合同

1. Watch 的 `ServiceObserver.ServingGeneration()` 是服务身份的来源；Hook、请求 `ServingGen`、端点响应 `Gen` 与客户端校验必须使用同一 Watch generation 编号域。
2. 端点实际 owner 捕获代次与请求 `ServingGen` 不一致时，返回 `ErrMismatch` 且不返回 payload。
3. 客户端收到的成功响应 `Gen` 与请求的旧代次不一致时，返回 `ErrMismatch` 且不接受 payload。适配器跨命名空间时必须在边界显式映射编号，不得猜测或静默转换。
4. generation 校验是会话一致性和过期保护，不是认证机制；它不能替代 token/session/schema/app 绑定与 checksum 校验。

### 2.5 Token 的边界

Token 仅用于会话绑定、请求关联和重放拒绝。Token 不视为认证、授权、加密或公网安全边界；需要这些能力时必须另立安全设计与 ADR。

## 3. 决策矩阵

| 项目 | 采用范围 | 拒绝的替代方案 | 兼容性/安全边界 | 仍需裁决 |
|---|---|---|---|---|
| 传输 | `127.0.0.1:0` 显式 TCP | 公共监听、隐式端口、平台专用管道 | 默认关闭；只允许开发会话回环通道 | 发布级认证、加密与跨平台实现 |
| 数据格式 | 显式 Envelope/DTO | 反射序列化任意 VM、裸 JSON 无版本 | 可校验 schema/checksum/大小；不携带运行时句柄 | 最终公开类型命名与版本升级策略 |
| 启用策略 | Hook/端点显式 opt-in | CLI 或应用默认自动恢复 | nil/disabled 无监听、线程和额外 I/O | CLI 参数和产品默认值 |
| 状态位置 | `.goxml/hydration/<session>.state` 推荐 | 覆盖用户任意文件、固定全局文件 | 原子替换、session 所有权、失败保旧 | 发布清理、保留周期和迁移 |
| 限制 | payload/Envelope/timeout/并发可配置 | 无界读取、无界在途捕获 | 限制是开发态推荐默认 | 正式阈值与跨平台差异 |
| generation | 共享 Watch 编号域；错配 `ErrMismatch` 且无 payload | 独立计数器、静默接受错代数据 | 防止过期/错服务状态泄露 | 跨产品调度器的映射审计 |
| token | 会话绑定、关联、重放拒绝 | 把 token 当认证或授权 | 不提供公网安全保证 | 需要认证时另立安全 ADR |
| 发布等级 | Windows 开发态首发 | 推断原生/跨平台发布完成 | 原生窗口和跨平台运行仍未验收 | 发布门禁与平台清单 |

## 4. 后果与非目标

- 负责人已批准开发态 Windows 首发范围，原生/跨平台发布等级待验收。
- 默认关闭保证旧应用在不启用 Hydration 时无新增监听、线程、I/O 或恢复顺序变化。
- generation 错配在端点和客户端双侧 fail-closed，避免把旧代或错误服务的 payload 暴露给调用方。
- 本 ADR 不批准 CLI 自动恢复参数、通用 VM 反射、任意用户回调回滚、原生窗口/IME、多窗口协调、Linux/macOS 运行或公网安全。
- `TASK-DEVX-001` 整卡仍为 InProgress；本 ADR 的批准不等于整卡、R27/R28 原生验收或发布验收完成。

## 5. 证据与复核边界

- `internal/devx/hydrationchannel`：回环 Endpoint/Client/Applier、token/session/schema 绑定、generation 双侧守卫。
- `internal/devx/watch`：`ServiceObserver.ServingGeneration()` 与 opt-in Hydration Hook 的代次来源。
- `pkg/runtime/hydration` 与 `internal/devx/hydration`：显式 Envelope、校验、原子保存与失败保旧。
- 既有 R28-A generation closure 证据保留原始失败与修复后结果；本 ADR 不改生产代码、不重写历史报告。
- 当前批准仅覆盖开发态 Windows 组合；原生窗口、跨平台运行、最终 API 收敛和正式性能/安全门禁必须另行验收。
