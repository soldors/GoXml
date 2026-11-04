package router

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// ---- 测试辅助 ---------------------------------------------------------------

// stubPage 是可计数的最小页面：只记录 Layout 调用次数。
type stubPage struct{ calls atomic.Int64 }

func (p *stubPage) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.calls.Add(1)
	return layout.Dimensions{}
}

// facRec 记录工厂被调用的次数与每次收到的页面 Context。
type facRec struct {
	mu   sync.Mutex
	ctxs []context.Context
}

func (rec *facRec) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.ctxs)
}

func (rec *facRec) lastCtx() context.Context {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.ctxs) == 0 {
		return nil
	}
	return rec.ctxs[len(rec.ctxs)-1]
}

// factory 构造 PageFactory：每次调用登记 ctx，返回 page（或 err）。
func factory(rec *facRec, page View, err error) PageFactory {
	return func(ctx context.Context) (View, error) {
		rec.mu.Lock()
		rec.ctxs = append(rec.ctxs, ctx)
		rec.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return page, nil
	}
}

// frame 以合成 layout.Context 驱动一帧（等价 app 事件循环在 FrameEvent 中
// 调用根 View.Layout；零值 Context 安全——gio 文档明示零值不产生事件）。
func frame(t *testing.T, r *Router) {
	t.Helper()
	gtx := layout.Context{Ops: &op.Ops{}}
	r.View().Layout(gtx, material.NewTheme())
}

var errBoom = errors.New("boom: 页面构造失败")

// ---- 测试 -------------------------------------------------------------------

// 授权 §4.1：注册成功及重复注册。
func TestRegisterSuccessAndDuplicate(t *testing.T) {
	r := New()
	rec := &facRec{}
	if err := r.Register("/a", factory(rec, &stubPage{}, nil)); err != nil {
		t.Fatalf("首次注册不应报错: %v", err)
	}
	if err := r.Register("/b", factory(rec, &stubPage{}, nil)); err != nil {
		t.Fatalf("注册不同路径不应报错: %v", err)
	}
	err := r.Register("/a", factory(rec, &stubPage{}, nil))
	if !errors.Is(err, ErrPathRegistered) {
		t.Fatalf("重复注册应返回 ErrPathRegistered，得到: %v", err)
	}
	if err := r.Register("", factory(rec, &stubPage{}, nil)); err == nil {
		t.Fatal("空路径应被拒绝")
	}
	if err := r.Register("/nil", nil); err == nil {
		t.Fatal("nil 工厂应被拒绝")
	}
}

// 授权 §4.2 + §4.5：精确匹配成功；导航后当前页面与路径状态一致。
func TestNavigateExactMatchActivatesPage(t *testing.T) {
	r := New()
	pageA := &stubPage{}
	recA := &facRec{}
	if err := r.Register("/a", factory(recA, pageA, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatalf("导航: %v", err)
	}
	// 帧前：请求已排队但未应用（状态变更只发生在 UI 线程帧内）。
	if got := r.Current(); got != "" {
		t.Fatalf("帧前 Current 应为空，得到 %q", got)
	}
	frame(t, r)
	if got := r.Current(); got != "/a" {
		t.Fatalf("帧后 Current 应为 /a，得到 %q", got)
	}
	if recA.count() != 1 {
		t.Fatalf("工厂应恰好调用 1 次，得到 %d", recA.count())
	}
	if pageA.calls.Load() != 1 {
		t.Fatalf("本帧应渲染新页面 1 次，得到 %d", pageA.calls.Load())
	}
	if ctx := recA.lastCtx(); ctx == nil || ctx.Err() != nil {
		t.Fatalf("新页面 Context 应有效（非 nil、未取消），得到 %v", ctx)
	}
}

// 授权 §4.3：未注册路径的明确错误。
func TestNavigateUnregisteredPathError(t *testing.T) {
	r := New()
	err := r.Navigate("/nope")
	if !errors.Is(err, ErrPathNotRegistered) {
		t.Fatalf("未注册路径应返回 ErrPathNotRegistered，得到: %v", err)
	}
	if got := r.Current(); got != "" {
		t.Fatalf("失败的导航不得改变当前路径，得到 %q", got)
	}
}

// 授权 §4.4：不因尾部斜杠、大小写或相似路径而意外匹配。
func TestExactMatchingNoLooseMatches(t *testing.T) {
	r := New()
	rec := &facRec{}
	if err := r.Register("/a", factory(rec, &stubPage{}, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	for _, p := range []string{"/a/", "/A", "/a/b", "a", "/ab"} {
		if err := r.Navigate(p); !errors.Is(err, ErrPathNotRegistered) {
			t.Fatalf("Navigate(%q) 应返回 ErrPathNotRegistered，得到: %v", p, err)
		}
	}
	if got := rec.count(); got != 0 {
		t.Fatalf("相似路径不得触发任何页面构造，得到 %d 次", got)
	}
	// 注册表未被破坏：精确路径仍可导航。
	if err := r.Navigate("/a"); err != nil {
		t.Fatalf("精确路径应仍可导航: %v", err)
	}
	frame(t, r)
	if got := r.Current(); got != "/a" {
		t.Fatalf("Current 应为 /a，得到 %q", got)
	}
}

// 授权 §4.6：页面创建失败时不产生半提交的导航状态。
func TestPageCreationFailureNoHalfCommit(t *testing.T) {
	r := New()
	pageA := &stubPage{}
	recA := &facRec{}
	recBad := &facRec{}
	if err := r.Register("/ok", factory(recA, pageA, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Register("/bad", factory(recBad, nil, errBoom)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Navigate("/ok"); err != nil {
		t.Fatalf("导航: %v", err)
	}
	frame(t, r)
	callsBefore := pageA.calls.Load()
	oldCtx := recA.lastCtx()

	// 导航到会创建失败的页面。
	if err := r.Navigate("/bad"); err != nil {
		t.Fatalf("Navigate 本身应成功（校验阶段）: %v", err)
	}
	frame(t, r)

	if got := r.Current(); got != "/ok" {
		t.Fatalf("创建失败后 Current 必须保持 /ok，得到 %q", got)
	}
	if ctx := recA.lastCtx(); ctx == nil || ctx != oldCtx || ctx.Err() != nil {
		t.Fatalf("旧页面 Context 不得被取消或更换：old=%v now=%v err=%v", oldCtx, recA.lastCtx(), func() error {
			if ctx := recA.lastCtx(); ctx != nil {
				return ctx.Err()
			}
			return nil
		}())
	}
	if got := recBad.count(); got != 1 {
		t.Fatalf("失败工厂应恰好被调用 1 次，得到 %d", got)
	}
	if got := r.NavError(); got == nil || !errors.Is(got, errBoom) {
		t.Fatalf("NavError 应包裹工厂错误 errBoom，得到: %v", got)
	}
	// 失败导航的那一帧里，旧页面照常渲染（callsBefore+1）；
	// NavError 不影响后续帧继续渲染旧页。
	frame(t, r)
	if got := pageA.calls.Load(); got != callsBefore+2 {
		t.Fatalf("旧页面应在失败导航帧及后续帧继续渲染（before=%d after=%d）", callsBefore, got)
	}
}

// 授权 §4.7：同一路径重复导航的确定语义（no-op）。
func TestRepeatNavigationSamePathIsNoOp(t *testing.T) {
	r := New()
	pageA := &stubPage{}
	recA := &facRec{}
	if err := r.Register("/a", factory(recA, pageA, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatalf("首次导航: %v", err)
	}
	frame(t, r)
	if err := r.Navigate("/a"); err != nil {
		t.Fatalf("重复导航应返回 nil，得到: %v", err)
	}
	frame(t, r)
	if got := recA.count(); got != 1 {
		t.Fatalf("重复导航不得重新构造页面（工厂调用 %d 次）", got)
	}
	if got := pageA.calls.Load(); got != 2 {
		t.Fatalf("页面应被渲染两次（每帧一次），得到 %d", got)
	}
	if ctx := recA.lastCtx(); ctx == nil || ctx.Err() != nil {
		t.Fatal("重复导航不得取消当前页面 Context")
	}
	if got := r.NavError(); got != nil {
		t.Fatalf("重复导航不是错误，NavError 应为 nil，得到: %v", got)
	}
}

// 授权 §4.8 + §4.9：离开取消旧 Context；新页面获得独立有效 Context。
func TestLeaveCancelsOldContextNewContextIndependent(t *testing.T) {
	r := New()
	recA, recB := &facRec{}, &facRec{}
	if err := r.Register("/a", factory(recA, &stubPage{}, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Register("/b", factory(recB, &stubPage{}, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatalf("导航: %v", err)
	}
	frame(t, r)
	ctxA := recA.lastCtx()
	if ctxA == nil {
		t.Fatal("/a 页面工厂未被调用（未登记 Context）")
	}
	if err := r.Navigate("/b"); err != nil {
		t.Fatalf("导航: %v", err)
	}
	frame(t, r)
	if err := ctxA.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("旧页面 Context 应已取消，得到: %v", err)
	}
	ctxB := recB.lastCtx()
	if ctxB == nil || ctxB.Err() != nil {
		t.Fatalf("新页面 Context 应有效，得到 %v", ctxB)
	}
	if ctxA == ctxB {
		t.Fatal("新旧页面必须持有不同 Context 实例")
	}
	// 独立性：旧页 Context 取消不影响 Router 级 Context。
	if err := r.Context().Err(); err != nil {
		t.Fatalf("Router 级 Context 不应被页面切换取消: %v", err)
	}
	if r.rootCtx == nil || ctxA == r.rootCtx {
		t.Fatal("页面 Context 必须派生自 Router 级 Context 且非同一实例")
	}
}

// 授权 §4.10：页面生命周期不会被重复执行或重复释放。
func TestFactoryCalledExactlyOncePerNavigation(t *testing.T) {
	r := New()
	recA, recB := &facRec{}, &facRec{}
	if err := r.Register("/a", factory(recA, &stubPage{}, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Register("/b", factory(recB, &stubPage{}, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	_ = r.Navigate("/a")
	_ = r.Navigate("/b") // 同帧两次导航：合并，只应用最后一个
	frame(t, r)
	if got := recA.count(); got != 0 {
		t.Fatalf("被合并的中间导航不得构造页面（/a 工厂被调 %d 次）", got)
	}
	if got := recB.count(); got != 1 {
		t.Fatalf("/b 工厂应恰好调用 1 次，得到 %d", got)
	}
	// 重复导航不重复构造。
	_ = r.Navigate("/b")
	frame(t, r)
	if got := recB.count(); got != 1 {
		t.Fatalf("重复导航后 /b 工厂仍应只被调用 1 次，得到 %d", got)
	}
}

// 授权 §4.11：不允许后台线程直接修改 Router 状态——后台唯一合法入口
// Navigate 的效果必须延迟到 UI 线程帧内；Dispatch 的函数必须在帧内执行。
func TestBackgroundNavigationDeferredToFrame(t *testing.T) {
	r := New()
	pageA, pageB := &stubPage{}, &stubPage{}
	recA, recB := &facRec{}, &facRec{}
	if err := r.Register("/a", factory(recA, pageA, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Register("/b", factory(recB, pageB, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	_ = r.Navigate("/a")
	frame(t, r) // 此时 UI 线程（测试 goroutine）已建立 /a

	dispatched := make(chan struct{}, 1)
	bgDone := make(chan struct{})
	go func() {
		defer close(bgDone)
		// 后台线程：合法入口只有 Navigate / Dispatch / Invalidate。
		_ = r.Navigate("/b")
		r.Dispatch(func() { dispatched <- struct{}{} })
	}()
	<-bgDone // 等后台请求入队（入队是同步返回的），再断言"帧前"状态。

	// 帧前：状态不得已被修改。
	if got := r.Current(); got != "/a" {
		t.Fatalf("后台请求必须在帧内才生效（帧前 Current=%q）", got)
	}
	if got := recB.count(); got != 0 {
		t.Fatalf("帧前不得构造新页面，得到 %d 次", got)
	}
	select {
	case <-dispatched:
		t.Fatal("Dispatch 的函数不得在帧外执行")
	default:
	}

	// UI 线程帧：应用导航并执行队列。
	frame(t, r)
	if got := r.Current(); got != "/b" {
		t.Fatalf("帧后 Current 应为 /b，得到 %q", got)
	}
	select {
	case <-dispatched:
	default:
		t.Fatal("Dispatch 的函数应在帧内执行")
	}
}

// 线程模型 §4：Dispatcher 关闭后新任务被丢弃并记录诊断。
func TestDispatchAfterCancelDropped(t *testing.T) {
	r := New()
	if r.rootCancel == nil {
		t.Fatal("Router 应初始化 rootCancel（B1 无生产取消源，测试用）")
	}
	ran := false
	r.rootCancel()
	r.Dispatch(func() { ran = true })
	if got := r.DroppedCount(); got != 1 {
		t.Fatalf("关闭后 Dispatch 应被丢弃并计数，得到 %d", got)
	}
	if ran {
		t.Fatal("关闭后 Dispatch 的函数不得执行")
	}
	if err := r.Navigate("/x"); err == nil {
		t.Fatal("关闭后 Navigate 应返回错误")
	}
}

// 授权 §2：UI 线程归属及必要的防误用机制——Invalidate 接线。
func TestInvalidatorWiring(t *testing.T) {
	r := New()
	if err := r.SetInvalidator(nil); err == nil {
		t.Fatal("nil invalidator 应被拒绝")
	}
	var calls atomic.Int64
	inv := invFunc(func() { calls.Add(1) })
	if err := r.SetInvalidator(inv); err != nil {
		t.Fatalf("首次设置: %v", err)
	}
	if err := r.SetInvalidator(inv); err == nil {
		t.Fatal("重复设置应被拒绝（防止运行中换窗）")
	}
	if err := r.Register("/a", factory(&facRec{}, &stubPage{}, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	_ = r.Navigate("/a")
	if got := calls.Load(); got != 1 {
		t.Fatalf("Navigate 应触发恰好一次 Invalidate，得到 %d", got)
	}
	r.Invalidate()
	if got := calls.Load(); got != 2 {
		t.Fatalf("Invalidate 应透传，得到 %d", got)
	}
}

type invFunc func()

func (f invFunc) Invalidate() { f() }

// 授权 §4.12：与项目既有 View / Gio 帧入口兼容（结构化形状 + 合成帧渲染）。
// app.View 的冻结形状见运行时入口契约 §2：Layout(gtx, th)；
// 本断言证明 router 根 View 与该形状逐字一致（不导入 pkg/runtime/app）。
// R17-B candidateRef：F-05 自指形状断言已删除（与 View 接口定义同构，断言力≈0；
// 真实兼容由集成测试与 r16_acceptance_test 的宿主组合测试承担）。

func TestRootViewRendersCurrentPageOnly(t *testing.T) {
	r := New()
	pageA, pageB := &stubPage{}, &stubPage{}
	if err := r.Register("/a", factory(&facRec{}, pageA, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	if err := r.Register("/b", factory(&facRec{}, pageB, nil)); err != nil {
		t.Fatalf("注册: %v", err)
	}
	// 无当前页：渲染为空，不 panic。
	if d := r.View().Layout(layout.Context{Ops: &op.Ops{}}, material.NewTheme()); d.Size.X != 0 || d.Size.Y != 0 {
		t.Fatal("无当前页时应返回零 Dimensions")
	}
	_ = r.Navigate("/a")
	frame(t, r)
	_ = r.Navigate("/b")
	frame(t, r)
	if got := pageA.calls.Load(); got != 1 {
		t.Fatalf("切走后旧页面不得再被渲染（得到 %d 次）", got)
	}
	if got := pageB.calls.Load(); got != 1 {
		t.Fatalf("新页面应渲染 1 次，得到 %d", got)
	}
}
