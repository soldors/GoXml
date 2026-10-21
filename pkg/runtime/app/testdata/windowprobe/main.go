// windowprobe 是 TASK-FONT-001-D-PROBE 的取证探针：
// 验证**真实平台窗口**上 `Window.Invalidate()` 是否可靠地产生后续 FrameEvent。
//
// 与同目录其它探针的分工：
//   - lifecycleprobe / concurrencyprobe 用 headless.NewWindow（无 Invalidate 方法）
//   - 本探针用 gioui.org/app.Window（真实平台窗口，D3D11 后端）
//
// 为什么不用 go test：真实窗口在无图形会话时可能永不产生 FrameEvent，
// 而 `t.Skip` 在汇总里和 pass 一样是绿的——会把「没验」显示成「没问题」。
// 本探针改为显式退出码：0 全通过 / 1 存在 FAIL / 3 超时或环境阻塞。
//
// 判据来源：docs/04-WBS/任务卡/TASK-FONT-001-D-PROBE.md §3.2（不得在这里放宽）。
//
// 运行：go run ./pkg/runtime/app/testdata/windowprobe
//
// 执行时会在桌面短暂创建 5 个窗口（各约 1s），这是真实窗口验证的固有代价。
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"image/color"
)

const (
	// phaseTimeout 是单个 phase 的硬上限。超过即判定事件循环卡住，
	// 直接中止整个探针（挂起的窗口无法安全回收）。
	phaseTimeout = 4 * time.Second
	// globalTimeout 是全程上限，防止某个 phase 内部逻辑绕圈。
	globalTimeout = 40 * time.Second
	// stuckWindow 给 P2 在帧处理期间人为制造的窗口宽度。
	stuckWindow = 20 * time.Millisecond
)

// ============================================================================
// 基础设施
// ============================================================================

// frameLog 记录每次 FrameEvent 的时间戳。事件循环写、driver 读，故加锁。
type frameLog struct {
	mu    sync.Mutex
	times []time.Time
}

func (f *frameLog) add(t time.Time) {
	f.mu.Lock()
	f.times = append(f.times, t)
	f.mu.Unlock()
}

func (f *frameLog) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.times)
}

// at 返回第 i 帧的时间戳（i 从 0 开始）。越界返回零值。
func (f *frameLog) at(i int) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < 0 || i >= len(f.times) {
		return time.Time{}
	}
	return f.times[i]
}

// after 统计时间戳晚于 t 的帧数——用于判定「最后一次唤醒之后仍出帧」。
func (f *frameLog) after(t time.Time) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, ts := range f.times {
		if ts.After(t) {
			n++
		}
	}
	return n
}

// newWindow 创建一个真实平台窗口。每个 phase 独立窗口，避免前一 phase
// 的待处理帧污染后一 phase 的计数。
func newWindow(title string) *app.Window {
	w := new(app.Window)
	w.Option(
		app.Title(title),
		app.Size(unit.Dp(240), unit.Dp(90)),
	)
	return w
}

// drive 驱动事件循环直到窗口销毁。与 pkg/runtime/app/app.go 的 loop 同构
// （同样的 ops 复用 + NewContext + e.Frame 顺序），但多两个观测钩子。
//
// onFrame 在 e.Frame 之前调用，n 从 1 开始——这样钩子确实在
// 「帧处理期间」执行，而不是在帧提交之后。
func drive(w *app.Window, onFrame func(n int, at time.Time), onDestroy func()) {
	var ops op.Ops
	n := 0
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			if onDestroy != nil {
				onDestroy()
			}
			return
		case app.FrameEvent:
			n++
			gtx := app.NewContext(&ops, e)
			paint.Fill(gtx.Ops, color.NRGBA{R: 0x1E, G: 0x4F, B: 0x8F, A: 0xFF})
			if onFrame != nil {
				onFrame(n, time.Now())
			}
			e.Frame(gtx.Ops)
		}
	}
}

// waitFor 轮询等待条件成立。轮询而非固定 sleep：窗口创建的耗时
// 受 DPI 缓存与后端影响，固定等待会读到未就绪状态而误判。
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// quiesce 等待「连续 quiet 时间内没有新帧」，用于建立可归因的基线。
func quiesce(fl *frameLog, quiet, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	last := fl.count()
	lastChange := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		if n := fl.count(); n != last {
			last = n
			lastChange = time.Now()
			continue
		}
		if time.Since(lastChange) >= quiet {
			return true
		}
	}
	return false
}

// closeAndWait 关闭窗口并等待事件循环退出。
func closeAndWait(w *app.Window, done <-chan struct{}, wait time.Duration) bool {
	w.Perform(system.ActionClose)
	select {
	case <-done:
		return true
	case <-time.After(wait):
		return false
	}
}

type result struct {
	id     string
	ok     bool
	detail string
}

func verdict(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

type phase struct {
	id  string
	run func() (bool, string)
}

// runPhase 在独立 goroutine 中执行 phase，用超时兜住「事件循环卡住」。
// panic 也在这一层兜住——注意：concurrent map 是 fatal error，
// recover 无效，那种情况进程会直接以退出码 2 终止（本身就是证据）。
func runPhase(p phase) result {
	type out struct {
		ok     bool
		detail string
	}
	ch := make(chan out, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- out{false, fmt.Sprintf("panic: %v", r)}
			}
		}()
		ok, detail := p.run()
		ch <- out{ok, detail}
	}()
	select {
	case o := <-ch:
		return result{p.id, o.ok, o.detail}
	case <-time.After(phaseTimeout):
		return result{p.id, false, fmt.Sprintf("TIMEOUT：未在 %s 内返回，事件循环疑似卡住", phaseTimeout)}
	}
}

// ============================================================================
// P1：首帧之前唤醒
// ============================================================================

// phaseP1 在首次 Event() 之前连续调用两次 Invalidate。
//
// 判据（卡 §3.2 P1）：不 panic、不阻塞、循环仍能产生 ≥1 帧。
//
// ⚠️ 明确不判定「唤醒生效」：Gio 的 mayInvalidate 零值为 false，
// driver 未创建时 Invalidate 会被静默跳过。因此帧数只能说明
// 「窗口创建本身产生了首帧」，不得写成「首帧由 Invalidate 触发」。
func phaseP1() (bool, string) {
	w := newWindow("goxml probe P1")
	created := time.Now()

	// Event() 之前：driver 尚未创建，这两次调用应被静默跳过。
	w.Invalidate()
	w.Invalidate()

	var fl frameLog
	done := make(chan struct{})
	go func() {
		drive(w, func(_ int, at time.Time) { fl.add(at) }, nil)
		close(done)
	}()

	time.Sleep(1200 * time.Millisecond)
	closed := closeAndWait(w, done, 2*time.Second)

	n := fl.count()
	if !closed {
		return false, "Perform(ActionClose) 后 2s 内事件循环未退出（可能卡住）"
	}
	if n < 1 {
		return false, "帧数=0——窗口创建后连一帧都没有，环境可能无交互式图形会话"
	}
	toFirst := fl.at(0).Sub(created)

	// 记录帧间距：静态窗口也会被平台事件（create/configure/focus）触发额外帧，
	// 这个事实直接决定了「帧数」不能当作唤醒生效的证据（P4 判据据此设计）。
	var gaps []string
	for i := 1; i < fl.count() && i < 6; i++ {
		gaps = append(gaps, fmt.Sprintf("%.1f", float64(fl.at(i).Sub(fl.at(i-1)).Microseconds())/1000.0))
	}
	return true, fmt.Sprintf(
		"首帧前两次 Invalidate：未 panic、未阻塞；1.2s 内帧数=%d；创建→首帧 %.1f ms；"+
			"帧间距(ms)=[%s]（静态窗口也有系统触发帧 → 帧数不可归因于 Invalidate）",
		n, float64(toFirst.Microseconds())/1000.0, strings.Join(gaps, " "))
}

// ============================================================================
// P2：首帧处理期间唤醒
// ============================================================================

// phaseP2 在第 1 个 FrameEvent 的处理过程中（e.Frame 之前）让后台
// goroutine 调用 Invalidate，验证该请求不会被吞掉。
//
// 这是 D 的真实失效模式：懒加载完成的那一刻，很可能正好落在一帧的处理里。
func phaseP2() (bool, string) {
	w := newWindow("goxml probe P2")

	var fl frameLog
	trigger := make(chan struct{})
	bgDone := make(chan struct{})
	var invAt atomic.Int64

	// 后台：被触发后先记为「请求时刻」，再调用 Invalidate，
	// 返回后关闭 bgDone——让事件循环能确认「请求已落在帧处理期间」。
	go func() {
		defer close(bgDone)
		<-trigger
		invAt.Store(time.Now().UnixNano())
		w.Invalidate()
	}()

	done := make(chan struct{})
	go func() {
		drive(w, func(n int, at time.Time) {
			fl.add(at)
			if n == 1 {
				close(trigger)
				<-bgDone // 确保 Invalidate 已返回，此刻仍在帧 1 的处理中
				time.Sleep(stuckWindow)
			}
		}, nil)
		close(done)
	}()

	ok := waitFor(2*time.Second, func() bool { return fl.count() >= 2 })
	closed := closeAndWait(w, done, 2*time.Second)

	if !ok {
		return false, fmt.Sprintf(
			"帧处理期间 Invalidate 未产生第 2 帧（帧数停在 %d）——请求被吞，D 会静默丢更新",
			fl.count())
	}
	if !closed {
		return false, "第 2 帧已出现，但关闭后 2s 内事件循环未退出"
	}
	inv := time.Unix(0, invAt.Load())
	lat := fl.at(1).Sub(inv)
	// 延迟里含探针自己造的 stuckWindow，报净值便于与 P3（空闲态唤醒）对照。
	netLat := lat - stuckWindow
	return true, fmt.Sprintf(
		"帧1 处理中调用 Invalidate → 2s 内出现帧2；帧2 延迟 %.1f ms"+
			"（含探针人为睡眠 %.1f ms，净等待 %.1f ms——帧在途时的唤醒需等本帧提交完成，与 P3 空闲态对照）",
		float64(lat.Microseconds())/1000.0,
		float64(stuckWindow.Microseconds())/1000.0,
		float64(netLat.Microseconds())/1000.0)
}

// ============================================================================
// P3：静默后唤醒（基线链路 + 延迟量级）
// ============================================================================

func phaseP3() (bool, string) {
	w := newWindow("goxml probe P3")
	created := time.Now()

	var fl frameLog
	done := make(chan struct{})
	go func() {
		drive(w, func(_ int, at time.Time) { fl.add(at) }, nil)
		close(done)
	}()

	if !waitFor(2*time.Second, func() bool { return fl.count() >= 1 }) {
		closeAndWait(w, done, time.Second)
		return false, "2s 内没有首帧——环境可能无交互式图形会话"
	}
	toFirst := fl.at(0).Sub(created)

	if !quiesce(&fl, 300*time.Millisecond, 2*time.Second) {
		closeAndWait(w, done, time.Second)
		return false, "等待静默超时（帧持续不断），无法建立可归因基线"
	}

	before := fl.count()
	invAt := time.Now()
	w.Invalidate()
	ok := waitFor(2*time.Second, func() bool { return fl.count() > before })
	closed := closeAndWait(w, done, 2*time.Second)

	if !ok {
		return false, fmt.Sprintf("静默后单次 Invalidate 未产生新帧（帧数停在 %d）", before)
	}
	if !closed {
		return false, "已产生新帧，但关闭后 2s 内事件循环未退出"
	}
	lat := fl.at(before).Sub(invAt)
	return true, fmt.Sprintf(
		"静默 300ms 后单次 Invalidate → 新帧延迟 %.1f ms（帧数 %d→%d；创建→首帧 %.1f ms）",
		float64(lat.Microseconds())/1000.0, before, fl.count(), float64(toFirst.Microseconds())/1000.0)
}

// ============================================================================
// P4：突发重复唤醒（合并行为 + 最后一次不丢 + 事后不僵死）
// ============================================================================

func phaseP4() (bool, string) {
	const burst = 50

	w := newWindow("goxml probe P4")

	// 因果判据的核心：帧内读取「已返回的唤醒请求序号」。
	//
	// 为什么不能只看「帧时间戳是否晚于突发结束」：静态窗口本身就会被
	// 平台事件（create/configure/focus）触发额外帧（P1 实测 1.2s 内 5 帧），
	// 时间戳比对会把系统帧算成唤醒的成果。
	// 而 acked 在每轮 Invalidate **返回之后**才自增，因此「某帧读到的
	// acked == 50」只能来自「第 50 次请求返回之后才开始的帧」。
	var acked atomic.Int64
	var maxAckSeen atomic.Int64

	var fl frameLog
	done := make(chan struct{})
	go func() {
		drive(w, func(_ int, at time.Time) {
			fl.add(at)
			storeMax(&maxAckSeen, acked.Load())
		}, nil)
		close(done)
	}()

	if !waitFor(2*time.Second, func() bool { return fl.count() >= 1 }) {
		closeAndWait(w, done, time.Second)
		return false, "2s 内没有首帧"
	}
	if !quiesce(&fl, 300*time.Millisecond, 2*time.Second) {
		closeAndWait(w, done, time.Second)
		return false, "等待静默超时"
	}

	// (a) 无间隔连发 50 次
	before := fl.count()
	burstStart := time.Now()
	for i := int64(1); i <= burst; i++ {
		w.Invalidate()
		acked.Store(i)
	}
	burstDur := time.Since(burstStart)

	// (b) 因果判据：必须出现一帧，它读到 acked == burst
	okLast := waitFor(2*time.Second, func() bool { return maxAckSeen.Load() >= burst })
	during := fl.count() - before

	// (c) 突发之后再单次唤醒仍出帧 —— 链路未僵死
	if !quiesce(&fl, 300*time.Millisecond, 2*time.Second) {
		closeAndWait(w, done, time.Second)
		return false, "突发后等待静默超时"
	}
	beforeSingle := fl.count()
	w.Invalidate()
	okSingle := waitFor(2*time.Second, func() bool { return fl.count() > beforeSingle })
	closed := closeAndWait(w, done, 2*time.Second)

	// (a) 只作边界守卫：合并可以，但不能 0 帧（全被吞），也不应超过请求数。
	okA := during >= 1 && during <= burst
	if !closed {
		return false, "关闭后 2s 内事件循环未退出"
	}

	detail := fmt.Sprintf(
		"连发 %d 次（耗时 %.2f ms）→ 突发窗口内新帧 %d（合并比 %d:1）；"+
			"帧内读到的最大已返回序号=%d（需达 %d）；事后单次唤醒%s",
		burst, float64(burstDur.Microseconds())/1000.0, during, burst/max(during, 1),
		maxAckSeen.Load(), burst,
		map[bool]string{true: "出帧", false: "未出帧"}[okSingle])

	switch {
	case !okA && during == 0:
		return false, detail + " → 全部唤醒被吞（0 帧），链路失效"
	case !okA:
		return false, detail + fmt.Sprintf(" → 新帧数超出 [1,%d] 区间", burst)
	case !okLast:
		return false, detail + " → 第 50 次唤醒返回后没有任何帧读到该序号：最后一次更新未被渲染（丢最终更新）"
	case !okSingle:
		return false, detail + " → 突发后链路僵死"
	}
	return true, detail
}

// storeMax 把 v 写入 dst 时保持单调递增（并发安全）。
func storeMax(dst *atomic.Int64, v int64) {
	for {
		old := dst.Load()
		if v <= old || dst.CompareAndSwap(old, v) {
			return
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ============================================================================
// P5：关闭期并发唤醒（生命周期）
// ============================================================================

func phaseP5() (bool, string) {
	w := newWindow("goxml probe P5")

	var fl frameLog
	var destroyed atomic.Bool
	done := make(chan struct{})
	go func() {
		drive(w, func(_ int, at time.Time) { fl.add(at) }, func() { destroyed.Store(true) })
		close(done)
	}()

	if !waitFor(2*time.Second, func() bool { return fl.count() >= 1 }) {
		closeAndWait(w, done, time.Second)
		return false, "2s 内没有首帧"
	}

	// driver 每 1ms 请求一次重绘，制造「关闭与唤醒并发」的窗口。
	stopInv := make(chan struct{})
	var invCount atomic.Int64
	invDone := make(chan struct{})
	go func() {
		defer close(invDone)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopInv:
				return
			case <-ticker.C:
				w.Invalidate()
				invCount.Add(1)
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	closeAt := time.Now()
	w.Perform(system.ActionClose)

	gotDestroy := waitFor(3*time.Second, func() bool { return destroyed.Load() })
	closed := waitFor(2*time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	close(stopInv)
	<-invDone

	// 关闭之后再唤醒一次：不得 panic（recover 在 runPhase 外层兜）。
	w.Invalidate()

	if !gotDestroy {
		return false, fmt.Sprintf("ActionClose 后 3s 内未收到 DestroyEvent（唤醒次数=%d）", invCount.Load())
	}
	if !closed {
		return false, "收到 DestroyEvent 但事件循环 2s 内未退出"
	}
	lat := time.Duration(0)
	if fl.count() > 0 {
		lat = time.Since(closeAt)
	}
	return true, fmt.Sprintf(
		"关闭期并发唤醒 %d 次；关闭→DestroyEvent %.1f ms；关闭后再 Invalidate 未 panic；总帧数=%d",
		invCount.Load(), float64(lat.Microseconds())/1000.0, fl.count())
}

// ============================================================================
// main
// ============================================================================

func printEnv() {
	gioVer := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "gioui.org" {
				gioVer = d.Version
			}
		}
		for _, s := range info.Settings {
			if s.Key == "CGO_ENABLED" {
				fmt.Printf("ENV_CGO=%s\n", s.Value)
			}
		}
	}
	fmt.Printf("ENV go=%s os=%s arch=%s gio=%s mode=real-window(app.Window)\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, gioVer)
}

func main() {
	fmt.Println("=== TASK-FONT-001-D-PROBE：真实窗口异步唤醒验证 ===")
	fmt.Println("判据来源：docs/04-WBS/任务卡/TASK-FONT-001-D-PROBE.md §3.2（探针不得放宽）")
	printEnv()

	phases := []phase{
		{"P1", phaseP1},
		{"P2", phaseP2},
		{"P3", phaseP3},
		{"P4", phaseP4},
		{"P5", phaseP5},
	}

	start := time.Now()
	var results []result
	for _, p := range phases {
		if time.Since(start) > globalTimeout {
			fmt.Printf("ABORT：全程超过 %s，剩余 phase 不再执行\n", globalTimeout)
			break
		}
		fmt.Printf(">>> %s 开始\n", p.id)
		r := runPhase(p)
		fmt.Printf("PHASE %s %s %s\n", r.id, verdict(r.ok), r.detail)
		results = append(results, r)
		if strings.Contains(r.detail, "TIMEOUT") {
			fmt.Println("ABORT：phase 超时，后续 phase 不再执行（避免挂起窗口叠加）")
			printSummary(results)
			os.Exit(3)
		}
	}

	failed := printSummary(results)
	if failed > 0 || len(results) < len(phases) {
		os.Exit(1)
	}
	os.Exit(0)
}

// printSummary 打印汇总并返回失败数。
func printSummary(results []result) int {
	failed := 0
	fmt.Println("--- 汇总 ---")
	for _, r := range results {
		if !r.ok {
			failed++
		}
	}
	fmt.Printf("TOTAL=%d PASS=%d FAIL=%d\n", len(results), len(results)-failed, failed)
	return failed
}
