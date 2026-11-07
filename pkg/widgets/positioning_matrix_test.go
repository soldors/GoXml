package widgets

import (
	"image"
	"testing"

	"gioui.org/layout"
)

// B3 四边定位矩阵：锚点贴近视口四边/四角时面板必须被钳进视口，且钳制
// 后条目仍可真实点击（离屏 Router 验证，不是纯几何断言）。
func TestContextMenuFourEdgePositioning(t *testing.T) {
	const ( // 视口 300x200；两项菜单面板 ≈ 96-100px 宽 × 64px 高
		vw = 300
		vh = 200
	)
	cases := []struct {
		name   string
		anchor image.Point
	}{
		{"左缘", image.Pt(2, 100)},
		{"右缘", image.Pt(298, 100)},
		{"上缘", image.Pt(150, 2)},
		{"下缘", image.Pt(150, 198)},
		{"左上角", image.Pt(2, 2)},
		{"右下角", image.Pt(298, 198)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			menu := NewContextMenu(
				MenuItem{Title: "剪切", Action: "cut"},
				MenuItem{Title: "粘贴", Action: "paste"},
			)
			h := newOverlayHarness(t, vw, vh)
			h.overlay = func(gtx layout.Context) { menu.Layout(gtx, h.th) }

			h.frame()
			menu.OpenAt(tc.anchor)
			h.frame()

			r := menu.Rect()
			if r.Empty() {
				t.Fatal("打开态必须产出非空面板几何")
			}
			if r.Min.X < 0 || r.Min.Y < 0 || r.Max.X > vw || r.Max.Y > vh {
				t.Fatalf("面板越出视口：rect=%v 视口=%dx%d", r, vw, vh)
			}
			// 越界锚点必须被拉回（钳制真实发生，不是恰好放下）。
			clampedX := tc.anchor.X+panelTestWidth(r) > vw
			clampedY := tc.anchor.Y+panelTestHeight(r) > vh
			if (clampedX && r.Min.X == tc.anchor.X) || (clampedY && r.Min.Y == tc.anchor.Y) {
				t.Fatalf("锚点越界但未被钳制：anchor=%v rect=%v", tc.anchor, r)
			}

			center := rectCenter(menu.ItemRect(0))
			h.click(center.X, center.Y)
			if action, ok := menu.Clicked(); !ok || action != "cut" {
				t.Fatalf("钳制后的条目必须可真实点击，得到 action=%q ok=%v", action, ok)
			}
		})
	}
}

func panelTestWidth(r image.Rectangle) int  { return r.Dx() }
func panelTestHeight(r image.Rectangle) int { return r.Dy() }
