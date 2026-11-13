//go:build !nativewindow

// R29-C R4：同一宿主并发 Run 的所有权结果线性化测试（默认无窗口集合）。
//
// 覆盖：成功对照（等待者不依赖 Gio Main 返回即拿到结果）、两个等待者共享
// 同一拒绝、一窗取消与 Run 并发、ExitAll 与 Run 并发（hostStarting 排队
// 关闭）。全部用 channel/原子推进，不创建平台窗口。
package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/widget/material"
)

// 成功对照：胜者发布启动结果**不等待 Main 返回**；等待者在 Main 仍阻塞时
// 就拿到 nil。零窗宿主随后完整收口。
func TestR4RunVerdictPublishedBeforeMainReturns(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	entered := make(chan struct{})
	release := make(chan struct{})
	mainFunc = func() { close(entered); <-release }
	t.Cleanup(func() { mainFunc = orig })

	host := NewWindowHost()
	winner := make(chan error, 1)
	go func() { winner <- host.Run() }()
	select {
	case <-entered: // 胜者已进入 Main（发布已完成）
	case <-time.After(3 * time.Second):
		t.Fatal("胜者未进入 Main")
	}

	waiter := make(chan error, 1)
	go func() { waiter <- host.Run() }()
	select {
	case err := <-waiter:
		if err != nil {
			t.Fatalf("等待者应看到成功，实得 %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待者被阻塞到 Main 返回：发布点错误地依赖 Main 返回")
	}

	close(release)
	if err := <-winner; err != nil {
		t.Fatalf("胜者应成功，实得 %v", err)
	}
	if res := host.Wait(); !res.Drained || res.Closed != 0 {
		t.Fatalf("零窗收口异常: %+v", res)
	}
}

// 两个等待者 + 胜者：同一宿主被拒时，三个调用返回同一拒绝，且被拒宿主
// 不进入事件源（R3 边界保持）。
func TestR4TwoWaitersShareSameRejection(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	var mainCalls atomic.Int32
	mainFunc = func() { mainCalls.Add(1) }
	t.Cleanup(func() { mainFunc = orig })

	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("旧入口应照常可用: %v", err)
	}
	host := NewWindowHost()
	w := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"A": w})
	if _, err := host.Open("A", &s1View{}, material.NewTheme(), Config{Title: "A"}); err != nil {
		t.Fatal(err)
	}

	// 持住所有权锁，把胜者固定在 claim 内（协调者探针同款门闩）。
	gioMain.mu.Lock()
	winner := make(chan error, 1)
	go func() { winner <- host.Run() }()
	r1SpinUntil(t, "hostStarting", func() bool {
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.state == hostStarting
	})
	waiters := []chan error{make(chan error, 1), make(chan error, 1)}
	for _, ch := range waiters {
		go func(ch chan error) { ch <- host.Run() }(ch)
	}
	gioMain.mu.Unlock()

	for i, ch := range append([]chan error{winner}, waiters...) {
		select {
		case err := <-ch:
			if !errors.Is(err, ErrGioMainAlreadyOwned) {
				t.Fatalf("调用方 %d 应共享同一拒绝，实得 %v", i, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("调用方 %d 未在有界时间内完成", i)
		}
	}
	if got := mainCalls.Load(); got != 1 {
		t.Fatalf("Main 应只调用一次，实得 %d", got)
	}
	if got := w.enters.Load(); got != 0 {
		t.Fatalf("被拒宿主不得进入事件源，实得 %d", got)
	}
	if res := host.Wait(); !res.Rejected || res.Closed != 0 {
		t.Fatalf("被拒宿主结果异常: %+v", res)
	}
}

// 一窗取消与 Run 并发：两种交错都有明确结果——取消赢则该窗无事件源，
// Run 赢则 Cancel 明确失败；宿主计数守恒、收口有界。
func TestR4CancelConcurrentWithRunIsLinearized(t *testing.T) {
	wa := &r3ProbeWindow{s2Window: newS2Window()}
	wb := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"A": wa, "B": wb})
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	mainFunc = func() {}
	t.Cleanup(func() { mainFunc = orig })

	host := NewWindowHost()
	ha, err := host.Open("A", &s1View{}, material.NewTheme(), Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	hb, err := host.Open("B", &s1View{}, material.NewTheme(), Config{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	var cancelErr error
	go func() { defer wg.Done(); cancelErr = ha.Cancel() }()
	go func() { defer wg.Done(); _ = host.Run() }()
	wg.Wait()

	if ha.Canceled() {
		if cancelErr != nil {
			t.Fatalf("取消成功的路径不应报错: %v", cancelErr)
		}
		if wa.enters.Load() != 0 {
			t.Fatalf("被取消窗口不得进入事件源，实得 %d", wa.enters.Load())
		}
		wb.frame()
		s2WaitFrames(t, hb, 1)
		if err := hb.Close(); err != nil {
			t.Fatal(err)
		}
		s2WaitDone(t, hb)
		res := host.Wait()
		if !res.Drained || res.Canceled != 1 || res.Closed != 1 {
			t.Fatalf("取消赢的收口异常: %+v", res)
		}
	} else {
		if !errors.Is(cancelErr, errWindowNotCancelable) {
			t.Fatalf("Run 赢时 Cancel 应明确失败，实得 %v", cancelErr)
		}
		wa.frame()
		wb.frame()
		s2WaitFrames(t, ha, 1)
		s2WaitFrames(t, hb, 1)
		if err := ha.Close(); err != nil {
			t.Fatal(err)
		}
		if err := hb.Close(); err != nil {
			t.Fatal(err)
		}
		s2WaitDone(t, ha)
		s2WaitDone(t, hb)
		res := host.Wait()
		if !res.Drained || res.Canceled != 0 || res.Closed != 2 {
			t.Fatalf("Run 赢的收口异常: %+v", res)
		}
	}
}

// ExitAll 与 Run 并发（胜者在 claim 内）：hostStarting 只排队关闭请求；
// Run 成功启动后由各窗 loop 交付，结果 Explicit/Code=7、有界收口。
func TestR4ExitAllDuringStartingDeliversQueuedCloses(t *testing.T) {
	wa := &r3ProbeWindow{s2Window: newS2Window()}
	wb := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"A": wa, "B": wb})
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	mainFunc = func() {}
	t.Cleanup(func() { mainFunc = orig })

	host := NewWindowHost()
	ha, err := host.Open("A", &s1View{}, material.NewTheme(), Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	hb, err := host.Open("B", &s1View{}, material.NewTheme(), Config{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}

	gioMain.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- host.Run() }()
	r1SpinUntil(t, "hostStarting", func() bool {
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.state == hostStarting
	})
	host.ExitAll(7) // 胜者尚在 claim：只排队关闭，不改状态
	gioMain.mu.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("胜者应成功启动: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("胜者未在有界时间内完成")
	}

	// 给每窗一帧，让排队的关闭请求交付。
	wa.frame()
	wb.frame()
	resCh := make(chan HostResult, 1)
	go func() { resCh <- host.Wait() }()
	var res HostResult
	select {
	case res = <-resCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ExitAll 并发收口未在有界时间内完成")
	}
	if !res.Explicit || res.Code != 7 || !res.Drained || res.Closed != 2 {
		t.Fatalf("ExitAll 并发收口异常: %+v", res)
	}
	for _, h := range []*WindowHandle{ha, hb} {
		select {
		case <-h.Done():
		default:
			t.Fatalf("窗口 %q 未收口", h.Label())
		}
	}
	if wa.closes.Load() != 1 || wb.closes.Load() != 1 {
		t.Fatalf("每窗应恰一次关闭请求: A=%d B=%d", wa.closes.Load(), wb.closes.Load())
	}
}
