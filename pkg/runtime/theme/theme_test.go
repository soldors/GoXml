package theme

import (
	"image/color"
	"testing"

	"gioui.org/widget/material"
)

// ============================================================================
// THEME-SEAM-001-5：主题构建入口的验收
//
// 目标不是「证明字体功能可用」（那是 FONT-001-A..D，未实现），
// 而是证明这个入口现在就能被 `pkg/runtime/app` 用作默认主题来源，
// 且返回的是**可直接交给 material 控件**的独立实例。
// ============================================================================

// isUsable 判定主题是否可供 material 控件直接使用。
//
// 写成谓词而非一串散落断言，是为了能在同一处做**反向自检**：
// 零值 `material.Theme{}` 必须被判为不可用，否则这套断言是空的
// （一个恒为真的检查等于没检查）。
func isUsable(th *material.Theme) bool {
	if th == nil {
		return false
	}
	// 无 Shaper 时任何文本控件都无法排版。
	if th.Shaper == nil {
		return false
	}
	if th.TextSize <= 0 || th.FingerSize <= 0 {
		return false
	}
	// 前景与背景同色则界面必然不可读；零值主题正是两者皆为零值。
	if th.Palette.Fg == th.Palette.Bg {
		return false
	}
	return true
}

func TestNewThemeProducesUsableTheme(t *testing.T) {
	th := NewTheme()
	if !isUsable(th) {
		t.Fatalf("NewTheme 产出的主题不可直接用于 material 控件: %+v", th)
	}

	t.Run("反向自检：零值主题必须判为不可用", func(t *testing.T) {
		if isUsable(&material.Theme{}) {
			t.Fatal("谓词把零值主题判为可用——本用例的断言是空的，必须修 isUsable")
		}
	})
}

func TestNewThemeReturnsIndependentInstances(t *testing.T) {
	a, b := NewTheme(), NewTheme()
	if a == b {
		t.Fatal("NewTheme 返回了共享实例：两个窗口会互相污染主题状态")
	}

	// 主题是可变结构：改一个实例的调色板不得影响另一个。
	before := b.Palette.Fg
	a.Palette.Fg = color.NRGBA{R: 0xff, G: 0, B: 0, A: 0xff}
	if b.Palette.Fg != before {
		t.Errorf("两个实例共享底层状态: b.Palette.Fg 由 %v 变为 %v", before, b.Palette.Fg)
	}
}

// TestNewThemeCurrentlyMatchesGioDefault 锁住「当前只构建 Gio 默认主题」。
//
// 这不是要永久冻结调色板，而是防止有人顺手改了主题默认值却没走 ADR。
// FONT-001 接线时**允许且应当**修改本用例：字体策略改变的是 `Shaper` /
// 字体集合，不应顺带改变调色板、字号或触摸尺寸。
func TestNewThemeCurrentlyMatchesGioDefault(t *testing.T) {
	th, want := NewTheme(), material.NewTheme()
	if th.Palette != want.Palette {
		t.Errorf("调色板与 Gio 默认不一致: got %+v want %+v", th.Palette, want.Palette)
	}
	if th.TextSize != want.TextSize || th.FingerSize != want.FingerSize {
		t.Errorf("字号/触摸尺寸与 Gio 默认不一致: got %v/%v want %v/%v",
			th.TextSize, th.FingerSize, want.TextSize, want.FingerSize)
	}
}
