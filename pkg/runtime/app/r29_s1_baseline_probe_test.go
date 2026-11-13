//go:build !nativewindow

// R29-C S1：受控两窗探针（默认无窗口集合）。
//
// 两个证明，互不冒充：
//  1. 基线缺口：既有 RunSingleWindowProcess 对每个窗口实例都做**进程级**
//     收口（exit），因此两个窗口共用它时，先关的那个会终止整个进程，
//     另一个窗口无法保持服务。用 opts.next / opts.exit 与注入 mainFunc
//     在受控形态下证明，不启动真实窗口、不 os.Exit。
//  2. 健康对照：两个真实、独立的 router.Router + lifecycle.Owner 可以在
//     同一个进程里各自完成挂载/卸载；A 的 Finalize 不影响 B 的 Context、
//     页面与布局能力；后台 Shutdown 只取消不卸载，卸载仍由各自 UI owner
//     在关闭路径完成。这是 R29 多窗口宿主候选的可行性基线。
package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/lifecycle"
	"github.com/goxml/goxml/pkg/runtime/router"
)

type s1View struct{ layouts atomic.Int32 }

func (v *s1View) Layout(layout.Context, *material.Theme) layout.Dimensions {
	v.layouts.Add(1)
	return layout.Dimensions{}
}

// s1Events 是纯内存事件序列：n 帧后一个正常 DestroyEvent。
func s1Events(n int) func() event.Event {
	ch := make(chan event.Event, n+1)
	for i := 0; i < n; i++ {
		ch <- synthFrameEvent(&frameRecorder{})
	}
	ch <- gioapp.DestroyEvent{}
	return func() event.Event { return <-ch }
}

// TestS1SingleWindowProcessOwnsWholeProcess 证明旧单窗进程入口的进程级
// 所有权：每个实例都以一次 exit 收口，故不能作为多窗口宿主。
func TestS1SingleWindowProcessOwnsWholeProcess(t *testing.T) {
	orig := mainFunc
	defer func() { mainFunc = orig }()
	mainFunc = func() {}

	for i := 0; i < 2; i++ {
		exits, code := 0, -1
		var observed bool
		opts := ProcessOptions{
			Theme: material.NewTheme(),
			next:  s1Events(2),
			exit: func(c int) {
				exits++
				code = c
			},
			Observer: func(ExitResult) { observed = true },
		}
		RunSingleWindowProcess(&s1View{}, Config{Title: "s1-probe"}, opts)
		if exits != 1 || code != 0 || !observed {
			t.Fatalf("窗口 %d：单窗进程入口应恰好一次进程级收口且先通知 observer: exits=%d code=%d observer=%v",
				i, exits, code, observed)
		}
	}
	// 结论（不靠推断）：两个窗口若各用该入口，先关者即 exit(code)，
	// 另一窗口不可能保持服务——这是基线的结构性缺口。
}

type s1RouterSet struct {
	r          *router.Router
	owner      *lifecycle.Owner
	page       *s1View
	mounts     atomic.Int32
	unmounts   atomic.Int32
	pageCtx    atomic.Value // context.Context：由 Mount 回调交出（ADR-0018 页面 Context）
	cancelSeen atomic.Bool
}

func newS1RouterSet(t *testing.T) *s1RouterSet {
	t.Helper()
	s := &s1RouterSet{r: router.New(), page: &s1View{}}
	if err := s.r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	owner, err := s.r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	s.owner = owner
	if err := s.r.Register("/home", func(ctx context.Context) (router.View, error) {
		return &router.LifecyclePage{
			View: s.page,
			Mount: func(ctx context.Context) {
				// 与 R28 C2 fixture 同源：页面 Context 由 Mount 回调交出，
				// 卸载时读它的 Err()，不能用 factory 的 ctx 代替。
				s.pageCtx.Store(ctx)
				s.mounts.Add(1)
			},
			Unmount: func() {
				if pc, ok := s.pageCtx.Load().(context.Context); ok && errors.Is(pc.Err(), context.Canceled) {
					s.cancelSeen.Store(true)
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
	return s
}

func (s *s1RouterSet) draw(t *testing.T) {
	t.Helper()
	var ops op.Ops
	gtx := gioapp.NewContext(&ops, synthFrameEvent(&frameRecorder{}))
	s.r.View().Layout(gtx, material.NewTheme())
}

// TestS1IndependentRouterOwnersSurviveSiblingFinalize 是健康对照：两个
// 独立 Router/Owner 互不串写、互不串停。
func TestS1IndependentRouterOwnersSurviveSiblingFinalize(t *testing.T) {
	a := newS1RouterSet(t)
	b := newS1RouterSet(t)
	if a.r == b.r || a.owner == b.owner {
		t.Fatal("两个窗口必须各有独立 Router 与 Owner")
	}
	a.draw(t)
	b.draw(t)

	// A 正常关闭：Finalize 恰一次卸载，且 Finalize 不隐式 Shutdown。
	a.r.LifecycleFinalize()
	if got := a.unmounts.Load(); got != 1 {
		t.Fatalf("A 关闭应卸载恰一次，实得 %d", got)
	}
	if !a.owner.CleanupDone() || !a.cancelSeen.Load() {
		t.Fatal("A 的页面 Context 应先取消、owner 应完成清理")
	}
	if a.r.Context().Err() != nil {
		t.Fatal("A 的 Finalize 不应隐式取消 Router 根")
	}

	// 关 A 后 B 仍健康：根活跃、未卸载、仍能布局。
	if b.r.Context().Err() != nil || b.unmounts.Load() != 0 || b.owner.CleanupDone() {
		t.Fatal("关 A 不应影响 B 的 Router/Owner 状态")
	}
	before := b.page.layouts.Load()
	b.draw(t)
	if b.page.layouts.Load() <= before {
		t.Fatal("关 A 后 B 应仍能布局/响应")
	}

	// B 后台取消：只取消，不卸载（ADR-0018 CancelOnly 语义）。
	b.r.Shutdown()
	if !errors.Is(b.r.Context().Err(), context.Canceled) {
		t.Fatal("B 后台 Shutdown 应取消 Router 根")
	}
	if b.unmounts.Load() != 0 || b.owner.CleanupDone() {
		t.Fatal("后台 Shutdown 只取消，不得执行 UI 卸载")
	}

	// B 关闭：卸载由 B 自己的 UI owner 完成，恰一次。
	b.r.LifecycleFinalize()
	if got := b.unmounts.Load(); got != 1 {
		t.Fatalf("B 关闭后应卸载恰一次，实得 %d", got)
	}
	if !b.owner.CleanupDone() {
		t.Fatal("B 的 owner 应完成清理")
	}
	if a.unmounts.Load() != 1 {
		t.Fatal("B 的关闭不得重复清理 A（串停/重复清理反例）")
	}
}
