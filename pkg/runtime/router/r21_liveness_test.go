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
// 有限实测与源码推断的区分：64 节完整链为实测；
// 「无上限增长」来自源码推断（drain 无预算字段），以 candidateRef 原型对照。
// 每个探针帧均有墙钟超时（frameWithDeadline）。

import (
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
func runChain(r *Router, k int) (head func(), seenCurrent *[]string) {
	observed := make([]string, 0, k)
	var next func(i int)
	next = func(i int) {
		observed = append(observed, r.Current())
		if i < k {
			r.Dispatch(func() { next(i + 1) })
		}
	}
	return func() { next(1) }, &observed
}

// TestR21ChainRunsSameFrameAndDelaysNavigation F-08 主判据。
// 64 节链全部同帧执行，链内 Current 恒为 /a，帧末切换至 /b。
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

	head, seenCurrent := runChain(r, 64)
	r.Dispatch(head)
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	f.frameWithDeadline(t, 0)
	elapsed := time.Since(start)

	if got := len(*seenCurrent); got != 64 {
		t.Fatalf("链应全部同帧执行：executed=%d, want 64", got)
	}
	for i, current := range *seenCurrent {
		if current != "/a" {
			t.Fatalf("第 %d 节观察到 Current=%q, want /a", i+1, current)
		}
	}
	if got := r.Current(); got != "/b" {
		t.Fatalf("帧末 Current=%q, want /b（同帧仍生效，最后导航胜出）", got)
	}
	if pb.constructed.Load() != 1 {
		t.Fatalf("/b 构造次数 = %d, want 1", pb.constructed.Load())
	}
	t.Logf("64 节链同帧完成，耗时 %v", elapsed)
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
	head, seenCurrent := runChain(r, 16)
	f.r.Dispatch(head)
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/c"); err != nil {
		t.Fatal(err)
	}
	f.frameWithDeadline(t, 0)
	if got := len(*seenCurrent); got != 16 {
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
// 回调再入队后触发 Shutdown，待执行回调计入
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
	var queuedRan atomic.Bool
	r.Dispatch(func() {
		r.Dispatch(func() { queuedRan.Store(true) })
		close(chainStarted)
		<-gate
	})
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		f.gtx.Reset()
		f.r.View().Layout(*f.gtx, f.th)
		close(done)
	}()
	select {
	case <-chainStarted:
	case <-time.After(10 * time.Second):
		close(gate)
		t.Fatal("等待链首回调超过墙钟上限 10s")
	}
	r.Shutdown() // 关闭竞争：锁序保证见 R18
	close(gate)  // 放行链
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
	if dropped != 2 || queuedRan.Load() {
		t.Fatalf("关闭竞争后 DroppedCount=%d, want 2；排队回调执行=%v, want false", dropped, queuedRan.Load())
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
	head, seenCurrent := runChain(f1.r, 32)
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
	f1.frameWithDeadline(t, 0)
	if got := len(*seenCurrent); got != 32 {
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
