package router_test

// R28-C / S2：真实 Router 成功提交接缝 + 真实 app.WithOnDestroy 受控组合。
//
// 本文件是**外部测试包**（沿用 R18 integration_test.go 的约定），可直接
// import pkg/runtime/app 而不产生导入环。所有帧由真实 Router.View().Layout
// 或真实 app.WithOnDestroy 包装后的 View.Layout 驱动，使用合成
// layout.Context/Ops，不进入 Window.Event / Win32 后端。
//
// 作者阶段无原生窗口授权：app 的私有 notifyViewDestroyed 不可从本包调用，
// 因此销毁通知用**注册给 WithOnDestroy 的同一个回调**在 owner 调用点受控
// 触发；app 生产代码零改动。真实 DestroyEvent 组合由 R27 已发布的 app 包
// 测试 TestDestroyEventShutsDownWrappedRouter 覆盖（S3 门禁复跑），
// 协调者授权后需再做真实事件组合复验。

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/lifecycle"
	"github.com/goxml/goxml/pkg/runtime/router"
)

type r28View interface {
	Layout(layout.Context, *material.Theme) layout.Dimensions
}

type r28Page struct {
	name       string
	layouts    atomic.Int64
	mu         sync.Mutex
	mount      int
	unmount    int
	mountCtx   context.Context
	unmountErr error
	onMount    func(context.Context)
	onUnmount  func()
	steps      *r28Steps
}

func (p *r28Page) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.layouts.Add(1)
	return layout.Dimensions{}
}

func (p *r28Page) OnMount(ctx context.Context) {
	p.mu.Lock()
	p.mount++
	p.mountCtx = ctx
	fn := p.onMount
	p.mu.Unlock()
	if p.steps != nil {
		p.steps.add("mount:" + p.name)
	}
	if fn != nil {
		fn(ctx)
	}
}

func (p *r28Page) OnUnmount() {
	p.mu.Lock()
	p.unmount++
	p.unmountErr = p.mountCtx.Err()
	fn := p.onUnmount
	p.mu.Unlock()
	if p.steps != nil {
		p.steps.add("unmount:" + p.name)
	}
	if fn != nil {
		fn()
	}
}

func (p *r28Page) counts() (int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mount, p.unmount, p.unmountErr
}

type r28Steps struct {
	mu sync.Mutex
	s  []string
}

func (s *r28Steps) add(v string) {
	s.mu.Lock()
	s.s = append(s.s, v)
	s.mu.Unlock()
}

func (s *r28Steps) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.s...)
}

func r28Wrap(p *r28Page) *router.LifecyclePage {
	return &router.LifecyclePage{
		View:    p,
		Mount:   p.OnMount,
		Unmount: p.OnUnmount,
	}
}

func r28Frame(v r28View) {
	v.Layout(layout.Context{Ops: new(op.Ops)}, material.NewTheme())
}

func r28Owner(t *testing.T, r *router.Router) *lifecycle.Owner {
	t.Helper()
	o, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func r28Enable(t *testing.T, r *router.Router) *lifecycle.Owner {
	t.Helper()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	return r28Owner(t, r)
}

// TestR28OptInGuards 显式 opt-in，导航后/重复开启均拒绝；未开启时无 owner。
func TestR28OptInGuards(t *testing.T) {
	r := router.New()
	if _, err := r.LifecycleOwner(); !errors.Is(err, router.ErrLifecycleNotEnabled) {
		t.Fatalf("未开启时应返回 ErrLifecycleNotEnabled: %v", err)
	}
	page := &r28Page{name: "plain"}
	if err := r.Register("/p", func(context.Context) (router.View, error) { return page, nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	if err := r.EnableLifecycle(); !errors.Is(err, router.ErrLifecycleAfterNavigation) {
		t.Fatalf("导航后才开启应被拒绝: %v", err)
	}
	r2 := router.New()
	r28Enable(t, r2)
	if err := r2.EnableLifecycle(); !errors.Is(err, router.ErrLifecycleAlreadyEnabled) {
		t.Fatalf("重复开启应被拒绝: %v", err)
	}
}

// TestR28NoOptInPreservesPublishedBehavior 未 opt-in 的普通 Router 行为不变。
func TestR28NoOptInPreservesPublishedBehavior(t *testing.T) {
	r := router.New()
	page := &r28Page{name: "plain"}
	if err := r.Register("/p", func(context.Context) (router.View, error) { return page, nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	if r.Current() != "/p" || page.layouts.Load() != 1 {
		t.Fatalf("普通 Router 行为被改变: current=%q layouts=%d", r.Current(), page.layouts.Load())
	}
	m, u, _ := page.counts()
	if m != 0 || u != 0 {
		t.Fatal("未 opt-in 却触发了生命周期回调")
	}
	r.LifecyclePost(func() { t.Error("未 opt-in 不应执行 lifecycle 更新") })
	r.Shutdown()
	r28Frame(r.View())
	if r.Current() != "/p" || r.DroppedCount() != 0 {
		t.Fatalf("既有 Shutdown/drop 计数合同被改变: %q %d", r.Current(), r.DroppedCount())
	}
}

// TestR28SuccessCommitsMountBeforeFirstBusinessLayout 成功发布后、首次业务
// Layout 之前才挂载；构造阶段绝不 Mount。
func TestR28SuccessCommitsMountBeforeFirstBusinessLayout(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	steps := &r28Steps{}
	page := &r28Page{name: "a", steps: steps}
	constructed := 0
	if err := r.Register("/a", func(context.Context) (router.View, error) {
		constructed++
		return r28Wrap(page), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	// 导航只入队；不驱动帧则工厂都没跑，更不该 Mount。
	if constructed != 0 || o.Snapshot().MountCalls != 0 {
		t.Fatal("导航请求本身触发了构造或挂载")
	}
	r28Frame(r.View())
	if constructed != 1 || o.Snapshot().MountCalls != 1 || page.layouts.Load() != 1 {
		t.Fatalf("成功发布帧未恰好挂载一次: constructed=%d snap=%+v layouts=%d",
			constructed, o.Snapshot(), page.layouts.Load())
	}
	want := []string{"mount:a"}
	got := steps.snapshot()
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("挂载顺序异常: %v", got)
	}
	// 稳定帧不再 Mount（且 Layout 仍执行）。
	r28Frame(r.View())
	if o.Snapshot().MountCalls != 1 || page.layouts.Load() != 2 {
		t.Fatal("稳定帧重复挂载")
	}
}

// TestR28FailedFactoryDoesNotMountAndKeepsOldPage 工厂失败不挂载，
// 旧页不因新工厂失败被取消/卸载。
func TestR28FailedFactoryDoesNotMountAndKeepsOldPage(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	old := &r28Page{name: "old"}
	failed := &r28Page{name: "failed"}
	if err := r.Register("/old", func(context.Context) (router.View, error) { return r28Wrap(old), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/bad", func(context.Context) (router.View, error) {
		return r28Wrap(failed), errors.New("factory failed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/old"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	if err := r.Navigate("/bad"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	om, ou, _ := old.counts()
	fm, fu, _ := failed.counts()
	if fm != 0 || fu != 0 {
		t.Fatalf("失败工厂页面被 Mount/Unmount: %d/%d", fm, fu)
	}
	if om != 1 || ou != 0 {
		t.Fatalf("旧页被错误卸载或重复挂载: %d/%d", om, ou)
	}
	if r.Current() != "/old" || r.NavError() == nil {
		t.Fatalf("失败导航未保旧页: current=%q err=%v", r.Current(), r.NavError())
	}
	if o.Snapshot().MountCalls != 1 {
		t.Fatalf("失败导航计入了挂载: %+v", o.Snapshot())
	}
}

// TestR28MergedNavigationDoesNotConstructDiscardedPage 同帧合并导航只构造最后页。
func TestR28MergedNavigationDoesNotConstructDiscardedPage(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	middle, last := &r28Page{name: "middle"}, &r28Page{name: "last"}
	middleBuilt := 0
	if err := r.Register("/middle", func(context.Context) (router.View, error) {
		middleBuilt++
		return r28Wrap(middle), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/last", func(context.Context) (router.View, error) { return r28Wrap(last), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/middle"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/last"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	mm, mu, _ := middle.counts()
	lm, lu, _ := last.counts()
	if middleBuilt != 0 || mm != 0 || mu != 0 {
		t.Fatalf("被合并的页面被构造/挂载: built=%d %d/%d", middleBuilt, mm, mu)
	}
	if lm != 1 || lu != 0 || r.Current() != "/last" || o.Snapshot().MountCalls != 1 {
		t.Fatalf("最后页未恰好挂载一次: %d/%d current=%q snap=%+v", lm, lu, r.Current(), o.Snapshot())
	}
}

// TestR28SuccessfulSwitchCancelsOldThenMountsNew 成功切换：旧 ctx 先取消、
// OnUnmount 一次，再按声明顺序挂新页；构造期旧页仍活。
func TestR28SuccessfulSwitchCancelsOldThenMountsNew(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	steps := &r28Steps{}
	a, b := &r28Page{name: "a", steps: steps}, &r28Page{name: "b", steps: steps}
	oldCtxAliveAtConstruction := false
	if err := r.Register("/a", func(context.Context) (router.View, error) { return r28Wrap(a), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", func(ctx context.Context) (router.View, error) {
		oldCtxAliveAtConstruction = a.mountCtx.Err() == nil
		return r28Wrap(b), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	if !oldCtxAliveAtConstruction {
		t.Fatal("构造新页时旧页 Context 已被取消（违反既有构造时旧页仍活合同）")
	}
	am, au, aerr := a.counts()
	bm, bu, _ := b.counts()
	if am != 1 || au != 1 || !errors.Is(aerr, context.Canceled) {
		t.Fatalf("旧页未先取消再清理一次: mount=%d unmount=%d err=%v", am, au, aerr)
	}
	if bm != 1 || bu != 0 {
		t.Fatalf("新页挂载异常: %d/%d", bm, bu)
	}
	want := []string{"mount:a", "unmount:a", "mount:b"}
	got := steps.snapshot()
	if len(got) != len(want) {
		t.Fatalf("切换顺序不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("切换顺序不符: got %v want %v", got, want)
		}
	}
	if o.Snapshot().MountCalls != 2 || o.Snapshot().UnmountCalls != 1 {
		t.Fatalf("owner 计数异常: %+v", o.Snapshot())
	}
}

// TestR28ShutdownWinsLockDropsInFlight 工厂在途时关闭赢锁：结果被丢弃、
// 不 Mount，且后台关闭不执行 UI OnUnmount；owner 最终化才清理一次。
func TestR28ShutdownWinsLockDropsInFlight(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	old, inflight := &r28Page{name: "old"}, &r28Page{name: "inflight"}
	if err := r.Register("/old", func(context.Context) (router.View, error) { return r28Wrap(old), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/new", func(context.Context) (router.View, error) {
		r.Shutdown()
		return r28Wrap(inflight), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/old"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	if err := r.Navigate("/new"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	im, iu, _ := inflight.counts()
	om, ou, _ := old.counts()
	if im != 0 || iu != 0 {
		t.Fatalf("被丢弃的在途页被 Mount/Unmount: %d/%d", im, iu)
	}
	if om != 1 || ou != 0 {
		t.Fatalf("后台 Shutdown 在 UI 线程执行了 OnUnmount: %d/%d", om, ou)
	}
	if r.Current() != "/old" || o.CleanupDone() {
		t.Fatal("关闭结果被发布或错误宣称清理完成")
	}
	// UI owner 销毁回调最终化：无需再 Layout/Dispatch 即完成一次清理。
	r.LifecycleFinalize()
	om, ou, oerr := old.counts()
	if ou != 1 || !errors.Is(oerr, context.Canceled) || !o.CleanupDone() {
		t.Fatalf("owner 最终化未完成一次清理: mount=%d unmount=%d err=%v done=%v",
			om, ou, oerr, o.CleanupDone())
	}
}

// TestR28BackgroundShutdownNeverRunsUIUnmount 真正的后台 goroutine 调用
// Shutdown：只取消/拒绝，UI OnUnmount 必须由 owner 后续最终化执行。
func TestR28BackgroundShutdownNeverRunsUIUnmount(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	page := &r28Page{name: "bg"}
	if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Shutdown()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("后台 Shutdown 未返回")
	}
	_, u, _ := page.counts()
	if u != 0 || o.CleanupDone() {
		t.Fatal("后台 Shutdown 执行了 UI OnUnmount 或宣称清理完成")
	}
	if !errors.Is(r.Context().Err(), context.Canceled) {
		t.Fatal("后台 Shutdown 未取消 Router Context")
	}
	r.LifecycleFinalize()
	r.LifecycleFinalize() // 幂等
	_, u, uerr := page.counts()
	if u != 1 || !errors.Is(uerr, context.Canceled) {
		t.Fatalf("owner 最终化应恰好清理一次: %d %v", u, uerr)
	}
}

// TestR28LateOwnerUpdateRejectedAtExecutionTime 投递时活跃，关闭后才
// 执行 -> 实际执行时复查并拒绝，业务无需手写检查。
func TestR28LateOwnerUpdateRejectedAtExecutionTime(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	page := &r28Page{name: "late"}
	if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	applied := 0
	r.LifecyclePost(func() { applied++ })
	if snap := o.Snapshot(); snap.Posted != 1 {
		t.Fatalf("更新未入队: %+v", snap)
	}
	r.LifecycleFinalize()
	r28Frame(r.View()) // drain -> runPosted，在实际执行时复查
	if applied != 0 {
		t.Fatal("取消后才执行的迟到更新被应用")
	}
	if snap := o.Snapshot(); snap.Rejected != 1 || snap.Applied != 0 {
		t.Fatalf("迟到拒绝计数异常: %+v", snap)
	}
}

// TestR28CurrentGenerationUpdateStillApplies 正常生命期内的 owner 更新照常执行。
func TestR28CurrentGenerationUpdateStillApplies(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	page := &r28Page{name: "ok"}
	if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	applied := 0
	r.LifecyclePost(func() { applied++ })
	r28Frame(r.View())
	if applied != 1 || o.Snapshot().Applied != 1 {
		t.Fatalf("当前代次更新未执行: applied=%d snap=%+v", applied, o.Snapshot())
	}
}

// TestR28RootDestroyWithRealWithOnDestroyCombination 真实 app.WithOnDestroy
// 包装 Router.View()：受控触发同一销毁回调，root 关闭无需下一帧或再 Dispatch。
func TestR28RootDestroyWithRealWithOnDestroyCombination(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	page := &r28Page{name: "root"}
	if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}

	// 真实 App 组合：WithOnDestroy 的回调就是 owner 最终化入口。
	var ownerDestroy func()
	wrapped, err := app.WithOnDestroy(r.View(), func() {
		if ownerDestroy != nil {
			ownerDestroy()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerDestroy = r.LifecycleFinalize

	// 真实 App 包装的 View.Layout 驱动真实 Router 帧（结构化接口兼容）。
	r28Frame(wrapped)
	m, u, _ := page.counts()
	if m != 1 || u != 0 || page.layouts.Load() != 1 {
		t.Fatalf("App 包装帧未正确驱动挂载: %d/%d layouts=%d", m, u, page.layouts.Load())
	}

	// 受控 owner 销毁通知（app 在真实 DestroyEvent 时调用同一个 fn）。
	// 不依赖再次 Layout 或额外 Dispatch。
	ownerDestroy()
	ownerDestroy() // 重复 Destroy/Finalize 幂等
	_, u, uerr := page.counts()
	if u != 1 || !errors.Is(uerr, context.Canceled) || !o.CleanupDone() {
		t.Fatalf("root 关闭未在当前 owner 完成一次清理: %d %v done=%v", u, uerr, o.CleanupDone())
	}
	// lifecycle Finalize 只负责 UI owner 清理，不改 Router 路由状态；
	// 真实窗口关闭时 app 的 WithOnDestroy 回调里还会另行调用 r.Shutdown()。
	if err := r.Navigate("/p"); err != nil {
		t.Fatalf("lifecycle Finalize 不应改变同路径导航语义: %v", err)
	}
	_ = wrapped
}

// TestR28OnMountMayRequestNavigate OnMount 中合法导航回入不死锁。
func TestR28OnMountMayRequestNavigate(t *testing.T) {
	r := router.New()
	r28Enable(t, r)
	a, b := &r28Page{name: "a"}, &r28Page{name: "b"}
	a.onMount = func(context.Context) {
		if err := r.Navigate("/b"); err != nil {
			t.Errorf("OnMount 内合法导航失败: %v", err)
		}
	}
	if err := r.Register("/a", func(context.Context) (router.View, error) { return r28Wrap(a), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", func(context.Context) (router.View, error) { return r28Wrap(b), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r28Frame(r.View())
		r28Frame(r.View())
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnMount 回入导致死锁")
	}
	bm, _, _ := b.counts()
	if r.Current() != "/b" || bm != 1 {
		t.Fatalf("回入导航未生效: current=%q bMount=%d", r.Current(), bm)
	}
}

// TestR28TwoRoutersIsolated 两个 Router 的生命周期互不干扰。
func TestR28TwoRoutersIsolated(t *testing.T) {
	makeRouter := func(name string) (*router.Router, *r28Page, *lifecycle.Owner) {
		rr := router.New()
		oo := r28Enable(t, rr)
		pp := &r28Page{name: name}
		if err := rr.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(pp), nil }); err != nil {
			t.Fatal(err)
		}
		if err := rr.Navigate("/p"); err != nil {
			t.Fatal(err)
		}
		r28Frame(rr.View())
		return rr, pp, oo
	}
	r1, p1, o1 := makeRouter("one")
	r2, p2, o2 := makeRouter("two")
	r1.LifecycleFinalize()
	_, u1, _ := p1.counts()
	_, u2, _ := p2.counts()
	if u1 != 1 || u2 != 0 || !o1.CleanupDone() || o2.CleanupDone() {
		t.Fatal("一个 Router 的最终化影响了另一个")
	}
	if r2.Current() != "/p" {
		t.Fatal("另一个 Router 的路由状态被改变")
	}
}

// TestR28StableFrameZeroAllocs 稳定帧（无导航/更新）Layout 零分配。
// 生命周期创建/切换的分配不计入每帧开销。
func TestR28StableFrameZeroAllocs(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "no-opt-in"
		if enabled {
			name = "opt-in"
		}
		t.Run(name, func(t *testing.T) {
			r := router.New()
			if enabled {
				if err := r.EnableLifecycle(); err != nil {
					t.Fatal(err)
				}
			}
			page := &r28Page{name: "stable"}
			if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
				t.Fatal(err)
			}
			if err := r.Navigate("/p"); err != nil {
				t.Fatal(err)
			}
			ops := new(op.Ops)
			gtx := layout.Context{Ops: ops}
			th := material.NewTheme()
			v := r.View()
			for i := 0; i < 3; i++ { // 预热：首帧含工厂/挂载
				gtx.Reset()
				v.Layout(gtx, th)
			}
			const runs = 100
			allocs := testing.AllocsPerRun(runs, func() {
				gtx.Reset()
				v.Layout(gtx, th)
			})
			if allocs != 0 {
				t.Fatalf("稳定帧 allocs/op = %v，要求 0", allocs)
			}
		})
	}
}

// TestR28StalePostRejectedAtExecutionTime 投递时活跃，但在 owner 实际执行
// 之前旧生命期已被清理：RunPosted 复查代次，旧结果被丢弃且不再写入。
func TestR28StalePostRejectedAtExecutionTime(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	a := &r28Page{name: "a"}
	if err := r.Register("/a", func(context.Context) (router.View, error) { return r28Wrap(a), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	applied := 0
	r.LifecyclePost(func() { applied += 100 }) // 入队时 a 仍活跃
	// UI owner 完成切换的清理阶段（先取消 + 一次 OnUnmount），但不新增页面。
	o.UnmountCurrent()
	r28Frame(r.View()) // drain -> RunPosted 在此刻复查
	if applied != 0 {
		t.Fatalf("旧生命期的迟到更新被应用: applied=%d", applied)
	}
	if snap := o.Snapshot(); snap.Rejected != 1 || snap.Applied != 0 {
		t.Fatalf("执行时复查未拒绝旧结果: %+v", snap)
	}
	am, au, aerr := a.counts()
	if am != 1 || au != 1 || !errors.Is(aerr, context.Canceled) {
		t.Fatalf("旧生命期清理顺序异常: %d/%d %v", am, au, aerr)
	}
}

// TestR28OnMountMayRequestShutdown OnMount 中合法关闭不死锁，
// 且关闭不在该回调里执行 UI 清理。
func TestR28OnMountMayRequestShutdown(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	page := &r28Page{name: "close"}
	page.onMount = func(context.Context) { r.Shutdown() }
	if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r28Frame(r.View())
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnMount 内关闭导致死锁")
	}
	m, u, _ := page.counts()
	if m != 1 || u != 0 || o.CleanupDone() {
		t.Fatalf("OnMount 内关闭不应同步执行 UI 清理: %d/%d done=%v", m, u, o.CleanupDone())
	}
	r.LifecycleFinalize()
	_, u, uerr := page.counts()
	if u != 1 || !errors.Is(uerr, context.Canceled) {
		t.Fatalf("owner 最终化清理异常: %d %v", u, uerr)
	}
}

// TestR28ParentPageContextCancellation Router 页面 Context 取消传播到
// lifecycle handle Context；取消后的投递被拒绝。
func TestR28ParentPageContextCancellation(t *testing.T) {
	r := router.New()
	o := r28Enable(t, r)
	page := &r28Page{name: "child"}
	var pageCtx context.Context
	if err := r.Register("/p", func(ctx context.Context) (router.View, error) {
		pageCtx = ctx
		return r28Wrap(page), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View())
	h := o.Current()
	if h == nil || h.Context() == nil {
		t.Fatal("生命周期 handle 未建立独立 Context")
	}
	applied := 0
	r.LifecyclePost(func() { applied++ })
	r.Shutdown() // 后台取消：pageCtx -> handle ctx 级联取消
	r28Frame(r.View())
	if !errors.Is(pageCtx.Err(), context.Canceled) || !errors.Is(h.Err(), context.Canceled) {
		t.Fatalf("父子 Context 取消未传播: page=%v handle=%v", pageCtx.Err(), h.Err())
	}
	if applied != 0 {
		t.Fatal("取消后的投递被应用")
	}
	r.LifecycleFinalize()
	_, u, _ := page.counts()
	if u != 1 {
		t.Fatalf("最终化清理应恰一次: %d", u)
	}
}

// TestR28RepeatedSwitchAndDestroyUnmountOnce 重复切换/销毁/卸载时每页只清理一次。
func TestR28RepeatedSwitchAndDestroyUnmountOnce(t *testing.T) {
	r := router.New()
	r28Enable(t, r)
	pages := map[string]*r28Page{}
	for _, name := range []string{"a", "b", "c"} {
		p := &r28Page{name: name}
		pages[name] = p
		if err := r.Register("/"+name, func(context.Context) (router.View, error) { return r28Wrap(p), nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a", "b", "c", "a"} {
		if err := r.Navigate("/" + name); err != nil {
			t.Fatal(err)
		}
		r28Frame(r.View())
	}
	for i := 0; i < 3; i++ {
		r.LifecycleFinalize()
	}
	for name, p := range pages {
		m, u, _ := p.counts()
		if u > m {
			t.Fatalf("页面 %q 重复卸载: mount=%d unmount=%d", name, m, u)
		}
	}
}
