# TASK-THEME-001-B2-PROPOSAL：方案评估报告（主题色引用语义）

> **2026-11-06 全库同步说明**：**未批准的历史提案/评审**。本文件保留 2026-10-24 的提案/评审原文，不是已批准 ADR，也不是当前实现说明。ThemeRef 运行时解析与 Palette 刷新仍待负责人裁决；下文涉及“theme 包不存在”“静态 Background 未绘制”“仅等待字体实现”等判断属于当时基线，已被后续实现取代。现行批准范围见 [R25 总览](../04-WBS/2026-11-06-R25-项目状态总览.md)，统一问题表见 [R24 压缩提案](../03-架构/adr/ADR-0013-主题色引用解析语义（提案压缩稿）.md)；原文的设计建议不因入库而获批。

> 角色：语言架构师 ｜ 时间：2026-10-24 23:2x–23:4x +08:00
> 配套交付：`docs/03-架构/adr/ADR-0013-主题色引用语义与解析时机-提案.md`（**提案 / 未批准**，编号经实测确认为下一空闲）
> 约束遵守：仅新增本报告 + ADR 提案草稿两份文件；**未修改任何冻结规范/契约/既有 ADR/WBS/任务卡/Go 源码**；未实现 B3/B4；未裁决任何语义。

## 0. 基线（本轮实测）

| 项 | 值 |
|---|---|
| GitHub API 查询 | 23:22:34 返回 master = `e92e696748b982adb151e397586ccfbc8ede0edb`（提交 2026-10-23T02:21:08Z），exit 0 |
| `git ls-remote` 实时验证 | **本轮成功**，exit 0，返回同一 `e92e696…` —— **两通道一致，远端 master 确证**（网络窗口已恢复；上两轮的失败记录各自独立保留） |
| 本地 | HEAD `9f3ab8e…`（master，领先 15 提交，**≠ 远端 master**）；0 暂存；29 条工作区差异（写入前） |
| 并发修改检查 | 主题相关域（`docs/03-架构/`、`docs/04-WBS/`、`internal/compiler/codegen/`、`pkg/runtime/theme/`）的在制改动仍全部属于字体 Agent（`theme/{font,lazy}.go` 等）；**无任何 Agent 正在修改主题规范、ADR、Codegen 颜色链路** |

## 1. 核心结论源码证据表（B1 报告六项 + 授权 §1 全项）

> 标注：【事实】= 本轮直接读取/执行所得；【推断】= 由事实链推出；【设计判断】= 方案性意见。证据缺口单独标注。

| # | 问题 | 结论 | 证据（路径:行） | 标注 |
|---|---|---|---|---|
| 1.1 | `$Theme.X` 规范定义与示例 | 颜色属性值三形态（`#RRGGBB/#RRGGBBAA/$Theme.Field`）；仅限颜色属性、非绑定；示例用 `$Theme.Primary`；`Accent` 仅作为**用户 Prop 名**出现；字段全集未定义 | `GXML语言规范.md:630-637, 720, 872, 913, 931` | 事实 |
| 1.2 | Parser 生成主题引用 | `parseColor` 识别 `$Theme.` 前缀 → `NRGBAColor{IsThemeRef:true, ThemeField:field}`；十六进制 → RGBA；`Background` 在 `layout.go:66` 挂到节点 | `internal/compiler/parser/attrs.go:257-278`、`parser/layout.go:66`、`parser/layout.go:170` | 事实 |
| 1.3 | Expander 是否保留主题引用 | **保留**：`resolveColorAttr` 对已有 Background 直接返回（不动）；Def Prop 替换经 `colorFromValue`，Prop 值若为 `$Theme.X` 则主题引用继续传播（Def 展开后仍是主题引用） | `internal/compiler/expander/color.go:18-53`、`expand_def.go:58`、`expander.go:267` | 事实 |
| 1.4 | Analyzer 是否解析/验证/改写 | **零处理**：analyzer 包对 `ThemeField/IsThemeRef/NRGBAColor/主题` 零命中 → **未知字段今天没有任何诊断** | `internal/compiler/analyzer/`（grep 实测空） | 事实 |
| 1.5 | IR 如何保存 | `NRGBAColor{IsThemeRef, ThemeField, R,G,B,A}`（主题引用不带色值）；挂在 `NodeIR.Background *NRGBAColor`；Prop 类型枚举含 `PropColor` | `contracts/ir_schema.go:129-136, 717, 593/608/629` | 事实 |
| 1.6 | **Codegen 是否消费（普查，非单字符串搜索）** | **颜色渲染链整体缺失**：① `NRGBAColor` 全仓引用仅 parser/expander（生产方）、contracts（定义）、runtime_api（注释）——**codegen 零引用**；② `NodeIR.Background` 的全仓消费者仅生产侧；③ codegen 无任何颜色绘制调用（`paint./color.NRGBA/op.ColorOp/FillShape` 零命中）；④ codegen 测试零 Background 断言。**结论（强化 B1）：不仅主题引用，静态 `#RRGGBB` 背景色今天同样不被渲染，且无诊断静默丢弃** | `internal/compiler/codegen/`（三组反向 grep + 主渲染函数阅读 `codegen.go:297-603`） | 事实（双向验证：正向引用普查 + 反向渲染点阅读） |
| 1.7 | `ThemeProvider.GetColor` | 定义于 `runtime_api.go:46-48`（`GetColor(field string) color.NRGBA`）；**0 个调用方**（全仓 grep 实证，与注释自述一致）；**替换约束**：string key vs 强类型二选一，**不得并存**（:43） | `contracts/runtime_api.go:36-48`；全仓 grep | 事实 |
| 1.8 | `material.Theme`/`Palette` 真实字段 | `Theme{Shaper, Palette, TextSize, Icon, Face, FingerSize}`；`Palette{Bg:#ffffff, Fg:#000000, ContrastBg:#3f51b5(Material Indigo 500), ContrastFg:#ffffff}`（`material.NewTheme` 默认值）；`ContrastBg` 官方语义 = "用于活跃/重要/交互控件的醒目色"，被 Button 背景（button.go:58/74/81）、CheckBox 图标（checkbox.go:22）、Label 选区（label.go:117）消费 | `gioui.org@v0.10.3/widget/material/theme.go`、`button.go`、`checkbox.go`、`label.go`（模块缓存源码） | 事实（外部依赖源码） |
| 1.9 | app.go 中 Palette/Shaper 初始化与更新 | 初始化：`newThemeWithReport()`（= `theme.NewThemeWithReport`，app.go@HEAD:67/69）→ `material.NewTheme()` + 字体集装配 `Shaper`（theme.go:101/116）；每帧传 `th`（app.go@HEAD:349）；**Shaper 更新有六步帧内门禁**（swapPending，:373-394，IMPL-2 冻结）；**Palette 无专用更新路径**（调用方可自行改，无一致性门禁）——【设计判断】这是运行时方案需要补的约束点 | `pkg/runtime/app/app.go@HEAD:61-83,342-394`、`pkg/runtime/theme/theme.go:96-116` | 事实 + 设计判断 |
| 1.10 | `Text.Color` 与 `Background` 的通路 | **不同通路且都未渲染**：`Background` = 专用 IR 字段（`ir_schema.go:717`，生产未消费）；`Text.Color` = Schema 声明的组件 Prop（`expander/schema.go:131` `{Name:"Color", Type:PropColor}`）进 `Attrs`，**无专用 IR 字段**，Codegen 渲染 `material.Body1(th, …)` 不带任何颜色/TextStyle（`codegen.go:589,603`） | 上列路径 | 事实 |
| 1.11 | `STYLE-001-1` 范围与状态 | 范围 =「定义默认 Theme、颜色字段及主题切换接口实现；让 Codegen 渲染主题色」；状态 ⬜ 未完成；卡自注「IR 已有 `NRGBAColor{IsThemeRef, ThemeField}`，**Codegen 完全未用**」并自注其验收命令 `go test ./pkg/runtime/theme/...` **当前空转** | `docs/04-WBS/任务卡/TASK-STYLE-001.md:18,28-30` | 事实 |

**证据缺口**：无重大缺口。1.6 采用"正向全引用普查 + 反向渲染函数阅读"双向验证，排除"字符串搜索零命中≠无消费"的风险。

## 2. 对 B1 评审结论的确认 / 修正 / 保留

| B1 结论 | 本轮处置 |
|---|---|
| `$Theme.X` 语法边界（仅颜色属性、非绑定） | **确认**（规范原文复核） |
| IR 表示与上游产出 | **确认**（补充：主题引用不带色值，`R,G,B,A` 为零值） |
| Analyzer 零处理、未知字段无诊断 | **确认** |
| Codegen"完全未用"（引 STYLE-001 卡） | **修正并扩大**：本轮全引用普查证明不止主题引用——**静态颜色渲染同样缺失、无诊断**；且 `Text.Color` 是与 `Background` 不同的未渲染通路。B1 表述正确但低估了范围 |
| 三方解析时机矛盾（:891 vs ThemeProvider vs §4.3.1） | **确认并锐化**：补入 material 默认 Palette 事实与"编译期烘焙将使逐帧 Palette 失效"的推演 |
| `Accent`→`Palette.ContrastBg` 映射依据 | **保留**，补默认值 `#3f51b5` 与三个控件消费点证据 |
| ADR-0013 必要 | **确认**，且编号 0013 经 `ls docs/03-架构/adr/` 实测为下一空闲 |
| AccentForeground / Accent 自动升格的禁令 | **遵守**：字段候选表中不包含未经批准项（见 §3） |

## 3. 字段候选表（正式）

| 类别 | 内容 | 对应 Gio 字段 | 映射依据 | 需负责人批准 |
|---|---|---|---|---|
| 现有规范中的字段或示例 | `Primary`（仅示例，:720/:872/:913）；`Accent`（仅 Prop 名示例） | 无（示例未映射） | — | — |
| 现有代码中真正定义的字段 | **无**（零枚举：`"Primary"` 在 compiler/contracts 生产代码零命中；字段集不存在于任何代码或文档） | — | — | — |
| 拟议 GXML 语义字段（候选） | `Primary`（候选；值与来源待 D-4）；`Accent`（候选；映射待 D-5）。**明确不提出**：`AccentForeground`（未经批准不进字段集）；`Bg`/`Fg`（material 控件已直接消费，是否公开为 GXML 字段属语义决策，留负责人） | 见右 | `Accent`→`Palette.ContrastBg`（Gio 官方语义"活跃/重要/交互控件醒目色" + 3 个控件消费点）；`Primary`→**无直接对应字段**（`Palette` 无 Primary），值来源必须由 D-4 定义（颜色表 / Palette 约定 / 项目扩展结构） | **是**（字段集与映射均为语义决策，本提案不代决） |
| 现有代码中真正定义的字段（重复强调） | 无 —— `Primary` 不能因示例存在而视为已有正式值定义 | — | — | — |

## 4. 方案比较（要点；完整版见 ADR-0013 提案 §3）

- **方案 A（编译期固化）**：颜色值唯一来源必须**新建**（仓库现状无任何来源——这是硬前置）；未知字段=编译期诊断（需新诊断码）；运行时切换**不影响**已烘焙颜色（与双进程热重载兼容、与逐帧 Palette 互斥）；:891 文字完全一致无需改；主要代价 = 与 §4.3.1 及 material 控件**双轨不一致**。
- **方案 B（运行时解析 + 编译期字段校验）**：IR 不变；Codegen 按 D-3 发射（string key→接线 ThemeProvider / 强类型→**替换** ThemeProvider）；字段不存在=编译期诊断（字段集封闭后运行时无失败路径；`Palette` 恒有默认值）；主题切换 = 帧内改 `Palette` + 既有 Invalidate → 下一帧生效（状态更新契约 §2 零新增）；Palette 更新须遵守与 Shaper 相同的**帧内线程约束**（新增规则）；:891 需精确化（受控契约修订，D-6）。
- **推荐 B**（依据见 ADR §4）：与 Gio 惯例/material 单轨、通知机制零新增、保留编译期错误暴露；**最终选择权在负责人**。

## 5. 与 STYLE-001-1 的任务边界

- **已有任务覆盖**：主题色 Codegen 接线、默认 Theme/颜色字段/主题切换接口定义（`TASK-STYLE-001.md:18`）——**不得新建重复任务**。
- **尚未覆盖**：① ADR 级决策（解析时机/字段集/映射）= 本提案；② Analyzer 未知字段诊断；③ **静态颜色渲染**（卡字面只说"主题色"，但本轮证实静态色也未渲染——范围需扩）；④ 其验收命令空转问题（卡 :18 自注 `go test ./pkg/runtime/theme/...` 空转——注意 `pkg/runtime/theme` 已存在且有大量测试，该自注与现状不符，需一并修正）。
- **是否调整现有任务卡**：建议（未应用，见 §6）：`STYLE-001-1` 扩为「颜色渲染（静态 + 主题引用）」、修正验收命令、以 ADR-0013 批准为前置；**不新建任务**。
- **依赖顺序**：本提案批准（D-1～D-7）→ 契约修订落盘（D-6）→ `STYLE-001-1` 扩卡并实现（= 主题线 **B3**）→ **B4** 验收（测试矩阵 + 文档同步 + examples/demo 视觉确认）。
- **B3 前置/交付物**：前置 = ADR 批准 + :891 修订 + 诊断码登记；交付物 = Analyzer 诊断、Codegen 颜色发射、`pkg/runtime/theme` 解析 API、单元+集成测试。
- **B4 前置/交付物**：前置 = B3 完成；交付物 = 验收报告（含真实窗口/人工视觉确认）、全链路测试矩阵、`STYLE-001`/文档状态同步。

## 6. 修改权限遵守与建议 diff（**未应用**）

本轮仅新增：① `docs/03-架构/adr/ADR-0013-主题色引用语义与解析时机-提案.md`；② 本报告。其余一律未动。以下为**建议 diff**（待裁决后由有权者应用）：

1. `Analyzer输入输出契约.md:891`：原「`$Theme.Primary` 在编译期解析为具体颜色，不是运行时绑定」→ 建议「`$Theme.Field` 在编译期校验字段合法性；颜色最终取值在运行时按当前主题解析（ADR-0013），不是 ViewModel 运行时绑定」（以 D-1/D-6 裁决为准；若裁决方案 A 则保留原句并补颜色表定义）。
2. `代码生成输出契约.md` §4.3.2：增补主题色发射条款（形态待 D-3）。
3. `TASK-STYLE-001.md:18`：范围扩为「颜色渲染（静态+主题引用）」、修正空转验收命令、增加"前置：ADR-0013 批准"。
4. `GXML语言规范.md` §4.4：增一句"字段集见 ADR-0013 字段表"（非冻结区指引）。

**冲突检查**：写入前确认 `docs/03-架构/adr/` 无并发修改（ADR 目录零未跟踪文件）；目标路径 `ADR-0013-…-提案.md` 不存在，无覆盖风险。

## 7. 交付清单、命令与未完成项

**交付**：① ADR-0013 提案草稿（10 节齐全，标注"提案 / 未批准"）；② 本评估报告；③ §1 证据表（11 项，路径：行）；④ §2 对 B1 的确认/修正/保留；⑤ 负责人决策清单 = ADR 提案 §8 的 D-1～D-7；⑥ 新增文件 2 份（写入前后状态见下）；⑦ 命令记录见下。

**命令记录（本轮真实执行）**：`curl api.github.com/.../commits/master` exit 0（23:22:34）；`git ls-remote origin master` **exit 0**（网络恢复，两通道一致 = `e92e696`）；`git status` 系列只读命令若干；`go list`（0 条新增）；grep 普查若干（只读）。**未运行任何测试**（本任务为纯文档/调查任务，无代码变更，无回归面）。

**写入前后工作区状态**：前 = 29 条；后 = **31 条**（+ADR 提案草稿 +本报告，均在允许清单内）；暂存区 0；未跟踪 router 交付与字体在制文件原样未动。

**未完成项（须负责人/后续阶段处理）**：D-1～D-7 未裁决（本任务无权裁决）；`Primary` 颜色值未定义（D-4）；B3/B4 未实现（授权边界）；`诊断码目录.md` 新码登记（实现前流程）。

**声明**：主题系统**尚未打通**（Codegen 颜色渲染整体缺失是本轮确证的事实）；本提案不构成任何已生效决策。
