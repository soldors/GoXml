package widgets

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget/material"
)

// R1 持久回归：逐条对应协调者 review-1 的 F-B1..F-B4 判据（探针源码在
// coordinator/review-1/probes/b_test.go，历史证据一字不改；本文件是
// 修复后的自有持久版本，判据不降级）。

// --- F-B1：跨帧点击与真实 Release 状态 ---

func TestR1OutsideClickAcrossFrames(t *testing.T) {
	menu := NewContextMenu(MenuItem{Title: "one", Action: "one"})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	// 帧内单独 Press：不得立即关闭。
	h.press(290, 190)
	if !menu.Opened() {
		t.Fatal("仅 Press 一帧不应关闭菜单")
	}
	// 下一帧 Release（Buttons=0，真实宿主形态）：必须关闭。
	h.release(290, 190)
	if menu.Opened() {
		t.Fatal("跨帧真实 Press/Release 外点未关闭菜单")
	}
	if h.bgClicks != 0 {
		t.Fatalf("遮罩应阻断背景按钮，背景点击数 = %d", h.bgClicks)
	}
}

func TestR1ReleaseButtonsZeroSameFrame(t *testing.T) {
	menu := NewContextMenu(MenuItem{Title: "one", Action: "one"})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	// 同帧成对，但 Release 带真实形态 Buttons=0：必须关闭。
	h.frame(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32Pt(290, 190)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32Pt(290, 190)},
	)
	if menu.Opened() {
		t.Fatal("外点 Release Buttons=0 未关闭菜单")
	}
}

func TestR1PressThenCancelKeepsOpen(t *testing.T) {
	menu := NewContextMenu(MenuItem{Title: "one", Action: "one"})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	h.press(290, 190)
	h.frame(pointer.Event{Kind: pointer.Cancel, Source: pointer.Mouse})
	if !menu.Opened() {
		t.Fatal("Press→Cancel 不应关闭菜单")
	}
	if _, ok := menu.Clicked(); ok {
		t.Fatal("被 Cancel 的按压不得产生遮罩点击")
	}
	// Cancel 后新的一次完整点击仍然有效。
	h.press(290, 190)
	h.release(290, 190)
	if menu.Opened() {
		t.Fatal("Cancel 后的新外点应正常关闭菜单")
	}
}

func TestR1PressDragReleaseAcrossFramesCloses(t *testing.T) {
	menu := NewContextMenu(MenuItem{Title: "one", Action: "one"})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	h.press(290, 190)   // 遮罩上按下
	h.drag(200, 150)    // 遮罩区内拖动（仍在遮罩上）
	h.release(200, 150) // 遮罩区内释放（Buttons=0）
	if menu.Opened() {
		t.Fatal("遮罩区内按下-拖动-释放的外点应关闭菜单")
	}
	if h.bgClicks != 0 {
		t.Fatalf("拖动期间背景不得收到点击，背景点击数 = %d", h.bgClicks)
	}
}

func TestR1PressOnScrimReleaseOverPanelCloses(t *testing.T) {
	// 契约（无 grab 的遮罩）：按压起点在遮罩上，释放无论落在面板/条目上
	// 还是遮罩上，都视为一次完整外点——菜单关闭、无动作（不会误选条目）。
	menu := NewContextMenu(MenuItem{Title: "one", Action: "one"})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	h.press(290, 190)
	center := rectCenter(menu.ItemRect(0))
	h.release(center.X, center.Y)
	if menu.Opened() {
		t.Fatal("遮罩按压后的释放（落点在面板上）应视为外点关闭菜单")
	}
	if _, ok := menu.Clicked(); ok {
		t.Fatal("外点释放不得产生条目动作")
	}
	if h.bgClicks != 0 {
		t.Fatalf("全程背景不得收到点击，背景点击数 = %d", h.bgClicks)
	}
}

func TestR1ItemClickAcrossFrames(t *testing.T) {
	// 条目点击同样必须支持真实分帧：Press 一帧、Release 下一帧。
	menu := NewContextMenu(MenuItem{Title: "剪切", Action: "cut"})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(20, 20))
	h.frame()

	center := rectCenter(menu.ItemRect(0))
	h.press(center.X, center.Y)
	if action, ok := menu.Clicked(); ok {
		t.Fatalf("仅 Press 不应产生动作，得到 %q", action)
	}
	h.release(center.X, center.Y)
	if menu.Opened() {
		t.Fatal("跨帧条目点击应关闭菜单")
	}
	if action, ok := menu.Clicked(); !ok || action != "cut" {
		t.Fatalf("跨帧条目点击应选中 cut，得到 action=%q ok=%v", action, ok)
	}
}

// --- F-B2：焦点只在打开转换请求；内容焦点/Escape/回交同时可用 ---

func TestR1ModalKeepsContentFocus(t *testing.T) {
	h := newOverlayHarness(t, 300, 200)
	inside := &keyConsumer{}
	first := true
	modal := NewModal(nil)
	modal.Content = func(gtx layout.Context, _ *material.Theme) layout.Dimensions {
		inside.layout(gtx)
		if first {
			gtx.Execute(key.FocusCmd{Tag: &inside.tag})
			first = false
		}
		return layout.Dimensions{Size: image.Pt(80, 40)}
	}
	h.overlay = func(gtx layout.Context) { modal.Layout(gtx, h.th) }
	modal.Open()
	h.frame()
	if !h.r.Source().Focused(&inside.tag) {
		t.Fatal("对照：内容首帧未取得焦点")
	}
	h.frame()
	if !h.r.Source().Focused(&inside.tag) {
		t.Fatal("Modal 在无新 Open 的下一帧夺走内容键盘焦点")
	}
}

func TestR1ModalContentFocusEscapeAndReturnTogether(t *testing.T) {
	h := newOverlayHarness(t, 300, 200)
	consumer := &keyConsumer{names: []key.Name{"x", "y"}}
	first := true
	var m Modal
	m.ReturnFocusTo = &consumer.tag
	m.tag.owner = &m
	m.Content = func(gtx layout.Context, _ *material.Theme) layout.Dimensions {
		consumer.layout(gtx)
		if first {
			gtx.Execute(key.FocusCmd{Tag: &consumer.tag})
			first = false
		}
		return layout.Dimensions{Size: image.Pt(80, 40)}
	}
	h.overlay = func(gtx layout.Context) { consumer.layout(gtx); m.Layout(gtx, h.th) }

	h.frame()
	m.Open()
	h.frame()
	if !h.r.Source().Focused(&consumer.tag) {
		t.Fatal("内容应持有焦点")
	}

	// 内容持焦时真实键盘输入可达（Modal 不拦截内容输入）。
	h.frame(key.Event{Name: "x", State: key.Press})
	gotX := false
	for _, n := range consumer.got {
		if n == "x" {
			gotX = true
		}
	}
	if !gotX {
		t.Fatalf("内容应收到真实键盘输入 x，得到 %v", consumer.got)
	}

	// Escape 经约定路径关闭（焦点在内容上，不靠抢焦点）。
	h.escape()
	if m.Opened() {
		t.Fatal("内容持焦时 Escape 应关闭 Modal（约定路径）")
	}
	if !m.EscapeRequested() {
		t.Fatal("EscapeRequested 应报告一次")
	}

	// 关闭后焦点回交，回交标签可继续收键。
	h.frame(key.Event{Name: "y", State: key.Press})
	if !h.r.Source().Focused(&consumer.tag) {
		t.Fatal("关闭后焦点应回交 ReturnFocusTo")
	}
	gotY := false
	for _, n := range consumer.got {
		if n == "y" {
			gotY = true
		}
	}
	if !gotY {
		t.Fatalf("回交标签应收到关闭后的按键 y，得到 %v", consumer.got)
	}

	// 背景恢复可点。
	h.click(150, 100)
	if h.bgClicks != 1 {
		t.Fatalf("Modal 关闭后背景按钮应恢复可点击，点击数 = %d", h.bgClicks)
	}
}

func TestR1ContextMenuFocusOnlyOnOpenTransition(t *testing.T) {
	// 消费者逐帧请求焦点：菜单在打开转换帧抢到焦点；此后菜单不再
	// 每帧强抢（消费者重新赢回焦点）；Escape 经约定路径仍可关菜单。
	// 消费者按名过滤（"z"）：catch-all 键盘消费者会先于约定路径吃掉
	// Escape——真实 Editor 用具名过滤器，不受影响（见 scope-matrix C10）。
	h := newOverlayHarness(t, 300, 200)
	consumer := &keyConsumer{requestFocus: true, names: []key.Name{"z"}}
	menu := NewContextMenu(MenuItem{Title: "one", Action: "one"})
	h.overlay = func(gtx layout.Context) {
		consumer.layout(gtx)
		menu.Layout(gtx, h.th)
	}
	h.frame()
	menu.OpenAt(image.Pt(20, 20))
	h.frame()
	if !h.r.Source().Focused(&menu.scrim) {
		t.Fatal("打开转换帧菜单应取得焦点（最后一次 Execute 胜出）")
	}
	h.frame()
	if !h.r.Source().Focused(&consumer.tag) {
		t.Fatal("非转换帧菜单不得重新抢焦点（消费者应赢回）")
	}
	// 内容持焦时 Escape 仍走约定路径关闭菜单。
	h.escape()
	if menu.Opened() {
		t.Fatal("内容持焦时 Escape 应关闭菜单（约定路径）")
	}
}

// --- F-B3：Modal 偏移不泄漏到后续同级 ---

func TestR1ModalSiblingAtOriginReceivesPress(t *testing.T) {
	// 探针同型：Modal 之后声明的原点小输入区必须能收到 (2,2) 的 Press
	// （居中偏移不得泄漏到后续同级布局的变换）。
	h := newOverlayHarness(t, 300, 200)
	modal := NewModal(nil)
	modal.Content = func(cgtx layout.Context, _ *material.Theme) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(80, 40)}
	}
	modal.Open()
	sentinel := &overlayTag{}
	sentinel.owner = sentinel
	presses := 0
	h.overlay = func(gtx layout.Context) {
		modal.Layout(gtx, h.th)
		defer clip.Rect(image.Rectangle{Max: image.Pt(10, 10)}).Push(gtx.Ops).Pop()
		for {
			_, ok := gtx.Source.Event(pointer.Filter{Target: sentinel, Kinds: pointer.Press})
			if !ok {
				break
			}
			presses++
		}
		event.Op(gtx.Ops, sentinel)
	}
	h.frame()
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32Pt(2, 2)})
	if presses != 1 {
		t.Fatalf("Modal 后的原点兄弟未收到 Press：got=%d want=1（偏移泄漏）", presses)
	}
}

func TestR1ModalSiblingsBeforeAndAfterBothIndependent(t *testing.T) {
	// 强于探针的两阶段：
	//  阶段 1（Modal 打开）：Modal 之后的兄弟在自己声明的偏移位置之外
	//    的原点区可命中（居中偏移不泄漏、不扩大）；遮罩照常阻断更早
	//    布局的背景。
	//  阶段 2（Modal 关闭）：关闭不残留变换/命中区污染——原点区恢复可命中。
	h := newOverlayHarness(t, 300, 200)
	modal := NewModal(nil)
	modal.Content = func(cgtx layout.Context, _ *material.Theme) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(80, 40)}
	}
	modal.Open()
	beforeTag := &overlayTag{} // 区 (0,0)-(10,10)
	beforeTag.owner = "before"
	afterTag := &overlayTag{} // 区 (250,180)-(260,190)
	afterTag.owner = "after"
	beforePresses, afterPresses := 0, 0
	consume := func(gtx layout.Context, tag *overlayTag, n *int) {
		for {
			_, ok := gtx.Source.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
			if !ok {
				break
			}
			*n++
		}
	}
	h.overlay = func(gtx layout.Context) {
		func() {
			defer clip.Rect(image.Rectangle{Max: image.Pt(10, 10)}).Push(gtx.Ops).Pop()
			consume(gtx, beforeTag, &beforePresses)
			event.Op(gtx.Ops, beforeTag)
		}()
		modal.Layout(gtx, h.th)
		func() {
			defer clip.Rect(image.Rectangle{Min: image.Pt(250, 180), Max: image.Pt(260, 190)}).Push(gtx.Ops).Pop()
			consume(gtx, afterTag, &afterPresses)
			event.Op(gtx.Ops, afterTag)
		}()
	}
	h.frame()

	// 阶段 1：Modal 打开——后置兄弟在自己声明的区（远离居中内容）命中
	//（偏移既不泄漏也不扩大到它）；遮罩上的原点 (2,2) 被遮罩吃掉。
	// 全程使用完整点击形态（press+release），不用裸 press——按压中的
	// 指针再按下不是真实硬件形态。
	h.click(255, 185)
	if afterPresses != 1 {
		t.Fatalf("Modal 后置兄弟应在自己声明的区命中：got=%d want=1（偏移泄漏）", afterPresses)
	}
	modal.ScrimClicked() // 丢弃可能的广播遮罩点击，隔离下一步判据
	h.click(2, 2)        // 遮罩上的完整外点（按压+释放同帧）
	if beforePresses != 0 {
		t.Fatalf("打开态遮罩应阻断更早布局的背景兄弟：%d", beforePresses)
	}
	if !modal.ScrimClicked() {
		t.Fatal("遮罩应收到原点外点")
	}

	// 阶段 2：关闭后原点区恢复——先声明的兄弟可命中（无变换残留）。
	modal.Close()
	h.frame()
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32Pt(2, 2)})
	if beforePresses != 1 {
		t.Fatalf("Modal 关闭后原点兄弟应恢复命中：got=%d want=1（变换残留）", beforePresses)
	}
}

// --- F-B4：短视口/高菜单有界策略 ---

func TestR1MenuFitsShortViewport(t *testing.T) {
	items := make([]MenuItem, 8)
	for i := range items {
		items[i] = MenuItem{Title: "one", Action: "one"}
	}
	menu := NewContextMenu(items...)
	h := newOverlayHarness(t, 300, 100)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(290, 90))
	h.frame()
	if r := menu.Rect(); r.Max.Y > 100 || r.Max.X > 300 || r.Min.X < 0 || r.Min.Y < 0 {
		t.Fatalf("封顶后仍越出短视口：rect=%v 视口=300x100", r)
	}
}

func TestR1MenuViewportSmallerThanOneRow(t *testing.T) {
	menu := NewContextMenu(MenuItem{Title: "only", Action: "only"})
	h := newOverlayHarness(t, 60, 20)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(5, 15))
	h.frame()
	r := menu.Rect()
	if r.Max.Y > 20 || r.Max.X > 60 || r.Min.X < 0 || r.Min.Y < 0 {
		t.Fatalf("视口小于一行时面板必须仍有界：rect=%v 视口=60x20", r)
	}
	// 可见部分仍可点（行被裁剪，但面板内区域有效）。
	clickY := (r.Min.Y + r.Max.Y) / 2
	h.click((r.Min.X+r.Max.X)/2, clickY)
	if action, ok := menu.Clicked(); !ok || action != "only" {
		t.Fatalf("视口小于一行时可见部分应可点，得到 action=%q ok=%v", action, ok)
	}
}

func TestR1MenuScrollReachesHiddenItems(t *testing.T) {
	items := make([]MenuItem, 8)
	for i := range items {
		items[i] = MenuItem{Title: "one", Action: "one"}
	}
	menu := NewContextMenu(items...)
	h := newOverlayHarness(t, 300, 100)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(10, 10))
	h.frame()

	// 初始：最后一项不可见（零矩形），第一项可点。
	if !menu.ItemRect(7).Empty() {
		t.Fatalf("未滚动时最后一项不应有可见命中区：%v", menu.ItemRect(7))
	}
	c0 := rectCenter(menu.ItemRect(0))
	h.click(c0.X, c0.Y)
	if action, ok := menu.Clicked(); !ok || action != "one" {
		t.Fatalf("滚动前第一项应可点，得到 action=%q ok=%v", action, ok)
	}
	menu.OpenAt(image.Pt(10, 10)) // 重开（测试继续用同一菜单）
	h.frame()

	// 滚轮向下（Windows 原生 Scroll.Y>0：查看更靠后条目）→ 最后一项进入视口。
	h.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32Pt(50, 50), Scroll: f32FromPtY(500)})
	r7 := menu.ItemRect(7)
	if r7.Empty() {
		t.Fatal("滚动后最后一项应进入可见窗口")
	}
	if menu.Rect().Max.Y > 100 {
		t.Fatalf("滚动不得改变面板有界性：%v", menu.Rect())
	}
	c7 := rectCenter(r7)
	h.click(c7.X, c7.Y)
	if action, ok := menu.Clicked(); !ok || action != "one" {
		t.Fatalf("滚动后最后一项应可点，得到 action=%q ok=%v", action, ok)
	}

	// 反向滚动回到顶部：第一项恢复可见可点。
	menu.OpenAt(image.Pt(10, 10))
	h.frame()
	h.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32Pt(50, 50), Scroll: f32FromPtY(-500)})
	if menu.ItemRect(0).Empty() {
		t.Fatal("反向滚动后第一项应回到可见窗口")
	}
}

func TestR1MenuScrollClampedToBounds(t *testing.T) {
	items := make([]MenuItem, 8)
	for i := range items {
		items[i] = MenuItem{Title: "one", Action: "one"}
	}
	menu := NewContextMenu(items...)
	h := newOverlayHarness(t, 300, 100)
	h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }
	menu.OpenAt(image.Pt(10, 10))
	h.frame()

	// 超量滚动不得把可见窗口推出内容范围：最后一项保持贴底可见。
	h.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32Pt(50, 50), Scroll: f32FromPtY(100000)})
	if menu.ItemRect(7).Empty() {
		t.Fatal("向下滚动到底后最后一项应可见")
	}
	if r := menu.Rect(); r.Min.Y < 0 || r.Max.Y > 100 {
		t.Fatalf("滚动钳制后面板必须仍在视口内：%v", r)
	}
	// 重开后滚动复位（无跨状态残留）。
	menu.Close()
	menu.OpenAt(image.Pt(10, 10))
	h.frame()
	if menu.ItemRect(7).Empty() == false && menu.ItemRect(0).Empty() {
		t.Fatal("重开后应回到顶部")
	}
	if menu.ItemRect(0).Empty() {
		t.Fatal("重开后第一项应可见（滚动复位）")
	}
}

func f32FromPtY(y int) f32.Point { return f32.Point{X: 0, Y: float32(y)} }
