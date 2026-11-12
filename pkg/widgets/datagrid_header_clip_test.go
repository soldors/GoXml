package widgets

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func TestDataGridHeaderStaysVisibleAtPartialRow(t *testing.T) {
	const (
		width   = 120
		height  = 300
		header  = 36 // 24 dp at 150%.
		scroll  = 24
		sampleX = 60
	)
	win, err := headless.NewWindow(width, height)
	if err != nil {
		t.Skipf("offscreen Gio renderer unavailable: %v", err)
	}
	defer win.Release()
	th := material.NewTheme()
	rows := []DataGridRow{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}, {ID: "f"}, {ID: "g"}, {ID: "h"}}
	for _, custom := range []bool{false, true} {
		name := "default"
		if custom {
			name = "custom-cell"
		}
		t.Run(name, func(t *testing.T) {
			grid := NewDataGrid(24)
			if err := grid.SetColumns([]DataGridColumn{{WidthDp: 80}}); err != nil {
				t.Fatal(err)
			}
			if report := grid.SetRows(rows); !report.OK {
				t.Fatalf("SetRows: %+v", report)
			}
			if custom {
				grid.Cell = func(gtx layout.Context, _ DataGridRow, _ int, bounds image.Rectangle) layout.Dimensions {
					paint.FillShape(gtx.Ops, color.NRGBA{R: 255, A: 255}, clip.Rect(bounds).Op())
					return layout.Dimensions{Size: bounds.Size()}
				}
			}

			render := func(offset int) *image.RGBA {
				t.Helper()
				grid.scrollOff = offset
				var ops op.Ops
				grid.Layout(layout.Context{
					Ops:         &ops,
					Constraints: layout.Exact(image.Pt(width, height)),
					Metric:      unit.Metric{PxPerDp: 1.5, PxPerSp: 1.5},
				}, th)
				if err := win.Frame(&ops); err != nil {
					t.Fatal(err)
				}
				frame := image.NewRGBA(image.Rect(0, 0, width, height))
				if err := win.Screenshot(frame); err != nil {
					t.Fatal(err)
				}
				return frame
			}

			before := render(0)
			after := render(scroll)
			headerColor := before.RGBAAt(sampleX, 20)
			bodyColor := before.RGBAAt(sampleX, 40)
			if headerColor.A == 0 || bodyColor.A == 0 || headerColor == bodyColor {
				t.Fatalf("test requires visible, distinct header and body pixels, got %v and %v", headerColor, bodyColor)
			}
			if !bytes.Equal(before.Pix[:header*before.Stride], after.Pix[:header*after.Stride]) {
				t.Errorf("body painted into fixed header after a %dpx scroll: header sample got %v, want %v",
					scroll, after.RGBAAt(sampleX, 20), headerColor)
			}
			if got := after.RGBAAt(sampleX, 40); got != bodyColor {
				t.Errorf("visible portion of first row should still paint: got %v, want %v", got, bodyColor)
			}
		})
	}
}
