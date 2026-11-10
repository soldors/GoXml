// HY4 / R28-C LIFECYCLE-COMBINATION 独立复核：把 lifecycle.Owner 的最终化
// 接到**生产 app.loopEvents 的 app.DestroyEvent 分支**上。
//
// 边界必须分开记录，不得混写：
//   - 生产接线是真实的：case app.DestroyEvent → notifyViewDestroyed(a.view)
//     → WithOnDestroy 回调 → Router.LifecycleFinalize → Owner.Finalize；
//   - 事件源是受控的：本文件用内存 channel 合成 gioapp.DestroyEvent，直接喂给
//     loopEvents 的 next 参数，不经过 a.w.Event / Win32 driver / 原生窗口；
//   - 来自原生窗口的**真实 DestroyEvent**、窗口可见性、原生退出、跨平台与
//     人工视觉验收均**未验证**（见 known 边界登记）。
//
// 本文件是 package app 的内部测试，可直接调用私有 loopEvents / frame；
// app 包已有测试（destroy_view_test.go）同样 import router，无导入环。
package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/router"
)

// hy4Events 返回完全受控的事件序列。channel 耗尽后回退为 DestroyEvent，
// 保证任何误用都会在有界步数内结束循环，而不是无限阻塞。
func hy4Events(evs ...event.Event) func() event.Event {
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

type hy4Page struct {
	name     string
	mounts   int
	unmounts int
	layouts  int
	steps    *[]string
}

func (p *hy4Page) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.layouts++
	*p.steps = append(*p.steps, "layout:"+p.name)
	return layout.Dimensions{}
}

func (p *hy4Page) wrap() *router.LifecyclePage {
	return &router.LifecyclePage{
		View: p,
		Mount: func(context.Context) {
			p.mounts++
			*p.steps = append(*p.steps, "mount:"+p.name)
		},
		Unmount: func() {
			p.unmounts++
			*p.steps = append(*p.steps, "unmount:"+p.name)
		},
	}
}

func hy4Join(steps []string) string { return strings.Join(steps, ",") }

// hy4Setup 构造一个 opt-in Router + 已注册并已 Navigate 的页面 + 真实
// app.WithOnDestroy 包装（回调 = LifecycleFinalize）。不驱动任何帧。
func hy4Setup(t *testing.T, steps *[]string, name string) (View, *router.Router, *hy4Page, *int) {
	t.Helper()
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	pg := &hy4Page{name: name, steps: steps}
	path := "/" + name
	if err := r.Register(path, func(ctx context.Context) (router.View, error) {
		*steps = append(*steps, "construct:"+name)
		return pg.wrap(), nil
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
	return wrapped, r, pg, &callbacks
}

// TestHY4DestroyEventWiringFinalizesOwnerExactlyOnce 是 M1 的健康守卫：
// 删除 loopEvents 里的 notifyViewDestroyed(a.view) 后本用例必须 rc=1。
func TestHY4DestroyEventWiringFinalizesOwnerExactlyOnce(t *testing.T) {
	var steps []string
	wrapped, r, pg, callbacks := hy4Setup(t, &steps, "a")
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}

	// 只 Navigate、未驱动任何帧：不得构造、不得挂载。
	if len(steps) != 0 {
		t.Fatalf("Navigate 阶段不应构造或挂载: %v", steps)
	}
	if s := owner.Snapshot(); s.MountCalls != 0 || s.UnmountCalls != 0 {
		t.Fatalf("Navigate 阶段 owner 应为空: %+v", s)
	}

	// 受控事件源：一帧（成功发布 + 首次业务 Layout 前 Mount）+ 一个 DestroyEvent。
	a := &App{view: wrapped, th: material.NewTheme()}
	a.loopEvents(hy4Events(synthFrameEvent(&frameRecorder{}), gioapp.DestroyEvent{}))

	if got, want := hy4Join(steps), "construct:a,mount:a,layout:a,unmount:a"; got != want {
		t.Fatalf("生命周期顺序 = %q，期望 %q", got, want)
	}
	if *callbacks != 1 {
		t.Fatalf("DestroyEvent 销毁回调次数 = %d，期望 1", *callbacks)
	}
	if pg.mounts != 1 || pg.unmounts != 1 || pg.layouts != 1 {
		t.Fatalf("页面计数 mount=%d unmount=%d layout=%d，期望 1/1/1",
			pg.mounts, pg.unmounts, pg.layouts)
	}
	s := owner.Snapshot()
	if !s.CleanupDone || !s.Closed || s.MountCalls != 1 || s.UnmountCalls != 1 {
		t.Fatalf("DestroyEvent 后 owner 快照不符（清理必须恰好一次）: %+v", s)
	}

	// 同一 owner 再次收到销毁通知：回调可再次触发，但 owner 清理仍恰好一次。
	a.loopEvents(hy4Events(gioapp.DestroyEvent{}))
	if s2 := owner.Snapshot(); s2.UnmountCalls != 1 || pg.unmounts != 1 {
		t.Fatalf("重复销毁通知导致重复清理: %+v / page unmounts=%d", s2, pg.unmounts)
	}
	if *callbacks != 2 {
		t.Fatalf("第二次 DestroyEvent 应再次触发同一回调，实际 %d", *callbacks)
	}
}

// TestHY4TwoRouterAppInstancesAreFullyIsolated 验证两个 Router/App 实例完全隔离。
func TestHY4TwoRouterAppInstancesAreFullyIsolated(t *testing.T) {
	var steps1, steps2 []string
	v1, r1, p1, c1 := hy4Setup(t, &steps1, "one")
	v2, r2, p2, c2 := hy4Setup(t, &steps2, "two")
	o1, err := r1.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	o2, err := r2.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}

	// 两个实例各自驱动一帧（用 frame 直接排版，不触发销毁）。
	a1 := &App{view: v1, th: material.NewTheme()}
	a2 := &App{view: v2, th: material.NewTheme()}
	var ops1, ops2 op.Ops
	a1.frame(&ops1, synthFrameEvent(&frameRecorder{}))
	a2.frame(&ops2, synthFrameEvent(&frameRecorder{}))
	if o1.Snapshot().MountCalls != 1 || o2.Snapshot().MountCalls != 1 {
		t.Fatalf("两个实例应各自挂载一次: %+v / %+v", o1.Snapshot(), o2.Snapshot())
	}

	// 只销毁实例 1。
	a1.loopEvents(hy4Events(gioapp.DestroyEvent{}))
	if *c1 != 1 || *c2 != 0 {
		t.Fatalf("销毁回调串台: 实例1=%d 实例2=%d", *c1, *c2)
	}
	if s := o1.Snapshot(); !s.CleanupDone || s.UnmountCalls != 1 {
		t.Fatalf("实例1 未完成一次清理: %+v", s)
	}
	if s := o2.Snapshot(); s.CleanupDone || s.UnmountCalls != 0 || s.MountCalls != 1 {
		t.Fatalf("实例2 被误清理: %+v", s)
	}
	if p1.unmounts != 1 || p2.unmounts != 0 {
		t.Fatalf("页面清理串台: p1=%d p2=%d", p1.unmounts, p2.unmounts)
	}
	if r2.Context().Err() != nil {
		t.Fatal("实例2 的 Router Context 不应被取消")
	}

	// 实例 2 仍可继续渲染，生命周期未被污染。
	a2.frame(&ops2, synthFrameEvent(&frameRecorder{}))
	if p2.layouts != 2 || o2.Snapshot().MountCalls != 1 {
		t.Fatalf("实例2 后续帧异常: layouts=%d snap=%+v", p2.layouts, o2.Snapshot())
	}
}

// TestHY4ShutdownIsCancelOnlyUntilOwnerFinalizes 验证后台 Shutdown 只取消、
// 绝不执行 UI OnUnmount；最终化必须由 UI owner 的销毁回调完成。
func TestHY4ShutdownIsCancelOnlyUntilOwnerFinalizes(t *testing.T) {
	var steps []string
	wrapped, r, pg, callbacks := hy4Setup(t, &steps, "a")
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	a := &App{view: wrapped, th: material.NewTheme()}
	var ops op.Ops
	a.frame(&ops, synthFrameEvent(&frameRecorder{}))
	if s := owner.Snapshot(); s.MountCalls != 1 || s.CleanupDone {
		t.Fatalf("首帧后应已挂载且未最终化: %+v", s)
	}

	r.Shutdown()
	if s := owner.Snapshot(); !s.Closed || s.CleanupDone || s.UnmountCalls != 0 || pg.unmounts != 0 {
		t.Fatalf("Shutdown 不应执行 UI OnUnmount: %+v / page unmounts=%d", s, pg.unmounts)
	}

	a.loopEvents(hy4Events(gioapp.DestroyEvent{}))
	if *callbacks != 1 {
		t.Fatalf("销毁回调应恰好一次, got %d", *callbacks)
	}
	if s := owner.Snapshot(); !s.CleanupDone || s.UnmountCalls != 1 || pg.unmounts != 1 {
		t.Fatalf("销毁回调后应完成一次清理: %+v / page unmounts=%d", s, pg.unmounts)
	}
}

// TestHY4NoOptInDefaultDestroyPathUnchanged 验证默认未启用生命周期时，
// R27 已发布的 DestroyEvent → WithOnDestroy → Shutdown 行为逐字不变。
func TestHY4NoOptInDefaultDestroyPathUnchanged(t *testing.T) {
	var steps []string
	r := router.New() // 故意不调用 EnableLifecycle
	pg := &hy4Page{name: "plain", steps: &steps}
	if err := r.Register("/plain", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:plain")
		return pg, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/plain"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LifecycleOwner(); !errors.Is(err, router.ErrLifecycleNotEnabled) {
		t.Fatalf("未 opt-in 时 LifecycleOwner 应返回 ErrLifecycleNotEnabled, got %v", err)
	}

	calls := 0
	wrapped, err := WithOnDestroy(r.View(), func() {
		calls++
		r.Shutdown()
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{view: wrapped, th: material.NewTheme()}
	a.loopEvents(hy4Events(synthFrameEvent(&frameRecorder{}), gioapp.DestroyEvent{}))

	if calls != 1 || pg.layouts != 1 {
		t.Fatalf("默认路径回调/排版数异常: calls=%d layouts=%d", calls, pg.layouts)
	}
	if !errors.Is(r.Context().Err(), context.Canceled) {
		t.Fatalf("默认路径应取消 Router Context, got %v", r.Context().Err())
	}
	if pg.mounts != 0 || pg.unmounts != 0 {
		t.Fatalf("默认路径不得产生生命周期回调: mount=%d unmount=%d", pg.mounts, pg.unmounts)
	}
	if got, want := hy4Join(steps), "construct:plain,layout:plain"; got != want {
		t.Fatalf("默认路径步骤 = %q，期望 %q", got, want)
	}
}
