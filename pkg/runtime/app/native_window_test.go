//go:build nativewindow

// 真实 Gio 窗口测试集合（R27-C R1 / F-C3）。默认 go test **不选中**本文件；
// 只有协调者显式 -tags nativewindow 才编译/运行。原测试名与断言逐字保留，
// 作者返修阶段不执行任何原生窗口。
//
// 事实更正：R27-C 原批次的默认选择器确实进入过真实 Window.Event /
// Win32 init / callbacks.SetDriver（见 review-1 F-C3 与旧 raw 首败）。
// 可见性、渲染效果与原生退出仍**未验证**，不因本文件存在而升级为通过。
package app

import (
	"testing"
	"time"

	"gioui.org/unit"
	"gioui.org/widget/material"
)

// TestRunWindowStartsAndStopsCleanly（原名、原断言保留）
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

// TestRunWindowCallsLayout（原名、原断言保留）
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

// TestRunWithStartsMainLoop（原名、原断言保留）
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

// TestRunDefaultsToMaterialTheme（原名、原断言保留）
func TestRunDefaultsToMaterialTheme(t *testing.T) {
	orig := mainFunc
	defer func() { mainFunc = orig }()
	mainFunc = func() {}

	// Run 内部创建主题；这里只验证它不会因为缺主题而报错。
	if err := Run(&countingView{text: "x"}, Config{Title: "t"}); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
}

// TestAppRunDrivesAndInvalidateRefreshes（原名、原断言保留）
func TestAppRunDrivesAndInvalidateRefreshes(t *testing.T) {
	orig := mainFunc
	defer func() { mainFunc = orig }()

	vm := &stateVM{Title: "AAAA"}
	view, err := newBindingView(vm)
	if err != nil {
		t.Fatal(err)
	}

	th := material.NewTheme()
	a, err := NewApp(view, th, Config{Title: "s3-handle", Width: 200, Height: 60})
	if err != nil {
		t.Fatal(err)
	}

	// Run 的 mainFunc 置为等待 stop——模拟「主线程被占住」的生产形态。
	stop := make(chan struct{})
	mainFunc = func() { <-stop }

	// 后台工作线程：改状态 + 失效（生产典型形态，先于 Run 启动）。
	go func() {
		// 等首帧出现（说明事件循环已在跑）。
		deadline := time.After(3 * time.Second)
		for {
			select {
			case got := <-view.observed:
				if got == "AAAA" {
					goto ready
				}
			case <-deadline:
				close(stop)
				return
			}
		}
	ready:
		vm.Set("WWWWWWWW")
		a.Invalidate()
	}()

	// Run 在 goroutine 中驱动（mainFunc 已注入，不会占住主线程）。
	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	// 等新值出现。
	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-view.observed:
			if got == "WWWWWWWW" {
				close(stop)
				<-done
				t.Logf("句柄形态链路生效：AAAA → WWWWWWWW（帧数=%d）", view.frames.Load())
				return
			}
		case <-deadline:
			close(stop)
			<-done
			t.Fatalf("3 秒内未观察到新值；帧数=%d", view.frames.Load())
		}
	}
}
