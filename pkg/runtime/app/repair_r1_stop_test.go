package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"
)

// ============================================================================
// R27-C R1 / S2：初始化期 Stop 的确定性屏障守卫（F-C2）
//
// 全部使用受控 windowEvents，不创建平台窗口。判定来自 channel 信号与
// 结构计数，不用 sleep 推断事件已发生；time.After 只是有界观察窗口 /
// 防挂死保护，超时本身判为「未观察到」而不是「已经发生」。
//
// 修复契约：首次 Event() 返回（driver 就绪）之前绝不 Perform；提前到达的
// stop 不丢，driver 就绪后有界执行一次 ActionClose；Destroy 优先；
// 回调恰一次；DestroyEvent.Err 透传；循环结束不留 watcher。
// ============================================================================

// r1InitBarrierWindow 把首次 Event() 停在初始化屏障上。
type r1InitBarrierWindow struct {
	entered   chan struct{}
	release   chan struct{}
	performed chan struct{}
	once      sync.Once
	mu        sync.Mutex
	performs  int
}

func (w *r1InitBarrierWindow) Event() event.Event {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return gioapp.DestroyEvent{}
}
func (w *r1InitBarrierWindow) Perform(system.Action) {
	w.mu.Lock()
	w.performs++
	w.mu.Unlock()
	select {
	case w.performed <- struct{}{}:
	default:
	}
}
func (w *r1InitBarrierWindow) Invalidate() {}

func (w *r1InitBarrierWindow) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.performs
}

// TestR1EarlyStopBeforeFirstEventNeverPerforms 与协调者同材料、判据更严：
// 屏障期间与全程都不得有 Perform；循环必须确定性返回 nil。
func TestR1EarlyStopBeforeFirstEventNeverPerforms(t *testing.T) {
	w := &r1InitBarrierWindow{
		entered: make(chan struct{}), release: make(chan struct{}),
		performed: make(chan struct{}, 1),
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, &countingView{}, material.NewTheme(), stop, nil) }()

	<-w.entered // Event 已进入初始化屏障：driver 必然尚未就绪
	close(stop)

	bad := false
	select {
	case <-w.performed:
		bad = true
	case <-time.After(100 * time.Millisecond):
		// 有界观察窗口：不是用延时证明事件已发生。
	}
	close(w.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("受控循环应正常返回 nil: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("受控循环未在 2 秒内返回（可能遗留 watcher / 死锁）")
	}
	if bad || w.count() != 0 {
		t.Fatalf("初始化屏障期间发起了 Perform: observedInWindow=%v total=%d", bad, w.count())
	}
}

// r1SeqWindow 用受控事件序列；在交出 DestroyEvent 之前等 watcher 的
// Perform 信号（确定性屏障，不用 sleep 猜顺序）。
type r1SeqWindow struct {
	events      chan event.Event
	performed   chan struct{}
	performs    atomic.Int32
	invalidates atomic.Int32
	// waitForPerform 让 DestroyEvent 在交出之前确定性地等 watcher 的
	// ActionClose 信号；只有需要验证「driver 就绪后有界执行」的用例打开。
	waitForPerform atomic.Bool
}

func (w *r1SeqWindow) Event() event.Event {
	e := <-w.events
	if _, ok := e.(gioapp.DestroyEvent); ok && w.waitForPerform.Load() {
		select {
		case <-w.performed:
		case <-time.After(time.Second):
		}
	}
	return e
}
func (w *r1SeqWindow) Perform(a system.Action) {
	if a == system.ActionClose {
		w.performs.Add(1)
		select {
		case w.performed <- struct{}{}:
		default:
		}
	}
}
func (w *r1SeqWindow) Invalidate() { w.invalidates.Add(1) }

func r1StopTestFrame() event.Event {
	return synthFrameEvent(&frameRecorder{})
}

// TestR1PendingStopRunsBoundedCloseAfterDriverReady 提前到达的 stop 不丢：
// 首个 FrameEvent 让 driver 就绪后，watcher 有界执行一次 ActionClose，
// 然后 DestroyEvent 结束循环，回调恰一次、错误为 nil。
func TestR1PendingStopRunsBoundedCloseAfterDriverReady(t *testing.T) {
	w := &r1SeqWindow{events: make(chan event.Event, 4), performed: make(chan struct{}, 1)}
	w.waitForPerform.Store(true)
	w.events <- r1StopTestFrame()
	w.events <- gioapp.DestroyEvent{}
	stop := make(chan struct{})
	close(stop) // 比首个事件更早到达

	view, err := WithOnDestroy(&countingView{text: "r1-stop"}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, view, material.NewTheme(), stop, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("正常关窗应返回 nil: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("受控循环未在 3 秒内返回")
	}
	if got := w.performs.Load(); got != 1 {
		t.Fatalf("driver 就绪后应恰好执行一次 ActionClose，实际 %d", got)
	}
	if got := w.invalidates.Load(); got != 1 {
		t.Fatalf("关闭请求后应恰好失效一次，实际 %d", got)
	}
}

// TestR1DestroyTakesPriorityOverPendingStop 首个事件就是 Destroy 时，
// 不启动 watcher、不发冗余 Perform（Destroy 优先）。
func TestR1DestroyTakesPriorityOverPendingStop(t *testing.T) {
	w := &r1SeqWindow{events: make(chan event.Event, 2), performed: make(chan struct{}, 1)}
	w.events <- gioapp.DestroyEvent{}
	stop := make(chan struct{})
	close(stop)

	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, &countingView{}, material.NewTheme(), stop, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("正常关窗应返回 nil: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("受控循环未在 3 秒内返回")
	}
	if got := w.performs.Load(); got != 0 {
		t.Fatalf("Destroy 优先时不应再发关闭动作，实际 %d", got)
	}
}

// TestR1DestroyCauseAndCallbackOnceAfterStopFix 修复后仍交出
// DestroyEvent.Err（双判别），且 WithOnDestroy 回调恰一次。
func TestR1DestroyCauseAndCallbackOnceAfterStopFix(t *testing.T) {
	cause := errors.New("r1 premature destroy")
	w := &r1SeqWindow{events: make(chan event.Event, 2), performed: make(chan struct{}, 1)}
	w.events <- gioapp.DestroyEvent{Err: cause}
	stop := make(chan struct{})

	callbacks := 0
	view, err := WithOnDestroy(&countingView{text: "r1-cause"}, func() { callbacks++ })
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, view, material.NewTheme(), stop, nil) }()
	select {
	case got := <-done:
		if !errors.Is(got, cause) || !errors.Is(got, ErrWindowDestroy) {
			t.Fatalf("销毁原因未双判别透传: %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("受控循环未在 3 秒内返回")
	}
	if callbacks != 1 {
		t.Fatalf("WithOnDestroy 回调应恰一次，实际 %d", callbacks)
	}
	if got := w.performs.Load(); got != 0 {
		t.Fatalf("Destroy 分支不应发起 Perform: %d", got)
	}
}

// TestR1StopWatcherDoesNotOutliveLoop 循环结束后 watcher 不再发起动作。
// 用受控序列 + 有界观察窗口确认没有迟到的 Perform。
func TestR1StopWatcherDoesNotOutliveLoop(t *testing.T) {
	w := &r1SeqWindow{events: make(chan event.Event, 2), performed: make(chan struct{}, 1)}
	w.events <- r1StopTestFrame()
	w.events <- gioapp.DestroyEvent{}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, &countingView{}, material.NewTheme(), stop, nil) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("受控循环未在 3 秒内返回")
	}
	close(stop) // 循环结束后才请求停止：watcher 应已随 loopDone 退出
	select {
	case <-w.performed:
		t.Fatal("循环结束后 watcher 仍发起了 Perform（遗留 watcher）")
	case <-time.After(100 * time.Millisecond):
	}
	if got := w.performs.Load(); got != 0 {
		t.Fatalf("循环结束后不应有迟到关闭动作，实际 %d", got)
	}
}
