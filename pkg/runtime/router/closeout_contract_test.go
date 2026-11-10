package router_test

// R28-C combination 后续收口：对 EnableLifecycle / LifecyclePage / Router
// 生命周期接缝的独立合同探针（外部测试包，真实 Router.View().Layout 驱动帧）。
//
// 角度与作者/HY4 不同：本文件把「opt-in 默认值与守卫」「错误/合并/关闭路径
// 不误跑 UI 回调」「后台 Shutdown 只取消 + UI owner Finalize 一次清理」做成
// 可单独 -run 的公开 API 断言，并在 OnUnmount 里回读 Context 取消原因以
// 验证「先取消再清理」的线程/顺序约束。不修改生产代码。

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/router"
)

type closeoutPage struct {
	name       string
	mu         sync.Mutex
	mounts     int
	unmounts   int
	layouts    int
	mountCtx   context.Context
	unmountErr error
	steps      *[]string
}

func (p *closeoutPage) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.mu.Lock()
	p.layouts++
	p.mu.Unlock()
	*p.steps = append(*p.steps, "layout:"+p.name)
	return layout.Dimensions{}
}

func (p *closeoutPage) OnMount(ctx context.Context) {
	p.mu.Lock()
	p.mounts++
	p.mountCtx = ctx
	p.mu.Unlock()
	*p.steps = append(*p.steps, "mount:"+p.name)
}

func (p *closeoutPage) OnUnmount() {
	p.mu.Lock()
	p.unmounts++
	if p.mountCtx != nil {
		p.unmountErr = p.mountCtx.Err()
	}
	p.mu.Unlock()
	*p.steps = append(*p.steps, "unmount:"+p.name)
}

func (p *closeoutPage) counts() (int, int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mounts, p.unmounts, p.layouts, p.unmountErr
}

func (p *closeoutPage) wrap() *router.LifecyclePage {
	return &router.LifecyclePage{
		View:    p,
		Mount:   p.OnMount,
		Unmount: p.OnUnmount,
	}
}

func closeoutFrame(v router.View) {
	v.Layout(layout.Context{Ops: new(op.Ops)}, material.NewTheme())
}

func closeoutStepsEqual(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestCloseoutOptInIsNotDefault 验证生命周期 opt-in 默认关闭，且未开启时
// LifecyclePost / LifecycleFinalize 为 no-op，既有 Router 行为逐字不变。
func TestCloseoutOptInIsNotDefault(t *testing.T) {
	var steps []string
	r := router.New()
	if _, err := r.LifecycleOwner(); !errors.Is(err, router.ErrLifecycleNotEnabled) {
		t.Fatalf("默认未 opt-in 时 LifecycleOwner = %v", err)
	}
	p := &closeoutPage{name: "x", steps: &steps}
	if err := r.Register("/x", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:x")
		return p.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/x"); err != nil {
		t.Fatal(err)
	}
	closeoutFrame(r.View())

	if !closeoutStepsEqual(steps, "construct:x", "layout:x") {
		t.Fatalf("默认路径不得产生生命周期回调: %v", steps)
	}
	if m, u, l, _ := p.counts(); m != 0 || u != 0 || l != 1 {
		t.Fatalf("默认路径计数: mount=%d unmount=%d layout=%d", m, u, l)
	}
	posted := false
	r.LifecyclePost(func() { posted = true })
	r.LifecycleFinalize()
	if posted {
		t.Fatal("未 opt-in 时 LifecyclePost 不得产生执行")
	}
	if r.Context().Err() != nil {
		t.Fatal("未 opt-in 时 LifecycleFinalize 不得取消 Router Context")
	}
	if r.Current() != "/x" || r.NavError() != nil {
		t.Fatalf("默认导航状态被改变: current=%q err=%v", r.Current(), r.NavError())
	}
	r.Shutdown()
	if !errors.Is(r.Context().Err(), context.Canceled) {
		t.Fatalf("默认 Shutdown 仍须取消 Router Context, got %v", r.Context().Err())
	}
}

// TestCloseoutEnableLifecycleGuards 验证 opt-in 的三个失败边界。
func TestCloseoutEnableLifecycleGuards(t *testing.T) {
	r1 := router.New()
	if err := r1.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	if err := r1.EnableLifecycle(); !errors.Is(err, router.ErrLifecycleAlreadyEnabled) {
		t.Fatalf("重复 opt-in = %v", err)
	}

	r2 := router.New()
	if err := r2.Register("/x", func(ctx context.Context) (router.View, error) {
		return &closeoutPage{name: "x", steps: new([]string)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r2.Navigate("/x"); err != nil {
		t.Fatal(err)
	}
	if err := r2.EnableLifecycle(); !errors.Is(err, router.ErrLifecycleAfterNavigation) {
		t.Fatalf("导航后 opt-in = %v", err)
	}

	r3 := router.New()
	r3.Shutdown()
	if err := r3.EnableLifecycle(); !errors.Is(err, router.ErrLifecycleClosed) {
		t.Fatalf("关闭后 opt-in = %v", err)
	}
}

// TestCloseoutMountOnlyAfterSuccessfulCommit 验证成功发布后首次业务 Layout
// 之前才 Mount、旧页先取消再一次锁外 OnUnmount。
func TestCloseoutMountOnlyAfterSuccessfulCommit(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	a := &closeoutPage{name: "a", steps: &steps}
	b := &closeoutPage{name: "b", steps: &steps}
	if err := r.Register("/a", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:a")
		return a.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:b")
		return b.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	// 失败工厂是「未发布页不得挂载」的判别点：若把 Mount 提前到工厂构造
	// 阶段，失败页会进入生命周期、旧页会被误清理，本守卫必须翻红。
	if err := r.Register("/bad", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:bad")
		return nil, errors.New("factory failed")
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 || owner.Snapshot().MountCalls != 0 {
		t.Fatalf("Navigate 只入队，不得构造/挂载: %v / %+v", steps, owner.Snapshot())
	}
	closeoutFrame(r.View())
	if !closeoutStepsEqual(steps, "construct:a", "mount:a", "layout:a") {
		t.Fatalf("首次发布顺序: %v", steps)
	}

	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	closeoutFrame(r.View())
	if !closeoutStepsEqual(steps,
		"construct:a", "mount:a", "layout:a",
		"construct:b", "unmount:a", "mount:b", "layout:b") {
		t.Fatalf("成功切换顺序: %v", steps)
	}
	if _, _, _, unmountErr := a.counts(); !errors.Is(unmountErr, context.Canceled) {
		t.Fatalf("OnUnmount 必须观察到已取消的旧页 Context（先取消再清理）, got %v", unmountErr)
	}
	if s := owner.Snapshot(); s.MountCalls != 2 || s.UnmountCalls != 1 {
		t.Fatalf("切换后 owner 快照: %+v", s)
	}

	// 失败工厂：旧页保持、失败页不 Mount、不产生额外 UI OnUnmount。
	// 这一步是「构造期挂载」变异的判别点（首轮守卫缺少该步，已如实登记）。
	if err := r.Navigate("/bad"); err != nil {
		t.Fatal(err)
	}
	closeoutFrame(r.View())
	if !closeoutStepsEqual(steps,
		"construct:a", "mount:a", "layout:a",
		"construct:b", "unmount:a", "mount:b", "layout:b",
		"construct:bad", "layout:b") {
		t.Fatalf("失败保旧顺序（未发布页不得挂载/误清理）: %v", steps)
	}
	if r.Current() != "/b" || r.NavError() == nil {
		t.Fatalf("失败导航应保留旧页: current=%q err=%v", r.Current(), r.NavError())
	}
	if s := owner.Snapshot(); s.MountCalls != 2 || s.UnmountCalls != 1 {
		t.Fatalf("失败页不得被挂载、旧页不得重复清理: %+v", s)
	}
}

// TestCloseoutErrorAndMergedPathsNeverRunUICallback 验证失败工厂与合并导航
// 都不会误跑 UI OnUnmount，也不会挂载未发布页。
func TestCloseoutErrorAndMergedPathsNeverRunUICallback(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	ok := &closeoutPage{name: "ok", steps: &steps}
	final := &closeoutPage{name: "final", steps: &steps}
	if err := r.Register("/ok", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:ok")
		return ok.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/bad", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:bad")
		return nil, errors.New("factory failed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/merged", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:merged")
		return &closeoutPage{name: "merged", steps: &steps}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/final", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:final")
		return final.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}

	if err := r.Navigate("/ok"); err != nil {
		t.Fatal(err)
	}
	closeoutFrame(r.View())

	// 失败工厂：旧页保持，失败页不 Mount，不产生 UI OnUnmount。
	if err := r.Navigate("/bad"); err != nil {
		t.Fatal(err)
	}
	closeoutFrame(r.View())
	if r.Current() != "/ok" || r.NavError() == nil {
		t.Fatalf("失败导航应保留旧页并记录 NavError: current=%q err=%v", r.Current(), r.NavError())
	}
	if _, u, l, _ := ok.counts(); u != 0 || l != 2 {
		t.Fatalf("失败路径旧页应保持挂载且未清理: unmount=%d layout=%d", u, l)
	}
	if s := owner.Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 0 {
		t.Fatalf("错误路径不得改动挂载/清理计数: %+v", s)
	}

	// 合并导航：只构造最终目标，被合并页不 Mount。
	if err := r.Navigate("/merged"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/final"); err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		t.Fatal("步骤记录丢失")
	}
	closeoutFrame(r.View())
	for _, s := range steps {
		if s == "construct:merged" || s == "layout:merged" {
			t.Fatalf("被合并的在途页不得被构造/渲染: %v", steps)
		}
	}
	if s := owner.Snapshot(); s.MountCalls != 2 || s.UnmountCalls != 1 {
		t.Fatalf("合并导航后只应挂载最终页一次: %+v", s)
	}
}

// TestCloseoutBackgroundShutdownCancelOnlyThenFinalizeOnce 验证后台 Shutdown
// 只 CancelOnly（不跑 UI OnUnmount），随后 UI owner 的 LifecycleFinalize
// 恰好完成一次清理，且重复调用幂等。
func TestCloseoutBackgroundShutdownCancelOnlyThenFinalizeOnce(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	p := &closeoutPage{name: "x", steps: &steps}
	if err := r.Register("/x", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:x")
		return p.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/x"); err != nil {
		t.Fatal(err)
	}
	closeoutFrame(r.View())
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.Shutdown() // 后台线程
	}()
	wg.Wait()

	if !errors.Is(r.Context().Err(), context.Canceled) {
		t.Fatalf("Shutdown 应取消 Router Context, got %v", r.Context().Err())
	}
	if !owner.Closed() || owner.CleanupDone() {
		t.Fatalf("Shutdown 后应 Closed=true / CleanupDone=false: closed=%v cleanup=%v",
			owner.Closed(), owner.CleanupDone())
	}
	if _, u, _, _ := p.counts(); u != 0 {
		t.Fatalf("后台 Shutdown 绝不能执行 UI OnUnmount: %d", u)
	}
	if s := owner.Snapshot(); s.UnmountCalls != 0 {
		t.Fatalf("Shutdown 后清理计数应为零: %+v", s)
	}

	r.LifecycleFinalize() // UI owner 销毁回调
	if !owner.CleanupDone() {
		t.Fatal("LifecycleFinalize 后 CleanupDone 必须为 true")
	}
	if _, u, _, _ := p.counts(); u != 1 {
		t.Fatalf("LifecycleFinalize 应恰好一次清理: %d", u)
	}
	r.LifecycleFinalize()
	r.LifecycleFinalize()
	if _, u2, _, _ := p.counts(); u2 != 1 || owner.Snapshot().UnmountCalls != 1 {
		t.Fatalf("重复 LifecycleFinalize 不得重复清理: unmounts=%d snap=%+v", u2, owner.Snapshot())
	}
}

// TestCloseoutShutdownDropsInFlightWithoutMount 验证关闭赢锁时丢弃在途导航，
// 不挂载未发布页、不执行 UI OnUnmount。
func TestCloseoutShutdownDropsInFlightWithoutMount(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	late := &closeoutPage{name: "late", steps: &steps}
	if err := r.Register("/late", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:late")
		return late.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/late"); err != nil {
		t.Fatal(err)
	}
	r.Shutdown()
	closeoutFrame(r.View()) // 关闭后帧不得发布/挂载
	if r.Current() != "" {
		t.Fatalf("关闭后不得发布页面: current=%q", r.Current())
	}
	if _, u, _, _ := late.counts(); u != 0 {
		t.Fatalf("丢弃页不得产生 UI OnUnmount: %d", u)
	}
	if s := owner.Snapshot(); s.MountCalls != 0 || s.UnmountCalls != 0 {
		t.Fatalf("关闭后在途页不得挂载/清理: %+v", s)
	}
}
