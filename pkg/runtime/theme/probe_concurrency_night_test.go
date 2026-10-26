package theme

// NIGHT-FONT-CONCURRENCY-MUTATION（ZCode，2026-10-27）
//
// 本文件是夜间任务 07 的并发与快照隔离证据（第一阶段），只新增测试，
// 不修改任何生产代码。M13/M14/M15 生产变异在独立克隆内临时进行，
// 每次实验后必须逐字节恢复到固定基线 d0c547be 并核对 SHA-256。
//
// # 覆盖的并发不变量（与任务书五项一一对应）
//
//  1. 共享 Provider 的多个 Lazy 并发 Request：探测合并/串行缓存，
//     以 Scans 与 Read 计数证明，不靠"结果看起来一样"推断；
//  2. 冷加载进行期间多个 goroutine 读取 Report/State：每份报告自身
//     字段组合满足契约（快照自洽），终态后继续满足；
//  3. 冷加载完成后的并发 warm Lazy：ProbeRan=false、ProbeElapsed==0、
//     历史诊断保留、无重扫（Scans 与 Read 计数不变）；
//  4. Report 深拷贝隔离：污染返回副本的 Candidates / Embedded /
//     Coverage.Missing 不影响内部快照、后续读取与 Provider 缓存；
//  5. 失败路径不缓存与显式重试：Loading 期 Request 合并（Attempts 不涨）、
//     失败不缓存（resolved 为空、Scans 随重试增长）、Err 包装 ErrFontNotFound。
//
// # 执行纪律
//
//  - 启动顺序用 barrier（arrive/start 通道）显式控制，观测步进用
//    固定 seed（nightZCFixedSeed）的确定性 rand，失败时打印 seed；
//  - 计数器与 Read 注入自身线程安全（mutex/atomic）；
//  - 所有 goroutine 有界回收（WaitGroup / 通道 / 测试超时）；
//  - 不并发渲染、不共享 Shaper；不比较两次独立 Report() 并声称原子同刻。

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/goxml/goxml/contracts"
)

// nightZCFixedSeed 固定读取顺序的选择；不控制或重放 Go 调度器交错。
const nightZCFixedSeed = int64(20261027)

// nightZCReadCounter 是线程安全的计数型 Read 注入。
//
// gate 非 nil 时 Read 阻塞在 gate 上（用于制造确定的 Loading 窗口）；
// fail 为真时返回 os.ErrNotExist（失败路径）。
type nightZCReadCounter struct {
	mu    sync.Mutex
	calls int
	gate  chan struct{}
	fail  bool
}

func (c *nightZCReadCounter) read(path string) ([]byte, error) {
	c.mu.Lock()
	c.calls++
	gate := c.gate
	c.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if c.readShouldFail() {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(path)
}

func (c *nightZCReadCounter) readShouldFail() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fail
}

func (c *nightZCReadCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *nightZCReadCounter) openGate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gate != nil {
		close(c.gate)
		c.gate = nil
	}
}

// nightZCInlineLoader 是返回固定字节的嵌入兜底加载器（B-3b 嵌入路径用）。
type nightZCInlineLoader struct {
	data []byte
	name string
}

func (l nightZCInlineLoader) Load() ([]byte, string, error) {
	return l.data, l.name, nil
}

// nightZCHitProvider 构造"系统命中"Provider：单一候选、计数 Read、
// 固定步长注入时钟、嵌入不可用（保证命中路径不触碰嵌入）。
func nightZCHitProvider(t *testing.T, clk *accept07Clock, rc *nightZCReadCounter) *Provider {
	t.Helper()
	good := writeFont(t, t.TempDir(), "nightzc_good.otf", hanziFontBytes(t))
	return NewProvider(ProviderOptions{
		Candidates: []string{good},
		Timeout:    time.Second,
		Read:       rc.read,
		Now:        clk.Next,
		Embedded:   &failingLoader{err: errors.New("nightZC：嵌入不可用（系统命中用例）")},
	})
}

// nightZCMissProvider 构造"系统全失败 + 嵌入兜底成功"Provider：
// rep.Embedded 非 nil、rep.Candidates 为缺失记录，整体解析成功（Ready）。
func nightZCMissProvider(t *testing.T, clk *accept07Clock) *Provider {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "nightzc-missing.otf")
	return NewProvider(ProviderOptions{
		Candidates: []string{missing},
		Read: func(string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		Now: clk.Next,
		Embedded: nightZCInlineLoader{
			data: hanziFontBytes(t),
			name: "nightzc-embedded.otf",
		},
	})
}

// nightZCBarrier 返回启动门闩：n 个 goroutine 各自 arrive 一次，
// 协调者等满 n 个 arrive 后 close(start)，从而固定启动顺序。
func nightZCBarrier(n int) (arrive chan struct{}, start chan struct{}) {
	arrive = make(chan struct{}, n)
	start = make(chan struct{})
	return arrive, start
}

// nightZCWaitTerminal 有界等待 Lazy 进入终态（Ready 或 Failed），返回终态。
func nightZCWaitTerminal(t *testing.T, lz *Lazy, d time.Duration) LoadState {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		if s := lz.State(); s == LoadReady || s == LoadFailed {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("nightZC：等待终态超时（%v），当前 state=%s", d, lz.State())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// nightZCValidateShape 校验一份报告自身的字段组合契约（层次 B 快照自洽）。
//
// 只校验**同一份报告内部**的一致性；State() 与 Report() 是两次独立加锁读取，
// 两者的时点差异不构成失败（任务书明确禁止声称跨调用原子同刻）。
func nightZCValidateShape(t *testing.T, rep LoadReport, where string) {
	t.Helper()
	if rep.Family == "" {
		t.Errorf("[%s] Family 为空", where)
	}
	if rep.RequestedAt.IsZero() {
		t.Errorf("[%s] RequestedAt 为零（Request 后必非零）", where)
	}
	if rep.TempSource == "" {
		t.Errorf("[%s] TempSource 为空（Request 同步构造临时主题后必非空）", where)
	}
	if rep.NotifyLateBound {
		t.Errorf("[%s] NotifyLateBound 为 true（本测试从未失败绑定）", where)
	}
	if !rep.FrameSubmittedAt.IsZero() {
		t.Errorf("[%s] FrameSubmittedAt 非零（本测试无渲染线程）", where)
	}
	if rep.FirstLayoutAfterSwap != 0 {
		t.Errorf("[%s] FirstLayoutAfterSwap 非零（本测试无渲染线程）", where)
	}
	if rep.ProbeElapsed < 0 {
		t.Errorf("[%s] ProbeElapsed 为负", where)
	}
	switch rep.State {
	case LoadLoading:
		if !rep.LoadedAt.IsZero() {
			t.Errorf("[%s] Loading 却 LoadedAt 非零", where)
		}
		if !rep.CompletedAt.IsZero() {
			t.Errorf("[%s] Loading 却 CompletedAt 非零", where)
		}
		if rep.LoadElapsed != 0 {
			t.Errorf("[%s] Loading 却 LoadElapsed=%v", where, rep.LoadElapsed)
		}
		if rep.Err != nil {
			t.Errorf("[%s] Loading 却 Err=%v", where, rep.Err)
		}
		if rep.Probe != nil {
			t.Errorf("[%s] Loading 却 Probe 非nil（Request 已清零，终态才写入）", where)
		}
		if rep.ProbeElapsed != 0 {
			t.Errorf("[%s] Loading 却 ProbeElapsed=%v", where, rep.ProbeElapsed)
		}
	case LoadReady:
		if rep.LoadedAt.IsZero() {
			t.Errorf("[%s] Ready 却 LoadedAt 为零", where)
		}
		if rep.CompletedAt.IsZero() {
			t.Errorf("[%s] Ready 却 CompletedAt 为零", where)
		}
		if rep.Err != nil {
			t.Errorf("[%s] Ready 却 Err=%v", where, rep.Err)
		}
		if want := rep.CompletedAt.Sub(rep.RequestedAt); rep.LoadElapsed != want {
			t.Errorf("[%s] LoadElapsed=%v ≠ CompletedAt-RequestedAt=%v",
				where, rep.LoadElapsed, want)
		}
		if rep.Probe == nil {
			t.Errorf("[%s] Ready 却 Probe 为 nil", where)
		} else {
			if rep.Probe.Resolved == "" {
				t.Errorf("[%s] Ready 却 Probe.Resolved 为空", where)
			}
			if len(rep.Probe.Candidates) == 0 {
				t.Errorf("[%s] Ready 却 Candidates 为空", where)
			}
		}
	case LoadFailed:
		if rep.Err == nil || !errors.Is(rep.Err, contracts.ErrFontNotFound) {
			t.Errorf("[%s] Failed 却 Err 不满足 ErrFontNotFound 包装: %v", where, rep.Err)
		}
		if !rep.LoadedAt.IsZero() {
			t.Errorf("[%s] Failed 却 LoadedAt 非零（失败不得伪造产出）", where)
		}
		if rep.CompletedAt.IsZero() {
			t.Errorf("[%s] Failed 却 CompletedAt 为零（成败都非零）", where)
		}
		if rep.Probe == nil {
			t.Errorf("[%s] Failed 却 Probe 为 nil（失败也有探测报告）", where)
		}
	default:
		t.Errorf("[%s] 非预期状态 %s", where, rep.State)
	}
	if rep.Probe != nil {
		if !rep.Probe.ProbeRan && rep.Probe.Elapsed != 0 {
			// 命中副本允许保留历史 Elapsed（D-ACCEPT-07 裁决②）；
			// ProbeRan=false 且 Elapsed==0 是"本轮未探测"的合法组合。
			// 这里不判失败：命中副本的历史 Elapsed 是契约允许的组合。
			_ = rep.Probe.Elapsed
		}
	}
}

// TestNight07ConcurrentSharedProviderMergesProbe 覆盖不变量 1：
// 全新共享 Provider 上 16 个 Lazy 并发 Request，探测合并/串行缓存，
// 以 Scans=1 与 Read 计数=1 证明，且恰好一份报告 ProbeRan=true。
func TestNight07ConcurrentSharedProviderMergesProbe(t *testing.T) {
	const n = 16
	clk := newAccept07Clock(1 * time.Millisecond)
	rc := &nightZCReadCounter{}
	p := nightZCHitProvider(t, clk, rc)

	lzs := make([]*Lazy, n)
	for i := range lzs {
		lz, err := NewLazy(LazyOptions{Provider: p})
		if err != nil {
			t.Fatalf("NewLazy 失败: %v", err)
		}
		lzs[i] = lz
	}

	arrive, start := nightZCBarrier(n)
	var wg sync.WaitGroup
	for i := range lzs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			arrive <- struct{}{}
			<-start
			lzs[i].Request()
		}(i)
	}
	for i := 0; i < n; i++ {
		<-arrive
	}
	close(start)
	wg.Wait()

	for i := range lzs {
		if st := nightZCWaitTerminal(t, lzs[i], 5*time.Second); st != LoadReady {
			t.Errorf("Lazy#%d 终态=%s，期望 Ready", i, st)
		}
	}

	// 合并/串行缓存的可证证据：一次扫描、一次读取。
	if got := p.Scans(); got != 1 {
		t.Errorf("Scans=%d，期望恰好 1（共享 Provider 的并发探测应串行合并）", got)
	}
	if got := rc.count(); got != 1 {
		t.Errorf("Read 调用=%d，期望恰好 1", got)
	}

	ranTrue, ranFalse := 0, 0
	for i := range lzs {
		rep := lzs[i].Report()
		if rep.State != LoadReady {
			t.Errorf("Lazy#%d 状态=%s", i, rep.State)
			continue
		}
		if rep.Probe == nil {
			t.Errorf("Lazy#%d Probe=nil", i)
			continue
		}
		if len(rep.Probe.Candidates) != 1 || rep.Probe.Candidates[0].Status != StatusOK {
			t.Errorf("Lazy#%d 候选异常: %+v", i, rep.Probe.Candidates)
		}
		if rep.Probe.Resolved == "" {
			t.Errorf("Lazy#%d Probe.Resolved 为空", i)
		}
		if rep.Probe.ProbeRan {
			ranTrue++
		} else {
			ranFalse++
		}
	}
	if ranTrue != 1 || ranFalse != n-1 {
		t.Errorf("探测合并不成立: ProbeRan=true %d 份 / false %d 份（期望 1/%d）",
			ranTrue, ranFalse, n-1)
	}

	for i := range lzs {
		if lzs[i].Theme() == nil {
			t.Errorf("Lazy#%d Theme() 为 nil（Request 后 apply 应指向临时主题）", i)
		}
	}
}

// TestNight07ConcurrentReadersSeeConsistentSnapshots 覆盖不变量 2：
// 八个读者先观测 Loading，再与后台终态提交并发读取，最后各自读取终态。
// 通道保证两侧阶段均执行；固定 seed 仅选择读取顺序，不控制调度器。
func TestNight07ConcurrentReadersSeeConsistentSnapshots(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	rc := &nightZCReadCounter{gate: make(chan struct{})}
	p := nightZCHitProvider(t, newAccept07Clock(time.Millisecond), rc)
	lz, err := NewLazy(LazyOptions{Provider: p})
	if err != nil {
		t.Fatal(err)
	}
	lz.Request()
	if lz.State() != LoadLoading {
		t.Fatalf("前提不成立：Request 后 state=%s，期望 loading", lz.State())
	}

	const readers = 8
	loading := make(chan int, readers)
	terminal := make(chan int, readers)
	done := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(nightZCFixedSeed + int64(r)))
			for i := 0; i < 24; i++ {
				rep := lz.Report()
				nightZCValidateShape(t, rep,
					fmt.Sprintf("loading reader%d iter%d seed%d", r, i, nightZCFixedSeed))
				if rep.State != LoadLoading {
					t.Errorf("reader%d left Loading before the gate opened: %s", r, rep.State)
				}
			}
			loading <- r
			for {
				if ctx.Err() != nil {
					return
				}
				if rng.Intn(2) == 0 {
					_ = lz.State() // Separate read, not an atomic pair with Report.
				}
				rep := lz.Report()
				nightZCValidateShape(t, rep, fmt.Sprintf("transition reader%d", r))
				if rep.State == LoadReady {
					break
				}
				if rep.State == LoadFailed {
					t.Errorf("reader%d: success fixture failed: %v", r, rep.Err)
					return
				}
				runtime.Gosched()
			}
			for i := 0; i < 64; i++ {
				rep := lz.Report()
				nightZCValidateShape(t, rep, fmt.Sprintf("terminal reader%d iter%d", r, i))
				if rep.State != LoadReady {
					t.Errorf("reader%d terminal state regressed: %s", r, rep.State)
				}
			}
			terminal <- r
		}(r)
	}
	go func() { wg.Wait(); close(done) }()
	t.Cleanup(func() {
		cancel()
		rc.openGate()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("reader goroutines did not stop")
		}
	})
	for i := 0; i < readers; i++ {
		select {
		case <-loading:
		case <-ctx.Done():
			t.Fatal("readers did not all observe Loading")
		}
	}
	rc.openGate()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("readers did not finish the terminal phase")
	}
	if len(terminal) != readers {
		t.Fatalf("terminal phase completed by %d/%d readers", len(terminal), readers)
	}
	t.Logf("%d readers each observed Loading, transition, and 64 terminal snapshots", readers)
}

// TestNight07ConcurrentWarmLaziesNoRescan 覆盖不变量 3：
// 冷加载完成后 12 个 warm Lazy 并发 Request：ProbeRan=false、
// ProbeElapsed==0、历史诊断保留、Scans 与 Read 计数不变（无重扫）。
func TestNight07ConcurrentWarmLaziesNoRescan(t *testing.T) {
	const n = 12
	clk := newAccept07Clock(1 * time.Millisecond)
	rc := &nightZCReadCounter{}
	p := nightZCHitProvider(t, clk, rc)

	_, rep0, err0 := p.ResolveWithReport(FamilyCJK)
	if err0 != nil || rep0 == nil || !rep0.ProbeRan {
		t.Fatalf("冷加载前提不成立: err=%v rep=%v", err0, rep0)
	}
	scans0 := p.Scans()
	reads0 := rc.count()
	if scans0 != 1 || reads0 != 1 {
		t.Fatalf("冷加载前提异常: Scans=%d reads=%d", scans0, reads0)
	}

	lzs := make([]*Lazy, n)
	for i := range lzs {
		lz, err := NewLazy(LazyOptions{Provider: p})
		if err != nil {
			t.Fatalf("NewLazy 失败: %v", err)
		}
		lzs[i] = lz
	}
	arrive, start := nightZCBarrier(n)
	var wg sync.WaitGroup
	for i := range lzs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			arrive <- struct{}{}
			<-start
			lzs[i].Request()
		}(i)
	}
	for i := 0; i < n; i++ {
		<-arrive
	}
	close(start)
	wg.Wait()

	for i := range lzs {
		if st := nightZCWaitTerminal(t, lzs[i], 5*time.Second); st != LoadReady {
			t.Errorf("Lazy#%d 终态=%s", i, st)
			continue
		}
		rep := lzs[i].Report()
		if rep.Probe == nil {
			t.Errorf("Lazy#%d warm 却 Probe=nil（历史诊断应保留）", i)
			continue
		}
		if rep.Probe.ProbeRan {
			t.Errorf("Lazy#%d warm 却 ProbeRan=true（应走缓存命中副本）", i)
		}
		// M15 的判别点：命中路径去掉 ProbeRan 门控后，
		// ProbeElapsed 会被历史探测耗时污染（非 0）。
		if rep.ProbeElapsed != 0 {
			t.Errorf("Lazy#%d warm 却 ProbeElapsed=%v（期望 0，本轮未探测）", i, rep.ProbeElapsed)
		}
		if len(rep.Probe.Candidates) == 0 || rep.Probe.Candidates[0].Status != StatusOK {
			t.Errorf("Lazy#%d 历史诊断丢失: %+v", i, rep.Probe.Candidates)
		}
		if rep.Probe.Resolved != rep0.Resolved {
			t.Errorf("Lazy#%d 历史路径漂移: %q ≠ %q", i, rep.Probe.Resolved, rep0.Resolved)
		}
	}
	if got := p.Scans(); got != scans0 {
		t.Errorf("warm 触发重扫: Scans %d → %d", scans0, got)
	}
	if got := rc.count(); got != reads0 {
		t.Errorf("warm 触发重读: reads %d → %d", reads0, got)
	}
}

// TestNight07ConcurrentReportDeepCopyIsolation 覆盖不变量 4：
// 污染返回副本的 Candidates / Coverage.Missing / Embedded 不影响
// 内部快照、后续 Report() 与 Provider 缓存。分两个子场景：
// 系统命中路径（Candidates+Coverage）与嵌入兜底路径（Embedded）。
func TestNight07ConcurrentReportDeepCopyIsolation(t *testing.T) {
	t.Run("CandidatesAndCoverage", func(t *testing.T) {
		clk := newAccept07Clock(1 * time.Millisecond)
		rc := &nightZCReadCounter{}
		p := nightZCHitProvider(t, clk, rc)
		lz, err := NewLazy(LazyOptions{Provider: p})
		if err != nil {
			t.Fatalf("NewLazy 失败: %v", err)
		}
		lz.Request()
		if st := nightZCWaitTerminal(t, lz, 5*time.Second); st != LoadReady {
			t.Fatalf("终态=%s", st)
		}
		rep1 := lz.Report()
		if rep1.Probe == nil || len(rep1.Probe.Candidates) == 0 || rep1.Probe.Coverage == nil {
			t.Fatalf("fixture 前提：成功路径应有 Candidates 与 Coverage")
		}
		origPath := rep1.Probe.Candidates[0].Path

		// 污染返回副本（层次 B 的真实可变字段）。
		rep1.Probe.Candidates[0].Path = "nightZC-MUTATED"
		rep1.State = LoadFailed
		rep1.Err = errors.New("nightZC 污染注入")

		// 后续读取与内部快照不受影响。
		rep2 := lz.Report()
		if rep2.State != LoadReady {
			t.Errorf("副本 State 污染内部: %s", rep2.State)
		}
		if rep2.Err != nil {
			t.Errorf("副本 Err 污染内部: %v", rep2.Err)
		}
		if rep2.Probe == nil || rep2.Probe.Candidates[0].Path != origPath {
			t.Errorf("Candidates 污染内部快照: %+v", rep2.Probe)
		}
		if rep2.Probe == nil || rep2.Probe.Coverage == nil {
			t.Fatalf("Coverage 丢失")
		}
		// 同包内部直查：Lazy.rep 与 Provider.last 均不得被污染。
		if lz.rep.Probe == nil || lz.rep.Probe.Candidates[0].Path != origPath {
			t.Errorf("污染进入 Lazy 内部 rep")
		}
		if p.last == nil || len(p.last.Candidates) == 0 ||
			p.last.Candidates[0].Path != origPath {
			t.Errorf("污染进入 Provider.last")
		}
		// Provider 缓存命中派生副本（M14 的判别点）：
		_, repP, errP := p.ResolveWithReport(FamilyCJK)
		if errP != nil {
			t.Fatalf("缓存命中解析失败: %v", errP)
		}
		if repP.ProbeRan {
			t.Errorf("缓存命中应 ProbeRan=false")
		}
		if len(repP.Candidates) > 0 && repP.Candidates[0].Path != origPath {
			t.Errorf("Provider 命中副本被污染（M14 判别力证据）: %q",
				repP.Candidates[0].Path)
		}
	})

	t.Run("Embedded", func(t *testing.T) {
		clk := newAccept07Clock(1 * time.Millisecond)
		p := nightZCMissProvider(t, clk)
		lz, err := NewLazy(LazyOptions{Provider: p})
		if err != nil {
			t.Fatalf("NewLazy 失败: %v", err)
		}
		lz.Request()
		if st := nightZCWaitTerminal(t, lz, 5*time.Second); st != LoadReady {
			t.Fatalf("嵌入兜底路径终态=%s，期望 Ready", st)
		}
		rep1 := lz.Report()
		if rep1.Probe == nil || rep1.Probe.Embedded == nil {
			t.Fatalf("fixture 前提：嵌入路径应有 Embedded 诊断")
		}
		origID := rep1.Probe.Embedded.ID
		origBytes := rep1.Probe.Embedded.Bytes
		if origID == "" || origBytes <= 0 {
			t.Fatalf("fixture 前提异常: ID=%q Bytes=%d", origID, origBytes)
		}

		rep1.Probe.Embedded.ID = "nightZC-MUTATED"
		rep1.Probe.Embedded.Bytes = -1

		rep2 := lz.Report()
		if rep2.Probe == nil || rep2.Probe.Embedded == nil {
			t.Fatalf("第二次读取 Embedded 丢失")
		}
		if rep2.Probe.Embedded.ID != origID {
			t.Errorf("Embedded.ID 污染内部: %q → %q", origID, rep2.Probe.Embedded.ID)
		}
		if rep2.Probe.Embedded.Bytes != origBytes {
			t.Errorf("Embedded.Bytes 污染内部: %d → %d", origBytes, rep2.Probe.Embedded.Bytes)
		}
		if p.last == nil || p.last.Embedded == nil || p.last.Embedded.ID != origID {
			t.Errorf("污染进入 Provider.last 的 Embedded")
		}
	})
}

// TestNight07ConcurrentNonemptyMissingSlice 专门验证已有元素的深拷贝。
// 对空切片 append 可能分配新存储，不能用来排除浅拷贝。
func TestNight07ConcurrentNonemptyMissingSlice(t *testing.T) {
	lz, err := NewLazy(LazyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// No Request or background goroutine: seed only the diagnostic fixture.
	lz.rep.Probe = &ProbeReport{Coverage: &CoverageResult{Missing: []rune{'缺'}}}
	rep := lz.Report()
	rep.Probe.Coverage.Missing[0] = '改'
	if got := lz.Report().Probe.Coverage.Missing[0]; got != '缺' {
		t.Fatalf("Missing slice aliases internal storage: %q", got)
	}
}

// TestNight07ConcurrentFailPathMergeAndRetry 覆盖不变量 5：
// Loading 期并发 Request 合并（Attempts 不涨）；失败不缓存；
// 失败报告满足 ErrFontNotFound 包装与 LoadedAt 为零；显式重试 Attempts=2。
func TestNight07ConcurrentFailPathMergeAndRetry(t *testing.T) {
	lz, p, release := newLazyBlocked(t)
	lz.Request()
	if lz.State() != LoadLoading {
		t.Fatalf("前提不成立：Request 后 state=%s", lz.State())
	}

	// Loading 期 6 个并发 Request：全部合并，Attempts 保持 1。
	const merges = 6
	var wg sync.WaitGroup
	arrive, start := nightZCBarrier(merges)
	for i := 0; i < merges; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			arrive <- struct{}{}
			<-start
			lz.Request()
		}()
	}
	for i := 0; i < merges; i++ {
		<-arrive
	}
	close(start)
	wg.Wait()
	if got := lz.Report().Attempts; got != 1 {
		t.Fatalf("Loading 期 Request 应合并：Attempts=%d，期望 1", got)
	}

	// 放行 Read → 全候选缺失 → 嵌入兜底失败 → Failed。
	release()
	if st := nightZCWaitTerminal(t, lz, 5*time.Second); st != LoadFailed {
		t.Fatalf("终态=%s，期望 Failed", st)
	}
	rep1 := lz.Report()
	nightZCValidateShape(t, rep1, "failed-1")
	if rep1.Attempts != 1 {
		t.Errorf("失败后 Attempts=%d，期望 1", rep1.Attempts)
	}
	if rep1.Err == nil || !errors.Is(rep1.Err, contracts.ErrFontNotFound) {
		t.Errorf("失败 Err 应包装 ErrFontNotFound: %v", rep1.Err)
	}
	if !rep1.LoadedAt.IsZero() {
		t.Errorf("失败却 LoadedAt 非零: %v", rep1.LoadedAt)
	}
	if rep1.CompletedAt.IsZero() {
		t.Errorf("失败却 CompletedAt 为零")
	}
	if rep1.Probe == nil || !rep1.Probe.ProbeRan {
		t.Errorf("失败路径的探测报告应 ProbeRan=true（本轮真的探测过）: %+v", rep1.Probe)
	} else if len(rep1.Probe.Candidates) != 1 ||
		rep1.Probe.Candidates[0].Status != StatusMissing {
		t.Errorf("失败候选状态异常: %+v", rep1.Probe.Candidates)
	}
	firstCompleted := rep1.CompletedAt

	// 显式重试：Attempts=2，CompletedAt 前移，仍失败。
	lz.Request()
	if st := nightZCWaitTerminal(t, lz, 5*time.Second); st != LoadFailed {
		t.Fatalf("重试后终态=%s，期望 Failed", st)
	}
	rep2 := lz.Report()
	nightZCValidateShape(t, rep2, "failed-2")
	if rep2.Attempts != 2 {
		t.Errorf("显式重试后 Attempts=%d，期望 2", rep2.Attempts)
	}
	if rep2.CompletedAt.IsZero() || !rep2.CompletedAt.After(firstCompleted) {
		t.Errorf("第二次 CompletedAt 应晚于第一次: %v → %v", firstCompleted, rep2.CompletedAt)
	}

	// 失败不缓存（同包内部断言）：resolved 为空、Scans=2、每次探测一次 Read。
	if len(p.resolved) != 0 {
		t.Errorf("失败路径不应缓存 resolved: %v", p.resolved)
	}
	if got := p.Scans(); got != 2 {
		t.Errorf("两次失败尝试应两次探测: Scans=%d", got)
	}
}
