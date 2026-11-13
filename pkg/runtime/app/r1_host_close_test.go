//go:build !nativewindow

// R29-C R1：关窗时序与进程级 Main 所有权的受控门闩测试。
// 全部使用 channel/原子条件推进，不靠 sleep 证明调用时点；不启动原生窗口。
package app

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/widget/material"
)

// r1LatchWindow 用门闩把「首个 Event 进入/返回」「第二个 DestroyEvent 被
// 后端读出」变成可观察时点；Perform 仍交给 s2Window 计数。
type r1LatchWindow struct {
	*s2Window
	firstEntered   chan struct{}
	firstGate      chan struct{}
	destroyOffered chan struct{}
	mode           string // "init-fail" | "destroy-race"
	eventsRead     atomic.Int32
}

func newR1LatchWindow(mode string) *r1LatchWindow {
	return &r1LatchWindow{
		s2Window:       newS2Window(),
		firstEntered:   make(chan struct{}),
		firstGate:      make(chan struct{}),
		destroyOffered: make(chan struct{}),
		mode:           mode,
	}
}

func (w *r1LatchWindow) Event() event.Event {
	switch n := w.eventsRead.Add(1); {
	case n == 1:
		close(w.firstEntered)
		<-w.firstGate
		if w.mode == "init-fail" {
			return gioapp.DestroyEvent{Err: errors.New("driver init failed")}
		}
		return synthFrameEvent(&frameRecorder{})
	case n == 2 && w.mode == "destroy-race":
		close(w.destroyOffered)
		return gioapp.DestroyEvent{}
	default:
		return w.s2Window.Event()
	}
}

func r1UseWindow(t *testing.T, w windowEvents) {
	t.Helper()
	orig := hostWindowFactory
	hostWindowFactory = func(Config) windowEvents { return w }
	t.Cleanup(func() { hostWindowFactory = orig })
}

// r1SpinUntil 是无 sleep 的条件门闩（只用于等待状态，不用于证明时序）。
func r1SpinUntil(t *testing.T, why string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("未在 3s 内满足门闩: %s", why)
		}
		runtime.Gosched()
	}
}

func r1WaitDone(t *testing.T, wh *WindowHandle) {
	t.Helper()
	select {
	case <-wh.Done():
	case <-time.After(3 * time.Second):
		t.Fatalf("窗口 %q 未在有界窗口内收口", wh.Label())
	}
}

// 协调者红 1 的扩展：首事件初始化失败（第一个事件就是 DestroyEvent）时，
// 提前排队的 Close 不得触后端，错误仍可判别。
func TestR1FirstEventInitFailureSuppressesBackend(t *testing.T) {
	w := newR1LatchWindow("init-fail")
	r1UseWindow(t, w)
	host := NewWindowHost()
	wh, err := host.Open("init", &s1View{}, material.NewTheme(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)
	<-w.firstEntered // 首个 Event 已进入，驱动尚未就绪
	if err := wh.Close(); err != nil {
		t.Fatal(err)
	}
	close(w.firstGate)
	r1WaitDone(t, wh)
	if got := w.closes.Load(); got != 0 {
		t.Fatalf("首事件初始化失败后不得触后端，实得 %d", got)
	}
	if !errors.Is(wh.Err(), ErrWindowDestroy) {
		t.Fatalf("应保留窗口销毁错误类别: %v", wh.Err())
	}
	if res := host.Wait(); !res.Drained || len(res.Errors) != 1 {
		t.Fatalf("初始化失败应汇入宿主结果: %+v", res)
	}
}

// Close 与 Destroy 竞争：后端已读出 DestroyEvent 后，Close 与 flushClose
// 都不得再交付关闭请求。
func TestR1CloseCompetesWithDestroyNeverTouchesBackend(t *testing.T) {
	w := newR1LatchWindow("destroy-race")
	r1UseWindow(t, w)
	host := NewWindowHost()
	wh, err := host.Open("race", &s1View{}, material.NewTheme(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)
	<-w.firstEntered
	close(w.firstGate) // 首个 Event 返回 FrameEvent → 驱动就绪
	<-w.destroyOffered // 后端已交出第二个 DestroyEvent
	r1SpinUntil(t, "destroySeen", func() bool { return wh.destroySeen.Load() })
	if err := wh.Close(); err != nil {
		t.Fatal(err)
	}
	r1WaitDone(t, wh)
	if got := w.closes.Load(); got != 0 {
		t.Fatalf("DestroyEvent 已进入后不得再触后端，实得 %d", got)
	}
	if wh.CloseDelivered() {
		t.Fatal("关闭请求不应被交付")
	}
}

// 连续/并发 Close：至多一次后端交付，窗口一次收口。
func TestR1ConcurrentCloseDeliversAtMostOnce(t *testing.T) {
	w := newS2Window()
	s2UseFakeWindows(t, map[string]*s2Window{"many": w})
	host := NewWindowHost()
	wh, err := host.Open("many", &s1View{}, material.NewTheme(), Config{Title: "many"})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)
	w.frame()
	s2WaitFrames(t, wh, 1)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = wh.Close() }()
	}
	wg.Wait()
	r1WaitDone(t, wh)
	if got := w.closes.Load(); got != 1 {
		t.Fatalf("并发 Close 应至多一次交付，实得 %d", got)
	}
	if res := host.Wait(); !res.Drained || res.Closed != 1 {
		t.Fatalf("宿主收口异常: %+v", res)
	}
}

// 零窗口显式退出：不触任何后端、结果可判别、重复调用不改写。
func TestR1ZeroWindowExplicitExit(t *testing.T) {
	host := NewWindowHost()
	host.ExitAll(4)
	res := host.Wait()
	if !res.Explicit || !res.Drained || res.Code != 4 || res.Closed != 0 {
		t.Fatalf("零窗口显式退出结果异常: %+v", res)
	}
	host.ExitAll(9)
	if again := host.Wait(); again.Code != 4 || again.Closed != 0 {
		t.Fatalf("重复显式退出不得改写结果: %+v", again)
	}
}

// 关 A 保 B：A 的关闭请求只交给 A，B 仍能继续布局。
func TestR1CloseAKeepsBServing(t *testing.T) {
	fakes := map[string]*s2Window{"A": newS2Window(), "B": newS2Window()}
	s2UseFakeWindows(t, fakes)
	host := NewWindowHost()
	th := material.NewTheme()
	ha, err := host.Open("A", &s1View{}, th, Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	hb, err := host.Open("B", &s1View{}, th, Config{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)
	fakes["A"].frame()
	fakes["B"].frame()
	s2WaitFrames(t, ha, 1)
	s2WaitFrames(t, hb, 1)
	if err := ha.Close(); err != nil {
		t.Fatal(err)
	}
	r1WaitDone(t, ha)
	if fakes["A"].closes.Load() != 1 || fakes["B"].closes.Load() != 0 {
		t.Fatal("关 A 不得串停/串写 B 的后端")
	}
	fakes["B"].frame()
	s2WaitFrames(t, hb, 2)
	if err := hb.Close(); err != nil {
		t.Fatal(err)
	}
	r1WaitDone(t, hb)
	if res := host.Wait(); !res.Drained || res.Explicit || res.Closed != 2 {
		t.Fatalf("两窗自然收口异常: %+v", res)
	}
}

// 任务 2：两个 Host 竞争进程级 Main，第二个明确拒绝；首个 Host 重复 Run
// 也只调用一次 Main。
func TestR1SingleGioMainOwnerRejectsSecondHost(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	var calls atomic.Int32
	mainFunc = func() { calls.Add(1) }
	t.Cleanup(func() { mainFunc = orig })

	a := NewWindowHost()
	b := NewWindowHost()
	if err := a.Run(); err != nil {
		t.Fatalf("首个宿主应取得 Main 所有权: %v", err)
	}
	if err := a.Run(); err != nil {
		t.Fatalf("同一宿主重复 Run 应幂等: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Main 应只调用一次，实得 %d", got)
	}
	if err := b.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("第二个宿主应被明确拒绝，实得 %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("被拒绝的宿主不得再调用 Main，实得 %d", got)
	}
}

// 任务 2：旧单窗入口（RunWith）先行调用 Main 后，Host.Run 明确拒绝；
// 旧入口 API 本身保持可用。
func TestR1LegacyMainAndHostMixIsRejected(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	var legacyCalls atomic.Int32
	mainFunc = func() { legacyCalls.Add(1) }
	t.Cleanup(func() { mainFunc = orig })

	// R2：旧入口通过 claimGioMain 认领（不再靠包装计数）。
	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("旧 RunWith 应仍可用: %v", err)
	}
	if got := legacyCalls.Load(); got != 1 {
		t.Fatalf("旧入口应照常调用 Main，实得 %d", got)
	}
	host := NewWindowHost()
	if err := host.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("旧入口与 Host 混用应被明确拒绝，实得 %v", err)
	}
	// 混用被拒绝后没有第二次 Main 调用，也未扩大退出政策。
	if got := legacyCalls.Load(); got != 1 {
		t.Fatalf("混用不得重复调用 Main，实得 %d", got)
	}
}
