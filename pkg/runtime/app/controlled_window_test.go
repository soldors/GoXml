//go:build !nativewindow

// 默认（无窗口）测试集合的受控事件源与等价回归（R27-C R1 / F-C3）。
//
// 事实更正：R27-C 原批次的默认选择器**确实**通过 RunWindow* / a.Run() 进入过
// 真实 app.Window.Event → w.init() → Windows callbacks.SetDriver（见
// review-1 F-C3 与旧 raw 首败）。旧措辞「未执行任何原生路径」不成立；
// 但窗口实际可见性、渲染效果与原生退出**仍未验证**，本文件也不追加作者
// 原生执行。真实窗口测试按原名原断言隔离在 nativewindow 标签集合中。
package app

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"
)

// TestMain 把包内事件源换成受控序列：一帧 + 正常 DestroyEvent。
// 生产源码的 windowEventSource 默认值不变（仍是 a.w.Event），
// 只有默认测试集合经这里替换；带 nativewindow 标签时本文件被排除。
func TestMain(m *testing.M) {
	windowEventSource = func(a *App) func() event.Event {
		ch := make(chan event.Event, 2)
		ch <- synthFrameEvent(&frameRecorder{})
		ch <- gioapp.DestroyEvent{}
		return func() event.Event { return <-ch }
	}
	os.Exit(m.Run())
}

// r1ControlledWindow 是纯内存 windowEvents，绝不创建平台窗口。
type r1ControlledWindow struct {
	events      chan event.Event
	actions     chan system.Action
	invalidates atomic.Int32
}

func newR1ControlledWindow() *r1ControlledWindow {
	return &r1ControlledWindow{events: make(chan event.Event, 8), actions: make(chan system.Action, 4)}
}

func (w *r1ControlledWindow) Event() event.Event      { return <-w.events }
func (w *r1ControlledWindow) Perform(a system.Action) { w.actions <- a }
func (w *r1ControlledWindow) Invalidate()             { w.invalidates.Add(1) }

func r1ControlledFrame() event.Event { return synthFrameEvent(&frameRecorder{}) }

// TestControlledEventSourceIsNotNativeWindow 守卫默认选择器：
// 受控事件源必须立即产出 FrameEvent，不能阻塞在真实窗口的 Event() 上。
func TestControlledEventSourceIsNotNativeWindow(t *testing.T) {
	a, err := NewApp(&countingView{text: "select"}, material.NewTheme(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	next := windowEventSource(a)
	got := make(chan event.Event, 1)
	go func() { got <- next() }()
	select {
	case e := <-got:
		if _, ok := e.(gioapp.FrameEvent); !ok {
			t.Fatalf("默认事件源未产出受控帧: %T", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("默认事件源仍在驱动真实窗口 Event()（选择器隔离失效）")
	}
}

// TestControlledRunWindowStartsAndStopsCleanly 对应原生冒烟的启动/停止断言：
// stop 提前到达、driver 就绪后有界执行一次 ActionClose、循环正常返回。
func TestControlledRunWindowStartsAndStopsCleanly(t *testing.T) {
	w := newR1ControlledWindow()
	stop := make(chan struct{})
	close(stop)
	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, &countingView{text: "smoke"}, material.NewTheme(), stop, nil) }()

	w.events <- r1ControlledFrame() // 首个事件 = driver 就绪
	select {
	case a := <-w.actions:
		if a != system.ActionClose {
			t.Fatalf("关闭动作不是 ActionClose: %v", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("driver 就绪后未发出有界关闭动作")
	}
	w.events <- gioapp.DestroyEvent{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("受控 RunWindow 应正常返回: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("受控 RunWindow 未在 2 秒内退出")
	}
	if got := w.invalidates.Load(); got != 1 {
		t.Fatalf("关闭请求后应恰好失效一次，实际 %d", got)
	}
}

// TestControlledRunWindowCallsLayout 对应原生「布局被调用」断言。
func TestControlledRunWindowCallsLayout(t *testing.T) {
	w := newR1ControlledWindow()
	w.events <- r1ControlledFrame()
	w.events <- gioapp.DestroyEvent{}
	stop := make(chan struct{})
	view := &countingView{text: "layout"}
	if err := runWindowEvents(w, view, material.NewTheme(), stop, nil); err != nil {
		t.Fatal(err)
	}
	if view.calls.Load() == 0 {
		t.Fatal("受控事件循环未驱动 Layout")
	}
}

// TestControlledRunWithStartsMainLoop 对应原生「RunWith 必须调用 Main」断言。
func TestControlledRunWithStartsMainLoop(t *testing.T) {
	orig := mainFunc
	defer func() { mainFunc = orig }()
	called := false
	mainFunc = func() { called = true }

	if err := RunWith(&countingView{text: "x"}, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("RunWith 返回错误: %v", err)
	}
	if !called {
		t.Fatal("RunWith 必须调用 Main（否则主线程不会接管）")
	}
}

// r1WaitValue 从受控 Layout 的观察通道读到目标值（channel 驱动，非 sleep）。
func r1WaitValue(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-ch:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("未在 3 秒内从受控帧读到 %q", want)
		}
	}
}

// TestControlledWindowStateRefresh 对应原生「状态更新 → 新帧 → 新值」链路，
// 并覆盖连续更新；全部由受控事件序列确定性推进。
func TestControlledWindowStateRefresh(t *testing.T) {
	w := newR1ControlledWindow()
	vm := &stateVM{Title: "AAAA"}
	v, err := newBindingView(vm)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, v, material.NewTheme(), stop, nil) }()

	w.events <- r1ControlledFrame()
	r1WaitValue(t, v.observed, "AAAA")
	for _, want := range []string{"BBBB", "CCCCCC"} {
		vm.Set(want)
		w.events <- r1ControlledFrame()
		r1WaitValue(t, v.observed, want)
	}
	if v.frames.Load() < 3 {
		t.Fatalf("受控帧数不足，状态刷新链路未真正跑过: %d", v.frames.Load())
	}
	w.events <- gioapp.DestroyEvent{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("受控事件循环未退出")
	}
}

// TestControlledNilVMRejected 保留原生集合里同名的参数校验断言。
func TestControlledNilVMRejected(t *testing.T) {
	if _, err := newBindingView(nil); err == nil {
		t.Fatal("nil ViewModel 应被明确拒绝")
	}
}
