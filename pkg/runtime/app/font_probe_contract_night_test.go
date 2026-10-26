package app

// NIGHT-FONT-07-DOWNSTREAM：`D-ACCEPT-07` 要求④的下游计时断言候选证据。
//
// 本文件只新增测试与测试辅助（`nightDS` 前缀），**不改生产代码**。
// 目标是把「M2 区间 ⊂ M3 区间」这条不变式所依赖的**路径前提**变成可独立运行的证据：
//
//	口径1 Ready 且本轮探测（Probe.ProbeRan==true）→ ProbeElapsed <= LoadElapsed（同源同轮）
//	口径2 真实缓存命中（Probe.ProbeRan==false） → ProbeElapsed == 0，历史 Probe.Elapsed 保留
//	口径3 ProbeRan==false ≠ 缓存命中 → 命中必须有**独立证据**（本文件用 Read 调用计数）
//	口径4 Probe==nil 按路径分别处理 → 不当命中、不直接解引用
//	口径5 虚拟时钟的 ProbeElapsed 不得与真实墙钟 LoadElapsed 做包含比较
//
// 依据：`TASK-FONT-001-D-ACCEPT.md`（12）要求④、（13）边界 2/3；
// `theme/lazy.go:359-371`（ProbeElapsed 门控）、`theme/font.go:320-335`（命中派生副本）。

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ---------------------------------------------------------------------------
// 测试辅助（nightDS 前缀，避免与其他 Agent 在同一测试包内新增的辅助重名）
// ---------------------------------------------------------------------------

// nightDSReadCounter 是线程安全的 Read 调用计数器。
//
// 它是"真实缓存命中"的**独立证据**：`ProbeRan==false` 本身可能是"从未进入探测"
// （未知 family 等），只有 Read 调用次数为 0 才能证明本轮确实没有重扫。
type nightDSReadCounter struct {
	calls atomic.Int64
	delay time.Duration
}

func (c *nightDSReadCounter) read(path string) ([]byte, error) {
	c.calls.Add(1)
	if c.delay > 0 {
		time.Sleep(c.delay)
	}
	return os.ReadFile(path)
}

func (c *nightDSReadCounter) Calls() int64 { return c.calls.Load() }

// nightDSNewLazyWith 用给定 ProviderOptions 构造 Lazy，并**立即发起加载**。
//
// ⚠ 必须显式调用 `Request()`：`Lazy` 构造后处于 `unloaded`，它**不会自动开始**加载
// （首败证据：cmd02b 中 5 个用例全部 `state=unloaded attempts=0`，就是因为漏了这一步；
// `requireReady` 只轮询等待、不触发）。真实 App 路径由 `App.frame` 间接触发。
func nightDSNewLazyWith(t *testing.T, opts theme.ProviderOptions, temp *material.Theme) (*theme.Lazy, *theme.Provider) {
	t.Helper()
	p := theme.NewProvider(opts)
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Temp: temp})
	if err != nil {
		t.Fatalf("构造 Lazy 失败: %v", err)
	}
	lz.Request() // 启动异步加载；否则永远停在 unloaded
	return lz, p
}

// nightDSRequireReportShape 断言报告存在且状态为 Ready。
func nightDSRequireReadyReport(t *testing.T, lz *theme.Lazy) theme.LoadReport {
	t.Helper()
	rep := lz.Report()
	if rep.State != theme.LoadReady {
		t.Fatalf("期望 LoadReady，实际 state=%s err=%v", rep.State, rep.Err)
	}
	return rep
}

// ---------------------------------------------------------------------------
// 口径 1：全新 Provider 的一次真实冷加载 —— ProbeRan==true 且 ProbeElapsed<=LoadElapsed
// ---------------------------------------------------------------------------

// TestNight07DownstreamColdLoadSameSource 覆盖口径 1。
//
// 关键点：这次是**真实冷加载**（Read 被真正调用），两个耗时来自同一个 `time.Now`
// 时钟（未注入 Now），因此包含关系有物理意义。
func TestNight07DownstreamColdLoadSameSource(t *testing.T) {
	// 用**真实系统候选**（不覆盖 Candidates / Embedded），只把 Read 包装成计数器：
	// 这样既走真实探测流程，又能用读次数作为"本轮确实探测"的独立证据。
	// 前置：本机存在可用 CJK 系统字体（已由 TestFontLazyLoadSystemHitSkipsEmbeddedAsync
	// 在同机证明：Scanned=1、命中）。若前提不成立，下面的 state 检查会明确失败而不是静默通过。
	rc := &nightDSReadCounter{delay: 2 * time.Millisecond}
	lz, _ := nightDSNewLazyWith(t, theme.ProviderOptions{
		Read: rc.read,
	}, nil)

	requireReady(t, lz)
	rep := nightDSRequireReadyReport(t, lz)

	// 冷加载必然真的读了候选：这是"本轮确实探测"的独立证据。
	if got := rc.Calls(); got == 0 {
		t.Fatalf("冷加载路径 Read 调用次数 = 0，前提不成立（没有真正探测）")
	}
	if rep.Probe == nil {
		t.Fatalf("冷加载后 Probe == nil：无法判读本轮探测状态（口径4：不得当命中）")
	}
	if !rep.Probe.ProbeRan {
		t.Fatalf("冷加载后 ProbeRan = false，期望 true（本轮确实执行了探测）")
	}
	if rep.ProbeElapsed <= 0 {
		t.Errorf("冷加载 ProbeElapsed = %v，期望 > 0（已注入 2ms 读延迟）", rep.ProbeElapsed)
	}
	// 口径 1：同源同轮的包含关系。
	if rep.ProbeElapsed > rep.LoadElapsed {
		t.Errorf("口径1 违反：ProbeElapsed=%v > LoadElapsed=%v（探测是后台总量的一部分）",
			rep.ProbeElapsed, rep.LoadElapsed)
	}
	t.Logf("冷加载：ProbeRan=%v ProbeElapsed=%v LoadElapsed=%v ReadCalls=%d",
		rep.Probe.ProbeRan, rep.ProbeElapsed, rep.LoadElapsed, rc.Calls())
}

// ---------------------------------------------------------------------------
// 口径 2 + 3：同一个 Provider 的第二次加载走真实缓存命中
// ---------------------------------------------------------------------------

// TestNight07DownstreamWarmCacheHitIsProvenByReadCount 覆盖口径 2 与口径 3。
//
// 用**同一个 Provider** 建两个 Lazy：第一个完成加载后 Provider 已有解析缓存，
// 第二个走命中。命中必须有**独立证据**——这里用 Read 调用计数不增长来证明，
// 而不是只靠 `ProbeRan==false`（那可能只是"从未进入探测"）。
func TestNight07DownstreamWarmCacheHitIsProvenByReadCount(t *testing.T) {
	// 真实系统候选 + Read 计数包装（同冷加载用例的前提说明）。
	rc := &nightDSReadCounter{delay: 2 * time.Millisecond}
	opts := theme.ProviderOptions{
		Read: rc.read,
	}

	// —— 第一次：冷加载，填充 Provider 解析缓存 ——
	p := theme.NewProvider(opts)
	lz1, err := theme.NewLazy(theme.LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("构造 Lazy#1 失败: %v", err)
	}
	lz1.Request() // 启动异步加载
	requireReady(t, lz1)
	rep1 := nightDSRequireReadyReport(t, lz1)
	callsAfterCold := rc.Calls()
	if callsAfterCold == 0 {
		t.Fatalf("Lazy#1 未触发任何 Read，冷加载前提不成立")
	}
	if rep1.Probe == nil || !rep1.Probe.ProbeRan {
		t.Fatalf("Lazy#1 应为本轮探测（ProbeRan=true），实际 %+v", rep1.Probe)
	}
	// 记录历史探测耗时：命中路径必须把它保留在 Probe.Elapsed 里（裁决②）。
	histProbeElapsed := rep1.Probe.Elapsed
	if histProbeElapsed <= 0 {
		t.Fatalf("Lazy#1 Probe.Elapsed = %v，期望 > 0", histProbeElapsed)
	}

	// —— 第二次：同一 Provider，应走缓存命中 ——
	lz2, err := theme.NewLazy(theme.LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("构造 Lazy#2 失败: %v", err)
	}
	lz2.Request() // 启动第二次加载（应命中 Provider 缓存）
	requireReady(t, lz2)
	rep2 := nightDSRequireReadyReport(t, lz2)
	callsAfterWarm := rc.Calls()

	// 口径 3：真实缓存命中的**独立证据** —— 没有新的 Read 调用。
	if callsAfterWarm != callsAfterCold {
		t.Errorf("warm 路径 Read 调用从 %d 增至 %d：并非缓存命中，不能用命中语义断言",
			callsAfterCold, callsAfterWarm)
	}

	// 口径 4：先判 nil，不当命中、也不解引用。
	if rep2.Probe == nil {
		t.Fatalf("warm 路径 Probe == nil：无法判读，不得当作缓存命中（口径4）")
	}
	// 口径 2：本轮未探测。
	if rep2.Probe.ProbeRan {
		t.Errorf("warm 路径 ProbeRan = true，期望 false（本轮没有执行探测）")
	}
	if rep2.ProbeElapsed != 0 {
		t.Errorf("warm 路径 ProbeElapsed = %v，期望 0（不得沿用历史 Probe.Elapsed）",
			rep2.ProbeElapsed)
	}
	// 裁决②：历史诊断必须保留，不得因命中被清空。
	if rep2.Probe.Elapsed != histProbeElapsed {
		t.Errorf("warm 路径历史 Probe.Elapsed = %v，期望保留 %v（裁决②：诊断不得清空）",
			rep2.Probe.Elapsed, histProbeElapsed)
	}
	// LoadElapsed 的边界（**实测发现，非假设**）：
	//
	// 缓存命中时后台几乎不做事，`CompletedAt - RequestedAt` 可能**低于本机 time.Now
	// 有效粒度而读成 0**（`-count=30` 实测：warm 路径 LoadElapsed=0s 稳定重现，
	// 与 D-ACCEPT-08 记录的"成功路径耗时低于计时粒度"是同一类现象）。
	// 因此这里**不能**断言 `LoadElapsed > 0`——那是拿计时分辨率当契约。
	//
	// 正确判据（本用例关注的是"本轮有没有探测"）：
	//   - `LoadElapsed >= 0` 且 `LoadElapsed` 不超过同轮 `CompletedAt-RequestedAt`；
	//   - warm 与冷加载的关键差别在 `ProbeElapsed==0` 与 `ProbeRan==false`，不在 LoadElapsed。
	if rep2.LoadElapsed < 0 {
		t.Errorf("warm 路径 LoadElapsed = %v，为负值（不可能）", rep2.LoadElapsed)
	}
	if want := rep2.CompletedAt.Sub(rep2.RequestedAt); want != rep2.LoadElapsed {
		t.Errorf("warm 路径 M3 计量边界不符：LoadElapsed=%v, CompletedAt-RequestedAt=%v",
			rep2.LoadElapsed, want)
	}
	if rep2.LoadElapsed == 0 {
		// 不是失败，但必须在日志中显式记录，避免"0 被当成有值"。
		t.Logf("注意：warm 路径 LoadElapsed=0s（低于本机计时粒度），"+
			"CompletedAt-RequestedAt=%v；这不是缺陷，但也**不能**用 >0 作判据",
			rep2.CompletedAt.Sub(rep2.RequestedAt))
	}

	t.Logf("warm：ReadCalls %d→%d（未增加）ProbeRan=%v ProbeElapsed=%v 历史Probe.Elapsed=%v LoadElapsed=%v",
		callsAfterCold, callsAfterWarm, rep2.Probe.ProbeRan, rep2.ProbeElapsed,
		rep2.Probe.Elapsed, rep2.LoadElapsed)
}

// ---------------------------------------------------------------------------
// 口径 3 的反例：ProbeRan==false 但**不是**缓存命中
// ---------------------------------------------------------------------------

// TestNight07DownstreamProbeRanFalseIsNotCacheHit 覆盖口径 3 的反向证明。
//
// 未实现探测的 family（`FamilyBody`/`FamilyMono`）不进入探测流程，
// 其报告同样表现为 `ProbeRan=false`（或 Probe 为 nil），但它**不是缓存命中**。
// 该用例存在的意义：防止下游把两者等价化（（13）第 2 条明令禁止）。
//
// 构造方式：family 由 `LazyOptions.Family` 决定（`Request()` 无参），
// 因此这里直接建一个 `Family: FamilyBody` 的 Lazy。
func TestNight07DownstreamProbeRanFalseIsNotCacheHit(t *testing.T) {
	rc := &nightDSReadCounter{delay: time.Millisecond}
	// 不注入 Candidates / Embedded：本用例要验的是"family 未实现探测"这条路径，
	// 与候选是否可用无关；注入假候选只会让 ReadCalls 的语义变模糊。
	p := theme.NewProvider(theme.ProviderOptions{
		Read: rc.read,
	})
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Family: theme.FamilyBody})
	if err != nil {
		t.Fatalf("构造 Lazy(FamilyBody) 失败: %v", err)
	}
	lz.Request() // 启动加载（FamilyBody 未实现探测 ⇒ 应进入失败终态）

	// FamilyBody 在 A 阶段未实现探测 ⇒ 必须进入失败终态（严格失败，不伪装）。
	requireTerminal(t, lz)
	rep := lz.Report()
	if rep.State != theme.LoadFailed {
		t.Fatalf("FamilyBody 期望 LoadFailed，实际 %s", rep.State)
	}
	if rep.Err == nil {
		t.Fatal("FamilyBody 失败路径 Err 为 nil，失败语义被掩盖")
	}
	if !errors.Is(rep.Err, contracts.ErrFontNotFound) {
		t.Errorf("FamilyBody 失败应能用 errors.Is 判定为 ErrFontNotFound，实际: %v", rep.Err)
	}

	// 核心断言：该路径**不得**被当作缓存命中。
	// 它 ProbeRan 同样为 false，但没有读取任何候选（Read 计数为 0 是因为
	// family 未实现探测 ⇒ 没有进入探测流程），与"缓存命中"是两件事。
	if rc.Calls() != 0 {
		t.Errorf("FamilyBody 路径 Read 调用 %d 次，期望 0（不应进入探测）", rc.Calls())
	}
	if rep.Probe != nil && rep.Probe.ProbeRan {
		t.Errorf("FamilyBody 路径 ProbeRan = true，与「A 阶段只探测 CJK」矛盾")
	}
	if rep.ProbeElapsed != 0 {
		t.Errorf("FamilyBody 路径 ProbeElapsed = %v，期望 0（没有执行探测）", rep.ProbeElapsed)
	}
	t.Logf("FamilyBody：state=%s ProbeRan=%v Probe==nil? %v ReadCalls=%d —— "+
		"该状态与缓存命中**形似而实不同**（口径3）",
		rep.State, rep.Probe != nil && rep.Probe.ProbeRan, rep.Probe == nil, rc.Calls())
}

// ---------------------------------------------------------------------------
// 口径 4：Probe==nil 的安全处理
// ---------------------------------------------------------------------------

// TestNight07DownstreamNilProbeIsNotCacheHit 覆盖口径 4。
//
// 无论何种路径导致 `Probe==nil`，下游都**不得**据此判"缓存命中"，
// 也**不得**直接解引用。本用例断言"nil 时不产生 panic 且不宣称命中"。
func TestNight07DownstreamNilProbeIsNotCacheHit(t *testing.T) {
	// 失败链：候选不可读 + 嵌入也失败 ⇒ 终态 Failed。
	rc := &nightDSReadCounter{delay: time.Millisecond}
	lz, _ := nightDSNewLazyWith(t, theme.ProviderOptions{
		Candidates: []string{"/__night07__/probe-d.ttc"},
		Read:       rc.read,
		Embedded:   nightDSFailingEmbedded{},
	}, nil)

	requireTerminal(t, lz)
	rep := lz.Report()
	if rep.State != theme.LoadFailed {
		t.Fatalf("期望 LoadFailed，实际 %s", rep.State)
	}
	// 不预设 Probe 是否为 nil：只在非 nil 时读字段，并断言"失败链不被条件化掩盖"。
	if rep.Probe != nil {
		t.Logf("失败链 Probe 非 nil：ProbeRan=%v Elapsed=%v", rep.Probe.ProbeRan, rep.Probe.Elapsed)
	} else {
		t.Logf("失败链 Probe == nil：按口径4 不判命中、不解引用")
	}
	// 失败语义的既有约束不得被掩盖：失败路径 CompletedAt 必须非零、LoadedAt 必须零值。
	if rep.CompletedAt.IsZero() {
		t.Error("失败路径 CompletedAt 为零值，与「成败都记 CompletedAt」矛盾")
	}
	if !rep.LoadedAt.IsZero() {
		t.Errorf("失败路径 LoadedAt = %v，期望零值（没有产出可用主题）", rep.LoadedAt)
	}
	if rep.Err == nil {
		t.Error("失败路径 Err 为 nil，失败语义被掩盖")
	}
	if rep.LoadElapsed <= 0 {
		t.Errorf("失败路径 LoadElapsed = %v，期望 > 0", rep.LoadElapsed)
	}
}

// ---------------------------------------------------------------------------
// 口径 5：虚拟时钟只用于自身契约断言，不与真实墙钟做包含比较
// ---------------------------------------------------------------------------

// TestNight07DownstreamVirtualClockNotComparableToWallClock 覆盖口径 5。
//
// 注入 `Now` 后 `ProbeElapsed` 来自虚拟时钟，`LoadElapsed` 来自真实墙钟 ——
// 两者**不同源**，禁止做 `<=`/`>=` 比较。本用例只断言虚拟时钟自身的契约：
// 探测按固定步长推进，因此 `Probe.Elapsed` 与推进次数成正比。
func TestNight07DownstreamVirtualClockNotComparableToWallClock(t *testing.T) {
	var tick atomic.Int64
	base := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	const step = time.Millisecond

	rc := &nightDSReadCounter{}
	lz, _ := nightDSNewLazyWith(t, theme.ProviderOptions{
		Read: rc.read,
		Now: func() time.Time {
			// 每次取时钟前进一个固定步长：使虚拟耗时完全可控。
			return base.Add(time.Duration(tick.Add(1)) * step)
		},
	}, nil)

	requireReady(t, lz)
	rep := nightDSRequireReadyReport(t, lz)

	if rep.Probe == nil {
		t.Fatalf("虚拟时钟路径 Probe == nil")
	}
	// 虚拟时钟契约：探测确实执行（ProbeRan=true），且耗时是 step 的整数倍。
	if !rep.Probe.ProbeRan {
		t.Errorf("虚拟时钟路径 ProbeRan = false，期望 true")
	}
	if rep.Probe.Elapsed <= 0 || rep.Probe.Elapsed%step != 0 {
		t.Errorf("虚拟时钟 Probe.Elapsed = %v，不是步长 %v 的整数倍（时钟未被按预期推进）",
			rep.Probe.Elapsed, step)
	}
	if rep.ProbeElapsed != rep.Probe.Elapsed {
		t.Errorf("本轮探测耗时未传导到 Lazy：ProbeElapsed=%v Probe.Elapsed=%v", rep.ProbeElapsed, rep.Probe.Elapsed)
	}
	// ⚠ 明确不做的事：不比较 ProbeElapsed 与 LoadElapsed（不同源，口径 5）。
	t.Logf("虚拟时钟：Probe.Elapsed=%v（步长整数倍）ProbeElapsed=%v；"+
		"LoadElapsed=%v 来自真实墙钟，**不参与**包含比较",
		rep.Probe.Elapsed, rep.ProbeElapsed, rep.LoadElapsed)
}

// ---------------------------------------------------------------------------
// 测试用的最小 Embedded 加载器
// ---------------------------------------------------------------------------

// nightDSFailingEmbedded 强制嵌入链路失败，用于构造失败终态。
type nightDSFailingEmbedded struct{}

func (nightDSFailingEmbedded) Load() ([]byte, string, error) { return nil, "", os.ErrNotExist }
