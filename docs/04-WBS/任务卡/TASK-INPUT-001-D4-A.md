# TASK-INPUT-001-D4-A：Input 控件结构与事件接线

> 2026-11-05 状态阅读提示：下文是2026-10-20的历史实现/自验记录。
> 同步顺序与期望以2026-11-02修订的 [Input 输出契约](../../03-架构/Input输出契约.md) 为准；
> 负责人已批准用户编辑优先、程序化同步静默与归一化追随，不能把本卡旧断言当作当前语义。
> 自动化实现存在不等于D5-F原生窗口/真实焦点/IME人工验收完成，整体验收范围见 [父卡](TASK-INPUT-001.md)。


## 元信息

- 状态：**Done（2026-10-20）** | 证据：`internal/compiler/codegen/input.go` + `input_test.go`（12 项测试全过，含真实编译与 Button 回归）
- 父卡：[TASK-INPUT-001.md](TASK-INPUT-001.md)（D4 第一切片）
- 前置：D1 通用载荷 ✅ `6491774`、D3 Schema ✅ `64e5855`
- 契约依据：[Input输出契约.md](../../03-架构/Input输出契约.md) §2/§4/§6、
  [ADR-0011](../../03-架构/adr/ADR-0011-Input受控输入与事件分派.md) 决策 3/5

## 1. 范围（本切片做什么）

| # | 内容 |
|---|---|
| A1 | `<Input>` 生成 `widget.Editor` **实例字段**（每个 Input 一个，跨帧稳定） |
| A2 | 属性透传：`Hint`（material 第三参）、`ReadOnly`、`SingleLine`、`MaxLen`、`Filter` |
| A3 | **控件自持事件消费**：`Update(gtx)` 在 Input 自己的生成位置，**不进顶部轮询** |
| A4 | 事件接线：`ValueChanged` 载荷 = `editor.Text()`；`Submit` 载荷 = 提交事件的 `Text`（经 D1 的 `renderInvocationArgs`） |
| A5 | `Submit` 事件声明才设 `Editor.Submit = true`（未声明不得擅自开启） |
| A6 | **预留** `lastApplied` 字段 + 帧内执行位置（**不实现同步算法**） |

## 2. 明确不做（推到 D4-B）

- 外部值 `V` 与 `lastApplied` 的比较与 `SetText`
- 回写后重求值 `V` 并更新 `A`
- 光标保留的完整验证

**本切片允许 `Value` 完全不被消费**（生成代码不 SetText），
但**禁止**出现「每帧无条件 `SetText`」的临时实现——那会在 D4-B 被错误路径锁定。

## 3. 允许修改的文件

| 文件 | 改动 |
|---|---|
| `internal/compiler/codegen/codegen.go` | 新增 `case "Input"` 分支、generator 字段（input 槽位与提交事件状态） |
| `internal/compiler/codegen/input.go`（新建） | Input 生成逻辑 |
| `internal/compiler/codegen/input_test.go`（新建） | 结构断言 + 编译验证 |

**不得修改**：`payload.go`（D1 已验收）、`expander/schema.go`（D3 已验收）、
Parser/Analyzer、运行时包。

## 4. 实现要点（约束）

### 4.1 Editor 字段必须持久

```go
type GoxmlXView struct {
    vm      *VM
    input0  widget.Editor    // ← 实例字段，非 Layout 局部
    applied0 string          // ← lastApplied 占位（D4-B 使用）
}
```

### 4.2 事件消费位置

```go
// 在 Input 自己的生成位置（不是 Layout 顶部）：
func(gtx layout.Context) layout.Dimensions {
    ed := &v.input0
    // 消费本帧事件
    for {
        ev, ok := ed.Update(gtx)
        if !ok { break }
        switch ev.(type) {
        case widget.ChangeEvent:  v.vm.OnChange(ed.Text())
        case widget.SubmitEvent:  v.vm.OnSubmit(ev.(widget.SubmitEvent).Text)
        }
    }
    return material.Editor(th, ed, "hint").Layout(gtx)
}
```

**理由**：`Editor.Update` 依赖本帧 `Layout` 注册的焦点 tag 与 IME 上下文
（契约 §6.2）。提到顶部会让事件无法路由。

### 4.3 静态属性字面量必须校验

**A 阶段审计发现**：Analyzer 只校验**绑定**属性
（`analyzeElement` 里 `if !attr.Value.IsBinding { continue }`），
静态字面量完全不被校验。因此 Codegen 必须自己守卫：

| 属性 | 接受 | 拒绝 |
|---|---|---|
| `ReadOnly`/`SingleLine` | `true`/`false` | 其他 → 明确错误 |
| `MaxLen` | 非负整数 | 负数/非数字 → 明确错误 |
| `Hint`/`Filter` | 任意字面量 | 绑定值 → 明确错误（v1 只接受字面量） |
| `Enabled` | — | **一律报错**（沿用 Button 裁决） |

### 4.4 与既有机制的关系

- Input 是**动态节点**（`isInteractiveTag` 已含 `Input`）→ 不进静态固化
- `Value` **不得**进 `bindText` 槽位（契约 §8）
- Button 的顶部轮询**不受影响**

## 5. 测试命令与验收

```powershell
go test -run TestInputCodegen ./internal/compiler/codegen/
go build ./... ; go vet ./... ; go test ./...
```

| # | 验收 |
|---|---|
| 1 | 生成代码含 `input0 widget.Editor` 实例字段 |
| 2 | `Update(gtx)` 在 Input 位置；`Layout` 顶部**无** Editor 轮询 |
| 3 | `ValueChanged` 载荷为 `ed.Text()`；`Submit` 载荷为提交事件的 `Text` |
| 4 | 仅声明 Submit 时 `Editor.Submit = true`；未声明为 false |
| 5 | 属性透传正确（Hint 第三参、ReadOnly/SingleLine/MaxLen/Filter 赋值） |
| 6 | `Enabled` 报错；非法字面量（`ReadOnly="yes"`、`MaxLen="-1"`）报错 |
| 7 | 生成代码在临时包中**真实编译** |
| 8 | Button 生成结果不变（回归） |
| 9 | 含 Input 的页面不整页固化 |

## 6. 禁止事项

- ❌ 每帧无条件 `SetText`
- ❌ 把 Input 事件塞进顶部轮询
- ❌ 在 D4-A 声称 lastApplied 算法已完成
- ❌ 修改 D1/D3 已验收的代码

---

## 7. 实现记录

### 7.1 生成形态（实测）

```go
type GoxmlDashboardView struct {
	vm       *fixture.ViewModel
	input0   widget.Editor   // A1：实例字段，跨帧稳定
	applied0 string          // A6：lastApplied 占位（D4-B 使用）
}

// 在 Input 自己的位置（不是 Layout 顶部）：
return func() layout.Dimensions {
	ed := &v.input0
	ed.Submit = true          // A5：仅 Submit 声明时
	ed.SingleLine = false     // A2：属性透传
	ed.ReadOnly = true
	ed.MaxLen = 20
	ed.Filter = "abc"
	for {                     // A3：控件自持消费
		ev, ok := ed.Update(gtx)
		if !ok { break }
		switch e := ev.(type) {
		case widget.ChangeEvent:
			v.vm.OnNameChanged(ed.Text())   // A4：载荷 = 编辑器文本
		case widget.SubmitEvent:
			v.vm.OnEcho(e.Text)             // A4：载荷 = 事件文本（不复用 Change）
		}
	}
	return material.Editor(th, ed, "请输入").Layout(gtx)
}()
```

### 7.2 A 阶段审计发现的额外缺口

**Analyzer 不校验静态属性字面量**：`analyzeElement` 里
`if !attr.Value.IsBinding { continue }`——非绑定属性直接跳过。
因此 Codegen 必须自己守卫，否则会生成 `ed.ReadOnly = yes` 这种坏代码。

本切片新增三个守卫：`boolAttr`（只接受 true/false）、
`intAttr`（非负整数）、`Hint`/`Filter` 拒绝绑定值。

### 7.3 验收（12 项）

| 测试 | 内容 |
|---|---|
| TestInputCodegenStructure | 字段/属性透传/Hint 第三参 |
| **TestInputNoEventsNoUpdate** | 无事件不生成消费块（契约 §4.2 只读镜像） |
| TestInputWithEventHasUpdate | 有事件则生成 Update + ChangeEvent 分支 |
| **TestInputNotPolledAtTop** | 顶部无 ed.Update/ChangeEvent（**自持消费**） |
| TestInputChangePayloadIsEditorText | Change 载荷 = `ed.Text()` |
| **TestInputSubmitPayloadIsEventText** | Submit 载荷 = `e.Text`，且**不等于** Change 的 |
| TestInputSubmitEnablesEditorSubmit | 声明才 `ed.Submit=true`（两子例） |
| TestInputRejectsEnabled | Enabled 报错 |
| TestInputRejectsBadLiterals | 4 个非法字面量子例 |
| TestInputGeneratedCodeCompiles | **真实编译并渲染**（含两个事件） |
| TestInputPageNotFrozen | 不整页固化 |
| **TestButtonPollingUnaffected** | Button 仍在顶部轮询，Input 不在 |

### 7.4 明确未做（D4-B）

- `Value` 完全未消费——生成代码**不调用 SetText**
- `applied0` 只是占位字段，未被读写
- 未实现外部值比较、回写后重求值、光标保留验证

**这符合切片约束：不出现「每帧无条件 SetText」的临时实现。**

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **结构接线有通过记录（现行可重放），同步语义不适用旧卡**：A1–A5 仍有效；A6 的 lastApplied 占位与旧断言属于被取代的历史 |
| 归类 | 未闭环（现行语义以 R8 为准，D5-F 未执行） |
| 实现 SHA | 02b8593bba2d311ac7af4982f5394de9590a110a |
| 验收 SHA / 记录 | 卡 §7.3 自验；无独立验收提交 |
| 执行方是否独立 | **未证（缺具名执行者/独立会话记录）**；上一版措辞「否：dev-log 2026-10-20 §21.5/§22.7 明确「并发线（Codegen）提交…仅登记提交号与标题，内容不由本线背书」」已撤销。依据：dev-log 记录的是并发线归属，不是独立验收会话证据。本轮标准台账 `-v` 重放另列，E3 可复跑不自动等于独立历史验收。 |
| 命令 / rc / raw 可定位性 | go test -run TestInputCodegen ./internal/compiler/codegen/；go build ./...；go vet ./...；go test ./...（卡 §5，无逐条 rc）；E1：无历史 raw。本轮 `r1c-c1-input-v` 在现行基线重放 -run ^TestInput 全族并留 stdout.raw |
| 当时范围 | Editor 实例字段、属性透传、控件自持消费、Change/Submit 载荷、仅声明 Submit 才开启；明确不实现同步算法 |
| 现行适用性 | 12 个命名测试在基线存在、现行窄跑 rc=0；2026-11-02 `3c969f0`/`e200ace` 改了 input.go 46 行并新增 input_adoption_test.go / input_candidatesem_test.go，A1–A5 结构仍成立，但事件/同步期望须按契约 §5.2 新规则读 |
| 仍缺哪一层资格 | 独立复核、历史 raw、与 11-02 规则的逐断言对照 |
| 下一动作 | 以契约 §5.2 + `input_adoption_test.go` 为现行判据；不要在总览里写「D4-A 已完成全部 Input 验收」 |

## 2026-11-06 R23-R1 返修 R1 补正（覆盖上一版补记中被撤销的结论）

> 本次返修关闭协调者 F-C1 / F-C2 / F-C3。完整逐项字段见 [13 项证据资格复核（R1 返修补正版）](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md) 与交接包 `repair-r1/raw/closure-matrix.json`。

| 字段 | R1 返修后结论 |
|---|---|
| 单一状态（每项只计一次） | B-未闭环（现行语义以 R8 为准） |
| 证据层 | E1（历史）→ 本轮 E3 窄重放 |
| 历史原命令是否有效 | `TestInputCodegen` 是有效前缀，在 codegen 包命中 1 个真实入口 `TestInputCodegenStructure`；非空洞 |
| 本轮另选命令 | `go test -count=1 -timeout 10m -v -run ^TestInput ./internal/compiler/codegen`（r1r-replay-input） |
| 真实用例集合 | `r1r-replay-input`：41 个顶层 PASS / 22 个子测试 PASS / 0 SKIP / 0 FAIL |
| 能证明的范围 | Input 族 41 个顶层用例 + 22 个子测试全部 PASS（63 条 PASS 行） |
| 未覆盖的范围 | 同步语义（已被 2026-11-02 用户编辑优先规则取代）、D5-F 原生焦点/IME、真实窗口 |
| 历史执行方独立性 | 未证（缺具名执行者/独立会话记录） |
| 本轮 E3 重放 | r1r-replay-input 是本轮标准台账 E3 重放，不等于独立历史验收 |
