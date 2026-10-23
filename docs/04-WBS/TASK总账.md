# TASK 总账（审计骨架）

> 本文件由 `TASK-AUDIT-001` 建立，**数据由脚本从 WBS 总表与任务卡生成**，非手工转录。
> 审计基线：`e92e696748b982adb151e397586ccfbc8ede0edb`（远端 `master`）｜审计时间：2026-10-23
> 状态维度定义与判定矩阵见 `docs/04-WBS/任务卡/TASK-AUDIT-001.md`。
> **本轮为阶段 1 骨架**：「原记录状态」取自任务卡原文；除已取证项外状态一律记 `待审计`，
> **不得**把任务卡的「Done/已实现」直接当作验收通过。

> **幂等性**：重新生成时会保留「审计备注」列已填写的内容，不会悄悄覆盖人工审计结果。
> 逐项核证的**详细证据**写入各批次审计报告，不在本表展开。

## 1. 统计口径

- 主统计以**最底层可验收交付项**为单位；父任务状态由子任务 + 父级独立验收条件推导。
- 三个率：任务关闭率 / 验收通过率 / 证据完整率；另需分别统计阻塞、部分实现、未执行验收、未登记任务。
- 平台状态按 Windows / Linux / macOS / CI 分别记录，不合并。

## 2. 总账表

| # | TASK ID | 模块 | 标题 | 依赖 | 核心验收命令 | 原记录状态（任务卡原文） | 实现 | 验收 | 证据 | 文档 | 平台 | 审计备注 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | `TASK-ANALYZER-001` | analyzer | 实现语义校验（绑定路径、事件签名、For 作用域） | TASK-EXPANDER-001 | `go test -v ./internal/compiler/analyzer/...` | **Done**（2026-10-18，整体验收通过） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 2 | `TASK-BENCH-001` | bench | 性能基准测试与旗舰 Demo 验收 | 全部前置任务 | `go test -bench . -benchmem ./benchmarks/...` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 3 | `TASK-GENERATE-CACHE-001` | cli/cache | 增量生成与缓存（契约 §11：缓存键、命中条件、失效、原子性、可观测性；… | TASK-GENERATE-CLI-001 | `go test -run TestCache ./internal/cli/` | **B1–B5 全部完成（2026-10-21）**；证据：`internal/cli/cache_{key,store,wire,outp… | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 4 | `TASK-GENERATE-CLI-001` | cli/generate | 实现 Generate CLI、Manifest 与输出清理边界 | TASK-CODEGEN-001、TASK-ORCH-001… | `go test -v ./cmd/goxml/...` | **已完成（2026-10-20 收口）**——子任务 1-1 至 1-7 全部完成，AC-002-01~04 全部通过， | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 5 | `TASK-DEVX-002` | cli/preview | 实现独立毫秒级静态 UI 预览器 | TASK-STYLE-001 | `go test -v ./cmd/goxml/preview/...` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 6 | `TASK-DEVX-001` | cli/watch | 实现双进程防抖热重启与状态 Hydration | TASK-LIFE-001 | `go test -v -race ./cmd/goxml/watch/... ./pkg/r…` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 7 | `TASK-BINDING-CONTRACT-001` | codegen | 核查绑定现状并冻结绑定语义契约 | TASK-ANALYZER-001、TASK-CODEGEN… | `文档审查；`go test -count=1 ./internal/docgate`` | **契约已冻结（语义层）** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 8 | `TASK-BINDING-IMPLEMENT-001` | codegen | 接通 Analyzer Facts 到绑定生成与运行时读取 | TASK-CODEGEN-001、TASK-BINDING-… | `直接 Codegen 和运行时读取已验；Pipeline `ModulePath` 集成仍开…` | **步骤 1 直接 Codegen 与步骤 2 已完成；Pipeline 集成未完成；步骤 3 显示缓存实现已交付，完整验收见子任务卡** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 9 | `TASK-BINDING-IMPLEMENT-002` | codegen | 绑定运行时读取与显式重新布局验收 | TASK-BINDING-IMPLEMENT-001 | `go test -count=1 -run '^TestBindingRuntime' ./…` | **完成** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 10 | `TASK-BINDING-IMPLEMENT-003` | codegen | 绑定显示缓存与 shaping 观测验证 | TASK-BINDING-IMPLEMENT-002 | `显示缓存已实现；契约场景③/⑤和 Gio shaping 证据未闭环，见验收报告` | **显示缓存实现已交付；契约全维度验收尚未闭环** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 11 | `TASK-CODEGEN-001` | codegen | 实现 Gio 布局（含 Align/Justify/Gap/尺寸/Gri… | TASK-ANALYZER-001 | `go test -v ./internal/compiler/codegen/...` | **进行中**（静态最小切片完成；绑定读取最小切片已接通） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 12 | `TASK-CODEGEN-NAMING-001` | codegen | 生成符号命名与包级冲突契约（同包多 `.gxml` 的 `renderK… | TASK-CODEGEN-001、TASK-GENERATE… | ``go test -run '^TestNaming' ./internal/compile…` | **已裁决（D-1~D-5）；C1 预检检测 **项目负责人验收通过（2026-10-22，附带非阻塞遗留项，见 §10.11）**；C2 … | 已实现 | 通过 | 完整 | 待审计 | Windows |  |
| 13 | `TASK-CODEGEN-STATIC-CACHE-001` | codegen | 实现静态子树固化（`op.Record`/`op.CallOp`，关闭契… | TASK-CODEGEN-001 | `go test -bench BenchmarkStaticSubtree ./intern…` | **Done**（A1–A6 全部完成） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 14 | `TASK-STATE-001` | codegen/runtime | 状态更新与 UI 自动刷新（AOT Diff 降为 S5 评估项） | TASK-CODEGEN-001 | `见 `TASK-STATE-001` 分阶段验收` | **S0–S5 全部完成**（S5 结论：不引入 AOT Diff，REQ-003 推迟） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 15 | `TASK-STATE-IMPLEMENT-001` | codegen/runtime | S2 最小状态通知机制与统一绑定 View 接口 | TASK-STATE-001、ADR-0010 | ``f5c45bd` 已归档生成构造函数 nil 回归；定向和 Codegen 全包测试通过` | **已完成（2026-10-20 证据归档）**——实现已交付；生成构造函数 nil 回归测试已在 | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 16 | `TASK-EVENT-PAYLOAD-001` | compiler/codegen | 事件载荷的通用生成能力（FromPayload 渲染 + 独立测试），先… | TASK-BINDING-IMPLEMENT-001、TAS… | `go test -run TestEventPayload ./internal/compi…` | **Done（2026-10-20）** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 17 | `TASK-INPUT-001-D4-A` | compiler/codegen | Input 控件结构与事件接线（Editor 字段、属性透传、控件自持消… | TASK-EVENT-PAYLOAD-001、TASK-IN… | `go test -run TestInputCodegen ./internal/compi…` | **Done（2026-10-20）** ／ 证据：`internal/compiler/codegen/input.go` + `inpu… | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 18 | `TASK-INPUT-001-D4-B` | compiler/codegen | Input 的 lastApplied 同步算法（两条更新路径 + 逐帧… | TASK-INPUT-001-D4-A | `go test -run TestInputSync ./internal/compiler…` | **Done（2026-10-20）** ／ 证据：`internal/compiler/codegen/input.go` + | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 19 | `TASK-INPUT-001-D4-C` | compiler/codegen | Input 集成验收：同帧冲突运行时验证 + 编译门禁矩阵 + AC 映… | TASK-INPUT-001-D4-B | `go test -run 'TestInputSameFrame\／TestInputCom…` | **Done（2026-10-20）** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 20 | `TASK-INPUT-001-D5` | compiler/codegen | Input 自动化验收（MaxLen/Filter/Submit/改回）… | TASK-INPUT-001-D4-C、TASK-INPUT… | `go test -run TestInputAcceptance ./internal/co…` | **A–E 全部完成（2026-10-20）** ／ 证据：`internal/compiler/codegen/input_accepta… | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 21 | `TASK-INPUT-001` | compiler/input | Input 控件契约阶段：现状审计、语义裁决、输出契约冻结（先契约后实现… | TASK-STATE-001、TASK-BINDING-IM… | `go test ./internal/cli/ ./internal/compiler/co…` | **D1–D4 与 D5-A~E 完成（2026-10-20）；D5-F 人工验收待做**。契约见 [Input输出契约.md](../..… | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 22 | `TASK-CONTRACT-001` | contracts | 定义编译器 IR 与运行时核心接口契约 | - | `go test -v ./contracts/...` | **Done**（2026-10-18） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 23 | `TASK-DOC-BASELINE-001` | docs | 批次 0 文档冻结与基线建立 | 范围与退出门槛确认（已完成） | `0A 完成；0B 进行中，0D 门禁校准已推进；见任务卡及 B0 清单` | **进行中（0A 已完成，0B 审计进行中）** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 24 | `TASK-B0-ADR-CONFLICT-001` | docs/architecture | 校准 AOT Diff 设计储备的追踪链 | - | `决策已由 `f5c45bd` 固化；待全仓表述复核，见任务卡与 B0-F01` | **决策已记录；追踪校准进行中** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 25 | `TASK-AUDIT-001` | docs/audit | 全项目 TASK 总账审计：基线锁定、任务全集、逐项核证与总账发布（**… | - | `见任务卡；产物：`docs/04-WBS/TASK总账.md`、`docs/04-WBS/审…` | **阶段 0–1 已执行（规则冻结 + 任务全集建立）；阶段 2 逐项核证未开始** | 部分实现 | 未执行 | 完整 | 待审计 | Windows |  |
| 26 | `TASK-INPUT-001-DEMO` | examples/demo | Input 交互样板（手动验收入口，非功能开发） | TASK-INPUT-001-D4-C | `go build ./examples/demo/` | **Done（2026-10-20）** ／ 证据：`examples/demo` 生成代码含全部 Input 接线，冒烟存活 10 秒 | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 27 | `TASK-EXPANDER-001` | expander | 实现组件 Schema 分类、属性事件区分与 Def/Slot 展开 | TASK-PARSER-002 | `go test -v ./internal/compiler/expander/...` | **Done**（2026-10-18） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 28 | `TASK-STYLE-001` | macro/theme | 实现强类型 Theme 与组件宏展开（`<Def>`/`<Slot>` … | TASK-CODEGEN-001 | `go test ./internal/compiler/expander/ ./intern…` | **部分完成**（`<Def>`/`<Slot>` 已实现于 `internal/compiler/expander`； | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 29 | `TASK-PARSER-001` | parser | 实现带行列号的 GXML 语法分析与控制流解析 | TASK-CONTRACT-001 | `go test -v ./internal/compiler/parser/...` | **Done**（2026-10-18） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 30 | `TASK-PARSER-002` | parser | 实现强类型属性解析器 (EdgeInsets/Color/Binding… | TASK-PARSER-001 | `go test -v ./internal/compiler/parser/...` | **Done**（2026-10-18） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 31 | `TASK-ORCH-001` | pipeline | 实现编译编排层与失败门禁（关闭 Codegen 契约 §13 第 20 … | TASK-CODEGEN-001 | `go test -v ./internal/compiler/pipeline/` | **Done**（2026-10-19） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 32 | `TASK-OS-001` | platform | 实现 Windows Zero-CGO 剪贴板与异步原生文件弹窗 | TASK-LIFE-001 | `Windows: CGO_ENABLED=0 go test -v ./pkg/platfo…` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 33 | `TASK-OS-002` | platform | 实现系统托盘与系统菜单栏（Windows 纯 Go，Linux/macO… | TASK-OS-001 | `make build-windows && make build-linux` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 34 | `TASK-LIFE-001` | runtime | 实现 Context 生命周期与 //go:embed 资产管线 | TASK-ROUTER-001 | `go test -v -race ./pkg/runtime/lifecycle/...` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 35 | `TASK-ROUTER-001` | runtime | 实现单窗口路由、MacroOp 转场动画与 OSWindow | TASK-STATE-001 | `go test -v ./pkg/runtime/router/...` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 36 | `TASK-DEMO-001` | runtime/app | 最小可运行窗口：管线产物接入 Gio 主循环 | TASK-ORCH-001 | `go test -v ./pkg/runtime/app/` | **Done**（2026-10-19） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 37 | `TASK-THEME-SEAM-001` | theme/contract | 冻结主题/字体接缝（路线 B）：`FontProvider` 契约、`p… | TASK-CODEGEN-001、ADR-0012 | `go test ./contracts/... ./pkg/runtime/... ./in…` | **已完成**（6/6 子任务）｜优先级：P0 ｜ 预计：3h | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 38 | `TASK-FONT-001` | theme/font | CJK 字体探测（A）/ 嵌入子集（B）/ fallback 链（C）/… | TASK-THEME-SEAM-001 | `go test ./pkg/runtime/theme/...` | **进行中（A、B、C 已实现；D 设计已冻结且 IMPL-1 已实现，IMPL-2/3 与 D-ACCEPT 未开工）** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 39 | `TASK-FONT-001-A` | theme/font | 系统字体探测 + `FontProvider.Resolve` + 诊断… | TASK-THEME-SEAM-001 | `go test -run TestFontProbe ./pkg/runtime/theme…` | **实现已落地（2026-10-20）**，门禁复跑中；B/C/D 仍未开始 | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 40 | `TASK-FONT-001-B` | theme/font | 嵌入字体兜底：OFL 字体选型 + 构建期子集化 + `//go:emb… | TASK-FONT-001-A | `go test -run TestEmbeddedFallback ./pkg/runtim…` | **B3–B6 已实现并通过验收（2026-10-21）**；两个开工阻塞项均已解除（§7） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 41 | `TASK-FONT-001-C` | theme/font | 按字形覆盖的 fallback 链：GlyphID 反解与 notdef… | TASK-FONT-001-A、TASK-FONT-001-… | `go test -run TestFontFallbackChain ./pkg/runti…` | **已实现并通过验收**（2026-10-21；边界同日冻结） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 42 | `TASK-FONT-001-D` | theme/font | 懒加载与不阻塞首帧：冷启动基线测量、加载状态机、首帧不等待探测、帧内换 … | TASK-FONT-001-A、TASK-FONT-001-… | `go test -run TestFontLazyLoad ./pkg/runtime/th…` | **设计已冻结（2026-10-21）；实现部分完成——IMPL-1 已实现（`948c406`）， | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 43 | `TASK-FONT-001-D-ACCEPT` | theme/font | D 整体验收与回归：D6 十项映射到具体用例、四指标分行实测、十一项回归… | TASK-FONT-001-D-IMPL-1、TASK-FO… | `go test ./pkg/runtime/theme/ ./pkg/runtime/app…` | **验收矩阵已冻结（第 3 版，按评审意见同步 `CompletedAt` 与 `NotifyLateBound` 口径），执行未开始** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 44 | `TASK-FONT-001-D-IMPL-1` | theme/font | 异步加载器与状态机：`theme.Lazy`（Unloaded/Load… | TASK-FONT-001-D | `go test -run TestFontLazyLoad ./pkg/runtime/th…` | **已实现**（按第 3 版接口，17 个用例全 PASS，结构门禁反向验证通过） | 已实现 | 部分通过 | 完整 | 待审计 | Windows |  |
| 45 | `TASK-FONT-001-D-IMPL-2` | theme/font | 帧内切换与布局一致性：`NewLazyApp` 接线、只在 `Frame… | TASK-FONT-001-D-IMPL-1 | `go test -run TestFontLazyLoad ./pkg/runtime/ap…` | **接口与切换时机已冻结（第 3 版，按评审意见收回 `NotifyLateBound` 写入责任），实现未开始** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 46 | `TASK-FONT-001-D-IMPL-3` | theme/font | 诊断、失败终态与严格语义：`Report` 完整字段、`Config.S… | TASK-FONT-001-D-IMPL-1、TASK-FO… | `go test -run TestFontLazyLoad ./pkg/runtime/th…` | **接口与失败语义已冻结（第 3 版，按评审意见同步 `CompletedAt` 失败路径口径），实现未开始** | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 47 | `TASK-FONT-001-D-PROBE` | theme/font | 真实窗口异步唤醒验证（D 卡 §10 第 2b 步的唯一执行载体）：首帧… | TASK-FONT-001-D | `go run ./pkg/runtime/app/testdata/windowprobe` | **已执行（2026-10-21），PROBE-1..6 全部 PASS，4/4 次运行稳定** | 已实现 | 通过 | 完整 | 待审计 | Windows |  |
| 48 | `TASK-WIDGET-001` | widgets | 构建标准自绘组件库 (ContextMenu/DataGrid) | TASK-STYLE-001 | `go test -v ./pkg/widgets/...` | Todo | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |
| 49 | `TASK-SEC-001` | 安全 | 威胁建模与安全边界审查（**须在 Codegen 接入前完成**） | TASK-CODEGEN-001 | `见任务卡` | **有条件通过**（2026-10-19） | 待审计 | 待审计 | 待审计 | 待审计 | 未测 |  |

## 3. 本轮已取证项

已取证 **4** 项，待审计 **45** 项（合计 49 项）。

| TASK ID | 本轮取证 | 实现 | 验收 | 证据 | 平台 |
|---|---|---|---|---|---|
| `TASK-FONT-001-D-IMPL-1` | 17 用例 PASS / 0 FAIL / 0 SKIP（基线 `e92e696`，测试矩阵 T1） | 已实现 | 部分通过 | 完整 | Windows |
| `TASK-FONT-001-D-PROBE` | windowprobe 5/5 PASS，`mode=real-window`，帧数=5（T15） | 已实现 | 通过 | 完整 | Windows |
| `TASK-CODEGEN-NAMING-001` | codegen 42 + cli 12 = 54 用例 PASS / 0 SKIP（T8/T9） | 已实现 | 通过 | 完整 | Windows |

> 口径说明：上表合计 49 项 = 原有 48 个功能任务 + 本次新增的 `TASK-AUDIT-001`
> （审计任务自身，为通过 docgate「有卡必须在 WBS 登记」门禁而登记）。
> 被审的**功能任务**为 48 项，其中已取证 3 项、待审计 45 项。

## 4. 生成方式（可复现）

```
git worktree add --detach <path> e92e696748b982adb151e397586ccfbc8ede0edb
# 本表由脚本解析 WBS总表.md 与 任务卡/TASK-*.md 的「- 状态：」行生成
```
