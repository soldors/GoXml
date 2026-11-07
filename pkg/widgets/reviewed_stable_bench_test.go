package widgets

import (
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget/material"
	"image"
	"testing"
)

// Reuse Ops as a real event loop does. The original benchmark intentionally
// included a fresh Ops allocation per frame, so its allocations are not a pure
// component steady-state result. No GPU or native window is used here.
func BenchmarkReviewedStableOverlayFrame(b *testing.B) {
	benchmarkReviewedOverlay(b, true)
}

// Component-only numbers exclude input.Router.Frame. Both scopes are reported;
// this does not turn the complete routed-frame allocation into a passing zero.
func BenchmarkReviewedOverlayLayoutOnly(b *testing.B) {
	benchmarkReviewedOverlay(b, false)
}

func benchmarkReviewedOverlay(b *testing.B, routed bool) {
	for _, kind := range []string{"menu-closed", "menu-open", "modal-closed", "modal-open"} {
		b.Run(kind, func(b *testing.B) {
			th := material.NewTheme()
			var ops op.Ops
			var router input.Router
			menu := NewContextMenu(MenuItem{Title: "cut", Action: "cut"}, MenuItem{Title: "paste", Action: "paste"}, MenuItem{Title: "copy", Action: "copy"})
			modal := NewModal(func(gtx layout.Context, _ *material.Theme) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(100, 60)}
			})
			if kind == "menu-open" {
				menu.OpenAt(image.Pt(20, 20))
			}
			if kind == "modal-open" {
				modal.Open()
			}
			frame := func() {
				ops.Reset()
				gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(300, 200))}
				if kind == "menu-open" || kind == "menu-closed" {
					menu.Layout(gtx, th)
				} else {
					modal.Layout(gtx, th)
				}
			}
			for i := 0; i < 50; i++ {
				frame()
				router.Frame(&ops)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				frame()
				if !routed {
					b.StopTimer()
				}
				router.Frame(&ops)
				if !routed {
					b.StartTimer()
				}
			}
		})
	}
}

// A dependency control: no Goxml component, only one visible focusable Gio tag.
// Gio 0.10.3 keyQueue.updateFocusLayout uses reflect-backed SliceStable sorts.
func BenchmarkReviewedGioFocusableFrame(b *testing.B) {
	var ops op.Ops
	var router input.Router
	tag := new(int)
	frame := func() {
		ops.Reset()
		router.Source().Event(key.FocusFilter{Target: tag})
		stack := clip.Rect(image.Rect(0, 0, 100, 60)).Push(&ops)
		event.Op(&ops, tag)
		stack.Pop()
		router.Frame(&ops)
	}
	for i := 0; i < 50; i++ {
		frame()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame()
	}
}
