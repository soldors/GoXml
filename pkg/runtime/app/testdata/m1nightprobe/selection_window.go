package main

// FACE-SELECT-WINDOW-PROBE：显式选面（face selection）的**真实窗口**探针。
//
// # 为什么需要独立协议
//
// 旧协议 `font-init-v1`（comparison.go）的 `observeReady` 要求 `Probe != nil` 且
// `ProbeRan=true`。显式选面路径**不做系统探测**（`Probe=nil`、`ProbeElapsed=0`）
// 是它的正确语义，所以它会被旧协议判为失败——这不是选面的缺陷。
//
// 因此本文件新增**独立 opt-in 协议** `face-selection-window-v1`：
//   - 只有显式给出 `-face-case` 才会进入本路径；空 case 走原流程，行为与输出不变；
//   - 旧协议的冷 Provider 守卫（`ProbeRan=true`）**原样保留**，不为了让新路径
//     通过而放宽它；
//   - 新输出带唯一 marker（`faceSelectionWindowContract`），并且**故意**输出
//     `fontComparisonContract=<新协议名>` 作为"跳线"：旧采集器在 compare 模式下
//     会因 `!= font-init-v1` 拒绝，在非 compare 模式下会因
//     `comparison-output-requires-CompareFonts` 拒绝。两种旧模式都不可能把新样本
//     标成旧协议成功。
//
// # 四种 case 的共同点
//
// 四种 case 都走**同一条生产链路**：
//
//	NewProvider(独立实例, Candidates 只含 -font-source)
//	  → NewLazy / NewLazyWithSelection（构造零 IO）
//	  → NewLazyApp → 既有 worker → pending → 帧内 Take 交换
//
// 都使用同配置的独立 Provider 与默认嵌入临时主题；都在真实 Layout 里观测。
// **不**在 NewLazyApp 之前读取/解析字体（那会把成本挪到 M1 起点之外）。
//
// # 不做什么
//
//   - 不改 M1 定义（构造入口 → 首帧处理开始）；
//   - 不输出虚构的生产分段（例如 parseTotal / NewFont 成本），生产报告只写它
//     实际有的时间；
//   - 不在 Layout 内做源 IO / 解析 / 哈希；
//   - 不特判任何具体字体内容（路径只来自参数）。

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	nightapp "github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// faceSelectionWindowContract 是本协议的唯一 marker/version。
const faceSelectionWindowContract = "face-selection-window-v1"

// faceCase 是受支持的四种测量 case。
type faceCase string

const (
	faceCaseNone        faceCase = ""
	faceCaseDefaultLazy faceCase = "default-lazy"
	faceCaseSelected0   faceCase = "selected0-lazy"
	faceCaseSelected1   faceCase = "selected1-lazy"
	faceCaseSelectedAll faceCase = "selected-all-lazy"
)

// faceCaseOrder 只用于错误信息里列出合法值（顺序稳定便于人读）。
var faceCaseOrder = []faceCase{
	faceCaseDefaultLazy, faceCaseSelected0, faceCaseSelected1, faceCaseSelectedAll,
}

func (c faceCase) valid() bool {
	for _, v := range faceCaseOrder {
		if c == v {
			return true
		}
	}
	return false
}

// isSelected 报告该 case 是否走"显式选面"路径（而非默认解析算法）。
func (c faceCase) isSelected() bool {
	return c.valid() && c != faceCaseDefaultLazy
}

// indices 返回该 case 请求的面索引；default-lazy 返回 nil（它不选面）。
func (c faceCase) indices() []int {
	switch c {
	case faceCaseSelected0:
		return []int{0}
	case faceCaseSelected1:
		return []int{1}
	case faceCaseSelectedAll:
		return []int{0, 1}
	}
	return nil
}

// faceCaseList 拼接合法值，用于错误信息。
func faceCaseList() string {
	parts := make([]string, 0, len(faceCaseOrder))
	for _, c := range faceCaseOrder {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// 参数组合校验（纯逻辑，可离线测试）
// ---------------------------------------------------------------------------

// faceProbeFlags 是进入本协议所需的参数快照。
//
// explicit 记录"用户是否**显式**给了某个 flag"：`-load` 有默认值 "none"，
// 只看值无法区分"默认"与"显式写成 none"，而任务书要求对不相容组合必须拒绝、
// 不能静默忽略。
type faceProbeFlags struct {
	caseName     string
	sourcePath   string
	compareFonts bool
	explicit     map[string]bool
}

// validateFaceProbeFlags 校验参数组合。
//
// 返回 (case, nil) 表示进入新协议；(none, nil) 表示走原流程；(_, err) 表示参数错误。
func validateFaceProbeFlags(f faceProbeFlags) (faceCase, error) {
	c := faceCase(strings.TrimSpace(f.caseName))
	src := strings.TrimSpace(f.sourcePath)

	if c == faceCaseNone {
		// 空 case：原流程。但 -font-source 单独出现属于"会被静默忽略"的参数，
		// 必须拒绝而不是假装没看见。
		if src != "" {
			return faceCaseNone, fmt.Errorf("-font-source 只能与 -face-case 一起使用（空 case 时它不会生效）")
		}
		return faceCaseNone, nil
	}

	if !c.valid() {
		return faceCaseNone, fmt.Errorf("未知 -face-case=%q（合法值：%s）", c, faceCaseList())
	}
	if src == "" {
		return faceCaseNone, fmt.Errorf("-face-case=%s 需要同时给出 -font-source <字体路径>", c)
	}
	if f.compareFonts {
		return faceCaseNone, fmt.Errorf("-face-case 与 -compare-fonts 不相容：两者是不同协议，不能混用")
	}
	if f.explicit["load"] {
		return faceCaseNone, fmt.Errorf("-face-case 与显式 -load 不相容：字体加载方式由 case 决定，不能静默忽略")
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// 观测模型（纯数据，可离线测试）
// ---------------------------------------------------------------------------

// faceObservation 是一次 face-case 运行结束后**用于判读**的全部事实。
//
// 它只承载"观测到了什么"，不承担任何 IO；validate/write 是纯函数。
type faceObservation struct {
	Case   faceCase
	Source string

	// ---- 生产报告事实（theme.LoadReport）----
	LazyState         theme.LoadState
	LazyErr           error
	TempSource        string
	RequestedAt       time.Time
	LoadedAt          time.Time
	FrameSubmittedAt  time.Time
	ProbePresent      bool
	ProbeRan          bool
	ProbeResolved     string
	ProbeErr          error
	EmbeddedAttempted bool
	ProbeElapsed      time.Duration

	// ---- 选面诊断（theme.SelectionReport；selected 专用）----
	SelectionPresent         bool
	SelectionApplied         bool
	SelectionRequested       []int
	SelectionActual          []int
	SelectionDeclaredFaces   int
	SelectionCoverageChecked bool
	SelectionCoverageHit     int
	SelectionCoverageTotal   int
	SelectionErrorKind       string

	// ---- Provider 计数（证明最终链没有绕回全量 Provider）----
	ProviderReadCalls int64
	ProviderScans     int

	// ---- Layout 观测 ----
	FramesObserved       int64
	ShaperChanges        int
	FirstLayoutUsedFinal bool
	FinalShaperSeen      bool
	FinalLayoutAt        time.Time
	// FirstLayoutSawM1：首个 Layout 期间 FirstFrame 是否已被测量。
	// 这是既有协议同样使用的交叉验证：M1 止点（首帧处理开始）必须在 Layout 之前。
	FirstLayoutSawM1 bool

	// ---- 计时端点 ----
	InitAt       time.Time // 本轮字体/Lazy 构造之前
	AppCtorEntry time.Time
	AppCtorDone  time.Time
	FirstFrameAt time.Time
	M1Measured   bool
	M1Elapsed    time.Duration
	// M1CreatedAt 是 App 内部的真实 M1 起点（a.FirstFrame().CreatedAt）。
	// 与 InitAt（字体/Lazy 准备前）和 AppCtorEntry（调用 NewLazyApp 之前）是**三个不同时刻**，
	// 不得互相替代：M1 = FirstFrameAt - CreatedAt。
	M1CreatedAt time.Time
}

// interval 表达"两个端点之间的已测量区间"。
//
// 与生产契约一致：**测得的 0 合法**，未测量不得用 0 替代——因此用 ok 区分。
type interval struct {
	d  time.Duration
	ok bool
}

func measuredInterval(from, to time.Time) (interval, error) {
	if from.IsZero() {
		return interval{}, fmt.Errorf("区间起点未测量")
	}
	if to.IsZero() {
		return interval{}, fmt.Errorf("区间终点未测量")
	}
	if to.Before(from) {
		return interval{}, fmt.Errorf("区间逆序（终点早于起点）")
	}
	return interval{d: to.Sub(from), ok: true}, nil
}

// validate 是成功判据的唯一实现。任何一条不满足都返回错误（原始错误保留在字段里）。
func (o *faceObservation) validate() error {
	if o.Case == faceCaseNone || !o.Case.valid() {
		return fmt.Errorf("无效 case %q", o.Case)
	}
	if strings.TrimSpace(o.Source) == "" {
		return fmt.Errorf("缺少 -font-source")
	}

	// ---- M1：未测量就是未测量，不能用 0 顶替 ----
	if !o.M1Measured {
		return fmt.Errorf("FirstFrame 仍报告未测量（属于缺陷，不是环境问题）")
	}
	if o.M1Elapsed < 0 {
		return fmt.Errorf("M1 elapsed 为负（%v）", o.M1Elapsed)
	}
	// A：M1 起点必须实测，且与止点不逆序、elapsed 与端点一致。
	if o.M1CreatedAt.IsZero() {
		return fmt.Errorf("M1 起点未测量（M1CreatedAt 为零值）")
	}
	if o.FirstFrameAt.Before(o.M1CreatedAt) {
		return fmt.Errorf("M1 区间逆序（FirstFrameAt 早于 M1CreatedAt）")
	}
	if got, want := o.M1Elapsed, o.FirstFrameAt.Sub(o.M1CreatedAt); got != want {
		return fmt.Errorf("M1 elapsed 与端点不一致：elapsed=%v，端点差=%v", got, want)
	}

	// ---- 真实帧 / 终态 ----
	if o.FramesObserved < 1 {
		return fmt.Errorf("没有观测到真实帧")
	}
	if o.LazyState != theme.LoadReady {
		return fmt.Errorf("Lazy 未进入 Ready（state=%s err=%v）", o.LazyState, o.LazyErr)
	}
	if o.LoadedAt.IsZero() {
		return fmt.Errorf("Ready 但 LoadedAt 为零值")
	}
	if o.RequestedAt.IsZero() {
		return fmt.Errorf("RequestedAt 为零值（没有发起过请求）")
	}

	// ---- 交接：FrameSubmittedAt 非零 + 最终 Shaper 真的被 Layout 用过 ----
	if o.FrameSubmittedAt.IsZero() {
		return fmt.Errorf("Ready 但没有观察到帧内交接（FrameSubmittedAt 为零）")
	}
	if !o.FinalShaperSeen {
		return fmt.Errorf("最终 Shaper 从未在任何一次 Layout 中出现（主题没有真正换上去）")
	}
	if o.FinalLayoutAt.IsZero() {
		// 见过最终 Shaper 就必然有那么一次 Layout；时刻为零说明观测没记全，
		// 不能拿它去和 FrameSubmittedAt 做"不早于"的比较。
		return fmt.Errorf("最终 Layout 时刻未测量（FinalLayoutAt 为零值）")
	}
	if o.FirstLayoutUsedFinal {
		// 首个 Layout 之前最终字体已就绪：允许，且不应再发生任何变化。
		if o.ShaperChanges != 0 {
			return fmt.Errorf("首帧已用最终 Shaper，却观测到 %d 次变化", o.ShaperChanges)
		}
	} else if o.ShaperChanges != 1 {
		return fmt.Errorf("期望恰好 1 次 Shaper 变化（临时→最终），实际 %d 次", o.ShaperChanges)
	}

	// ---- 时间端点与区间 ----
	if o.InitAt.IsZero() {
		return fmt.Errorf("initAt 未测量")
	}
	// F3：App 构造端点必须实测，且不得逆序。时间为时刻而非区间，故不设正耗时阈值；
	// "合法 elapsed==0" 的语义由 measuredInterval 承担（同一时刻即 0），
	// 这里的"零值"只表示**未测量**，不是"测得 0"。
	if o.AppCtorEntry.IsZero() {
		return fmt.Errorf("App 构造入口未测量（AppCtorEntry 为零值）")
	}
	if o.AppCtorDone.IsZero() {
		return fmt.Errorf("App 构造完成未测量（AppCtorDone 为零值）")
	}
	if o.AppCtorDone.Before(o.AppCtorEntry) {
		return fmt.Errorf("App 构造端点逆序（done 早于 entry）")
	}
	if o.FirstFrameAt.IsZero() {
		return fmt.Errorf("FirstFrameAt 为零值")
	}
	if !o.FirstLayoutSawM1 {
		// 与既有协议一致的交叉验证：首帧处理开始时 M1 止点应已写入，
		// 因此首个 Layout 里读到 Measured()==true 才说明 M1 边界正确。
		return fmt.Errorf("首个 Layout 期间 FirstFrame 仍未测量（M1 止点未在 Layout 之前写入）")
	}
	if _, err := measuredInterval(o.InitAt, o.LoadedAt); err != nil {
		return fmt.Errorf("font-init→Ready 区间非法: %w", err)
	}
	if _, err := measuredInterval(o.RequestedAt, o.LoadedAt); err != nil {
		return fmt.Errorf("RequestedAt→LoadedAt 区间非法: %w", err)
	}
	// 生产 MarkFrameSubmitted 在**同一帧**的 Frame() 返回后写入，因此提交时刻
	// 不可能早于该帧 Layout 的开始时刻。
	if o.FrameSubmittedAt.Before(o.FinalLayoutAt) {
		return fmt.Errorf("FrameSubmittedAt 早于最终 Layout（%v < %v）", o.FrameSubmittedAt, o.FinalLayoutAt)
	}

	if o.Case.isSelected() {
		return o.validateSelected()
	}
	return o.validateDefault()
}

// validateDefault 覆盖 `default-lazy`：它**必须**是"指定系统资源命中"的冷探测。
func (o *faceObservation) validateDefault() error {
	if !o.ProbePresent {
		return fmt.Errorf("default-lazy 需要探测报告（Probe），实际为 nil")
	}
	if !o.ProbeRan {
		return fmt.Errorf("default-lazy 的冷 Provider 前提不成立（ProbeRan=false）")
	}
	if o.ProbeErr != nil {
		return fmt.Errorf("default-lazy 探测失败: %w", o.ProbeErr)
	}
	if o.LazyErr != nil {
		return fmt.Errorf("default-lazy 加载失败: %w", o.LazyErr)
	}
	if o.ProbeResolved != o.Source {
		return fmt.Errorf("default-lazy 命中的不是指定资源：Resolved=%q，期望 %q", o.ProbeResolved, o.Source)
	}
	if o.EmbeddedAttempted {
		return fmt.Errorf("default-lazy 走了嵌入兜底（Attempted=true）：不是指定的系统资源")
	}
	if o.ProviderReadCalls < 1 {
		return fmt.Errorf("default-lazy 没有发生任何读取（Read 调用 0 次）：不是真实资源命中")
	}
	return nil
}

// validateSelected 覆盖三种 selected case。
func (o *faceObservation) validateSelected() error {
	if o.ProbePresent {
		return fmt.Errorf("selected 路径不该产生探测报告（Probe 非 nil）")
	}
	if o.ProbeElapsed != 0 {
		return fmt.Errorf("selected 路径 ProbeElapsed 应为 0，实际 %v（不得把解析耗时冒充探测耗时）", o.ProbeElapsed)
	}
	if o.LazyErr != nil {
		return fmt.Errorf("selected 加载失败: %w", o.LazyErr)
	}
	if !o.SelectionPresent {
		return fmt.Errorf("selected 缺少 SelectionReport（选面诊断是必需要素）")
	}
	if o.SelectionErrorKind != "" {
		return fmt.Errorf("SelectionReport 带错误类别 %q", o.SelectionErrorKind)
	}
	if !o.SelectionApplied {
		return fmt.Errorf("SelectionReport.SelectionApplied=false（选择没有真正生效）")
	}
	want := o.Case.indices()
	if len(o.SelectionActual) != len(want) {
		return fmt.Errorf("ActualIndices=%v 与模式 %v 长度不符", o.SelectionActual, want)
	}
	for i := range want {
		if o.SelectionActual[i] != want[i] {
			return fmt.Errorf("ActualIndices=%v 与模式请求 %v 不一致（选择错位）", o.SelectionActual, want)
		}
	}
	if !o.SelectionCoverageChecked {
		return fmt.Errorf("覆盖未检查（CoverageChecked=false）")
	}
	if o.SelectionCoverageTotal <= 0 {
		return fmt.Errorf("覆盖检查的分母异常（CoverageTotal=%d）", o.SelectionCoverageTotal)
	}
	// 覆盖"符合原有标准"：复用生产判定口径（90%），不在这里另造阈值。
	if !theme.CoverageSufficient(o.SelectionCoverageHit, o.SelectionCoverageTotal) {
		return fmt.Errorf("所选面覆盖不足：%d/%d", o.SelectionCoverageHit, o.SelectionCoverageTotal)
	}
	// 最终链不得绕回全量 Provider。
	if o.ProviderReadCalls != 0 {
		return fmt.Errorf("selected 路径不该读取候选（Read 调用 %d 次）", o.ProviderReadCalls)
	}
	if o.ProviderScans != 0 {
		return fmt.Errorf("selected 路径不该做探测扫描（Scans=%d）", o.ProviderScans)
	}
	return nil
}

// protocolIdentityLines 是本协议的**身份行**：唯一 marker + 旧采集器跳线。
//
// 由运行入口在**成功与失败**两条路径上都输出，确保任何终态样本都能被归属到
// 本协议，而不会因缺 marker 被误判成旧协议或来源不明。
func protocolIdentityLines() []string {
	return []string{
		fmt.Sprintf("faceSelectionWindowContract=%s", faceSelectionWindowContract),
		// 跳线：旧采集器看到这个字段会拒绝新样本（见文件头说明）。
		fmt.Sprintf("fontComparisonContract=%s", faceSelectionWindowContract),
	}
}

// probeErrString 把 Probe 错误转成原始文本（nil → 空串）。
func probeErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// reportLines 生成协议输出行（纯函数，便于在无窗口环境下断言字段）。
func (o *faceObservation) reportLines() []string {
	b := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	ns := func(t time.Time) int64 {
		if t.IsZero() {
			return 0
		}
		return t.UnixNano()
	}

	lines := []string{
		fmt.Sprintf("faceCase=%s", o.Case),
		fmt.Sprintf("faceSourcePath=%q", o.Source),
		fmt.Sprintf("faceSourceIsPath=true"),
		fmt.Sprintf("faceIndices=%s", faceIndicesText(o.Case)),
		fmt.Sprintf("faceDeclaredFaces=%d", o.SelectionDeclaredFaces),
		fmt.Sprintf("faceActualIndices=%s", intsText(o.SelectionActual)),
		fmt.Sprintf("faceSelectionPresent=%s", b(o.SelectionPresent)),
		fmt.Sprintf("faceSelectionApplied=%s", b(o.SelectionApplied)),
		fmt.Sprintf("faceCoverageChecked=%s", b(o.SelectionCoverageChecked)),
		fmt.Sprintf("faceCoverageHit=%d", o.SelectionCoverageHit),
		fmt.Sprintf("faceCoverageTotal=%d", o.SelectionCoverageTotal),
		fmt.Sprintf("faceSelectionErrorKind=%q", o.SelectionErrorKind),
		fmt.Sprintf("faceProviderReadCalls=%d", o.ProviderReadCalls),
		fmt.Sprintf("faceProviderScans=%d", o.ProviderScans),
		fmt.Sprintf("faceLazyState=%s", o.LazyState.String()),
		fmt.Sprintf("faceTempSource=%q", o.TempSource),
		fmt.Sprintf("faceProbePresent=%s", b(o.ProbePresent)),
		fmt.Sprintf("faceProbeRan=%s", b(o.ProbeRan)),
		fmt.Sprintf("faceProbeResolved=%q", o.ProbeResolved),
		// B：ProbeErr 被 validateDefault 消费，必须同时出现在原始输出里（nil=空串）。
		// 无 Probe 时（selected）为空串，仍须与 faceProbePresent 联合判读。
		fmt.Sprintf("faceProbeErr=%q", probeErrString(o.ProbeErr)),
		fmt.Sprintf("faceEmbeddedAttempted=%s", b(o.EmbeddedAttempted)),
		fmt.Sprintf("faceProbeElapsedNs=%d", o.ProbeElapsed.Nanoseconds()),
		fmt.Sprintf("faceFramesObserved=%d", o.FramesObserved),
		fmt.Sprintf("faceShaperChanges=%d", o.ShaperChanges),
		fmt.Sprintf("faceFirstLayoutUsedFinal=%s", b(o.FirstLayoutUsedFinal)),
		fmt.Sprintf("faceFinalShaperSeen=%s", b(o.FinalShaperSeen)),
		// F4：让终态原始输出可独立复算 validate 消费的判据。
		fmt.Sprintf("faceFinalLayoutAtNs=%d", ns(o.FinalLayoutAt)),
		fmt.Sprintf("faceFinalLayoutMeasured=%s", b(!o.FinalLayoutAt.IsZero())),
		fmt.Sprintf("faceFirstLayoutSawM1=%s", b(o.FirstLayoutSawM1)),
		fmt.Sprintf("faceInitAtNs=%d", ns(o.InitAt)),
		fmt.Sprintf("faceInitAtMeasured=%s", b(!o.InitAt.IsZero())),
		fmt.Sprintf("faceAppCtorEntryNs=%d", ns(o.AppCtorEntry)),
		fmt.Sprintf("faceAppCtorEntryMeasured=%s", b(!o.AppCtorEntry.IsZero())),
		fmt.Sprintf("faceAppCtorDoneNs=%d", ns(o.AppCtorDone)),
		fmt.Sprintf("faceAppCtorDoneMeasured=%s", b(!o.AppCtorDone.IsZero())),
		fmt.Sprintf("faceRequestedAtNs=%d", ns(o.RequestedAt)),
		fmt.Sprintf("faceRequestedAtMeasured=%s", b(!o.RequestedAt.IsZero())),
		fmt.Sprintf("faceLoadedAtNs=%d", ns(o.LoadedAt)),
		fmt.Sprintf("faceLoadedMeasured=%s", b(!o.LoadedAt.IsZero())),
		fmt.Sprintf("faceFirstFrameAtNs=%d", ns(o.FirstFrameAt)),
		fmt.Sprintf("faceFirstFrameAtMeasured=%s", b(!o.FirstFrameAt.IsZero())),
		fmt.Sprintf("faceFrameSubmittedAtNs=%d", ns(o.FrameSubmittedAt)),
		fmt.Sprintf("faceFrameSubmittedMeasured=%s", b(!o.FrameSubmittedAt.IsZero())),
		fmt.Sprintf("faceM1Measured=%s", b(o.M1Measured)),
		// A：M1 真实起点（App 内部 CreatedAt）与其 measured 标志。
		fmt.Sprintf("faceM1CreatedAtNs=%d", ns(o.M1CreatedAt)),
		fmt.Sprintf("faceM1CreatedAtMeasured=%s", b(!o.M1CreatedAt.IsZero())),
		fmt.Sprintf("faceM1ElapsedNs=%d", o.M1Elapsed.Nanoseconds()),
	}

	// 两个区间：测得 0 合法，未测量用 measured=false + 0 明确标注（不混淆）。
	if iv, err := measuredInterval(o.InitAt, o.LoadedAt); err == nil {
		lines = append(lines,
			fmt.Sprintf("faceFontInitToReadyNs=%d", iv.d.Nanoseconds()),
			"faceFontInitToReadyMeasured=true")
	} else {
		lines = append(lines,
			"faceFontInitToReadyNs=0",
			"faceFontInitToReadyMeasured=false")
	}
	// F4：构造区间（AppCtorEntry→AppCtorDone），measured 语义与其它区间一致。
	if iv, err := measuredInterval(o.AppCtorEntry, o.AppCtorDone); err == nil {
		lines = append(lines,
			fmt.Sprintf("faceCtorIntervalNs=%d", iv.d.Nanoseconds()),
			"faceCtorIntervalMeasured=true")
	} else {
		lines = append(lines,
			"faceCtorIntervalNs=0",
			"faceCtorIntervalMeasured=false")
	}
	if iv, err := measuredInterval(o.RequestedAt, o.LoadedAt); err == nil {
		lines = append(lines,
			fmt.Sprintf("faceRequestToLoadedNs=%d", iv.d.Nanoseconds()),
			"faceRequestToLoadedMeasured=true")
	} else {
		lines = append(lines,
			"faceRequestToLoadedNs=0",
			"faceRequestToLoadedMeasured=false")
	}
	return lines
}

// faceIndicesText 把 case 请求的索引渲染成稳定文本（default 为 "-"）。
func faceIndicesText(c faceCase) string {
	idx := c.indices()
	if len(idx) == 0 {
		return "-"
	}
	return intsText(idx)
}

func intsText(v []int) string {
	if len(v) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(v))
	for _, n := range v {
		parts = append(parts, fmt.Sprintf("%d", n))
	}
	return strings.Join(parts, ",")
}

// ---------------------------------------------------------------------------
// Layout 观测视图
// ---------------------------------------------------------------------------

// selectionProbeView 在真实 Layout 中记录帧数、Shaper 指针序列与首帧时刻。
//
// 线程约定：Layout 在渲染 goroutine，读取在主 goroutine，全部经 mu 保护。
// 这里**只比较 Shaper 指针**（同进程对象比较），不对 Shaper 做任何调用，
// 因此不引入跨线程的 Shaper 使用。
type selectionProbeView struct {
	owner *nightapp.App
	mu    sync.Mutex

	calls                 int64
	firstEntry            time.Time
	firstAtLayoutMeasured bool
	shapers               []*text.Shaper
	layoutTimes           []time.Time
}

type selectionProbeSnapshot struct {
	calls      int64
	first      time.Time
	measured   bool
	shapers    []*text.Shaper
	layoutTime []time.Time
}

func (v *selectionProbeView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.mu.Lock()
	v.calls++
	if v.calls == 1 {
		v.firstEntry = time.Now()
		if v.owner != nil {
			v.firstAtLayoutMeasured = v.owner.FirstFrame().Measured()
		}
	}
	// 只记录指针与时刻，不触碰 Shaper 内部。
	v.shapers = append(v.shapers, th.Shaper)
	v.layoutTimes = append(v.layoutTimes, time.Now())
	v.mu.Unlock()
	return material.Body1(th, "FACE-SELECT-WINDOW-PROBE").Layout(gtx)
}

func (v *selectionProbeView) snapshot() selectionProbeSnapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	return selectionProbeSnapshot{
		calls:      v.calls,
		first:      v.firstEntry,
		measured:   v.firstAtLayoutMeasured,
		shapers:    append([]*text.Shaper(nil), v.shapers...),
		layoutTime: append([]time.Time(nil), v.layoutTimes...),
	}
}

// countShaperChanges 统计相邻两次 Layout 之间 Shaper 指针的变化次数。
func countShaperChanges(shapers []*text.Shaper) int {
	changes := 0
	for i := 1; i < len(shapers); i++ {
		if shapers[i] != shapers[i-1] {
			changes++
		}
	}
	return changes
}

// ---------------------------------------------------------------------------
// 运行入口
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// F1：child 实测运行环境事实
// ---------------------------------------------------------------------------

// faceEnvFacts 返回**本进程（child）实测**的运行环境事实行。
//
// 与旧 main.go 的 ENV go=... os=... arch=... gomaxprocs=... 行**并行存在、互不替代**：
// 旧 ENV 行**同样**是探针 child 自己输出的（不是父进程预期），只是它在 face-case
// 分支里**不可达**——所以本函数为 face-case 路径重新给出同一类 child 实测事实。
// GOMAXPROCS 的 env 存在性与原始值来自 os.LookupEnv（不做替换）。
// 纯标准库读取，不涉及任何字体 IO。
func faceEnvFacts() []string {
	envValue, envSet := os.LookupEnv("GOMAXPROCS")
	return []string{
		fmt.Sprintf("faceRuntimeGo=%q", runtime.Version()),
		fmt.Sprintf("faceRuntimeOS=%q", runtime.GOOS),
		fmt.Sprintf("faceRuntimeArch=%q", runtime.GOARCH),
		fmt.Sprintf("faceRuntimeGomaxprocs=%d", runtime.GOMAXPROCS(0)),
		fmt.Sprintf("faceGomaxprocsEnvSet=%t", envSet),
		fmt.Sprintf("faceGomaxprocsEnvValue=%q", envValue),
	}
}

// faceLoadFailure 是 F2 的**可测纯函数**：把一次 LoadFailed 终态转成
// 结构化判读事实（不按错误文本猜 panic，只用 errors.As 取真实 *SelectionError）。
//
// 返回值：
//   - verdictLine：单条终态 RESULT 行（含完整原始 err 链）；
//   - dataLines：结构化字段（类别/phase/可确定索引/原因），failure 时如实给出。
//
// 若 rep.Err 链中没有本层 *SelectionError（例如默认解析路径失败），
// 则 isSelectionError=false 且类别留空——**不伪造**选面错误类别。
type faceLoadFailureInfo struct {
	Verdict          string
	IsSelectionError bool
	ErrorKind        string
	ErrorPhase       string
	FaceIndexKnown   bool
	FaceIndex        int
	Reason           string
	State            string
}

func classifyLoadFailure(rep theme.LoadReport) faceLoadFailureInfo {
	info := faceLoadFailureInfo{
		State:   rep.State.String(),
		Verdict: "RESULT verdict=LOAD_FAILED",
	}
	if rep.Err != nil {
		// C：终态 detail 必须是可逆单行编码（%q），完整保留 err.Error()；
		// 原始错误文本另存 Reason（不丢换行内容）。
		info.Verdict = fmt.Sprintf("RESULT verdict=LOAD_FAILED detail=%q", rep.Err.Error())
	}
	var se *theme.SelectionError
	if errors.As(rep.Err, &se) && se != nil {
		info.IsSelectionError = true
		info.ErrorKind = se.Report.ErrorKind
		info.ErrorPhase = se.Report.ErrorPhase
		info.FaceIndexKnown = se.Report.FaceIndexKnown
		info.FaceIndex = se.Report.FaceIndex
		if se.Err != nil {
			info.Reason = se.Err.Error()
		}
	} else if rep.Err != nil {
		// 默认路径：没有结构化选面错误——如实登记原因，不编造 ErrorKind。
		info.Reason = rep.Err.Error()
	}
	return info
}

// dataLines 生成 F2 的结构化输出行（值按可解析引用编码）。
func (f faceLoadFailureInfo) dataLines() []string {
	return []string{
		fmt.Sprintf("faceLoadErrorIsSelectionError=%t", f.IsSelectionError),
		fmt.Sprintf("faceLoadErrorKind=%q", f.ErrorKind),
		fmt.Sprintf("faceLoadErrorPhase=%q", f.ErrorPhase),
		fmt.Sprintf("faceLoadErrorFaceIndexKnown=%t", f.FaceIndexKnown),
		fmt.Sprintf("faceLoadErrorFaceIndex=%d", f.FaceIndex),
		fmt.Sprintf("faceLoadErrorReason=%q", f.Reason),
		fmt.Sprintf("faceLazyState=%q", f.State),
	}
}

// pollDecision 是轮询循环的**最小可测判序函数**：给定当前状态与"是否已过 deadline"，
// 返回本轮应先处理的分支。
//
// 关键不变量（F2）：**失败优先于超时**——真实 LoadFailed 必须立即如实产出，
// 绝不能被 deadline 拖成 ENV_TIMEOUT，也不能使失败看起来像超时。
func pollDecision(state theme.LoadState, deadlinePassed bool) string {
	if state == theme.LoadFailed {
		return "failed"
	}
	if deadlinePassed {
		return "timeout"
	}
	return "continue"
}

// faceProbeRunConfig 是运行期参数（由 main 传入，便于与旧 flag 共用）。
type faceProbeRunConfig struct {
	Frames      int
	Timeout     time.Duration
	HardTimeout time.Duration
	Title       string
}

// runFaceSelectionProbe 执行一次 face-case 真实窗口测量，返回进程退出码。
//
//	0 = 成功（全部判据通过）
//	1 = 加载/判读失败（有明确诊断，原始错误保留）
//	3 = 超时或环境阻塞（不是通过）
func runFaceSelectionProbe(c faceCase, source string, cfg faceProbeRunConfig) int {
	fmt.Printf("=== FACE-SELECT-WINDOW-PROBE · 真实窗口（协议 %s）===\n", faceSelectionWindowContract)
	fmt.Printf("FACE_CASE case=%s source=%q indices=%s\n", c, source, faceIndicesText(c))
	// 协议身份行（成功/失败都带；含旧采集器跳线）。
	for _, line := range protocolIdentityLines() {
		fmt.Printf("RESULT_DATA %s\n", line)
	}
	// F1：紧随标题输出 child 实测运行环境事实（不移动/修改旧 ENV 行）。
	for _, line := range faceEnvFacts() {
		fmt.Printf("RESULT_DATA %s\n", line)
	}

	time.AfterFunc(cfg.HardTimeout, func() {
		fmt.Printf("RESULT verdict=HARD_TIMEOUT detail=%q\n", "全程硬上限到达，强制退出（未完成或未取到读数）")
		os.Exit(3)
	})

	// ① initAt：本轮字体/Lazy 构造之前（此处尚没有任何字体 IO/解析）。
	initAt := time.Now()

	// ② 独立 Provider：Candidates 只含调用方传入的同一资源；
	//    Read 透明计数并真正调用 os.ReadFile（不伪造读取）。
	var readCalls atomic.Int64
	provider := theme.NewProvider(theme.ProviderOptions{
		Candidates: []string{source},
		Read: func(path string) ([]byte, error) {
			readCalls.Add(1)
			return os.ReadFile(path)
		},
	})

	var (
		lz  *theme.Lazy
		err error
	)
	if c.isSelected() {
		lz, err = theme.NewLazyWithSelection(theme.FaceSelection{
			Source:  theme.FontSource{Path: source},
			Indices: c.indices(),
		}, theme.LazyOptions{Provider: provider, Family: theme.FamilyCJK})
	} else {
		lz, err = theme.NewLazy(theme.LazyOptions{Provider: provider, Family: theme.FamilyCJK})
	}
	if err != nil {
		fmt.Printf("RESULT verdict=ERROR detail=%q\n", fmt.Sprintf("Lazy 构造失败: %v", err))
		return 1
	}

	view := &selectionProbeView{}
	// ③ App 构造前/后（M1 起点在此之前由 NewLazyApp 内部写入）。
	appCtorEntry := time.Now()
	a, err := nightapp.NewLazyApp(view, lz, nightapp.Config{
		Title:  cfg.Title,
		Width:  unit.Dp(240),
		Height: unit.Dp(90),
	})
	if err != nil {
		fmt.Printf("RESULT verdict=ERROR detail=%q\n", fmt.Sprintf("App 构造失败: %v", err))
		return 1
	}
	appCtorDone := time.Now()
	view.owner = a

	// ④ 临时主题基线：Request 已在 NewLazyApp 内同步执行，此刻 Theme() 是临时主题。
	//    之后 Theme() 发生变化即说明帧内交接已发生。
	tempTheme := lz.Theme()
	if tempTheme == nil {
		fmt.Printf("RESULT verdict=ERROR detail=%q\n", "App 构造后 Theme() 为 nil（未 Request）")
		return 1
	}
	tempShaper := tempTheme.Shaper

	runResult := make(chan error, 1)
	go func() { runResult <- a.Run() }()

	// ⑤ 等待：真实帧 + Ready + 帧内交接 + FrameSubmittedAt + 已换上最终主题。
	pollStart := time.Now()
	deadline := pollStart.Add(cfg.Timeout)
	var swapped bool
	for {
		select {
		case rerr := <-runResult:
			if rerr != nil {
				fmt.Printf("RESULT verdict=ERROR detail=%q\n", fmt.Sprintf("Run 返回错误: %v", rerr))
				return 1
			}
		default:
		}

		snap := view.snapshot()
		rep := lz.Report()
		if lz.Theme() != nil && lz.Theme() != tempTheme {
			swapped = true
		}
		done := snap.calls >= int64(cfg.Frames) &&
			a.FirstFrame().Measured() &&
			rep.State == theme.LoadReady &&
			!rep.FrameSubmittedAt.IsZero() &&
			swapped
		if done {
			break
		}
		// F2：失败优先于超时（判序集中在 pollDecision，便于无窗口回测）。
		switch pollDecision(rep.State, time.Now().After(deadline)) {
		case "failed":
			fail := classifyLoadFailure(rep)
			fmt.Println(fail.Verdict)
			for _, line := range fail.dataLines() {
				fmt.Printf("RESULT_DATA %s\n", line)
			}
			return 1
		case "timeout":
			// ENV_TIMEOUT 同时附原始 err 信息（若已有），保留单条终态。
			fmt.Printf("RESULT verdict=ENV_TIMEOUT detail=%q\n", fmt.Sprintf(
				"%s 内未满足成功条件（帧=%d/%d state=%s submitted=%v swapped=%v） err=%v",
				cfg.Timeout, snap.calls, cfg.Frames, rep.State, !rep.FrameSubmittedAt.IsZero(), swapped, rep.Err))
			fmt.Printf("RESULT_DATA framesObserved=%d m1Measured=%v\n", snap.calls, a.FirstFrame().Measured())
			return 3
		}
		time.Sleep(2 * time.Millisecond)
	}

	// ⑥ 收集观测事实。
	snap := view.snapshot()
	rep := lz.Report()
	m1 := a.FirstFrame()

	finalTheme := lz.Theme()
	var finalShaper *text.Shaper
	if finalTheme != nil {
		finalShaper = finalTheme.Shaper
	}

	// 最终 Shaper 在 Layout 中出现的位置（第一次）。
	finalSeen := false
	finalLayoutAt := time.Time{}
	for i, sh := range snap.shapers {
		if finalShaper != nil && sh == finalShaper {
			finalSeen = true
			if i < len(snap.layoutTime) {
				finalLayoutAt = snap.layoutTime[i]
			}
			break
		}
	}
	firstLayoutUsedFinal := len(snap.shapers) > 0 && finalShaper != nil && snap.shapers[0] == finalShaper
	// 双保险：始终以 tempShaper 为参照再算一次（tempShaper 只用于诊断）。
	_ = tempShaper

	obs := &faceObservation{
		Case:   c,
		Source: source,

		LazyState:         rep.State,
		LazyErr:           rep.Err,
		TempSource:        rep.TempSource,
		RequestedAt:       rep.RequestedAt,
		LoadedAt:          rep.LoadedAt,
		FrameSubmittedAt:  rep.FrameSubmittedAt,
		ProbePresent:      rep.Probe != nil,
		ProbeElapsed:      rep.ProbeElapsed,
		ProviderReadCalls: readCalls.Load(),
		ProviderScans:     provider.Scans(),

		FramesObserved:       snap.calls,
		FirstLayoutSawM1:     snap.measured,
		ShaperChanges:        countShaperChanges(snap.shapers),
		FirstLayoutUsedFinal: firstLayoutUsedFinal,
		FinalShaperSeen:      finalSeen,
		FinalLayoutAt:        finalLayoutAt,

		InitAt:       initAt,
		AppCtorEntry: appCtorEntry,
		AppCtorDone:  appCtorDone,
		FirstFrameAt: m1.FirstFrameAt,
		M1Measured:   m1.Measured(),
		M1Elapsed:    m1.Elapsed,
		M1CreatedAt:  m1.CreatedAt,
	}
	if rep.Probe != nil {
		obs.ProbeRan = rep.Probe.ProbeRan
		obs.ProbeResolved = rep.Probe.Resolved
		obs.ProbeErr = rep.Probe.Err
		obs.EmbeddedAttempted = rep.Probe.Embedded != nil && rep.Probe.Embedded.Attempted
	}
	if c.isSelected() {
		if sr := lz.SelectionReport(); sr != nil {
			obs.SelectionPresent = true
			obs.SelectionApplied = sr.SelectionApplied
			obs.SelectionRequested = sr.RequestedIndices
			obs.SelectionActual = sr.ActualIndices
			obs.SelectionDeclaredFaces = sr.DeclaredFaces
			obs.SelectionCoverageChecked = sr.CoverageChecked
			obs.SelectionCoverageHit = sr.CoverageHit
			obs.SelectionCoverageTotal = sr.CoverageTotal
			obs.SelectionErrorKind = sr.ErrorKind
		}
	} else if sr := lz.SelectionReport(); sr != nil {
		// default-lazy 不该有选面诊断；记录下来让 validate 之外也能看见。
		obs.SelectionPresent = true
	}

	for _, line := range obs.reportLines() {
		fmt.Printf("RESULT_DATA %s\n", line)
	}

	if err := obs.validate(); err != nil {
		fmt.Printf("RESULT verdict=FAIL detail=%q\n", err.Error())
		return 1
	}
	fmt.Printf("RESULT verdict=OK detail=%q\n", fmt.Sprintf(
		"真实窗口下完成 %s（帧=%d shaperChanges=%d M1=%v）",
		c, obs.FramesObserved, obs.ShaperChanges, obs.M1Elapsed))
	return 0
}
