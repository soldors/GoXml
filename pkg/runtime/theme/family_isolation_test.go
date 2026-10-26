package theme

// R3-DS-FAMILY-BOUNDARY：现有「CJK 缓存 与 未实现/未知 family 失败路径」的隔离边界回归。
//
// # 本文件证明什么 / 不证明什么（证据强度边界，务必按此引用）
//
// 证明（可真实执行）：
//   - 一个 Provider 解析 CJK 成功后，请求 Body / Mono / 未知 family **严格失败**且
//     包 `contracts.ErrFontNotFound`，不伪装成功、不写 CJK 的缓存键；
//   - 失败之后再次请求 CJK，**仍命中同一缓存 ID**，`Scans()` 不增加，
//     历史诊断未被这些失败改写；
//   - 预热 Provider 上 CJK warm Lazy 与不支持 family 的 Lazy 可并发，各自报告互不污染；
//   - 失败 family 的显式重试仍失败，且不触发 CJK 重扫。
//
// **不证明**：
//   - **不**证明 Body / Mono 的探测已实现（A 阶段只探测 CJK）；
//   - **不**是「跨 family 成功路径」的验收证据；
//   - **不**因此关闭 `D-ACCEPT-07`（其跨 family 项要求两个**已实现**族的端到端验证）。
//
// 生产代码只读：本文件不新增公共 API、不做生产变异、不修改任何既有测试。
// 所有助手统一 `familyBoundary` 前缀，避免与其他 Agent 在同包内新增的助手重名。

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// 助手
// ---------------------------------------------------------------------------

// familyBoundaryProvider 构造一个带读计数包装的真实 Provider。
//
// 前置：本机需存在可用的 CJK 系统字体（或可用的嵌入兜底）。若 CJK 本身无法就绪，
// 相关用例会明确 Fatal 说明前提不成立，**不得**当作通过。
type familyBoundaryReadCounter struct {
	mu    sync.Mutex
	calls int
}

func (c *familyBoundaryReadCounter) read(string) ([]byte, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	// 让候选不可读，迫使流程走嵌入兜底或失败——本文件的关注点是
	// **family 级缓存隔离**，不是候选内容。
	return nil, errors.New("family-boundary: candidate unreadable by design")
}

func (c *familyBoundaryReadCounter) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func familyBoundaryNewProvider() (*Provider, *familyBoundaryReadCounter) {
	rc := &familyBoundaryReadCounter{}
	p := NewProvider(ProviderOptions{Read: rc.read})
	return p, rc
}

// familyBoundaryRequireCJKReady 要求 CJK 能成功解析；否则说明本机前提不成立。
func familyBoundaryRequireCJKReady(t *testing.T, p *Provider) string {
	t.Helper()
	id, rep, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("CJK 解析失败，本机前提不成立（本用例不得当作通过）: %v", err)
	}
	if id == "" {
		t.Fatal("CJK 解析成功但 ID 为空")
	}
	if rep == nil {
		t.Fatal("CJK 解析成功但诊断报告为 nil")
	}
	if !rep.ProbeRan {
		t.Fatal("CJK 首次解析后 ProbeRan=false：不是本轮探测，前提不成立")
	}
	return id
}

// familyBoundaryRequireStrictFailure 要求该 family 严格失败且包 ErrFontNotFound。
func familyBoundaryRequireStrictFailure(t *testing.T, p *Provider, family string) {
	t.Helper()
	id, rep, err := p.ResolveWithReport(family)
	if err == nil {
		t.Fatalf("family %q 不应成功（A 阶段未实现探测），却返回 id=%q", family, id)
	}
	if !errors.Is(err, contracts.ErrFontNotFound) {
		t.Errorf("family %q 的错误必须能用 errors.Is 判定为 ErrFontNotFound，实际: %v", family, err)
	}
	if id != "" {
		t.Errorf("family %q 失败时不应返回非空 ID，实际 %q", family, id)
	}
	if rep != nil {
		t.Errorf("family %q 失败时不应返回诊断报告（避免调用方误读为成功），实际非 nil", family)
	}
}

// ---------------------------------------------------------------------------
// 1 + 2：CJK 成功后，失败 family 不污染 CJK 缓存与诊断
// ---------------------------------------------------------------------------

// TestFamilyBoundaryCJKCacheSurvivesUnimplementedFamilyFailures 覆盖目标 1 与 2。
func TestFamilyBoundaryCJKCacheSurvivesUnimplementedFamilyFailures(t *testing.T) {
	p, rc := familyBoundaryNewProvider()

	// —— 第一次：CJK 成功，记录基线 ——
	cjkID := familyBoundaryRequireCJKReady(t, p)
	scansAfterCJK := p.Scans()
	readsAfterCJK := rc.Calls()
	lastAfterCJK := p.LastReport()
	if lastAfterCJK == nil {
		t.Fatal("CJK 成功后 LastReport() 为 nil")
	}
	histElapsed := lastAfterCJK.Elapsed
	if scansAfterCJK == 0 {
		t.Fatalf("CJK 成功后 Scans()=0：无法作为后续「未增加」的基线")
	}

	// —— 依次请求未实现 / 未知 family，必须严格失败 ——
	for _, fam := range []string{FamilyBody, FamilyMono, "NotAFamily", ""} {
		if fam == "" {
			// 空 family 在 Lazy 层被归一为 CJK，Provider 层则走 unknown 分支；
			// 这里只测 Provider 的直接行为，避免与 Lazy 的归一化混淆。
			continue
		}
		familyBoundaryRequireStrictFailure(t, p, fam)
	}

	// —— 失败之后：CJK 缓存仍命中同一 ID，且未重扫 ——
	id2, rep2, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("失败 family 之后 CJK 解析出错（缓存应仍可用）: %v", err)
	}
	if id2 != cjkID {
		t.Errorf("CJK 缓存 ID 变化：%q → %q（失败 family 不应影响 CJK 缓存键）", cjkID, id2)
	}
	if got := p.Scans(); got != scansAfterCJK {
		t.Errorf("失败 family 之后 CJK 发生重扫：Scans %d → %d", scansAfterCJK, got)
	}
	if got := rc.Calls(); got != readsAfterCJK {
		t.Errorf("失败 family 之后出现新的磁盘读取：Reads %d → %d", readsAfterCJK, got)
	}
	if rep2 == nil {
		t.Fatal("CJK 缓存命中后诊断报告为 nil")
	}
	// 命中路径：本轮未探测。
	if rep2.ProbeRan {
		t.Errorf("CJK 缓存命中路径 ProbeRan=true，期望 false（本轮没有执行探测）")
	}
	// 历史诊断必须保留，不被失败 family 改写。
	//
	// ⚠ 不能只比 `Elapsed`：本机 `time.Now()` 粒度约 0.5ms，CJK 一次探测的
	// `Probe.Elapsed` 实测可能读成 0（本次观测即 `历史 Elapsed=0s`），
	// 那样 `0 == 0` 是**空判据**。因此改用与计时无关的结构化字段：
	// 命中返回的是派生副本，历史 `Candidates` / `Resolved` / `Scanned`
	// 必须与首次探测逐项一致；被别的 family 失败改写时这些字段会变。
	if histScan := lastAfterCJK.Scanned; rep2.Scanned != histScan {
		t.Errorf("历史诊断被污染：Scanned %d → %d", histScan, rep2.Scanned)
	}
	if rep2.Resolved != lastAfterCJK.Resolved {
		t.Errorf("历史诊断被污染：Resolved %q → %q", lastAfterCJK.Resolved, rep2.Resolved)
	}
	if len(rep2.Candidates) != len(lastAfterCJK.Candidates) {
		t.Errorf("历史诊断被污染：Candidates 数量 %d → %d",
			len(lastAfterCJK.Candidates), len(rep2.Candidates))
	}
	// Elapsed 只作**记录**：非零时要求相等，为零时不得据此判定"未污染"。
	if histElapsed != 0 && rep2.Elapsed != histElapsed {
		t.Errorf("历史诊断被污染：Probe.Elapsed %v → %v", histElapsed, rep2.Elapsed)
	}

	t.Logf("CJK id=%q Scans %d→%d Reads %d→%d；Body/Mono/unknown 均严格失败；"+
		"命中路径 ProbeRan=%v 历史 Elapsed=%v",
		cjkID, scansAfterCJK, p.Scans(), readsAfterCJK, rc.Calls(),
		rep2.ProbeRan, rep2.Elapsed)
}

// TestFamilyBoundaryLazyWarmProbeElapsedZero 覆盖目标 2 的 Lazy 层表现。
//
// 断言：CJK warm Lazy 的 `LoadReport.ProbeElapsed == 0` 且 `Probe.ProbeRan == false`，
// 而 `Probe.Elapsed`（历史）仍保留非零值。
func TestFamilyBoundaryLazyWarmProbeElapsedZero(t *testing.T) {
	p, _ := familyBoundaryNewProvider()

	lz1, err := NewLazy(LazyOptions{Provider: p, Family: FamilyCJK})
	if err != nil {
		t.Fatalf("构造 Lazy#1 失败: %v", err)
	}
	lz1.Request()
	if !familyBoundaryWaitState(lz1, LoadReady, 15*time.Second) {
		rep := lz1.Report()
		t.Fatalf("Lazy#1 未进入 Ready（state=%s err=%v）；本机前提不成立", rep.State, rep.Err)
	}
	rep1 := lz1.Report()
	if rep1.Probe == nil || !rep1.Probe.ProbeRan {
		t.Fatalf("Lazy#1 应为本轮探测（ProbeRan=true），实际 %+v", rep1.Probe)
	}
	histElapsed := rep1.Probe.Elapsed

	// 同 Provider 的第二个 Lazy：应命中缓存。
	lz2, err := NewLazy(LazyOptions{Provider: p, Family: FamilyCJK})
	if err != nil {
		t.Fatalf("构造 Lazy#2 失败: %v", err)
	}
	lz2.Request()
	if !familyBoundaryWaitState(lz2, LoadReady, 15*time.Second) {
		rep := lz2.Report()
		t.Fatalf("Lazy#2 未进入 Ready（state=%s err=%v）", rep.State, rep.Err)
	}
	rep2 := lz2.Report()

	if rep2.Probe == nil {
		t.Fatal("Lazy#2 Probe==nil：无法判读（不得当作缓存命中）")
	}
	if rep2.Probe.ProbeRan {
		t.Errorf("Lazy#2 命中路径 ProbeRan=true，期望 false")
	}
	if rep2.ProbeElapsed != 0 {
		t.Errorf("Lazy#2 命中路径 ProbeElapsed=%v，期望 0（本轮无探测）", rep2.ProbeElapsed)
	}
	if rep2.Probe.Elapsed != histElapsed {
		t.Errorf("Lazy#2 历史 Probe.Elapsed=%v，期望保留 %v", rep2.Probe.Elapsed, histElapsed)
	}
}

// familyBoundaryWaitState 轮询等待状态（有界）。
func familyBoundaryWaitState(lz *Lazy, want LoadState, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if lz.State() == want {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return lz.State() == want
}

// ---------------------------------------------------------------------------
// 3：预热 Provider 上 CJK warm Lazy 与不支持 family 的 Lazy 并发
// ---------------------------------------------------------------------------

// TestFamilyBoundaryConcurrentWarmAndFailedLazy 覆盖目标 3。
//
// 用 channel 表达提交顺序，不用固定短 sleep 赌交错；所有 goroutine 有界结束。
func TestFamilyBoundaryConcurrentWarmAndFailedLazy(t *testing.T) {
	p, _ := familyBoundaryNewProvider()

	// 先预热：确保后续 CJK Lazy 走缓存、且 Body 走失败分支。
	familyBoundaryRequireCJKReady(t, p)
	scansWarm := p.Scans()

	// Body Lazy：构造期允许（族名合法），Request 后必须失败。
	lzBody, err := NewLazy(LazyOptions{Provider: p, Family: FamilyBody})
	if err != nil {
		t.Fatalf("Body Lazy 构造失败（构造期只校验族名，不应失败）: %v", err)
	}
	lzCJK, err := NewLazy(LazyOptions{Provider: p, Family: FamilyCJK})
	if err != nil {
		t.Fatalf("CJK Lazy 构造失败: %v", err)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() { defer wg.Done(); <-start; lzBody.Request() }()
	go func() { defer wg.Done(); <-start; lzCJK.Request() }()
	close(start) // 同一时刻放行
	wg.Wait()    // 只等 Request 返回，不等后台终态

	// 有界等待两者各自进入终态。
	if !familyBoundaryWaitTerminal(lzBody, 15*time.Second) {
		t.Fatalf("Body Lazy 未进入终态（state=%s）", lzBody.State())
	}
	if !familyBoundaryWaitTerminal(lzCJK, 15*time.Second) {
		t.Fatalf("CJK Lazy 未进入终态（state=%s）", lzCJK.State())
	}

	repBody := lzBody.Report()
	repCJK := lzCJK.Report()

	// Body：必须失败、包 ErrFontNotFound、Family 正确。
	if repBody.State != LoadFailed {
		t.Errorf("Body Lazy 应为 LoadFailed，实际 %s", repBody.State)
	}
	if repBody.Family != FamilyBody {
		t.Errorf("Body Lazy 报告 Family=%q，期望 %q", repBody.Family, FamilyBody)
	}
	if repBody.Err == nil || !errors.Is(repBody.Err, contracts.ErrFontNotFound) {
		t.Errorf("Body Lazy 的 Err 必须包 ErrFontNotFound，实际: %v", repBody.Err)
	}

	// CJK：必须成功、Family 正确。
	if repCJK.State != LoadReady {
		t.Errorf("CJK Lazy 应为 LoadReady，实际 %s（err=%v）", repCJK.State, repCJK.Err)
	}
	if repCJK.Family != FamilyCJK {
		t.Errorf("CJK Lazy 报告 Family=%q，期望 %q", repCJK.Family, FamilyCJK)
	}

	// 交叉污染检查：CJK 成功报告不得带 Body 的错误。
	if repCJK.Err != nil {
		t.Errorf("CJK 成功报告 Err 非 nil（被失败 family 污染）: %v", repCJK.Err)
	}
	// Body 失败不得让 CJK 重扫。
	if got := p.Scans(); got != scansWarm {
		t.Errorf("并发失败 family 导致 CJK 重扫：Scans %d → %d", scansWarm, got)
	}

	// Probe==nil 不得被当作命中：两边都要求 Probe 可判读。
	if repCJK.Probe == nil {
		t.Error("CJK 并发路径 Probe==nil：无法判读（不得当作缓存命中）")
	} else if repCJK.Probe.ProbeRan {
		t.Errorf("CJK warm 路径 ProbeRan=true，期望 false")
	}
	if repCJK.ProbeElapsed != 0 {
		t.Errorf("CJK warm 路径 ProbeElapsed=%v，期望 0", repCJK.ProbeElapsed)
	}

	t.Logf("并发：Body state=%s err=%v；CJK state=%s ProbeRan=%v ProbeElapsed=%v；Scans 保持 %d",
		repBody.State, repBody.Err, repCJK.State,
		repCJK.Probe != nil && repCJK.Probe.ProbeRan, repCJK.ProbeElapsed, p.Scans())
}

func familyBoundaryWaitTerminal(lz *Lazy, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := lz.State()
		if s == LoadReady || s == LoadFailed {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	s := lz.State()
	return s == LoadReady || s == LoadFailed
}

// ---------------------------------------------------------------------------
// 4：失败 family 的显式重试仍失败，且不触发 CJK 重扫
// ---------------------------------------------------------------------------

// TestFamilyBoundaryFailedFamilyRetryStaysFailed 覆盖目标 4。
func TestFamilyBoundaryFailedFamilyRetryStaysFailed(t *testing.T) {
	p, _ := familyBoundaryNewProvider()
	familyBoundaryRequireCJKReady(t, p)
	scansWarm := p.Scans()

	lz, err := NewLazy(LazyOptions{Provider: p, Family: FamilyMono})
	if err != nil {
		t.Fatalf("Mono Lazy 构造失败: %v", err)
	}

	// 第一次：失败。
	lz.Request()
	if !familyBoundaryWaitTerminal(lz, 15*time.Second) {
		t.Fatalf("Mono Lazy 首次请求未进入终态（state=%s）", lz.State())
	}
	rep1 := lz.Report()
	if rep1.State != LoadFailed {
		t.Fatalf("Mono 首次请求应为 LoadFailed，实际 %s", rep1.State)
	}
	if !errors.Is(rep1.Err, contracts.ErrFontNotFound) {
		t.Errorf("Mono 首次失败应包 ErrFontNotFound，实际: %v", rep1.Err)
	}
	attempts1 := rep1.Attempts

	// 显式重试：必须仍是失败（不得伪装成功）。
	lz.Request()
	if !familyBoundaryWaitTerminal(lz, 15*time.Second) {
		t.Fatalf("Mono Lazy 重试未进入终态（state=%s）", lz.State())
	}
	rep2 := lz.Report()
	if rep2.State != LoadFailed {
		t.Errorf("Mono 重试后应为 LoadFailed，实际 %s（重试不得伪装成功）", rep2.State)
	}
	if !errors.Is(rep2.Err, contracts.ErrFontNotFound) {
		t.Errorf("Mono 重试后错误应仍包 ErrFontNotFound，实际: %v", rep2.Err)
	}
	// 重试应当真的再尝试了一次（Attempts 增加），而不是把 Loading 期间
	// 的合并请求当成了重试完成。
	if rep2.Attempts <= attempts1 {
		t.Errorf("重试后 Attempts 未增加：%d → %d（可能把合并请求当成了重试完成）",
			attempts1, rep2.Attempts)
	}
	// 失败的 Mono 不得触发 CJK 重扫。
	if got := p.Scans(); got != scansWarm {
		t.Errorf("失败 family 的重试触发了 CJK 重扫：Scans %d → %d", scansWarm, got)
	}

	t.Logf("Mono 重试：Attempts %d→%d，state 仍为 %s；Scans 保持 %d",
		attempts1, rep2.Attempts, rep2.State, p.Scans())
}

// TestFamilyBoundaryUnknownFamilyRejectedAtConstruct 覆盖未知 family 的构造期拒绝。
//
// 未知族名在 `NewLazy` 就被拒绝（`lazy.go:193-197`），不会进入加载流程；
// 这与「未实现但族名合法」的 Body/Mono 是两条不同路径，不得混为一谈。
func TestFamilyBoundaryUnknownFamilyRejectedAtConstruct(t *testing.T) {
	p, _ := familyBoundaryNewProvider()
	scansBefore := p.Scans()

	_, err := NewLazy(LazyOptions{Provider: p, Family: "NotAFamily"})
	if err == nil {
		t.Fatal("未知 family 应在构造期被拒绝")
	}
	if !errors.Is(err, contracts.ErrFontNotFound) {
		t.Errorf("未知 family 构造错误应包 ErrFontNotFound，实际: %v", err)
	}
	// 构造期拒绝不应产生任何扫描。
	if got := p.Scans(); got != scansBefore {
		t.Errorf("构造期拒绝却产生了扫描：Scans %d → %d", scansBefore, got)
	}
}
