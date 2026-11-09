package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"
)

// ============================================================================
// R27-C C1：退出责任的受控实证探针（不打开任何原生窗口）
//
// 这些用例只做一件事：把 App.Run() 的**结构**用受控事件源重放出来，
// 观察「事件循环结束」「显式销毁回调」「清理」「主线程 Main 是否返回」
// 四者的真实先后。mainFunc 被注入为永不返回，等价于 Windows 上
// Gio v0.10.3 osMain() 的 `select {}`（app/os_windows.go:87-90）。
//
// 这不是原生验收：没有窗口、没有 Gio 消息循环、没有进程退出。
// R26 的真实 rc2 stderr 证据在交付目录引用，不在这里重放。
// ============================================================================

// c1Trace 用互斥锁记录探针顺序；所有写入点都由通道同步，不靠 sleep 猜时序。
type c1Trace struct {
	mu    sync.Mutex
	steps []string
}

func (t *c1Trace) add(step string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.steps = append(t.steps, step)
}

func (t *c1Trace) snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.steps...)
}

func c1MustNext(next func() event.Event, _ chan event.Event) func() event.Event { return next }

func c1ControlledEvents(evts ...event.Event) (func() event.Event, chan event.Event) {
	ch := make(chan event.Event, len(evts))
	for _, e := range evts {
		ch <- e
	}
	return func() event.Event { return <-ch }, ch
}

// TestC1RunStructureLoopEndsWhileMainStaysBlocked 证明契约冲突：
//
//	事件循环（goroutine）在 DestroyEvent 后结束、销毁回调已运行、
//	late worker 清理已发生；但主线程 Main 仍未返回——因此
//	`App.Run()` 里 `mainFunc(); return a.fontFailure()` 的 return 在
//	Windows 真实 Main 下不可达，「关闭后返回 error」不能由现有签名兑现。
func TestC1RunStructureLoopEndsWhileMainStaysBlocked(t *testing.T) {
	orig := mainFunc
	never := make(chan struct{})
	mainFunc = func() { <-never } // 模拟 osMain 的永久阻塞，不接管真实主线程
	// 捕获注入值到局部变量：后台 goroutine 只读这个局部函数变量，
	// 不再读全局 mainFunc，避免 cleanup 写回时产生数据竞争（C3 race 修复）。
	blockingMain := mainFunc
	mainDone := make(chan struct{})
	t.Cleanup(func() {
		close(never)
		select {
		case <-mainDone:
		case <-time.After(5 * time.Second):
			t.Error("注入的 Main goroutine 未在 5 秒内收口")
		}
		mainFunc = orig
	})

	trace := &c1Trace{}
	wrapped, err := WithOnDestroy(&countingView{text: "c1"}, func() { trace.add("callback") })
	if err != nil {
		t.Fatal(err)
	}

	a := &App{view: wrapped, th: material.NewTheme()}
	// lateStop 非空使 stopLateCompletion 的清理可被观测；lateWake 保持 nil，
	// 让合成帧不进入懒加载分支（本包 lz == nil）。
	a.lateStop = make(chan struct{})

	cause := errors.New("c1 destroy cause")
	next, remaining := c1ControlledEvents(
		synthFrameEvent(&frameRecorder{}),
		gioapp.DestroyEvent{Err: cause},
		gioapp.DestroyEvent{Err: cause}, // 重复关闭：循环应在第一个就返回
	)

	loopEnded := make(chan struct{})
	go func() {
		defer close(loopEnded)
		trace.add("loop-start")
		a.loopEvents(next)
		trace.add("loop-end")
	}()

	// 与 a.Run() 同构：mainFunc 在调用方（主）线程上阻塞。
	mainReturned := make(chan struct{})
	go func() {
		defer close(mainDone)
		blockingMain()
		close(mainReturned)
	}()

	select {
	case <-loopEnded:
	case <-time.After(3 * time.Second):
		t.Fatal("受控事件循环未在 3 秒内结束")
	}
	// never 未关闭，mainReturned 不可能闭合；这里用非阻塞检查而非猜测延迟。
	select {
	case <-mainReturned:
		t.Fatal("Main 不应返回；若返回则与 R26 原生 rc2 证据矛盾")
	default:
	}

	steps := trace.snapshot()
	want := []string{"loop-start", "callback", "loop-end"}
	if len(steps) != len(want) {
		t.Fatalf("探针顺序异常: %v", steps)
	}
	for i := range want {
		if steps[i] != want[i] {
			t.Fatalf("探针顺序异常: got %v want %v", steps, want)
		}
	}
	if a.lateStop != nil {
		t.Fatal("stopLateCompletion 未执行：late worker 清理没有在循环结束前发生")
	}
	// 只消费了第一个 DestroyEvent；重复关闭不应触发第二次回调。
	if got := len(remaining); got != 1 {
		t.Fatalf("重复 DestroyEvent 被继续消费，剩余事件=%d", got)
	}
	t.Logf("OBS C1 顺序=%v；Main 仍阻塞；重复关闭回调=1 次；DestroyEvent.Err=%q 已交出但 return 不可达", steps, cause)
}

// TestC1BothLoopsReturnDestroyCauseAfterRepair 是 C1 冻结现状探针的
// 修复后形态（**判据只强化不弱化**）：
//
//	C1 在 d6b1fc0 上观测到两条循环都丢弃 DestroyEvent.Err，见
//	delivery/raw/cmdlogs/r27c-c1-suite/ 与 r27c-c2-red-probe-first-fail/
//	的原始输出（首败保留，不重跑覆盖）。C2 修复后这里改为要求
//	两条循环都把 Err 交出，且 errors.Is 同时可判定 ErrWindowDestroy
//	与原始 cause；正常关窗仍返回 nil。
func TestC1BothLoopsReturnDestroyCauseAfterRepair(t *testing.T) {
	cause := errors.New("c1 premature destroy")

	// 环路 1（App.loopEvents）：结果记入 App，供 Run / 显式入口消费。
	a := &App{view: &countingView{text: "c1"}, th: material.NewTheme()}
	next, _ := c1ControlledEvents(gioapp.DestroyEvent{Err: cause})
	loopEnded := make(chan struct{})
	go func() {
		defer close(loopEnded)
		a.loopEvents(next)
	}()
	select {
	case <-loopEnded:
	case <-time.After(3 * time.Second):
		t.Fatal("App 事件循环未结束")
	}
	got := a.takeLoopExit()
	if !errors.Is(got, cause) || !errors.Is(got, ErrWindowDestroy) {
		t.Fatalf("App 环路未交出可判别的销毁原因: %v", got)
	}

	// 环路 2（runWindowEvents）：直接返回同一原因。
	w := &c1Window{
		events:  c1MustNext(c1ControlledEvents(gioapp.DestroyEvent{Err: cause})),
		actions: make(chan system.Action, 1),
	}
	stop := make(chan struct{})
	defer close(stop)
	got = runWindowEvents(w, &countingView{text: "c1"}, material.NewTheme(), stop, nil)
	if !errors.Is(got, cause) || !errors.Is(got, ErrWindowDestroy) {
		t.Fatalf("RunWindow 环路未交出可判别的销毁原因: %v", got)
	}
	t.Logf("OBS C1 修复后两条循环都交出 DestroyEvent.Err=%q，双判别成立", cause)
}

// c1Window 是离屏 windowEvents，不创建 Gio 原生窗口。
type c1Window struct {
	events  func() event.Event
	actions chan system.Action
}

func (w *c1Window) Event() event.Event      { return w.events() }
func (w *c1Window) Perform(a system.Action) { w.actions <- a }
func (w *c1Window) Invalidate()             {}

// TestC1RunWindowControlNeverCallsMain 证明 RunWindow* 控制形态不接管主线程：
// 注入的 mainFunc 计数器必须保持 0，且循环结束不触发任何进程退出。
func TestC1RunWindowControlNeverCallsMain(t *testing.T) {
	orig := mainFunc
	t.Cleanup(func() { mainFunc = orig })
	called := make(chan struct{}, 1)
	mainFunc = func() { called <- struct{}{} }

	w := &c1Window{
		events:  c1MustNext(c1ControlledEvents(gioapp.DestroyEvent{})),
		actions: make(chan system.Action, 1),
	}
	stop := make(chan struct{})
	defer close(stop)
	if err := runWindowEvents(w, &countingView{text: "c1"}, material.NewTheme(), stop, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
		t.Fatal("RunWindow 控制形态调用了 Main（契约 §3 明确禁止）")
	default:
	}
	if len(w.actions) != 0 {
		t.Fatal("RunWindow 控制形态向窗口发送了关闭动作")
	}
}
