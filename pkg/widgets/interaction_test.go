package widgets

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// B2 真实交互测试：离屏 input.Router + 指针/键盘事件队列 + 帧循环，
// 不启动原生窗口（无 app.Window、无 GPU）。事件按 Gio 窗口同款顺序
// 驱动：Queue(事件) → Layout（对上一帧注册的命中区消费事件）→
// Router.Frame(ops)（收集本帧命中区）。

// keyConsumer 是焦点观察哨：记录发往自己标签的按键，用于验证 Modal
// 关闭后的焦点回交（候选能力）。
type keyConsumer struct {
	tag overlayTag
	got []key.Name
	// names 非空时按名注册过滤器（不收 catch-all 按键）；空时收全部。
	names []key.Name
	// requestFocus 为 true 时每帧请求焦点（测试焦点竞争用）。
	requestFocus bool
}

func (k *keyConsumer) layout(gtx layout.Context) {
	// 焦点持有资格：focusable 由 key.FocusFilter 授予，visible 由输入
	// 区（clip 内 event.Op）授予——二者缺一，帧末焦点都会被摘除。
	defer clip.Rect(image.Rectangle{Max: image.Pt(10, 10)}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &k.tag)
	gtx.Source.Event(key.FocusFilter{Target: &k.tag})
	if k.requestFocus {
		gtx.Execute(key.FocusCmd{Tag: &k.tag})
	}
	if len(k.names) == 0 {
		for {
			ev, ok := gtx.Source.Event(key.Filter{Focus: &k.tag})
			if !ok {
				break
			}
			if e, isKey := ev.(key.Event); isKey {
				k.got = append(k.got, e.Name)
			}
		}
		return
	}
	for _, n := range k.names {
		for {
			ev, ok := gtx.Source.Event(key.Filter{Focus: &k.tag, Name: n})
			if !ok {
				break
			}
			if e, isKey := ev.(key.Event); isKey {
				k.got = append(k.got, e.Name)
			}
		}
	}
}

type overlayHarness struct {
	t      *testing.T
	r      input.Router
	th     *material.Theme
	ops    *op.Ops
	gtx    layout.Context
	size   image.Point
	frames int

	background *widget.Clickable
	bgClicks   int

	// overlay 是被测组件的布局入口（背景之后布局，被遮罩压住）。
	overlay func(gtx layout.Context)
}

func newOverlayHarness(t *testing.T, w, hh int) *overlayHarness {
	h := &overlayHarness{
		t:          t,
		th:         material.NewTheme(),
		size:       image.Pt(w, hh),
		background: new(widget.Clickable),
	}
	return h
}

// frame 驱动一帧：可选事件入队 → 背景 → 被测组件 → Router.Frame。
func (h *overlayHarness) frame(events ...event.Event) {
	h.frames++
	h.r.Queue(events...)
	h.ops = new(op.Ops)
	h.gtx = layout.Context{
		Ops:         h.ops,
		Source:      h.r.Source(),
		Constraints: layout.Exact(h.size),
	}
	// 背景按钮：铺满整窗，先声明（遮罩组件之后布局，应被遮罩压住）。
	// Gio 惯用法：先消费 Clicked 再 Layout（Layout 内部的 update 会
	// 消费并丢弃点击）。
	for h.background.Clicked(h.gtx) {
		h.bgClicks++
	}
	bgGtx := h.gtx
	bgGtx.Constraints = layout.Exact(h.size)
	material.ButtonLayout(h.th, h.background).Layout(bgGtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: gtx.Constraints.Min}
		})
	if h.overlay != nil {
		h.overlay(h.gtx)
	}
	h.r.Frame(h.ops)
}

// click 模拟一次左键按下+释放（同一帧内成对入队，Gio 窗口同款）。
func (h *overlayHarness) click(x, y int) {
	h.frame(
		pointer.Event{
			Kind:     pointer.Press,
			Position: f32Pt(x, y),
			Source:   pointer.Mouse,
			Buttons:  pointer.ButtonPrimary,
		},
		pointer.Event{
			Kind:     pointer.Release,
			Position: f32Pt(x, y),
			Source:   pointer.Mouse,
			Buttons:  pointer.ButtonPrimary,
		},
	)
}

// escape 模拟一次 Escape 按下（键事件需要焦点，打开帧已请求）。
func (h *overlayHarness) escape() {
	h.frame(key.Event{Name: key.NameEscape, State: key.Press})
}

func (h *overlayHarness) resize(w, hh int) { h.size = image.Pt(w, hh) }

// press/release/cancel 拆帧事件（R1：真实输入中 Press 与 Release 分属两帧，
// 且 Release 的 Buttons 是释放后的剩余按钮——主键释放后为 0）。
func (h *overlayHarness) press(x, y int) {
	h.frame(pointer.Event{
		Kind:     pointer.Press,
		Position: f32Pt(x, y),
		Source:   pointer.Mouse,
		Buttons:  pointer.ButtonPrimary,
	})
}

func (h *overlayHarness) release(x, y int) {
	h.frame(pointer.Event{
		Kind:     pointer.Release,
		Position: f32Pt(x, y),
		Source:   pointer.Mouse,
	})
}

func (h *overlayHarness) drag(x, y int) {
	// 按住移动入队为 Move（Router 对按压中的指针生成 Drag）。
	h.frame(pointer.Event{
		Kind:     pointer.Move,
		Position: f32Pt(x, y),
		Source:   pointer.Mouse,
		Buttons:  pointer.ButtonPrimary,
	})
}

func f32Pt(x, y int) f32.Point { return f32.Point{X: float32(x), Y: float32(y)} }

func rectCenter(r image.Rectangle) image.Point {
	return image.Pt((r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2)
}

// --- ContextMenu ---

func TestContextMenuOutsideClickClosesAndBlocksBackground(t *testing.T) {
	menu := NewContextMenu(
		MenuItem{Title: "剪切", Action: "cut"},
		MenuItem{Title: "粘贴", Action: "paste"},
	)
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }

	h.frame() // 建立命中区
	menu.OpenAt(image.Pt(20, 20))
	h.frame() // 打开帧：注册遮罩/面板
	if !menu.Opened() {
		t.Fatal("打开帧后菜单应保持打开")
	}

	h.click(280, 180) // 外点：在遮罩上、面板外
	if menu.Opened() {
		t.Fatal("外点应关闭菜单")
	}
	if h.bgClicks != 0 {
		t.Fatalf("遮罩应阻断背景按钮，背景点击数 = %d", h.bgClicks)
	}
	if _, ok := menu.Clicked(); ok {
		t.Fatal("外点不应产生条目动作")
	}

	h.frame()
	h.click(280, 180) // 菜单已关：同一位置应打到背景按钮
	if h.bgClicks != 1 {
		t.Fatalf("菜单关闭后背景按钮应恢复可点击，点击数 = %d", h.bgClicks)
	}
}

func TestContextMenuItemClickSelectsOnceAndCloses(t *testing.T) {
	menu := NewContextMenu(
		MenuItem{Title: "剪切", Action: "cut"},
		MenuItem{Title: "粘贴", Action: "paste"},
	)
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }

	h.frame()
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	item0 := menu.ItemRect(0)
	if item0.Empty() {
		t.Fatal("条目 0 应有非零命中区")
	}
	center := rectCenter(item0)
	h.click(center.X, center.Y)

	if menu.Opened() {
		t.Fatal("选中条目应关闭菜单")
	}
	action, ok := menu.Clicked()
	if !ok || action != "cut" {
		t.Fatalf("应选中 cut，得到 action=%q ok=%v", action, ok)
	}
	if _, ok := menu.Clicked(); ok {
		t.Fatal("Clicked 必须单次触发：第二次读取不应有动作")
	}
	if h.bgClicks != 0 {
		t.Fatalf("菜单条目点击不应穿透到背景，背景点击数 = %d", h.bgClicks)
	}
}

func TestContextMenuDisabledItemIgnoresClick(t *testing.T) {
	menu := NewContextMenu(
		MenuItem{Title: "剪切", Action: "cut"},
		MenuItem{Title: "删除", Action: "del", Disabled: true},
	)
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }

	h.frame()
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	center := rectCenter(menu.ItemRect(1)) // 禁用条目
	h.click(center.X, center.Y)
	if menu.Opened() == false {
		t.Fatal("点击禁用条目不应关闭菜单（候选默认）")
	}
	if _, ok := menu.Clicked(); ok {
		t.Fatal("禁用条目不应产生动作")
	}

	center = rectCenter(menu.ItemRect(0)) // 启用条目仍可用
	h.click(center.X, center.Y)
	action, ok := menu.Clicked()
	if !ok || action != "cut" {
		t.Fatalf("启用条目应可选中，得到 action=%q ok=%v", action, ok)
	}
	if menu.Opened() {
		t.Fatal("选中启用条目应关闭菜单")
	}
}

func TestContextMenuEscapeClosesAndReopenWorks(t *testing.T) {
	menu := NewContextMenu(MenuItem{Title: "剪切", Action: "cut"})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }

	h.frame()
	menu.OpenAt(image.Pt(20, 20))
	h.frame()
	h.escape()
	if menu.Opened() {
		t.Fatal("Escape 应关闭菜单（候选默认）")
	}

	// 重新打开后指针交互必须完好（焦点不残留、几何重算）。
	h.frame()
	menu.OpenAt(image.Pt(50, 60))
	h.frame()
	center := rectCenter(menu.ItemRect(0))
	h.click(center.X, center.Y)
	if action, ok := menu.Clicked(); !ok || action != "cut" {
		t.Fatalf("重开后应可选中条目，得到 action=%q ok=%v", action, ok)
	}
}

func TestContextMenuGeometryFollowsConstraints(t *testing.T) {
	menu := NewContextMenu(
		MenuItem{Title: "剪切", Action: "cut"},
		MenuItem{Title: "粘贴", Action: "paste"},
	)
	h := newOverlayHarness(t, 200, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }

	h.frame()
	menu.OpenAt(image.Pt(190, 190)) // 右下角越界锚点
	h.frame()
	r := menu.Rect()
	if r.Min.X < 0 || r.Min.Y < 0 || r.Max.X > 200 || r.Max.Y > 200 {
		t.Fatalf("面板必须被钳进视口，得到 %v", r)
	}
	center := rectCenter(menu.ItemRect(0))
	h.click(center.X, center.Y)
	if _, ok := menu.Clicked(); !ok {
		t.Fatal("钳制后的条目应可选中（无跨状态缓存坐标）")
	}

	// 视口变大：不重新 OpenAt，几何应按新约束与原锚点重算。
	h.resize(400, 400)
	h.frame()
	menu.OpenAt(image.Pt(190, 190))
	h.frame()
	r2 := menu.Rect()
	if r2.Max.X > 400 || r2.Max.Y > 400 {
		t.Fatalf("新视口下面板应重算，得到 %v", r2)
	}
	if r2 == r {
		t.Fatalf("视口变化后面板几何应变化：旧 %v 新 %v", r, r2)
	}
	center = rectCenter(menu.ItemRect(0))
	h.click(center.X, center.Y)
	if _, ok := menu.Clicked(); !ok {
		t.Fatal("新几何下的条目应可选中")
	}
}

func TestDualContextMenuInstancesIndependent(t *testing.T) {
	m1 := NewContextMenu(MenuItem{Title: "A1", Action: "a1"})
	m2 := NewContextMenu(MenuItem{Title: "B1", Action: "b1"})
	h := newOverlayHarness(t, 400, 300)
	h.overlay = func(gtx layout.Context) {
		m1.Layout(gtx, h.th)
		m2.Layout(gtx, h.th)
	}

	h.frame()
	m1.OpenAt(image.Pt(10, 10))
	m2.OpenAt(image.Pt(220, 10))
	h.frame()

	// 真实 Gio 层级语义：两个独立实例同帧打开时，后声明（m2）的全屏
	// 遮罩压住先声明（m1）的一切——点击 m1 条目区域会被 m2 遮罩接收，
	// 判为 m2 的外点而关闭 m2；m1 不受影响、也无动作。实例间无全局
	// 协调（本包无全局活跃菜单），互斥须由调用方保证（候选边界，
	// 记录于 scope-matrix.md）。
	c1 := rectCenter(m1.ItemRect(0))
	h.click(c1.X, c1.Y)
	if _, ok := m1.Clicked(); ok {
		t.Fatal("被 m2 遮罩覆盖的实例 1 不应收到点击（后声明遮罩获胜）")
	}
	if !m1.Opened() {
		t.Fatal("实例 1 不应受实例 2 的外点影响")
	}
	if m2.Opened() {
		t.Fatal("实例 2 应把落在自己遮罩上的点击判为外点而关闭")
	}

	// m2 关闭后 m1 恢复为最顶层：条目可选中。
	h.frame()
	h.click(c1.X, c1.Y)
	if action, ok := m1.Clicked(); !ok || action != "a1" {
		t.Fatalf("m2 关闭后实例 1 应可选中，得到 action=%q ok=%v", action, ok)
	}
	if m1.Opened() {
		t.Fatal("实例 1 选中后应关闭")
	}

	// 状态不串扰：重新独立打开/关闭互不影响。
	h.frame()
	m1.OpenAt(image.Pt(10, 10))
	h.frame()
	m1.Close()
	m2.OpenAt(image.Pt(220, 10))
	if m1.Opened() || !m2.Opened() {
		t.Fatalf("实例状态不应串扰：m1=%v m2=%v", m1.Opened(), m2.Opened())
	}
}

// --- Modal ---

func TestModalBlocksBackgroundAndStaysOpenOnScrimClick(t *testing.T) {
	var okClicks int
	okBtn := new(widget.Clickable)
	modal := NewModal(func(gtx layout.Context, th *material.Theme) layout.Dimensions {
		for okBtn.Clicked(gtx) {
			okClicks++
		}
		btn := material.Button(th, okBtn, "OK")
		return btn.Layout(gtx)
	})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { modal.Layout(gtx, h.th) }

	h.frame()
	modal.Open()
	h.frame()
	if !modal.Opened() {
		t.Fatal("打开帧后 Modal 应保持打开")
	}

	h.click(15, 185) // 左下角：遮罩上、内容外
	if h.bgClicks != 0 {
		t.Fatalf("遮罩应阻断背景按钮，背景点击数 = %d", h.bgClicks)
	}
	if !modal.Opened() {
		t.Fatal("候选默认：遮罩点击不应关闭 Modal")
	}
	if !modal.ScrimClicked() {
		t.Fatal("ScrimClicked 应报告本次遮罩点击")
	}
	if modal.ScrimClicked() {
		t.Fatal("ScrimClicked 必须单次读取")
	}

	// 内容按钮可点击（在遮罩之上），且不关闭 Modal。
	// 内容居中于 300x200 窗口；直接点窗口中心命中最稳。
	h.click(150, 100)
	if okClicks != 1 {
		t.Fatalf("内容按钮应被点击一次，得到 %d", okClicks)
	}
	if !modal.Opened() {
		t.Fatal("点击内容不应关闭 Modal（候选默认）")
	}
	if modal.ScrimClicked() {
		t.Fatal("内容点击不应被记为遮罩点击")
	}
}

func TestModalEscapeClosesReturnsFocusAndRestoresBackground(t *testing.T) {
	consumer := &keyConsumer{}
	var m Modal
	m.Content = func(gtx layout.Context, th *material.Theme) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(80, 60)}
	}
	m.ReturnFocusTo = &consumer.tag
	m.tag.owner = &m

	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) {
		consumer.layout(gtx)
		m.Layout(gtx, h.th)
	}

	h.frame()
	m.Open()
	h.frame()
	h.escape()
	if m.Opened() {
		t.Fatal("Escape 应关闭 Modal（候选默认）")
	}
	if !m.EscapeRequested() {
		t.Fatal("EscapeRequested 应报告本次 Escape")
	}
	if m.EscapeRequested() {
		t.Fatal("EscapeRequested 必须单次读取")
	}

	// 焦点回交：下一帧向回交标签发一个普通键，观察哨应收到。
	h.frame(key.Event{Name: "a", State: key.Press})
	if len(consumer.got) == 0 {
		t.Fatal("Modal 关闭后焦点应回交 ReturnFocusTo（候选能力）")
	}

	// 背景按钮恢复可点击。
	h.click(150, 100)
	if h.bgClicks != 1 {
		t.Fatalf("Modal 关闭后背景按钮应恢复可点击，点击数 = %d", h.bgClicks)
	}
}
