# TASK-PARSER-002：实现强类型属性解析器

## 元信息
- 状态：**Done**（2026-10-18）| 优先级：P0 | 预计：3h
- 分支：feature/TASK-PARSER-002
- 依赖：TASK-PARSER-001

## 1. 目标
将 GXML 属性解析为契约定义的强类型值，支持 EdgeInsets、颜色、绑定和事件属性，并为非法输入返回带源码位置的诊断。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-PARSER-002-1 | 实现 EdgeInsets 的 1/2/4 值解析及非法值诊断 | go test -v -run TestEdgeInsets ./internal/compiler/parser/... |
| TASK-PARSER-002-2 | 实现十六进制颜色与主题引用解析 | go test -v -run TestColor ./internal/compiler/parser/... |
| TASK-PARSER-002-3 | 实现 Binding/Event 属性解析与错误位置测试 | go test -v -run 'Test(Binding|Event)' ./internal/compiler/parser/... |

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **证据资格不足，但可定位到相邻 AC 记录**：本卡自身没有验收结果，不能凭「Done」判通过；AC-001-01/02 在另一份版本化文档里登记了通过并给出真实测试名，可据此认定「属性解析能力有通过记录（限 AC 范围）」 |
| 归类 | 无法核实（本卡）/ 有通过记录（相邻 AC 范围） |
| 实现 SHA | 1ef9247（基线，parser/attrs.go 等）；无任务专属提交 |
| 验收 SHA / 记录 | 本卡无。相邻证据：docs/02-需求/验收用例.md AC-001-01/02 登记「2026-10-19 实跑 ok」，给出 TestParsePaddingVariants / TestParsePaddingRejects / TestParseAttributeSpan 等真实测试名 |
| 执行方是否独立 | 无法确认：AC 表未写执行人；卡内无执行者 |
| 命令 / rc / raw 可定位性 | 卡内 3 条 -run 前缀命令（TestEdgeInsets / TestColor / Test(Binding\|Event)）；**均未附退出码与结果**；E0（本卡）→ 相邻 E1：本卡零执行记录；AC 表有文字结果但无 raw。前缀选择器在现行基线分别命中 1 / 1 / 18 / 41 个测试函数，不是空判据；但验收用例表里 11 个 -run 名称不存在，说明该表命令列不可直接执行 |
| 当时范围 | 强类型属性解析：EdgeInsets 1/2/4 值、十六进制颜色与主题引用、Binding/Event 属性与错误位置 |
| 现行适用性 | attrs.go 与 7 个 AC 命名测试中的 6 个在基线存在；本轮 `r1c-c1-parser-v` 重放其中 3 个（Padding 两形态 + AttributeSpan）rc=0 |
| 仍缺哪一层资格 | 本卡缺验收结果段、缺执行方、缺 rc/raw、缺任务专属提交 |
| 下一动作 | 在卡内补一段「验收结果＝指向 AC-001-01/02，2026-10-19，非本卡独立验收」即可关闭资格问题；不需要重跑历史，也不把它升级为独立验收 |

