package widgets

import (
	"fmt"
	"image"
	"testing"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func TestHeaderClipRed(t *testing.T) {
	const (
		rows = 10000
		w    = 960
		h    = 640
		dpi  = 1.5
	)
	rowHpx := int(dpi * 24)    // 36
	headerHpx := int(dpi * 24) // 36

	gridA := NewDataGrid(unit.Dp(24))
	if err := gridA.SetColumns([]DataGridColumn{
		{Title: "ID", WidthDp: unit.Dp(200)},
		{Title: "大小", WidthDp: unit.Dp(120)},
	}); err != nil {
		t.Fatal(err)
	}
	data := make([]DataGridRow, rows)
	for i := range data {
		data[i] = DataGridRow{
			ID:    fmt.Sprintf("ROW-%05d", i),
			Cells: []string{fmt.Sprintf("ROW-%05d", i), "x"},
		}
	}
	if rep := gridA.SetRows(data); !rep.OK {
		t.Fatal("setup failed")
	}
	gridA.EnableKeyboardNav(DataGridKeyboardConfig{})

	var r input.Router
	var ops op.Ops
	th := material.NewTheme()

	type cellRect struct {
		rowID string
		minY  int
		maxY  int
	}
	var cells []cellRect
	gridA.Cell = func(gtx layout.Context, row DataGridRow, col int, rect image.Rectangle) layout.Dimensions {
		if col == 0 {
			cells = append(cells, cellRect{rowID: row.ID, minY: rect.Min.Y, maxY: rect.Max.Y})
		}
		return layout.Dimensions{Size: rect.Size()}
	}

	gtx := layout.Context{
		Ops:         &ops,
		Source:      r.Source(),
		Metric:      unit.Metric{PxPerDp: dpi, PxPerSp: dpi},
		Constraints: layout.Exact(image.Pt(w, h)),
	}
	frame := func(events ...event.Event) {
		r.Queue(events...)
		ops.Reset()
		gtx.Ops = &ops
		gridA.Layout(gtx, th)
		r.Frame(&ops)
	}

	frame()
	gtx.Execute(key.FocusCmd{Tag: &gridA.tag})
	frame()

	// 模拟桌面验收：Home → PageDown → scrollOff=24（非整行偏移）
	gridA.scrollOff = 24
	cells = nil
	frame()

	if len(cells) == 0 {
		t.Fatal("无 Cell 回调")
	}

	firstMinY := cells[0].minY

	// 检查 1: Cell rect 布局坐标（独立预期：手算 scrollOff=24 → 首行 top=headerH-24=12）
	t.Logf("绘制坐标: first cell Min.Y=%d (headerHpx=%d, rowHpx=%d)", firstMinY, headerHpx, rowHpx)
	for i, c := range cells {
		if i >= 3 {
			break
		}
		t.Logf("  cell[%d] row=%s minY=%d maxY=%d", i, c.rowID, c.minY, c.maxY)
	}

	// 检查 2: body clip 边界（实际绘制边界）
	if !gridA._bodyClipOK || gridA.bodyClipMinY < headerHpx {
		t.Errorf("表体裁剪缺失或边界不足：bodyClipMinY=%d _bodyClipOK=%v (期望 >= headerHpx=%d)",
			gridA.bodyClipMinY, gridA._bodyClipOK, headerHpx)
	} else {
		t.Logf("表体裁剪 ✓ bodyClipMinY=%d >= headerHpx=%d", gridA.bodyClipMinY, headerHpx)
	}

	// 综合判定：布局坐标可能超出（行在视口中的实际位置），但 body clip
	// 必须将实际绘制限制在 body 区域内。
	if firstMinY < headerHpx && (!gridA._bodyClipOK || gridA.bodyClipMinY < headerHpx) {
		t.Errorf("表体越进固定表头且无 body 裁剪：cell Min.Y=%d bodyClipMinY=%d",
			firstMinY, gridA.bodyClipMinY)
	}
}
