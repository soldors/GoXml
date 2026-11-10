package app

// R28-C combination 后续收口：App 销毁回调 → owner finalize 的独立合同探针。
//
// 与 HY4 的区别：本文件同时覆盖**两条生产事件循环** loopEvents 与
// runWindowEvents，并把「错误路径在真实 App 帧里不误跑 UI OnUnmount」、
// 「首个 DestroyEvent 结束循环、不双触发」做成独立断言。
// 事件源全部为内存受控 channel 合成的 gioapp.DestroyEvent，不经过
// a.w.Event / Win32 driver / 原生窗口；来自原生窗口的真实 DestroyEvent
// 仍未验证（见 proposed ADR 的原生验收最小步骤）。
//
// 本文件不修改 App 生产代码。

import (
	"context"
	"errors"
	"image"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/router"
)

func closeoutFrameEvent() gioapp.FrameEvent {
	return gioapp.FrameEvent{
		Now:    time.Now(),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Size:   image.Pt(200, 100),
		Frame:  func(*op.Ops) {},
	}
}

// closeoutEventSource 内存受控事件源；耗尽后回退 DestroyEvent 防止误用挂死。
func closeoutEventSource(evs ...event.Event) func() event.Event {
	ch := make(chan event.Event, len(evs)+2)
	for _, e := range evs {
		ch <- e
	}
	return func() event.Event {
		select {
		case e := <-ch:
			return e
		default:
			return gioapp.DestroyEvent{}
		}
	}
}

// closeoutWindow 是纯内存 windowEvents，绝不创建平台窗口。
type closeoutWindow struct {
	events      chan event.Event
	actions     chan system.Action
	invalidates atomic.Int32
}

func (w *closeoutWindow) Event() event.Event      { return <-w.events }
func (w *closeoutWindow) Perform(a system.Action) { w.actions <- a }
func (w *closeoutWindow) Invalidate()             { w.invalidates.Add(1) }

type closeoutAppPage struct {
	name            string
	mounts          int
	unmounts        int
	layouts         int
	unmountAtLayout []int
	mountCtx        context.Context
}

func (p *closeoutAppPage) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.layouts++
	p.unmountAtLayout = append(p.unmountAtLayout, p.unmounts)
	return layout.Dimensions{}
}

// closeoutBuild 构造 opt-in Router + 真实 WithOnDestroy 包装（回调 =
// LifecycleFinalize），帧尚未驱动。
func closeoutBuild(t *testing.T, name string) (*router.Router, *closeoutAppPage, View, *int) {
	t.Helper()
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	pg := &closeoutAppPage{name: name}
	path := "/" + name
	if err := r.Register(path, func(ctx context.Context) (router.View, error) {
		return &router.LifecyclePage{
			View: pg,
			Mount: func(c context.Context) {
				pg.mounts++
				pg.mountCtx = c
			},
			Unmount: func() { pg.unmounts++ },
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate(path); err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	wrapped, err := WithOnDestroy(r.View(), func() {
		callbacks++
		r.LifecycleFinalize()
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, pg, wrapped, &callbacks
}

// TestCloseoutDestroyEventFinalizesOwnerExactlyOnceAppLoop 验证生产 loopEvents
// 的 DestroyEvent 分支恰好一次调用 owner finalize，且重复销毁幂等。
func TestCloseoutDestroyEventFinalizesOwnerExactlyOnceAppLoop(t *testing.T) {
	r, pg, wrapped, callbacks := closeoutBuild(t, "a")
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	a := &App{view: wrapped, th: material.NewTheme()}
	a.loopEvents(closeoutEventSource(closeoutFrameEvent(), gioapp.DestroyEvent{}))

	if *callbacks != 1 {
		t.Fatalf("一次 DestroyEvent 应只触发一次销毁回调, got %d", *callbacks)
	}
	if pg.mounts != 1 || pg.unmounts != 1 || pg.layouts != 1 {
		t.Fatalf("页面计数 mount=%d unmount=%d layout=%d，期望 1/1/1",
			pg.mounts, pg.unmounts, pg.layouts)
	}
	if s := owner.Snapshot(); !s.CleanupDone || !s.Closed || s.MountCalls != 1 || s.UnmountCalls != 1 {
		t.Fatalf("销毁后 owner 快照（清理必须恰好一次）: %+v", s)
	}
	if pg.mountCtx == nil || !errors.Is(pg.mountCtx.Err(), context.Canceled) {
		t.Fatalf("销毁必须取消页面 Context, got %v", pg.mountCtx)
	}

	// 同一 owner 再次收到销毁通知：回调可再次触发，清理仍恰好一次。
	a.loopEvents(closeoutEventSource(gioapp.DestroyEvent{}))
	if *callbacks != 2 {
		t.Fatalf("第二次 DestroyEvent 应再次触发同一回调, got %d", *callbacks)
	}
	if pg.unmounts != 1 || owner.Snapshot().UnmountCalls != 1 {
		t.Fatalf("重复销毁通知不得重复清理: unmounts=%d snap=%+v", pg.unmounts, owner.Snapshot())
	}
}

// TestCloseoutDestroyEventFinalizesOwnerExactlyOnceIntegrationLoop 验证另一条
// 生产事件循环 runWindowEvents 的 DestroyEvent 分支同样恰好一次。
func TestCloseoutDestroyEventFinalizesOwnerExactlyOnceIntegrationLoop(t *testing.T) {
	r, pg, wrapped, callbacks := closeoutBuild(t, "a")
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	w := &closeoutWindow{
		events:  make(chan event.Event, 4),
		actions: make(chan system.Action, 4),
	}
	w.events <- closeoutFrameEvent()
	w.events <- gioapp.DestroyEvent{}
	stop := make(chan struct{})
	defer close(stop)
	if err := runWindowEvents(w, wrapped, material.NewTheme(), stop, nil); err != nil {
		t.Fatalf("正常关窗应返回 nil: %v", err)
	}
	if *callbacks != 1 {
		t.Fatalf("integration loop 应恰好一次销毁回调, got %d", *callbacks)
	}
	if pg.mounts != 1 || pg.unmounts != 1 || pg.layouts != 1 {
		t.Fatalf("页面计数 mount=%d unmount=%d layout=%d", pg.mounts, pg.unmounts, pg.layouts)
	}
	if !owner.CleanupDone() || owner.Snapshot().UnmountCalls != 1 {
		t.Fatalf("integration loop 后 owner 未恰好一次清理: %+v", owner.Snapshot())
	}
	if len(w.events) != 0 {
		t.Fatalf("应只消费一帧 + 一个 DestroyEvent, 剩余 %d", len(w.events))
	}
	if got := w.invalidates.Load(); got != 0 {
		t.Fatalf("正常 DestroyEvent 不应发出额外 Invalidate/ActionClose: %d", got)
	}
}

// TestCloseoutErrorPathThroughAppLoopDoesNotRunUIUnmount 在真实 App 连续帧里
// 让工厂失败，确认错误路径不误跑 UI OnUnmount；只有后续 DestroyEvent 才清理。
func TestCloseoutErrorPathThroughAppLoopDoesNotRunUIUnmount(t *testing.T) {
	pg := &closeoutAppPage{name: "ok"}
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/ok", func(ctx context.Context) (router.View, error) {
		return &router.LifecyclePage{
			View: pg,
			Mount: func(c context.Context) {
				pg.mounts++
				pg.mountCtx = c
				if err := r.Navigate("/bad"); err != nil { // 回入导航，下一帧失败
					t.Errorf("OnMount 回入导航失败: %v", err)
				}
			},
			Unmount: func() { pg.unmounts++ },
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/bad", func(ctx context.Context) (router.View, error) {
		return nil, errors.New("factory failed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/ok"); err != nil {
		t.Fatal(err)
	}
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	wrapped, err := WithOnDestroy(r.View(), func() {
		callbacks++
		r.LifecycleFinalize()
	})
	if err != nil {
		t.Fatal(err)
	}

	a := &App{view: wrapped, th: material.NewTheme()}
	// 帧1 发布 /ok 并回入 /bad；帧2 工厂失败、旧页继续渲染；帧3 销毁。
	a.loopEvents(closeoutEventSource(
		closeoutFrameEvent(), closeoutFrameEvent(), gioapp.DestroyEvent{}))

	if len(pg.unmountAtLayout) != 2 {
		t.Fatalf("应有两次旧页业务 Layout: %v", pg.unmountAtLayout)
	}
	if pg.unmountAtLayout[0] != 0 || pg.unmountAtLayout[1] != 0 {
		t.Fatalf("错误路径帧不得误跑 UI OnUnmount: %v", pg.unmountAtLayout)
	}
	if r.Current() != "/ok" || r.NavError() == nil {
		t.Fatalf("失败导航应保留旧页: current=%q err=%v", r.Current(), r.NavError())
	}
	if pg.mounts != 1 || pg.unmounts != 1 || callbacks != 1 {
		t.Fatalf("只有 DestroyEvent 允许一次清理: mount=%d unmount=%d callbacks=%d",
			pg.mounts, pg.unmounts, callbacks)
	}
	if s := owner.Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 1 || !s.CleanupDone {
		t.Fatalf("失败页不得被挂载，清理只能来自销毁回调: %+v", s)
	}
}

// TestCloseoutFirstDestroyEventEndsLoopWithoutDoubleFire 验证事件循环在首个
// DestroyEvent 立即结束，不消费后续事件、不双触发销毁回调。
func TestCloseoutFirstDestroyEventEndsLoopWithoutDoubleFire(t *testing.T) {
	_, pg, wrapped, callbacks := closeoutBuild(t, "a")
	ch := make(chan event.Event, 4)
	ch <- closeoutFrameEvent()
	ch <- gioapp.DestroyEvent{}
	ch <- gioapp.DestroyEvent{}
	a := &App{view: wrapped, th: material.NewTheme()}
	a.loopEvents(func() event.Event { return <-ch })
	if *callbacks != 1 {
		t.Fatalf("首个 DestroyEvent 后不得双触发: %d", *callbacks)
	}
	if pg.unmounts != 1 || pg.layouts != 1 {
		t.Fatalf("只应一次 Layout / 一次清理: layout=%d unmount=%d", pg.layouts, pg.unmounts)
	}
	if len(ch) != 1 {
		t.Fatalf("循环应在首个 DestroyEvent 结束，留下一个未消费事件: %d", len(ch))
	}
}

// TestCloseoutUnwrappedViewHasNoLifecycleFinalize 验证默认未接线路径：
// 不 opt-in、不 WithOnDestroy 时 DestroyEvent 只是结束循环，不产生任何
// 生命周期回调，也不取消 Router Context。
func TestCloseoutUnwrappedViewHasNoLifecycleFinalize(t *testing.T) {
	pg := &closeoutAppPage{name: "plain"}
	r := router.New() // 故意不 EnableLifecycle
	if err := r.Register("/plain", func(ctx context.Context) (router.View, error) {
		return pg, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/plain"); err != nil {
		t.Fatal(err)
	}
	a := &App{view: r.View(), th: material.NewTheme()} // 故意不 WithOnDestroy
	a.loopEvents(closeoutEventSource(closeoutFrameEvent(), gioapp.DestroyEvent{}))
	if pg.layouts != 1 || pg.mounts != 0 || pg.unmounts != 0 {
		t.Fatalf("默认路径不得产生生命周期回调: layout=%d mount=%d unmount=%d",
			pg.layouts, pg.mounts, pg.unmounts)
	}
	if r.Context().Err() != nil {
		t.Fatalf("默认未接线路径不得取消 Router Context, got %v", r.Context().Err())
	}
}
