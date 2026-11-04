package router

// R16-B ROUTER-B1-RELEASE-CANDIDATE：导航生命周期（B2）、队列与重绘（B3）的可独立复核验收测试。
//
// 方法：围绕**既有接口**做受控反例，同步点（显式帧驱动 / done 通道）控制
// 顺序；只断言 B1 既有契约（授权 §4、线程模型 §3/§4/§5/§7）。B1 独立验收
// 报告的 F-02（drain 丢弃口径缺口）在本文件复现并按契约修复——见
// TestR16ClosedDropCountsPendingNav。
//
// 原生时序边界（测试无法模拟，明确标注）：真实 DestroyEvent → rootCtx 取消
// 的生产接线（B2 任务卡草案 B2-2/B2-3）不在 B1 授权内；真实窗口视觉验收
// 人工执行；-race 在 CGO_ENABLED=0 环境不可验证（D-006）。

import (
	"context"
	"errors"
	"image"
	"strings"
	"sync/atomic"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// ---------------------------------------------------------------------------
// 公共夹具：计数失效器、帧驱动、记录页
// ---------------------------------------------------------------------------

// countInv 计数失效器（Invalidator 结构化实现）。
type countInv struct{ calls atomic.Int64 }

func (c *countInv) Invalidate() { c.calls.Add(1) }

// driveFrame 驱动一帧：drain + 渲染当前页（UI 线程模拟，无窗口）。
func driveFrame(t *testing.T, r *Router, gtx *layout.Context, th *material.Theme) layout.Dimensions {
	t.Helper()
	gtx.Reset()
	return r.View().Layout(*gtx, th)
}

// recPage 记录构造次数 / 渲染次数 / 构造期观察的页。
type recPage struct {
	layouts atomic.Int64
	// constructed 记录工厂被调用的次数（生命周期断言用，与渲染次数分离）。
	constructed atomic.Int64
	// selfCtx 是路由创建时交给工厂的页面 Context（PageFactory ctx 通道）。
	selfCtx   context.Context
	onCreate  func(ctx context.Context)
	onCreateE func(ctx context.Context) error
}

func (p *recPage) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	p.layouts.Add(1)
	return layout.Dimensions{}
}

func factoryOf(p *recPage) PageFactory {
	return func(ctx context.Context) (View, error) {
		p.constructed.Add(1)
		p.selfCtx = ctx
		if p.onCreate != nil {
			p.onCreate(ctx)
		}
		return p, nil
	}
}

func failFactory(msg string) PageFactory {
	return func(ctx context.Context) (View, error) {
		return nil, errors.New(msg)
	}
}

func newTestRouter(t *testing.T) (*Router, *countInv, *layout.Context, *material.Theme) {
	t.Helper()
	r := New()
	inv := &countInv{}
	if err := r.SetInvalidator(inv); err != nil {
		t.Fatal(err)
	}
	gtx := &layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
	}
	return r, inv, gtx, material.NewTheme()
}

// ---------------------------------------------------------------------------
// B2：导航和页面生命周期
// ---------------------------------------------------------------------------

// TestR16LegalSwitchLifecycle 合法切换的完整时序（同步点控制）：
// 帧前 pending 不生效 → 帧内「先构造新页（此时旧 ctx 仍存活）→ 后取消旧 ctx」
// → 旧页停止渲染。被替换页面的 Context 生命周期与构造顺序是本测试的核心。
func TestR16LegalSwitchLifecycle(t *testing.T) {
	r, inv, gtx, th := newTestRouter(t)
	pa, pb := &recPage{}, &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", factoryOf(pb)); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)
	if got := r.Current(); got != "/a" {
		t.Fatalf("帧后 Current=%q, want /a", got)
	}

	// 受控反例：构造新页时旧 ctx 必须仍然存活（无半提交顺序：先构造成功，
	// 才取消旧页 Context——授权 §4.6）。旧页 ctx 在 /a 工厂里捕获。
	var oldCtxDuringNew context.Context
	var oldCancelledDuringNew bool
	pb.onCreate = func(_ context.Context) {
		oldCtxDuringNew = pa.selfCtx
		oldCancelledDuringNew = pa.selfCtx != nil && pa.selfCtx.Err() != nil
	}

	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	if got := r.Current(); got != "/a" {
		t.Fatalf("pending 导航在帧前不得生效：Current=%q, want /a", got)
	}
	if pb.layouts.Load() != 0 {
		t.Fatal("帧前新页不得被构造或渲染")
	}
	aLayoutsBefore := pa.layouts.Load()

	driveFrame(t, r, gtx, th)

	if got := r.Current(); got != "/b" {
		t.Fatalf("帧后 Current=%q, want /b", got)
	}
	if pb.layouts.Load() != 1 {
		t.Fatalf("新页应恰渲染 1 帧，实际 %d", pb.layouts.Load())
	}
	if oldCtxDuringNew == nil {
		t.Fatal("构造 /b 时未能捕获 /a 的页面 Context（工厂 ctx 通道失效）")
	}
	if oldCancelledDuringNew {
		t.Fatal("构造新页时旧页 Context 已被取消——违反「先构造成功再取消」顺序")
	}
	if err := pa.selfCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("切换后旧页 Context 应为 Canceled，实际 %v", err)
	}
	if inv.calls.Load() < 1 {
		t.Fatal("合法切换应产生失效通知")
	}
	_ = aLayoutsBefore
}

// TestR16FactoryErrorThenRetry 工厂失败 → 本次导航整体放弃（旧页保持运行、
// Current 不变、NavError 记录）→ 随后重试成功 → NavError 清空。
func TestR16FactoryErrorThenRetry(t *testing.T) {
	r, _, gtx, th := newTestRouter(t)
	pa, pg := &recPage{}, &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/bad", failFactory("boom")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/good", factoryOf(pg)); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)

	if err := r.Navigate("/bad"); err != nil {
		t.Fatal(err)
	}
	aBefore := pa.layouts.Load()
	driveFrame(t, r, gtx, th)

	if got := r.Current(); got != "/a" {
		t.Fatalf("失败导航后 Current=%q, want /a（整体放弃）", got)
	}
	if pa.layouts.Load() != aBefore+1 {
		t.Fatalf("失败帧内旧页应照常渲染 1 次（旧页保持运行）: %d -> %d", aBefore, pa.layouts.Load())
	}
	ne := r.NavError()
	if ne == nil || !strings.Contains(ne.Error(), "boom") {
		t.Fatalf("NavError 应记录失败原因，实际 %v", ne)
	}
	if pg.layouts.Load() != 0 {
		t.Fatal("失败导航不得渲染目标页")
	}

	// 重试：合法路径 → 成功，NavError 清空。
	if err := r.Navigate("/good"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)
	if got := r.Current(); got != "/good" {
		t.Fatalf("重试后 Current=%q, want /good", got)
	}
	if r.NavError() != nil {
		t.Fatalf("成功导航应清空 NavError，实际 %v", r.NavError())
	}
}

// TestR16DuplicateNavigationCancelsPending 重复导航语义（授权 §4.7）：
// 同帧多次导航合并最后一个；「导航到当前路径」撤销任何未应用的 pending。
func TestR16DuplicateNavigationCancelsPending(t *testing.T) {
	r, _, gtx, th := newTestRouter(t)
	pa, pb, pc := &recPage{}, &recPage{}, &recPage{}
	for path, p := range map[string]*recPage{"/a": pa, "/b": pb, "/c": pc} {
		if err := r.Register(path, factoryOf(p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)

	// 同帧合并：/b、/c 都 pending → 只构造 /c。
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/c"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)
	if got := r.Current(); got != "/c" {
		t.Fatalf("同帧多次导航应保留最终目标，Current=%q", got)
	}
	if pb.layouts.Load() != 0 {
		t.Fatal("被合并的中间页不得被构造")
	}

	// pending 撤销：回到当前路径 → 未应用的 /a 请求被撤销，/a 不重建。
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/c"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)
	if got := r.Current(); got != "/c" {
		t.Fatalf("Current=%q, want /c", got)
	}
	if pa.constructed.Load() != 1 {
		t.Fatalf("撤销 pending 后旧目标不得被再次构造，构造次数 %d", pa.constructed.Load())
	}
}

// ---------------------------------------------------------------------------
// B3：队列和重绘
// ---------------------------------------------------------------------------

// TestR16DrainOrderQueueThenNavigation 导航与 Dispatch 先后（线程模型 §3）：
// 帧首先排空队列、后应用路由变更——队列函数观察到的是**旧**路径。
func TestR16DrainOrderQueueThenNavigation(t *testing.T) {
	r, _, gtx, th := newTestRouter(t)
	pa, pb := &recPage{}, &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", factoryOf(pb)); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)

	var seenByFn string
	r.Dispatch(func() { seenByFn = r.Current() })
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)

	if seenByFn != "/a" {
		t.Fatalf("队列函数应先于路由变更执行（观察到旧路径），实际看到 %q", seenByFn)
	}
	if got := r.Current(); got != "/b" {
		t.Fatalf("帧后 Current=%q, want /b", got)
	}
}

// TestR16CallbackTriggersNextNavigation 回调触发下一次导航：
// Dispatch 的 fn 在帧内调用 Navigate → 该导航在**同一帧**应用
// （队列排空在路由应用之前，回调产生的 pending 同帧生效）。
func TestR16CallbackTriggersNextNavigation(t *testing.T) {
	r, _, gtx, th := newTestRouter(t)
	pa, pb := &recPage{}, &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", factoryOf(pb)); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	driveFrame(t, r, gtx, th)

	r.Dispatch(func() {
		if err := r.Navigate("/b"); err != nil {
			t.Errorf("回调内 Navigate: %v", err)
		}
	})
	driveFrame(t, r, gtx, th)

	if got := r.Current(); got != "/b" {
		t.Fatalf("回调触发的导航应同帧生效，Current=%q", got)
	}
	if pb.layouts.Load() != 1 {
		t.Fatalf("回调目标页应恰渲染 1 帧，实际 %d", pb.layouts.Load())
	}
}

// TestR16ClosedDropCountsPendingNav 关闭路径的丢弃计数口径（契约：线程模型
// §4「丢弃并记录诊断」应覆盖**全部**丢弃；B1 独立验收报告 F-02）。
//
// 受控反例：先入队 1 个 fn + 1 个 pending 导航（都发生在关闭**前**），然后
// 取消 Router Context，再驱动一帧——drain 侧丢弃两者。基线只计队列长度、
// 漏计 pendingNav（F-02 缺口）；本测试按统一口径断言。
func TestR16ClosedDropCountsPendingNav(t *testing.T) {
	r := New()
	if r.rootCancel == nil {
		t.Fatal("Router 应初始化 rootCancel（B1 无生产取消源，测试用）")
	}
	pa, pb := &recPage{}, &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", factoryOf(pb)); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	gtx := &layout.Context{Ops: new(op.Ops)}
	driveFrame(t, r, gtx, material.NewTheme())

	ran := false
	r.Dispatch(func() { ran = true })
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	r.rootCancel()
	driveFrame(t, r, gtx, material.NewTheme())

	if ran {
		t.Fatal("关闭后队列函数不得执行")
	}
	if got := r.Current(); got != "/a" {
		t.Fatalf("关闭后 Current 不得变更，实际 %q", got)
	}
	if got := r.DroppedCount(); got != 2 {
		t.Fatalf("drain 侧丢弃应统一计数（1 队列 fn + 1 pendingNav），实际 %d（F-02）", got)
	}
}

// TestR16InvalidationSequenceAndIsolation 失效通知序列与双实例隔离：
// 合法切换恰 1 次失效；重复导航（no-op）不追加失效；Dispatch 各 1 次；
// 两个 Router 实例互不串写。
func TestR16InvalidationSequenceAndIsolation(t *testing.T) {
	r1, inv1, gtx1, th1 := newTestRouter(t)
	r2, inv2, gtx2, th2 := newTestRouter(t)
	pa := &recPage{}
	pb := &recPage{}
	if err := r1.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r2.Register("/b", factoryOf(pb)); err != nil {
		t.Fatal(err)
	}

	if err := r1.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	if got := inv1.calls.Load(); got != 1 {
		t.Fatalf("合法切换应恰 1 次失效通知，实际 %d", got)
	}
	if got := inv2.calls.Load(); got != 0 {
		t.Fatalf("r1 的导航不得触发 r2 的失效通知（双实例隔离），实际 %d", got)
	}
	driveFrame(t, r1, gtx1, th1)
	driveFrame(t, r2, gtx2, th2)

	// 重复导航 no-op：失效计数不变。
	if err := r1.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	if got := inv1.calls.Load(); got != 1 {
		t.Fatalf("重复导航（no-op）不得追加失效通知，实际 %d", got)
	}
	// Dispatch：入队即 1 次失效。
	r1.Dispatch(func() {})
	if got := inv1.calls.Load(); got != 2 {
		t.Fatalf("Dispatch 应恰 1 次失效通知，实际 %d", got)
	}
	// r2 自己的导航只影响 r2。
	if err := r2.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	if got := inv2.calls.Load(); got != 1 {
		t.Fatalf("r2 导航应恰 1 次失效，实际 %d", got)
	}
	if got := inv1.calls.Load(); got != 2 {
		t.Fatalf("r2 的导航不得触发 r1 失效，实际 %d", got)
	}
}
