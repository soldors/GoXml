# TASK-INPUT-001-D4-C：集成与 AC 映射

> 2026-11-05 状态阅读提示：下文是2026-10-20的历史实现/自验记录。
> 同步顺序与期望以2026-11-02修订的 [Input 输出契约](../../03-架构/Input输出契约.md) 为准；
> 负责人已批准用户编辑优先、程序化同步静默与归一化追随，不能把本卡旧断言当作当前语义。
> 自动化实现存在不等于D5-F原生窗口/真实焦点/IME人工验收完成，整体验收范围见 [父卡](TASK-INPUT-001.md)。


## 元信息

- 状态：**Done（2026-10-20）**
- 父卡：[TASK-INPUT-001.md](TASK-INPUT-001.md)（D4 第三切片）
- 前置：D4-A ✅ `02b8593`、D4-B ✅ `3487289`
- 契约依据：[Input输出契约.md](../../03-架构/Input输出契约.md) §5.4、§9 场景 6

## 1. 范围（两道路径）

| 关口 | 内容 |
|---|---|
| C1 | **同帧冲突运行时验证**（契约 §5.4 / §9 场景 6，D4-B 明确留到此） |
| C2 | 生成代码**编译门禁矩阵**（四种事件组合 + 属性组合 + 容器组合 + 多 Input） |
| C3 | AC-002-10..19/23 映射（测试名/层级/命令/结果/未覆盖原因） |
| C4 | 契约与任务卡同步（实测结论 + 未覆盖边界） |

## 2. C1：同帧冲突——实测观测链

构造：先聚焦 → 一帧内同时「`vm.Title` 改为 `NEW`」+「队列有 `key.EditEvent{"X"}`」。

### 2.1 五个观测点的实测值

| # | 观测点 | 实测 | 符合契约 |
|---|---|---|---|
| 1 | Editor 最终文本 | `XNEW` | ✅ 外部值 `NEW` 先应用（`SetText` 后光标归零），用户输入 `X` 插入开头 |
| 2 | ChangeEvent 是否产生并消费 | 已消费 | ✅ 否则 `vm.Title` 会停在 `NEW` |
| 3 | ViewModel 最终值 | `XNEW` | ✅ 等于编辑器文本（`RecordText` 回写） |
| 4 | `applied` 重新求值 | `XNEW` | ✅ 路径 2 生效（等于回写后的 V） |
| 5 | 下一帧稳定性 | 两帧后仍 `XNEW`，vm 未再变 | ✅ 无重复回写、无额外 SetText |

**结论**：实现符合契约 §5.4 已冻结的「外部值先应用、不做合并/延迟」。

### 2.2 两个补充对照

| 测试 | 结论 |
|---|---|
| `TestInputSameFrameExternalWinsOverLocalEdit` | 外部值**覆盖**未回写的本地编辑（`LOCAL-ONLY` → `CHANGED`） |
| `TestInputNoValueKeepsLocalEdit` | **非受控**形态（无 `Value`）本地编辑**不被**覆盖——证明覆盖行为只由 Value 绑定触发 |

## 3. C2：编译门禁矩阵（11 个组合）

| 维度 | 组合 |
|---|---|
| 事件组合 | 仅 Change / 仅 Submit / 两者 / 都没有 |
| 属性组合 | Hint + ReadOnly + SingleLine + MaxLen + Filter 全开 |
| 容器组合 | Column / Row / `<If>` 的 Then / Else |
| 多实例 | 两个 Input（各自独立槽位与 `applied`） |

**全部真实 `go build` + `go test`**，不只字符串断言。

> 背景：D4-B 出现过两次**只有 `go build` 才能发现**的缺陷（receiver 遮蔽、
> `e declared and not used`）。本矩阵是对该类缺陷的系统性防线。

## 4. C3：AC 映射结果

| AC | 状态 | 测试 |
|---|---|---|
| 10 初始同步 | 通过（Codegen 运行时） | `TestInputSyncRuntimeBehaviors`、`TestInputSyncPath1Generated` |
| 11 回写 | 通过（Codegen 运行时） | `TestInputSyncWriteBackRuntime`、`TestInputSyncPath2Generated` |
| 12 回写后不重置 | 通过（Codegen 运行时） | `TestInputSyncWriteBackRuntime` 末段 |
| 13 外部改值同步 | 通过（Codegen 运行时） | `TestInputSyncRuntimeBehaviors` |
| 14 改回旧值 | **部分通过（结构）** | 判据 `V != A` 保证会触发；未做运行时改回 |
| **15 同帧冲突** | **通过（Codegen 运行时）** | `TestInputSameFrameConflictRuntime`（五观测点） |
| **16 MaxLen 截断** | **未执行** | 需真实按键合成 → D5 |
| **17 Filter 过滤** | **未执行** | 同上 → D5 |
| 18 Submit 声明 | 通过（结构） | `TestInputSubmitEnablesEditorSubmit`、`TestInputCompileMatrix` |
| 19 未声明 Submit | 通过（结构） | `TestInputSubmitEnablesEditorSubmit`、`TestInputNoEventsNoUpdate` |
| 23 不整页固化 | 通过（Codegen 结构） | `TestInputPageNotFrozen` |

**未执行原因已写入验收用例表**（16/17 需真实按键事件，属 D5）。

## 5. 明确未覆盖（不混入自动化通过项）

| 项 | 原因 | 归属 |
|---|---|---|
| MaxLen 截断不产生 ChangeEvent | 需真实按键合成 | D5 |
| Filter 过滤不产生 ChangeEvent | 同上 | D5 |
| Submit 回车运行时触发 | 同上 | D5 |
| **IME 组合输入** | **Gio 无合成 API** | **D5 人工验收，不得标为自动化覆盖** |
| 真实窗口中的 Input 交互 | 需图形会话 | D5 |
| 改回旧值的运行时验证 | 与 AC-002-13 同机制 | 可在 D5 补 |

## 6. 测试基础设施的一处改进

`rtProbe` 的子包默认不带 `-v`，`t.Log` 被丢弃，观测值无法展示。
改为子包用 `fmt.Println("OBS…")`，父测试从 stdout 提取并 `t.Log`。
这样**运行时行为的观测链可以作为测试输出被审计**，而不只是"绿了"。

（为此给 probe 共享头补了 `fmt` 导入；这属于测试基础设施，不影响生产代码。）

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **历史记录，现行语义下失效**：不得把 XNEW / 外部覆盖计为现行通过；AC-002-16/17 当时明确未执行，D5 后续补了自动化但 D5-F 仍人工 |
| 归类 | 未闭环（现行同帧语义以 R8 为准） |
| 实现 SHA | 46accb9bbb13f2c82cabb2a228246471487edb6d |
| 验收 SHA / 记录 | 卡 §2/§3/§4 自验；dev-log §22.7 记录该提交还夹带了另一条线的 WBS 两行 |
| 执行方是否独立 | **未证（缺具名执行者/独立会话记录）**；上一版措辞「否：并发线提交，且提交内容混入了别线未提交改动（已如实登记，不回滚）」已撤销。依据：归属说明，不是独立验收会话证据。本轮标准台账 `-v` 重放另列，E3 可复跑不自动等于独立历史验收。 |
| 命令 / rc / raw 可定位性 | 卡 §3 称 11 个组合「全部真实 go build + go test」；AC 表把 AC-002-14 记「部分通过（结构）」、16/17「未执行」；无逐条 rc/raw；E1：无历史 raw；本轮现行重放留 raw |
| 当时范围 | 同帧冲突观测链（XNEW，外部值先应用）、编译门禁矩阵、AC-002-10..19/23 映射 |
| 现行适用性 | **核心结论已被 11-02 推翻**：R8 报告写「同帧外部 NEW + 用户 X + Return 采用用户值」，契约 §5.4 改为「用户编辑优先」。旧卡 §2.1 的五观测点与 §2.2 的「外部值覆盖本地编辑」不再是现行期望 |
| 仍缺哪一层资格 | 现行同帧语义的独立复核 raw；旧 AC 映射表须按 R8 更新 |
| 下一动作 | 以 `input_adoption_test.go`/`input_candidatesem_test.go` 为现行判据；把 AC-002-15 的期望从「外部先应用」改为「用户优先」 |

## 2026-11-06 R23-R1 返修 R1 补正（覆盖上一版补记中被撤销的结论）

> 本次返修关闭协调者 F-C1 / F-C2 / F-C3。完整逐项字段见 [13 项证据资格复核（R1 返修补正版）](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md) 与交接包 `repair-r1/raw/closure-matrix.json`。

| 字段 | R1 返修后结论 |
|---|---|
| 单一状态（每项只计一次） | B-未闭环（现行同帧语义以 R8 为准） |
| 证据层 | E1（历史）+ 本轮 E3 精确名重放 |
| 历史原命令是否有效 | 卡内无可执行命令；AC 表命令列多为不存在的精确名，`TestInputSameFrameConflict` 是有效前缀（命中 1 个） |
| 本轮另选命令 | `go test -count=1 -timeout 10m -v -run ^(TestInputSameFrameConflictRuntime\|…)$ ./internal/compiler/codegen`（r1r-replay-d4c-structure） |
| 真实用例集合 | `r1r-replay-d4c-structure`：5 个顶层 PASS / 6 个子测试 PASS / 0 SKIP / 0 FAIL |
| 能证明的范围 | 本轮 5 个顶层 + 6 个子测试全部 PASS（结构/运行时层面） |
| 未覆盖的范围 | AC-002-16/17 当时明确未执行；`XNEW`/外部覆盖等旧期望在现行语义下失效；真实窗口 |
| 历史执行方独立性 | 未证（缺具名执行者/独立会话记录） |
| 本轮 E3 重放 | r1r-replay-d4c-structure 是本轮标准台账 E3 重放，不等于独立历史验收 |

