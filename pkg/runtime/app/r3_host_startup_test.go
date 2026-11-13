//go:build !nativewindow

// R29-C R3：宿主启动边界与进程级所有权的受控测试（默认无窗口集合）。
//
// 覆盖：被拒宿主无 Event / 无后端调用 / 无遗留等待者；旧入口先行；Host 先行；
// 两个 Host 竞争；Open/Run/ExitAll 并发；Run 前取消已注册窗口；零窗；以及
// 旧单窗进程入口在「Main 已被宿主占用」时的默认退出与显式返回两套收口。
// 全部用 channel 与原子计数推进，不创建平台窗口、不注入键鼠。
package app

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/widget/material"
)

// r3ProbeWindow 统计 Event 进入次数，后端行为沿用 s2Window（无副作用）。
type r3ProbeWindow struct {
	*s2Window
	enters atomic.Int32
}

func (w *r3ProbeWindow) Event() event.Event {
	w.enters.Add(1)
	return w.s2Window.Event()
}

func r3UseWindows(t *testing.T, fakes map[string]windowEvents) {
	t.Helper()
	orig := hostWindowFactory
	hostWindowFactory = func(cfg Config) windowEvents { return fakes[cfg.Title] }
	t.Cleanup(func() { hostWindowFactory = orig })
}

func r3StepIndex(steps []string, want string) int {
	for i, s := range steps {
		if s == want {
			return i
		}
	}
	return -1
}

// 被拒宿主：无 Event、无后端调用、无遗留窗口/等待者，结果可判别且重复 Run 稳定。
func TestR3RejectedHostLeavesNoEventBackendOrWaiter(t *testing.T) {
	first := NewWindowHost()
	r3StartHost(t, first) // 首个宿主取得进程级 Main（零窗，已收口）

	w := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"rejected": w})
	second := NewWindowHost()
	handle, err := second.Open("rejected", &s1View{}, material.NewTheme(), Config{Title: "rejected"})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("第二个宿主应被明确拒绝，实得 %v", err)
	}
	if got := w.enters.Load(); got != 0 {
		t.Fatalf("被拒宿主不得进入事件源，实得 %d 次 Event()", got)
	}
	if w.closes.Load() != 0 || w.invalidates.Load() != 0 {
		t.Fatalf("被拒宿主不得触后端: closes=%d invalidates=%d", w.closes.Load(), w.invalidates.Load())
	}
	if !handle.Rejected() || handle.Started() || handle.Canceled() {
		t.Fatalf("句柄状态异常: rejected=%t started=%t canceled=%t",
			handle.Rejected(), handle.Started(), handle.Canceled())
	}
	select {
	case <-handle.Done():
	default:
		t.Fatal("被拒窗口必须立即收口（无遗留等待者）")
	}
	if !errors.Is(handle.Err(), ErrGioMainAlreadyOwned) {
		t.Fatalf("句柄错误应可判别，实得 %v", handle.Err())
	}
	if got := second.Active(); got != 0 {
		t.Fatalf("被拒宿主不得留下登记窗口，实得 %d", got)
	}
	res := second.Wait()
	if !res.Rejected || !res.Drained || res.Closed != 0 || !errors.Is(res.Err, ErrGioMainAlreadyOwned) {
		t.Fatalf("被拒宿主结果异常: %+v", res)
	}
	if err := second.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("重复 Run 应返回同一拒绝，实得 %v", err)
	}
	if got := w.enters.Load(); got != 0 {
		t.Fatalf("重复 Run 仍不得进入事件源，实得 %d", got)
	}
}

// 旧入口先行：Host 被拒且不进入事件源（R3 覆盖先注册后启动的顺序）。
func TestR3LegacyFirstThenHostRejectedWithoutEventSource(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	var legacyCalls atomic.Int32
	mainFunc = func() { legacyCalls.Add(1) }
	t.Cleanup(func() { mainFunc = orig })

	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("旧单窗入口应照常可用: %v", err)
	}
	w := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"A": w})
	host := NewWindowHost()
	handle, err := host.Open("A", &s1View{}, material.NewTheme(), Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("旧入口先行后 Host 应被拒绝，实得 %v", err)
	}
	if legacyCalls.Load() != 1 {
		t.Fatalf("旧入口 Main 应恰一次，实得 %d", legacyCalls.Load())
	}
	if w.enters.Load() != 0 || w.closes.Load() != 0 {
		t.Fatalf("被拒宿主不得触后端: enters=%d closes=%d", w.enters.Load(), w.closes.Load())
	}
	if !handle.Rejected() {
		t.Fatal("句柄应标记为被拒")
	}
	if res := host.Wait(); !res.Rejected || res.Closed != 0 {
		t.Fatalf("被拒宿主结果异常: %+v", res)
	}
}

// 两个 Host 竞争 + 同宿主重复 Run：不得把 Run 次数误记为 Main 调用次数。
func TestR3TwoHostsCompeteAndRepeatRunIsNotAnotherMainCall(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	var mainCalls atomic.Int32
	mainFunc = func() { mainCalls.Add(1) }
	t.Cleanup(func() { mainFunc = orig })

	a := NewWindowHost()
	b := NewWindowHost()
	if err := a.Run(); err != nil {
		t.Fatalf("首个宿主应取得所有权: %v", err)
	}
	if err := a.Run(); err != nil {
		t.Fatalf("同宿主重复 Run 应幂等: %v", err)
	}
	if err := a.Run(); err != nil {
		t.Fatalf("同宿主第三次 Run 应幂等: %v", err)
	}
	if got := mainCalls.Load(); got != 1 {
		t.Fatalf("Main 应只调用一次，实得 %d", got)
	}
	if err := b.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("第二个宿主应被拒绝，实得 %v", err)
	}
	owner, calls, blocked := gioMainState()
	if owner != a.id || calls != 1 || blocked != 1 {
		t.Fatalf("诊断状态异常: owner=%q calls=%d blocked=%d（calls 不得等于 Run 次数）", owner, calls, blocked)
	}
	if got := mainCalls.Load(); got != 1 {
		t.Fatalf("被拒宿主不得再进 Main，实得 %d", got)
	}
}

// Run 前取消：被取消窗口不启动事件源、不触后端；其余窗口正常服务并收口。
func TestR3CancelBeforeRunNeverStartsWindow(t *testing.T) {
	wa := &r3ProbeWindow{s2Window: newS2Window()}
	wb := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"A": wa, "B": wb})
	host := NewWindowHost()
	ha, err := host.Open("A", &s1View{}, material.NewTheme(), Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	hb, err := host.Open("B", &s1View{}, material.NewTheme(), Config{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ha.Cancel(); err != nil {
		t.Fatalf("Run 前取消应成功: %v", err)
	}
	if err := ha.Cancel(); err != nil {
		t.Fatalf("重复取消应幂等: %v", err)
	}
	if !ha.Canceled() || ha.Started() {
		t.Fatalf("取消状态异常: canceled=%t started=%t", ha.Canceled(), ha.Started())
	}
	if !errors.Is(ha.Err(), errWindowCanceled) {
		t.Fatalf("取消原因应可判别，实得 %v", ha.Err())
	}
	if got := host.Active(); got != 1 {
		t.Fatalf("取消后应只剩 1 个登记窗口，实得 %d", got)
	}
	r3StartHost(t, host)
	wb.frame()
	s2WaitFrames(t, hb, 1)
	if got := wa.enters.Load(); got != 0 {
		t.Fatalf("被取消窗口不得进入事件源，实得 %d", got)
	}
	if wa.closes.Load() != 0 || wa.invalidates.Load() != 0 {
		t.Fatalf("被取消窗口不得触后端: closes=%d invalidates=%d", wa.closes.Load(), wa.invalidates.Load())
	}
	if err := hb.Close(); err != nil {
		t.Fatal(err)
	}
	s2WaitDone(t, hb)
	res := host.Wait()
	if !res.Drained || res.Explicit || res.Canceled != 1 || res.Closed != 1 || len(res.Errors) != 0 {
		t.Fatalf("取消+自然收口结果异常: %+v", res)
	}
}

// 零窗：仍只认领一次 Main，Wait 立即 drained，重复 Run 幂等。
func TestR3ZeroWindowRunClaimsMainOnce(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	var mainCalls atomic.Int32
	mainFunc = func() { mainCalls.Add(1) }
	t.Cleanup(func() { mainFunc = orig })

	host := NewWindowHost()
	if err := host.Run(); err != nil {
		t.Fatalf("零窗 Run 应正常: %v", err)
	}
	if err := host.Run(); err != nil {
		t.Fatalf("零窗重复 Run 应幂等: %v", err)
	}
	res := host.Wait()
	if !res.Drained || res.Explicit || res.Closed != 0 || res.Rejected {
		t.Fatalf("零窗收口结果异常: %+v", res)
	}
	if host.Active() != 0 {
		t.Fatalf("零窗宿主不应留下登记窗口，实得 %d", host.Active())
	}
	if got := mainCalls.Load(); got != 1 {
		t.Fatalf("零窗 Main 应只一次，实得 %d", got)
	}
}

// Open/Run/ExitAll 并发：有界收口、无重复交付、每个已开窗口都有明确结果。
func TestR3ConcurrentOpenRunExitAllIsBounded(t *testing.T) {
	const n = 8
	fakes := make(map[string]windowEvents, n)
	ws := make([]*r3ProbeWindow, n)
	for i := 0; i < n; i++ {
		w := &r3ProbeWindow{s2Window: newS2Window()}
		ws[i] = w
		fakes["w"+strconv.Itoa(i)] = w
	}
	r3UseWindows(t, fakes)
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	mainFunc = func() {}
	t.Cleanup(func() { mainFunc = orig })

	host := NewWindowHost()
	var mu sync.Mutex
	var opened []*WindowHandle
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			label := "w" + strconv.Itoa(i)
			h, err := host.Open(label, &s1View{}, material.NewTheme(), Config{Title: label})
			if err == nil {
				mu.Lock()
				opened = append(opened, h)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	var race sync.WaitGroup
	race.Add(2)
	go func() { defer race.Done(); _ = host.Run() }()
	go func() { defer race.Done(); host.ExitAll(2) }()
	race.Wait()

	// 若事件泵已启动，每窗先给一帧，让排队的关闭请求可以交付。
	for i := 0; i < n; i++ {
		ws[i].frame()
	}
	done := make(chan HostResult, 1)
	go func() { done <- host.Wait() }()
	var res HostResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("并发 Open/Run/ExitAll 未在有界时间内收口")
	}
	if !res.Drained || !res.Explicit || res.Code != 2 {
		t.Fatalf("并发收口结果异常: %+v", res)
	}
	if res.Closed+res.Canceled != len(opened) {
		t.Fatalf("收口计数不守恒: closed=%d canceled=%d opened=%d",
			res.Closed, res.Canceled, len(opened))
	}
	for _, h := range opened {
		select {
		case <-h.Done():
		default:
			t.Fatalf("窗口 %q 未收口", h.Label())
		}
	}
	for i, w := range ws {
		if w.closes.Load() > 1 {
			t.Fatalf("窗口 w%d 被重复请求关闭 %d 次", i, w.closes.Load())
		}
		if w.enters.Load() > 2 {
			t.Fatalf("窗口 w%d 事件读取异常: %d", i, w.enters.Load())
		}
	}
}
