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
| 终结论 | **本卡证据资格不足**：卡内三条 `-run` 选择器在 `./internal/compiler/parser` 实测 0 个真实名字、`-v` 输出 `no tests to run`，rc=0 不等于执行；相邻 AC 证据列只是文字登记，不能替整卡验收 |
| 归类 | C 本卡证据资格不足（E0）；相邻 AC 文字记录是同一行另一字段，不另计一项 |
| 实现 SHA | 1ef9247（基线，parser/attrs.go 等）；无任务专属提交 |
| 验收 SHA / 记录 | 本卡无。相邻证据：docs/02-需求/验收用例.md AC-001-01/02 登记「2026-10-19 实跑 ok」，给出 TestParsePaddingVariants / TestParsePaddingRejects / TestParseAttributeSpan 等真实测试名 |
| 执行方是否独立 | **未证（缺具名执行者/独立会话记录）**；上一版措辞「无法确认：AC 表未写执行人；卡内无执行者」已撤销。依据：**无执行者身份/会话记录 → 历史执行方独立性未证**。本轮标准台账 `-v` 重放另列，E3 可复跑不自动等于独立历史验收。 |
| 命令 / rc / raw 可定位性 | 卡内三条 `-run` 前缀在指定 parser 包均为空选择器；本轮 `r1r-empty-parser-*-list` / `-run` 留原始输出。旧「1/1/18/41」是全仓前缀计数，命中在 contracts/expander/codegen/analyzer，不是 parser 包证据 |
| 当时范围 | 强类型属性解析：EdgeInsets 1/2/4 值、十六进制颜色与主题引用、Binding/Event 属性与错误位置 |
| 现行适用性 | 三条原命令在现行基线同样空跑；AC 表命令列含 `\|` 也为空跑，只有证据列的 7 个真实测试名在 parser 包存在（`r1r-replay-parser` 执行 PASS，仅作相邻能力检查） |
| 仍缺哪一层资格 | 本卡缺验收结果段、缺执行方、缺 rc/raw、缺任务专属提交 |
| 下一动作 | 由卡主补本卡三个子任务的专属验收命令与 raw；不得用相邻 AC 用例或 Done 字样代替，也不改写历史原文 |

## 2026-11-06 R23-R1 返修 R1 补正（覆盖上一版补记中被撤销的结论）

> 本次返修关闭协调者 F-C1 / F-C2 / F-C3。完整逐项字段见 [13 项证据资格复核（R1 返修补正版）](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md) 与交接包 `repair-r1/raw/closure-matrix.json`。

| 字段 | R1 返修后结论 |
|---|---|
| 单一状态（每项只计一次） | C-本卡证据资格不足（E0） |
| 证据层 | E0（本卡全 16 行，无验收结果段、无报告/契约链接、无 rc/raw） |
| 历史原命令是否有效 | 三条在 `./internal/compiler/parser` 的 `go test -list` 均 **0 个真实名字**；`-v` 实测 `testing: warning: no tests to run` + `[no tests to run]`，rc=0 但 0 用例执行。旧「1/1/18/41」是全仓前缀计数，命中在 contracts/expander/codegen/analyzer，**不是 parser 包证据** |
| 本轮另选命令 | `go test -count=1 -timeout 10m -v -run ^(7 个精确 AC 名)$ ./internal/compiler/parser`（与 PARSER-001 共用 r1r-replay-parser），**仅作相邻 AC 能力检查，不替代本卡验收** |
| 真实用例集合 | `r1r-replay-parser`：7 个顶层 PASS / 0 个子测试 PASS / 0 SKIP / 0 FAIL |
| 能证明的范围 | 相邻 AC 表「证据列」的 7 个测试名在 parser 包真实存在且本轮执行 PASS；但 AC 表命令列同样含 `\\|` 为空跑，登记表是文字而非命令执行记录 |
| 未覆盖的范围 | 本卡三个子任务（EdgeInsets 1/2/4 值、颜色/主题引用、Binding/Event 属性与错误位置）的专属验收、rc/raw、执行方、任务专属提交 |
| 历史执行方独立性 | 未证（缺具名执行者/独立会话记录） |
| 本轮 E3 重放 | r1r-replay-parser 是相邻 AC 的本轮 E3 重放，不构成本卡独立验收 |
