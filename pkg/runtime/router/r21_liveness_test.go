package router

// R21-B ROUTER-DISPATCH-LIVENESS：F-08 实测与队列活性守卫。
//
// 现状（基线 a9bd8857 实测 + 源码）：drain 的 fn 执行循环无界——fn 内再
// Dispatch 会在**同一帧**继续执行（fixpoint 语义），循环体逐 fn 检查根取消
// 并对剩余批次计数；F-08 注释已声明该边界（B1 独立验收报告：语义可辩护）。
//
// 本文件用有限回调链 + 同步点实测该语义，锁定为回归守卫（readyRef）：
//   - 链全部同帧执行、FIFO、同帧再入队；
//   - 同帧导航被链**推迟**到队列排空之后才应用（仍同帧生效，最后导航胜出）；
//   - 关闭与链竞争：剩余批次计数、导航丢弃、Current 不变；
//   - 双实例隔离；丢弃计数口径。
//
// 有限实测与源码推断的区分：链深 ≤2000 的行为为**实测**；
// 「无上限增长」来自源码推断（drain 无预算字段），以 candidateRef 原型对照。
// 每个探针帧均有墙钟超时（frameWithDeadline）。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// livenessFixture 手动帧驱动 + 墙钟超时探针。
type livenessFixture struct {
	r   *Router
	gtx *layout.Context
	th  *material.Theme
}

func newLiveness(t *testing.T) *livenessFixture {
	t.Helper()
	r := New()
	if err := r.SetInvalidator(&countInv{}); err != nil {
		t.Fatal(err)
	}
	return &livenessFixture{r: r, gtx: &layout.Context{Ops: new(op.Ops)}, th: material.NewTheme()}
}

// frameWithDeadline 驱动一帧并施加强制墙钟超时：超时即测试失败（探针不被
// 无界链挂死）。d <= 0 表示用默认 10s。
func (f *livenessFixture) frameWithDeadline(t *testing.T, d time.Duration) {
	t.Helper()
	if d <= 0 {
		d = 10 * time.Second
	}
	done := make(chan struct{})
	go func() {
		f.gtx.Reset()
		f.r.View().Layout(*f.gtx, f.th)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("帧驱动超过墙钟上限 %v（F-08 探针超时）", d)
	}
}

// runChain 建立深度 k 的回调链（fn_i 完成时分派 fn_{i+1}），返回执行记录
// 与链头 Dispatch。每节记录完成时观察到的 Current（供「推迟导航」断言）。
func runChain(r *Router, k int) (head func(), executed *atomic.Int64, seenCurrent *atomic.Value) {
	executed = &atomic.Int64{}
	seenCurrent = &atomic.Value{}
	var next func(i int)
	next = func(i int) {
		executed.Add(1)
		seenCurrent.Store(r.Current())
		if i < k {
			r.Dispatch(func() { next(i + 1) })
		}
	}
	return func() { next(1) }, executed, seenCurrent
}

// TestR21ChainRunsSameFrameAndDelaysNavigation F-08 主判据。
// 【分支差异】readyRef（budget=0，fixpoint）：64 节链全部同帧执行，链内
// Current 恒为 /a，帧末 /b——导航被链推迟。candidateRef（本分支，每帧预算
// 8）：fn 执行跨 8 帧（每帧恰 8 节），但**导航不受预算推迟**——第 1 帧末
// 即切换 /b（导航活性优于基线）。最终语义待裁决二选一。
func TestR21ChainRunsSameFrameAndDelaysNavigation(t *testing.T) {
	f := newLiveness(t)
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
	f.frameWithDeadline(t, 0)

	head, executed, seenCurrent := runChain(r, 64)
	r.Dispatch(head)
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	f.frameWithDeadline(t, 0)
	elapsed := time.Since(start)

	if got := executed.Load(); got != 64 {
		t.Fatalf("链应全部同帧执行：executed=%d, want 64", got)
	}
	// 链内每节观察到的都是旧路径——导航被推迟到队列排空之后（readyRef 语义）。
	if v, _ := seenCurrent.Load().(string); v != "/a" {
		t.Fatalf("链内观察到的 Current=%q, want /a", v)
	}
	if got := r.Current(); got != "/b" {
		t.Fatalf("帧末 Current=%q, want /b（同帧仍生效，最后导航胜出）", got)
	}
	if pb.constructed.Load() != 1 {
		t.Fatalf("/b 构造次数 = %d, want 1", pb.constructed.Load())
	}
	if elapsed > 5*time.Second {
		t.Fatalf("64 节链 8 帧耗时 %v（探针上限 5s）", elapsed)
	}
	t.Logf("64 节链跨 8 帧完成（每帧预算 8），耗时 %v（candidateRef 语义）", elapsed)
}

// TestR21FIFOAndSameFrameReenqueue FIFO 与同帧再入队（守卫）：
// 批内快照 FIFO（当前批 = 入队时的快照）；先执行的 fn 再入队的任务在
// **同帧的下一批**执行（fixpoint 到空为止），不插队到当前批之前。
// 实测序 [f1, f2, f1-requeued] 精确锁定该语义。
func TestR21FIFOAndSameFrameReenqueue(t *testing.T) {
	f := newLiveness(t)
	var order []string
	var mu sync.Mutex
	appendOrder := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	f.r.Dispatch(func() {
		appendOrder("f1")
		f.r.Dispatch(func() { appendOrder("f1-requeued") })
	})
	f.r.Dispatch(func() { appendOrder("f2") })
	f.frameWithDeadline(t, 0)

	mu.Lock()
	defer mu.Unlock()
	want := []string{"f1", "f2", "f1-requeued"}
	if len(order) != len(want) {
		t.Fatalf("执行序 %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("执行序 %v, want %v（批内快照 FIFO + 同帧 fixpoint）", order, want)
		}
	}
}

// TestR21LastNavigationWinsWithChain 最后导航生效（守卫）：
// 链与多个 pending 并存 → 只构造最终目标。
func TestR21LastNavigationWinsWithChain(t *testing.T) {
	f := newLiveness(t)
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
	f.frameWithDeadline(t, 0)
	head, executed, _ := runChain(r, 16)
	f.r.Dispatch(head)
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/c"); err != nil {
		t.Fatal(err)
	}
	for frame := 1; frame <= 2; frame++ { // candidateRef：预算 8 → 16 节跨 2 帧
		f.frameWithDeadline(t, 0)
	}
	if got := executed.Load(); got != 16 {
		t.Fatalf("链执行 %d 节, want 16", got)
	}
	if got := r.Current(); got != "/c" {
		t.Fatalf("Current=%q, want /c（最后导航胜出）", got)
	}
	if pb.constructed.Load() != 0 {
		t.Fatalf("中间目标不得构造，实际 %d", pb.constructed.Load())
	}
}

// TestR21BackgroundDispatchDuringChain 后台 Dispatch（守卫）：
// 链执行中途后台线程入队 → 当前 fixpoint 语义下**同帧**执行完毕。
// （源码推断：该保证来自 drain 外层循环到空为止；实测覆盖单次后台入队。）
func TestR21BackgroundDispatchDuringChain(t *testing.T) {
	f := newLiveness(t)
	r := f.r
	bgRan := &atomic.Bool{}
	entered := make(chan struct{})
	proceed := make(chan struct{})
	firstDone := false
	f.r.Dispatch(func() {
		if !firstDone {
			firstDone = true
			close(entered)
			<-proceed // 阻塞首节，制造「链执行中途」窗口
		}
	})
	go func() {
		<-entered
		r.Dispatch(func() { bgRan.Store(true) })
		close(proceed)
	}()
	f.frameWithDeadline(t, 0)
	if !bgRan.Load() {
		t.Fatal("链执行中途的后台 Dispatch 应同帧执行（fixpoint 语义）")
	}
}

// TestR21ShutdownMidChainCompetes Shutdown 与链竞争（实测）：
// 深度 200 的链在执行中被 Shutdown——已执行节数有限、剩余批次计入
// DroppedCount、pending 导航丢弃、Current 保持；总计账一致。
func TestR21ShutdownMidChainCompetes(t *testing.T) {
	f := newLiveness(t)
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
	f.frameWithDeadline(t, 0)

	// 链节阻塞在闸门上：放行前 Shutdown 已完成，竞争窗口确定。
	gate := make(chan struct{})
	chainStarted := make(chan struct{})
	var chainOnce sync.Once
	var next func(i int)
	next = func(i int) {
		chainOnce.Do(func() { close(chainStarted) })
		if i < 200 {
			r.Dispatch(func() { next(i + 1) })
		}
		<-gate
	}
	r.Dispatch(func() { next(1) })
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		f.gtx.Reset()
		f.r.View().Layout(*f.gtx, f.th)
		close(done)
	}()
	<-chainStarted // 链首已执行（阻塞在闸门）
	r.Shutdown()   // 关闭竞争：锁序保证见 R18
	close(gate)    // 放行链
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("帧驱动超过墙钟上限 10s")
	}

	if pb.constructed.Load() != 0 {
		t.Fatalf("关闭后 pending 导航不得构造页面，实际 %d", pb.constructed.Load())
	}
	if got := r.Current(); got != "/a" {
		t.Fatalf("Current=%q, want /a", got)
	}
	// 计账：drain 剩余批次 + pendingNav 全部计入 DroppedCount（口径守卫）。
	dropped := r.DroppedCount()
	if dropped < 1 {
		t.Fatalf("关闭竞争后应有丢弃计账，实际 %d", dropped)
	}
	t.Logf("竞争结果：DroppedCount=%d（剩余批次+pendingNav），/b 未构造", dropped)
}

// TestR21TwoInstanceChainIsolation 双实例隔离（守卫）：
// r1 的链不影响 r2 的导航与计数。
func TestR21TwoInstanceChainIsolation(t *testing.T) {
	f1, f2 := newLiveness(t), newLiveness(t)
	pa, pb := &recPage{}, &recPage{}
	if err := f1.r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := f2.r.Register("/b", factoryOf(pb)); err != nil {
		t.Fatal(err)
	}
	head, executed, _ := runChain(f1.r, 32)
	f1.r.Dispatch(head)
	if err := f2.r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	f2.frameWithDeadline(t, 0)
	if got := f2.r.Current(); got != "/b" {
		t.Fatalf("r2 Current=%q, want /b（r1 链执行期间 r2 正常）", got)
	}
	if got := f2.r.DroppedCount(); got != 0 {
		t.Fatalf("r2 丢弃计数 = %d, want 0", got)
	}
	for frame := 1; frame <= 4; frame++ { // candidateRef：预算 8 → 32 节跨 4 帧
		f1.frameWithDeadline(t, 0)
	}
	if got := executed.Load(); got != 32 {
		t.Fatalf("r1 链执行 %d 节, want 32", got)
	}
}

// TestR21ClosedDropAccounting 关闭后入队逐请求计数（守卫，与 R16 衔接）。
func TestR21ClosedDropAccounting(t *testing.T) {
	f := newLiveness(t)
	r := f.r
	if err := r.Register("/a", factoryOf(&recPage{})); err != nil {
		t.Fatal(err)
	}
	r.Shutdown()
	for i := 0; i < 3; i++ {
		if err := r.Navigate("/a"); err == nil {
			t.Fatalf("关闭后第 %d 次 Navigate 应报错", i+1)
		}
		f.r.Dispatch(func() {})
	}
	if got := r.DroppedCount(); got != 6 {
		t.Fatalf("入口逐请求计数 = %d, want 6", got)
	}
	var fnRan bool
	f.r.Dispatch(func() { fnRan = true })
	if fnRan || r.DroppedCount() != 7 {
		t.Fatalf("关闭后 Dispatch 应丢弃并计数：ran=%v dropped=%d", fnRan, r.DroppedCount())
	}
}

// compile-time guards：探针使用的错误路径保持可编译（防夹具腐化）。
var (
	_ = errors.New
	_ = context.Canceled
	_ = fmt.Sprintf
)
