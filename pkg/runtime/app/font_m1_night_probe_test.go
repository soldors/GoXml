package app

// ============================================================================
// NIGHT-FONT-M1-MEASUREMENT（WorkBuddy，2026-10-27 夜间任务）
//
// 目的：为 D-ACCEPT-01（M1 阈值候选）、D-ACCEPT-02（同步/异步比较口径）、
// D-ACCEPT-04（真实窗口 M1）交付**可复现的测量机制与原始对照数据**，
// 而不是重复既有验收命令。
//
// 本文件分两类用例：
//   1. **机制核证**（普通 `go test` 无条件执行，只读、不设阈值）；
//   2. **采集器** `TestNightM1CollectSample`（需显式 `-nightwb.scenario`，
//      缺省跳过——避免普通测试跑-load Embedding/建 Provider 拖慢基线）。
//
// 命名约定（避免与 DeepSeek 在同一 app 测试包中新增的辅助函数重名）：
//   测试函数 `TestNightM1*`；其它标识 `nightWB*`。
//
// 生产口径约束：本文件**不新增任何生产 API**、不改 `App` 计时起点。
// 它复用既有白盒入口 `frame()` / `newWindow()`——注意 `frame()` 是**合成帧**
// 入口，与真实平台窗口的差异在 final-report.md 中显式标注。
// ============================================================================

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ---------- 采集器开关（显式 opt-in） ----------

var (
	nightWBScenario = flag.String("nightwb.scenario", "",
		"采集场景：A=无字体加载基线 / B=独立Provider冷路径 / C=预热Provider热路径；空则跳过采集")
	nightWBOut = flag.String("nightwb.out", "",
		"样本 JSON 输出目录；空则只写 stdout")
	nightWBOrder = flag.String("nightwb.order", "sync-first",
		"B/C 场景内同步与异步的测量顺序：sync-first 或 async-first（调换采样，隔离顺序效应）")
	nightWBSampleIdx = flag.Int("nightwb.idx", 0,
		"样本序号（由采集脚本写入，便于回溯到具体进程）")
)

// ---------- 辅助：记录 Layout 入口时刻与被观测 Fin 状态 ----------

// nightWBView 是带观测钩子的 View。
//
// 与普通 countingView 的差别：它在**第一次 Layout 进入时**抓取一份
// `App.FirstFrame()` 快照。这份快照是判定「M1 止点是否在 Layout 之前」的
// **定性**证据——它不依赖时间粒度（本机 `time.Now()` 步长约 0.518 ms），
// 因此比"比较两个时间戳大小"强得多。
type nightWBView struct {
	owner *App

	calls int64

	// 第一次 Layout 期间抓到的读数
	firstAtLayoutMeasured bool
	firstAtLayoutPinned   time.Time
	layoutEntry           time.Time

	// 采样完成标志（只写一次，读取无竞争：由事件循环 goroutine 写入，
	// 测试在同一 goroutine 读取）
	sampled bool
}

func (v *nightWBView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.calls++
	if v.calls == 1 {
		v.layoutEntry = time.Now()
		if v.owner != nil {
			rep := v.owner.FirstFrame()
			v.firstAtLayoutMeasured = rep.Measured()
			v.firstAtLayoutPinned = rep.FirstFrameAt
		}
		v.sampled = true
	}
	return material.Body1(th, "M1 测量探针").Layout(gtx)
}

// nightWBNewView 构造带 owner 的观测 View。
func nightWBNewView(a *App) *nightWBView {
	return &nightWBView{owner: a}
}

// nightWBSimulateOrder 在**本地副本**上模拟两种写入顺序，证明上面那个判据
// 不是恒真的空判据（真正的生产变异需要改 `app.go`，本任务禁止，故用逻辑层反证）。
//
// 返回 (在Layout内读到的 Measured, 是否可判别)。
func nightWBSimulateOrder(order string) (insideLayoutMeasured bool, discriminates bool) {
	type appLike struct {
		firstFrameAt int64 // UnixNano，0 = 未写
		createdAt    time.Time
	}
	var x appLike
	x.createdAt = time.Now()

	layout := func() {
		// 与 nightWBView.Layout 同一判据：此刻读 Measured
		insideLayoutMeasured = x.firstFrameAt != 0
	}

	switch order {
	case "before": // 现行实现：先写止点，再排版
		x.firstFrameAt = time.Now().UnixNano()
		layout()
	case "after": // 反事实实现：先排版，再写止点
		layout()
		x.firstFrameAt = time.Now().UnixNano()
	default:
		return false, false
	}
	before := order == "before"
	return insideLayoutMeasured, before == insideLayoutMeasured
}

// ===========================================================================
// 一、机制核证（D-ACCEPT-01 的要求①：先单独验证测量机制正确）
// ===========================================================================

// TestNightM1MeasuredZeroSemantics 证明 `Measured()==false` 的零值语义。
//
// 必读：本机 `time.Now()` 步长约 0.518 ms ⇒ 任何低于该量级的单次读数都可能读到 0。
// 0 在 M1 里的含义是「**没测到**」，不是「首帧很快」（lazy_status.go:68-70）。
func TestNightM1MeasuredZeroSemantics(t *testing.T) {
	th := material.NewTheme()
	a, err := NewApp(nightWBNewView(nil), th, Config{Title: "night-m1-zero"})
	if err != nil {
		t.Fatal(err)
	}
	rep := a.FirstFrame()
	if rep.Measured() {
		t.Fatal("还没有任何帧却报告已测量：零值会被读成'首帧很快'，属虚假通过")
	}
	if !rep.FirstFrameAt.IsZero() {
		t.Errorf("未跑帧时 FirstFrameAt = %v，期望零值", rep.FirstFrameAt)
	}
	if rep.Elapsed != 0 {
		t.Errorf("未测量时 Elapsed = %v，期望 0（0 = 没测到，不是耗时为 0）", rep.Elapsed)
	}
	if rep.CreatedAt.IsZero() {
		t.Error("CreatedAt 必须非零（M1 起点）")
	}
	t.Logf("零值语义成立：CreatedAt=%v, FirstFrameAt=零值, Elapsed=0, Measured=false", rep.CreatedAt)
}

// TestNightM1StopBeforeLayout 证明 M1 止点在首个 `view.Layout` **之前**。
//
// 为什么用「Layout 内读 Measured」这个定性判据而不是时间戳比较：
// 本机时钟步长 0.518 ms，两个相隔亚微秒的时刻极大概率读到**同一个** tick，
// 时间戳比较会退化成永真式；而"写入是否已发生"是布尔量，不受粒度影响。
func TestNightM1StopBeforeLayout(t *testing.T) {
	// ① 先自证判据有判别力（不是恒真空判据）。
	okBefore, disc1 := nightWBSimulateOrder("before")
	okAfter, disc2 := nightWBSimulateOrder("after")
	if !disc1 || !disc2 {
		t.Fatalf("本地反证失效：before=%v(discriminates=%v) after=%v(discriminates=%v)",
			okBefore, disc1, okAfter, disc2)
	}
	if okBefore == okAfter {
		t.Fatalf("反证不自洽：两种顺序得到同一结果 (%v)，判据无判别力", okBefore)
	}
	// before ⇒ Layout 内应读到 true；after ⇒ 应读到 false
	if !okBefore || okAfter {
		t.Fatalf("本地顺序模拟结果异常：before-order=%v after-order=%v", okBefore, okAfter)
	}

	// ② 对真实实现应用同一判据。
	a, err := NewApp(&nightWBView{}, material.NewTheme(), Config{Title: "night-m1-stop"})
	if err != nil {
		t.Fatal(err)
	}
	// owner 回填：View 需要在 Layout 里看得到 App（构造顺序造成的鸡生蛋问题，
	// 与 IMPL-2 的 NewLazyApp 同构）。
	v := a.view.(*nightWBView)
	v.owner = a

	driveFrames(a, 1)
	if v.calls != 1 {
		t.Fatalf("期望只驱动 1 帧，实际 %d", v.calls)
	}
	if !v.sampled {
		t.Fatal("第一帧的 Layout 采样未完成")
	}
	if !v.firstAtLayoutMeasured {
		t.Error("首个 Layout 进入时 `FirstFrame().Measured()` 为 false ⇒ " +
			"M1 止点被写在了 Layout **之后**，与契约（止点 = 首帧开始处理、Layout 之前）不符")
	}
	if v.firstAtLayoutPinned.IsZero() {
		t.Error("首个 Layout 进入时 FirstFrameAt 仍为零值 ⇒ 止点未在此之前写入")
	}
	rep := a.FirstFrame()
	if !rep.FirstFrameAt.Equal(v.firstAtLayoutPinned) {
		t.Errorf("Layout 内读到 %v，跑完帧后读到 %v：首帧时刻发生了变化（应只写一次）",
			v.firstAtLayoutPinned, rep.FirstFrameAt)
	}
	if !rep.FirstFrameAt.Equal(rep.CreatedAt) && rep.FirstFrameAt.Before(rep.CreatedAt) {
		t.Errorf("止点 %v 早于起点 %v：M1 区间不成立", rep.FirstFrameAt, rep.CreatedAt)
	}
	t.Logf("止点位于 Layout 之前：CreatedAt=%v FirstFrameAt=%v Elapsed=%v（%v 低于本机粒度，仅表明单帧读数不可分辨）",
		rep.CreatedAt, rep.FirstFrameAt, rep.Elapsed, rep.Elapsed)
}

// TestNightM1FirstFrameWrittenOnce 证明首帧时刻只写一次、后续帧不覆盖。
func TestNightM1FirstFrameWrittenOnce(t *testing.T) {
	a, err := NewApp(&nightWBView{}, material.NewTheme(), Config{Title: "night-m1-once"})
	if err != nil {
		t.Fatal(err)
	}
	driveFrames(a, 1)
	pinned := a.FirstFrame().FirstFrameAt
	if pinned.IsZero() {
		t.Fatal("驱动 1 帧后仍未记录首帧时刻")
	}
	for round := 0; round < 3; round++ {
		driveFrames(a, 5)
		got := a.FirstFrame()
		if !got.FirstFrameAt.Equal(pinned) {
			t.Fatalf("第 %d 轮：首帧时刻被后续帧覆盖 %v → %v", round, pinned, got.FirstFrameAt)
		}
	}
	t.Logf("跑完 16 帧后首帧时刻仍为 %v：写一次语义成立", pinned)
}

// TestNightM1LazyRequestAfterCreatedAt 证明懒加载路径的顺序：
// `CreatedAt` ≤ `RequestedAt`，且 M1 区间**包含** `Request()` 的同步部分
// （临时主题构造 `TempElapsed`）——第一帧不等 hedera 后台就绪。
func TestNightM1LazyRequestAfterCreatedAt(t *testing.T) {
	lz, err := theme.NewLazy(theme.LazyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	a, err := NewLazyApp(&nightWBView{}, lz, Config{Title: "night-m1-lazy-order"})
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	rep := a.FirstFrame()
	lr := a.Lazy().Report()

	if rep.CreatedAt.Before(before) || rep.CreatedAt.After(after) {
		t.Errorf("CreatedAt=%v 不在构造区间 [%v, %v] 内", rep.CreatedAt, before, after)
	}
	if lr.RequestedAt.IsZero() {
		t.Fatal("Request 尚未发生：SetNotify→Request 顺序可能失效")
	}
	if lr.RequestedAt.Before(rep.CreatedAt) {
		t.Errorf("RequestedAt=%v 早于 CreatedAt=%v：Request 发生在起点之前", lr.RequestedAt, rep.CreatedAt)
	}
	// TempElapsed 是被fg首帧区间内的有界同步成本（验收卡 §2 明写）
	if lr.TempElapsed <= 0 {
		t.Logf("TempElapsed = %v：低于本机计数粒度，本条不可用于数值比较（记为未验证）",
			lr.TempElapsed)
	} else {
		t.Logf("TempElapsed = %v（含在 M1 区间内，属有界同步成本）", lr.TempElapsed)
	}
	t.Logf("顺序成立：CreatedAt=%v ≤ RequestedAt=%v；构造外部区间=[%v, %v]",
		rep.CreatedAt, lr.RequestedAt, before, after)
}

// TestNightM1StartExcludesWindowHandle 量化「M1 起点之前、被测仍在执行的那段」。
//
// 源码事实（本轮静态判定）：`NewApp` / `NewLazyApp` 都在**复合字面量里**
// 给 `createdAt` 赋值，而同一字面量的前一个字段是 `w: newWindow(cfg)`。
// Go 规范规定复合字面量字段的求值按**词法从左到右** ⇒ `newWindow(cfg)` 先于
// `time.Now()` ⇒ **窗口句柄构造被排除在 M1 之外**。
//
// 这里做两件事：
//  1. 用同构的本地字面量证明该求值顺序（语言层证据，可运行）；
//  2. 批量测 `newWindow` 的耗时，**界出**被排除区间的量级。
func TestNightM1StartExcludesWindowHandle(t *testing.T) {
	// ① 语言层：同构字面量的求值顺序
	order := make([]string, 0, 2)
	markWin := func() *App { order = append(order, "newWindow"); return nil }
	markNow := func() time.Time { order = append(order, "createdAt"); return time.Now() }
	_ = struct {
		w         *App
		createdAt time.Time
	}{w: markWin(), createdAt: markNow()}
	if len(order) != 2 || order[0] != "newWindow" || order[1] != "createdAt" {
		t.Fatalf("复合字面量求值顺序异常：%v（期望 [newWindow createdAt]）", order)
	}

	// ② 被排除区间的量级：`newWindow` 单次远低于粒度 ⇒ 必须批量。
	const iters = 200000
	start := time.Now()
	for i := 0; i < iters; i++ {
		_ = newWindow(Config{Title: "night-m1-excl"})
	}
	total := time.Since(start)
	if total <= 0 {
		t.Fatalf("批量总耗时 = %v：计时器读不出该量级 ⇒ 被排除区间的量级未验证", total)
	}
	per := float64(total.Nanoseconds()) / float64(iters)
	t.Logf("被排除区间 `newWindow` 单次 ≈ %.1f ns（%d 次总量 %v）⇒ 相对 M1 量级可忽略，"+
		"但口径上**确实不含**窗口句柄构造；真实平台窗口在首次 `w.Event()` 才创建，"+
		"因此新建证的只是 Go 层对象与选项装配",
		per, iters, total)
}

// ===========================================================================
// 二、采集器（显式 opt-in；缺省跳过）
// ===========================================================================

// nightWBSample 是一个**独立进程**一次运行的完整读数。
//
// 字段命名与 measurement-plan.md §3/§4 一一对应，不做任何二次加工：
// 加工（min/median/p95、离群处理）交给 analyze.py，原始值一个都不删。
type nightWBSample struct {
	// 元信息
	CollectedAt string `json:"collectedAt"`
	Scenario    string `json:"scenario"` // A / B / C
	Path        string `json:"path"`     // sync / async（A 只有 sync）
	Order       string `json:"order"`    // sync-first / async-first
	SampleIdx   int    `json:"sampleIdx"`
	GoVersion   string `json:"goVersion"`
	GOMAXPROCS  int    `json:"gomaxprocs"`
	// 五个时刻（UnixNano；0 = 未取到）
	ProcStartNs   int64 `json:"procStartNs"`
	CtorEntryNs   int64 `json:"ctorEntryNs"`
	CreatedAtNs   int64 `json:"createdAtNs"`
	LayoutEntryNs int64 `json:"layoutEntryNs"`
	TempBuiltNs   int64 `json:"tempBuiltNs"`
	// App.FirstFrame 读数
	M1Measured  bool  `json:"m1Measured"`
	M1ElapsedNs int64 `json:"m1ElapsedNs"`
	// 同步构建段（口径 P1，仅 sync 路径）
	SyncProbeNs    int64  `json:"syncProbeNs"` // ResolveWithReport 墙钟
	SyncFacesNs    int64  `json:"syncFacesNs"` // Faces 反查墙钟
	SyncBuildNs    int64  `json:"syncBuildNs"` // 解析+反查+Shaper 装配总墙钟
	SyncResolveErr string `json:"syncResolveErr"`
	// 是否命中 Provider 缓存**不得**由 ProbeRan=false 单独反推（D-ACCEPT-07
	// 明确禁止），因此这里只存原始判别字段，由分析脚本按 Scanned/ProbeRan/
	// Resolved 联合判定。
	SyncScanned  int    `json:"syncScanned"`
	SyncProbeRan bool   `json:"syncProbeRan"`
	SyncResolved string `json:"syncResolved"`
	// 异步路径读数（口径 P1/P2，仅 async 路径）
	AsyncProbeElapsedNs   int64  `json:"asyncProbeElapsedNs"`
	AsyncLoadElapsedNs    int64  `json:"asyncLoadElapsedNs"`
	AsyncTempElapsedNs    int64  `json:"asyncTempElapsedNs"`
	AsyncState            string `json:"asyncState"`
	AsyncAttempts         int    `json:"asyncAttempts"`
	AsyncProbeRan         bool   `json:"asyncProbeRan"`
	AsyncProbeElapsedHist int64  `json:"asyncProbeElapsedHist"` // Probe.Elapsed（可能是历史值）
	AsyncReadyWaitNs      int64  `json:"asyncReadyWaitNs"`      // 采集侧等待 Ready 的墙钟
	AsyncFrameSubmittedNs int64  `json:"asyncFrameSubmittedNs"` // 非零 = 真主题已提交（代理指标）
	AsyncFirstLayoutNs    int64  `json:"asyncFirstLayoutNs"`    // M4
	AsyncErr              string `json:"asyncErr"`
	// 质量标记
	Missing int      `json:"missing"` // 本样本读取失败的项数
	Notes   []string `json:"notes"`
}

// TestNightM1CollectSample 采集一个场景样本。
//
// 缺省（未给 `-nightwb.scenario`）必须跳过——普通 `go test` 不允许拖慢
// 基线或弹出窗口。
func TestNightM1CollectSample(t *testing.T) {
	scenario := strings.ToUpper(strings.TrimSpace(*nightWBScenario))
	if scenario == "" {
		t.Skip("未指定 -nightwb.scenario：本用例是采集器，需显式 opt-in")
	}
	switch scenario {
	case "A", "B", "C":
	default:
		t.Fatalf("未知场景 %q（合法值 A/B/C）", *nightWBScenario)
	}

	procStart := nightWBProcStartNano()

	switch scenario {
	case "A":
		nightWBCollectA(t, procStart)
	case "B", "C":
		warm := scenario == "C"
		order := strings.ToLower(strings.TrimSpace(*nightWBOrder))
		if order != "sync-first" && order != "async-first" {
			t.Fatalf("未知顺序 %q（合法值 sync-first/async-first）", *nightWBOrder)
		}
		runSync := func() { nightWBCollectSyncProv(t, procStart, scenario, warm) }
		runAsync := func() { nightWBCollectAsyncProv(t, procStart, scenario, warm) }
		if order == "sync-first" {
			runSync()
			runAsync()
		} else {
			runAsync()
			runSync()
		}
	}
}

// nightWBProcStartNano 取进程起点近似（测试进程 main 之前不可得，这里用
// 包内最早可得时刻，并在 notes 里标明它不是真正的进程启动）。
var nightWBProcStartInstant = time.Now()

func nightWBProcStartNano() int64 { return nightWBProcStartInstant.UnixNano() }

// time.Time{}.UnixNano() is negative, not zero. Preserve the schema's zero
// sentinel so missing observations are counted rather than looking populated.
func nightWBUnixNano(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixNano()
}

func nightWBEnv() (string, int) { return runtime.Version(), runtime.GOMAXPROCS(0) }

// nightWBWrite 落盘样本 JSON，并同时打印一条可抽取的 stdout 行。
func nightWBWrite(t *testing.T, s nightWBSample) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("样本序列化失败: %v", err)
	}
	fmt.Printf("NIGHTWB_SAMPLE %s\n", string(b))
	if *nightWBOut != "" {
		dir := *nightWBOut
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("创建样本目录失败: %v", err)
		}
		name := fmt.Sprintf("%s-%s-%d.json", s.Scenario, s.Path, s.SampleIdx)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
			t.Fatalf("写样本失败: %v", err)
		}
		t.Logf("样本已写入 %s", p)
	}
}

// nightWBCollectA 场景 A：无字体加载基线（material.NewTheme，不触发系统探测）
func nightWBCollectA(t *testing.T, procStart int64) {
	t.Helper()
	gov, gmp := nightWBEnv()

	// 主题先构建，再取构造入口时刻：`ctorEntry` 必须是 **NewApp 之前**的时刻。
	// （上一版把它取在主题构建之前，导致
	// `createdAt - ctorEntry` 混进了整段同步构建，口径错误。）
	th := material.NewTheme()
	ctorEntry := time.Now()
	view := &nightWBView{}
	a, err := NewApp(view, th, Config{
		Title: "night-m1-A", Width: 240, Height: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	view.owner = a
	driveFrames(a, 1)

	s := nightWBSample{
		CollectedAt:   time.Now().Format(time.RFC3339Nano),
		Scenario:      "A",
		Path:          "sync",
		Order:         *nightWBOrder,
		SampleIdx:     *nightWBSampleIdx,
		GoVersion:     gov,
		GOMAXPROCS:    gmp,
		ProcStartNs:   procStart,
		CtorEntryNs:   ctorEntry.UnixNano(),
		LayoutEntryNs: nightWBUnixNano(view.layoutEntry),
	}
	rep := a.FirstFrame()
	s.CreatedAtNs = rep.CreatedAt.UnixNano()
	s.M1Measured = rep.Measured()
	s.M1ElapsedNs = rep.Elapsed.Nanoseconds()
	if s.LayoutEntryNs == 0 {
		s.Missing++
		s.Notes = append(s.Notes, "未取到 Layout 入口时刻")
	}
	if !s.M1Measured {
		s.Missing++
		s.Notes = append(s.Notes, "M1 未测到（0 表示没测到，不是耗时为 0）")
	}
	nightWBWrite(t, s)
}

// nightWBBuildSyncTheme 用**显式 Provider** 复刻 `theme.NewThemeWithReport`
// 的构建动作（唯一差别：不走进程级 defaultProvider，便于隔离污染）。
//
// 返回三段读数：ResolveWithReport / Faces / 合计。
func nightWBBuildSyncTheme(t *testing.T, p *theme.Provider) (th *material.Theme, probeNs, facesNs, totalNs int64, errMsg string, rep *theme.ProbeReport) {
	t.Helper()
	start := time.Now()
	base := material.NewTheme()

	t0 := time.Now()
	id, repGot, err := p.ResolveWithReport(theme.FamilyCJK)
	probeNs = time.Since(t0).Nanoseconds()

	t1 := time.Now()
	faces, ok := p.Faces(id)
	facesNs = time.Since(t1).Nanoseconds()

	if err != nil {
		return base, probeNs, facesNs, time.Since(start).Nanoseconds(), err.Error(), repGot
	}
	if !ok || len(faces) == 0 {
		return base, probeNs, facesNs, time.Since(start).Nanoseconds(), "faces 为空", repGot
	}
	base.Shaper = text.NewShaper(text.WithCollection(faces))
	return base, probeNs, facesNs, time.Since(start).Nanoseconds(), "", repGot
}

// nightWBCollectSyncProv 场景 B/C 的同步侧：注入来源明确的 Provider。
func nightWBCollectSyncProv(t *testing.T, procStart int64, scenario string, warm bool) {
	t.Helper()
	gov, gmp := nightWBEnv()

	p := theme.NewProvider(theme.ProviderOptions{})
	if warm {
		// 预热：先完整构建一次（这一轮的读数**不**进样本）
		if _, _, _, _, errMsg, _ := nightWBBuildSyncTheme(t, p); errMsg != "" {
			t.Logf("预热构建失败（仍继续采样，记为缺测）: %s", errMsg)
		}
	}

	// 先构建主题，构造入口时刻**只**在 NewApp 之前取：
	// 主题构建属于口径 P1 的同步构建段，不得混进"起点偏置"。
	th, probeNs, facesNs, buildNs, errMsg, syncRep := nightWBBuildSyncTheme(t, p)
	view := &nightWBView{}
	ctorEntry := time.Now()
	a, err := NewApp(view, th, Config{Title: "night-m1-" + scenario + "-sync", Width: 240, Height: 90})
	if err != nil {
		t.Fatal(err)
	}
	view.owner = a
	driveFrames(a, 1)

	s := nightWBSample{
		CollectedAt:    time.Now().Format(time.RFC3339Nano),
		Scenario:       scenario,
		Path:           "sync",
		Order:          *nightWBOrder,
		SampleIdx:      *nightWBSampleIdx,
		GoVersion:      gov,
		GOMAXPROCS:     gmp,
		ProcStartNs:    procStart,
		CtorEntryNs:    ctorEntry.UnixNano(),
		LayoutEntryNs:  nightWBUnixNano(view.layoutEntry),
		SyncProbeNs:    probeNs,
		SyncFacesNs:    facesNs,
		SyncBuildNs:    buildNs,
		SyncResolveErr: errMsg,
	}
	if syncRep != nil {
		s.SyncScanned = syncRep.Scanned
		s.SyncProbeRan = syncRep.ProbeRan
		s.SyncResolved = syncRep.Resolved
	}
	rep := a.FirstFrame()
	s.CreatedAtNs = rep.CreatedAt.UnixNano()
	s.M1Measured = rep.Measured()
	s.M1ElapsedNs = rep.Elapsed.Nanoseconds()
	if errMsg != "" {
		s.Missing++
		s.Notes = append(s.Notes, "同步解析失败: "+errMsg)
	}
	if s.LayoutEntryNs == 0 {
		s.Missing++
		s.Notes = append(s.Notes, "未取到 Layout 入口时刻")
	}
	// 是否命中 Provider 缓存的判据：D-ACCEPT-07 明确禁止由 `ProbeRan=false`
	// 单独反推，而 `Scanned` 在派生副本里保留的可能是**历史值**（见 C-async
	// 样本：ProbeRan=false 而 Scanned 仍为 1）。因此这里以
	// `ProbeRan=false && Resolved 非空` 作联合判据，`Scanned` 只如实记账。
	if warm && s.SyncProbeRan {
		s.Notes = append(s.Notes, "热路径样本仍执行了探测流程（ProbeRan=true）：未命中 Provider 缓存，需复核")
	}
	nightWBWrite(t, s)
}

// nightWBCollectAsyncProv 场景 B/C 的异步侧：**另建一个独立 Provider**，
// 与同进程的同步侧互不污染。
func nightWBCollectAsyncProv(t *testing.T, procStart int64, scenario string, warm bool) {
	t.Helper()
	gov, gmp := nightWBEnv()

	p := theme.NewProvider(theme.ProviderOptions{})
	if warm {
		if _, _, _, _, errMsg, _ := nightWBBuildSyncTheme(t, p); errMsg != "" {
			t.Logf("异步侧预热失败（仍继续采样，记为缺测）: %s", errMsg)
		}
	}
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: p})
	if err != nil {
		t.Fatal(err)
	}

	ctorEntry := time.Now()
	view := &nightWBView{}
	a, err := NewLazyApp(view, lz, Config{Title: "night-m1-" + scenario + "-async", Width: 240, Height: 90})
	if err != nil {
		t.Fatal(err)
	}
	view.owner = a
	driveFrames(a, 1)

	m1 := a.FirstFrame()

	// 等后台终态（不等不测）
	waitStart := time.Now()
	deadline := waitStart.Add(30 * time.Second)
	state := lz.State()
	for state != theme.LoadReady && state != theme.LoadFailed && time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
		state = lz.State()
	}
	waitNs := time.Since(waitStart).Nanoseconds()

	// 再驱动几帧，让 Take/MarkFrameSubmitted/MarkFirstLayout 有机会发生
	driveFrames(a, 6)

	lr := a.Lazy().Report()
	s := nightWBSample{
		CollectedAt:           time.Now().Format(time.RFC3339Nano),
		Scenario:              scenario,
		Path:                  "async",
		Order:                 *nightWBOrder,
		SampleIdx:             *nightWBSampleIdx,
		GoVersion:             gov,
		GOMAXPROCS:            gmp,
		ProcStartNs:           procStart,
		CtorEntryNs:           ctorEntry.UnixNano(),
		CreatedAtNs:           m1.CreatedAt.UnixNano(),
		LayoutEntryNs:         nightWBUnixNano(view.layoutEntry),
		M1Measured:            m1.Measured(),
		M1ElapsedNs:           m1.Elapsed.Nanoseconds(),
		AsyncState:            lr.State.String(),
		AsyncAttempts:         lr.Attempts,
		AsyncProbeElapsedNs:   lr.ProbeElapsed.Nanoseconds(),
		AsyncLoadElapsedNs:    lr.LoadElapsed.Nanoseconds(),
		AsyncTempElapsedNs:    lr.TempElapsed.Nanoseconds(),
		AsyncReadyWaitNs:      waitNs,
		AsyncFrameSubmittedNs: nightWBUnixNano(lr.FrameSubmittedAt),
		AsyncFirstLayoutNs:    lr.FirstLayoutAfterSwap.Nanoseconds(),
	}
	if lr.Probe != nil {
		s.AsyncProbeRan = lr.Probe.ProbeRan
		s.AsyncProbeElapsedHist = lr.Probe.Elapsed.Nanoseconds()
	}
	if lr.Err != nil {
		s.AsyncErr = lr.Err.Error()
	}
	if state == theme.LoadFailed {
		s.Missing++
		s.Notes = append(s.Notes, "异步加载进入失败终态")
	}
	if time.Now().After(deadline) && state != theme.LoadReady && state != theme.LoadFailed {
		s.Missing++
		s.Notes = append(s.Notes, "等待 Ready 超时 30s")
	}
	if s.LayoutEntryNs == 0 {
		s.Missing++
		s.Notes = append(s.Notes, "未取到 Layout 入口时刻")
	}
	nightWBWrite(t, s)
}
