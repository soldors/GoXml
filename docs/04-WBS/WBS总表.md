# WBS 总表

| Task ID | 模块 | 标题 | 依赖 | 核心验收命令 |
|---|---|---|---|---|
| TASK-DOC-BASELINE-001 | docs | 批次 0 文档冻结与基线建立 | 范围与退出门槛确认（已完成） | 0A 完成；0B 进行中，0D 门禁校准已推进；见任务卡及 B0 清单 |
| TASK-B0-ADR-CONFLICT-001 | docs/architecture | 校准 AOT Diff 设计储备的追踪链 | - | 决策已由 `f5c45bd` 固化；待全仓表述复核，见任务卡与 B0-F01 |
| TASK-CONTRACT-001 | contracts | 定义编译器 IR 与运行时核心接口契约 | - | go test -v ./contracts/... |
| TASK-PARSER-001 | parser | 实现带行列号的 GXML 语法分析与控制流解析 | TASK-CONTRACT-001 | go test -v ./internal/compiler/parser/... |
| TASK-PARSER-002 | parser | 实现强类型属性解析器 (EdgeInsets/Color/Binding) | TASK-PARSER-001 | go test -v ./internal/compiler/parser/... |
| TASK-EXPANDER-001 | expander | 实现组件 Schema 分类、属性事件区分与 Def/Slot 展开 | TASK-PARSER-002 | go test -v ./internal/compiler/expander/... |
| TASK-ANALYZER-001 | analyzer | 实现语义校验（绑定路径、事件签名、For 作用域） | TASK-EXPANDER-001 | go test -v ./internal/compiler/analyzer/... |
| TASK-CODEGEN-001 | codegen | 实现 Gio 布局（含 Align/Justify/Gap/尺寸/Grid）与控件代码生成器 | TASK-ANALYZER-001 | go test -v ./internal/compiler/codegen/... |
| TASK-BINDING-CONTRACT-001 | codegen | 核查绑定现状并冻结绑定语义契约 | TASK-ANALYZER-001、TASK-CODEGEN-001、TASK-CODEGEN-STATIC-CACHE-001 | 文档审查；`go test -count=1 ./internal/docgate` |
| TASK-BINDING-IMPLEMENT-001 | codegen | 接通 Analyzer Facts 到绑定生成与运行时读取 | TASK-CODEGEN-001、TASK-BINDING-CONTRACT-001 | 直接 Codegen 和运行时读取已验；Pipeline `ModulePath` 集成仍开放 |
| TASK-BINDING-IMPLEMENT-002 | codegen | 绑定运行时读取与显式重新布局验收 | TASK-BINDING-IMPLEMENT-001 | go test -count=1 -run '^TestBindingRuntime' ./internal/compiler/codegen/ |
| TASK-BINDING-IMPLEMENT-003 | codegen | 绑定显示缓存与 shaping 观测验证 | TASK-BINDING-IMPLEMENT-002 | 显示缓存已实现；契约场景③/⑤和 Gio shaping 证据未闭环，见验收报告 |
| TASK-CODEGEN-STATIC-CACHE-001 | codegen | 实现静态子树固化（`op.Record`/`op.CallOp`，关闭契约 §15.1 缺口）| TASK-CODEGEN-001 | go test -bench BenchmarkStaticSubtree ./internal/compiler/codegen/ |
| TASK-ORCH-001 | pipeline | 实现编译编排层与失败门禁（关闭 Codegen 契约 §13 第 20 条） | TASK-CODEGEN-001 | go test -v ./internal/compiler/pipeline/ |
| TASK-GENERATE-CLI-001 | cli/generate | 实现 Generate CLI、Manifest 与输出清理边界 | TASK-CODEGEN-001、TASK-ORCH-001、TASK-SEC-001 | go test -v ./cmd/goxml/... |
| TASK-INPUT-001 | compiler/input | Input 控件契约阶段：现状审计、语义裁决、输出契约冻结（先契约后实现） | TASK-STATE-001、TASK-BINDING-IMPLEMENT-001、TASK-GENERATE-CLI-001 | go test ./internal/cli/ ./internal/compiler/codegen/ |
| TASK-DEMO-001 | runtime/app | 最小可运行窗口：管线产物接入 Gio 主循环 | TASK-ORCH-001 | go test -v ./pkg/runtime/app/ |
| TASK-SEC-001 | 安全 | 威胁建模与安全边界审查（**须在 Codegen 接入前完成**） | TASK-CODEGEN-001 | 见任务卡 |
| TASK-STATE-001 | codegen/runtime | 状态更新与 UI 自动刷新（AOT Diff 降为 S5 评估项） | TASK-CODEGEN-001 | 见 `TASK-STATE-001` 分阶段验收 |
| TASK-STATE-IMPLEMENT-001 | codegen/runtime | S2 最小状态通知机制与统一绑定 View 接口 | TASK-STATE-001、ADR-0010 | `f5c45bd` 已归档生成构造函数 nil 回归；定向和 Codegen 全包测试通过 |
| TASK-STYLE-001 | macro/theme | 实现强类型 Theme 与组件宏展开 | TASK-CODEGEN-001 | go test -v ./internal/compiler/macro/... ./pkg/runtime/theme/... |
| TASK-FONT-001 | theme/font | 实现 CJK 字体探测、嵌入兜底、fallback 链与 IME 验收 | TASK-STYLE-001 | go test -v ./pkg/runtime/theme/... |
| TASK-ROUTER-001 | runtime | 实现单窗口路由、MacroOp 转场动画与 OSWindow | TASK-STATE-001 | go test -v ./pkg/runtime/router/... |
| TASK-LIFE-001 | runtime | 实现 Context 生命周期与 //go:embed 资产管线 | TASK-ROUTER-001 | go test -v -race ./pkg/runtime/lifecycle/... |
| TASK-DEVX-001 | cli/watch | 实现双进程防抖热重启与状态 Hydration | TASK-LIFE-001 | go test -v -race ./cmd/goxml/watch/... ./pkg/runtime/hydration/... |
| TASK-DEVX-002 | cli/preview | 实现独立毫秒级静态 UI 预览器 | TASK-STYLE-001 | go test -v ./cmd/goxml/preview/... |
| TASK-OS-001 | platform | 实现 Windows Zero-CGO 剪贴板与异步原生文件弹窗 | TASK-LIFE-001 | Windows: CGO_ENABLED=0 go test -v ./pkg/platform/clipboard/... ./pkg/platform/dialog/... |
| TASK-OS-002 | platform | 实现系统托盘与系统菜单栏（Windows 纯 Go，Linux/macOS 允许 CGO） | TASK-OS-001 | make build-windows && make build-linux |
| TASK-WIDGET-001 | widgets | 构建标准自绘组件库 (ContextMenu/DataGrid) | TASK-STYLE-001 | go test -v ./pkg/widgets/... |
| TASK-BENCH-001 | bench | 性能基准测试与旗舰 Demo 验收 | 全部前置任务 | go test -bench . -benchmem ./benchmarks/... |

## 开发流程与契约治理

本表只列**交付物与依赖**，不规定执行顺序。实际推进方式是契约驱动，四条规则如下。

### 规则 1：契约先行

涉及以下任一改动的，**先更新契约与对应测试，再写实现**：

- 跨阶段的数据结构（`contracts` 类型、语义 IR 字段）
- 诊断码的语义或编号
- 语言语法或已冻结的行为语义
- 阶段之间的职责边界

理由：这类改动一旦先落代码，后续会被实现细节反向绑架——契约变成对已写代码的追认，而不是设计。

### 规则 2：实现分步

一个大任务可拆成多个可独立验收的步骤。

- **WBS 负责列出交付物**（做什么）；
- **任务卡负责记录实际进度**（做到哪了、还剩什么）。

因此 WBS 的顺序与实际执行顺序可以不同。例如 `TASK-ANALYZER-001` 实际拆成五步，且先做类型加载再做绑定解析——因为后者依赖前者。

### 规则 3：状态一致

WBS、任务卡、契约状态、开发日志与实际代码必须能相互对应。发现偏差**及时修正**，不要留在那里等下次审计。

已发生的漂移案例（均已修正）：

| 漂移 | 表现 |
|---|---|
| 契约状态过期 | 文档索引标「Q7 待确认」，而 Q7 早已冻结 |
| 任务卡滞后 | `TASK-PARSER-002` 标 Todo，但属性解析已实现且有测试 |
| 缺卡 | `TASK-EXPANDER-001` 在 WBS 中却无任务卡 |
| 文件损坏 | `测试报告.md` 每个字符前被插入 `\| 性能 \|`，且在首次提交前就已损坏 |

### 规则 4：证据闭环

「通过」必须有真实的测试或检查结果支持。

**未验证项必须明确标记**，不得因接口存在或单元测试通过就推断整条管线已验证。

### 两条容易产生误解的区别

| 说法 | 实际含义 |
|---|---|
| **契约冻结 ≠ 实现完成** | 契约描述「应当如何」，实现是「是否做到了」。当前 Analyzer 诊断码已全部有发射点，但 REQ-002 至 REQ-006 仍有未实现能力，状态以需求清单和任务卡为准 |
| **单元测试通过 ≠ 端到端门禁已验证** | Codegen 的单元测试证明「能生成可编译代码」，但不证明「管线失败时不产生产物」——后者需要编排层的真实调用链测试（`TASK-ORCH-001`）。两者证据不可互替；`StatusOK` 也不代表生成结果正确 |

这两条正是本项目最容易产生状态漂移的地方：把「设计已定」误读成「功能可用」，或把「组件测试通过」误读成「管线可用」。

## 说明

- `TASK-FONT-001` 为本次补充新增：此前 WBS 缺失字体与 IME 相关工作项，而 Gio 默认字体不含 CJK 字形（实测 `GlyphIndex('中') == 0`），中文界面会渲染为空白。依据见 `docs/03-架构/字体与文本渲染策略.md`。
- `TASK-SEC-001` 为本次补充新增：框架会加载 Go 类型并执行代码生成，需要明确信任边界与输入审查，详见任务卡。
- 性能相关不变量与阈值见 `docs/03-架构/性能预算与优化策略.md`；列表虚拟化与静态子树固化的决策见 `ADR-0006`。

### 待补文档（未建卡，先登记以免遗忘）

| 文档 | 触发条件 | 说明 |
|---|---|---|
| ~~语法速查表~~ | ✅ 已于 2026-10-19 完成 | 见 `docs/03-架构/语法速查表.md`；规范是「定义」，速查表是「手册」 |
| ~~安全边界与威胁模型~~ | ✅ 已于 2026-10-19 完成 | 见 `docs/03-架构/安全边界与威胁模型.md`；原前置条件曾被绕过（`TASK-SEC-001` §1.1） |
| ~~复盘报告~~ | ✅ 已于 2026-10-19 完成 | 见 `docs/11-复盘/复盘报告.md`；记录了契约治理、门禁自证、状态漂移、并发写入与安全审查五条经验 |

三项均已完成。后续新增文档仍按同一原则处理：**先登记触发条件，条件满足再写**，避免为「看起来完整」而提前填充。
