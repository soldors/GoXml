package widgets

import (
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget/material"
	"image"
	"testing"
)

// No spare blank frame is inserted after closing: real idle windows need not
// produce one before the user's next click.
func TestNativeMenuClosingFrameDropsHitAreas(t *testing.T) {
	for _, how := range []string{"outside", "item", "escape"} {
		t.Run(how, func(t *testing.T) {
			m := NewContextMenu(MenuItem{Title: "one", Action: "one"})
			h := newOverlayHarness(t, 300, 200)
			h.overlay = func(gtx layout.Context) { m.Layout(gtx, h.th) }
			m.OpenAt(image.Pt(10, 10))
			h.frame()
			switch how {
			case "outside":
				h.press(280, 180)
				h.release(280, 180)
			case "item":
				p := rectCenter(m.ItemRect(0))
				h.press(p.X, p.Y)
				h.release(p.X, p.Y)
			case "escape":
				h.escape()
			}
			if m.Opened() {
				t.Fatal("menu did not close")
			}
			h.press(280, 180)
			h.release(280, 180)
			if h.bgClicks != 1 {
				t.Fatalf("first post-close click swallowed: background=%d want1", h.bgClicks)
			}
		})
	}
}

func TestNativeModalClosingFrameDropsHitAreas(t *testing.T) {
	for _, how := range []string{"escape", "content-close"} {
		t.Run(how, func(t *testing.T) {
			var m *Modal
			closeInContent := false
			m = NewModal(func(gtx layout.Context, _ *material.Theme) layout.Dimensions {
				if closeInContent {
					m.Close()
				}
				return layout.Dimensions{Size: image.Pt(80, 50)}
			})
			h := newOverlayHarness(t, 300, 200)
			h.overlay = func(gtx layout.Context) { m.Layout(gtx, h.th) }
			m.Open()
			h.frame()
			if how == "escape" {
				h.escape()
			} else {
				closeInContent = true
				h.frame()
			}
			if m.Opened() {
				t.Fatal("modal did not close")
			}
			h.press(280, 180)
			h.release(280, 180)
			if h.bgClicks != 1 {
				t.Fatalf("first post-close click swallowed: background=%d want1", h.bgClicks)
			}
		})
	}
}

func TestNativeWheelPositiveYRevealsLaterItems(t *testing.T) {
	items := make([]MenuItem, 8)
	for i := range items {
		items[i] = MenuItem{Title: "row", Action: "row"}
	}
	m := NewContextMenu(items...)
	h := newOverlayHarness(t, 300, 100)
	h.overlay = func(gtx layout.Context) { m.Layout(gtx, h.th) }
	m.OpenAt(image.Pt(10, 10))
	h.frame()
	if !m.ItemRect(7).Empty() {
		t.Fatal("last item already visible")
	}
	// Windows Gio converts native down-wheel to positive Scroll.Y. Match Gio's
	// gesture.Scroll convention; positive moves the viewport toward later items.
	h.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32Pt(50, 50), Scroll: f32FromPtY(500)})
	if m.ItemRect(7).Empty() {
		t.Fatal("native down-wheel did not reveal last item")
	}
	h.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32Pt(50, 50), Scroll: f32FromPtY(-500)})
	if m.ItemRect(0).Empty() {
		t.Fatal("native up-wheel did not restore first item")
	}
}
