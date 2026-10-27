package theme

// 显式字体面选择（FACE-SELECT）：只解析调用方明确指定的 TTC/TTF 面。
//
// # 定位
//
// 本文件提供**通用原语**：调用方给出显式字体源 + 面索引，框架只解析所选面，
// 并如实报告发生了什么。它**不**改变默认路径：不调用本 API 时，字体仍走
// `Provider` 探测 + 系统/嵌入回退 + TTC 全量面（既有语义零改动）。
//
// # 为什么是独立入口而不是 ProviderOptions 字段
//
// 采样证据（240 样本，协调者验收）显示收益来自「少解析未选面」：
// 单面 parseTotal 中位数 ≈ 50.4/51.2 ms，全解析 ≈ 102.6 ms。
// 但把 selection 放进 `ProviderOptions` 就等于把它注入**自动探测**这条策略，
// 并会牵扯既有集合身份语义；而 selection 是**调用方显式指定的资源**，
// 与"自动探测哪个系统字体"是两件事。因此本次选择做成独立 opt-in：
// selection 只在本文件内生效，不碰 `setID` / `Provider.sets` / `resolved`。
//
// （纠正旧说法：不是"技术上无法区分没配与配空"——用指针字段或显式标志都能区分；
// 把选择塞进自动探测的真实代价是策略耦合，不是类型能力不足。）
//
// # 明确不承诺
//
//   - 不承诺"选面后仍等价于全量"：未选面的 family/style/weight/别名与字形覆盖
//     确实消失，这部分由调用方显式承担；
//   - 不承诺系统回退不会掩盖缺失：最终主题仍保留系统字体（与 `newThemeWithFaces`
//     同口径），所以"渲染看起来正常"不等于"选中的面覆盖足够"；
//   - 不承诺零成本：读取/解析成本真实存在，只是**不再解析未选面**。
//
// # 与策略 §3.3（禁止 main/init 同步加载）的关系
//
// `LoadSelectedFaces` 是**同步库函数**，供受控场景（工具、测试、或调用方在 UI
// 启动前已接受该成本）使用；它不违反"库函数不得同步加载"的表述，但**生产接线**
// 必须走异步路径 `NewLazyWithSelection`（见 `lazy_facesel.go`），由既有后台
// worker 承担读取/解析，首帧用临时主题占位。

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	giofont "gioui.org/font"
	gioot "gioui.org/font/opentype"
	"gioui.org/widget/material"
	fontapi "github.com/go-text/typesetting/font"
	gottext "github.com/go-text/typesetting/font/opentype"
)

// 选择失败的错误类别。取值稳定，供调用方分支与诊断使用。
const (
	// SelectionInvalidConfig 配置本身非法（缺源、源字段冲突、索引非法等）。
	SelectionInvalidConfig = "invalid-config"
	// SelectionSourceIO 读取显式源的 IO 失败。
	SelectionSourceIO = "source-io"
	// SelectionParse 解析器返回了错误（fail-closed，无 panic）。
	SelectionParse = "parse"
	// SelectionParserPanic 第三方解析器 panic，已被本包局部边界收敛。
	// ⚠ 这只表示**适配层**把 panic 变成可控失败，不表示上游已修复。
	SelectionParserPanic = "parser-panic"
	// SelectionCoverage 所选面覆盖不足，且调用方要求强制覆盖。
	SelectionCoverage = "coverage-insufficient"
)

// 选择失败的阶段标签。粒度足以定位，但不假装比上游公开 API 更精确。
const (
	selPhaseSource = "source"
	selPhaseHeader = "header"
	selPhaseIndex  = "indices"
	selPhaseLoad   = "loaders-prep"
	selPhaseFont   = "new-font"
	selPhaseCover  = "coverage"
)

// selTTCTag 是 TrueType Collection 的 magic（与 go-text `reader.go` 一致）。
const selTTCTag = "ttcf"

// selMaxNumFonts 与 go-text 实现上限一致（安全限制）。
// 只用于读头阶段拒绝荒谬值，不复制其解析逻辑。
const selMaxNumFonts = 2048

// selDfontOffset 是 dfont 的特征值。本 API 不支持 dfont（多面偏移在资源叉里）。
const selDfontOffset = 0x00000100

// FontSource 是显式字体源：`Bytes` 与 `Path` **互斥**。
//
// 三条来源校验规则（违反一律 `invalid-config`，不做任何猜测性回退）：
//   - 两者都设置 ⇒ 冲突；
//   - 两者都未设置（`Bytes == nil && Path == ""`）⇒ 无源；
//   - `Bytes` 源必须给出 `Name`（诊断与 label 用）；`Path` 源缺省取 basename。
//
// ⚠ **已给空 `Bytes` 不会被当成"未给"去改读 `Path`**：`Bytes != nil` 但长度 0
// 视为明确的空源配置错误，避免"我以为在传内存字体，实际读到了磁盘文件"。
type FontSource struct {
	// Bytes 内存字体字节；与 Path 互斥。加载时会做一次独立快照，
	// 因此 API 返回后调用方修改自己的切片不会影响已加载结果。
	Bytes []byte
	// Path 字体文件路径；与 Bytes 互斥。经 filepath.Clean 后用于诊断。
	Path string
	// Name 诊断名称。Bytes 源必填；Path 源留空则取 basename。
	Name string
}

// FaceSelection 是"源 + 面索引"的绑定，避免出现与源脱钩的全局裸索引列表。
type FaceSelection struct {
	Source FontSource
	// Indices 非空、无重复、非负；顺序即结果集合顺序（顺序会影响回退优先级）。
	// 越界只能在读到集合头之后判定，故该错误发生在加载阶段。
	Indices []int
	// RequireCoverage 为 true 时，所选面须达到既有 `CoverageSufficient` 线，
	// 否则以 `coverage-insufficient` 拒绝。默认 false：只记录覆盖事实，不拒绝。
	RequireCoverage bool
}

// FaceMeta 是单个面的元数据（来自字体自身 Describe）。
type FaceMeta struct {
	Index    int
	Typeface string
	Style    int
	Weight   int
}

// SelectionReport 是一次显式选面的**独立**诊断。
//
// 刻意不复用也不扩展 `LoadReport` / `ProbeReport`：那些类型已冻结且语义属于
// Provider 探测链。本报告只描述"这次显式选择发生了什么"。
type SelectionReport struct {
	// Label 仅作诊断标签，**非唯一身份**：不含内容哈希，同 Name 同 Indices 即同 Label。
	Label string
	// SourceName 是 FontSource.Name（或 Path 的 basename）。
	SourceName string
	// DeclaredFaces 是集合头**声明**的面数（读头失败时为 0）。
	DeclaredFaces int
	// RequestedIndices 是调用方请求的索引（保序原样）。
	RequestedIndices []int
	// ActualIndices 是**实际**解析出的面的索引，保序。
	// 只有成功时才有意义；失败时为空，不得写成"已应用"。
	ActualIndices []int
	// Meta 是所选面的元数据，逐位对应 ActualIndices。
	Meta []FaceMeta
	// CoverageChecked 区分"未检查"与"已检查但零命中"。
	//
	// 为 false 表示**根本没走到覆盖阶段**（读头/索引/解析先失败了），
	// 因此不能把 CoverageHit==0 当成"覆盖为零"的结论。
	CoverageChecked bool
	// CoverageHit / CoverageTotal 是对**所选面自身**的 CJK 抽样覆盖事实。
	// 这 20/20 只是 CJK 抽样，不是通用覆盖证明。
	CoverageHit   int
	CoverageTotal int
	// CoverageMissing 是缺失码位（深拷贝保存）。
	CoverageMissing []rune
	// SelectionApplied 仅在成功解析出所选面后为 true。
	SelectionApplied bool
	// ErrorKind 为空表示无错误；否则为 SelectionXxx 常量之一。
	ErrorKind string
	// ErrorPhase 是失败阶段标签。
	ErrorPhase string
	// FaceIndexKnown 表示 FaceIndex 是否可确定。
	// 逐面解析可以确定；集合内循环解析（无法归因）则为 false。
	FaceIndexKnown bool
	// FaceIndex 在 FaceIndexKnown 为 true 时有效。
	FaceIndex int
}

// clone 深拷贝报告（切片逐层复制），用于对外发布。
func (r SelectionReport) clone() SelectionReport {
	cp := r
	cp.RequestedIndices = append([]int(nil), r.RequestedIndices...)
	cp.ActualIndices = append([]int(nil), r.ActualIndices...)
	cp.CoverageMissing = append([]rune(nil), r.CoverageMissing...)
	if r.Meta != nil {
		cp.Meta = append([]FaceMeta(nil), r.Meta...)
	}
	return cp
}

// SelectionError 是显式选面失败的**结构化**错误。
//
// 调用方可用 `errors.As(err, &se)` 取回失败报告（含 ErrorKind/Phase/FaceIndex
// 与已测到的事实），并用 `errors.Is/As` 沿 `Unwrap()` 检查底层错误。
type SelectionError struct {
	// Report 是失败时的诊断快照（永不包含"已应用"的成功标记）。
	Report SelectionReport
	// Err 是底层错误（IO / 解析器原文 / 收敛后的 panic 描述）。
	Err error
}

func (e *SelectionError) Error() string {
	if e == nil {
		return "theme: nil SelectionError"
	}
	msg := "theme: 显式选面失败"
	if e.Report.ErrorKind != "" {
		msg += " [" + e.Report.ErrorKind + "]"
	}
	if e.Report.ErrorPhase != "" {
		msg += " at " + e.Report.ErrorPhase
	}
	if e.Report.FaceIndexKnown {
		msg += fmt.Sprintf(" (face %d)", e.Report.FaceIndex)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap 暴露底层错误，供 errors.Is/As 使用。
func (e *SelectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// clone 深拷贝本层自有的 SelectionError（含嵌套自有 Error）。
//
// 用途：在**报告读取边界**为每个调用方返回独立对象，避免多个 Report()
// 共享同一个可变指针。外部不透明原因错误（IO 错误等）按 Go 只读惯例**不**深拷贝，
// 只有本层自己 new 出来的 SelectionError 需要隔离。
func (e *SelectionError) clone() *SelectionError {
	if e == nil {
		return nil
	}
	cp := &SelectionError{Report: e.Report.clone()}
	if inner, ok := e.Err.(*SelectionError); ok {
		cp.Err = inner.clone()
	} else {
		cp.Err = e.Err
	}
	return cp
}

// selPanicError 是**只在真正 recover 分支**构造的私有错误类型。
//
// 它存在的唯一目的：把"第三方解析器 panic 被本层收敛"与"解析器正常返回错误"
// 在**类型层**区分开，而不是靠错误文本猜测。
//
// 不导出：调用方通过公开的 SelectionReport.ErrorKind（parser-panic / parse）
// 观察分类，不需要也不应该依赖本类型。
type selPanicError struct {
	phase     string
	faceIdx   int
	faceKnown bool
	reason    any
}

func (e *selPanicError) Error() string {
	if e == nil {
		return "theme: nil selPanicError"
	}
	if e.faceKnown {
		return fmt.Sprintf("面 %d 解析 panic（已收敛，上游未修复）: %v", e.faceIdx, e.reason)
	}
	return fmt.Sprintf("Loaders 构造 panic（已收敛，上游未修复）: %v", e.reason)
}

// selIsPanicErr 报告 err 链中是否含本层构造的 selPanicError。
//
// 用于分类：命中 ⇒ parser-panic；未命中 ⇒ parse（普通解析错误原样归类，
// **不再用错误文本是否包含 "panic" 猜测**）。
func selIsPanicErr(err error) (*selPanicError, bool) {
	var pe *selPanicError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}

// selClassifyParseErr 把"解析阶段"的错误归为两类之一。
//
// 规则只有一条：错误链里含本层构造的 selPanicError ⇒ parser-panic；
// 否则一律 parse。**不看错误文本**——普通错误即使内容出现 "panic" 也不会被改变类别。
func selClassifyParseErr(err error) string {
	if _, ok := selIsPanicErr(err); ok {
		return SelectionParserPanic
	}
	return SelectionParse
}

// newSelectionError 构造结构化错误；report 会被深拷贝保存。
func newSelectionError(kind, phase string, rep SelectionReport, err error) *SelectionError {
	rep.ErrorKind = kind
	rep.ErrorPhase = phase
	rep.SelectionApplied = false
	rep.ActualIndices = nil
	rep.Meta = nil
	return &SelectionError{Report: rep.clone(), Err: err}
}

// SelectedFaces 是一次显式选面的不透明结果。
//
// 不可变：不导出字段，`Faces()`/`Report()` 都返回副本。
// **不持有源字节**：解析完成后仅保留已解析的字体对象；依赖的 `RawTable`
// 在 `dst == nil` 时总是新分配缓冲并把表字节读入其中，因此字体对象不别名
// 调用方的输入切片（详见 ownership.md）。
type SelectedFaces struct {
	faces  []giofont.FontFace
	report SelectionReport
}

// Faces 返回所选面的**外层切片副本**。
//
// 逐元素是值拷贝（`giofont.FontFace` 为值类型）；`Face()` 每次调用都会
// 产生独立可变 Face，因此调用方之间不会共享可变状态。
func (s *SelectedFaces) Faces() []giofont.FontFace {
	if s == nil || len(s.faces) == 0 {
		return nil
	}
	return append([]giofont.FontFace(nil), s.faces...)
}

// Report 返回诊断的深拷贝。
func (s *SelectedFaces) Report() SelectionReport {
	if s == nil {
		return SelectionReport{}
	}
	return s.report.clone()
}

// Theme 用所选集合构造一个**独立**主题（复用 `newThemeWithFaces` 的系统回退口径）。
//
// # nil 与"零结果"的边界（只把语义写清，不改变行为）
//
//   - 只有**成功加载**得到的 `SelectedFaces` 调用 `Theme()` 才代表"这次选择"；
//   - 对 nil 接收者调用返回 Gio 默认主题（`newThemeWithFaces(nil)` 的既有行为），
//     那是**兜底默认**，**不代表**任何选择成功，也不得被引用为"选面生效"；
//   - 失败路径不产出 `SelectedFaces`（失败恒 nil + error），因此不存在
//     "用失败结果的主题假装成功"的路径。
//
// 线程与成本：内部会 `text.NewShaper`，采样中 Shaper 构建中位数约 31.6–32.8 ms。
// 每次调用都构造新主题，**不缓存、不共享** mutable Theme/Shaper；
// 返回值归调用方独占（不得跨渲染线程共享同一个 Shaper）。
func (s *SelectedFaces) Theme() *material.Theme {
	if s == nil {
		return newThemeWithFaces(nil)
	}
	return newThemeWithFaces(s.faces)
}

// ---------------------------------------------------------------------------
// 配置冻结与校验（纯配置，不读源）
// ---------------------------------------------------------------------------

// selectionConfig 是冻结后的内部配置。构造期只做纯配置校验，**不读源**。
type selectionConfig struct {
	label           string
	name            string
	path            string
	inlineBytes     []byte // Bytes 源：构造期快照，保证异步所有权
	hasInlineBytes  bool   // 区分"给了空 Bytes"与"未给 Bytes"
	indices         []int
	requireCoverage bool

	// readFileFn 是 Path 源的读取函数；nil 表示 os.ReadFile。
	//
	// 存在的唯一理由是**可判别性测试**：需要在测试里阻塞/计数读取，
	// 以确定性证明"构造不读源"与"Request 不阻塞"。它是**私有字段**，
	// 不进入任何公开 API，也不影响默认行为。
	readFileFn func(string) ([]byte, error)
}

// freezeSelection 校验纯配置并冻结副本。返回的配置可安全跨 goroutine 使用。
//
// **不做**任何 IO 或解析：越界、损坏、格式不支持等只能读源才知道的错误
// 一律延后到加载阶段，避免构造器"偷读"字体。
func freezeSelection(sel FaceSelection) (*selectionConfig, error) {
	rep := SelectionReport{RequestedIndices: append([]int(nil), sel.Indices...)}

	src := sel.Source
	hasBytes := src.Bytes != nil
	hasPath := strings.TrimSpace(src.Path) != ""

	switch {
	case hasBytes && hasPath:
		return nil, newSelectionError(SelectionInvalidConfig, selPhaseSource, rep,
			errors.New("FontSource 的 Bytes 与 Path 互斥，不能同时设置"))
	case hasBytes && len(src.Bytes) == 0:
		// 显式给了空 Bytes：视为错误的空源，**绝不**回退去读 Path。
		return nil, newSelectionError(SelectionInvalidConfig, selPhaseSource, rep,
			errors.New("FontSource.Bytes 已设置但长度为 0：空源非法（不会改读 Path）"))
	case !hasBytes && !hasPath:
		return nil, newSelectionError(SelectionInvalidConfig, selPhaseSource, rep,
			errors.New("FontSource 未提供源：Bytes 与 Path 至少需设置一个"))
	}

	name := strings.TrimSpace(src.Name)
	if hasBytes {
		if name == "" {
			return nil, newSelectionError(SelectionInvalidConfig, selPhaseSource, rep,
				errors.New("Bytes 源必须提供 FontSource.Name（用于诊断与 label）"))
		}
	} else if name == "" {
		name = filepath.Base(filepath.Clean(src.Path))
	}

	if len(sel.Indices) == 0 {
		return nil, newSelectionError(SelectionInvalidConfig, selPhaseIndex, rep,
			errors.New("Indices 为空：显式选面必须至少指定一个面（不隐式等价于全量）"))
	}
	seen := make(map[int]struct{}, len(sel.Indices))
	indices := make([]int, 0, len(sel.Indices))
	for _, idx := range sel.Indices {
		if idx < 0 {
			return nil, newSelectionError(SelectionInvalidConfig, selPhaseIndex, rep,
				fmt.Errorf("面索引 %d 为负数", idx))
		}
		if _, dup := seen[idx]; dup {
			return nil, newSelectionError(SelectionInvalidConfig, selPhaseIndex, rep,
				fmt.Errorf("面索引 %d 重复（每个面最多选一次）", idx))
		}
		seen[idx] = struct{}{}
		indices = append(indices, idx)
	}

	cfg := &selectionConfig{
		name:            name,
		path:            filepath.Clean(src.Path),
		label:           "selected:" + name + "#faces=" + selJoinInts(indices),
		indices:         indices,
		requireCoverage: sel.RequireCoverage,
	}
	if hasBytes {
		// 构造期快照：保证 worker 异步读取时调用方已无权改动这批字节。
		// 这是**纯内存成本**（≈ 源大小），不计入"零成本"。
		cfg.inlineBytes = append([]byte(nil), src.Bytes...)
		cfg.hasInlineBytes = true
	}
	return cfg, nil
}

func selJoinInts(vs []int) string {
	var b strings.Builder
	for i, v := range vs {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%d", v)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// 加载核心（同步 loader 与 Lazy worker 共用）
// ---------------------------------------------------------------------------

// selCollectionInfo 是只读集合头得到的事实。
type selCollectionInfo struct {
	isCollection bool
	numFonts     int
}

// readSelHeader 只解析头部：magic、面数、偏移合法性。不解析任何表。
func readSelHeader(data []byte) (selCollectionInfo, error) {
	if len(data) < 12 {
		return selCollectionInfo{}, fmt.Errorf("字体文件过短（%d 字节），不足以容纳头部", len(data))
	}
	if string(data[0:4]) == selTTCTag {
		num := binary.BigEndian.Uint32(data[8:12])
		if num == 0 {
			return selCollectionInfo{}, errors.New("空字体集合：TTC 头声明 numFonts=0")
		}
		if num > selMaxNumFonts {
			return selCollectionInfo{}, fmt.Errorf("集合面数 %d 超过实现上限 %d", num, selMaxNumFonts)
		}
		need := 12 + 4*int(num)
		if len(data) < need {
			return selCollectionInfo{}, fmt.Errorf("TTC 头被截断：声明 %d 面需 %d 字节偏移表，实际 %d", num, need, len(data))
		}
		for i := 0; i < int(num); i++ {
			off := binary.BigEndian.Uint32(data[12+4*i : 16+4*i])
			if int64(off) >= int64(len(data)) {
				return selCollectionInfo{}, fmt.Errorf("TTC 面 %d 的偏移 %d 超出文件长度 %d", i, off, len(data))
			}
		}
		return selCollectionInfo{isCollection: true, numFonts: int(num)}, nil
	}
	if binary.BigEndian.Uint32(data[0:4]) == selDfontOffset {
		return selCollectionInfo{}, errors.New("dfont 资源字体不受支持（多面偏移在资源叉内）")
	}
	switch string(data[0:4]) {
	case "\x00\x01\x00\x00", "OTTO", "true", "typ1":
		return selCollectionInfo{isCollection: false, numFonts: 1}, nil
	default:
		return selCollectionInfo{}, fmt.Errorf("不支持或损坏的字体格式：magic=%q", data[0:4])
	}
}

// selFaceWrapper 把不可变 `*fontapi.Font` 包装成 gio 的 `font.Face`。
//
// 每次 `Face()` 都返回一个**独立的可变 Face**（自带字形/度量缓存），
// 因此不同 Shaper 不共享可变状态；不可变 Font 本身可安全复用。
type selFaceWrapper struct {
	font *fontapi.Font
}

// Face 返回该字体的一次性可变包装。
func (w selFaceWrapper) Face() *fontapi.Face { return fontapi.NewFace(w.font) }

// newSelLoaders 把字节解析成逐面 Loader。
//
// 独立函数是为了让 panic 收敛点集中：`NewLoaders` 会读取**每个面**的头部，
// 若在此处 panic 则无法归因到具体面（与 ParseCollection 同样的限制），
// 故阶段记作 loaders-prep、不假称面索引可确定。
func newSelLoaders(data []byte) (loaders []*gottext.Loader, err error) {
	defer func() {
		if r := recover(); r != nil {
			loaders = nil
			// 只有真正 recover 才构造私有 panic 类型；普通错误原样返回。
			err = &selPanicError{phase: selPhaseLoad, reason: r}
		}
	}()
	return gottext.NewLoaders(bytes.NewReader(data))
}

// selSafeNewFont 在**单面调用边界**收敛第三方解析 panic，并记录可确定的面索引。
//
// 已复现的上游行为：损坏的 `loca` 会让 `tables.ParseGlyf` 越界切片 panic。
// 这里只做适配层收敛：保留原始 reason、给出阶段与索引，不重写解析器。
func selSafeNewFont(ld *gottext.Loader, idx int) (f *fontapi.Font, err error) {
	defer func() {
		if r := recover(); r != nil {
			f = nil
			// 只有真正 recover 才构造私有 panic 类型；普通错误原样返回。
			err = &selPanicError{phase: selPhaseFont, faceIdx: idx, faceKnown: true, reason: r}
		}
	}()
	return fontapi.NewFont(ld)
}

// loadSelectionBytes 是加载核心：读头 → 校验索引 → 取 loaders → 逐面解析 → 覆盖检查。
//
// 入参 cfg 已冻结；data 是本次加载使用的字节快照。
// 成功返回 (faces, report, nil)；失败返回 (nil, report, *SelectionError)，
// 其中 report 与 SelectionError.Report 内容一致，且**不含**成功标记。
func loadSelectionBytes(cfg *selectionConfig, data []byte) ([]giofont.FontFace, SelectionReport, error) {
	rep := SelectionReport{
		Label:            cfg.label,
		SourceName:       cfg.name,
		RequestedIndices: append([]int(nil), cfg.indices...),
	}

	info, err := readSelHeader(data)
	if err != nil {
		se := newSelectionError(SelectionParse, selPhaseHeader, rep, err)
		return nil, se.Report, se
	}
	rep.DeclaredFaces = info.numFonts

	// 越界只能在读到面数之后判定。
	for _, idx := range cfg.indices {
		if idx >= info.numFonts {
			e := fmt.Errorf("面索引 %d 越界：该文件共 %d 个面（合法范围 0..%d）",
				idx, info.numFonts, info.numFonts-1)
			se := newSelectionError(SelectionInvalidConfig, selPhaseIndex, rep, e)
			return nil, se.Report, se
		}
	}

	loaders, lerr := newSelLoaders(data)
	if lerr != nil {
		// 类型化分类：只有真正 recover 出来的 selPanicError 才是 parser-panic。
		kind := selClassifyParseErr(lerr)
		phase := selPhaseLoad
		if pe, ok := selIsPanicErr(lerr); ok && pe.phase != "" {
			phase = pe.phase
		}
		se := newSelectionError(kind, phase, rep, lerr)
		return nil, se.Report, se
	}
	if len(loaders) != info.numFonts {
		e := fmt.Errorf("面数不一致：头部声明 %d，Loaders 实际 %d", info.numFonts, len(loaders))
		se := newSelectionError(SelectionParse, selPhaseLoad, rep, e)
		return nil, se.Report, se
	}

	out := make([]giofont.FontFace, 0, len(cfg.indices))
	meta := make([]FaceMeta, 0, len(cfg.indices))
	actual := make([]int, 0, len(cfg.indices))
	for _, idx := range cfg.indices {
		f, ferr := selSafeNewFont(loaders[idx], idx)
		if ferr != nil {
			// 失败：零结果，不返回半成品。面索引可确定（逐面解析）。
			rep.FaceIndexKnown = true
			rep.FaceIndex = idx
			// 类型化分类：recover 出来的 panic ⇒ parser-panic；
			// 解析器正常返回的错误（缺表、表损坏等）⇒ parse。
			kind := selClassifyParseErr(ferr)
			se := newSelectionError(kind, selPhaseFont, rep, ferr)
			return nil, se.Report, se
		}
		md := gioot.DescriptionToFont(f.Describe())
		out = append(out, giofont.FontFace{Face: selFaceWrapper{font: f}, Font: md})
		meta = append(meta, FaceMeta{
			Index: idx, Typeface: string(md.Typeface),
			Style: int(md.Style), Weight: int(md.Weight),
		})
		actual = append(actual, idx)
	}

	// 覆盖检查只针对**所选面自身**：不用系统回退掩盖，也不补回未选面。
	// 复用既有 Coverage / RequiredRunes / CoverageSufficient，不复制 0.9 常量。
	required := RequiredRunes()
	hit, missing := Coverage(out, required)
	rep.CoverageChecked = true
	rep.CoverageHit = hit
	rep.CoverageTotal = len(required)
	rep.CoverageMissing = append([]rune(nil), missing...)

	if cfg.requireCoverage && !CoverageSufficient(hit, len(required)) {
		e := fmt.Errorf("所选面覆盖不足：%d/%d（缺 %q）", hit, len(required), string(missing))
		se := newSelectionError(SelectionCoverage, selPhaseCover, rep, e)
		return nil, se.Report, se
	}

	rep.ActualIndices = actual
	rep.Meta = meta
	rep.SelectionApplied = true
	return out, rep, nil
}

// readSelectionSource 取得本次加载使用的字节快照。
//
// Bytes 源使用构造期已冻结的快照；Path 源在**每次加载尝试时重新读入**
// ——不监听文件变化，也不跨尝试缓存字节：显式重试会用当时磁盘上的新内容。
func readSelectionSource(cfg *selectionConfig) ([]byte, SelectionReport, error) {
	base := SelectionReport{
		Label: cfg.label, SourceName: cfg.name,
		RequestedIndices: append([]int(nil), cfg.indices...),
	}
	if cfg.hasInlineBytes {
		return cfg.inlineBytes, base, nil
	}
	read := cfg.readFileFn
	if read == nil {
		read = os.ReadFile
	}
	data, err := read(cfg.path)
	if err != nil {
		return nil, base, newSelectionError(SelectionSourceIO, selPhaseSource, base,
			fmt.Errorf("读取字体 %q 失败: %w", cfg.path, err))
	}
	return data, base, nil
}

// ---------------------------------------------------------------------------
// 同步加载入口
// ---------------------------------------------------------------------------

// LoadSelectedFaces 同步读取显式源并只解析所选面。
//
// 成本与线程：本函数会**同步读文件或复制字节并解析**，耗时随所选面数量增长
// （采样中单面解析中位数约 50 ms，本机 msyh.ttc）。因此：
//   - 不要在渲染 Layout 内调用；
//   - 生产异步路径请用 `NewLazyWithSelection`，把该成本放进既有后台 worker；
//   - 不要在 `NewApp` 之前"前置"调用后再声称启动更快——那只是把成本移出了
//     M1 起点，属禁手。
func LoadSelectedFaces(sel FaceSelection) (*SelectedFaces, error) {
	cfg, err := freezeSelection(sel)
	if err != nil {
		return nil, err
	}
	data, _, err := readSelectionSource(cfg)
	if err != nil {
		return nil, err
	}
	faces, rep, lerr := loadSelectionBytes(cfg, data)
	if lerr != nil {
		return nil, lerr
	}
	return &SelectedFaces{faces: faces, report: rep}, nil
}
