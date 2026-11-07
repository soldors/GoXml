package widgets

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// B1 状态单元测试：不驱动事件路由，只验证打开/关闭状态机、单次读取
// 缓冲、几何归零与视口钳制纯函数。真实指针/键盘交互在 interaction_test.go
// （B2）以离屏 input.Router 验证。

func closedFrame(t *testing.T, w, h int) layout.Context {
	t.Helper()
	return layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(w, h)),
	}
}

func TestContextMenuOpenCloseState(t *testing.T) {
	menu := NewContextMenu(
		MenuItem{Title: "剪切", Action: "cut"},
		MenuItem{Title: "粘贴", Action: "paste"},
	)
	if menu.Opened() {
		t.Fatal("构造后菜单不应处于打开状态")
	}
	if menu.Rect() != (image.Rectangle{}) {
		t.Fatalf("未布局前 Rect 应为零矩形，得到 %v", menu.Rect())
	}

	// 关闭态 Layout：零尺寸、几何归零（closedFrame 不含事件源，只走
	// 关闭退路）。
	gtx := closedFrame(t, 300, 200)
	if dims := menu.Layout(gtx, material.NewTheme()); dims.Size != (image.Point{}) {
		t.Fatalf("关闭态 Layout 应返回零尺寸，得到 %v", dims.Size)
	}
	if menu.Rect() != (image.Rectangle{}) {
		t.Fatalf("关闭态 Layout 后 Rect 应归零，得到 %v", menu.Rect())
	}

	menu.OpenAt(image.Pt(20, 20))
	if !menu.Opened() {
		t.Fatal("OpenAt 后应处于打开状态")
	}
	menu.Close()

	menu.OpenAt(image.Pt(5, 5))
	menu.Close()
	if menu.Opened() {
		t.Fatal("Close 后不应处于打开状态")
	}
	menu.Close() // 幂等
	if menu.Opened() {
		t.Fatal("重复 Close 后不应处于打开状态")
	}
}

func TestContextMenuEmptyMenuOpensAsCandidate(t *testing.T) {
	// 候选行为（待裁决）：空菜单允许打开，渲染零内容空面板，仍可经
	// 外点/Escape 关闭（B2 验证关闭路径）。
	menu := NewContextMenu()
	menu.OpenAt(image.Pt(10, 10))
	if !menu.Opened() {
		t.Fatal("空菜单应允许打开（候选默认）")
	}
	if _, ok := menu.Clicked(); ok {
		t.Fatal("空菜单不应产生任何动作")
	}
}

func TestContextMenuClickedSingleReadWithoutSelection(t *testing.T) {
	menu := NewContextMenu(MenuItem{Title: "A", Action: "a"})
	if _, ok := menu.Clicked(); ok {
		t.Fatal("无选中时 Clicked 不应报告动作")
	}
	if _, ok := menu.Clicked(); ok {
		t.Fatal("Clicked 必须是单次读取：第二次读取不应报告动作")
	}
}

func TestModalStateDefaults(t *testing.T) {
	var m Modal
	if m.Opened() {
		t.Fatal("零值 Modal 不应处于打开状态")
	}
	m.Close() // 关闭态 Close 为 no-op
	if m.Opened() {
		t.Fatal("关闭态 Close 后不应变为打开")
	}

	m = Modal{Content: func(gtx layout.Context, th *material.Theme) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(50, 50)}
	}}
	m.tag.owner = &m
	m.Open()
	if !m.Opened() {
		t.Fatal("Open 后应处于打开状态")
	}
	m.Close()
	if m.Opened() {
		t.Fatal("Close 后不应处于打开状态")
	}
	// 候选默认：无任何待定单次读取。
	if m.ScrimClicked() {
		t.Fatal("默认不应有遮罩点击待读取")
	}
	if m.EscapeRequested() {
		t.Fatal("默认不应有 Escape 待读取")
	}
	// 单次读取不可重复。
	if m.ScrimClicked() || m.EscapeRequested() {
		t.Fatal("单次读取必须读取即清空")
	}
}

func TestModalClosedLayoutZeroDims(t *testing.T) {
	m := NewModal(func(gtx layout.Context, th *material.Theme) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(50, 50)}
	})
	gtx := closedFrame(t, 300, 200)
	if dims := m.Layout(gtx, material.NewTheme()); dims.Size != (image.Point{}) {
		t.Fatalf("关闭态 Modal Layout 应返回零尺寸，得到 %v", dims.Size)
	}
}

// TestViewportClamp 是四边钳制的纯函数矩阵（B3 的定位变异必须打红
// 依赖它的真实布局矩阵，这里的白盒矩阵保证钳制函数本身不被静默破坏）。
func TestViewportClamp(t *testing.T) {
	viewport := image.Pt(200, 100)
	cases := []struct {
		name     string
		pos      image.Point
		size     image.Point
		expected image.Point
	}{
		{"左上角内", image.Pt(10, 10), image.Pt(50, 30), image.Pt(10, 10)},
		{"右边越界内收", image.Pt(180, 10), image.Pt(50, 30), image.Pt(150, 10)},
		{"下边越界内收", image.Pt(10, 90), image.Pt(50, 30), image.Pt(10, 70)},
		{"右下角钳到视口内", image.Pt(190, 95), image.Pt(50, 30), image.Pt(150, 70)},
		{"负锚点收到零", image.Pt(-5, -3), image.Pt(50, 30), image.Pt(0, 0)},
		{"弹层大于视口钳到零", image.Pt(30, 30), image.Pt(300, 200), image.Pt(0, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := viewportClamp(tc.pos, tc.size, viewport)
			if got != tc.expected {
				t.Fatalf("viewportClamp(%v, %v, %v) = %v，期望 %v",
					tc.pos, tc.size, viewport, got, tc.expected)
			}
		})
	}
}

func TestCenterPos(t *testing.T) {
	viewport := image.Pt(200, 100)
	if got := centerPos(viewport, image.Pt(50, 40)); got != (image.Point{X: 75, Y: 30}) {
		t.Fatalf("centerPos = %v，期望 (75,30)", got)
	}
	// 内容大于视口：钳到 0，不产生负偏移。
	if got := centerPos(viewport, image.Pt(300, 200)); got != (image.Point{}) {
		t.Fatalf("centerPos 超大内容 = %v，期望零点", got)
	}
}
