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
| TASK-CODEGEN-NAMING-001 | codegen | 生成符号命名与包级冲突契约（同包多 `.gxml` 的 `renderKey`/`textCache` 重名）。**已裁决：支持同包多文件 + N1 命名 + 冲突预检零副作用**。**切片状态（C1–C5 均已分别验收/复审通过）**：C1 预检实现起点 `948c406`，2026-10-22 项目负责人验收通过（附非阻塞遗留项，卡 §10.11）——预检在写盘前、只对未命中输入、冲突整批放弃（退出码 2），并完成现状核对与 G1–G8 清单冻结、补 4 条断言、修 1 处 fail-open；C2 类型名按源唯一，验收通过（附非阻塞遗留项，卡 §10.4）；C3 `CODEGEN-NAME-001/002` 独立复审 PASS WITH NON-BLOCKING，发现项已修复；C4 接线完成并获项目负责人最终验收通过（2026-10-22，卡 §10.8；含接线时修复「批内冲突未按目标包分组」）；C5 项目负责人最终验收通过（2026-10-22，基线 `538c2a6`，卡 §10.9.9）——`TextCache` 按 `bindSlots > 0` 收窄（`slots.go` 单一派生 + 前端复用）、A7 补齐、变异 7 组，复审 P1-1/P1-2 补护栏（M8/M9）、形态数 14→13 已更正。**边界：切片全部通过 ≠ 整个分支或项目的最终验收；`master` 整合与最终裁决另计** | TASK-CODEGEN-001、TASK-GENERATE-CACHE-001 | `go test -run '^TestNaming' ./internal/compiler/codegen/`（合并态**实测 29/29 PASS、0 SKIP**）；CLI 侧 `go test -run '^TestNaming' ./internal/cli/`（合并态**实测 12/12 PASS、0 SKIP**）；零副作用验收见 `naming_conflict_test.go`，同包双源编译用例见 `naming_samepkg_test.go`；计数口径＝测试函数数 |
| TASK-ORCH-001 | pipeline | 实现编译编排层与失败门禁（关闭 Codegen 契约 §13 第 20 条） | TASK-CODEGEN-001 | go test -v ./internal/compiler/pipeline/ |
| TASK-GENERATE-CLI-001 | cli/generate | 实现 Generate CLI、Manifest 与输出清理边界 | TASK-CODEGEN-001、TASK-ORCH-001、TASK-SEC-001 | go test -v ./cmd/goxml/... |
| TASK-GENERATE-CACHE-001 | cli/cache | 增量生成与缓存（契约 §11：缓存键、命中条件、失效、原子性、可观测性；**B1–B5 已实现**，42 个缓存用例全绿） | TASK-GENERATE-CLI-001 | go test -run TestCache ./internal/cli/ |
| TASK-INPUT-001 | compiler/input | Input 控件契约阶段：现状审计、语义裁决、输出契约冻结（先契约后实现） | TASK-STATE-001、TASK-BINDING-IMPLEMENT-001、TASK-GENERATE-CLI-001 | go test ./internal/cli/ ./internal/compiler/codegen/ |
| TASK-EVENT-PAYLOAD-001 | compiler/codegen | 事件载荷的通用生成能力（FromPayload 渲染 + 独立测试），先于 Input 事件实现 | TASK-BINDING-IMPLEMENT-001、TASK-CODEGEN-001 | go test -run TestEventPayload ./internal/compiler/codegen/ |
| TASK-INPUT-001-D4-A | compiler/codegen | Input 控件结构与事件接线（Editor 字段、属性透传、控件自持消费、载荷接线） | TASK-EVENT-PAYLOAD-001、TASK-INPUT-001 | go test -run TestInputCodegen ./internal/compiler/codegen/ |
| TASK-INPUT-001-D4-B | compiler/codegen | Input 的 lastApplied 同步算法（两条更新路径 + 逐帧顺序） | TASK-INPUT-001-D4-A | go test -run TestInputSync ./internal/compiler/codegen/ |
| TASK-INPUT-001-D4-C | compiler/codegen | Input 集成验收：同帧冲突运行时验证 + 编译门禁矩阵 + AC 映射 | TASK-INPUT-001-D4-B | go test -run 'TestInputSameFrame\|TestInputCompile' ./internal/compiler/codegen/ |
| TASK-INPUT-001-DEMO | examples/demo | Input 交互样板（手动验收入口，非功能开发） | TASK-INPUT-001-D4-C | go build ./examples/demo/ |
| TASK-INPUT-001-D5 | compiler/codegen | Input 自动化验收（MaxLen/Filter/Submit/改回）+ 人工验收分离 | TASK-INPUT-001-D4-C、TASK-INPUT-001-DEMO | go test -run TestInputAcceptance ./internal/compiler/codegen/ |
| TASK-DEMO-001 | runtime/app | 最小可运行窗口：管线产物接入 Gio 主循环 | TASK-ORCH-001 | go test -v ./pkg/runtime/app/ |
| TASK-SEC-001 | 安全 | 威胁建模与安全边界审查（**须在 Codegen 接入前完成**） | TASK-CODEGEN-001 | 见任务卡 |
| TASK-STATE-001 | codegen/runtime | 状态更新与 UI 自动刷新（AOT Diff 降为 S5 评估项） | TASK-CODEGEN-001 | 见 `TASK-STATE-001` 分阶段验收 |
| TASK-STATE-IMPLEMENT-001 | codegen/runtime | S2 最小状态通知机制与统一绑定 View 接口 | TASK-STATE-001、ADR-0010 | `f5c45bd` 已归档生成构造函数 nil 回归；定向和 Codegen 全包测试通过 |
| TASK-STYLE-001 | macro/theme | 实现强类型 Theme 与组件宏展开（`<Def>`/`<Slot>` 已实现于 `expander`；剩余：Theme + Codegen 主题色渲染） | TASK-CODEGEN-001 | go test ./internal/compiler/expander/ ./internal/compiler/codegen/ |
| TASK-THEME-SEAM-001 | theme/contract | 冻结主题/字体接缝（路线 B）：`FontProvider` 契约、`pkg/runtime/theme` 包边界、主题构建入口、文档一致性门禁（**已完成 6/6**） | TASK-CODEGEN-001、ADR-0012 | go test ./contracts/... ./pkg/runtime/... ./internal/docgate/ |
| TASK-FONT-001-B | theme/font | 嵌入字体兜底：OFL 字体选型 + 构建期子集化 + `//go:embed` 解析 + 兜底选择 + 体积预算 + 关闭 A 的失败语义缺口（**已实现**，6 个验收用例全 PASS；二进制增量 2,182,144 B ≤ 3 MiB） | TASK-FONT-001-A | go test -run TestEmbeddedFallback ./pkg/runtime/theme/... |
| TASK-FONT-001-D-ACCEPT | theme/font | D 整体验收与回归。**最新证据见 ACCEPT 卡第 18 版**：固定快照 eaa8487 已交付分路径下游、普通并发、family 失败隔离与真实窗口 M1 各场景 30 次（合计 60/60 有效）；e4b0612 合并态全仓 rc=0，最终快照无 Go/module 差异。**§5 正式结论维持第 17 版**：03/08 复核完成（08 限 A 案、不构成生产正确性新证据）；07 仍未验证／待独立复核（新增测试不替代原冻结断言修订，成功跨 family 与 race 仍缺）；01/02/04/05/06 仍待正式闭环。**正式验收未完成**，阈值未冻结，限 Windows/amd64 | TASK-FONT-001-D-IMPL-1、TASK-FONT-001-D-IMPL-2、TASK-FONT-001-D-IMPL-3 | 见 ACCEPT 卡 §3 与 §5：`go test -count=1 -v -run 'TestFontLazyLoad' ./pkg/runtime/theme/ ./pkg/runtime/app/`；`go test -count=1 -v -run '^TestFontLazyLoadNoFrameCounting$' ./pkg/runtime/app/`（见 `TASK-APP-FLAKY-001`）；`go test -count=1 -v -run TestAccept07 ./pkg/runtime/theme/` |
| TASK-FONT-001-D-IMPL-3 | theme/font | 诊断、失败终态与严格语义：Report、StrictFontFailure、四指标分离。**实现已本地提交 e7ad530**；abc571d 补 M1 构造入口，2f03384 增加独立下游测试，合并态回归已通过。第 3 版公共契约保持，原冻结断言修订仍待处理；正式验收见 ACCEPT 第 18 版 | TASK-FONT-001-D-IMPL-1、TASK-FONT-001-D-IMPL-2 | go test -run TestFontLazyLoad ./pkg/runtime/theme/ ./pkg/runtime/app/ |
| TASK-FONT-001-D-IMPL-2 | theme/font | 帧内切换与布局一致性：NewLazyApp、仅 FrameEvent 内换 shaper、提交返回代理指标与布局 delta。**实现已本地提交 9f3ab8e**，已有独立克隆 app 复验及合并态回归；正式验收未完成，范围见 ACCEPT 第 18 版 | TASK-FONT-001-D-IMPL-1 | go test -run TestFontLazyLoad ./pkg/runtime/app/ |
| TASK-FONT-001-D-IMPL-1 | theme/font | 异步加载器与状态机：`theme.Lazy`（Unloaded/Loading/Ready/Failed）、`Request` 唯一入口、`SetNotify` 后绑定、Loading 立即返回、失败不自动重试、`Theme()` 无副作用、后台禁用渲染 shaper（AST 门禁）、临时主题并发只构造一次、失败路径 `CompletedAt`、`NotifyLateBound` 自持、临时主题不污染 B4 诊断（**已实现**：17 用例全 PASS，结构门禁反向验证通过；产品路径零 `.Shaper`，`app` 一行未改） | TASK-FONT-001-D | go test -run TestFontLazyLoad ./pkg/runtime/theme/ |
| TASK-FONT-001-D-PROBE | theme/font | 真实窗口异步唤醒验证（D 卡 §10 第 2b 步的唯一执行载体）：首帧前/首帧处理中/静默后/突发合并/关闭期五种情形下 `Window.Invalidate()` 是否可靠产生后续 `FrameEvent`（**已执行，5 phase × 4 次全 PASS**；唤醒可靠、合并允许、最后一次更新必被渲染；帧在途净等待 75.7–105.2 ms；未覆盖窗口最小化与 macOS/X11） | TASK-FONT-001-D | go run ./pkg/runtime/app/testdata/windowprobe |
| TASK-FONT-001-D | theme/font | 懒加载与不阻塞首帧：**IMPL-1/2/3 实现已本地提交**（948c406／9f3ab8e／e7ad530），夜间集成快照 eaa8487 已取得 60 次真实窗口 M1 和补充回归。M1 是构造入口至首帧处理开始，不是进程启动至呈现；**D-ACCEPT 部分复验已执行、正式验收未完成**，阈值／同步比较／跨平台与 race 仍待处理 | TASK-FONT-001-A、TASK-FONT-001-B | go test -run TestFontLazyLoad ./pkg/runtime/theme/... |
| TASK-FONT-001-C | theme/font | 按字形覆盖的 fallback 链：GlyphID 反解与 notdef 可观测、覆盖判定作为候选准入（挡住"能解析但没汉字"）、按需字形级补充链、emoji 实测降级（**已实现**，10 个验收用例全 PASS；覆盖判定单次 11.86µs） | TASK-FONT-001-A、TASK-FONT-001-B | go test -run TestFontFallbackChain ./pkg/runtime/theme/... |
| TASK-FONT-001-A | theme/font | 系统字体探测 + `FontProvider.Resolve` + 诊断（**已实现**，Windows 实测；macOS/Linux 待验证） | TASK-THEME-SEAM-001 | go test -run TestFontProbe ./pkg/runtime/theme/... |
| TASK-FONT-001 | theme/font | CJK 字体探测（A）/ 嵌入子集（B）/ fallback 链（C）/ 懒加载（D）；IME 归 WIDGET-001。**A/B/C 已实现，D 的 IMPL-1/2/3 已本地提交，正式验收未完成**。最新进度及 60 次真实窗口 M1 证据见 D-ACCEPT 第 18 版；成功跨 family、跨平台、race、同步比较和阈值不因夜间交付自动通过 | TASK-THEME-SEAM-001 | go test ./pkg/runtime/theme/... |
| TASK-ROUTER-001 | runtime | 实现单窗口路由、MacroOp 转场动画与 OSWindow | TASK-STATE-001 | go test -v ./pkg/runtime/router/... |
| TASK-LIFE-001 | runtime | 实现 Context 生命周期与 //go:embed 资产管线 | TASK-ROUTER-001 | go test -v -race ./pkg/runtime/lifecycle/... |
| TASK-DEVX-001 | cli/watch | 实现双进程防抖热重启与状态 Hydration | TASK-LIFE-001 | go test -v -race ./cmd/goxml/watch/... ./pkg/runtime/hydration/... |
| TASK-DEVX-002 | cli/preview | 实现独立毫秒级静态 UI 预览器 | TASK-STYLE-001 | go test -v ./cmd/goxml/preview/... |
| TASK-OS-001 | platform | 实现 Windows Zero-CGO 剪贴板与异步原生文件弹窗 | TASK-LIFE-001 | Windows: CGO_ENABLED=0 go test -v ./pkg/platform/clipboard/... ./pkg/platform/dialog/... |
| TASK-OS-002 | platform | 实现系统托盘与系统菜单栏（Windows 纯 Go，Linux/macOS 允许 CGO） | TASK-OS-001 | make build-windows && make build-linux |
| TASK-WIDGET-001 | widgets | 构建标准自绘组件库 (ContextMenu/DataGrid) | TASK-STYLE-001 | go test -v ./pkg/widgets/... |
| TASK-BENCH-001 | bench | 性能基准测试与旗舰 Demo 验收 | 全部前置任务 | go test -bench . -benchmem ./benchmarks/... |
| TASK-AUDIT-001 | docs/audit | 全项目 TASK 总账审计：基线锁定、任务全集、逐项核证与总账发布（**只读审计**，不顺手修代码；发现/处置/整改/验收四阶段分离） | - | 见任务卡；产物：`docs/04-WBS/TASK总账.md`、`docs/04-WBS/审计发现清单.md` |
| TASK-AUDIT-002 | docs/audit | A1 深化成果的独立复核（A2）：**A2-0 提交链与远端状态独立核验为前置门槛**（五个 SHA、父子关系、远端引用、审计工作树状态），通过后进入原五组问题；允许确认/降级/升级/撤销；只接收固定版本材料，**不得直接修复** | TASK-AUDIT-001 | 见任务卡；产物建议落盘：`docs/04-WBS/A2独立复核报告.md` |
| TASK-APP-FLAKY-001 | runtime/app | `TestFontLazyLoadNoFrameCounting/晚到` 提前退出缺陷的最小修复（**任务卡第 6 版＝实施与验收结果回填；第 1–5 版身份与修订记录保留**；**实施已完成**：`pkg/runtime/app/font_lazy_test.go` `H_base 6d9b451f…` → `H_fix f5cf868a…`，+26/−4，CRLF 保留）：删除 `:735-737` 的 500 帧退出条件、让 `:719` 的 15 s 墙钟成为真实预算、500 帧降级为诊断观察点、区分墙钟耗尽与帧观察点的失败归因；保留 `straddle` 分类与全部核心断言，不改生产代码。**实测**：修复前隔离运行 **7/7 失败**（已可确定性复现；此前审计 60 次未复现是因整包/整用例先跑「早到」预热了进程内共享 `defaultProvider`）；修复后隔离 **6/6**、父用例 **3/3**、整包 **20/20** 均 PASS，build/vet/docgate rc=0。**结论边界（不得扩大）**：①**测试自身提前退出的根因已定位并修正**；②**生产代码未发现缺陷证据**——不等于"生产代码已证明无缺陷"（未做变异/负向验证、未跑 `-race`、未跨平台验证）。**未完成项**：`gofmt -l` 因该文件为 CRLF 基线仍列出它（待负责人裁决 V8 口径）；`-race` 因本机缺 gcc rc=1；负向验证 N 系列未授权未执行 | TASK-FONT-001-D-IMPL-2 | 见任务卡 §6：`go test -count=1 -v -run '^TestFontLazyLoadNoFrameCounting$' ./pkg/runtime/app/`（**×1 ＋ ×30**）；`go test ./pkg/runtime/app/ -count=1 -v -timeout 30m`（**×20**，第 2 版由 10 上调）；负向验证 N1–N4 只改该测试文件。⚠ 上述为**卡内计划次数**（以卡为准，不自行推测）；第 6 版实际执行次数见卡 §6「第 6 版实际」列（V2 实际 6 < 计划 30；V4/V5/V6 未执行；V8 未通过） |

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
