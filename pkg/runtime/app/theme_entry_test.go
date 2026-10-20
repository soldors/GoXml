package app

import (
	"image/color"
	"testing"

	"gioui.org/widget/material"
)

// ============================================================================
// THEME-SEAM-001-5：默认主题入口 vs 显式注入入口
//
// 背景：`Run` 与 `RunWith` 的语义**不同**，不能把 `material.NewTheme()`
// 全局替换掉之后又删掉注入路径：
//   - Run：便捷入口，默认主题由 pkg/runtime/theme 构建，调用方拿不到它；
//   - RunWith / NewApp / RunWindow*：显式注入，主题按原样传递。
//
// 本文件的用例只证明这三条边界，不验证字体功能（FONT-001 未实现）。
// ============================================================================

// stubMain 把 app.Main 换成空实现，返回还原函数。
func stubMain(t *testing.T) {
	t.Helper()
	orig := mainFunc
	t.Cleanup(func() { mainFunc = orig })
	mainFunc = func() {}
}

func TestRunBuildsDefaultThemeThroughThemePackage(t *testing.T) {
	stubMain(t)

	t.Run("走默认主题构建入口", func(t *testing.T) {
		orig := newTheme
		defer func() { newTheme = orig }()

		calls := 0
		newTheme = func() *material.Theme {
			calls++
			return material.NewTheme()
		}
		if err := Run(&countingView{text: "x"}, Config{}); err != nil {
			t.Fatalf("Run 返回错误: %v", err)
		}
		if calls != 1 {
			t.Errorf("Run 应恰好调用一次默认主题构建入口, 实际 %d 次", calls)
		}
	})

	t.Run("入口的返回值真的进入校验链路", func(t *testing.T) {
		// 反向证明：把入口的返回值换成 nil，Run 必须因「nil theme」失败。
		// 若有人把 Run 改回 `material.NewTheme()`，本用例会立刻变红——
		// 没有这条，光看「Run 不报错」无法区分两条实现路径。
		orig := newTheme
		defer func() { newTheme = orig }()
		newTheme = func() *material.Theme { return nil }

		if err := Run(&countingView{text: "x"}, Config{}); err == nil {
			t.Error("Run 未把主题构建入口的结果交给 NewApp 校验：" +
				"入口返回 nil 却仍然成功，说明默认主题不是从入口取的")
		}
	})
}

func TestRunWithDoesNotUseDefaultThemeBuilder(t *testing.T) {
	stubMain(t)

	orig := newTheme
	defer func() { newTheme = orig }()

	// 默认入口被破坏（返回 nil）：显式注入路径必须完全不受影响。
	called := false
	newTheme = func() *material.Theme {
		called = true
		return nil
	}

	if err := RunWith(&countingView{text: "x"}, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("RunWith 不应受默认主题入口影响: %v", err)
	}
	if called {
		t.Error("RunWith 不得调用默认主题构建入口：它已经拿到了调用方注入的主题")
	}
}

func TestInjectedThemeIsPassedThroughUnchanged(t *testing.T) {
	th := material.NewTheme()
	th.Palette.Fg = color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}

	a, err := NewApp(&countingView{text: "x"}, th, Config{})
	if err != nil {
		t.Fatalf("NewApp 返回错误: %v", err)
	}
	if a.th != th {
		t.Fatal("NewApp 未原样持有注入的主题（指针不同）")
	}
	if a.th.Palette.Fg != (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}) {
		t.Errorf("注入主题的自定义调色板丢失: %+v", a.th.Palette.Fg)
	}
	if a.view == nil {
		t.Error("NewApp 未持有 view")
	}
}

func TestDefaultThemeEntryIsUsable(t *testing.T) {
	// 把 app 依赖的默认入口与 theme 包的行为接起来：
	// 入口不可用时 Run 会一路带着坏主题走到 Layout 才炸。
	th := newTheme()
	if th == nil {
		t.Fatal("默认主题构建入口返回 nil")
	}
	if th.Shaper == nil {
		t.Error("默认主题缺少 Shaper——任何文本控件都无法排版")
	}
	if th.TextSize <= 0 {
		t.Errorf("默认主题字号非正: %v", th.TextSize)
	}
}
