package theme

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// TASK-FONT-001-D-IMPL-1 验收用例（16 + 1 条深拷贝）
//
// 全部以 `TestFontLazyLoad` 开头：父卡的验收命令是
// `go test -run TestFontLazyLoad ./pkg/runtime/theme/...`，`-run` 是**前缀匹配**，
// 沿用前缀才不会退化成 `no tests to run` + 退出码 0（空转报绿）。
//
// # 假加载器怎么造
//
// 复用 B/C 卡的手法：注入 `ProviderOptions.Embedded` + `ProviderOptions.Read`，
// 不靠 `time.Sleep` 制造竞态。需要"卡住后台"时用一个显式放行通道，
// 测试结束前必须 `close` 掉，避免 goroutine 泄漏。
// ============================================================================

// newLazySystemHit 造一个"系统命中"的 Lazy，并返回嵌入加载计数器。
//
// 计数器的意义：系统命中且覆盖完整时，C 卡的按需补链**不会**启动，
// 因此嵌入加载只可能来自"临时主题构造"。于是 `calls` 天然就是
// "临时主题被构造了几次"的观测点——这正是问题 A 要钉住的东西。
func newLazySystemHit(t *testing.T) (*Lazy, *countingLoader) {
	t.Helper()
	font := hanziFontBytes(t)
	dir := t.TempDir()
	good := writeFont(t, dir, "good.otf", font)

	cl := &countingLoader{data: font}
	p := NewProvider(ProviderOptions{
		Candidates: []string{good},
		Embedded:   cl,
	})
	lz, err := NewLazy(LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("NewLazy 失败: %v", err)
	}
	return lz, cl
}

// newLazyAllFail 造一个"系统与嵌入两条链路都失败"的 Lazy。
func newLazyAllFail(t *testing.T) (*Lazy, *Provider) {
	t.Helper()
	return newLazyAllFailSlow(t, 0)
}

// newLazyAllFailSlow 同上，但候选读取固定耗时 delay。
//
// 为什么要能"慢"：失败路径要验的是 `LoadElapsed = CompletedAt-RequestedAt`
// 这条**公式**，而不是"这次失败恰好花了几微秒"。本机（Windows/虚拟机）的
// 时钟粒度可能粗到毫秒级，一次「文件不存在」的失败可以整个落在同一个刻度里，
// 于是 `LoadElapsed` 与 `ProbeElapsed` 都**真的**是 0——那既不能证明公式对，
// 也不能证明公式错。给它一个明确的下限（2ms），断言才有意义。
// 注意这**不是**放宽判据：0 值在真实世界里也会出现，只说明"它快"。
func newLazyAllFailSlow(t *testing.T, delay time.Duration) (*Lazy, *Provider) {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "definitely-missing.otf")
	read := ReadFileFunc(os.ReadFile)
	if delay > 0 {
		read = func(string) ([]byte, error) {
			time.Sleep(delay)
			return nil, os.ErrNotExist
		}
	}
	p := NewProvider(ProviderOptions{
		Candidates: []string{missing},
		Read:       read,
		Embedded:   &failingLoader{err: errors.New("测试注入：嵌入不可用")},
	})
	lz, err := NewLazy(LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("NewLazy 失败: %v", err)
	}
	return lz, p
}

// newLazyBlocked 造一个"读取卡住"的 Lazy：提交前后台一定停在 Loading。
//
// 返回的 release 必须被关闭，否则后台 goroutine 永远阻塞在 Read 里。
func newLazyBlocked(t *testing.T) (lz *Lazy, p *Provider, release func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)

	p = NewProvider(ProviderOptions{
		Candidates: []string{"blocked.otf"},
		Read: func(string) ([]byte, error) {
			<-gate
			return nil, os.ErrNotExist
		},
		Embedded: &failingLoader{err: errors.New("测试注入：嵌入不可用")},
	})
	var err error
	lz, err = NewLazy(LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("NewLazy 失败: %v", err)
	}
	return lz, p, release
}

// waitState 等待状态到达 want；超时即失败并打印报告（不静默跳过）。
func waitState(t *testing.T, lz *Lazy, want LoadState, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if lz.State() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待状态 %v 超时（当前 %v）\n报告: %+v", want, lz.State(), lz.Report())
}

// waitInt32 等待计数器到达 want（用于"通知是否发出"这类跨 goroutine 观测）。
func waitInt32(t *testing.T, p *int32, want int32, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(p) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待计数 %d 超时（当前 %d）", want, atomic.LoadInt32(p))
}

// ---------------------------------------------------------------------------
// 1. 状态机
// ---------------------------------------------------------------------------

// TestFontLazyLoadStateMachine 四态迁移合法，且 Ready 之后 Request 是 no-op。
func TestFontLazyLoadStateMachine(t *testing.T) {
	t.Run("Unloaded→Loading→Ready，Ready 后不再重载", func(t *testing.T) {
		lz, cl := newLazySystemHit(t)

		if got := lz.State(); got != LoadUnloaded {
			t.Fatalf("构造后状态应为 unloaded，实际 %v", got)
		}
		lz.Request()
		waitState(t, lz, LoadReady, 5*time.Second)

		before := lz.Report()
		lz.Request() // Ready 之后调用必须是 no-op
		time.Sleep(10 * time.Millisecond)
		after := lz.Report()

		if after.Attempts != before.Attempts {
			t.Errorf("Ready 后 Request 又启动了一轮：Attempts %d→%d", before.Attempts, after.Attempts)
		}
		if cl.calls != 1 {
			t.Errorf("嵌入解析次数 = %d，期望 1（临时主题只构造一次）", cl.calls)
		}
	})

	t.Run("Loading 期间状态稳定为 loading", func(t *testing.T) {
		lz, _, release := newLazyBlocked(t)
		lz.Request()
		if got := lz.State(); got != LoadLoading {
			t.Fatalf("后台卡住时状态应为 loading，实际 %v", got)
		}
		if got := lz.Report().State; got != LoadLoading {
			t.Errorf("报告状态应与状态机一致：Report.State=%v", got)
		}
		release()
		waitState(t, lz, LoadFailed, 5*time.Second)
	})

	t.Run("Failed→Loading 只能由 Request 触发", func(t *testing.T) {
		lz, p := newLazyAllFail(t)
		lz.Request()
		waitState(t, lz, LoadFailed, 5*time.Second)
		scans := p.Scans()

		// 任何读操作都不许把失败态推回 loading。
		for i := 0; i < 50; i++ {
			_ = lz.Theme()
			_ = lz.State()
			_ = lz.Report()
			_ = lz.Take()
		}
		if got := lz.State(); got != LoadFailed {
			t.Errorf("读操作把状态改成了 %v（失败必须是终态，不得隐式重试）", got)
		}
		if p.Scans() != scans {
			t.Errorf("读操作触发了重新扫描：%d→%d", scans, p.Scans())
		}

		lz.Request()
		lz.Request() // 合并：第二轮 Loading 期间的调用不再启动新任务
		waitState(t, lz, LoadFailed, 5*time.Second)
		if got := lz.Report().Attempts; got != 2 {
			t.Errorf("显式重试后 Attempts = %d，期望 2（两次 Request 触发、并发调用合并）", got)
		}
	})
}

// ---------------------------------------------------------------------------
// 2. 构造期零 IO
// ---------------------------------------------------------------------------

// TestFontLazyLoadNoIOOnConstruct 构造不允许任何读取/解析（规范 §3.3）。
func TestFontLazyLoadNoIOOnConstruct(t *testing.T) {
	lz, cl := newLazySystemHit(t)
	rep := lz.Report()

	if cl.calls != 0 {
		t.Errorf("NewLazy 解析了嵌入字体 %d 次——构造期禁止 IO", cl.calls)
	}
	if got := lz.State(); got != LoadUnloaded {
		t.Errorf("构造后状态 = %v，期望 unloaded", got)
	}
	if rep.Attempts != 0 {
		t.Errorf("构造后 Attempts = %d，期望 0", rep.Attempts)
	}
	if rep.TempSource != "" {
		t.Errorf("构造后 TempSource = %q，期望空（临时主题还没构造）", rep.TempSource)
	}
	if th := lz.Theme(); th != nil {
		t.Error("构造后 Theme() 应为 nil（此时没有任何主题，且不得顺手构造）")
	}
}

// ---------------------------------------------------------------------------
// 3. 并发合并
// ---------------------------------------------------------------------------

// TestFontLazyLoadCoalesce N=32 并发 Request 只触发一次加载。
func TestFontLazyLoadCoalesce(t *testing.T) {
	const n = 32
	lz, p, release := newLazyBlocked(t)

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			lz.Request()
		}()
	}
	close(start)
	wg.Wait()

	// 全部 Request 已返回，后台仍卡在 Read。
	//
	// ⚠️ 这里**不能**查 `p.Scans()`：`ResolveWithReport` 在整段探测期间持有
	// `Provider.mu`，探测未结束时去查会一直阻塞到探测结束（会把自己测成死锁）。
	// 所以"只启动了一轮"先由 Lazy 自己的 Attempts 证明——它只受 `Lazy.mu`
	// 保护，与 Provider 无关。
	if got := lz.Report().Attempts; got != 1 {
		t.Errorf("Attempts = %d，期望 1（Loading 期间的 31 次调用必须被合并）", got)
	}
	if got := lz.State(); got != LoadLoading {
		t.Errorf("后台仍在探测时状态应为 loading，实际 %v", got)
	}

	release()
	waitState(t, lz, LoadFailed, 5*time.Second)
	// 探测结束后再查扫描次数：这才是"真的只扫了一遍"的硬证据。
	if got := p.Scans(); got != 1 {
		t.Errorf("32 个并发 Request 最终触发了 %d 次扫描，期望 1（合并）", got)
	}
	if got := lz.Report().Attempts; got != 1 {
		t.Errorf("加载结束后 Attempts 变成 %d——说明并发请求没有被合并", got)
	}
}

// ---------------------------------------------------------------------------
// 4. 不阻塞
// ---------------------------------------------------------------------------

// TestFontLazyLoadNonBlockingOnLoading Request 不等待后台加载器完成。
//
// ⚠️ 边界：本用例**只**证明"不等后台加载器"，**不能**推断首帧预算达标——
// 首帧还含窗口创建与 TempElapsed，那要由 ACCEPT 端到端实测判定。
func TestFontLazyLoadNonBlockingOnLoading(t *testing.T) {
	lz, _, release := newLazyBlocked(t)

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		lz.Request()
		done <- time.Since(start)
	}()

	select {
	case d := <-done:
		// 后台此刻仍卡在 Read，Request 却已经返回：这就是"不等待"。
		if d > time.Second {
			t.Errorf("Request 耗时 %v（后台仍阻塞），说明它在等待加载", d)
		}
		if got := lz.State(); got != LoadLoading {
			t.Errorf("Request 返回后状态应为 loading，实际 %v", got)
		}
		if th := lz.Theme(); th == nil {
			t.Error("Request 返回后必须能立刻拿到临时主题（首帧要用），实际 nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Request 在后台阻塞时没有返回——首帧会被拖住")
	}
	release()
}

// ---------------------------------------------------------------------------
// 5/6. 失败是终态 / 显式重试
// ---------------------------------------------------------------------------

// TestFontLazyLoadNoAutoRetry 失败后不会自动重试。
func TestFontLazyLoadNoAutoRetry(t *testing.T) {
	lz, p := newLazyAllFail(t)
	lz.Request()
	waitState(t, lz, LoadFailed, 5*time.Second)

	scans := p.Scans()
	for i := 0; i < 100; i++ {
		_ = lz.Theme()
	}
	time.Sleep(20 * time.Millisecond)
	if got := p.Scans(); got != scans {
		t.Errorf("连续调用 Theme() 触发了重试：扫描 %d→%d", scans, got)
	}
	if got := lz.Report().Attempts; got != 1 {
		t.Errorf("Attempts = %d，期望 1（只有第一次 Request 触发加载）", got)
	}
}

// TestFontLazyLoadExplicitRetry 只有显式 Request 才能重试。
func TestFontLazyLoadExplicitRetry(t *testing.T) {
	lz, p := newLazyAllFail(t)
	lz.Request()
	waitState(t, lz, LoadFailed, 5*time.Second)
	if got := p.Scans(); got != 1 {
		t.Fatalf("首轮扫描次数 = %d，期望 1", got)
	}

	lz.Request()
	if got := lz.State(); got != LoadLoading && got != LoadFailed {
		t.Errorf("重试后状态 = %v，期望 loading 或 failed", got)
	}
	waitState(t, lz, LoadFailed, 5*time.Second)

	if got := p.Scans(); got != 2 {
		t.Errorf("重试后扫描次数 = %d，期望 2", got)
	}
	if got := lz.Report().Attempts; got != 2 {
		t.Errorf("重试后 Attempts = %d，期望 2", got)
	}
}

// ---------------------------------------------------------------------------
// 7. Theme() 无副作用
// ---------------------------------------------------------------------------

// TestFontLazyLoadThemeHasNoSideEffects Theme() 在任何状态都不触发加载。
func TestFontLazyLoadThemeHasNoSideEffects(t *testing.T) {
	t.Run("Unloaded", func(t *testing.T) {
		lz, cl := newLazySystemHit(t)
		for i := 0; i < 100; i++ {
			_ = lz.Theme()
		}
		if cl.calls != 0 {
			t.Errorf("Theme() 在 Unloaded 下解析了嵌入字体 %d 次", cl.calls)
		}
		if got := lz.Report().Attempts; got != 0 {
			t.Errorf("Theme() 触发了加载：Attempts = %d", got)
		}
	})

	t.Run("Failed", func(t *testing.T) {
		lz, p := newLazyAllFail(t)
		lz.Request()
		waitState(t, lz, LoadFailed, 5*time.Second)
		scans := p.Scans()
		for i := 0; i < 100; i++ {
			_ = lz.Theme()
		}
		if p.Scans() != scans {
			t.Errorf("Theme() 在 Failed 下触发了重试：扫描 %d→%d", scans, p.Scans())
		}
	})
}

// ---------------------------------------------------------------------------
// 8. Take 一次性
// ---------------------------------------------------------------------------

// TestFontLazyLoadTakeOnce Ready 后 Take 只成功一次。
func TestFontLazyLoadTakeOnce(t *testing.T) {
	lz, _ := newLazySystemHit(t)
	lz.Request()
	waitState(t, lz, LoadReady, 5*time.Second)

	first := lz.Take()
	if first == nil {
		t.Fatal("Ready 后第一次 Take() 返回 nil")
	}
	if second := lz.Take(); second != nil {
		t.Error("第二次 Take() 又返回值——一次性语义被破坏（会导致每帧重复替换）")
	}
	// Take 之后 Theme() 必须指向已经交付的那一个，而不是退回临时主题。
	if got := lz.Theme(); got != first {
		t.Error("Take 之后 Theme() 没有指向已交付的真主题")
	}
}

// ---------------------------------------------------------------------------
// 9. 失败保留临时主题
// ---------------------------------------------------------------------------

// TestFontLazyLoadFailureKeepsTemp 失败后界面仍有可用的临时主题且错误可见。
func TestFontLazyLoadFailureKeepsTemp(t *testing.T) {
	lz, _ := newLazyAllFail(t)
	lz.Request()
	waitState(t, lz, LoadFailed, 5*time.Second)

	th := lz.Theme()
	if th == nil {
		t.Fatal("Failed 后 Theme() 返回 nil——界面会没有任何主题可用")
	}
	if got := lz.Report().TempSource; got == "" {
		t.Error("Failed 后 TempSource 为空，无法知道首帧用的是哪套字体")
	}
	if err := lz.Report().Err; !errors.Is(err, contracts.ErrFontNotFound) {
		t.Errorf("Failed 后 Err 应包装 ErrFontNotFound，实际 %v", err)
	}
	if lz.Take() != nil {
		t.Error("Failed 后 Take() 不应有可交付的真主题")
	}
}

// ---------------------------------------------------------------------------
// 10/11. 通知时序
// ---------------------------------------------------------------------------

// TestFontLazyLoadNotifyBeforeRequest 正常绑定：完成后回调恰好一次。
func TestFontLazyLoadNotifyBeforeRequest(t *testing.T) {
	lz, _ := newLazySystemHit(t)

	var calls int32
	if ok := lz.SetNotify(func() { atomic.AddInt32(&calls, 1) }); !ok {
		t.Fatal("Request 之前 SetNotify 应返回 true")
	}
	lz.Request()
	waitState(t, lz, LoadReady, 5*time.Second)
	waitInt32(t, &calls, 1, 2*time.Second)

	time.Sleep(10 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("回调被调用 %d 次，期望恰好 1 次", got)
	}
	if lz.Report().NotifyLateBound {
		t.Error("正常绑定的路径不应置 NotifyLateBound")
	}
}

// TestFontLazyLoadNotifyAfterRequest 晚绑定返回 false，但结果不丢。
func TestFontLazyLoadNotifyAfterRequest(t *testing.T) {
	lz, _, release := newLazyBlocked(t)
	lz.Request() // 先请求、后绑定（反序操作）

	var calls int32
	if ok := lz.SetNotify(func() { atomic.AddInt32(&calls, 1) }); ok {
		t.Fatal("Request 之后 SetNotify 应返回 false")
	}
	if !lz.Report().NotifyLateBound {
		t.Error("晚绑定必须在诊断里可见（NotifyLateBound=true）")
	}

	release()
	waitState(t, lz, LoadFailed, 5*time.Second)
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("未绑定的回调被调用了 %d 次", got)
	}

	// 关键：通知没绑上，但**结果没丢**——失败态与主题都能读到。
	if lz.Report().State != LoadFailed {
		t.Error("晚绑定导致结果丢失：状态不是 failed")
	}
	if lz.Theme() == nil {
		t.Error("晚绑定导致结果丢失：Theme() 为 nil")
	}
}

// ---------------------------------------------------------------------------
// 12/14. 临时主题只构造一次
// ---------------------------------------------------------------------------

// TestFontLazyLoadTempOnlyBuiltOnce 读操作与重试都不会重建临时主题。
func TestFontLazyLoadTempOnlyBuiltOnce(t *testing.T) {
	lz, cl := newLazySystemHit(t)
	lz.Request()
	waitState(t, lz, LoadReady, 5*time.Second)

	base := lz.Report().TempElapsed
	if base <= 0 {
		t.Fatalf("TempElapsed = %v，期望 > 0（必须真的测了那一次构造）", base)
	}
	for i := 0; i < 100; i++ {
		_ = lz.Theme()
		_ = lz.Take()
	}
	if got := cl.calls; got != 1 {
		t.Errorf("嵌入解析次数 = %d，期望 1（临时主题不得重建）", got)
	}
	if got := lz.Report().TempElapsed; got != base {
		t.Errorf("TempElapsed 被改写：%v→%v（这类字段随重试漂移就没有意义了）", base, got)
	}
}

// TestFontLazyLoadTempBuiltOnceConcurrent 32 个 goroutine 并发首次 Request
// 也只解析一次嵌入字体。
//
// 断言的是**真实解析入口的调用次数**（嵌入加载器计数），不是计时字段的大小
// ——后者在小样本上根本不是可靠判据。
func TestFontLazyLoadTempBuiltOnceConcurrent(t *testing.T) {
	const n = 32
	lz, cl := newLazySystemHit(t)

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			lz.Request()
		}()
	}
	close(start)
	wg.Wait()
	waitState(t, lz, LoadReady, 5*time.Second)

	if got := cl.calls; got != 1 {
		t.Errorf("32 个并发首次 Request 解析了嵌入字体 %d 次，期望恰好 1 次", got)
	}
	if got := lz.Report().Attempts; got != 1 {
		t.Errorf("Attempts = %d，期望 1", got)
	}
	if got := lz.Report().TempElapsed; got <= 0 {
		t.Errorf("TempElapsed = %v，期望 > 0", got)
	}
	if got := lz.Report().TempSource; got != tempSourceEmbedded {
		t.Errorf("TempSource = %q，期望 %q", got, tempSourceEmbedded)
	}
}

// ---------------------------------------------------------------------------
// 13. 临时主题不污染最终链诊断
// ---------------------------------------------------------------------------

// TestFontLazyLoadTempDoesNotPolluteProbe 临时主题解析嵌入不得写进 ProbeReport。
func TestFontLazyLoadTempDoesNotPolluteProbe(t *testing.T) {
	lz, cl := newLazySystemHit(t)
	lz.Request()
	waitState(t, lz, LoadReady, 5*time.Second)

	// 前提：临时主题确实解析过嵌入（否则本用例是空的）。
	if cl.calls != 1 {
		t.Fatalf("嵌入被解析 %d 次，期望 1——前提不成立，下面的断言没有意义", cl.calls)
	}

	rep := lz.Report()
	if rep.Probe == nil {
		t.Fatal("Ready 后 Probe 为 nil")
	}
	if rep.Probe.Embedded != nil && rep.Probe.Embedded.Attempted {
		t.Error("临时主题的嵌入解析被写进了 ProbeReport.Embedded——" +
			"B4 的「系统命中 ⇒ 不解析嵌入」断言将失效")
	}
	if rep.Probe.Coverage != nil && rep.Probe.Coverage.Supplemented {
		t.Error("系统覆盖完整时 Coverage.Supplemented 应为 false")
	}
}

// ---------------------------------------------------------------------------
// 15. SetNotify(nil)
// ---------------------------------------------------------------------------

// TestFontLazyLoadSetNotifyNil nil 回调：返回 false、不 panic、永不触发。
func TestFontLazyLoadSetNotifyNil(t *testing.T) {
	lz, _ := newLazySystemHit(t)

	if ok := lz.SetNotify(nil); ok {
		t.Fatal("SetNotify(nil) 应返回 false")
	}
	if !lz.Report().NotifyLateBound {
		t.Error("nil 绑定失败必须在诊断里可见（NotifyLateBound=true）")
	}
	// 失败之后不再给第二次机会：否则"绑定成功与否"就没有确定答案了。
	if ok := lz.SetNotify(func() {}); ok {
		t.Error("nil 绑定失败后再次 SetNotify 应返回 false")
	}

	lz.Request()
	waitState(t, lz, LoadReady, 5*time.Second)
	if lz.Take() == nil {
		t.Error("通知绑定失败不应影响主题交付")
	}
}

// ---------------------------------------------------------------------------
// 16. 失败路径的 CompletedAt
// ---------------------------------------------------------------------------

// TestFontLazyLoadFailureCompletedAt 失败也要记真实后台耗时。
//
// 这是问题 B 的落地证据：`LoadedAt` 在失败时为零，若 `LoadElapsed` 仍按
// `LoadedAt-RequestedAt` 计算就只能是 0 或负数——必须靠 `CompletedAt`。
func TestFontLazyLoadFailureCompletedAt(t *testing.T) {
	lz, _ := newLazyAllFailSlow(t, 2*time.Millisecond)
	lz.Request()
	waitState(t, lz, LoadFailed, 5*time.Second)

	rep := lz.Report()
	if !rep.LoadedAt.IsZero() {
		t.Errorf("失败路径 LoadedAt 必须是零值，实际 %v", rep.LoadedAt)
	}
	if rep.CompletedAt.IsZero() {
		t.Error("失败路径 CompletedAt 必须非零（否则后台耗时无法计算）")
	}
	if rep.CompletedAt.Before(rep.RequestedAt) {
		t.Errorf("CompletedAt(%v) 早于 RequestedAt(%v)", rep.CompletedAt, rep.RequestedAt)
	}
	if rep.LoadElapsed <= 0 {
		t.Errorf("失败路径 LoadElapsed = %v，期望 > 0（失败也是一次真实的后台加载）", rep.LoadElapsed)
	}
	if got, want := rep.LoadElapsed, rep.CompletedAt.Sub(rep.RequestedAt); got != want {
		t.Errorf("LoadElapsed = %v，期望 CompletedAt-RequestedAt = %v", got, want)
	}
	if rep.ProbeElapsed <= 0 {
		t.Errorf("失败路径 ProbeElapsed = %v，期望 > 0（探测跑过就有值）", rep.ProbeElapsed)
	}
}

// ---------------------------------------------------------------------------
// 17. Report 副本（IMPL-1 §5 的深拷贝要求）
// ---------------------------------------------------------------------------

// TestFontLazyLoadReportIsCopy Report() 必须返回深拷贝，改副本不得影响内部状态。
func TestFontLazyLoadReportIsCopy(t *testing.T) {
	lz, _ := newLazyAllFail(t)
	lz.Request()
	waitState(t, lz, LoadFailed, 5*time.Second)

	rep := lz.Report()
	if rep.Probe == nil {
		t.Fatal("失败路径 Probe 为 nil——诊断不可观测")
	}
	if len(rep.Probe.Candidates) == 0 {
		t.Fatal("失败路径 Candidates 为空——无法回答「为什么失败」")
	}

	// 篡改副本的三处嵌套结构，内部状态必须纹丝不动。
	origCandidate := rep.Probe.Candidates[0].Path
	rep.Probe.Candidates[0].Path = "被测试改坏"
	rep.Probe.Candidates = append(rep.Probe.Candidates, CandidateResult{Path: "新增"})

	again := lz.Report()
	if again.Probe.Candidates[0].Path != origCandidate {
		t.Errorf("改动副本污染了内部 Candidates：%q", again.Probe.Candidates[0].Path)
	}
	if len(again.Probe.Candidates) != len(rep.Probe.Candidates)-1 {
		t.Errorf("append 副本影响了内部切片长度：%d", len(again.Probe.Candidates))
	}

	// Error 字段按值复制，改副本字段不得影响内部。
	rep.State = LoadReady
	if got := lz.Report().State; got != LoadFailed {
		t.Errorf("改副本 State 影响了内部状态：%v", got)
	}
}
