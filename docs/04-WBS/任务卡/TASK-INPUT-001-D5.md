# TASK-INPUT-001-D5：自动化验收（含测试基础设施审查）

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
