package theme

// FACE-SELECT 阶段二（Lazy 异步接线）测试。
//
// 目标：证明"选中面"成为 Lazy 后台任务的**最终产出**，且不破坏既有语义：
//   - 构造期不读源、不解析（用不存在的路径与可控读证明）；
//   - Request 立即返回，读取/解析在既有 worker 内完成（用阻塞读确定性证明）；
//   - 最终链零调用 Provider（不 Resolve / Faces / Read），但默认临时主题仍走
//     既有嵌入加载（由 TempSource 如实反映）；
//   - 终态事实（LoadedAt/CompletedAt/LoadElapsed/Err/Attempts）与默认链一致；
//   - Probe=nil、ProbeElapsed=0（本轮确实没有探测）；
//   - 失败保留临时主题、不产出 whole 最终主题、无自动重试；
//   - SelectionReport 与 LoadReport 同锁发布、深拷贝、每次新尝试复位。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/widget/material"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/goxml/goxml/contracts"
)

// selTestWait 有界轮询直到条件成立。
func selTestWait(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// selTestWaitState 等待 Lazy 进入指定状态。
func selTestWaitState(t *testing.T, lz *Lazy, want LoadState) {
	t.Helper()
	if selTestWait(15*time.Second, func() bool { return lz.State() == want }) {
		return
	}
	rep := lz.Report()
	t.Fatalf("等待 state=%s 超时，实际 state=%s err=%v", want, rep.State, rep.Err)
}

// selTestSel 构造一个"单字体 index 0"的选面配置（Go Regular）。
func selTestSel() FaceSelection {
	return FaceSelection{Source: FontSource{Bytes: goregular.TTF, Name: "regular"}, Indices: []int{0}}
}

// ---------------------------------------------------------------------------
// 构造不读源 / Request 不阻塞
// ---------------------------------------------------------------------------

// TestLazySelectionConstructDoesNotReadSource 证明构造期零 IO：
// 用**不存在的路径**也能构造成功，错误只在 Request 之后出现。
func TestLazySelectionConstructDoesNotReadSource(t *testing.T) {
	lz, err := NewLazyWithSelection(FaceSelection{
		Source:  FontSource{Path: filepath.Join("C:\\", "__goxml_absent__", "nope.ttf")},
		Indices: []int{0},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造期不应因源缺失而失败（不得偷读）：%v", err)
	}
	if got := lz.State(); got != LoadUnloaded {
		t.Errorf("构造后 state = %s，期望 unloaded", got)
	}
	if th := lz.Theme(); th != nil {
		t.Error("构造后 Theme() 应为 nil（未 Request）")
	}
	if r := lz.SelectionReport(); r != nil {
		t.Error("构造后 SelectionReport() 应为 nil（未尝试）")
	}

	// 只有 Request 之后才暴露源错误。
	lz.Request()
	selTestWaitState(t, lz, LoadFailed)
	rep := lz.Report()
	if rep.Err == nil {
		t.Fatal("失败终态应有 Err")
	}
	sr := lz.SelectionReport()
	if sr == nil {
		t.Fatal("失败后 SelectionReport 不应为 nil")
	}
	if sr.ErrorKind != SelectionSourceIO {
		t.Errorf("ErrorKind = %q，期望 %q", sr.ErrorKind, SelectionSourceIO)
	}
}

// TestLazySelectionRequestDoesNotBlock 用**阻塞读**确定性证明：
// Request 返回时后台仍在读源（state=loading），解除阻塞后才 Ready。
func TestLazySelectionRequestDoesNotBlock(t *testing.T) {
	distinct := selTestDistinctTTC(t)

	lz, err := NewLazyWithSelection(FaceSelection{
		Source: FontSource{Path: `C:\fake\injected.ttc`}, Indices: []int{1},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	// 私有字段注入：仅测试内使用，不进入公开 API。
	lz.sel.readFileFn = func(string) ([]byte, error) {
		close(entered)
		<-release
		return distinct, nil
	}

	lz.Request()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker 未进入读源阶段")
	}
	// 读仍被阻塞：Request 已经返回（否则我们走不到这里），且 state 还是 loading。
	if got := lz.State(); got != LoadLoading {
		t.Errorf("后台仍在读源时 state = %s，期望 loading", got)
	}
	close(release)
	selTestWaitState(t, lz, LoadReady)
}

// ---------------------------------------------------------------------------
// 最终主题来自选面 / Provider 最终链零调用
// ---------------------------------------------------------------------------

func TestLazySelectionFinalThemeFromSelection(t *testing.T) {
	lz, err := NewLazyWithSelection(selTestSel(), LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadReady)

	rep := lz.Report()
	if rep.Probe != nil {
		t.Error("选面路径不做探测，Probe 必须为 nil")
	}
	if rep.ProbeElapsed != 0 {
		t.Errorf("ProbeElapsed = %v，期望 0（不得把解析耗时冒充探测耗时）", rep.ProbeElapsed)
	}
	if rep.Attempts != 1 {
		t.Errorf("Attempts = %d，期望 1", rep.Attempts)
	}
	if rep.LoadedAt.IsZero() {
		t.Error("Ready 时 LoadedAt 不得为零值")
	}
	if rep.CompletedAt.IsZero() {
		t.Error("Ready 时 CompletedAt 不得为零值")
	}
	if rep.Err != nil {
		t.Errorf("Ready 时 Err 应为 nil，实际 %v", rep.Err)
	}
	if rep.TempSource != tempSourceEmbedded {
		t.Errorf("TempSource = %q，期望默认嵌入临时主题 %q", rep.TempSource, tempSourceEmbedded)
	}

	// 最终主题确实来自选面：Take 一次性交接，且与临时主题不同实例。
	temp := lz.Theme()
	if temp == nil {
		t.Fatal("Request 后应有临时主题")
	}
	next := lz.Take()
	if next == nil {
		t.Fatal("Ready 后 Take 应返回最终主题")
	}
	if next == temp {
		t.Error("最终主题与临时主题是同一实例：说明没有真正换主题")
	}
	if lz.Take() != nil {
		t.Error("Take 应为一次性")
	}
	if lz.Theme() != next {
		t.Error("Take 后 Theme() 应指向最终主题")
	}

	sr := lz.SelectionReport()
	if sr == nil || !sr.SelectionApplied {
		t.Fatalf("SelectionReport 应标记 SelectionApplied，实际 %+v", sr)
	}
	if len(sr.ActualIndices) != 1 || sr.ActualIndices[0] != 0 {
		t.Errorf("ActualIndices = %v，期望 [0]", sr.ActualIndices)
	}
	if len(sr.Meta) != 1 {
		t.Fatalf("Meta 长度 = %d，期望 1", len(sr.Meta))
	}
}

// selTestSpyProvider 构造一个"最终链一旦被调用就可观测"的 Provider。
//
// 它把 Read 换成一个计数器（选面路径不该调用），并通过 Scans/LastReport
// 观察是否发生过 Resolve。
func selTestSpyProvider(t *testing.T) (*Provider, *int) {
	t.Helper()
	calls := 0
	p := NewProvider(ProviderOptions{
		Candidates: []string{`C:\__goxml_spy__\a.ttc`},
		Read: func(string) ([]byte, error) {
			calls++
			return nil, os.ErrNotExist
		},
	})
	return p, &calls
}

func TestLazySelectionProviderFinalChainUntouched(t *testing.T) {
	spy, readCalls := selTestSpyProvider(t)
	lz, err := NewLazyWithSelection(selTestSel(), LazyOptions{Provider: spy})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadReady)

	if *readCalls != 0 {
		t.Errorf("最终链不应读取候选：Read 调用 %d 次", *readCalls)
	}
	if got := spy.Scans(); got != 0 {
		t.Errorf("最终链不应探测：Scans = %d，期望 0", got)
	}
	if got := spy.LastReport(); got != nil {
		t.Errorf("最终链不应产生探测报告：LastReport = %+v", got)
	}
	// 但默认临时主题仍走既有嵌入加载（不是"完全不碰 Provider"）。
	if rep := lz.Report(); rep.TempSource != tempSourceEmbedded {
		t.Errorf("TempSource = %q，期望嵌入临时主题", rep.TempSource)
	}
}

func TestLazySelectionCustomTempUnchanged(t *testing.T) {
	custom := material.NewTheme()
	lz, err := NewLazyWithSelection(selTestSel(), LazyOptions{Temp: custom})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadReady)
	if rep := lz.Report(); rep.TempSource != tempSourceCustom {
		t.Errorf("TempSource = %q，期望 custom", rep.TempSource)
	}
}

// ---------------------------------------------------------------------------
// 失败语义
// ---------------------------------------------------------------------------

func TestLazySelectionFailureKeepsTempAndNoWholeTheme(t *testing.T) {
	bad := selTestCorruptTable(t, goregular.TTF, "loca", 8)
	lz, err := NewLazyWithSelection(FaceSelection{
		Source: FontSource{Bytes: bad, Name: "corrupt-loca"}, Indices: []int{0},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadFailed)

	rep := lz.Report()
	if rep.Err == nil {
		t.Fatal("Failed 必须有 Err")
	}
	// 既有约定：终态错误可 errors.Is 到 ErrFontNotFound。
	if !errors.Is(rep.Err, contracts.ErrFontNotFound) {
		t.Errorf("Err 应满足 errors.Is(ErrFontNotFound)，实际 %v", rep.Err)
	}
	// 同时保留 SelectionError 原因链。
	var se *SelectionError
	if !errors.As(rep.Err, &se) {
		t.Fatalf("Err 应可用 errors.As 取回 *SelectionError，实际 %T", rep.Err)
	}
	if se.Report.ErrorKind != SelectionParserPanic {
		t.Errorf("ErrorKind = %q，期望 %q", se.Report.ErrorKind, SelectionParserPanic)
	}
	if !se.Report.FaceIndexKnown || se.Report.FaceIndex != 0 {
		t.Errorf("应给出面索引 0，实际 known=%v idx=%d", se.Report.FaceIndexKnown, se.Report.FaceIndex)
	}
	// 失败：LoadedAt 零值、无 pending（不产出最终主题）。
	if !rep.LoadedAt.IsZero() {
		t.Error("失败时 LoadedAt 必须为零值")
	}
	if lz.Take() != nil {
		t.Error("失败时不得有可交接的最终主题")
	}
	// 临时主题仍在（首帧可渲染）。
	if lz.Theme() == nil {
		t.Error("失败后仍应保留临时主题")
	}
	if rep.ProbeElapsed != 0 {
		t.Errorf("失败路径 ProbeElapsed 也应为 0，实际 %v", rep.ProbeElapsed)
	}
}

// TestLazySelectionRetryResetAndNoAutoRetry 验证：
// 无自动重试；显式重试递增 Attempts；新尝试清空上一轮 SelectionReport。
func TestLazySelectionRetryResetAndNoAutoRetry(t *testing.T) {
	bad := selTestCorruptTable(t, goregular.TTF, "loca", 8)
	lz, err := NewLazyWithSelection(FaceSelection{
		Source: FontSource{Bytes: bad, Name: "corrupt-loca"}, Indices: []int{0},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadFailed)
	if got := lz.Report().Attempts; got != 1 {
		t.Fatalf("首次 Attempts = %d，期望 1", got)
	}
	if sr := lz.SelectionReport(); sr == nil || sr.ErrorKind == "" {
		t.Fatal("首次失败后 SelectionReport 应带 ErrorKind")
	}

	// 等待一段时间，确认没有自动重试。
	time.Sleep(50 * time.Millisecond)
	if got := lz.Report().Attempts; got != 1 {
		t.Errorf("不应自动重试：Attempts = %d，期望仍为 1", got)
	}

	// 显式重试：新尝试必须清空上一轮选面诊断。
	lz.Request()
	if sr := lz.SelectionReport(); sr != nil {
		t.Errorf("新尝试开始后 SelectionReport 应被清空，实际 %+v", sr)
	}
	selTestWaitState(t, lz, LoadFailed)
	if got := lz.Report().Attempts; got != 2 {
		t.Errorf("重试后 Attempts = %d，期望 2", got)
	}
	if sr := lz.SelectionReport(); sr == nil || sr.ErrorKind != SelectionParserPanic {
		t.Errorf("重试失败后应重新发布选面诊断，实际 %+v", sr)
	}
}

// ---------------------------------------------------------------------------
// 通知重入 / 并发幂等 / 报告深拷贝
// ---------------------------------------------------------------------------

func TestLazySelectionNotifyReentrantNoDeadlock(t *testing.T) {
	lz, err := NewLazyWithSelection(selTestSel(), LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	done := make(chan struct{})
	if !lz.SetNotify(func() {
		// 回调内重入读取：若实现持锁调用就会自锁。
		_ = lz.State()
		_ = lz.Report()
		_ = lz.SelectionReport()
		_ = lz.Theme()
		close(done)
	}) {
		t.Fatal("SetNotify 应成功")
	}
	lz.Request()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("通知回调未在期限内完成：疑似持锁重入导致死锁")
	}
	selTestWaitState(t, lz, LoadReady)
}

func TestLazySelectionConcurrentRequestIdempotent(t *testing.T) {
	lz, err := NewLazyWithSelection(selTestSel(), LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	const n = 16
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
	selTestWaitState(t, lz, LoadReady)

	if got := lz.Report().Attempts; got != 1 {
		t.Errorf("并发首次请求后 Attempts = %d，期望 1（Loading 期间应合并）", got)
	}
}

func TestLazySelectionReportDeepCopy(t *testing.T) {
	buf := selTestDistinctTTC(t)
	lz, err := NewLazyWithSelection(FaceSelection{
		Source: FontSource{Bytes: buf, Name: "distinct"}, Indices: []int{1, 0},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadReady)

	sr := lz.SelectionReport()
	if sr == nil {
		t.Fatal("SelectionReport 为 nil")
	}
	// 篡改返回值，内部不应受影响。
	sr.ActualIndices[0] = 99
	sr.RequestedIndices[0] = 99
	if len(sr.Meta) > 0 {
		sr.Meta[0].Weight = 12345
	}
	if len(sr.CoverageMissing) > 0 {
		sr.CoverageMissing[0] = 'x'
	}

	again := lz.SelectionReport()
	if again.ActualIndices[0] != 1 {
		t.Errorf("ActualIndices 被外部改动污染：%v", again.ActualIndices)
	}
	if again.RequestedIndices[0] != 1 {
		t.Errorf("RequestedIndices 被污染：%v", again.RequestedIndices)
	}
	if len(again.Meta) > 0 && again.Meta[0].Weight == 12345 {
		t.Error("Meta 未深拷贝")
	}
	if len(again.CoverageMissing) > 0 && again.CoverageMissing[0] == 'x' {
		t.Error("CoverageMissing 未深拷贝")
	}
}

// TestLazySelectionLabelNonUnique 明确 Label 只是诊断标签，不承担身份。
func TestLazySelectionLabelIsDiagnosticOnly(t *testing.T) {
	lz, err := NewLazyWithSelection(selTestSel(), LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	// 未 Request 时无报告，但从冻结配置可直接看到 label 形态。
	want := "selected:regular#faces=0"
	if got := lz.sel.label; got != want {
		t.Errorf("label = %q，期望 %q", got, want)
	}
	if !strings.Contains(lz.sel.label, "#faces=") {
		t.Error("label 应包含面索引段")
	}
}

// ---------------------------------------------------------------------------
// R1.1 回归：Report() 读取边界必须隔离本层自有的可变 *SelectionError
// ---------------------------------------------------------------------------

// selTestSelErrOf 从错误链中取出最外层 *SelectionError。
func selTestSelErrOf(t *testing.T, err error) *SelectionError {
	t.Helper()
	var se *SelectionError
	if !errors.As(err, &se) {
		t.Fatalf("错误链中应含 *SelectionError，实际 %T: %v", err, err)
	}
	return se
}

// selTestAssertReportIsolated 断言两次 Report() 的错误对象既不共享，也不互相影响。
//
// 做法：取 r1/r2，errors.As 得 e1/e2；先要求 e1 != e2（本层自有对象独立）；
// 再对 e1 的 Report 字段与 e1.Err 做破坏性修改，要求 r2 与第三次 Report 不变。
func selTestAssertReportIsolated(t *testing.T, lz *Lazy) {
	t.Helper()

	r1 := lz.Report()
	r2 := lz.Report()
	if r1.Err == nil || r2.Err == nil {
		t.Fatal("失败路径的 Report().Err 不应为 nil")
	}
	e1 := selTestSelErrOf(t, r1.Err)
	e2 := selTestSelErrOf(t, r2.Err)
	if e1 == e2 {
		t.Fatal("两次 Report() 返回同一个 *SelectionError：读取边界未隔离")
	}

	// 记录 r2 的原始事实，稍后逐项比对。
	wantKind := e2.Report.ErrorKind
	wantPhase := e2.Report.ErrorPhase
	wantFaceKnown := e2.Report.FaceIndexKnown
	wantFace := e2.Report.FaceIndex
	wantReq := append([]int(nil), e2.Report.RequestedIndices...)
	wantMissing := append([]rune(nil), e2.Report.CoverageMissing...)
	wantReasonMsg := ""
	if e2.Err != nil {
		wantReasonMsg = e2.Err.Error()
	}
	if wantKind == "" {
		t.Fatal("失败报告的 ErrorKind 不应为空")
	}

	// 破坏 e1 的字段与原因（这些字段都真实存在于 SelectionReport）。
	e1.Report.ErrorKind = "MUTATED"
	e1.Report.ErrorPhase = "MUTATED"
	e1.Report.FaceIndexKnown = !e1.Report.FaceIndexKnown
	e1.Report.FaceIndex = 12345
	for i := range e1.Report.RequestedIndices {
		e1.Report.RequestedIndices[i] = 999
	}
	for i := range e1.Report.CoverageMissing {
		e1.Report.CoverageMissing[i] = 'X'
	}
	e1.Err = errors.New("MUTATED-REASON")

	// r2 必须完全不受影响。
	r2again := lz.Report()
	e2again := selTestSelErrOf(t, r2again.Err)
	if e2again.Report.ErrorKind != wantKind || e2again.Report.ErrorPhase != wantPhase {
		t.Errorf("修改 e1 后 r2 的分类变了：kind=%q phase=%q，期望 %q/%q",
			e2again.Report.ErrorKind, e2again.Report.ErrorPhase, wantKind, wantPhase)
	}
	if e2again.Report.FaceIndexKnown != wantFaceKnown || e2again.Report.FaceIndex != wantFace {
		t.Errorf("修改 e1 后 r2 的面索引事实变了：known=%v idx=%d，期望 %v/%d",
			e2again.Report.FaceIndexKnown, e2again.Report.FaceIndex, wantFaceKnown, wantFace)
	}
	if len(e2again.Report.RequestedIndices) != len(wantReq) {
		t.Errorf("修改 e1 后 r2 的 RequestedIndices 长度变了")
	} else {
		for i := range wantReq {
			if e2again.Report.RequestedIndices[i] != wantReq[i] {
				t.Errorf("修改 e1 后 r2 的 RequestedIndices[%d] = %d，期望 %d",
					i, e2again.Report.RequestedIndices[i], wantReq[i])
			}
		}
	}
	if len(e2again.Report.CoverageMissing) != len(wantMissing) {
		t.Errorf("修改 e1 后 r2 的 CoverageMissing 长度变了")
	} else {
		for i := range wantMissing {
			if e2again.Report.CoverageMissing[i] != wantMissing[i] {
				t.Errorf("修改 e1 后 r2 的 CoverageMissing[%d] 被污染", i)
			}
		}
	}
	gotReasonMsg := ""
	if e2again.Err != nil {
		gotReasonMsg = e2again.Err.Error()
	}
	if gotReasonMsg != wantReasonMsg {
		t.Errorf("修改 e1.Err 后，后续 Report 的原因事实变了：%q → %q", wantReasonMsg, gotReasonMsg)
	}
	if wantReasonMsg == "" {
		t.Error("失败路径的原因不应为空")
	}

	// 契约仍在：errors.Is(ErrFontNotFound) 与 errors.As 都要成立。
	if !errors.Is(r2again.Err, contracts.ErrFontNotFound) {
		t.Error("隔离后仍须满足 errors.Is(ErrFontNotFound)")
	}
	if _, ok := selTestSelErrOf(t, r2again.Err), true; !ok {
		t.Error("隔离后仍须可用 errors.As 取回 *SelectionError")
	}

	// 嵌套自有 SelectionError 也要隔离（source-io 路径会在 Err 里再嵌一层）。
	// 只对本层自有类型递归克隆；最内层是非自有的底层错误（IO 等）时按 Go 只读
	// 惯例共用同一个对象——因此这里只比较**自有**那一层的指针。
	var outerA, outerB *SelectionError
	if errors.As(r2again.Err, &outerA) && errors.As(lz.Report().Err, &outerB) {
		var innerA, innerB *SelectionError
		if errors.As(outerA.Err, &innerA) && errors.As(outerB.Err, &innerB) {
			if innerA == innerB {
				t.Error("嵌套的 *SelectionError 仍共享同一指针：下一层未隔离")
			}
		}
	}
}

// TestLazySelectionReportErrorIsolatedOnParseFailure 覆盖解析失败（含 panic 收敛）路径。
func TestLazySelectionReportErrorIsolatedOnParseFailure(t *testing.T) {
	bad := selTestCorruptTable(t, goregular.TTF, "loca", 8)
	lz, err := NewLazyWithSelection(FaceSelection{
		Source: FontSource{Bytes: bad, Name: "corrupt-loca"}, Indices: []int{0},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadFailed)
	selTestAssertReportIsolated(t, lz)
}

// TestLazySelectionReportErrorIsolatedOnSourceIO 覆盖 source-io（Err 内嵌套 SelectionError）路径。
func TestLazySelectionReportErrorIsolatedOnSourceIO(t *testing.T) {
	lz, err := NewLazyWithSelection(FaceSelection{
		Source:  FontSource{Path: filepath.Join("C:\\", "__goxml_absent__", "nope.ttf")},
		Indices: []int{0},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadFailed)
	if sr := lz.SelectionReport(); sr == nil || sr.ErrorKind != SelectionSourceIO {
		t.Fatalf("期望 source-io 失败，实际 %+v", sr)
	}
	selTestAssertReportIsolated(t, lz)
}

// TestLazySelectionMissingTableIsParse 覆盖 Lazy 侧的**普通解析错误**分类（P0-1 第二处）。
//
// 与 facesel_test.go 的同步回归互补：证明终态报告与 errors.As 取回的分类一致，
// 且 errors.Is(ErrFontNotFound) 同时成立。
func TestLazySelectionMissingTableIsParse(t *testing.T) {
	bad := selTestRenameTable(t, goregular.TTF, "cmap", "zzzz")
	lz, err := NewLazyWithSelection(FaceSelection{
		Source: FontSource{Bytes: bad, Name: "rename-cmap"}, Indices: []int{0},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	lz.Request()
	selTestWaitState(t, lz, LoadFailed)

	rep := lz.Report()
	if !errors.Is(rep.Err, contracts.ErrFontNotFound) {
		t.Errorf("Err 应满足 errors.Is(ErrFontNotFound)，实际 %v", rep.Err)
	}
	var se *SelectionError
	if !errors.As(rep.Err, &se) {
		t.Fatal("Err 应可用 errors.As 取回 *SelectionError")
	}
	if se.Report.ErrorKind != SelectionParse {
		t.Errorf("errors.As 取回的分类 = %q，期望 %q", se.Report.ErrorKind, SelectionParse)
	}
	sr := lz.SelectionReport()
	if sr == nil || sr.ErrorKind != SelectionParse {
		t.Errorf("SelectionReport 分类 = %+v，期望 %q", sr, SelectionParse)
	}
	// 两处分类必须一致（终态报告与 SelectionReport 不矛盾）。
	if sr != nil && se.Report.ErrorKind != sr.ErrorKind {
		t.Errorf("两处分类不一致：As=%q Report=%q", se.Report.ErrorKind, sr.ErrorKind)
	}
	if !errors.As(rep.Err, &se) || se.Report.SelectionApplied {
		t.Error("失败报告不得标记 SelectionApplied")
	}
}

// TestLazySelectionPathRetryRereadsSource 确定性证明 Path 源每次显式重试都重读。
//
// 做法：私有 readFileFn 第一次返回不可用字节（第二次调用才给合法源），
// 用计数 + 通道同步，不靠固定 Sleep 判断成功。
func TestLazySelectionPathRetryRereadsSource(t *testing.T) {
	good := selTestDistinctTTC(t)

	lz, err := NewLazyWithSelection(FaceSelection{
		Source:  FontSource{Path: filepath.Join("C:\\", "__goxml_retry__", "retry.ttc")},
		Indices: []int{1},
	}, LazyOptions{})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	var mu sync.Mutex
	reads := 0
	firstRead := make(chan struct{})
	lz.sel.readFileFn = func(string) ([]byte, error) {
		mu.Lock()
		reads++
		n := reads
		mu.Unlock()
		if n == 1 {
			// 第一次：返回不可用字节（读头即失败）——确定性失败，不依赖时序。
			close(firstRead)
			return []byte("XXXXXXXXXXXX"), nil
		}
		return good, nil
	}

	lz.Request()
	<-firstRead
	selTestWaitState(t, lz, LoadFailed)
	mu.Lock()
	afterFirst := reads
	mu.Unlock()
	if afterFirst != 1 {
		t.Fatalf("首次尝试应只读 1 次，实际 %d", afterFirst)
	}
	if sr := lz.SelectionReport(); sr == nil || sr.SelectionApplied {
		t.Fatalf("首次应失败，实际 %+v", sr)
	}
	if got := lz.Report().Attempts; got != 1 {
		t.Fatalf("首次 Attempts = %d，期望 1", got)
	}

	// 显式重试：必须**重新读取**（reads 增加），并成功。
	lz.Request()
	selTestWaitState(t, lz, LoadReady)
	mu.Lock()
	afterRetry := reads
	mu.Unlock()
	if afterRetry != 2 {
		t.Errorf("重试后读取次数 = %d，期望 2（Path 源每次尝试都重新读取）", afterRetry)
	}
	if got := lz.Report().Attempts; got != 2 {
		t.Errorf("重试后 Attempts = %d，期望 2", got)
	}
	sr := lz.SelectionReport()
	if sr == nil || !sr.SelectionApplied {
		t.Fatalf("重试后应成功，实际 %+v", sr)
	}
	// 诊断重置：失败痕迹不得残留。
	if sr.ErrorKind != "" || sr.SelectionApplied != true {
		t.Errorf("重试成功后诊断应重置为成功态，实际 %+v", sr)
	}
	if len(sr.ActualIndices) != 1 || sr.ActualIndices[0] != 1 {
		t.Errorf("ActualIndices = %v，期望 [1]", sr.ActualIndices)
	}
	// 最终链仍未被 Provider 使用（本用例用默认 Provider，仅验证无探测报告）。
	if rep := lz.Report(); rep.Probe != nil {
		t.Error("选面路径不应产生 Probe 报告")
	}
	// 最终主题可用。
	if lz.Take() == nil {
		t.Error("重试成功后应有可交接的最终主题")
	}
}
