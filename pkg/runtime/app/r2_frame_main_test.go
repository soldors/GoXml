//go:build !nativewindow

// R29-C R2：帧提交握手正例、关窗保持用例、进程级 Main 混用顺序与并发边界。
// 全部为受控无窗口测试；合成事件不冒充真实后端帧交换/ack。
package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// r2FrameWindow 记录「第二次 Event() 是否发生在第一帧 Frame 回调之后」。
type r2FrameWindow struct {
	*s2Window
	reads          atomic.Int32
	frameDone      atomic.Bool
	secondSawFrame atomic.Bool
}

func (w *r2FrameWindow) Event() event.Event {
	switch w.reads.Add(1) {
	case 1:
		e := synthFrameEvent(&frameRecorder{})
		e.Frame = func(*op.Ops) { w.frameDone.Store(true) }
		return e
	case 2:
		w.secondSawFrame.Store(w.frameDone.Load())
		return gioapp.DestroyEvent{}
	default:
		return w.s2Window.Event()
	}
}

func r2UseWindow(t *testing.T, w windowEvents) {
	t.Helper()
	orig := hostWindowFactory
	hostWindowFactory = func(Config) windowEvents { return w }
	t.Cleanup(func() { hostWindowFactory = orig })
}

func r2WaitDone(t *testing.T, wh *WindowHandle) {
	t.Helper()
	select {
	case <-wh.Done():
	case <-time.After(3 * time.Second):
		t.Fatalf("窗口 %q 未在有界窗口内收口", wh.Label())
	}
}

// 帧握手正例：前一帧 Layout + Frame 回调完成后才允许下一次 Event()。
func TestR2FrameHandshakePositive(t *testing.T) {
	w := &r2FrameWindow{s2Window: newS2Window()}
	r2UseWindow(t, w)
	host := NewWindowHost()
	wh, err := host.Open("A", &s1View{}, material.NewTheme(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)
	r2WaitDone(t, wh)
	if !w.secondSawFrame.Load() {
		t.Fatal("下一次 Event() 早于前一帧 Frame 回调（帧握手失效）")
	}
	if got := wh.Frames(); got != 1 {
		t.Fatalf("应恰好处理一帧，实得 %d", got)
	}
	if res := host.Wait(); !res.Drained || res.Closed != 1 {
		t.Fatalf("宿主收口异常: %+v", res)
	}
}

// 重复/并发关窗：仍至多一次后端交付（R1 关窗语义保持）。
func TestR2RepeatedCloseOnlyOneDelivery(t *testing.T) {
	w := newS2Window()
	s2UseFakeWindows(t, map[string]*s2Window{"A": w})
	host := NewWindowHost()
	wh, err := host.Open("A", &s1View{}, material.NewTheme(), Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)
	w.frame()
	s2WaitFrames(t, wh, 1)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = wh.Close() }()
	}
	wg.Wait()
	r2WaitDone(t, wh)
	if got := w.closes.Load(); got != 1 {
		t.Fatalf("重复/并发 Close 应只交付一次，实得 %d", got)
	}
}

// 无下一帧时 Close：pump 阻塞在 Event()，Close 仍应被 loop 结算并收口。
func TestR2CloseWithoutNextFrame(t *testing.T) {
	w := newS2Window()
	s2UseFakeWindows(t, map[string]*s2Window{"A": w})
	host := NewWindowHost()
	wh, err := host.Open("A", &s1View{}, material.NewTheme(), Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)
	w.frame() // 只给一帧，之后不再投递任何事件
	s2WaitFrames(t, wh, 1)
	if err := wh.Close(); err != nil {
		t.Fatal(err)
	}
	r2WaitDone(t, wh)
	if got := w.closes.Load(); got != 1 {
		t.Fatalf("无后续帧时 Close 应交付一次关闭，实得 %d", got)
	}
	if res := host.Wait(); !res.Drained || res.Explicit || len(res.Errors) != 0 {
		t.Fatalf("宿主应自然收口: %+v", res)
	}
}

func r2InjectMain(t *testing.T) *atomic.Int32 {
	t.Helper()
	orig := mainFunc
	calls := &atomic.Int32{}
	mainFunc = func() { calls.Add(1) }
	t.Cleanup(func() { mainFunc = orig })
	return calls
}

// 正常单窗路径（无 Host）必须逐字兼容。
func TestR2LegacySingleWindowPathCompatible(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	calls := r2InjectMain(t)
	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("RunWith 正常路径失败: %v", err)
	}
	code, exits := -1, 0
	RunSingleWindowProcess(&s1View{}, Config{}, ProcessOptions{
		Theme: material.NewTheme(),
		next:  s1Events(1),
		exit:  func(c int) { exits++; code = c },
	})
	if exits != 1 || code != 0 || calls.Load() != 2 {
		t.Fatalf("单窗正常路径异常: exits=%d code=%d mainCalls=%d", exits, code, calls.Load())
	}
}

// 混用顺序 1：旧入口先行 → Host.Run 明确拒绝，Main 不重复进入。
func TestR2LegacyFirstThenHostRejected(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	calls := r2InjectMain(t)
	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); err != nil {
		t.Fatal(err)
	}
	host := NewWindowHost()
	if err := host.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("旧入口先行后 Host 应被拒绝，实得 %v", err)
	}
	owner, mainCalls, blocked := gioMainState()
	if owner != gioMainLegacyOwner || mainCalls != 1 || blocked != 1 || calls.Load() != 1 {
		t.Fatalf("所有权状态异常: owner=%q calls=%d blocked=%d main=%d",
			owner, mainCalls, blocked, calls.Load())
	}
}

// 混用顺序 2：Host 先行 → 旧 RunWith / 单窗进程入口都被明确拒绝，
// 且绝不第二次进入 Main（这是 R1 包装计数未能阻止的路径）。
func TestR2HostFirstThenLegacyRejected(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	calls := r2InjectMain(t)
	host := NewWindowHost()
	if err := host.Run(); err != nil {
		t.Fatal(err)
	}
	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("Host 先行后 RunWith 应被拒绝，实得 %v", err)
	}
	mixCode, mixExits := -1, 0
	var mixErr error
	RunSingleWindowProcess(&s1View{}, Config{}, ProcessOptions{
		Theme:    material.NewTheme(),
		next:     s1Events(1),
		Observer: func(res ExitResult) { mixErr = res.Err },
		exit:     func(c int) { mixExits++; mixCode = c },
	})
	if calls.Load() != 1 || mixExits != 1 || mixCode != 1 || !errors.Is(mixErr, ErrGioMainAlreadyOwned) {
		t.Fatalf("Host 先行混用未精确失败: main=%d exits=%d code=%d err=%v",
			calls.Load(), mixExits, mixCode, mixErr)
	}
	owner, mainCalls, blocked := gioMainState()
	if owner != host.id || mainCalls != 1 || blocked != 2 {
		t.Fatalf("所有权状态异常: owner=%q calls=%d blocked=%d", owner, mainCalls, blocked)
	}
}

// 并发入口：多个 Host 同时认领只有一个成功且 Main 只调用一次；
// 之后并发旧入口请求全部被阻止。
func TestR2ConcurrentGioMainClaims(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	calls := r2InjectMain(t)
	hosts := []*WindowHost{NewWindowHost(), NewWindowHost(), NewWindowHost(), NewWindowHost()}
	results := make([]error, len(hosts))
	var wg sync.WaitGroup
	for i := range hosts {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i] = hosts[i].Run() }(i)
	}
	wg.Wait()
	success := 0
	for i, err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrGioMainAlreadyOwned) {
			t.Fatalf("并发认领失败应为明确错误: %v", err)
		}
		_ = i
	}
	if success != 1 || calls.Load() != 1 {
		t.Fatalf("并发认领应只有一个成功且 Main 只一次: success=%d main=%d", success, calls.Load())
	}
	owner, _, blocked := gioMainState()
	if owner == gioMainLegacyOwner || blocked != len(hosts)-1 {
		t.Fatalf("并发状态异常: owner=%q blocked=%d", owner, blocked)
	}

	// Host 已认领后，并发旧入口请求全部被阻止，Main 不再进入。
	var mixWG sync.WaitGroup
	var rejected atomic.Int32
	for i := 0; i < 16; i++ {
		mixWG.Add(1)
		go func() {
			defer mixWG.Done()
			if errors.Is(claimGioMain(gioMainLegacyOwner), ErrGioMainAlreadyOwned) {
				rejected.Add(1)
			}
		}()
	}
	mixWG.Wait()
	if rejected.Load() != 16 || calls.Load() != 1 {
		t.Fatalf("并发旧入口未全部被阻止: rejected=%d main=%d", rejected.Load(), calls.Load())
	}
}
