package widgets

import (
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget/material"
	"image"
	"testing"
)

func TestModalRepeatedOpenKeepsContentFocus(t *testing.T) {
	h := newOverlayHarness(t, 300, 200)
	inside := &keyConsumer{}
	first := true
	m := NewModal(nil)
	m.Content = func(gtx layout.Context, _ *material.Theme) layout.Dimensions {
		inside.layout(gtx)
		if first {
			gtx.Execute(key.FocusCmd{Tag: &inside.tag})
			first = false
		}
		return layout.Dimensions{Size: image.Pt(80, 40)}
	}
	h.overlay = func(gtx layout.Context) { m.Layout(gtx, h.th) }
	m.Open()
	h.frame()
	h.frame()
	if !h.r.Source().Focused(&inside.tag) {
		t.Fatal("control: content did not hold focus")
	}
	m.Open()
	h.frame()
	if !h.r.Source().Focused(&inside.tag) {
		t.Fatal("Open on already-open Modal stole content focus despite documented no-op")
	}
}
