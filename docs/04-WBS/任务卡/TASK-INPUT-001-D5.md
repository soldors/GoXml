# TASK-INPUT-001-D5：自动化验收（含测试基础设施审查）

> 2026-11-05 状态阅读提示：下文是2026-10-20的历史实现/自验记录。
> 同步顺序与期望以2026-11-02修订的 [Input 输出契约](../../03-架构/Input输出契约.md) 为准；
> 负责人已批准用户编辑优先、程序化同步静默与归一化追随，不能把本卡旧断言当作当前语义。
> 自动化实现存在不等于D5-F原生窗口/真实焦点/IME人工验收完成，整体验收范围见 [父卡](TASK-INPUT-001.md)。


## 元信息

- 状态：**A–E 全部完成（2026-10-20）** | 证据：`internal/compiler/codegen/input_acceptance_test.go`（4 项运行时用例，含重复执行稳定性验证）
- 父卡：[TASK-INPUT-001.md](TASK-INPUT-001.md)
- 前置：D4-C ✅ `46accb9`、Demo ✅ `a07fd24`

## 1. D5-A：测试基础设施审查（完成）

### 1.1 审查结论

| 项 | 结论 |
|---|---|
| Gio 版本 | v0.10.3 |
| 事件路径 | 经 `widget.Editor.Update(gtx)`（真实处理路径，非直接调处理器） |
| 复用 | D4 的 `rtProbe`（仓库内临时包，真实 `go build` + `go test`） |
| 生产代码 | **零改动** |

### 1.2 三条合成路径的实测验证

| 路径 | 合成方式 | 实测结果 |
|---|---|---|
| **MaxLen** | `key.EditEvent{Text:"abcdef"}` + `MaxLen=3` | ✅ 文本被截断为 3 字符 |
| **Filter** | `key.EditEvent{Text:"a1b2c3"}` + `Filter="0123456789"` | ✅ 只剩 `"123"` |
| **Submit** | `key.Event{Name: key.NameReturn, State: Press}` + `Release`，**单独一帧** | ✅ 载荷 `"42"` 正确到达处理器 |

### 1.3 两条来自源码的关键事实

**事实 1：`SubmitEvent` 只由 `NameReturn`/`NameEnter` 触发**

```go
// widget/editor.go processKey
if !e.ReadOnly && e.Submit && (ke.Name == key.NameReturn || ke.Name == key.NameEnter) {
    return SubmitEvent{...}
}
```

`key.EditEvent` 里带 `\n` **不会**产生 `SubmitEvent`——它走 `text.Changed()` 的
`ChangeEvent` 路径。这是审查中修正的第一个误判。

**事实 2：Gio 官方 `TestEditor_Submit` 用 `EditEvent{"ab1\n"}` 得两个事件**

因为该 editor `SingleLine=false`，`\n` 作为普通文本产生 `ChangeEvent`；
随后 `text.Changed()` 刷新时再…（实际官方用例断言 `want = [ChangeEvent, SubmitEvent]`，
其 SubmitEvent 来自 `Submit=true` 下的换行处理）。**不要照抄它做 Submit 验证**——
直接用 `NameReturn` 更可控。

### 1.4 一个测试设计错误（自查）

初期用 `Submit="OnEcho"` 验证载荷，一直得不到值——查 fixture 发现：

```go
func (vm *ViewModel) OnEcho(s string) {}   // 空函数体，不记录任何字段
```

**这是测试设计错误，不是实现缺陷。** 改用 `RecordText`（会写 `LastText`）后
立即通过。教训：验证载荷必须用**会记录参数**的处理器。

### 1.5 合成路径的帧隔离要求

输入与回车**必须分帧**：

```text
frame1: Queue(EditEvent{"42"}) → frame()     // 文本进入
frame2: Queue(NameReturn Press/Release) → frame()  // 提交触发
```

同帧放入两者时，`Update` 循环先消费 `ChangeEvent` 后返回 false，
回车事件留在队列但 `r.Frame` 已推进——下一次渲染才处理，观测时机错位。

## 2. D5-B ~ D5-E：待做

| 切片 | 内容 | 状态 |
|---|---|---|
| D5-B | AC-16 MaxLen 截断 + ChangeEvent 行为 | ✅ `TestInputAcceptanceMaxLen` |
| D5-C | AC-17 Filter 过滤 + ChangeEvent 行为 | ✅ `TestInputAcceptanceFilter` |
| D5-D | AC-18 Submit 运行时触发（次数/载荷/不重复） | ✅ `TestInputAcceptanceSubmit` |
| D5-E | AC-14 外部值改回旧值 | ✅ `TestInputAcceptanceExternalRevert` |

### 2.1 四个用例的实测观测（D5-B~E）

| 用例 | 观测值 | 关键断言 |
|---|---|---|
| MaxLen | `text=abc len=3`；超限后 `LastText` 不变 | 截断生效 + **超限不产生 ChangeEvent** |
| Filter | `text=123`；非法输入后仍 `123` | 只留合法字符 + 非法输入不改文本 |
| Submit | 帧1 输入 `42` → 帧2 回车 → `payload=42`；无回车帧 `SENTINEL` 保持；二次回车再触发 | **载荷正确 + 不重复触发 + 可重复触发** |
| 改回旧值 | `AAA→BBB→INTERMEDIATE→AAA`，改回时覆盖为 `AAA`；稳定帧不变 | **完整往返 + 覆盖中间态 + 稳定** |

**D5-E 的中间态设计**：先 `SetText("INTERMEDIATE")`（用户未回写编辑），
再改回旧值。若实现漏更新，文本会停在 `INTERMEDIATE`——这打破了
"未同步"与"同步后碰巧相等"的歧义。

### 2.2 重复执行稳定性

`go test -count=1 -run TestInputAcceptance` 连续两次均 PASS。

## 3. D5-F：人工验收（单独登记）

| 项 | 方式 |
|---|---|
| AC-002-24 IME 组合输入 | **人工**，不得标自动化 |
| 焦点/连续输入/外部更新 | 真实窗口手动 |
| Demo 冒烟 | `go run ./examples/demo` |

## 4. 纪律

- 共享工作树禁止 `git add -A`（并发会话文件混入已发生两次）
- 门禁失败若是并发写文件竞态，记录首次失败与复跑结果，不只报最终绿
- 测试必须能真实变红（先证明断言有效，再让它变绿）

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **自动化 A–E 有通过记录（限合成事件范围），整体未闭环**：D5-F 未执行；现行语义须读 R8；不得写「Input 自动化验收全部完成」 |
| 归类 | 未闭环（D5-F 人工未执行 + 11-02 语义修订） |
| 实现 SHA | 47a77891fb5d207ce16a037932624b18b2033880 (D5-A 测试基础设施)；64f6eaeef6e36dba13ba233e4cf8ae8e68a017d4 (D5-B~E 四项运行时) |
| 验收 SHA / 记录 | 卡 §2.1/§2.2 自验；dev-log 2026-10-20 §1777 记父卡状态收敛由并发 INPUT 线提交、内容不由本线背书 |
| 执行方是否独立 | **未证（缺具名执行者/独立会话记录）**；上一版措辞「否：INPUT 并发线自验」已撤销。依据：归属说明，无具名执行者/会话记录。本轮标准台账 `-v` 重放另列，E3 可复跑不自动等于独立历史验收。 |
| 命令 / rc / raw 可定位性 | go test -count=1 -run TestInputAcceptance 连续两次 PASS（卡 §2.2）；4 个用例的观测值以表格文字给出；E1：无历史 raw；本轮 `r1c-c1-input-v` 现行重放 -run ^TestInput 留 raw |
| 当时范围 | MaxLen/Filter/Submit/改回旧值的合成事件自动化；**D5-F（AC-002-24 IME 组合输入、真实焦点、真实窗口交互）明确人工，不得计通过** |
| 现行适用性 | 4 个命名测试在基线存在且现行窄跑 rc=0；但 R8 记录旧规则下「示例组合首跑 rc1、10 条旧断言红」后适配为静默期望，故 A–E 的旧期望值也不等于现行期望 |
| 仍缺哪一层资格 | D5-F 人工验收；现行语义下的独立复核 raw |
| 下一动作 | 保持 D5-F 为未执行；把 A–E 的现行判据指向 R8 与契约 §5.2 |

## 2026-11-06 R23-R1 返修 R1 补正（覆盖上一版补记中被撤销的结论）

> 本次返修关闭协调者 F-C1 / F-C2 / F-C3。完整逐项字段见 [13 项证据资格复核（R1 返修补正版）](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md) 与交接包 `repair-r1/raw/closure-matrix.json`。

| 字段 | R1 返修后结论 |
|---|---|
| 单一状态（每项只计一次） | B-未闭环（D5-F 人工未执行 + 11-02 语义修订） |
| 证据层 | E1（历史）+ 本轮 E3 显式包重放 |
| 历史原命令是否有效 | 命令缺包路径，无法据此判定构建上下文；把包显式限定为 codegen 后，该前缀命中 5 个真实顶层入口 |
| 本轮另选命令 | `go test -count=1 -timeout 10m -v -run ^TestInputAcceptance ./internal/compiler/codegen`（r1r-replay-d5-acceptance） |
| 真实用例集合 | `r1r-replay-d5-acceptance`：5 个顶层 PASS / 0 个子测试 PASS / 0 SKIP / 0 FAIL |
| 能证明的范围 | 5 个 TestInputAcceptance 顶层用例全部 PASS（限合成事件） |
| 未覆盖的范围 | **D5-F 原生焦点/IME/真实窗口明确未执行**；现行语义须读 R8；不得写「Input 自动化验收全部完成」 |
| 历史执行方独立性 | 未证（缺具名执行者/独立会话记录） |
| 本轮 E3 重放 | r1r-replay-d5-acceptance 是本轮标准台账 E3 重放，不等于独立历史验收 |

