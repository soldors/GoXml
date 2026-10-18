package app

import (
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// ============================================================================
// 冒烟测试：只能验证「不崩溃」与「布局被调用」
//
// 运行时入口契约 §5 明确：物理窗口与渲染效果无法自动验证。
// 这里不假装验证了窗口显示。
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

func TestRunWindowStartsAndStopsCleanly(t *testing.T) {
	// 核心冒烟：RunWindow 能启动、能通过 stop 干净退出、期间不 panic。
	//
	// 注意：这**不**证明窗口真的显示出来了。窗口创建在无图形会话的环境
	// 中可能失败，但进程不会崩溃——这正是本测试能覆盖的边界。
	view := &countingView{text: "smoke"}
	stop := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- RunWindow(view, material.NewTheme(), Config{
			Title:  "smoke",
			Width:  unit.Dp(200),
			Height: unit.Dp(100),
		}, stop)
	}()

	// 给它一点时间跑事件循环；期间若 panic 会在这里暴露。
	time.Sleep(300 * time.Millisecond)
	close(stop)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWindow 返回错误: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunWindow 未在 3 秒内退出")
	}
}

func TestRunWindowCallsLayout(t *testing.T) {
	// 证明「启动后确实驱动了布局」——这是自动化能验证的上限。
	// 渲染是否正确、窗口是否显示，不在自动化范围内。
	view := &countingView{text: "layout"}
	stop := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- RunWindow(view, material.NewTheme(), Config{}, stop)
	}()

	// 等待至少一帧。Gio 在无显示器环境下可能永不产生 FrameEvent，
	// 因此这里只在观察到调用时才断言，不把「0 帧」判为失败。
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			close(stop)
			<-done
			if view.calls.Load() == 0 {
				t.Skip("环境未产生任何帧（可能无图形会话）；窗口渲染未经自动验证")
			}
			return
		default:
			if view.calls.Load() > 0 {
				close(stop)
				<-done
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// ============================================================================
// Run / RunWith：注入 mainFunc 以便覆盖
//
// 真实的 app.Main() 会接管主线程且永不返回，测试无法调用。因此把
// app.Main 抽成可注入变量，测试替换为立即返回的空函数。
//
// 这**不**验证主线程接管行为——那是 Gio 的职责，且无法在单元测试中观察。
// ============================================================================

func TestRunWithStartsMainLoop(t *testing.T) {
	orig := mainFunc
	defer func() { mainFunc = orig }()

	var called bool
	mainFunc = func() { called = true }

	view := &countingView{text: "x"}
	if err := RunWith(view, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("RunWith 返回错误: %v", err)
	}
	if !called {
		t.Error("RunWith 必须调用 Main（否则主线程不会接管）")
	}
	// 给事件循环一点时间观察它是否会崩溃。
	time.Sleep(100 * time.Millisecond)
}

func TestRunDefaultsToMaterialTheme(t *testing.T) {
	orig := mainFunc
	defer func() { mainFunc = orig }()
	mainFunc = func() {}

	// Run 内部创建主题；这里只验证它不会因为缺主题而报错。
	if err := Run(&countingView{text: "x"}, Config{Title: "t"}); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
}
