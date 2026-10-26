package app

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gioui.org/text"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ============================================================================
// TASK-FONT-001-D-IMPL-3：诊断、失败终态与严格语义
//
// 与 font_lazy_test.go（IMPL-2）的分工：那个文件验证"**成功**路径下怎么换主题"，
// 本文件验证"**失败**路径下怎么让失败可见"，以及四个指标各自独立。
//
// # 失败是怎么造出来的
//
// 用 `theme.ProviderOptions` 的两个注入点，不依赖本机恰好没有字体：
//   - `Candidates` 指向不存在的路径 → 系统链路全部 StatusMissing；
//   - `Embedded` 换成必定失败的 loader → 嵌入兜底也失败。
// 两条链路都失败，`Lazy` 必须进入 `LoadFailed`，且 `Err` 包 `ErrFontNotFound`。
//
// # 三条纪律
//
//  1. 不靠崩溃：`StrictFontFailure=true` 时窗口必须照常运行，测试断言的是
//     "Run 返回了错误"，而不是"进程没崩"；
//  2. 不提前定阈值：M1 只验证"可测/不可测"，数值只打印；
//  3. 没测到就报未验证：零值不得被当成"很快"。
// ============================================================================

// failingFontLoader 让嵌入兜底必定失败（B 卡 §4 的第 2 条链路）。
//
// 实现 `theme.EmbeddedFontLoader`：返回错误而不是"空字节 + nil"——
// 后者会让"兜底成功但字体为空"这种更糟的静默故障成立。
type failingFontLoader struct{}

func (failingFontLoader) Load() ([]byte, string, error) {
	return nil, "", errors.New("嵌入字体不可用（测试注入）")
}

// failReadDelay 是失败场景里每次候选读取的注入延迟。
//
// 存在的理由：真实的"文件不存在"失败只要几微秒，而本机 `time.Now()` 粒度约
// 0.53 ms——那会让失败路径的 `LoadElapsed` 恒读成 0。0 在这里表示"没测到"，
// **不是**"没有耗时"；把它当成一个通过/失败的判据就是在拿一个读不出来的
// 数下结论。因此给候选读取注入一段确定的延迟，让耗时落进可分辨区间，
// 卡 §5 要求的"失败路径 LoadElapsed 非零"才是可验证的。
const failReadDelay = 50 * time.Millisecond

// newFailingLazy 构造一个必然进入失败终态的 Lazy。
//
// temp 可为 nil（走默认临时主题）；传入非 nil 可显式控制"界面上用的是什么字体"。
func newFailingLazy(t *testing.T, temp *material.Theme) *theme.Lazy {
	t.Helper()
	p := theme.NewProvider(theme.ProviderOptions{
		// 只放**一个**候选：放两个时注入延迟会累计到超过默认探测预算，
		// 第二个候选会被标成 skipped_timeout（带超时语义），
		// 把"文件不存在"的归因搅浑。
		Candidates: []string{"/__goxml_nonexistent__/a.ttc"},
		Read: func(string) ([]byte, error) {
			time.Sleep(failReadDelay)
			return nil, os.ErrNotExist
		},
		Embedded: failingFontLoader{},
	})
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Temp: temp})
	if err != nil {
		t.Fatalf("构造 Lazy 失败: %v", err)
	}
	return lz
}

// newSlowFailingLazy 与上面相同，但探测会慢 d，用于稳定观察 Loading 态。
//
// 没有它，"Loading 与 Failed 可区分"就只能赌时序——那正是本节要消灭的脆弱性。
func newSlowFailingLazy(t *testing.T, d time.Duration) *theme.Lazy {
	t.Helper()
	p := theme.NewProvider(theme.ProviderOptions{
		Candidates: []string{"/__goxml_nonexistent__/a.ttc"},
		Read: func(string) ([]byte, error) {
			time.Sleep(d)
			return nil, os.ErrNotExist
		},
		Embedded: failingFontLoader{},
	})
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("构造 Lazy 失败: %v", err)
	}
	return lz
}

// requireTerminal 等到进入终态（Ready 或 Failed）。
func requireTerminal(t *testing.T, lz *theme.Lazy) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		switch lz.State() {
		case theme.LoadReady, theme.LoadFailed:
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("15 秒内未进入终态（state=%s）", lz.State())
}

// ---------------------------------------------------------------------------
// 用例 1：成功路径的字段就位
// ---------------------------------------------------------------------------

// TestFontLazyLoadDiagnosticsFields 成功时 `Report()` 各字段就位。
//
// ⚠ 这里**必须**用全新的 Provider，不能用 `newTestLazy`（后者 `Provider` 为 nil，
// 落到包级共享的 `defaultProvider`）。
//
// 原因（D-ACCEPT-08 取证）：本用例断言 `LoadElapsed > 0`，而
// `LoadElapsed = CompletedAt - RequestedAt` 是墙钟差值。整包运行时共享 Provider
// 可能已被前面的用例探测过，命中解析缓存后后台加载从约 164ms 掉到 0.5–1ms；
// 本机 `time.Now()` 步长约 0.5ms，于是这个差值**有时恰好被量化成 0**
// ——实测整包 10 次中失败 2 次（单跑 6/6 通过）。
// 换成全新 Provider ⇒ 本轮不会被解析缓存短路，必须真实执行系统字体探测，
// 耗时回到 100ms 量级，相对时钟粒度有两个数量级以上的余量。
//
// 该缓存语义属 Provider 层（IMPL-1 范围），本卡不越界修改，只在测量时避开它。
// 断言本身**未放宽**：仍然要求 `LoadElapsed > 0`，没有改成 `>= 0`、没有删除、
// 没有加人为延迟、也没有按 `ProbeRan` 做条件放行。
func TestFontLazyLoadDiagnosticsFields(t *testing.T) {
	lz, err := theme.NewLazy(theme.LazyOptions{
		Provider: theme.NewProvider(theme.ProviderOptions{}),
	})
	if err != nil {
		t.Fatalf("构造 Lazy 失败: %v", err)
	}
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "diag-ok"})
	if err != nil {
		t.Fatal(err)
	}
	requireReady(t, lz)
	driveFrames(a, 1)

	rep := a.Lazy().Report()
	if rep.State != theme.LoadReady {
		t.Errorf("State = %s, 期望 ready", rep.State)
	}
	if rep.Err != nil {
		t.Errorf("成功路径 Err = %v, 期望 nil", rep.Err)
	}
	if rep.Family == "" {
		t.Error("Family 为空")
	}
	if rep.Attempts != 1 {
		t.Errorf("Attempts = %d, 期望 1", rep.Attempts)
	}
	if rep.TempSource == "" {
		t.Error("TempSource 为空：临时主题来源必须可追溯")
	}
	if rep.RequestedAt.IsZero() {
		t.Error("RequestedAt 为零值")
	}
	if rep.LoadedAt.IsZero() {
		t.Fatal("LoadedAt 为零值：成功路径必须记录产出时刻")
	}
	if rep.CompletedAt.IsZero() {
		t.Fatal("CompletedAt 为零值：成功路径必须记录终态时刻")
	}
	if rep.CompletedAt.Before(rep.LoadedAt) {
		t.Errorf("CompletedAt(%v) 早于 LoadedAt(%v)", rep.CompletedAt, rep.LoadedAt)
	}
	if rep.LoadElapsed <= 0 {
		t.Errorf("LoadElapsed = %v, 期望 > 0", rep.LoadElapsed)
	}
	if rep.Probe == nil {
		t.Error("Probe 为 nil：探测诊断必须可读")
	}
	t.Logf("成功路径：state=%s attempts=%d tempSource=%s loadElapsed=%v",
		rep.State, rep.Attempts, rep.TempSource, rep.LoadElapsed)
}

// ---------------------------------------------------------------------------
// 用例 2：失败终态可见
// ---------------------------------------------------------------------------

// TestFontLazyLoadFailureVisible 两条链路全失败时，失败必须被完整读出。
func TestFontLazyLoadFailureVisible(t *testing.T) {
	lz := newFailingLazy(t, nil)
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "diag-fail"})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)

	rep := a.Lazy().Report()
	if rep.State != theme.LoadFailed {
		t.Fatalf("State = %s, 期望 failed", rep.State)
	}
	if rep.Err == nil {
		t.Fatal("失败终态 Err 为 nil：诊断看不到原因，就是评审点名的静默故障")
	}
	if !errors.Is(rep.Err, contracts.ErrFontNotFound) {
		t.Errorf("Err = %v，必须能用 errors.Is 判定为 ErrFontNotFound", rep.Err)
	}
	// 失败路径的关键区分：LoadedAt 零值（没有产出），但 CompletedAt / LoadElapsed 非零。
	if !rep.LoadedAt.IsZero() {
		t.Errorf("失败路径 LoadedAt 必须为零值（没有产出可用主题），实际 %v", rep.LoadedAt)
	}
	if rep.CompletedAt.IsZero() {
		t.Error("失败路径 CompletedAt 必须非零（终态时刻成败都记）")
	}
	if rep.LoadElapsed <= 0 {
		t.Errorf("失败路径 LoadElapsed = %v，期望 > 0", rep.LoadElapsed)
	}
	// 失败原因必须落到候选层面，不能只有一个笼统的错误。
	if rep.Probe == nil {
		t.Fatal("Probe 为 nil：失败原因无从追溯")
	}
	if len(rep.Probe.Candidates) == 0 {
		t.Error("Probe.Candidates 为空：看不出是哪些候选失败")
	}
	withReason := 0
	for _, c := range rep.Probe.Candidates {
		if c.Status != "" && (c.Err != nil || c.Status != theme.StatusOK) {
			withReason++
			t.Logf("候选 %s: status=%s err=%v", c.Path, c.Status, c.Err)
		}
	}
	if withReason == 0 {
		t.Error("所有候选都没有失败原因：诊断无法归因")
	}
	t.Logf("失败终态：err=%v（errors.Is 已验证）", rep.Err)
}

// ---------------------------------------------------------------------------
// 用例 3：Loading 与 Failed 可区分
// ---------------------------------------------------------------------------

// TestFontLazyLoadFailureDistinguishable `Loading` 与 `Failed` 不能用同一种读数描述。
//
// 两者界面**都是**临时字体，所以区分必须发生在诊断层（卡 §2）。
func TestFontLazyLoadFailureDistinguishable(t *testing.T) {
	const slow = 400 * time.Millisecond

	// ---- Loading 态（慢探测保证可稳定观察）----
	lzLoading := newSlowFailingLazy(t, slow)
	aLoading, err := NewLazyApp(&lazyProbeView{}, lzLoading, Config{Title: "diag-loading"})
	if err != nil {
		t.Fatal(err)
	}
	loadingRep := lzLoading.Report()
	if loadingRep.State != theme.LoadLoading {
		t.Fatalf("慢探测下应能观察到 loading，实际 %s（探测耗时 %v）",
			loadingRep.State, slow)
	}
	if loadingRep.Err != nil {
		t.Errorf("Loading 态 Err = %v，期望 nil（还没到终态）", loadingRep.Err)
	}
	loadingShaper := aLoading.th.Shaper

	// ---- Failed 态 ----
	requireTerminal(t, lzLoading)
	failedRep := lzLoading.Report()
	if failedRep.State != theme.LoadFailed {
		t.Fatalf("终态 = %s, 期望 failed", failedRep.State)
	}
	if failedRep.Err == nil {
		t.Fatal("Failed 态 Err 为 nil")
	}

	// 二元组必须不同，否则"永久停在临时字体"就说不清是还在加载还是已经失败。
	if loadingRep.State == failedRep.State {
		t.Error("Loading 与 Failed 的 State 相同：不可区分")
	}
	if (loadingRep.Err == nil) == (failedRep.Err == nil) {
		t.Error("Loading 与 Failed 的 Err 是否为空相同：不可区分")
	}

	// 界面层确实相同（都是临时字体）——这正是必须靠诊断区分的原因。
	if aLoading.th.Shaper != loadingShaper {
		t.Error("失败后界面上的字体被换掉了：失败路径不得替换 Shaper")
	}
	t.Logf("Loading=(state=%s, err=%v) vs Failed=(state=%s, err!=nil)；"+
		"界面同为临时字体（shaper 未变），区分只能来自诊断",
		loadingRep.State, loadingRep.Err, failedRep.State)
}

// ---------------------------------------------------------------------------
// 用例 4 / 5：严格模式与默认模式
// ---------------------------------------------------------------------------

// stubMainNow 把 app.Main 换成立即返回（模拟"窗口已正常关闭"）。
func stubMainNow(t *testing.T) {
	t.Helper()
	orig := mainFunc
	t.Cleanup(func() { mainFunc = orig })
	mainFunc = func() {}
}

// TestFontLazyLoadStrictFailure `StrictFontFailure=true`：窗口照常运行，
// `Run()` 正常返回后错误含 `ErrFontNotFound`（**不得**闪退、**不得** panic）。
func TestFontLazyLoadStrictFailure(t *testing.T) {
	lz := newFailingLazy(t, nil)
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{
		Title: "strict", StrictFontFailure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)
	if lz.State() != theme.LoadFailed {
		t.Fatalf("构造的失败场景未进入 LoadFailed：%s", lz.State())
	}

	stubMainNow(t)

	// 能走到下一行就说明没有 panic / 没有强制关窗。
	runErr := a.Run()
	if runErr == nil {
		t.Fatal("严格模式下 Run() 必须返回错误，否则严格语义在异步下就静默消失了")
	}
	if !errors.Is(runErr, contracts.ErrFontNotFound) {
		t.Errorf("Run() 错误 = %v，必须可判定为 ErrFontNotFound", runErr)
	}
	// 失败原因在诊断里也要能读到，且两处一致。
	if rep := a.Lazy().Report(); rep.Err == nil || !errors.Is(rep.Err, contracts.ErrFontNotFound) {
		t.Errorf("Report().Err 与 Run() 的错误不一致：%v", rep.Err)
	}
	t.Logf("严格模式：Run() 返回 %v（窗口未闪退）", runErr)
}

// TestFontLazyLoadNonStrictKeepsRunning 默认 `false`：窗口继续跑，`Run()` 返回 nil，
// 但失败原因仍可从 `Report()` 读到。
func TestFontLazyLoadNonStrictKeepsRunning(t *testing.T) {
	lz := newFailingLazy(t, nil)
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "non-strict"})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)

	stubMainNow(t)

	if runErr := a.Run(); runErr != nil {
		t.Errorf("非严格模式 Run() 必须返回 nil（失败不终止窗口），实际 %v", runErr)
	}
	rep := a.Lazy().Report()
	if rep.Err == nil {
		t.Fatal("非严格模式下 Report().Err 也必须能读到失败原因——" +
			"否则调用方无从知晓")
	}
	if rep.State != theme.LoadFailed {
		t.Errorf("State = %s, 期望 failed", rep.State)
	}
	t.Logf("非严格模式：Run() = nil，失败原因仍可从 Report().Err 读到：%v", rep.Err)
}

// TestFontLazyLoadStrictFlagNoEffectOnSyncPath 完成判据 3：
// 同步路径在开关的两种取值下行为一致（`Lazy()` 为 nil、`Run()` 返回 nil）。
func TestFontLazyLoadStrictFlagNoEffectOnSyncPath(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(strings.Join([]string{"strict=", boolToStr(strict)}, ""), func(t *testing.T) {
			stubMainNow(t)
			a, err := NewApp(&countingView{text: "x"}, material.NewTheme(),
				Config{Title: "sync", StrictFontFailure: strict})
			if err != nil {
				t.Fatal(err)
			}
			if a.Lazy() != nil {
				t.Error("同步路径的 Lazy() 必须是 nil（没有懒加载状态）")
			}
			if runErr := a.Run(); runErr != nil {
				t.Errorf("同步路径在 strict=%v 下 Run() 必须返回 nil，实际 %v", strict, runErr)
			}
		})
	}
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---------------------------------------------------------------------------
// 用例 6：四个指标独立
// ---------------------------------------------------------------------------

// TestFontLazyLoadMetricSeparation 四个指标分别可读、来源不同、且不存在"合数"字段。
func TestFontLazyLoadMetricSeparation(t *testing.T) {
	// ⚠ 这里**必须**用全新的 Provider，不能用 `newTestLazy`（它用包级共享的
	// defaultProvider）。
	//
	// 【历史问题】（当时的事实，保留以备追溯）：在 `ProbeRan` 门控落地之前，
	// 共享 Provider 命中解析缓存时，`ResolveWithReport` 直接返回上一次的
	// `ProbeReport`，于是 `Report.ProbeElapsed` 带的是**上一次**探测的耗时，
	// 而 `LoadElapsed` 测的是本次（缓存命中，仅 1 ms）。实测出现过
	// ProbeElapsed=105.05ms 与 LoadElapsed=1.03ms 同框——两者不同源。
	//
	// 【当前语义】（D-ACCEPT-07 已实施 `ProbeRan` 门控，**问题已不再出现**）：
	// 缓存命中路径**返回派生副本**，其中 `Probe.ProbeRan=false`，
	// 且 `Lazy` 侧 `ProbeElapsed` **保持 0**（`lazy.go:359-369`）——
	// 不再把历史探测耗时伪装成本轮耗时。历史值仍保留在 `Probe.Elapsed` 里作诊断。
	//
	// 但"用全新 Provider"这条前提**仍然保留**：本用例要验的是
	// 「M2 区间 ⊂ M3 区间」这一**同源同轮**的包含关系，只有未命中路径
	// 才同时具备两个本轮实测值。命中路径的正确判据是"`ProbeElapsed==0`"，
	// 属另一条契约（见下面的条件化断言与 TestNight07Downstream* 用例）。
	lz, err := theme.NewLazy(theme.LazyOptions{
		Provider: theme.NewProvider(theme.ProviderOptions{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "metrics"})
	if err != nil {
		t.Fatal(err)
	}
	requireReady(t, lz)
	driveFrames(a, 1)

	rep := a.Lazy().Report()

	// M2：探测段（只覆盖 probe 的候选循环）
	// M3：后台总量（RequestedAt → CompletedAt）
	// M4：切换后首次排版
	// M1：首帧（本文件另一个用例覆盖，这里只打印，避免重复断言）
	ff := a.FirstFrame()
	t.Logf("M1 首帧        : createdAt=%v firstFrameAt=%v elapsed=%v（measured=%v）",
		ff.CreatedAt, ff.FirstFrameAt, ff.Elapsed, ff.Measured())
	t.Logf("M2 探测段      : ProbeElapsed = %v", rep.ProbeElapsed)
	t.Logf("M3 后台总量    : LoadElapsed  = %v", rep.LoadElapsed)
	t.Logf("M4 切换后排版  : FirstLayoutAfterSwap = %v", rep.FirstLayoutAfterSwap)

	if rep.LoadElapsed <= 0 {
		t.Errorf("M3 LoadElapsed = %v, 期望 > 0", rep.LoadElapsed)
	}
	// M3 的区间就是 CompletedAt - RequestedAt（成功/失败都记）。
	if want := rep.CompletedAt.Sub(rep.RequestedAt); want != rep.LoadElapsed {
		t.Errorf("M3 计量边界不符：LoadElapsed=%v, CompletedAt-RequestedAt=%v",
			rep.LoadElapsed, want)
	}
	// M2 是 M3 的一部分：候选循环不可能比整个后台还慢。
	//
	// ⚠ 该不变式 **只在"本轮确实执行了探测"时成立**（D-ACCEPT-07 要求④）：
	// 命中路径 `ProbeElapsed` 恒为 0 ⇒ `0 > LoadElapsed` 恒假，
	// 无条件写法在那种路径上会退化成**空判据**（不报错，也不再能发现"两种耗时不同源"）。
	// 因此按 `ProbeRan` 分路径断言，而**不是**改符号、删除或 `t.Skip`：
	//   - ProbeRan == true  → 同源同轮，断言 ProbeElapsed <= LoadElapsed；
	//   - ProbeRan == false → 本轮没探测，断言 ProbeElapsed == 0（历史值留在 Probe.Elapsed）。
	// 本用例用全新 Provider，实际走 ProbeRan==true 分支；else 分支是给
	// 「将来复用该断言到命中路径」兜底的显式契约，避免静默失效。
	if rep.Probe == nil {
		t.Fatal("本轮 Ready 报告缺少 Probe，不能按未探测或缓存命中处理")
	}
	if rep.Probe.ProbeRan {
		if rep.ProbeElapsed > rep.LoadElapsed {
			t.Errorf("M2(%v) > M3(%v)：候选循环不可能比后台总量还大",
				rep.ProbeElapsed, rep.LoadElapsed)
		}
	} else {
		// 本轮没有执行探测流程 ⇒ 本轮探测耗时必须为 0。
		// 注意：`Probe==nil` 不等于缓存命中（未知 family 等路径同样是 nil），
		// 所以这里只断言"本轮无探测耗时"，不宣称命中语义——
		// 命中语义由 TestNight07Downstream* 用读计数单独证明。
		if rep.ProbeElapsed != 0 {
			t.Errorf("ProbeRan=%v（本轮未探测）却记录 ProbeElapsed=%v，期望 0；"+
				"疑似把历史 Probe.Elapsed 当成了本轮耗时",
				rep.Probe != nil && rep.Probe.ProbeRan, rep.ProbeElapsed)
		}
	}
	if rep.FirstLayoutAfterSwap <= 0 {
		t.Errorf("M4 FirstLayoutAfterSwap = %v, 期望 > 0", rep.FirstLayoutAfterSwap)
	}
	// M4 发生在切换之后，因此必然晚于后台终态——它不属于 M3。
	if !rep.CompletedAt.IsZero() && rep.FirstLayoutAfterSwap >= rep.LoadElapsed &&
		rep.LoadElapsed > 0 && rep.FirstLayoutAfterSwap == rep.LoadElapsed {
		t.Error("M4 与 M3 数值相同：排版耗时疑似被并进了后台总量")
	}

	t.Run("不存在合数字段", func(t *testing.T) {
		// 结构判据：LoadReport 里不得出现 Total*/Sum*/All* 之类的合数字段。
		// 数值上的"不相等"证明不了结构，得连字段名一起看。
		typ := reflect.TypeOf(theme.LoadReport{})
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			low := strings.ToLower(name)
			if strings.Contains(low, "total") || strings.Contains(low, "sum") ||
				strings.Contains(low, "allin") || strings.Contains(low, "combined") {
				t.Errorf("LoadReport 出现合数字段 %s：四个指标各自有边界，"+
					"加起来没有物理含义（卡 §4.3）", name)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// 用例 7：失败后不得表现为"字体正常但内容错"
// ---------------------------------------------------------------------------

// TestFontLazyLoadNoSilentFallback 临时字体缺字形必须**可观测**，
// 不能退化成"看起来渲染正常、其实是豆腐块"。
//
// 不依赖本机嵌入字体恰好缺某个字形：临时主题由 `LazyOptions.Temp` 显式指定为
// **确定无字形**的主题，结果在任何机器上都必须一致。
func TestFontLazyLoadNoSilentFallback(t *testing.T) {
	const sample = "中文字体"

	// ① 对照：一个有字形的主题，MissingGlyphs 必须为空。
	//    没有这一步，"MissingGlyphs 返回非空"可能是恒真的空判据。
	ctrl := material.NewTheme()
	ctrlMissing := theme.NewResolver(ctrl.Shaper).MissingGlyphs(sample)
	if len(ctrlMissing) != 0 {
		t.Fatalf("对照不成立：本机默认主题对 %q 也报缺字 %q——"+
			"本用例无法在本机区分，记为未验证（不是通过）", sample, string(ctrlMissing))
	}
	t.Logf("对照：默认主题对 %q 缺字 = %d 个（期望 0，证明检测器不是恒真）",
		sample, len(ctrlMissing))

	// ② 确定无字形的临时主题：NoSystemFonts + 空集合。
	glyphless := material.NewTheme()
	glyphless.Shaper = text.NewShaper(text.NoSystemFonts())

	lz := newFailingLazy(t, glyphless)
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "no-silent"})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)

	rep := a.Lazy().Report()
	if rep.State != theme.LoadFailed {
		t.Fatalf("State = %s, 期望 failed", rep.State)
	}
	// 界面上用的仍是临时主题（失败不得偷偷换字体，也不得换成"看起来正常"的字体）。
	if a.th.Shaper != glyphless.Shaper {
		t.Error("失败后界面字体被换掉了：临时主题应当保持不变，缺字才可被观测")
	}

	// ③ 缺字形必须能被读出来——这是"不静默"的可执行证据。
	missing := theme.NewResolver(a.th.Shaper).MissingGlyphs(sample)
	if len(missing) == 0 {
		t.Errorf("无字形主题对 %q 未报缺字：失败会被读成'字体正常但内容错'，"+
			"这正是要消灭的静默故障", sample)
	}
	t.Logf("失败后界面主题对 %q 报缺字 %d 个：%q（可观测，不是静默）",
		sample, len(missing), string(missing))
}

// ---------------------------------------------------------------------------
// M1 首帧时刻：可记录、可读取、不可用时报未验证
// ---------------------------------------------------------------------------

// TestFontLazyLoadFirstFrameReport M1 的最小可测化（**不定阈值**）。
//
// 判据只有两条：没跑帧时 `Measured()` 必须为 false（不得把"没测到"当"很快"）；
// 跑过帧之后必须可测、且止点不早于起点。数值只打印，合格线归 D-ACCEPT。
func TestFontLazyLoadFirstFrameReport(t *testing.T) {
	lz := newTestLazy(t)
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "m1"})
	if err != nil {
		t.Fatal(err)
	}

	// 未跑帧：必须报"未测量"。
	before := a.FirstFrame()
	if before.Measured() {
		t.Fatal("还没有任何帧却报告已测量：零值会被当成'首帧很快'，属虚假通过")
	}
	if !before.FirstFrameAt.IsZero() {
		t.Errorf("未跑帧时 FirstFrameAt = %v，期望零值", before.FirstFrameAt)
	}
	if before.CreatedAt.IsZero() {
		t.Error("CreatedAt 必须非零（构造入口时刻）")
	}
	if before.Elapsed != 0 {
		t.Errorf("未测量时 Elapsed = %v，期望 0（零值表示没测到，不是耗时为 0）",
			before.Elapsed)
	}

	// 等真主题到位再跑帧：让 M1 的区间明确大于本机计时粒度（约 0.53 ms），
	// 否则读数可能恒为 0，那也是"未验证"而不是"很快"。
	requireReady(t, lz)
	driveFrames(a, 2)

	after := a.FirstFrame()
	if !after.Measured() {
		t.Fatal("跑过帧之后仍报未测量：M1 没有被记录")
	}
	if after.FirstFrameAt.Before(after.CreatedAt) {
		t.Errorf("止点 %v 早于起点 %v：M1 区间不成立", after.FirstFrameAt, after.CreatedAt)
	}
	if after.Elapsed <= 0 {
		t.Fatalf("M1 Elapsed = %v：读不出该量级（本机计时粒度约 0.53 ms）——"+
			"必须记为未验证，不得当作通过", after.Elapsed)
	}

	// 只记第一次：后续帧不得覆盖。
	first := after.FirstFrameAt
	driveFrames(a, 3)
	if got := a.FirstFrame(); got.FirstFrameAt != first {
		t.Errorf("首帧时刻被后续帧覆盖: %v → %v", first, got.FirstFrameAt)
	}
	t.Logf("M1：createdAt=%v firstFrameAt=%v elapsed=%v（只记录，阈值归 D-ACCEPT）",
		after.CreatedAt, after.FirstFrameAt, after.Elapsed)
}

// ---------------------------------------------------------------------------
// 重复请求：不得引入隐式重试或重复加载
// ---------------------------------------------------------------------------

// TestFontLazyLoadNoImplicitRetry 读诊断、跑帧都不得触发重新加载；
// 只有显式 `Request()` 才重试。
func TestFontLazyLoadNoImplicitRetry(t *testing.T) {
	lz := newFailingLazy(t, nil)
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "no-retry"})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)

	first := lz.Report()
	if first.State != theme.LoadFailed {
		t.Fatalf("State = %s, 期望 failed", first.State)
	}

	// 反复读诊断 + 反复跑帧：都不得触发新的一轮加载。
	for i := 0; i < 5; i++ {
		driveFrames(a, 2)
		_ = lz.Report()
		_ = lz.State()
		_ = a.Lazy().Report()
	}
	second := lz.Report()
	if second.Attempts != first.Attempts {
		t.Errorf("隐式重试：Attempts %d → %d（跑帧或读诊断不得触发重新加载）",
			first.Attempts, second.Attempts)
	}
	if !second.RequestedAt.Equal(first.RequestedAt) {
		t.Errorf("RequestedAt 被改动: %v → %v，说明发生了新的加载",
			first.RequestedAt, second.RequestedAt)
	}
	if second.State != theme.LoadFailed {
		t.Errorf("State 从 failed 变成了 %s", second.State)
	}

	// 显式 Request 才重试（不存在其它重试入口）。
	lz.Request()
	third := lz.Report()
	if third.Attempts != first.Attempts+1 {
		t.Errorf("显式 Request 后 Attempts = %d，期望 %d",
			third.Attempts, first.Attempts+1)
	}
	t.Logf("失败终态下 5 轮读诊断 + 10 帧未触发重试（Attempts 恒为 %d）；"+
		"显式 Request 后 Attempts = %d", second.Attempts, third.Attempts)
}
