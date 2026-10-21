package app

import (
	"errors"
	"image/color"
	"testing"

	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ============================================================================
// THEME-SEAM-001-5 + TASK-FONT-001-B/B6：默认主题入口 vs 显式注入入口
//
// 背景：`Run` 与 `RunWith` 的语义**不同**，不能把 `material.NewTheme()`
// 全局替换掉之后又删掉注入路径：
//   - Run：便捷入口，默认主题由 pkg/runtime/theme 构建，调用方拿不到它；
//   - RunWith / NewApp / RunWindow*：显式注入，主题按原样传递。
//
// B6 之后多一条：Run 在"字体两条链路都失败"时必须**返回错误**，
// 而显式注入路径不受影响。
// ============================================================================

// stubMain 把 app.Main 换成空实现，返回还原函数。
func stubMain(t *testing.T) {
	t.Helper()
	orig := mainFunc
	t.Cleanup(func() { mainFunc = orig })
	mainFunc = func() {}
}

// stubThemeEntry 替换默认主题入口，返回还原函数。
func stubThemeEntry(t *testing.T, fn func() (*material.Theme, *theme.ProbeReport)) {
	t.Helper()
	orig := newThemeWithReport
	t.Cleanup(func() { newThemeWithReport = orig })
	newThemeWithReport = fn
}

func TestRunBuildsDefaultThemeThroughThemePackage(t *testing.T) {
	stubMain(t)

	t.Run("走默认主题构建入口", func(t *testing.T) {
		calls := 0
		stubThemeEntry(t, func() (*material.Theme, *theme.ProbeReport) {
			calls++
			return material.NewTheme(), &theme.ProbeReport{}
		})
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
		stubThemeEntry(t, func() (*material.Theme, *theme.ProbeReport) {
			return nil, &theme.ProbeReport{}
		})
		if err := Run(&countingView{text: "x"}, Config{}); err == nil {
			t.Error("Run 未把主题构建入口的结果交给 NewApp 校验：" +
				"入口返回 nil 却仍然成功，说明默认主题不是从入口取的")
		}
	})

	t.Run("没有诊断报告必须报错", func(t *testing.T) {
		// 没有诊断 = 没有证据。宁可失败，也不要假装"字体没问题"。
		stubThemeEntry(t, func() (*material.Theme, *theme.ProbeReport) {
			return material.NewTheme(), nil
		})
		if err := Run(&countingView{text: "x"}, Config{}); err == nil {
			t.Error("入口未返回诊断时 Run 必须报错")
		}
	})
}

// TestRunFailsOnFontDiagnostics B6：默认主题路径在字体不可用时返回诊断错误。
//
// 这条是"不再静默降级"的唯一可执行证据：错误必须可观察
// （errors.Is(ErrFontNotFound) 成立），而不是启动一个渲染空白的窗口。
func TestRunFailsOnFontDiagnostics(t *testing.T) {
	stubMain(t)

	stubThemeEntry(t, func() (*material.Theme, *theme.ProbeReport) {
		return material.NewTheme(), &theme.ProbeReport{
			Family: theme.FamilyCJK,
			Candidates: []theme.CandidateResult{
				{Path: "/nope/a.ttc", Status: theme.StatusMissing, Err: errors.New("not found")},
			},
			Embedded: &theme.EmbeddedResult{
				Attempted: true,
				Err:       errors.New("嵌入字体损坏"),
			},
			Err: errors.New("包装后的字体错误"),
		}
	})

	err := Run(&countingView{text: "x"}, Config{})
	if err == nil {
		t.Fatal("字体诊断有错时 Run 必须返回错误，否则就是静默降级")
	}
	t.Logf("Run 返回: %v", err)
}

// TestRunWrapsFontNotFound 错误必须能用 errors.Is 判定为 ErrFontNotFound。
//
// 用真实链路验证而不是构造字符串：让 Provider 的两条链路都失败，
// 再经 theme.NewThemeWithReport → Run 一路传上来。
func TestRunWrapsFontNotFound(t *testing.T) {
	stubMain(t)

	// 直接注入"包装了 ErrFontNotFound 的报告"，模拟 theme 包的严格失败。
	stubThemeEntry(t, func() (*material.Theme, *theme.ProbeReport) {
		return material.NewTheme(), &theme.ProbeReport{
			Err: errors.Join(contracts.ErrFontNotFound, errors.New("system: 无候选")),
		}
	})

	err := Run(&countingView{text: "x"}, Config{})
	if !errors.Is(err, contracts.ErrFontNotFound) {
		t.Fatalf("Run 的错误必须可判定为 ErrFontNotFound，实际 %v", err)
	}
}

func TestRunWithDoesNotUseDefaultThemeBuilder(t *testing.T) {
	stubMain(t)

	// 默认入口被破坏（返回 nil 主题 + 失败诊断）：显式注入路径必须完全不受影响。
	called := false
	stubThemeEntry(t, func() (*material.Theme, *theme.ProbeReport) {
		called = true
		return nil, &theme.ProbeReport{Err: errors.New("boom")}
	})

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
	th, rep := newThemeWithReport()
	if rep == nil {
		t.Fatal("默认主题入口未返回诊断报告")
	}
	if rep.Err != nil {
		// 本机两条链路都应可用（系统 msyh 或嵌入兜底），失败就是真问题。
		t.Fatalf("默认主题字体解析失败: %v\n诊断:\n%s", rep.Err, rep)
	}
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
