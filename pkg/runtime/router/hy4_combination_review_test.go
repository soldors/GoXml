package router_test

// HY4 / R28-C LIFECYCLE-COMBINATION 独立复核（外部测试包）。
//
// 与作者 S2 组合测试的区别（不复用旧上下文、不弱化判据）：
//   - 作者用「注册给 WithOnDestroy 的同一回调」在 owner 调用点手工触发；
//     HY4 的 app 包测试改为让生产 loopEvents 的 DestroyEvent 分支真实走到
//     owner 最终化（见 app/hy4_combination_test.go）；
//   - 本文件把生命周期顺序 / 失败保旧 / 合并导航 / 默认路径 / 无反射
//     做成可单独 -run 的守卫，供三条 overlay 变异逐条定向验证；
//   - 全部帧由真实 Router.View().Layout 以合成 layout.Context 驱动，
//     不进入 Window.Event / 原生窗口后端。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/lifecycle"
	"github.com/goxml/goxml/pkg/runtime/router"
)

type hy4RPage struct {
	name     string
	mounts   int
	unmounts int
	layouts  int
	steps    *[]string
}

func (p *hy4RPage) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.layouts++
	*p.steps = append(*p.steps, "layout:"+p.name)
	return layout.Dimensions{}
}

func (p *hy4RPage) wrap() *router.LifecyclePage {
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

func hy4RFrame(v router.View) {
	v.Layout(layout.Context{Ops: new(op.Ops)}, material.NewTheme())
}

func hy4RJoin(steps []string) string { return strings.Join(steps, ",") }

func hy4ROwner(t *testing.T, r *router.Router) *lifecycle.Owner {
	t.Helper()
	o, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// TestHY4LifecycleOrderRequiresCommitBeforeMount 是 M2 的健康守卫：
// 把 Mount/生命周期接缝提前到工厂构造阶段后，本用例必须 rc=1。
//
// 判据顺序：Navigate 只入队 → 首次业务 Layout 才构造/提交/Mount →
// 成功切换时「旧页先取消，再一次 OnUnmount」→ 失败工厂不 Mount、旧页保持。
func TestHY4LifecycleOrderRequiresCommitBeforeMount(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	pa := &hy4RPage{name: "a", steps: &steps}
	pb := &hy4RPage{name: "b", steps: &steps}
	if err := r.Register("/a", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:a")
		return pa.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:b")
		return pb.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/bad", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:bad")
		return nil, errors.New("factory failed")
	}); err != nil {
		t.Fatal(err)
	}
	owner := hy4ROwner(t, r)

	// (1) Navigate 只入队，不构造、不挂载。
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("Navigate 阶段不应构造或挂载: %v", steps)
	}
	if s := owner.Snapshot(); s.MountCalls != 0 || s.UnmountCalls != 0 {
		t.Fatalf("Navigate 阶段 owner 应为空: %+v", s)
	}

	// (2) 首次业务 Layout：成功发布后、首次业务 Layout 之前才 Mount。
	hy4RFrame(r.View())
	want := "construct:a,mount:a,layout:a"
	if got := hy4RJoin(steps); got != want {
		t.Fatalf("首次发布顺序 = %q，期望 %q", got, want)
	}
	if s := owner.Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 0 {
		t.Fatalf("首次发布后 owner 快照: %+v", s)
	}

	// (3) 成功切换：构造时旧页仍活；提交后旧页先取消，再一次 OnUnmount。
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	hy4RFrame(r.View())
	want = "construct:a,mount:a,layout:a,construct:b,unmount:a,mount:b,layout:b"
	if got := hy4RJoin(steps); got != want {
		t.Fatalf("成功切换顺序 = %q，期望 %q", got, want)
	}
	if s := owner.Snapshot(); s.MountCalls != 2 || s.UnmountCalls != 1 {
		t.Fatalf("成功切换后 owner 快照: %+v", s)
	}
	if pa.mounts != 1 || pa.unmounts != 1 || pb.mounts != 1 || pb.unmounts != 0 {
		t.Fatalf("切换计数异常 a(m=%d,u=%d) b(m=%d,u=%d)",
			pa.mounts, pa.unmounts, pb.mounts, pb.unmounts)
	}

	// (4) 失败工厂：旧页保持，失败页不 Mount，不产生额外 OnUnmount。
	if err := r.Navigate("/bad"); err != nil {
		t.Fatal(err)
	}
	hy4RFrame(r.View())
	want = "construct:a,mount:a,layout:a,construct:b,unmount:a,mount:b,layout:b,construct:bad,layout:b"
	if got := hy4RJoin(steps); got != want {
		t.Fatalf("失败保旧顺序 = %q，期望 %q", got, want)
	}
	if r.Current() != "/b" || r.NavError() == nil {
		t.Fatalf("失败导航应保留旧页并记录 NavError: current=%q err=%v", r.Current(), r.NavError())
	}
	if s := owner.Snapshot(); s.MountCalls != 2 || s.UnmountCalls != 1 {
		t.Fatalf("失败/丢弃页不得被挂载或重复清理: %+v", s)
	}
}

// TestHY4FailedFactoryKeepsOldPageAndNoUIUnmount 是 M3 的健康守卫：
// 错误路径若错误执行 UI 回调，本用例必须 rc=1。
func TestHY4FailedFactoryKeepsOldPageAndNoUIUnmount(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	ok := &hy4RPage{name: "ok", steps: &steps}
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
	owner := hy4ROwner(t, r)

	if err := r.Navigate("/ok"); err != nil {
		t.Fatal(err)
	}
	hy4RFrame(r.View())
	if s := owner.Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 0 {
		t.Fatalf("首帧后快照: %+v", s)
	}

	if err := r.Navigate("/bad"); err != nil {
		t.Fatal(err)
	}
	hy4RFrame(r.View())
	if got, want := hy4RJoin(steps), "construct:ok,mount:ok,layout:ok,construct:bad,layout:ok"; got != want {
		t.Fatalf("失败保旧步骤 = %q，期望 %q（错误路径不得执行 UI OnUnmount）", got, want)
	}
	if r.Current() != "/ok" || r.NavError() == nil {
		t.Fatalf("失败导航应保留旧页: current=%q err=%v", r.Current(), r.NavError())
	}
	if ok.mounts != 1 || ok.unmounts != 0 || ok.layouts != 2 {
		t.Fatalf("旧页应保持挂载且未清理: m=%d u=%d l=%d", ok.mounts, ok.unmounts, ok.layouts)
	}
	if s := owner.Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 0 {
		t.Fatalf("错误路径不得改动 owner 挂载/清理计数: %+v", s)
	}
}

// TestHY4MergedNavigationConstructsOnlyFinalTarget 验证合并导航只构造最终目标。
func TestHY4MergedNavigationConstructsOnlyFinalTarget(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	pa := &hy4RPage{name: "a", steps: &steps}
	pb := &hy4RPage{name: "b", steps: &steps}
	if err := r.Register("/a", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:a")
		return pa.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:b")
		return pb.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("帧前不得构造任何页面: %v", steps)
	}
	hy4RFrame(r.View())
	if got, want := hy4RJoin(steps), "construct:b,mount:b,layout:b"; got != want {
		t.Fatalf("合并导航步骤 = %q，期望 %q", got, want)
	}
	if s := hy4ROwner(t, r).Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 0 {
		t.Fatalf("合并导航只应挂载最终页一次: %+v", s)
	}
}

// TestHY4ShutdownCancelOnlyNoUIUnmount 验证后台 Shutdown 只 CancelOnly。
func TestHY4ShutdownCancelOnlyNoUIUnmount(t *testing.T) {
	var steps []string
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	pg := &hy4RPage{name: "x", steps: &steps}
	if err := r.Register("/x", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:x")
		return pg.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/x"); err != nil {
		t.Fatal(err)
	}
	hy4RFrame(r.View())
	owner := hy4ROwner(t, r)

	r.Shutdown()
	if s := owner.Snapshot(); !s.Closed || s.CleanupDone || s.UnmountCalls != 0 || pg.unmounts != 0 {
		t.Fatalf("Shutdown 只应取消、不得执行 UI OnUnmount: %+v / u=%d", s, pg.unmounts)
	}
	// UI owner 显式最终化后，清理恰好一次。
	r.LifecycleFinalize()
	if s := owner.Snapshot(); !s.CleanupDone || s.UnmountCalls != 1 || pg.unmounts != 1 {
		t.Fatalf("LifecycleFinalize 后应完成一次清理: %+v / u=%d", s, pg.unmounts)
	}
}

// TestHY4NoOptInPreservesPublishedBehavior 验证默认未启用路径行为不变。
func TestHY4NoOptInPreservesPublishedBehavior(t *testing.T) {
	var steps []string
	r := router.New() // 不调用 EnableLifecycle
	pg := &hy4RPage{name: "x", steps: &steps}
	if err := r.Register("/x", func(ctx context.Context) (router.View, error) {
		steps = append(steps, "construct:x")
		return pg.wrap(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/x"); err != nil {
		t.Fatal(err)
	}
	hy4RFrame(r.View())

	if r.Current() != "/x" || r.NavError() != nil || pg.layouts != 1 {
		t.Fatalf("默认导航/排版行为被改变: current=%q err=%v layouts=%d",
			r.Current(), r.NavError(), pg.layouts)
	}
	if _, err := r.LifecycleOwner(); !errors.Is(err, router.ErrLifecycleNotEnabled) {
		t.Fatalf("未 opt-in 时 LifecycleOwner = %v", err)
	}
	posted := false
	r.LifecyclePost(func() { posted = true })
	r.LifecycleFinalize()
	if posted {
		t.Fatal("未 opt-in 时 LifecyclePost 不应产生任何执行")
	}
	if r.Context().Err() != nil {
		t.Fatal("未 opt-in 时 LifecycleFinalize 不得取消 Router Context")
	}
	if pg.mounts != 0 || pg.unmounts != 0 {
		t.Fatalf("未 opt-in 时不得产生生命周期回调: m=%d u=%d", pg.mounts, pg.unmounts)
	}
	if got, want := hy4RJoin(steps), "construct:x,layout:x"; got != want {
		t.Fatalf("默认路径步骤 = %q，期望 %q", got, want)
	}
	r.Shutdown()
	if !errors.Is(r.Context().Err(), context.Canceled) {
		t.Fatalf("默认路径 Shutdown 应取消 Router Context, got %v", r.Context().Err())
	}
}

// hy4PlainView 只有一个 Layout，带一个 VM 形态的字段；用于证明框架不反射
// 扫描/改写 VM，生命周期回调只能经显式 LifecyclePage 包装获得。
type hy4PlainView struct {
	layouts   int
	vmCounter int
	mountSeen int
}

func (p *hy4PlainView) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.layouts++
	return layout.Dimensions{}
}

// TestHY4PlainViewRequiresExplicitLifecycleWrapper 验证不反射 VM。
func TestHY4PlainViewRequiresExplicitLifecycleWrapper(t *testing.T) {
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	plain := &hy4PlainView{}
	if err := r.Register("/plain", func(ctx context.Context) (router.View, error) {
		return plain, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/plain"); err != nil {
		t.Fatal(err)
	}
	hy4RFrame(r.View())

	owner := hy4ROwner(t, r)
	h := owner.Current()
	if h == nil {
		t.Fatal("显式发布的页面应产生挂载句柄")
	}
	if h.State() != lifecycle.StateMounted {
		t.Fatalf("句柄状态 = %v，期望 mounted", h.State())
	}
	if h.Target() != plain {
		t.Fatalf("生命周期目标应就是显式传入的 View，实测 %T", h.Target())
	}
	if plain.layouts != 1 || plain.vmCounter != 0 || plain.mountSeen != 0 {
		t.Fatalf("不得反射扫描/改写 VM 字段，也不得凭空产生包装回调: %+v", plain)
	}
	if s := owner.Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 0 {
		t.Fatalf("普通 View 只应建立一次生命期句柄: %+v", s)
	}
}
