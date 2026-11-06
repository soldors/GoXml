# TASK-THEME-001-B1-REVIEW：任务错位核实与主题线独立评审报告

> **2026-11-06 全库同步说明**：**未批准的历史提案/评审**。本文件保留 2026-10-24 的提案/评审原文，不是已批准 ADR，也不是当前实现说明。ThemeRef 运行时解析与 Palette 刷新仍待负责人裁决；下文涉及“theme 包不存在”“静态 Background 未绘制”“仅等待字体实现”等判断属于当时基线，已被后续实现取代。现行批准范围见 [R25 总览](../04-WBS/2026-11-06-R25-项目状态总览.md)，统一问题表见 [R24 压缩提案](../03-架构/adr/ADR-0013-主题色引用解析语义（提案压缩稿）.md)；原文的设计建议不因入库而获批。

> 角色：独立评审（主题线）｜ 时间：2026-10-24 23:1x–23:3x +08:00
> 性质：只读评审。**本轮未修改任何 `.go`、测试、规范、ADR、任务卡、WBS 或开发日志**；唯一新增文件为本报告。
> 重要前置事实：仓库中**不存在** `TASK-THEME-001` 任务卡、`ADR-0013` 文档或任何主题变更提案文档（grep 零命中）——本评审的规格 = 负责人本轮纠偏指令的六项审查点；被评审对象 = 冻结语义现状 + 拟议中的主题变更方向。

---

## A. 任务错位原因与授权核实结果

### A.1 我实际执行了什么

本轮纠偏指令之前的**连续四轮**任务均为 Router 线，每轮都以负责人书面指令开场，我逐轮执行：

| 轮次 | 指令标题（原文） | 产物 |
|---|---|---|
| 1 | 《TASK-ROUTER-001 开工授权：先核查与冻结接口，再分阶段实现》 | 开工核查与接口设计报告 |
| 2 | 《TASK-ROUTER-001：阶段 B1 — 路由核心实现授权》 | `pkg/runtime/router/`（3 文件）+ B1 报告 |
| 3 | 《TASK：ROUTER-001-B1-REVIEW — 路由核心独立验收》 | B1 独立验收报告 |
| 4 | 《TASK-ROUTER-001-B1-CLOSE / B2-PLAN》 | **即本轮被指认的两份交付物**：B1 验收收口 + B2 任务卡草案 |

### A.2 为什么交付物与当前任务不一致

**本会话在收到本轮纠偏指令之前，从未出现任何包含 `TASK-THEME-001`、`ADR-0013` 或主题线评审的指令**——我可以核实且仅能核实的范围是我收到的指令（会话记录中四轮均为 Router 授权原文，可逐字引用）。因此：错位**不是我在会话内自行替换任务**；若第 4 轮原计划为主题线评审，则错位发生在**指令分发环节**（发给本 Agent 的指令文本本身是 Router 收口任务）。我无法核实负责人侧的分发意图，只能确认以上事实链。

### A.3 Router 任务是否有独立授权

**有，授权依据 = 上述四份负责人书面指令**（均来自负责人消息原文，非我推断）：第 1 轮授权阶段 A 核查与接口设计并明确"交付后暂停"；第 2 轮授权 B1 实现并划定"不得继续 B2/B3、不提交"；第 3 轮授权独立验收并限定"只审查不修改"；第 4 轮授权"验收收口 + B2 任务卡编制"并明确"不实现 B2、完成后停止等待批准"。四轮均未授权提交 Git，故 router 交付至今为**本地未跟踪文件**。

### A.4 本轮任务的授权

`TASK-THEME-001-B1-REVIEW` 在仓库中**无任务卡**（grep 零命中）；本轮负责人纠偏指令即其授权与规格（六项审查点），我据此执行。Router 线的收口报告与 B2 卡草案**维持草案状态**，未被任何一方批准，本轮起不再推进。

## B. 工作区改动归属清单（本轮实测，28 条）

| 归属 | 条目 | 依据 |
|---|---|---|
| 字体 Agent（在制） | M：3 张 FONT 卡、`dev-log/2026-10-23.md`、`pkg/runtime/app/app.go`、`testdata/concurrencyprobe/main.go`、`pkg/runtime/theme/{font,lazy}.go`、`WBS总表.md`；未跟踪：`lazy_status.go`、`font_lazy_{strict,embedded_hit,m1_baseline}_test.go`、`probe_elapsed_cache_semantics_test.go`、`dev-log/2026-10-24.md`、`任务卡/TASK-APP-FLAKY-001.md` | APP-FLAKY 卡自述"设计卡未实施"并自述其 WBS 登记；各文件 mtime 与其 dev-log 吻合；本轮零变化 |
| 我（Router 线四轮 + 本轮） | `pkg/runtime/router/`（3 文件）、5 份 Router 文档、`docs/04-WBS/A2独立复核报告.md`、**本轮新增：本报告** | 各报告落盘记录；router 文件 mtime 09:42–09:44 后未变 |
| 轮四遗留（先前执行者，非我） | `2026-10-23-四轮工作综合.md`、`Git操作边界与自动推送治理.md`、`2026-10-23-分类测试矩阵验收报告.md`、`原始日志/`、`GXML 项目质量审计统一交付协议 V2.md` | TASK-AUDIT-002 §7 红线清单所列同名产物 |
| 其他 | 无 | — |

**远端双通道**：`git ls-remote` 实时验证 **失败**（23:14:08，exit 128，网络窗口，与上轮同故障形态）；GitHub API 查询 **成功**——API 可见 master = `e92e696748b982adb151e397586ccfbc8ede0edb`（提交时间 2026-10-23T02:21:08Z）。按既有纪律（A2-0 §6）：API 结果不等同 ls-remote 实测，仅记为"API 可见提交"；本地跟踪引用一致为 `e92e696`。本地 HEAD `9f3ab8e`（领先 15 提交）。

## C. 主题线独立评审

### C.1 `$Theme.X` 的现有语法和语义边界【冻结，证据充分】

- 语法：颜色属性值允许三种形态——`#RRGGBB`、`#RRGGBBAA`、`$Theme.Field`（`GXML语言规范.md:630-637`）。
- 边界三条：① **只允许出现在颜色属性中**；② **不属于 ViewModel 绑定**（不能写 `{Theme.Field}`，:637）；③ 规范自带的示例使用 `$Theme.Primary`（:720、:872、:913），且示例中 `Accent` 只是**用户自定义 Prop 名**（`Props="Accent:color=$Theme.Primary"`）——**规范未定义 `$Theme.Accent`，也未定义字段全集**（`Primary` 仅存在于示例，规范正文没有字段清单）。
- 演进约束：规范 :6「v1 语义冻结；新增语法必须进入 v2，并通过新的 ADR 批准」；§11（:1050-1062）列出必须走 v2/新 ADR 的变更类型。

### C.2 Analyzer 契约中"编译期固化"的完整上下文【关键：这是目标语义，不是已实现行为】

- `Analyzer输入输出契约.md:891`：「**主题引用是静态的**：`$Theme.Primary` 在编译期解析为具体颜色，不是运行时绑定。因此绑定到 `color` 属性的值只可能是具体颜色，不需要 `IsThemeRef` 那种表示」；:892：`contracts.NRGBAColor`（含 `IsThemeRef`/`ThemeField`）是"静态字面量与主题引用的编译期表示"，**不作为绑定目标类型**（与 :880-884 的绑定兼容矩阵配套阅读——绑定到 color 属性只能来自 `color.NRGBA/RGBA` 运行时取值）。
- **实现现状与该条款的落差**：IR 表示已产出（`ir_schema.go:131-132`；`parser/attrs.go:265`、`expander/color.go:52` 构造 `NRGBAColor{IsThemeRef:true, ThemeField:field}`），但 **Codegen 对 `NRGBAColor/IsThemeRef/ThemeField` 的消费为零**（codegen 包 grep 零命中）；`"Primary"` 在 compiler/contracts 生产代码中零命中——**不存在字段集枚举，也不存在"解析为具体颜色"的任何实现**。`TASK-STYLE-001` 卡自证：`STYLE-001-1` ⬜ 未完成，「IR 已有 `NRGBAColor{IsThemeRef, ThemeField}`，**Codegen 完全未用**」。
- `contracts/runtime_api.go:33-43`：`ThemeProvider`（`GetColor(field string) color.NRGBA`）**当前 0 个调用方**（grep 实证），有意保留为"契约层落点"，并写明**替换规则**：若 STYLE-001-1 决定强类型字段而非 string key，应**替换**该接口并同步 `ThemeField` 语义，**不得新增第二种颜色获取方式**。

### C.3 `Primary` 的语义与兼容性

- **语义现状**：`Primary` 是唯一在规范示例中出现过的主题字段名，但**没有定义的颜色值、没有解析器、没有字段集声明**。"编译期解析为具体颜色"目前是契约声明而非可执行行为。
- **兼容性**：新增字段/解析不触碰语法文法（`$Theme.Field` 形态不变），无语法层破坏；但**契约层必然变更**——解析策略落点涉及 `ThemeProvider` 的"使用或替换"裁决、`代码生成输出契约.md` §4.3.2（参数固定 `*material.Theme`，项目主题结构如何到达生成代码未定义）、以及 §4.3.1 的"主题可逐帧变化"惯例。
- **必须由 ADR 裁决的张力（本轮最重要的发现）**：三方存在**解析时机矛盾**——① Analyzer 契约 :891 说"编译期解析为具体颜色"（静态烘焙）；② `ThemeProvider.GetColor` 是**运行时**按字段取色接口；③ 代码生成输出契约 §4.3.1 与 Gio 惯例是"主题逐帧传入、`Palette` 可变"（`theme.go:68-69` 明示调用方可改 `Palette`）。若真把颜色**编译期烘焙**进生成代码，运行时改 `Palette` 不会生效，与 §4.3.1 和 D-006"强类型 $Theme Struct"的直觉相悖；若运行时查表，则 :891 的表述需要精确化（例如"编译期校验字段合法性、运行时按字段解析"）。**这个矛盾在现有文档中无人裁决，是 ADR-0013 的核心议题。**

### C.4 `Accent` 与 Gio `ContrastBg` 的映射依据

- **映射目标存在且语义吻合**：gio v0.10.3 中 `material.Theme` 内嵌 `Palette{Bg, Fg, ContrastBg, ContrastFg}`（`widget/material/theme.go`）；`Palette.ContrastBg` 的官方注释为「a color used to draw attention to active, important, interactive widgets such as buttons」——即 Gio 自己的"强调色"，被 `Button` 背景（button.go:58/74/81）、`CheckBox.IconColor`（checkbox.go:22）、`Label.SelectionColor`（label.go:117）实际消费。**Accent→`Palette.ContrastBg` 在 Gio 惯例上有充分依据。**
- **兼容性**：规范既有示例中 `Accent` 是 Prop 名，`$Theme.Accent` 是主题字段名——两者命名空间不同（`$Theme.` 前缀 vs 裸标识符），新增 `$Theme.Accent` 字段**不与既有示例冲突**；且规范示例 `Accent:color=$Theme.Primary` 恰好预示了"Accent 与 Primary 派生关系"的设计直觉，可作为 ADR 讨论的先例引用。
- **边界**：映射是**语义决策**（强调色是否固定绑定 ContrastBg、深浅色下如何跟随），属负责人/ADR 范围，本评审只给出依据，**不代做决策**。

### C.5 Codegen、App、Palette 与 Shaper 的运行时生命周期【证据】

1. `pkg/runtime/theme.NewTheme()` **每次返回独立可变实例**（theme.go:68-70：「主题是可变结构（调用方可能改 `Palette`、换 `Shaper`），返回共享单例会让两个窗口互相污染」）——不存在全局单例。
2. Shaper 生命周期：`NewThemeWithReport` 构建字体集合并装配 `text.Shaper`（:101/:116）；FONT-D-IMPL-2 冻结了"只在 FrameEvent 内换 `Shaper`"的六步顺序（app.go@HEAD frame 注释）——**Shaper 替换已有严格门禁**。
3. App 生命周期：`NewApp` 持有 `th`，每帧以参数传给 `View.Layout`（app.go@HEAD:349），符合代码生成输出契约 §4.3.1"主题可逐帧变化"。
4. docgate 门禁锁定「生产代码不得自行调用 `material.NewTheme()`」（theme.go:8，docgate `theme_seam_test` 执行）——**新增主题色路径必须经 `pkg/runtime/theme` 装配**，不能绕过。
5. 对 ADR-0013 的含义：主题色解析若走运行时，落点应是**逐帧 `th` 参数 + Palette**（与上述生命周期一致）；任何"全局主题单例"或"绕过 theme 包"的设计都会与现有门禁/生命周期冲突。

### C.6 ADR-0013 是否必要——**必要**（五条依据）

1. 语言规范 :6 与 §11：主题字段语义属于行为语义变更，须走 ADR（即便语法不变）。
2. WBS 总表「规则 1：契约先行」：涉及"语言语义/阶段边界/跨阶段数据结构（IR 字段语义）"的改动必须先契约后实现。
3. `runtime_api.go:43` 的**替换规则**本身要求"决定"被记录：string key vs 强类型字段，二选一必须落 ADR，否则 `ThemeProvider` 与 `ThemeField` 的去留悬空。
4. **C.3 所列三方解析时机矛盾**必须由单一决策记录终结（编译期烘焙 vs 运行时查表），否则 STYLE-001-1 无法开工。
5. 先例一致性：主题/字体边界由 ADR-0012 记录；主题**色彩**边界无任何 ADR 覆盖，ADR-0013 是自然编号。

**ADR-0013 建议范围**：① `$Theme` 字段全集与扩展规则（Primary/Accent 起步）；② 解析时机（建议：编译期校验字段合法 + 运行时经 `th.Palette`/项目主题结构解析，兼容 §4.3.1）；③ `Primary` 的具体颜色与来源；④ `Accent`→`Palette.ContrastBg` 映射；⑤ `ThemeProvider` 使用或替换的裁决；⑥ 与代码生成输出契约 §4.3.2 参数形态的关系。

### C.7 主题线 B2/B3/B4 的准入条件与阻塞项

> 编号按负责人指令口径（主题线自身的 B2/B3/B4）；**最大阻塞项先行声明：拟议变更没有书面提案文档**——仓库无 TASK-THEME-001 卡、无 ADR-0013 草稿、无提案记录，本轮指令只有标题级要点。没有提案输入，B2 无法起草。

| 阶段 | 准入条件 | 当前阻塞项 |
|---|---|---|
| B2（起草 ADR-0013 + 契约提案） | 负责人提供**书面提案输入**（字段全集、Primary 颜色值与来源、Accent 语义主张、解析时机偏好）；确认与既有 `TASK-STYLE-001-1` 的**归属关系**（主题色接线本就是 STYLE-001-1 的交付物——新工作是替换其设计前提，不得平行撞卡） | 提案缺失（阻塞）；归属裁决（阻塞） |
| B3（实现：Analyzer 校验 + Codegen 消费 + Runtime/Palette 装配 + 测试） | ADR-0013 批准；Analyzer 契约 :891 表述按裁决精确化；代码生成输出契约 §4.3 附录更新；docgate `theme_seam` 门禁保持绿 | 无技术性阻塞（IR/门禁/生命周期全部就绪，见 C.2/C.5） |
| B4（验收） | 测试矩阵（字段解析、非法字段诊断、Palette 变更逐帧生效、与绑定不混淆）＋ 验收命令修正（STYLE-001 卡自注：`go test ./pkg/runtime/theme/...` 当前空转——B2 起草时一并修正该验收命令口径） | 依赖 B3 |

**评审结论：有条件通过。** 现状基础（语法冻结、IR 表示、契约落点、门禁、生命周期）齐备且自洽；但主题变更**必须**先补书面提案并经 ADR-0013 裁决解析时机与字段集，方可进入实现阶段。C.3 的三方张力与 C.7 的两项阻塞是"有条件"的全部内容。

## D. 声明

1. **本轮零代码修改**：未修改/删除/覆盖/重命名任何源码、测试、规范、ADR、任务卡、WBS 或开发日志；唯一新增文件为本报告；未执行任何改变工作区状态的 git 命令（无 reset/checkout/clean/stash/rebase/pull）。
2. **未代做主题语义决策**：`Primary` 的颜色值、`Accent` 的映射、解析时机、字段集扩展均为负责人/ADR-0013 范围，本报告只提供证据与建议。
3. **未冒充**：Router 线四轮交付与其授权链在 §A 如实分列；Router 的收口报告与 B2 卡草案维持草案状态，本轮起冻结，不再推进。
4. 远端状态如实分通道记录：API 可见 `e92e696…`；`git ls-remote` 本轮失败（网络），未以 API 结果冒充实测。
