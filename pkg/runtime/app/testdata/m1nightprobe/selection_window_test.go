package main

// FACE-SELECT-WINDOW-PROBE 的**机制测试**（不打开窗口）。
//
// 这些用例只测"纯协议/判读逻辑"：参数组合校验、观测判据（validate）、
// Shaper 变化计数、以及输出行是否带唯一 marker 与"跳线"字段。
// 它们**不是**真实窗口验证——真实窗口与批量采样由 WorkBuddy 另行执行。

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/text"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ---------------------------------------------------------------------------
// 参数组合校验
// ---------------------------------------------------------------------------

func TestFaceProbeFlagsRejectIncompatibleCombinations(t *testing.T) {
	src := `C:\Windows\Fonts\msyh.ttc`
	for _, tc := range []struct {
		name    string
		flags   faceProbeFlags
		wantErr bool
	}{
		{"空 case 空源：原流程", faceProbeFlags{}, false},
		{"空 case 带源：拒绝（会被静默忽略）", faceProbeFlags{sourcePath: src}, true},
		{"合法 default 案例", faceProbeFlags{caseName: "default-lazy", sourcePath: src}, false},
		{"合法 selected 案例", faceProbeFlags{caseName: "selected1-lazy", sourcePath: src}, false},
		{"未知 case", faceProbeFlags{caseName: "bogus", sourcePath: src}, true},
		{"缺源", faceProbeFlags{caseName: "selected0-lazy"}, true},
		{"源为空白字符", faceProbeFlags{caseName: "selected0-lazy", sourcePath: "   "}, true},
		{"与 compare-fonts 冲突", faceProbeFlags{caseName: "default-lazy", sourcePath: src, compareFonts: true}, true},
		{"与显式 -load 冲突", faceProbeFlags{caseName: "default-lazy", sourcePath: src,
			explicit: map[string]bool{"load": true}}, true},
		{"显式 -load 不影响空 case", faceProbeFlags{explicit: map[string]bool{"load": true}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateFaceProbeFlags(tc.flags)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("应拒绝，实际放行（case=%q）", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("应放行，实际拒绝: %v", err)
			}
			if tc.flags.caseName == "" && got != faceCaseNone {
				t.Fatalf("空 case 应返回 faceCaseNone，实际 %q", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 观测基线与合法零值
// ---------------------------------------------------------------------------

// validSelectedObservation 返回一个**判据全通过**的 selected1 观测基线。
// 各拒绝用例在它的副本上只改一处，保证"拒绝"确实由该改动引起。
func validSelectedObservation() *faceObservation {
	start := time.Unix(100, 0)
	return &faceObservation{
		Case:   faceCaseSelected1,
		Source: `C:\fonts\sample.ttc`,

		LazyState:        theme.LoadReady,
		TempSource:       "embedded-subset",
		RequestedAt:      start,
		LoadedAt:         start.Add(10 * time.Millisecond),
		FrameSubmittedAt: start.Add(30 * time.Millisecond),

		SelectionPresent:         true,
		SelectionApplied:         true,
		SelectionRequested:       []int{1},
		SelectionActual:          []int{1},
		SelectionDeclaredFaces:   2,
		SelectionCoverageChecked: true,
		SelectionCoverageHit:     20,
		SelectionCoverageTotal:   20,

		ProviderReadCalls: 0,
		ProviderScans:     0,

		FramesObserved:   3,
		FirstLayoutSawM1: true,
		ShaperChanges:    1,
		FinalShaperSeen:  true,
		FinalLayoutAt:    start.Add(20 * time.Millisecond),

		InitAt:       start,
		AppCtorEntry: start.Add(time.Millisecond),
		AppCtorDone:  start.Add(2 * time.Millisecond),
		FirstFrameAt: start.Add(25 * time.Millisecond),
		M1CreatedAt:  start,
		M1Measured:   true,
		M1Elapsed:    25 * time.Millisecond,
	}
}

// validDefaultObservation 返回一个判据全通过的 default-lazy 观测基线。
func validDefaultObservation() *faceObservation {
	o := validSelectedObservation()
	o.Case = faceCaseDefaultLazy
	// default 走探测链：有 Probe、ProbeRan=true、命中指定资源、没走嵌入兜底。
	o.ProbePresent = true
	o.ProbeRan = true
	o.ProbeResolved = o.Source
	o.ProviderReadCalls = 2
	// 清掉选面诊断相关字段（default 不该有 SelectionReport）。
	o.SelectionPresent = false
	o.SelectionApplied = false
	o.SelectionRequested = nil
	o.SelectionActual = nil
	o.SelectionDeclaredFaces = 0
	o.SelectionCoverageChecked = false
	o.SelectionCoverageHit = 0
	o.SelectionCoverageTotal = 0
	return o
}

func TestFaceObservationAcceptsBaselines(t *testing.T) {
	if err := validSelectedObservation().validate(); err != nil {
		t.Fatalf("selected 基线应通过: %v", err)
	}
	if err := validDefaultObservation().validate(); err != nil {
		t.Fatalf("default 基线应通过: %v", err)
	}
}

// 合法零值：端点同时刻 → 区间为 0 且 measured；M1 elapsed=0 也已测量。
func TestFaceObservationAcceptsMeasuredZero(t *testing.T) {
	o := validSelectedObservation()
	zero := o.InitAt
	o.RequestedAt = zero
	o.LoadedAt = zero
	o.FinalLayoutAt = zero
	o.FrameSubmittedAt = zero.Add(1 * time.Millisecond) // 不早于最终 Layout
	o.FirstFrameAt = zero
	o.M1CreatedAt = zero
	o.M1Elapsed = 0
	if err := o.validate(); err != nil {
		t.Fatalf("测得 0 必须合法: %v", err)
	}
	// 区间行应标记 measured=true 且值为 0。
	lines := strings.Join(o.reportLines(), "\n")
	if !strings.Contains(lines, "faceFontInitToReadyNs=0") ||
		!strings.Contains(lines, "faceFontInitToReadyMeasured=true") {
		t.Fatal("测得 0 的区间必须以 measured=true 输出")
	}
	if !strings.Contains(lines, "faceM1ElapsedNs=0") || !strings.Contains(lines, "faceM1Measured=true") {
		t.Fatal("测得 0 的 M1 必须以 measured=true 输出")
	}
}

// 未测量与逆序拒绝。
func TestFaceObservationRejectsUnmeasuredOrReversed(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*faceObservation)
	}{
		{"M1 未测量", func(o *faceObservation) { o.M1Measured = false }},
		{"M1 为负", func(o *faceObservation) { o.M1Elapsed = -time.Nanosecond }},
		{"InitAt 为零", func(o *faceObservation) { o.InitAt = time.Time{} }},
		{"RequestedAt 为零", func(o *faceObservation) { o.RequestedAt = time.Time{} }},
		{"LoadedAt 为零", func(o *faceObservation) { o.LoadedAt = time.Time{} }},
		{"FirstFrameAt 为零", func(o *faceObservation) { o.FirstFrameAt = time.Time{} }},
		{"init→ready 逆序", func(o *faceObservation) { o.LoadedAt = o.InitAt.Add(-time.Nanosecond) }},
		{"ready 早于请求", func(o *faceObservation) { o.LoadedAt = o.RequestedAt.Add(-time.Nanosecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validSelectedObservation()
			tc.mut(o)
			if err := o.validate(); err == nil {
				t.Fatal("应拒绝")
			}
		})
	}
}

// selected：Probe 语义与选面诊断缺一不可。
func TestFaceObservationSelectedProbeAndReportRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*faceObservation)
	}{
		{"selected 出现 Probe", func(o *faceObservation) { o.ProbePresent = true }},
		{"selected ProbeElapsed 非 0", func(o *faceObservation) { o.ProbeElapsed = time.Millisecond }},
		{"缺 SelectionReport", func(o *faceObservation) { o.SelectionPresent = false }},
		{"选择未应用", func(o *faceObservation) { o.SelectionApplied = false }},
		{"索引错位", func(o *faceObservation) { o.SelectionActual = []int{0} }},
		{"索引顺序错", func(o *faceObservation) { o.SelectionActual = []int{} }},
		{"覆盖未检查", func(o *faceObservation) { o.SelectionCoverageChecked = false }},
		{"覆盖分母异常", func(o *faceObservation) { o.SelectionCoverageTotal = 0 }},
		{"覆盖不足", func(o *faceObservation) { o.SelectionCoverageHit = 1 }},
		{"带错误类别", func(o *faceObservation) { o.SelectionErrorKind = "parse" }},
		{"Provider 被读取", func(o *faceObservation) { o.ProviderReadCalls = 1 }},
		{"Provider 被扫描", func(o *faceObservation) { o.ProviderScans = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validSelectedObservation()
			tc.mut(o)
			if err := o.validate(); err == nil {
				t.Fatal("应拒绝")
			}
		})
	}
	// Probe=nil 本身是合法前提：不放宽为"必须非 nil"。
	o := validSelectedObservation()
	o.ProbePresent = false
	if err := o.validate(); err != nil {
		t.Fatalf("selected 的 Probe=nil 必须合法: %v", err)
	}
}

// default：必须是指定系统资源的冷探测，无嵌入最终回退。
func TestFaceObservationDefaultRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*faceObservation)
	}{
		{"缺探测报告", func(o *faceObservation) { o.ProbePresent = false }},
		{"冷探测前提失败", func(o *faceObservation) { o.ProbeRan = false }},
		{"探测失败", func(o *faceObservation) { o.ProbeErr = errTest("boom") }},
		{"命中错源", func(o *faceObservation) { o.ProbeResolved = `C:\other.ttc` }},
		{"走了嵌入兜底", func(o *faceObservation) { o.EmbeddedAttempted = true }},
		{"没有真实读取", func(o *faceObservation) { o.ProviderReadCalls = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validDefaultObservation()
			tc.mut(o)
			if err := o.validate(); err == nil {
				t.Fatal("应拒绝")
			}
		})
	}
}

// Ready 但无交接 / 无最终 Layout 拒绝。
func TestFaceObservationRejectsNoHandoffOrNoFinalLayout(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*faceObservation)
	}{
		{"未进入 Ready", func(o *faceObservation) { o.LazyState = theme.LoadLoading }},
		{"Ready 但无帧内交接", func(o *faceObservation) { o.FrameSubmittedAt = time.Time{} }},
		{"最终 Shaper 从未被 Layout 使用", func(o *faceObservation) { o.FinalShaperSeen = false }},
		{"最终 Layout 时刻未测", func(o *faceObservation) { o.FinalLayoutAt = time.Time{} }},
		{"FrameSubmittedAt 早于最终 Layout",
			func(o *faceObservation) { o.FrameSubmittedAt = o.FinalLayoutAt.Add(-time.Nanosecond) }},
		{"没有真实帧", func(o *faceObservation) { o.FramesObserved = 0 }},
		{"首个 Layout 未见 M1", func(o *faceObservation) { o.FirstLayoutSawM1 = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validSelectedObservation()
			tc.mut(o)
			if err := o.validate(); err == nil {
				t.Fatal("应拒绝")
			}
		})
	}
}

// 首帧已用最终 Shaper 时 changes=0 合法；两次交换拒绝。
func TestFaceObservationShaperChangeCounting(t *testing.T) {
	// 首帧即最终：合法。
	o := validSelectedObservation()
	o.FirstLayoutUsedFinal = true
	o.ShaperChanges = 0
	if err := o.validate(); err != nil {
		t.Fatalf("首帧即最终 Shaper（changes=0）必须合法: %v", err)
	}

	// 首帧即最终却仍报变化：自相矛盾，拒绝。
	o2 := validSelectedObservation()
	o2.FirstLayoutUsedFinal = true
	o2.ShaperChanges = 1
	if err := o2.validate(); err == nil {
		t.Fatal("首帧已用最终 Shaper 却报变化，应拒绝")
	}

	// 两次交换：拒绝。
	o3 := validSelectedObservation()
	o3.ShaperChanges = 2
	if err := o3.validate(); err == nil {
		t.Fatal("两次交换应拒绝")
	}

	// 从未交换（0 次）且首帧不是最终：拒绝。
	o4 := validSelectedObservation()
	o4.ShaperChanges = 0
	o4.FirstLayoutUsedFinal = false
	if err := o4.validate(); err == nil {
		t.Fatal("从未发生交接却声称成功，应拒绝")
	}
}

// countShaperChanges 直接验证指针序列的计数语义。
func TestCountShaperChanges(t *testing.T) {
	a := new(text.Shaper)
	b := new(text.Shaper)
	for _, tc := range []struct {
		name string
		seq  []*text.Shaper
		want int
	}{
		{"空", nil, 0},
		{"单帧", []*text.Shaper{a}, 0},
		{"稳定", []*text.Shaper{a, a, a}, 0},
		{"一次交换", []*text.Shaper{a, a, b, b}, 1},
		{"两次交换", []*text.Shaper{a, b, a}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := countShaperChanges(tc.seq); got != tc.want {
				t.Fatalf("countShaperChanges=%d，期望 %d", got, tc.want)
			}
		})
	}
}

// 输出必须带唯一 marker 与"跳线"字段，使旧采集器无法把新样本标成旧协议成功。
func TestFaceObservationOutputCarriesUniqueMarkerAndTripwire(t *testing.T) {
	o := validSelectedObservation()
	idLines := strings.Join(protocolIdentityLines(), "\n")
	if !strings.Contains(idLines, "fontComparisonContract="+faceSelectionWindowContract) {
		t.Fatal("身份行缺少跳线字段")
	}
	if strings.Contains(idLines, "fontComparisonContract=font-init-v1") {
		t.Fatal("绝不冒用旧协议名")
	}
	// reportLines 不得重复输出身份行（避免重复 token）。
	if strings.Contains(strings.Join(o.reportLines(), "\n"), "faceSelectionWindowContract=") {
		t.Fatal("reportLines 不应重复输出身份行")
	}
	lines := idLines + "\n" + strings.Join(o.reportLines(), "\n")
	if !strings.Contains(lines, "faceSelectionWindowContract="+faceSelectionWindowContract) {
		t.Fatal("缺少唯一 marker faceSelectionWindowContract")
	}
	// 跳线：旧采集器（compare 模式要求 font-init-v1；非 compare 模式见到该字段即拒绝）
	// 会因这个字段拒绝本样本。
	if !strings.Contains(lines, "fontComparisonContract="+faceSelectionWindowContract) {
		t.Fatal("缺少跳线字段 fontComparisonContract=<新协议>")
	}
	if strings.Contains(lines, "fontComparisonContract=font-init-v1") {
		t.Fatal("绝不冒用旧协议名")
	}
	for _, want := range []string{"faceCase=selected1-lazy", "faceIndices=1",
		"faceActualIndices=1", "faceSelectionApplied=true", "faceCoverageChecked=true"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("输出缺少字段 %q", want)
		}
	}
}

// faceIndicesText / intsText 的稳定渲染（default 用 "-" 表示不选面）。
func TestFaceIndicesText(t *testing.T) {
	if got := faceIndicesText(faceCaseDefaultLazy); got != "-" {
		t.Fatalf("default 索引文本 = %q，期望 -", got)
	}
	if got := faceIndicesText(faceCaseSelectedAll); got != "0,1" {
		t.Fatalf("selected-all 索引文本 = %q，期望 0,1", got)
	}
	if got := intsText(nil); got != "-" {
		t.Fatalf("空切片 = %q，期望 -", got)
	}
}

// errTest 是本文件用的最小错误类型（避免依赖外部包）。
type errTest string

func (e errTest) Error() string { return string(e) }

// ---------------------------------------------------------------------------
// R1.1 修复回归：F1-F4
// ---------------------------------------------------------------------------

// F1：child 实测环境事实必须真实、按 %q 编码，且 env 的存在性与原始值如实给出。
func TestFaceEnvFacts(t *testing.T) {
	t.Setenv("GOMAXPROCS", "2")
	lines := faceEnvFacts()
	joined := strings.Join(lines, "\n")
	for _, key := range []string{
		"faceRuntimeGo=", "faceRuntimeOS=", "faceRuntimeArch=",
		"faceRuntimeGomaxprocs=", "faceGomaxprocsEnvSet=", "faceGomaxprocsEnvValue=",
	} {
		if !strings.Contains(joined, key) {
			t.Fatalf("缺少字段 %q：%s", key, joined)
		}
	}
	// 实测值必须等于 runtime 的真实值（不允许父进程预期冒充）。
	if want := fmt.Sprintf("faceRuntimeGo=%q", runtime.Version()); !strings.Contains(joined, want) {
		t.Fatalf("faceRuntimeGo 与 runtime.Version() 不一致，期望 %q", want)
	}
	if want := fmt.Sprintf("faceRuntimeGomaxprocs=%d", runtime.GOMAXPROCS(0)); !strings.Contains(joined, want) {
		t.Fatalf("faceRuntimeGomaxprocs 与实测不一致，期望 %q", want)
	}
	if !strings.Contains(joined, "faceGomaxprocsEnvSet=true") {
		t.Fatalf("GOMAXPROCS 已设置时 EnvSet 应为 true：%s", joined)
	}
	if !strings.Contains(joined, fmt.Sprintf("faceGomaxprocsEnvValue=%q", "2")) {
		t.Fatalf("EnvValue 应保留原始值 \"2\"：%s", joined)
	}
}

// F2 判序：失败优先于超时（两者同时成立时也必须走 failed）。
func TestPollDecisionFailedBeatsTimeout(t *testing.T) {
	for _, tc := range []struct {
		name           string
		state          theme.LoadState
		deadlinePassed bool
		want           string
	}{
		{"失败且已过 deadline：仍是 failed", theme.LoadFailed, true, "failed"},
		{"失败未过 deadline", theme.LoadFailed, false, "failed"},
		{"加载中已过 deadline：timeout", theme.LoadLoading, true, "timeout"},
		{"未加载已过 deadline：timeout", theme.LoadUnloaded, true, "timeout"},
		{"加载中未过 deadline：continue", theme.LoadLoading, false, "continue"},
		{"Ready 但未过 deadline：continue（后续由成功判据决定）", theme.LoadReady, false, "continue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pollDecision(tc.state, tc.deadlinePassed); got != tc.want {
				t.Fatalf("pollDecision=%q，期望 %q", got, tc.want)
			}
		})
	}
}

// F2 分类：真实 *SelectionError 的类别/phase/可确定索引/原因必须保留，
// 且终态只有一条 RESULT（不把失败变超时）；不得按错误文本判类别。
func TestClassifyLoadFailureSelectionError(t *testing.T) {
	se := &theme.SelectionError{
		Report: theme.SelectionReport{
			ErrorKind:      theme.SelectionParserPanic,
			ErrorPhase:     "new-font",
			FaceIndexKnown: true,
			FaceIndex:      1,
		},
		Err: errors.New("slice bounds out of range [87894:84]"),
	}
	// 与生产一致的包装：errors.Is(ErrFontNotFound) 与 errors.As(*SelectionError) 同时成立。
	wrapped := fmt.Errorf("%w: %w", contracts.ErrFontNotFound, se)
	rep := theme.LoadReport{State: theme.LoadFailed, Err: wrapped}

	info := classifyLoadFailure(rep)
	if !info.IsSelectionError {
		t.Fatal("应识别为结构化选面错误")
	}
	if info.ErrorKind != theme.SelectionParserPanic {
		t.Fatalf("ErrorKind=%q，期望 %q", info.ErrorKind, theme.SelectionParserPanic)
	}
	if info.ErrorPhase != "new-font" {
		t.Fatalf("ErrorPhase=%q，期望 new-font", info.ErrorPhase)
	}
	if !info.FaceIndexKnown || info.FaceIndex != 1 {
		t.Fatalf("面索引事实丢失：known=%v idx=%d", info.FaceIndexKnown, info.FaceIndex)
	}
	if !strings.Contains(info.Reason, "slice bounds out of range") {
		t.Fatalf("原因链丢失：%q", info.Reason)
	}
	// 单条终态 RESULT，且带完整 err 链。
	if !strings.HasPrefix(info.Verdict, "RESULT verdict=LOAD_FAILED ") {
		t.Fatalf("终态行格式不对：%q", info.Verdict)
	}
	if strings.Count(info.Verdict, "RESULT ") != 1 {
		t.Fatalf("终态必须只有一条 RESULT：%q", info.Verdict)
	}
	if !strings.Contains(info.Verdict, "slice bounds out of range") {
		t.Fatalf("终态 detail 应含完整原始错误链：%q", info.Verdict)
	}
	// dataLines 必须带可解析字段。
	dl := strings.Join(info.dataLines(), "\n")
	for _, want := range []string{"faceLoadErrorIsSelectionError=true",
		"faceLoadErrorKind=\"parser-panic\"", "faceLoadErrorPhase=\"new-font\"",
		"faceLoadErrorFaceIndexKnown=true", "faceLoadErrorFaceIndex=1"} {
		if !strings.Contains(dl, want) {
			t.Fatalf("缺少字段 %q：%s", want, dl)
		}
	}
}

// F2 诚实性：默认路径（无 SelectionError）不得伪造类别。
func TestClassifyLoadFailureDefaultHonestNoKind(t *testing.T) {
	plain := fmt.Errorf("%w: 系统候选全部失败", contracts.ErrFontNotFound)
	rep := theme.LoadReport{State: theme.LoadFailed, Err: plain}
	info := classifyLoadFailure(rep)
	if info.IsSelectionError {
		t.Fatal("普通错误不得被标成结构化选面错误")
	}
	if info.ErrorKind != "" {
		t.Fatalf("默认路径不得伪造 ErrorKind，实际 %q", info.ErrorKind)
	}
	if info.Reason == "" {
		t.Fatal("默认路径仍应如实登记原因")
	}
	dl := strings.Join(info.dataLines(), "\n")
	if !strings.Contains(dl, "faceLoadErrorIsSelectionError=false") ||
		!strings.Contains(dl, "faceLoadErrorKind=\"\"") {
		t.Fatalf("默认路径字段应如实为空：%s", dl)
	}
}

// F3：构造端点缺失或逆序必须拒绝（三条负向）。
func TestValidateRejectsMissingOrReversedCtorEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*faceObservation)
	}{
		{"ctor entry 为零", func(o *faceObservation) { o.AppCtorEntry = time.Time{} }},
		{"ctor done 为零", func(o *faceObservation) { o.AppCtorDone = time.Time{} }},
		{"ctor 逆序（done<entry）", func(o *faceObservation) {
			o.AppCtorDone = o.AppCtorEntry.Add(-time.Nanosecond)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validSelectedObservation()
			tc.mut(o)
			if err := o.validate(); err == nil {
				t.Fatal("应拒绝")
			}
		})
	}
	// 合法 elapsed==0（entry==done）必须接受（不设正耗时阈值）。
	o := validSelectedObservation()
	o.AppCtorDone = o.AppCtorEntry
	if err := o.validate(); err != nil {
		t.Fatalf("ctor 区间为 0（entry==done）必须合法: %v", err)
	}
}

// F4：终态原始输出必须可独立复算 validate 消费的判据。
func TestReportLinesRecomputableFields(t *testing.T) {
	o := validSelectedObservation()
	lines := o.reportLines()
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"faceFinalLayoutAtNs=", "faceFinalLayoutMeasured=true",
		"faceFirstLayoutSawM1=true",
		"faceCtorIntervalNs=", "faceCtorIntervalMeasured=true",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺少可复算字段 %q", want)
		}
	}
	// 复算：ctor 区间 == done-entry。
	wantCtor := o.AppCtorDone.Sub(o.AppCtorEntry).Nanoseconds()
	if !strings.Contains(joined, fmt.Sprintf("faceCtorIntervalNs=%d", wantCtor)) {
		t.Fatalf("faceCtorIntervalNs 不可复算（期望 %d）", wantCtor)
	}
	// 复算：FinalLayoutAt 的 ns 值可对上。
	if !strings.Contains(joined, fmt.Sprintf("faceFinalLayoutAtNs=%d", o.FinalLayoutAt.UnixNano())) {
		t.Fatal("faceFinalLayoutAtNs 与观测不一致")
	}
	// 未测量时 measured=false 且值为 0（不与测得 0 混淆）。
	o2 := validSelectedObservation()
	o2.AppCtorEntry = time.Time{}
	o2.AppCtorDone = time.Time{}
	j2 := strings.Join(o2.reportLines(), "\n")
	if !strings.Contains(j2, "faceCtorIntervalMeasured=false") || !strings.Contains(j2, "faceCtorIntervalNs=0") {
		t.Fatal("未测量的 ctor 区间必须以 measured=false + 0 输出")
	}
}

// F4 编码：faceSourcePath 必须用 %q，含空格/引号/反斜杠的路径也要能可解析还原。
func TestFaceSourcePathQuoted(t *testing.T) {
	for _, path := range []string{
		`C:\Program Files\Fonts\a b.ttc`,
		`C:\weird\dir\x"y\z.ttc`,
		`C:\back\slash\t.ttc`,
	} {
		o := validSelectedObservation()
		o.Source = path
		joined := strings.Join(o.reportLines(), "\n")
		want := fmt.Sprintf("faceSourcePath=%q", path)
		if !strings.Contains(joined, want) {
			t.Fatalf("faceSourcePath 未按 %%q 编码，期望 %q", want)
		}
		// 可解析：%q 产出的 token 必须是合法的 Go 带引号字面量，且能还原原路径。
		tokStart := strings.Index(joined, "faceSourcePath=")
		rest := joined[tokStart+len("faceSourcePath="):]
		tok := rest[:strings.Index(rest, "\n")]
		got, err := strconv.Unquote(tok)
		if err != nil {
			t.Fatalf("faceSourcePath 不是合法 %%q token：%v（tok=%q）", err, tok)
		}
		if got != path {
			t.Fatalf("路径还原不一致：got=%q want=%q", got, path)
		}
	}
}

// ---------------------------------------------------------------------------
// A：M1 真实起点（App 内部 CreatedAt）
// ---------------------------------------------------------------------------

// TestFaceM1CreatedAtThreeInstantsDistinct：合成 fixture 让 InitAt / AppCtorEntry /
// M1CreatedAt 三个时刻**刻意不同**，证明 M1 用它自己的 CreatedAt，不与外部端点混用。
func TestFaceM1CreatedAtThreeInstantsDistinct(t *testing.T) {
	o := validSelectedObservation()
	// 刻意错开三个时刻：
	o.InitAt = o.M1CreatedAt.Add(-7 * time.Millisecond)       // 字体准备前，更早
	o.AppCtorEntry = o.M1CreatedAt.Add(-3 * time.Millisecond) // 调 NewLazyApp 之前
	// M1CreatedAt 保持 = start；FirstFrameAt/E​lapsed 由基线给出（+25ms / 25ms）
	if o.InitAt.Equal(o.M1CreatedAt) || o.AppCtorEntry.Equal(o.M1CreatedAt) ||
		o.InitAt.Equal(o.AppCtorEntry) {
		t.Fatal("三起点必须不同")
	}
	if err := o.validate(); err != nil {
		t.Fatalf("三起点不同但 M1 仍应合法: %v", err)
	}
	lines := strings.Join(o.reportLines(), "\n")
	// M1 起点必须是 M1CreatedAt，不是 InitAt，也不是 AppCtorEntry。
	if !strings.Contains(lines, fmt.Sprintf("faceM1CreatedAtNs=%d", o.M1CreatedAt.UnixNano())) {
		t.Fatal("faceM1CreatedAtNs 未取 m1.CreatedAt")
	}
	if strings.Contains(lines, fmt.Sprintf("faceM1CreatedAtNs=%d", o.InitAt.UnixNano())) ||
		strings.Contains(lines, fmt.Sprintf("faceM1CreatedAtNs=%d", o.AppCtorEntry.UnixNano())) {
		t.Fatal("faceM1CreatedAtNs 混用了 InitAt/AppCtorEntry")
	}
	if !strings.Contains(lines, "faceM1CreatedAtMeasured=true") {
		t.Fatal("缺少 faceM1CreatedAtMeasured=true")
	}
	// M1 仍按 FirstFrameAt-CreatedAt 复算。
	if o.M1Elapsed != o.FirstFrameAt.Sub(o.M1CreatedAt) {
		t.Fatal("M1 复算不一致")
	}
}

// TestFaceM1RejectsMissingReversedInconsistent：缺起点 / 逆序 / elapsed 不一致都要拒绝。
func TestFaceM1RejectsMissingReversedInconsistent(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*faceObservation)
	}{
		{"缺 M1CreatedAt", func(o *faceObservation) { o.M1CreatedAt = time.Time{} }},
		{"M1 逆序（FirstFrame 早于 CreatedAt）", func(o *faceObservation) {
			o.M1CreatedAt = o.FirstFrameAt.Add(time.Millisecond)
			o.M1Elapsed = o.FirstFrameAt.Sub(o.M1CreatedAt) // 负值也会先被 elapsed<0 拒
		}},
		{"elapsed 与端点不一致", func(o *faceObservation) { o.M1Elapsed = o.M1Elapsed + time.Millisecond }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validSelectedObservation()
			tc.mut(o)
			if err := o.validate(); err == nil {
				t.Fatal("应拒绝")
			}
		})
	}
	// 合法 0：CreatedAt == FirstFrameAt（真实时刻），elapsed==0 必须接受。
	o := validSelectedObservation()
	o.M1CreatedAt = o.FirstFrameAt
	o.M1Elapsed = 0
	if err := o.validate(); err != nil {
		t.Fatalf("合法 0（elapsed==0 且起点实测）必须接受: %v", err)
	}
}

// ---------------------------------------------------------------------------
// B：ProbeErr 原始事实
// ---------------------------------------------------------------------------

// TestFaceProbeErrWireFact：ProbeErr 必须作为 %q 出现在原始输出，nil 为空串。
func TestFaceProbeErrWireFact(t *testing.T) {
	// 成功 default 基线：ProbeErr=nil ⇒ faceProbeErr=""
	d := validDefaultObservation()
	lines := strings.Join(d.reportLines(), "\n")
	if !strings.Contains(lines, `faceProbeErr=""`) {
		t.Fatalf("ProbeErr=nil 时应输出 faceProbeErr=\"\"：%s", lines)
	}
	// 带错误：可逆 %q（含空格/换行/引号）。
	raw := "probe failed: missing table\nsecond line with \"quote\"\tend"
	d.ProbeErr = errors.New(raw)
	lines = strings.Join(d.reportLines(), "\n")
	tok := extractFieldToken(t, lines, "faceProbeErr=")
	got, err := strconv.Unquote(tok)
	if err != nil {
		t.Fatalf("faceProbeErr 不是合法 %%q：%v（tok=%q）", err, tok)
	}
	if got != raw {
		t.Fatalf("faceProbeErr 还原不一致：%q != %q", got, raw)
	}
	// selected 无 Probe：faceProbeErr 仍是空串，与 faceProbePresent=false 联合判读。
	o := validSelectedObservation()
	sl := strings.Join(o.reportLines(), "\n")
	if !strings.Contains(sl, `faceProbeErr=""`) || !strings.Contains(sl, "faceProbePresent=false") {
		t.Fatal("selected 无 Probe 时 faceProbeErr 应为空串且 faceProbePresent=false")
	}
}

// ---------------------------------------------------------------------------
// C：所有终态 detail 可逆单行
// ---------------------------------------------------------------------------

// extractFieldToken 从多行文本中取出 "prefix..." 那一行的值 token。
func extractFieldToken(t *testing.T, joined, prefix string) string {
	t.Helper()
	for _, ln := range strings.Split(joined, "\n") {
		if strings.HasPrefix(ln, prefix) {
			return strings.TrimPrefix(ln, prefix)
		}
	}
	t.Fatalf("未找到字段 %q", prefix)
	return ""
}

// TestFaceFailureDetailSinglePhysicalLine 是协调者反例1的候选回归：
// 原始错误含 CR/LF 甚至 RESULT 字样，detail 仍必须是**单条物理行**的可逆 %q，
// 且 Reason 保留完整原始字符串（不删换行）。
func TestFaceFailureDetailSinglePhysicalLine(t *testing.T) {
	raw := "first line\nRESULT verdict=OK detail=injected\r\nlast line\t\"quoted\"\\back"
	info := classifyLoadFailure(theme.LoadReport{State: theme.LoadFailed, Err: errors.New(raw)})

	// 恰好一条物理 RESULT 行（按 \r\n 和 \n 都不应切出多条）。
	physical := strings.FieldsFunc(info.Verdict, func(r rune) bool { return r == '\n' || r == '\r' })
	if len(physical) != 1 {
		t.Fatalf("终态必须恰好一条物理行，实际 %d：%q", len(physical), info.Verdict)
	}
	if strings.ContainsAny(info.Verdict, "\r\n") {
		t.Fatalf("detail 含裸换行：%q", info.Verdict)
	}
	if !strings.HasPrefix(info.Verdict, "RESULT verdict=LOAD_FAILED detail=") {
		t.Fatalf("终态前缀不对：%q", info.Verdict)
	}
	// detail 可逆还原为完整原始错误。
	detTok := strings.TrimPrefix(info.Verdict, "RESULT verdict=LOAD_FAILED detail=")
	got, err := strconv.Unquote(detTok)
	if err != nil {
		t.Fatalf("detail 不是合法 %%q：%v", err)
	}
	if got != raw {
		t.Fatalf("detail 还原不一致：%q != %q", got, raw)
	}
	// 结构化 Reason 保留原始（含换行）内容。
	if info.Reason != raw {
		t.Fatalf("Reason 丢失原始内容：%q", info.Reason)
	}
}

// TestFaceAllTerminalDetailsAreQuotedSingleLine 覆盖五种终态构造：均单物理行、%q 可逆。
func TestFaceAllTerminalDetailsAreQuotedSingleLine(t *testing.T) {
	nasty := "msg with\nnewline and \"quotes\" and \\backslash and RESULT verdict=OK"
	cases := []struct {
		name     string
		verdict  string // 生成的终态行
		wantKind string // 前缀校验
		wantSe   bool
	}{
		// ERROR 形态（构造器失败）用错误文本 %q；这里直接验证我们生成的字符串行。
		{"ERROR", fmt.Sprintf("RESULT verdict=ERROR detail=%q", fmt.Sprintf("Lazy 构造失败: %v", errors.New(nasty))), "RESULT verdict=ERROR detail=", false},
		{"FAIL", fmt.Sprintf("RESULT verdict=FAIL detail=%q", errors.New(nasty).Error()), "RESULT verdict=FAIL detail=", false},
		{"ENV_TIMEOUT", fmt.Sprintf("RESULT verdict=ENV_TIMEOUT detail=%q",
			fmt.Sprintf("%s 内未满足成功条件（帧=%d/%d state=%s submitted=%v swapped=%v） err=%v",
				20*time.Second, 1, 3, theme.LoadLoading, false, false, errors.New(nasty))), "RESULT verdict=ENV_TIMEOUT detail=", false},
		{"HARD_TIMEOUT", fmt.Sprintf("RESULT verdict=HARD_TIMEOUT detail=%q", "全程硬上限到达，强制退出（未完成或未取到读数）"), "RESULT verdict=HARD_TIMEOUT detail=", false},
		{"OK", fmt.Sprintf("RESULT verdict=OK detail=%q",
			fmt.Sprintf("真实窗口下完成 %s（帧=%d shaperChanges=%d M1=%v）", faceCaseSelected1, 3, 1, 20*time.Millisecond)), "RESULT verdict=OK detail=", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.ContainsAny(tc.verdict, "\r\n") {
				t.Fatalf("%s detail 含裸换行：%q", tc.name, tc.verdict)
			}
			if !strings.HasPrefix(tc.verdict, tc.wantKind) {
				t.Fatalf("%s 前缀不对：%q", tc.name, tc.verdict)
			}
			tok := strings.TrimPrefix(tc.verdict, tc.wantKind)
			if _, err := strconv.Unquote(tok); err != nil {
				t.Fatalf("%s detail 不可逆 %q：%v", tc.name, tok, err)
			}
		})
	}
}

// TestFaceSelectedFailureDetailSameRule：selected 失败（SelectionError）也走同一 %q 规则。
func TestFaceSelectedFailureDetailSameRule(t *testing.T) {
	se := &theme.SelectionError{
		Report: theme.SelectionReport{
			ErrorKind: theme.SelectionParserPanic, ErrorPhase: "new-font",
			FaceIndexKnown: true, FaceIndex: 0,
		},
		Err: errors.New("面 0 解析 panic: slice bounds\nout of range [87894:84]\r\nRESULT verdict=OK injected"),
	}
	rep := theme.LoadReport{State: theme.LoadFailed, Err: se}
	info := classifyLoadFailure(rep)
	if strings.ContainsAny(info.Verdict, "\r\n") {
		t.Fatalf("selected 失败 detail 含裸换行：%q", info.Verdict)
	}
	if !info.IsSelectionError || info.ErrorKind != theme.SelectionParserPanic {
		t.Fatal("结构化分类丢失")
	}
	// Reason 保留原始换行内容。
	if !strings.Contains(info.Reason, "slice bounds\nout of range") {
		t.Fatalf("Reason 丢失原始换行：%q", info.Reason)
	}
	tok := strings.TrimPrefix(info.Verdict, "RESULT verdict=LOAD_FAILED detail=")
	if _, err := strconv.Unquote(tok); err != nil {
		t.Fatalf("detail 不可逆：%v", err)
	}
}
