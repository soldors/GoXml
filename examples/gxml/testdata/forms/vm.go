// Package forms 是「受控输入 + 回车提交 + 外部值改回」入门工程的手写一半。
//
// 与 testdata/starter / testdata/tasks 的区别：
//   - 本例演示 <Input> 的**受控外部值**：`Value="{Text}"` 是 vm → Editor 的
//     单向同步（Input 输出契约 §3.1），回写只能经事件方法显式完成；
//   - ValueChanged 在**事件处理器**里准备预览串，不在 Layout 热路径拼接；
//   - Submit 只作为事件存在（契约 §2.3）：回车才产生 SubmitEvent；
//   - Clear 演示「外部值改回」：这里只改 ViewModel，编辑器的同步由下一帧的
//     同步算法完成（契约 §5.3 路径 1）——工程代码里没有任何一处碰编辑器。
//
// 后台写入需要调用方自行同步：Invalidate 的并发安全不等于字段读写安全。
package forms

import "strconv"

// DefaultText 是 <Input Value="{Text}"> 的初始外部值。
//
// Clear 把它改回来，用于演示「外部值改回 → 下一帧同步编辑器」。
// 工程层拿不到编辑器，因此**不能**在处理器里 SetText。
const DefaultText = "初始文本"

// ViewModel 由 UI 线程的事件处理器更新。
type ViewModel struct {
	// Text 是 <Input Value="{Text}"> 的外部值（契约 §5.1 的 V）。
	Text string

	// Preview 是 ValueChanged 里准备好的显示串。
	// 显示串在事件处理器准备，不在 Layout 拼接（性能预算 INV-PERF-2）。
	Preview string

	// SubmitCount / LastSubmitted 记录提交次数与当次文本。
	SubmitCount   int
	LastSubmitted string

	// Status 是条件显示分支的显示串；HasStatus 驱动 If/Then/Else。
	Status    string
	HasStatus bool

	// Changes / LastPayload 记录真实输入事件的回写次数与载荷。
	// 自动测试靠它区分「输入事件触发」与「直接调用 VM 方法」。
	//
	// 注意：**外部值应用也会带来一次 ChangeEvent**（实测，见 README
	// 「常见错误」第 4 条）：外部值同步先 SetText，紧随的 Editor.Update
	// 会因为缓冲区 Changed 标记被置位而产出一次 ChangeEvent。
	// 因此 Changes 反映的是「事件链路调用次数」，不等于「用户敲了几个字」。
	Changes     int
	LastPayload string

	// LastAction 记录最近一次事件，供自动测试断言点到了哪个按钮。
	//
	// 它会被后续事件覆盖：点「清空」之后紧接着的那次外部应用会再触发一次
	// ChangeEvent，LastAction 随即变成 "change"。要统计按钮点击次数请用 Clears。
	LastAction string

	// Clears 记录「清空」按钮被真实点击的次数（不受后续事件覆盖）。
	Clears int
}

// NewViewModel 构造初始状态：外部值、预览串都为首帧准备好。
func NewViewModel() *ViewModel {
	m := &ViewModel{Text: DefaultText}
	m.Preview = previewOf(m.Text)
	return m
}

// previewOf 在**事件处理器**里准备显示串（不在 Layout 拼接）。
func previewOf(text string) string {
	return "预览：" + text
}

// OnTextChanged 是 ValueChanged 的处理器：载荷 = 编辑器当前文本。
//
// 回写之后生成器会重新求值 V 并更新 lastApplied（契约 §5.2 路径 2），
// 所以这里只更新模型：不碰编辑器、不需要 SetText。
func (m *ViewModel) OnTextChanged(value string) {
	m.Text = value
	m.Changes++
	m.LastPayload = value
	m.Preview = previewOf(value)
	m.LastAction = "change"
}

// OnSubmit 是 Submit 的处理器：载荷 = SubmitEvent.Text。
//
// 只声明 Submit 不会自动回写 Text（契约 §4.2），所以「编辑中但未提交」
// 与「已提交」是可区分的两种状态——本例刻意不覆盖 Text。
func (m *ViewModel) OnSubmit(text string) {
	m.SubmitCount++
	m.LastSubmitted = text
	m.Status = "已提交 " + strconv.Itoa(m.SubmitCount) + " 次：" + text
	m.HasStatus = true
	m.LastAction = "submit"
}

// Clear 把**外部值**改回初始值：改的是 ViewModel，不是编辑器。
//
// 编辑器同步发生在下一帧（V != A → SetText，契约 §5.3 路径 1）。
// Clear 不增加 SubmitCount：清空往返不得重复提交。
func (m *ViewModel) Clear() {
	m.Text = DefaultText
	m.Preview = previewOf(m.Text)
	m.LastAction = "clear"
	m.Clears++
}

// HideStatus 切回 Else 分支（条件提示的反向切换）。
func (m *ViewModel) HideStatus() {
	m.HasStatus = false
	m.LastAction = "hide"
}
