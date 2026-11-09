package app

import (
	"sync/atomic"
	"testing"

	"gioui.org/layout"
	"gioui.org/widget/material"
)

// ============================================================================
// 冒烟测试：只能验证「不崩溃」与「布局被调用」
//
// 运行时入口契约 §5 明确：物理窗口与渲染效果无法自动验证。
// 这里不假装验证了窗口显示。
//
// R27-C R1 / F-C3：原本进入真实 app.Window.Event / Win32 init 的
// TestRunWindowStartsAndStopsCleanly、TestRunWindowCallsLayout、
// TestRunWithStartsMainLoop、TestRunDefaultsToMaterialTheme 已按**原名原断言**
// 移到 native_window_test.go（//go:build nativewindow），默认选择器不再
// 触碰真实窗口后端；等价的受控事件源回归见 controlled_window_test.go。
// ============================================================================

// countingView 记录 Layout 被调用的次数。
type countingView struct {
	calls atomic.Int64
	text  string
}

func (v *countingView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.calls.Add(1)
	return material.Body1(th, v.text).Layout(gtx)
}

func TestConfigDefaults(t *testing.T) {
	c := Config{}.withDefaults()
	if c.Title == "" {
		t.Error("Title 应有默认值")
	}
	if c.Width <= 0 || c.Height <= 0 {
		t.Errorf("尺寸应有默认值, 实际 %vx%v", c.Width, c.Height)
	}

	// 显式值不应被覆盖。
	c2 := Config{Title: "T", Width: 320, Height: 240}.withDefaults()
	if c2.Title != "T" || c2.Width != 320 || c2.Height != 240 {
		t.Errorf("显式配置被覆盖: %+v", c2)
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	th := material.NewTheme()
	view := &countingView{text: "x"}

	if err := RunWith(nil, th, Config{}); err == nil {
		t.Error("nil view 应被拒绝")
	}
	if err := RunWith(view, nil, Config{}); err == nil {
		t.Error("nil theme 应被拒绝")
	}
	if err := RunWindow(nil, th, Config{}, make(chan struct{})); err == nil {
		t.Error("RunWindow nil view 应被拒绝")
	}
	if err := RunWindow(view, th, Config{}, nil); err == nil {
		t.Error("nil stop channel 应被拒绝")
	}
}
