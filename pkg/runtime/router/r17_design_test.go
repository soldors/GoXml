package router

// R17-B B2：无窗口设计验证——关闭与导航竞争、待处理队列、工厂失败、
// 旧页取消、重复关闭、双实例隔离与丢弃计数。
//
// 方法：真实 Router 方法 + 白盒同步点（rootCancel / applyNavigation 直调）+
// 手动帧驱动；零窗口。每条用例先注明判据来源（B1 既有契约 / 决策表候选），
// 并区分「现状行为（守卫锁定）」与「新语义候选（交裁决）」。
//
// 判据边界：B1 无公开 Shutdown——一次性关闭语义以 rootCancel 幂等性验证；
// 导出 Shutdown 属 B2 任务卡草案 B2-1（未批准），进 candidateRef 不进本文件。

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// r17Fixture 提供路由 + 记录页 + 手动帧驱动。
type r17Fixture struct {
	r   *Router
	gtx *layout.Context
	th  *material.Theme
}

func newR17(t *testing.T) *r17Fixture {
	t.Helper()
	r := New()
	if err := r.SetInvalidator(&countInv{}); err != nil {
		t.Fatal(err)
	}
	return &r17Fixture{r: r, gtx: &layout.Context{Ops: new(op.Ops)}, th: material.NewTheme()}
}

func (f *r17Fixture) frame() {
	f.gtx.Reset()
	f.r.View().Layout(*f.gtx, f.th)
}

// TestR17CloseRaceInvariants 关闭与导航竞争的不变量性质（多轮交错）：
// 无论 Navigate 与 rootCancel 以何种顺序交错，关闭完成并驱动一帧后——
//   - Current 要么保持旧值、要么是被成功构造的新页（无半提交）；
//   - 目标页至多构造一次；
//   - 丢弃计数与实际丢弃路径一致（入口侧逐请求、drain 侧队列+pendingNav）。
//
// 判据：B1 既有契约（授权 §4.6 无半提交、线程模型 §4 丢弃记录、§7 线程归属）。
func TestR17CloseRaceInvariants(t *testing.T) {
	for iter := 0; iter < 50; iter++ {
		f := newR17(t)
		r := f.r
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
		f.frame()

		// 竞争：导航与关闭并发（真 goroutine；不假设具体交错）。
		var wg sync.WaitGroup
		var navErr error
		wg.Add(2)
		go func() { defer wg.Done(); navErr = r.Navigate("/b") }()
		go func() { defer wg.Done(); r.rootCancel() }()
		wg.Wait()
		f.frame()

		// 不变量 1：无半提交——/b 构造至多一次。
		if got := pb.constructed.Load(); got > 1 {
			t.Fatalf("iter %d: /b 构造 %d 次（>1）", iter, got)
		}
		// 不变量 2：Current 与构造一致——要么保持 /a，要么已切换 /b（恰构造 1 次）。
		cur := r.Current()
		if cur != "/a" && cur != "/b" {
			t.Fatalf("iter %d: Current=%q 非法", iter, cur)
		}
		if cur == "/b" && pb.constructed.Load() != 1 {
			t.Fatalf("iter %d: Current=/b 但构造 %d 次", iter, pb.constructed.Load())
		}
		// 不变量 3：Navigate 返回 nil（关闭前被接受）而 Current 仍 /a 时，
		// 该 pending 必须在 drain 侧被丢弃并计数（F-02 修复口径）。
		if navErr == nil && cur == "/a" && r.DroppedCount() < 1 {
			t.Fatalf("iter %d: 被接受的导航在关闭后帧未计入丢弃（漏计数）", iter)
		}
		// 不变量 4：关闭后入口拒绝仍成立。
		if err := r.Navigate("/b"); err == nil {
			t.Fatalf("iter %d: 关闭后 Navigate 应报错", iter)
		}
	}
}

// TestR17PendingQueueMergedAtClose 待处理队列在关闭时的合并语义（守卫）：
// 关闭前多个 pending 合并为最后一个（同帧合并语义不因关闭改变）；
// drain 侧丢弃只按「1 个 pendingNav」计数，被合并的中间目标不构造。
//
// 判据：B1 既有契约（授权 §四.B 同帧合并 + 线程模型 §4）。
func TestR17PendingQueueMergedAtClose(t *testing.T) {
	f := newR17(t)
	r := f.r
	pa, pb, pc := &recPage{}, &recPage{}, &recPage{}
	for path, p := range map[string]*recPage{"/a": pa, "/b": pb, "/c": pc} {
		if err := r.Register(path, factoryOf(p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	f.frame()
	ran := false
	r.Dispatch(func() { ran = true })
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/c"); err != nil {
		t.Fatal(err)
	}
	r.rootCancel()
	f.frame()

	if ran {
		t.Fatal("关闭后队列函数不得执行")
	}
	if pb.constructed.Load() != 0 || pc.constructed.Load() != 0 {
		t.Fatalf("关闭后不得构造页面：b=%d c=%d", pb.constructed.Load(), pc.constructed.Load())
	}
	// 丢弃口径：1 个队列 fn + 1 个合并后的 pendingNav（F-02 修复后统一计数）。
	if got := r.DroppedCount(); got != 2 {
		t.Fatalf("丢弃计数 = %d, want 2（1 fn + 1 合并后 pendingNav）", got)
	}
	if got := r.Current(); got != "/a" {
		t.Fatalf("Current=%q, want /a", got)
	}
}

// TestR17FactoryFailureAtCloseBoundary 工厂失败与关闭边界（守卫）：
// 关闭前被接受的失败导航在关闭后帧被丢弃——工厂不执行、NavError 不被写入
// （失败从未发生）、Current 保持。失败记录只属于真实发生的构造。
//
// 判据：B1 既有契约（PageFactory 失败语义 + 线程模型 §4）。
func TestR17FactoryFailureAtCloseBoundary(t *testing.T) {
	f := newR17(t)
	r := f.r
	pa := &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	failRuns := 0
	if err := r.Register("/bad", func(ctx context.Context) (View, error) {
		failRuns++
		return nil, errors.New("boom")
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	f.frame()
	if err := r.Navigate("/bad"); err != nil {
		t.Fatal(err)
	}
	r.rootCancel()
	f.frame()

	if failRuns != 0 {
		t.Fatalf("关闭后失败工厂不得执行，实际 %d 次", failRuns)
	}
	if r.NavError() != nil {
		t.Fatalf("未发生的构造不得记录 NavError，实际 %v", r.NavError())
	}
	if got := r.DroppedCount(); got != 1 {
		t.Fatalf("丢弃计数 = %d, want 1（1 个 pendingNav）", got)
	}
}

// TestR17DoubleCancelIdempotent 重复关闭幂等（决策表「一次性关闭」的现状行）：
// rootCancel 多次调用无副作用；入口拒绝与计数稳定；Context 恒为 Canceled。
//
// 现状行为（守卫）。候选（candidateRef）：导出幂等 Shutdown()——B2-1，未批准。
func TestR17DoubleCancelIdempotent(t *testing.T) {
	f := newR17(t)
	r := f.r
	pa := &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	f.frame()

	r.rootCancel()
	r.rootCancel()
	r.rootCancel()

	if err := r.Context().Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Context.Err=%v, want Canceled", err)
	}
	for i := 0; i < 3; i++ {
		if err := r.Navigate("/a"); err == nil {
			t.Fatalf("关闭后第 %d 次 Navigate 应报错", i+1)
		}
		r.Dispatch(func() {})
	}
	// 入口侧丢弃：3 Navigate + 3 Dispatch = 6；drain 侧无新增（帧前已清空）。
	if got := r.DroppedCount(); got != 6 {
		t.Fatalf("入口侧逐请求计数 = %d, want 6", got)
	}
	if pa.constructed.Load() != 1 {
		t.Fatalf("页面构造次数 = %d, want 1", pa.constructed.Load())
	}
}

// TestR17TwoInstanceCloseIsolation 双实例关闭隔离（守卫）：
// 关闭 r1 不影响 r2 的导航、计数与 Context。
func TestR17TwoInstanceCloseIsolation(t *testing.T) {
	f1, f2 := newR17(t), newR17(t)
	pa, pb := &recPage{}, &recPage{}
	if err := f1.r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := f2.r.Register("/b", factoryOf(pb)); err != nil {
		t.Fatal(err)
	}
	f1.r.rootCancel()
	if err := f2.r.Navigate("/b"); err != nil {
		t.Fatalf("r1 关闭不得影响 r2 导航: %v", err)
	}
	f2.frame()
	if got := f2.r.Current(); got != "/b" {
		t.Fatalf("r2 Current=%q, want /b", got)
	}
	if got := f2.r.DroppedCount(); got != 0 {
		t.Fatalf("r2 丢弃计数 = %d, want 0", got)
	}
	if err := f1.r.Navigate("/a"); err == nil {
		t.Fatal("r1 关闭后导航应报错")
	}
}

// TestR17ApplyNavigationAfterCancelDecisionInput 决策表「取消顺序」：
// pending 导航被接受后、drain 入口检查之后落下关闭的中间态。
//
// 【分支差异】本测试在两个 ref 上断言不同语义（同一受控场景）：
//   - readyRef（现状锁定）：applyNavigation 以已取消根 Context 构造并切换；
//   - candidateRef（本分支，复查原型）：放弃刚构造页、保持旧页、丢弃 +1。
//
// 最终语义由负责人裁决后二选一归并；未批准前候选不得进 readyRef。
func TestR17ApplyNavigationAfterCancelDecisionInput(t *testing.T) {
	f := newR17(t)
	r := f.r
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
	f.frame()
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	// 模拟「入口检查之后、applyNavigation 之前」落下的关闭：直接取消后
	// 白盒直调 applyNavigation（绕过 drain 入口检查，精确复现中间态）。
	r.rootCancel()
	r.applyNavigation()

	// candidateRef 语义（复查原型）：放弃刚构造页、保持旧页、丢弃 +1。
	if got := r.Current(); got != "/a" {
		t.Fatalf("候选语义应保持旧页：Current=%q", got)
	}
	if pb.constructed.Load() != 1 {
		t.Fatalf("工厂应恰好执行 1 次（构造后才复查）：实际 %d", pb.constructed.Load())
	}
	if pb.selfCtx == nil || pb.selfCtx.Err() == nil {
		t.Fatal("候选语义：被放弃页的 Context 应已随根取消")
	}
	if got := r.DroppedCount(); got != 1 {
		t.Fatalf("候选语义：放弃的构造应计入丢弃，实际 %d", got)
	}
	if r.NavError() != nil {
		t.Fatalf("关闭边界不是构造失败，不得写 NavError：实际 %v", r.NavError())
	}
	_ = errors.Is
	_ = context.Canceled
}
