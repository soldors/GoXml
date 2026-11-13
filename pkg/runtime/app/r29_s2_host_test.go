//go:build !nativewindow

// R29-C S2：宿主候选的受控**真实生产分支**证明（默认无窗口集合）。
//
// 通过 hostWindowFactory 注入纯内存 windowEvents，但 Router / Owner /
// LifecyclePage / WithOnDestroy / WindowHost.loopWindow 全部走生产实现。
// 合成帧只用于驱动 Layout，不冒充原生 FrameEvent 或真实 DestroyEvent。
package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/lifecycle"
	"github.com/goxml/goxml/pkg/runtime/router"
)

// s2Window 是受控窗口：Perform(ActionClose) 交付一个 DestroyEvent，
// 与 Gio 的关闭请求语义对齐；FrameEvent 由测试显式投递。
type s2Window struct {
	events      chan event.Event
	closes      atomic.Int32
	invalidates atomic.Int32
}

func newS2Window() *s2Window {
	return &s2Window{events: make(chan event.Event, 32)}
}

func (w *s2Window) Event() event.Event { return <-w.events }
func (w *s2Window) Perform(a system.Action) {
	if a == system.ActionClose {
		w.closes.Add(1)
		w.events <- gioapp.DestroyEvent{}
	}
}
func (w *s2Window) Invalidate() { w.invalidates.Add(1) }
func (w *s2Window) frame()      { w.events <- synthFrameEvent(&frameRecorder{}) }

func s2UseFakeWindows(t *testing.T, fakes map[string]*s2Window) {
	t.Helper()
	orig := hostWindowFactory
	hostWindowFactory = func(cfg Config) windowEvents { return fakes[cfg.Title] }
	t.Cleanup(func() { hostWindowFactory = orig })
}

// s2RouterSet 是**每个窗口各自**的 Router / Owner / 页面状态。
type s2RouterSet struct {
	r        *router.Router
	owner    *lifecycle.Owner
	page     *s1View
	mounts   atomic.Int32
	unmounts atomic.Int32
	pageCtx  atomic.Value
	canceled atomic.Bool
}

func newS2RouterSet(t *testing.T) (*s2RouterSet, View) {
	t.Helper()
	s := &s2RouterSet{r: router.New(), page: &s1View{}}
	if err := s.r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	owner, err := s.r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	s.owner = owner
	if err := s.r.Register("/home", func(context.Context) (router.View, error) {
		return &router.LifecyclePage{
			View: s.page,
			Mount: func(ctx context.Context) {
				s.pageCtx.Store(ctx)
				s.mounts.Add(1)
			},
			Unmount: func() {
				if pc, ok := s.pageCtx.Load().(context.Context); ok && errors.Is(pc.Err(), context.Canceled) {
					s.canceled.Store(true)
				}
				s.unmounts.Add(1)
			},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.r.Navigate("/home"); err != nil {
		t.Fatal(err)
	}
	// ADR-0018：真实 DestroyEvent 的回调里由该窗口 UI owner 完成 Finalize。
	view, err := WithOnDestroy(s.r.View(), s.r.LifecycleFinalize)
	if err != nil {
		t.Fatal(err)
	}
	return s, view
}

func s2WaitFrames(t *testing.T, wh *WindowHandle, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for wh.Frames() < want {
		if time.Now().After(deadline) {
			t.Fatalf("窗口 %q 未在 3s 内处理 %d 帧（实得 %d）", wh.Label(), want, wh.Frames())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func s2WaitDone(t *testing.T, wh *WindowHandle) {
	t.Helper()
	select {
	case <-wh.Done():
	case <-time.After(3 * time.Second):
		t.Fatalf("窗口 %q 未在有界窗口内收口", wh.Label())
	}
}

// TestS2TwoIndependentRoutersCloseAKeepsBServing 是主验收路径：
// 关 A 只清 A，B 保持服务；关 B 后宿主完整收口（非显式）。
func TestS2TwoIndependentRoutersCloseAKeepsBServing(t *testing.T) {
	fakes := map[string]*s2Window{"A": newS2Window(), "B": newS2Window()}
	s2UseFakeWindows(t, fakes)
	host := NewWindowHost()
	setA, viewA := newS2RouterSet(t)
	setB, viewB := newS2RouterSet(t)
	th := material.NewTheme()

	ha, err := host.Open("A", viewA, th, Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	hb, err := host.Open("B", viewB, th, Config{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if setA.r == setB.r || setA.owner == setB.owner {
		t.Fatal("两个窗口必须各有独立 Router/Owner")
	}
	if got := host.Active(); got != 2 {
		t.Fatalf("宿主应登记 2 个窗口，实得 %d", got)
	}

	fakes["A"].frame()
	fakes["B"].frame()
	s2WaitFrames(t, ha, 1)
	s2WaitFrames(t, hb, 1)

	// 关 A：该窗回调完成一次卸载；B 不受影响。
	if err := ha.Close(); err != nil {
		t.Fatal(err)
	}
	s2WaitDone(t, ha)
	if got := setA.unmounts.Load(); got != 1 || !setA.canceled.Load() || !setA.owner.CleanupDone() {
		t.Fatalf("A 应恰一次卸载且页面 Context 先取消: unmounts=%d canceled=%v cleanup=%v",
			got, setA.canceled.Load(), setA.owner.CleanupDone())
	}
	if setA.r.Context().Err() != nil {
		t.Fatal("A 的 Finalize 不应隐式取消 Router 根")
	}
	if got := host.Active(); got != 1 {
		t.Fatalf("关 A 后宿主应仍服务 1 窗，实得 %d", got)
	}
	if setB.unmounts.Load() != 0 || setB.owner.CleanupDone() || setB.r.Context().Err() != nil {
		t.Fatal("关 A 不得串停/串写 B 的 Router/Owner")
	}

	// B 保持服务：继续布局并响应。
	fakes["B"].frame()
	s2WaitFrames(t, hb, 2)

	// 关 B：宿主自然 drained。
	if err := hb.Close(); err != nil {
		t.Fatal(err)
	}
	s2WaitDone(t, hb)
	res := host.Wait()
	if !res.Drained || res.Explicit || res.Code != 0 || res.Closed != 2 || len(res.Errors) != 0 {
		t.Fatalf("最后一窗自然关闭应 drained、非显式、rc0、无错: %+v", res)
	}
	if got := setB.unmounts.Load(); got != 1 || !setB.owner.CleanupDone() {
		t.Fatalf("B 应恰一次卸载: unmounts=%d cleanup=%v", got, setB.owner.CleanupDone())
	}
	if fakes["A"].closes.Load() != 1 || fakes["B"].closes.Load() != 1 {
		t.Fatal("每窗应恰好一次 ActionClose")
	}
}

// TestS2ExplicitExitAllClosesEachWindowOnce 区分调用方显式退出。
func TestS2ExplicitExitAllClosesEachWindowOnce(t *testing.T) {
	fakes := map[string]*s2Window{"A": newS2Window(), "B": newS2Window()}
	s2UseFakeWindows(t, fakes)
	host := NewWindowHost()
	setA, viewA := newS2RouterSet(t)
	setB, viewB := newS2RouterSet(t)
	th := material.NewTheme()
	ha, err := host.Open("A", viewA, th, Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	hb, err := host.Open("B", viewB, th, Config{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	fakes["A"].frame()
	fakes["B"].frame()
	s2WaitFrames(t, ha, 1)
	s2WaitFrames(t, hb, 1)

	host.ExitAll(3)
	res := host.Wait()
	if !res.Explicit || res.Code != 3 || !res.Drained || res.Closed != 2 {
		t.Fatalf("显式退出应 Explicit=true/Code=3/Drained: %+v", res)
	}
	for _, wh := range []*WindowHandle{ha, hb} {
		s2WaitDone(t, wh)
	}
	if setA.unmounts.Load() != 1 || setB.unmounts.Load() != 1 {
		t.Fatal("显式退出应让每个 UI owner 各完成恰一次清理")
	}
	if fakes["A"].closes.Load() != 1 || fakes["B"].closes.Load() != 1 {
		t.Fatal("显式退出不得重复请求关闭")
	}
	// 幂等：再次 ExitAll 不改写结果、不重复关闭。
	host.ExitAll(9)
	if again := host.Wait(); again.Code != 3 || again.Closed != 2 {
		t.Fatalf("重复 ExitAll 不得改写结果: %+v", again)
	}
}

// TestS2OpenFailuresAndDuplicateCloseAreBounded 覆盖打开失败、重复关窗、
// 重复 DestroyEvent（一次清理）。
func TestS2OpenFailuresAndDuplicateCloseAreBounded(t *testing.T) {
	fakes := map[string]*s2Window{"A": newS2Window(), "DUP": newS2Window()}
	s2UseFakeWindows(t, fakes)
	host := NewWindowHost()
	_, view := newS2RouterSet(t)
	th := material.NewTheme()

	if _, err := host.Open("", view, th, Config{Title: "A"}); err == nil {
		t.Fatal("空 label 应被拒绝")
	}
	if _, err := host.Open("nilview", nil, th, Config{Title: "nilview"}); err == nil {
		t.Fatal("nil view 应被拒绝")
	}
	if _, err := host.Open("nilt", view, nil, Config{Title: "nilt"}); err == nil {
		t.Fatal("nil theme 应被拒绝")
	}
	if host.Active() != 0 {
		t.Fatal("打开失败不得记账")
	}
	ha, err := host.Open("A", view, th, Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Open("A", view, th, Config{Title: "DUP"}); err == nil {
		t.Fatal("重复 label 应被拒绝")
	}
	hb, err := host.Open("DUP", view, th, Config{Title: "DUP"})
	if err != nil {
		t.Fatal(err)
	}
	// R1 语义：驱动就绪前 Close 只排队，首个 Event 返回后至多一次交付。
	fakes["A"].frame()
	s2WaitFrames(t, ha, 1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = ha.Close() }()
	}
	wg.Wait()
	s2WaitDone(t, ha)
	if fakes["A"].closes.Load() != 1 {
		t.Fatalf("连续/并发 Close 应只交付一次关闭，实得 %d", fakes["A"].closes.Load())
	}

	// 另一窗口：已进入 DestroyEvent 时禁止后端调用（重复 Destroy 也不重复清理）。
	fakes["DUP"].events <- gioapp.DestroyEvent{}
	fakes["DUP"].events <- gioapp.DestroyEvent{}
	_ = hb.Close()
	_ = hb.Close()
	s2WaitDone(t, hb)
	if fakes["DUP"].closes.Load() != 0 {
		t.Fatalf("DestroyEvent 已到达后不得再触后端，实得 %d", fakes["DUP"].closes.Load())
	}
	res := host.Wait()
	if res.Closed != 2 || !res.Drained || len(res.Errors) != 0 {
		t.Fatalf("两窗收口结果异常: %+v", res)
	}
}

// TestS2ConcurrentBackgroundCancelUsesEachUIOwner 证明并发后台取消只取消、
// 卸载仍由各 UI owner 在关闭回调里完成，互不串停。
func TestS2ConcurrentBackgroundCancelUsesEachUIOwner(t *testing.T) {
	fakes := map[string]*s2Window{"A": newS2Window(), "B": newS2Window()}
	s2UseFakeWindows(t, fakes)
	host := NewWindowHost()
	setA, viewA := newS2RouterSet(t)
	setB, viewB := newS2RouterSet(t)
	th := material.NewTheme()
	ha, err := host.Open("A", viewA, th, Config{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	hb, err := host.Open("B", viewB, th, Config{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	fakes["A"].frame()
	fakes["B"].frame()
	s2WaitFrames(t, ha, 1)
	s2WaitFrames(t, hb, 1)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); setA.r.Shutdown() }()
	go func() { defer wg.Done(); setB.r.Shutdown() }()
	wg.Wait()

	for _, s := range []*s2RouterSet{setA, setB} {
		if !errors.Is(s.r.Context().Err(), context.Canceled) {
			t.Fatal("并发 Shutdown 应取消各自 Router 根")
		}
		if s.unmounts.Load() != 0 || s.owner.CleanupDone() {
			t.Fatal("后台取消只取消，不得卸载")
		}
	}
	// 两个窗口并发关闭：每窗 UI owner 各完成一次卸载。
	wg.Add(2)
	go func() { defer wg.Done(); ha.Close() }()
	go func() { defer wg.Done(); hb.Close() }()
	wg.Wait()
	s2WaitDone(t, ha)
	s2WaitDone(t, hb)
	if setA.unmounts.Load() != 1 || setB.unmounts.Load() != 1 ||
		!setA.owner.CleanupDone() || !setB.owner.CleanupDone() {
		t.Fatal("并发关闭后每个 UI owner 应各恰一次清理")
	}
	if res := host.Wait(); !res.Drained || res.Closed != 2 || len(res.Errors) != 0 {
		t.Fatalf("并发收口结果异常: %+v", res)
	}
}

// TestS2LegacyEntryPointsRemainUnchanged 守卫旧入口兼容性（R2 语义）：
// 单窗正常路径照常；Host 的 Main 只调用一次；Host 已认领后旧入口混用
// 明确返回 ErrGioMainAlreadyOwned / 退出码 1，绝不第二次进入 Main。
func TestS2LegacyEntryPointsRemainUnchanged(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	defer func() { mainFunc = orig }()
	var called atomic.Int32
	mainFunc = func() { called.Add(1) }

	// (1) 已批准的单窗正常路径（无 Host 认领）。
	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); err != nil {
		t.Fatalf("单窗 RunWith 应仍正常返回: %v", err)
	}
	exits, code := 0, -1
	RunSingleWindowProcess(&s1View{}, Config{}, ProcessOptions{
		Theme: material.NewTheme(),
		next:  s1Events(1),
		exit:  func(c int) { exits++; code = c },
	})
	if exits != 1 || code != 0 || called.Load() != 2 {
		t.Fatalf("单窗正常路径被改动: exits=%d code=%d mainCalls=%d", exits, code, called.Load())
	}

	// (2) 互斥场景：reset 后 Host 取得所有权，Main 只调用一次。
	resetGioMainForTest()
	host := NewWindowHost()
	if err := host.Run(); err != nil {
		t.Fatalf("首个宿主应取得 Main 所有权: %v", err)
	}
	if err := host.Run(); err != nil {
		t.Fatalf("同一宿主重复 Run 应幂等: %v", err)
	}
	if got := called.Load(); got != 3 {
		t.Fatalf("Host Main 应恰好一次，实得 %d", got)
	}

	// (3) Host 先行 + 旧 RunWith：明确失败，不进入 Main。
	if err := RunWith(&s1View{}, material.NewTheme(), Config{}); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("Host 先行后旧入口应被明确拒绝，实得 %v", err)
	}
	// (4) Host 先行 + 单窗进程入口：Observer 可见错误、exit(1)、不进入 Main。
	mixExits, mixCode := 0, -1
	var mixErr error
	RunSingleWindowProcess(&s1View{}, Config{}, ProcessOptions{
		Theme:    material.NewTheme(),
		next:     s1Events(1),
		Observer: func(res ExitResult) { mixErr = res.Err },
		exit:     func(c int) { mixExits++; mixCode = c },
	})
	if mixExits != 1 || mixCode != 1 || !errors.Is(mixErr, ErrGioMainAlreadyOwned) || called.Load() != 3 {
		t.Fatalf("混用语义异常: exits=%d code=%d err=%v mainCalls=%d",
			mixExits, mixCode, mixErr, called.Load())
	}
	_, _, blocked := GioMainState()
	if blocked != 2 {
		t.Fatalf("两次被阻止的混用应可计数，实得 %d", blocked)
	}
}
