package widgets

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"
)

// colScrimTint 是 Modal 遮罩的半透明压暗（纯绘制，不影响事件语义）。
var colScrimTint = color.NRGBA{A: 0x55}

// Modal 是居中内容 + 全屏遮罩的模态层。零值可用（设置 Content 后 Open）。
//
// 候选默认行为（单列待裁决，见包注释）：
//   - 遮罩点击不关闭：ScrimClicked() 单次读取，处置权留给调用方；
//   - Escape 关闭：EscapeRequested() 单次读取；Escape 经约定路径（无
//     Focus 约束的过滤器）可达，内容持有的焦点不被逐帧强抢；
//   - 打开转换时请求一次键盘焦点（内容随后取得的焦点保持不动）；
//   - 关闭（Close 或 Escape）后于下一帧 Layout 把焦点交还
//     ReturnFocusTo（若设置）。焦点回交在 Layout 内执行——Close() 之后
//     仍需调用至少一帧 Layout 才会生效。
type Modal struct {
	// Content 在遮罩之上居中渲染；nil 时只渲染遮罩。
	Content func(gtx layout.Context, th *material.Theme) layout.Dimensions
	// ReturnFocusTo 是关闭后接收键盘焦点的标签（候选能力，可为 nil）。
	ReturnFocusTo event.Tag

	open    bool
	tag     overlayTag
	scrimSt scrimState // 遮罩跨帧按压状态（F-B1）
	focusSt focusState // 打开转换焦点请求（F-B2）

	scrimClickedPending bool
	escapePending       bool
	returnFocusPending  bool
}

// NewModal 构造 Modal。
func NewModal(content func(gtx layout.Context, th *material.Theme) layout.Dimensions) *Modal {
	m := &Modal{Content: content}
	m.tag.owner = m
	return m
}

// Open 打开 Modal；已打开时为 no-op。每次打开是新的焦点请求转换。
func (m *Modal) Open() {
	m.open = true
	m.focusSt.requested = false
}

// Close 关闭 Modal；焦点回交延迟到下一次 Layout（见类型注释）。
func (m *Modal) Close() {
	if !m.open {
		return
	}
	m.open = false
	m.returnFocusPending = true
}

// Opened 报告 Modal 当前是否打开。
func (m *Modal) Opened() bool { return m.open }

// ScrimClicked 单次读取「本帧遮罩被完整点击」；候选默认不因此关闭。
func (m *Modal) ScrimClicked() bool {
	v := m.scrimClickedPending
	m.scrimClickedPending = false
	return v
}

// EscapeRequested 单次读取「本帧 Escape 触发」（默认已随之关闭）。
func (m *Modal) EscapeRequested() bool {
	v := m.escapePending
	m.escapePending = false
	return v
}

// Layout 布局 Modal。关闭时执行待定的焦点回交并返回零尺寸；打开时依次：
// 遮罩事件区（阻断背景）→ Escape → 遮罩压暗绘制 → 居中内容。
func (m *Modal) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !m.open {
		m.returnFocusIfPending(gtx)
		return layout.Dimensions{}
	}

	// 1. 遮罩：全屏先声明区，阻断背景事件；点击不关闭（候选默认）。
	if m.scrimSt.layout(gtx, &m.tag) {
		m.scrimClickedPending = true
	}
	// 2. Escape 关闭：仅打开转换请求一次焦点；Escape 经约定路径可达
	// （焦点可在内容上）。
	if m.focusSt.layout(gtx, &m.tag, !m.focusSt.requested) {
		m.escapePending = true
		m.open = false
		m.returnFocusPending = true
	}
	if !m.open {
		m.returnFocusIfPending(gtx)
		return layout.Dimensions{}
	}

	// 3. 遮罩压暗（视觉提示，纯绘制）。
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops).Pop()
	paint.FillShape(gtx.Ops, colScrimTint, clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Op())

	// 4. 居中内容：宏录制一次、按内容尺寸居中回放（内容内的命中区随
	// 回放偏移一次性注册）。偏移必须 Push/Pop 作用域配对——裸 Add 会把
	// 变换泄漏给后续同级布局（R1 修复 F-B3）。
	if m.Content == nil {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	macro := op.Record(gtx.Ops)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: gtx.Constraints.Max}
	dims := m.Content(cgtx, th)
	call := macro.Stop()

	pos := centerPos(gtx.Constraints.Max, dims.Size)
	trans := op.Offset(pos).Push(gtx.Ops)
	call.Add(gtx.Ops)
	trans.Pop()
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// returnFocusIfPending 在关闭后的 Layout 帧执行焦点回交。
func (m *Modal) returnFocusIfPending(gtx layout.Context) {
	if !m.returnFocusPending {
		return
	}
	m.returnFocusPending = false
	if m.ReturnFocusTo != nil {
		gtx.Execute(key.FocusCmd{Tag: m.ReturnFocusTo})
	}
}

// centerPos 返回把 size 内容放进 viewport 的居中左上角（不越界、不为负）。
func centerPos(viewport, size image.Point) image.Point {
	pos := image.Pt((viewport.X-size.X)/2, (viewport.Y-size.Y)/2)
	if pos.X < 0 {
		pos.X = 0
	}
	if pos.Y < 0 {
		pos.Y = 0
	}
	return pos
}
