package theme

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// TASK-FONT-001-D-ACCEPT-07：缓存耗时语义**只读取证**（A 阶段）
//
// # 本文件要回答的是**事实问题**，不是设计问题
//
// 事实问题：`ResolveWithReport` 在「缓存未命中」与「缓存命中」两条路径下，
// 返回的 `ProbeReport.Elapsed` 是**本轮探测的实测耗时**，还是**上一次探测
// 遗留的历史值**？`Provider.Scans()` 如何变化？两次报告是否共享同一对象？
//
// 设计问题：`ProbeElapsed` **应该**表示哪一个语义？
// ——那是语义裁决，必须在拿到证据之后由负责人裁定。
// 因此本文件**不对任何一种语义做通过/失败断言**，只把观测类别原样记录。
//
// # 为什么不依赖真实计时
//
// `ProviderOptions.Now` 可注入。把时钟做成"每调用一次前进固定步长"的虚拟时钟，
// 每次实测的 `Elapsed` 就成了**确定值**，与本机计时粒度（2026-10-23 实测约
// 0.53 ms）完全无关。第二阶段把步长改为 0，于是：
//
//	观测 e2 == e1（且非零）   ⇒  命中返回的是**历史值**
//	观测 e2 == 0              ⇒  命中返回的是**本轮实测**（本轮没跑探测）
//	其它                      ⇒  第三种行为，需人工复核（例如改用真实时钟）
//
// 两个假设因此可以**判定区分**，而不是只能断言"耗时非零"。
//
// # 非空判据的自我校验
//
// `TestAccept07ElapsedFollowsInjectedClock` 是前提守卫：冻结时钟下首次解析的
// `Elapsed` 必须**恰为 0**。它证明 `Elapsed` 确实由 `opts.Now` 得出，
// 否则本文件所有观测都不成立。
//
// # 执行边界（A 阶段）
//
// 只新增测试文件；不修改任何生产代码（`font.go` / `lazy.go` / `theme.go`），
// 不修改 IMPL-3 的五文件候选，不修改任何任务卡与 dev-log。
// ============================================================================

// accept07Clock 是注入用的确定性虚拟时钟。
//
// step > 0 时每次调用前进 step，`Sub` 结果必然非零且可预测；
// step == 0 时恒返回同一时刻，任何区间的测量结果**必然恰为 0**。
type accept07Clock struct {
	mu    sync.Mutex
	now   time.Time
	step  time.Duration
	calls int
}

// newAccept07Clock 以固定基准起算，避免受运行环境当前时刻影响。
func newAccept07Clock(step time.Duration) *accept07Clock {
	return &accept07Clock{
		now:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		step: step,
	}
}

// Next 满足 ProviderOptions.Now。
func (c *accept07Clock) Next() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.now
	c.now = c.now.Add(c.step)
	c.calls++
	return t
}

// SetStep 改步长。step=0 即"冻结时钟"。
func (c *accept07Clock) SetStep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.step = d
}

// Jump 把基准瞬间推远，用于让第二阶段的绝对时刻明显区别于第一阶段。
func (c *accept07Clock) Jump(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Calls 返回 Now 被调用的次数（用于证明第二阶段是否真的没跑探测）。
func (c *accept07Clock) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// accept07Observation 是观测到的耗时语义类别。
type accept07Observation int

const (
	// accept07Historical：命中返回 e1 同值 ⇒ 历史值
	accept07Historical accept07Observation = iota
	// accept07FreshZero：命中返回 0，而首轮 e1 > 0 ⇒ 本轮实测（本轮未跑探测）
	accept07FreshZero
	// accept07Other：既非相等也非零 ⇒ 需人工复核
	accept07Other
)

func (o accept07Observation) String() string {
	switch o {
	case accept07Historical:
		return "历史值（e2 == e1）"
	case accept07FreshZero:
		return "本轮实测（e2 == 0，本轮未跑探测）"
	case accept07Other:
		return "未归类（e2 既不等于 e1 也不为 0，需人工复核）"
	}
	return "unknown"
}

// accept07Run 是一轮「未命中 → 命中」的观测结果。
//
// 注意 `obs` 是基于 **`ProbeReport.Elapsed`** 的类别，而按 D-ACCEPT-07 冻结的
// 契约，该字段在缓存命中时**保留历史值是正确行为**；判断"本轮跑没跑探测"
// 要看 `ran1` / `ran2`，本轮耗时看 `LoadReport.ProbeElapsed`。
type accept07Run struct {
	obs        accept07Observation
	e1, e2     time.Duration
	ran1, ran2 bool
	samePtr    bool
	sameSlice  bool
	scansAfter int
	reads      int
	nowCalls2  int
	rep1, rep2 *ProbeReport
}

// accept07Observe 跑一轮「未命中 → 命中」，返回观测结果。
func accept07Observe(t *testing.T, step time.Duration) accept07Run {
	t.Helper()

	dir := t.TempDir()
	good := writeFont(t, dir, "accept07_good.otf", hanziFontBytes(t))

	clk := newAccept07Clock(step)
	emb := &failingLoader{err: errors.New("accept07：嵌入不可用（本用例要求系统命中）")}
	reads := 0

	p := NewProvider(ProviderOptions{
		Candidates: []string{good},
		Timeout:    time.Second,
		Read: func(path string) ([]byte, error) {
			reads++
			return os.ReadFile(path)
		},
		Now:      clk.Next,
		Embedded: emb,
	})

	// ---------- 阶段一：缓存未命中 ----------
	if before := p.Scans(); before != 0 {
		t.Fatalf("用例前提不成立：新 Provider 的 Scans=%d，期望 0", before)
	}
	id1, rep1, err1 := p.ResolveWithReport(FamilyCJK)
	if err1 != nil {
		t.Fatalf("首次解析失败: %v", err1)
	}
	if rep1 == nil {
		t.Fatal("首次解析返回 nil 报告")
	}
	scansAfter1 := p.Scans()
	if scansAfter1 != 1 {
		t.Errorf("首次解析应扫描 1 次，实际 %d", scansAfter1)
	}
	if reads != 1 {
		t.Errorf("读取计数 %d，期望 1", reads)
	}
	e1 := rep1.Elapsed
	if e1 <= 0 {
		t.Fatalf("注入时钟未生效：首轮 Elapsed=%v，期望 > 0。"+
			"若此处失败，说明 Elapsed 并非由 opts.Now 得出，"+
			"本文件所有观测的前提都不成立", e1)
	}

	// ---------- 阶段二：切到冻结时钟，并把基准推远 ----------
	clk.Jump(1000 * time.Hour)
	clk.SetStep(0)

	scansBefore2 := p.Scans()
	readsBefore2 := reads
	nowCallsBefore2 := clk.Calls()

	id2, rep2, err2 := p.ResolveWithReport(FamilyCJK)
	if err2 != nil {
		t.Fatalf("第二次解析失败: %v", err2)
	}
	if rep2 == nil {
		t.Fatal("第二次解析返回 nil 报告")
	}
	scansAfter := p.Scans()
	e2 := rep2.Elapsed
	nowCallsPhase2 := clk.Calls() - nowCallsBefore2

	if scansAfter != scansBefore2 {
		t.Errorf("缓存命中却触发了新扫描: Scans %d → %d", scansBefore2, scansAfter)
	}
	if reads != readsBefore2 {
		t.Errorf("缓存命中却重新读文件: reads %d → %d", readsBefore2, reads)
	}
	if id2 != id1 {
		t.Errorf("标识不稳定: 首次 %q，命中 %q", id1, id2)
	}

	samePtr := rep1 == rep2
	sameSlice := false
	if len(rep1.Candidates) > 0 && len(rep2.Candidates) > 0 {
		sameSlice = &rep1.Candidates[0] == &rep2.Candidates[0]
	}

	var obs accept07Observation
	switch {
	case e2 == e1:
		obs = accept07Historical
	case e2 == 0:
		obs = accept07FreshZero
	default:
		obs = accept07Other
	}
	return accept07Run{
		obs:        obs,
		e1:         e1,
		e2:         e2,
		ran1:       rep1.ProbeRan,
		ran2:       rep2.ProbeRan,
		samePtr:    samePtr,
		sameSlice:  sameSlice,
		scansAfter: scansAfter,
		reads:      reads,
		nowCalls2:  nowCallsPhase2,
		rep1:       rep1,
		rep2:       rep2,
	}
}

// TestAccept07ProbeElapsedOnProviderCacheHit 命中与未命中两条路径的实际读数。
//
// 本用例**不判定对错**，只要求观测**可复现**：同样的构造跑两遍必须得到同一类别。
// 这是它唯一的"失败条件"——它能变红，但不预设实现应该是什么样。
func TestAccept07ProbeElapsedOnProviderCacheHit(t *testing.T) {
	const step = 7 * time.Millisecond

	t.Run("未命中→命中 单次观测", func(t *testing.T) {
		r := accept07Observe(t, step)

		t.Logf("=== D-ACCEPT-07 观测记录 ===")
		t.Logf("未命中：Elapsed=%v ProbeRan=%v", r.e1, r.ran1)
		t.Logf("命中　：Elapsed=%v ProbeRan=%v", r.e2, r.ran2)
		t.Logf("Elapsed 类别 = %v（该字段保留历史值是**契约要求**，不是缺陷）", r.obs)
		t.Logf("Scans 命中后 = %d（期望 1：命中不重扫）", r.scansAfter)
		t.Logf("Read 计数 = %d（期望 1：命中不重读）", r.reads)
		t.Logf("第二阶段 Now() 调用次数 = %d（0 意味着命中时**没有**跑 probe）", r.nowCalls2)
		t.Logf("两次报告同一指针 = %v（期望 false：命中返回独立派生副本）", r.samePtr)
		t.Logf("两次报告 Candidates 同一底层数组 = %v（期望 false）", r.sameSlice)

		// 契约断言（修复后）：
		if !r.ran1 {
			t.Errorf("未命中路径 ProbeRan = false，期望 true（本轮确实执行了探测）")
		}
		if r.ran2 {
			t.Errorf("缓存命中路径 ProbeRan = true，期望 false（本轮没有执行探测）")
		}
		if r.samePtr {
			t.Error("缓存命中仍返回 Provider 内部对象（未派生副本）——改动它会污染缓存")
		}
		if r.sameSlice {
			t.Error("命中返回的 Candidates 与内部对象共用底层数组（未被深拷贝）")
		}
		if r.e1 <= 0 {
			t.Fatalf("前提守卫：e1=%v 必须 > 0（注入时钟未生效）", r.e1)
		}
		// 历史诊断必须保留（裁决 ②）。
		if len(r.rep2.Candidates) == 0 {
			t.Error("命中返回的派生报告丢失了历史 Candidates 诊断")
		}
		if r.rep2.Resolved != r.rep1.Resolved {
			t.Errorf("命中返回的派生报告 Resolved=%q，与历史 %q 不一致", r.rep2.Resolved, r.rep1.Resolved)
		}
	})

	t.Run("观测必须可复现", func(t *testing.T) {
		ra := accept07Observe(t, step)
		rb := accept07Observe(t, step)

		if ra.obs != rb.obs {
			t.Errorf("同样构造跑两遍得到不同类别: 第一次 %v，第二次 %v", ra.obs, rb.obs)
		}
		if ra.samePtr != rb.samePtr || ra.sameSlice != rb.sameSlice {
			t.Errorf("对象共享关系不稳定: 第一次 samePtr=%v sameSlice=%v，第二次 samePtr=%v sameSlice=%v",
				ra.samePtr, ra.sameSlice, rb.samePtr, rb.sameSlice)
		}
		if ra.ran1 != rb.ran1 || ra.ran2 != rb.ran2 {
			t.Errorf("ProbeRan 不稳定: 第一次 %v/%v，第二次 %v/%v",
				ra.ran1, ra.ran2, rb.ran1, rb.ran2)
		}
		t.Logf("两次独立构造：类别=%v（一致），e1=%v/%v，e2=%v/%v，ProbeRan=%v/%v，samePtr=%v",
			ra.obs, ra.e1, rb.e1, ra.e2, rb.e2, ra.ran1, ra.ran2, ra.samePtr)
	})
}

// TestAccept07ElapsedFollowsInjectedClock 前提守卫：Elapsed 必须由注入时钟得出。
//
// 冻结时钟（步长 0）下任何区间的测量都只能是精确的 0。若此断言变红，
// 说明 probe 实际用了别的时钟，`TestAccept07ProbeElapsedOnProviderCacheHit`
// 的全部观测随之失效——这是"读不出就不放过"的护栏，不是性能断言。
func TestAccept07ElapsedFollowsInjectedClock(t *testing.T) {
	t.Run("冻结时钟下首次解析的 Elapsed 必须恰为 0", func(t *testing.T) {
		dir := t.TempDir()
		good := writeFont(t, dir, "accept07_frozen.otf", hanziFontBytes(t))

		clk := newAccept07Clock(0) // 步长 0：时钟冻结
		p := NewProvider(ProviderOptions{
			Candidates: []string{good},
			Timeout:    time.Second,
			Read:       os.ReadFile,
			Now:        clk.Next,
			Embedded:   &failingLoader{err: errors.New("accept07：嵌入不可用")},
		})

		_, rep, err := p.ResolveWithReport(FamilyCJK)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if got := rep.Elapsed; got != 0 {
			t.Errorf("冻结时钟下 Elapsed=%v，期望**恰为 0**——"+
				"若不为 0，说明 Elapsed 不是由 opts.Now 得出，"+
				"上面那个用例的观测前提不成立", got)
		}
		t.Logf("守卫通过：冻结时钟下 Elapsed=%v，Now 调用 %d 次", rep.Elapsed, clk.Calls())
	})

	t.Run("步长翻倍则耗时必须变大", func(t *testing.T) {
		small := accept07Observe(t, 3*time.Millisecond)
		big := accept07Observe(t, 30*time.Millisecond)
		eSmall, eBig := small.e1, big.e1

		if eBig <= eSmall {
			t.Errorf("步长 30ms 的 Elapsed(%v) 未大于步长 3ms 的 Elapsed(%v)——"+
				"注入时钟没有真正驱动耗时测量", eBig, eSmall)
		}
		t.Logf("步长 3ms → Elapsed=%v（类别=%v）；步长 30ms → Elapsed=%v（类别=%v）；比值=%.1fx",
			eSmall, small.obs, eBig, big.obs, float64(eBig)/float64(eSmall))
	})
}

// TestAccept07LazyProbeElapsedWithWarmProvider 在 `Lazy` 报告层面复核同一现象。
//
// 为什么要有这一层：`ProbeElapsed` 的真正消费方是 `LoadReport`。两个 Lazy 共享
// 同一个 Provider（等价于"进程里 `theme.NewTheme()` 先跑过一次"），
// 第二个 Lazy 的后台加载会命中 Provider 缓存。
//
// **限制（明确记录，不当通过）**：`LoadElapsed` 由 `lazy.go` 里的真实
// `time.Now()` 得出，无法注入；本机粒度约 0.53 ms，它可能读成 0。
// 因此本用例**不对 `LoadElapsed` 做任何断言**，只记录，供后面比较时用。
// 可判定的对比是 `ProbeElapsed`（虚拟时钟 → 确定值）。
func TestAccept07LazyProbeElapsedWithWarmProvider(t *testing.T) {
	const step = 7 * time.Millisecond

	dir := t.TempDir()
	good := writeFont(t, dir, "accept07_lazy.otf", hanziFontBytes(t))

	clk := newAccept07Clock(step)
	emb := &failingLoader{err: errors.New("accept07：嵌入不可用")}
	reads := 0
	p := NewProvider(ProviderOptions{
		Candidates: []string{good},
		Timeout:    time.Second,
		Read: func(path string) ([]byte, error) {
			reads++
			return os.ReadFile(path)
		},
		Now:      clk.Next,
		Embedded: emb,
	})

	// ---------- 第一个 Lazy：Provider 冷（未命中） ----------
	lz1, err := NewLazy(LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("NewLazy#1 失败: %v", err)
	}
	lz1.Request()
	waitState(t, lz1, LoadReady, 5*time.Second)
	r1 := lz1.Report()
	e1 := r1.ProbeElapsed
	if e1 <= 0 {
		t.Fatalf("前提守卫：第一个 Lazy 的 ProbeElapsed=%v，期望 > 0（注入时钟未生效）", e1)
	}

	// ---------- 第二个 Lazy：Provider 已热（命中） ----------
	clk.Jump(1000 * time.Hour)
	clk.SetStep(0)
	scansBefore := p.Scans()
	readsBefore := reads

	lz2, err := NewLazy(LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("NewLazy#2 失败: %v", err)
	}
	lz2.Request()
	waitState(t, lz2, LoadReady, 5*time.Second)
	r2 := lz2.Report()
	e2 := r2.ProbeElapsed

	if got := p.Scans(); got != scansBefore {
		t.Errorf("第二个 Lazy 应命中 Provider 缓存，Scans 却从 %d 变成 %d", scansBefore, got)
	}
	if reads != readsBefore {
		t.Errorf("命中却重新读文件: reads %d → %d", readsBefore, reads)
	}

	// ---- 契约断言（`LoadReport.ProbeElapsed` 是**本轮**指标）----
	if !r1.Probe.ProbeRan {
		t.Errorf("Lazy#1 命中前 Probe.ProbeRan = false，期望 true（首个 Lazy 真的执行了探测）")
	}
	if r2.Probe.ProbeRan {
		t.Errorf("Lazy#2 命中缓存，Probe.ProbeRan = true，期望 false（本轮没有执行探测）")
	}
	if e2 != 0 {
		t.Errorf("Lazy#2 命中缓存，ProbeElapsed = %v，期望**恰为 0**（不得沿用历史耗时 %v）", e2, e1)
	}
	// 历史诊断必须保留（裁决 ②：不因命中而清空报告内容）。
	if r2.Probe == nil {
		t.Fatal("Lazy#2 的 Probe 报告为 nil——命中不应清空历史诊断")
	}
	if len(r2.Probe.Candidates) == 0 {
		t.Error("Lazy#2 的历史 Candidates 诊断丢失")
	}
	if r2.Probe.Resolved != r1.Probe.Resolved {
		t.Errorf("Lazy#2 历史 Resolved=%q，与 Lazy#1 的 %q 不一致", r2.Probe.Resolved, r1.Probe.Resolved)
	}

	t.Logf("=== D-ACCEPT-07 观测记录（LoadReport 层面，修复后）===")
	t.Logf("Lazy#1：ProbeElapsed=%v ProbeRan=%v（本轮执行了探测）", e1, r1.Probe.ProbeRan)
	t.Logf("Lazy#2：ProbeElapsed=%v ProbeRan=%v（命中缓存，本轮未探测）", e2, r2.Probe.ProbeRan)
	t.Logf("Lazy#2 Probe.Elapsed = %v（**保留历史值是契约要求**，与本轮 ProbeElapsed 不是一回事）", r2.Probe.Elapsed)
	t.Logf("Scans（两个 Lazy 之后）= %d（期望 1）", p.Scans())
	t.Logf("Lazy#2 LoadElapsed = %v（**真实墙钟**，本机粒度约 0.53 ms，读成 0 不代表公式错——本用例不断言它）", r2.LoadElapsed)
	t.Logf("历史诊断保留：Candidates %d 条、Resolved=%q", len(r2.Probe.Candidates), r2.Probe.Resolved)
	t.Logf("→ IMPL-3 §4.1/§5 的 LoadElapsed >= ProbeElapsed：本路径 ProbeElapsed 已为 0，" +
		"该不等式失去适用前提（本轮未探测），须按契约写明前提后才能使用")
}

// accept07Classify 按观测到的两个耗时判定类别。
//
// 判定之所以可行，是因为第二阶段用的是**冻结时钟**（步长 0）：
// 若命中时真的重新测量，结果只能是精确的 0；若为历史值，则必等于 e1。
func accept07Classify(e1, e2 time.Duration) accept07Observation {
	switch {
	case e2 == e1:
		return accept07Historical
	case e2 == 0:
		return accept07FreshZero
	default:
		return accept07Other
	}
}

// TestAccept07ProbeRanPerPath `ProbeRan` 在各条路径上的取值（D-ACCEPT-07 契约）。
//
// 覆盖：零候选早退 / 未知 family / 以及"`false` 不得反推缓存命中"的反例守卫。
func TestAccept07ProbeRanPerPath(t *testing.T) {
	t.Run("零候选早退也算执行过探测流程", func(t *testing.T) {
		clk := newAccept07Clock(7 * time.Millisecond)
		p := NewProvider(ProviderOptions{
			// 非 nil 的空表：`withDefaults` 只补 nil，空表得以保留。
			Candidates: []string{},
			Timeout:    time.Minute,
			Read:       os.ReadFile,
			Now:        clk.Next,
			Embedded:   &failingLoader{err: errors.New("accept07：嵌入不可用")},
		})

		_, rep, err := p.ResolveWithReport(FamilyCJK)
		if err == nil {
			t.Fatal("用例前提不成立：两条链路都应不可用")
		}
		if rep == nil {
			t.Fatal("失败路径也应返回报告，实际为 nil")
		}
		if rep.Scanned != 0 {
			t.Errorf("零候选路径 Scanned=%d，期望 0（这才是「零工作量」）", rep.Scanned)
		}
		if !rep.ProbeRan {
			t.Errorf("零候选早退 ProbeRan = false，期望 **true**——" +
				"它表示是否执行了**探测流程**，不是是否扫过候选")
		}
		t.Logf("零候选路径：Scanned=%d（零工作量），ProbeRan=%v（仍执行了流程），Elapsed=%v",
			rep.Scanned, rep.ProbeRan, rep.Elapsed)
	})

	t.Run("未知 family 不进入探测流程且返回 nil 报告", func(t *testing.T) {
		dir := t.TempDir()
		good := writeFont(t, dir, "accept07_unknown.otf", hanziFontBytes(t))
		p := NewProvider(ProviderOptions{
			Candidates: []string{good},
			Timeout:    time.Minute,
			Read:       os.ReadFile,
			Embedded:   &failingLoader{err: errors.New("accept07：嵌入不可用")},
		})

		id, rep, err := p.ResolveWithReport("accept07-not-a-family")
		if err == nil {
			t.Fatalf("未知 family 应当失败，实际返回 id=%q", id)
		}
		if !errors.Is(err, contracts.ErrFontNotFound) {
			t.Errorf("错误链应含 ErrFontNotFound，实际: %v", err)
		}
		if rep != nil {
			t.Errorf("未知 family 应返回 nil 报告（无 ProbeRan 可读），实际 %v", rep)
		}
		if got := p.Scans(); got != 0 {
			t.Errorf("未知 family 触发了探测: Scans=%d，期望 0", got)
		}
		t.Logf("未知 family：报告为 nil（因此**没有** ProbeRan 可判读），Scans=%d", p.Scans())
	})

	t.Run("ProbeRan==false 不得被反推为缓存命中（反例守卫）", func(t *testing.T) {
		// 反例：`theme.go:105` 在 `rep == nil` 时会合成同形态的报告，
		// 它的零值同样是 false，但与缓存毫无关系。
		// 这条守卫不是为了断言业务逻辑，而是钉住"零值 = false"这个前提：
		// 一旦有人把零值改成 true，"false ⇒ 缓存命中"的错误推断就会重新成立。
		synth := &ProbeReport{Family: FamilyCJK}
		if synth.ProbeRan {
			t.Error("合成报告的 ProbeRan 零值应为 false；若为 true，说明零值语义被改动，" +
				"'false ⇒ 缓存命中'的推断会重新出现")
		}
		t.Logf("反例成立：合成报告（与 theme.go:105 同形态）ProbeRan=%v，与缓存无关 "+
			"⇒ 不得仅凭 false 推断命中", synth.ProbeRan)
	})
}

// TestAccept07DerivedReportIsolation 派生报告与缓存中原始报告的隔离。
//
// 对应验收条件 7：改派生报告的标量状态不得改变缓存中的原始报告；
// 原有诊断字段也不得因为缓存命中而丢失。
func TestAccept07DerivedReportIsolation(t *testing.T) {
	dir := t.TempDir()
	good := writeFont(t, dir, "accept07_isolate.otf", hanziFontBytes(t))

	clk := newAccept07Clock(7 * time.Millisecond)
	p := NewProvider(ProviderOptions{
		Candidates: []string{good},
		Timeout:    time.Minute,
		Read:       os.ReadFile,
		Now:        clk.Next,
		Embedded:   &failingLoader{err: errors.New("accept07：嵌入不可用")},
	})

	_, origin, err := p.ResolveWithReport(FamilyCJK) // 未命中，建立缓存
	if err != nil {
		t.Fatalf("首次解析失败: %v", err)
	}
	wantElapsed := origin.Elapsed
	wantResolved := origin.Resolved
	wantPath := origin.Candidates[0].Path

	clk.SetStep(0)
	_, derived, err := p.ResolveWithReport(FamilyCJK) // 命中，拿到派生副本
	if err != nil {
		t.Fatalf("命中解析失败: %v", err)
	}
	if derived == origin {
		t.Fatal("命中返回的仍是原始对象——本用例的隔离前提不成立")
	}

	// 把派生副本改坏：标量 + 切片元素 + 本轮状态。
	derived.Elapsed = 999 * time.Millisecond
	derived.Resolved = "被测试改坏"
	derived.ProbeRan = true
	derived.Candidates[0].Path = "被测试改坏"

	// 再取一次：缓存中的原始报告必须纹丝不动。
	_, third, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("第三次解析失败: %v", err)
	}
	if third.Elapsed != wantElapsed {
		t.Errorf("改动派生报告污染了缓存中的 Elapsed: %v → %v", wantElapsed, third.Elapsed)
	}
	if third.Resolved != wantResolved {
		t.Errorf("改动派生报告污染了缓存中的 Resolved: %q → %q", wantResolved, third.Resolved)
	}
	if third.Candidates[0].Path != wantPath {
		t.Errorf("改动派生报告污染了缓存中的 Candidates: %q → %q", wantPath, third.Candidates[0].Path)
	}
	if third.ProbeRan {
		t.Errorf("第三次调用（命中）ProbeRan = true，期望 false——" +
			"派生报告的状态不应影响 Provider 内部对象")
	}
	if third == origin {
		t.Error("缓存命中交出了 Provider 内部对象，而不是派生副本")
	}

	t.Logf("隔离证据：改坏派生副本（Elapsed/Resolved/ProbeRan/Candidates[0]）后，")
	t.Logf("  第三次调用读到 Elapsed=%v Resolved=%q Candidates[0]=%q ProbeRan=%v",
		third.Elapsed, third.Resolved, third.Candidates[0].Path, third.ProbeRan)
	t.Logf("  原始值：      Elapsed=%v Resolved=%q Candidates[0]=%q",
		wantElapsed, wantResolved, wantPath)
}

// TestAccept07EmbeddedFallbackWritesCacheThenHit 嵌入兜底**成功**路径也会写缓存
// （`font.go:326` 有 `p.resolved[family] = embID`），因此必须单独取证。
//
// 为什么这条不能省：D 卡 §7 第 4 项与 B4 关注的是"系统命中时不解析嵌入"，
// 那是**成功**路径；而这里恰恰相反——**系统全失败**、靠嵌入兜底才成功。
// 两条路径都会写 `p.resolved`，却从未被单独验证过第二轮的读数。
func TestAccept07EmbeddedFallbackWritesCacheThenHit(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "accept07_absent2.ttf")
	clk := newAccept07Clock(7 * time.Millisecond)
	emb := &countingLoader{data: hanziFontBytes(t)}

	p := NewProvider(ProviderOptions{
		Candidates: []string{missing},
		Timeout:    time.Minute,
		Read:       os.ReadFile,
		Now:        clk.Next,
		Embedded:   emb,
	})

	// ---------- 阶段一：系统全失败 → 嵌入兜底成功 ----------
	id1, rep1, err1 := p.ResolveWithReport(FamilyCJK)
	if err1 != nil {
		t.Fatalf("兜底路径应当成功，实际失败: %v", err1)
	}
	if rep1.Embedded == nil || !rep1.Embedded.Attempted {
		t.Fatalf("前提不成立：兜底未被尝试（Embedded=%v）", rep1.Embedded)
	}
	if emb.calls != 1 {
		t.Errorf("嵌入加载次数 %d，期望 1", emb.calls)
	}
	scans1 := p.Scans()
	if scans1 != 1 {
		t.Errorf("首次应扫描 1 次，实际 %d", scans1)
	}
	e1 := rep1.Elapsed
	embE1 := rep1.Embedded.Elapsed
	if e1 <= 0 {
		t.Fatalf("前提守卫：兜底路径 probe 耗时=%v，期望 > 0（注入时钟未生效）", e1)
	}

	// ---------- 阶段二：冻结时钟后再取（应命中兜底写入的缓存） ----------
	clk.Jump(1000 * time.Hour)
	clk.SetStep(0)
	nowBefore := clk.Calls()

	id2, rep2, err2 := p.ResolveWithReport(FamilyCJK)
	if err2 != nil {
		t.Fatalf("第二次解析失败: %v", err2)
	}
	scans2 := p.Scans()
	e2 := rep2.Elapsed
	embE2 := rep2.Embedded.Elapsed

	if scans2 != scans1 {
		t.Errorf("兜底缓存命中却重新扫描: Scans %d → %d", scans1, scans2)
	}
	if emb.calls != 1 {
		t.Errorf("命中兜底缓存却再次加载嵌入: calls=%d，期望仍为 1", emb.calls)
	}
	if id2 != id1 {
		t.Errorf("兜底标识不稳定: %q → %q", id1, id2)
	}

	// ---- 契约断言：兜底成功后的缓存命中，语义须与系统命中后完全一致 ----
	if !rep1.ProbeRan {
		t.Errorf("兜底成功路径 ProbeRan = false，期望 true（本轮确实执行了探测流程）")
	}
	if rep2.ProbeRan {
		t.Errorf("兜底缓存命中路径 ProbeRan = true，期望 false（本轮没有执行探测）")
	}
	if rep2.Embedded == nil || !rep2.Embedded.Attempted {
		t.Error("命中返回的派生报告丢失了历史 Embedded 诊断")
	}

	obs := accept07Classify(e1, e2)
	t.Logf("=== D-ACCEPT-07 观测记录（嵌入兜底成功 → 缓存命中）===")
	t.Logf("probe 耗时：首轮 %v（ProbeRan=%v）→ 命中 %v（ProbeRan=%v）；类别 = %v",
		e1, rep1.ProbeRan, e2, rep2.ProbeRan, obs)
	t.Logf("Embedded.Elapsed：首轮 %v → 命中 %v（同样可能是历史值）", embE1, embE2)
	t.Logf("Scans = %d；嵌入加载次数 = %d；第二阶段 Now() 调用 %d 次",
		scans2, emb.calls, clk.Calls()-nowBefore)
	t.Logf("两次报告同一指针 = %v", rep1 == rep2)
	t.Logf("→ 兜底成功路径与系统命中路径写的是同一个 `p.resolved`，" +
		"命中行为需与 `TestAccept07ProbeElapsedOnProviderCacheHit` 的结论一致")
}

// TestAccept07RepeatedCallsAndFamilyScope 重复调用与 family 作用域取证。
//
// 关注两点：① 命中若干次都不得重新扫描；② 缓存是否按 family 分键
// （若共用一个 `p.last`，即使 `resolved` 分键也会串味，见下方限制）。
func TestAccept07RepeatedCallsAndFamilyScope(t *testing.T) {
	dir := t.TempDir()
	good := writeFont(t, dir, "accept07_repeat.otf", hanziFontBytes(t))

	clk := newAccept07Clock(7 * time.Millisecond)
	p := NewProvider(ProviderOptions{
		Candidates: []string{good},
		Timeout:    time.Minute,
		Read:       os.ReadFile,
		Now:        clk.Next,
		Embedded:   &failingLoader{err: errors.New("accept07：嵌入不可用")},
	})

	// ---------- ① 连续五次调用 ----------
	var first time.Duration
	for i := 1; i <= 5; i++ {
		_, rep, err := p.ResolveWithReport(FamilyCJK)
		if err != nil {
			t.Fatalf("第 %d 次解析失败: %v", i, err)
		}
		if i == 1 {
			first = rep.Elapsed
			if first <= 0 {
				t.Fatalf("前提守卫：首轮 Elapsed=%v，期望 > 0", first)
			}
			if !rep.ProbeRan {
				t.Errorf("第 1 次（未命中）ProbeRan = false，期望 true")
			}
			continue
		}
		if got := rep.Elapsed; got != first {
			t.Errorf("第 %d 次调用 Elapsed=%v，与首轮 %v 不同（缓存却返回了新值）", i, got, first)
		}
		if rep.ProbeRan {
			t.Errorf("第 %d 次（缓存命中）ProbeRan = true，期望 false", i)
		}
		if got := p.Scans(); got != 1 {
			t.Errorf("第 %d 次调用后 Scans=%d，期望恒为 1", i, got)
		}
	}
	t.Logf("连续 5 次调用：Scans 恒为 %d，Elapsed 恒为 %v，第 2~5 次 ProbeRan 恒为 false",
		p.Scans(), first)

	// ---------- ② 未实现的 family 不得触发探测、不得写缓存 ----------
	scansBefore := p.Scans()
	for _, fam := range []string{FamilyBody, FamilyMono} {
		id, rep, err := p.ResolveWithReport(fam)
		if err == nil {
			t.Errorf("%s 应当严格失败，却成功了（id=%q）", fam, id)
		}
		if !errors.Is(err, contracts.ErrFontNotFound) {
			t.Errorf("%s 的错误链应含 ErrFontNotFound，实际: %v", fam, err)
		}
		if rep != nil {
			t.Errorf("%s 失败时应返回 nil 报告，实际非 nil", fam)
		}
	}
	if got := p.Scans(); got != scansBefore {
		t.Errorf("未实现的 family 触发了探测: Scans %d → %d", scansBefore, got)
	}

	// 未实现 family 失败后，CJK 的缓存仍必须可用且不重新扫描。
	_, repAfter, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("穿插未实现 family 后 CJK 解析失败: %v", err)
	}
	if got := p.Scans(); got != scansBefore {
		t.Errorf("穿插调用后 CJK 重新扫描: Scans %d → %d", scansBefore, got)
	}
	t.Logf("穿插 %s/%s 之后：Scans=%d（未增加），CJK Elapsed=%v（仍为缓存值）",
		FamilyBody, FamilyMono, p.Scans(), repAfter.Elapsed)

	// **限制（明确记录）**：A 阶段只有 FamilyCJK 实现了探测，因此无法用
	// "两个都已实现的 family" 端到端验证跨 family 缓存是否串味。
	// 静态阅读显示 `Provider.last` 是**单一字段、不按 family 分键**，
	// 若将来 FamilyBody/FamilyMono 实现探测，另一族的命中可能会返回
	// CJK 那份历史报告。本轮**未验证**，登记为 D-ACCEPT-07 的后续复核项。
	t.Logf("限制：本轮无法端到端验证跨 family 串味（仅 CJK 实现探测）；" +
		"`Provider.last` 不按 family 分键，属静态阅读所得，未实证")
}

// TestAccept07FailurePathIsNotCached 界定**受影响范围**：失败路径会不会也返回历史值。
//
// 为什么必须划这一刀：决定 D-ACCEPT-07 的语义口径时，需要知道"历史值"现象是
// 覆盖**全部**命中缓存的路径，还是只在**成功**缓存上出现。
// 若失败路径每次都真的重跑探测，那么它的 `ProbeElapsed` 始终是本轮实测，
// 语义问题就不必牵动失败分支。
//
// 判据：`Scans()` 若 +1，说明本轮确实跑了 `probe()`；配合把步长放大，
// 本轮实测值必须随之变大——两者互为印证。
func TestAccept07FailurePathIsNotCached(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "accept07_absent.ttf")

	clk := newAccept07Clock(7 * time.Millisecond)
	p := NewProvider(ProviderOptions{
		Candidates: []string{missing},
		// Timeout 放宽远超虚拟步长，避免 deadline 逻辑干扰 Now 调用次数。
		Timeout:  time.Minute,
		Read:     os.ReadFile,
		Now:      clk.Next,
		Embedded: &failingLoader{err: errors.New("accept07：嵌入不可用")},
	})

	_, rep1, err1 := p.ResolveWithReport(FamilyCJK)
	if err1 == nil {
		t.Fatal("用例前提不成立：本用例要求两条链路都失败")
	}
	if !errors.Is(err1, contracts.ErrFontNotFound) {
		t.Errorf("错误链应含 ErrFontNotFound，实际: %v", err1)
	}
	scans1 := p.Scans()
	e1 := rep1.Elapsed
	if e1 <= 0 {
		t.Fatalf("前提守卫：失败路径 Elapsed=%v，期望 > 0（注入时钟未生效）", e1)
	}

	// 步长放大 10 倍：若第二轮真的重跑探测，实测值必然随之变大。
	clk.SetStep(70 * time.Millisecond)

	_, rep2, err2 := p.ResolveWithReport(FamilyCJK)
	if err2 == nil {
		t.Fatal("第二次解析应当同样失败，却成功了")
	}
	scans2 := p.Scans()
	e2 := rep2.Elapsed

	if scans2 != scans1+1 {
		t.Errorf("失败路径应当每次重跑探测：Scans %d → %d，期望 +1。"+
			"若未增加，说明失败结果被缓存了", scans1, scans2)
	}
	if e2 <= e1 {
		t.Errorf("失败路径第二轮 Elapsed=%v 未随放大后的时钟变大（首轮 %v）——"+
			"它不是本轮实测", e2, e1)
	}
	// 契约断言：失败路径每次都真的执行了探测流程。
	if !rep1.ProbeRan {
		t.Errorf("失败路径首轮 ProbeRan = false，期望 true")
	}
	if !rep2.ProbeRan {
		t.Errorf("失败重试路径 ProbeRan = false，期望 true（每轮都真的跑了探测）")
	}
	if rep1 == rep2 {
		t.Error("失败重试返回了同一个报告对象，期望每轮都是新报告")
	}

	t.Logf("=== D-ACCEPT-07 观测记录（失败路径）===")
	t.Logf("Scans：首次后 %d → 第二次后 %d", scans1, scans2)
	t.Logf("首轮 Elapsed=%v（步长 7ms），第二轮 Elapsed=%v（步长 70ms），比值=%.1fx",
		e1, e2, float64(e2)/float64(e1))
	t.Logf("两次报告同一指针 = %v（false 才说明每次都是新的报告对象）", rep1 == rep2)
	t.Logf("→ 结论：失败路径%s", map[bool]string{true: "每次重跑探测，ProbeElapsed 是本轮实测；受影响范围仅成功缓存。",
		false: "也被缓存，受影响范围需重新界定。"}[scans2 == scans1+1])
}
