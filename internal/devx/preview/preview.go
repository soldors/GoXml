// Package preview 提供独立的静态 GXML 预览核心（TASK-DEVX-002）。
//
// 定位：把一个 .gxml 编译成「可接入专用预览宿主」的生成 View，
// 用于快速查看布局与主题效果。它**不是**：
//   - 用户业务应用的启动器（不 import 用户业务包、不运行目标 main/init）；
//   - Watch（不做热重载，不依赖 internal/devx/watch）。
//
// 复用边界（本包不造第二套校验器）：
//   - 解析/宏展开/类型校验/资产门禁/代码生成全部复用
//     internal/compiler/pipeline 的既有链路（Parser→Expander→Analyzer→
//     Codegen→资产门禁）；
//   - 「无 ViewModel 纯静态页」之外的范围拒绝由本包的 scope 门禁补充
//     （PREVIEW-* 诊断），不改现行语言准入——现行分析器已经拒绝的输入
//     （无 VM 的绑定/事件等）仍按标准 GXML-* 诊断原样透传。
//
// 输入边界（首版）：
//   - 单个 .gxml 文件，必须位于某个 Go module 内（前端需要读 go.mod）；
//   - 无 ViewModel、无绑定、无事件、无 $Theme 引用；
//   - 布局/Def/Slot/Image/Expanded 按现行批准范围支持。
//
// CLI adapter 建议（最小接线，见交付 integration-proposal.diff）：
//
//	goxml preview <file.gxml>
//	  → resolve module root（复用 cli.findModuleRoot 语义）
//	  → 测量有效 GOFLAGS（复用 cli.measureContextValues）
//	  → 解析用户 overlay（复用 cli.userOverlayMap，结果传 Options.AssetOverlay）
//	  → preview.Run(Options{...})
//	  → res.Phase == PhaseBuilt：启动宿主；诊断则打印后以非零码退出。
package preview

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/parser"
	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// Phase 标记一次预览运行**停在哪一个阶段**。
//
// 成功时是终点阶段（PhaseGenerated / PhaseBuilt）；失败时是首个失败阶段。
// 诊断（Diags）与阶段一一对应：调用方据此区分「输入被拒绝」与「环境故障」。
type Phase string

const (
	// PhaseResolve 输入定位：文件存在、后缀、module root。
	PhaseResolve Phase = "resolve"
	// PhaseFrontend 标准编译前端（parse→expand→资产门禁→analyze）。
	// 停在这里表示输入本身不合法（标准 GXML-*/CODEGEN-* 诊断）。
	PhaseFrontend Phase = "frontend"
	// PhaseScope 预览静态范围门禁：PREVIEW-* 拒绝。
	PhaseScope Phase = "scope"
	// PhaseGenerate Codegen 失败（理论上静态页不应走到这里的失败）。
	PhaseGenerate Phase = "generate"
	// PhaseGenerated 生成成功（内存产物就绪）。
	PhaseGenerated Phase = "generated"
	// PhaseHost 隔离宿主目录布置失败。
	PhaseHost Phase = "host"
	// PhaseBuild 宿主 go build 失败（证据保留在宿主目录）。
	PhaseBuild Phase = "build"
	// PhaseBuilt 宿主 go build 成功（本线终点：只 build 不运行原生窗口）。
	PhaseBuilt Phase = "built"
)

// Options 是预览运行的外部输入。
type Options struct {
	// Context cancels CLI preparation/build; nil retains the original background behavior.
	Context context.Context
	// File 是待预览的 .gxml 路径（相对 WorkDir 或绝对）。
	File string
	// WorkDir 是解析 File 的基准目录；空表示当前工作目录。
	WorkDir string
	// PackageName 是生成 View 的 package 子句；空则用 "preview"。
	//
	// 预览产物落在隔离宿主目录里（见 host 阶段），不进入用户源码树，
	// 因此包名由预览指定而不是从目标目录解析。
	PackageName string
	// AssetOverlay 是用户 Go -overlay 的归一化结果
	// （绝对路径 → 替换目标绝对路径，"" 表示删除）。
	//
	// 与现行 generate 管线同源：由接线层用 cli.userOverlayMap 从有效
	// GOFLAGS 解析后传入。本包**不复刻** GOFLAGS/overlay JSON 解析，
	// 避免出现第二份 overlay 语义。
	AssetOverlay map[string]string
	// AssetOutputPaths 是「构建优先级高于用户 overlay 的输出路径集合」
	// （本批生成产物 + Manifest 已登记 owned 产物，绝对路径）。
	//
	// 与现行 generate 管线同源：由接线层按 Manifest+保护头+Source 证明
	// 计算后传入；Src 解析到其中时资产门禁 fail-closed（防绕过 owned
	// 输出冲突守卫）。nil ⇒ 无。
	AssetOutputPaths map[string]bool
	// ToolRoot 是 goxml 源码根（宿主 replace 指向它，保证 runtime 与
	// 生成 View 同源）。空则从本包源码位置自动探测；探测失败 fail-closed
	// （-trimpath 二进制需显式指定）。
	ToolRoot string
	// SkipHost 为 true 时停在 PhaseGenerated（只生成不布置宿主；
	// 供核心单测与「只看产物」场景使用）。
	SkipHost bool
	// KeepHostDir 为 true 时成功后也保留宿主目录（默认由调用方经
	// Result.Cleanup 清理；失败总是保留证据）。
	KeepHostDir bool
	// BuildTimeout 是宿主 go build 的时限；零值默认 300s。
	BuildTimeout time.Duration

	// Stdout / Stderr 是进度输出；nil 表示丢弃。
	Stdout, Stderr io.Writer
}

// Result 是一次预览运行的完整结果。
type Result struct {
	// Phase 是停住的阶段（成功为终点阶段）。
	Phase Phase
	// Diags 是诊断（标准管线诊断原样透传 + PREVIEW-* 范围拒绝）。
	Diags []contracts.CompilerError

	// File 是解析后的输入文件绝对路径（PhaseResolve 之后有效）。
	File string
	// ModuleRoot 是输入所属 Go module 根。
	ModuleRoot string

	// 以下字段在对应阶段成功后填充。
	Source     []byte // 生成 View 源码（PhaseGenerated 起）
	OutputName string // 生成文件名（如 static_basic_gen.go）
	PkgName    string // 生成文件 package 子句

	// HostDir / HostExe 在宿主阶段成功后填充；失败时 HostDir 也可能
	// 被填充（保留证据），KeptForEvidence 说明原因。
	HostDir         string
	HostExe         string
	KeptForEvidence bool

	// 未导出：宿主布置/资产复制所需的中间状态。
	assets    []codegen.AssetRef // 门禁通过的唯一静态资产引用
	hostToken string             // 宿主目录所有权 token
}

// OK 报告本次运行是否产出了生成 View（预览核心判据；宿主阶段另见 Built）。
func (r *Result) OK() bool {
	return r != nil && (r.Phase == PhaseGenerated || r.Phase == PhaseBuilt)
}

// Built 报告宿主是否已构建成功（本线终点：只 build 不运行原生窗口）。
func (r *Result) Built() bool { return r != nil && r.Phase == PhaseBuilt }

// Run 执行预览核心：resolve → frontend → scope → generate。
//
// 返回值语义（调用方必须区分，不许混用）：
//   - err != nil：**环境/操作故障**（文件读不到、不在 module 内等），
//     Result.Phase 停在 PhaseResolve；这类问题重跑同样输入不会自愈；
//   - err == nil 且 !res.OK()：**输入被拒绝**，诊断在 res.Diags，
//     Phase 指明被拒的阶段（frontend = 标准入场拒绝；scope = 预览范围拒绝）；
//   - err == nil 且 res.OK()：res.Source 是生成 View 源码。
func Run(opts Options) (*Result, error) {
	r := &Result{Phase: PhaseResolve}
	if opts.Context != nil {
		if err := opts.Context.Err(); err != nil {
			return r, err
		}
	}

	// ---- 阶段 1：resolve ----
	file, src, moduleRoot, err := resolveInput(opts)
	if err != nil {
		return r, err
	}
	r.File = file
	r.ModuleRoot = moduleRoot
	sourceDir := filepath.Dir(file)
	base := filepath.Base(file)
	pkgName := opts.PackageName
	if pkgName == "" {
		pkgName = "preview"
	}

	// ---- 阶段 2a：语法 + ViewModel 早拒绝 ----
	//
	// 在进入完整前端之前先用**同一个 Parser** 检查 ViewModel：
	// 带 VM 的页面若走到 Analyzer 会去加载用户 VM 包（go/packages 子进程），
	// 而预览注定要拒绝它——早拒绝避免为一次注定失败的加载付出代价，
	// 也让诊断稳定指向「预览范围」而不是用户包的加载细节。
	doc, pdiags := parser.Parse(opts.File, src)
	if len(pdiags) > 0 {
		r.Phase = PhaseFrontend
		r.Diags = append(r.Diags, pdiags...)
		return r, nil
	}
	if !doc.ViewModel.IsZero() {
		r.Phase = PhaseScope
		r.Diags = append(r.Diags, contracts.NewError(opts.File, doc.ViewModel.Span,
			CodePreviewVM,
			"page declares ViewModel %q; static preview supports ViewModel-free pages only (first-version scope)",
			doc.ViewModel.Value))
		return r, nil
	}

	// ---- 阶段 2b：标准编译前端 + 资产门禁 ----
	//
	// TargetDir = SourceDir：与「生成到 .gxml 同目录」的现行默认一致，
	// <Image Src> 按源目录解析；overlay 与 owned 输出优先集合都按现行
	// generate 管线的口径原样传入 pipeline.Config（不另造资产/所有权语义）。
	// AssetOutputPaths 让门禁在写宿主之前就对本批/已登记 owned 产物
	// fail-closed：用户 overlay 把这些路径替换成合法 PNG 也不能放行。
	cfg := pipeline.Config{
		ModuleRoot:       moduleRoot,
		SourceDir:        sourceDir,
		TargetDir:        sourceDir,
		OutputDir:        sourceDir,
		PackageName:      pkgName,
		SourcePath:       base,
		AssetOverlay:     opts.AssetOverlay,
		AssetOutputPaths: opts.AssetOutputPaths,
	}
	prep := pipeline.Prepare(opts.File, src, cfg, pipeline.Hooks{})
	if prep.Doc == nil {
		r.Phase = PhaseFrontend
		r.Diags = append(r.Diags, prep.Diags...)
		return r, nil
	}

	// ---- 阶段 3：预览静态范围门禁 ----
	if rejections := checkStaticScope(opts.File, prep.Doc.Document()); len(rejections) > 0 {
		r.Phase = PhaseScope
		r.Diags = append(r.Diags, rejections...)
		return r, nil
	}

	// ---- 阶段 4：Codegen ----
	gen := pipeline.RunPrepared(prep, pipeline.Hooks{})
	r.Diags = append(r.Diags, gen.Diags...)
	if !gen.OK() {
		r.Phase = PhaseGenerate
		return r, nil
	}
	r.Phase = PhaseGenerated
	r.Source = gen.Artifact.Source
	r.OutputName = gen.Artifact.OutputName
	r.PkgName = gen.Artifact.PackageName
	r.assets = codegen.AssetRefs(prep.Doc)

	if opts.Context != nil {
		if err := opts.Context.Err(); err != nil {
			return r, err
		}
	}

	// ---- 阶段 5：隔离宿主布置（可选，SkipHost 时停在生成）----
	if opts.SkipHost {
		return r, nil
	}
	toolRoot, err := resolveToolRoot(opts.ToolRoot)
	if err != nil {
		r.Phase = PhaseHost
		return r, err
	}
	host, err := arrangeHost(r, toolRoot, opts)
	if err != nil {
		r.Phase = PhaseHost
		if r.HostDir != "" {
			// arrangeHost 在目录取得所有权标记的第一时间就把
			// HostDir/token 带回 Result：失败也必须保留证据与清理依据。
			r.KeptForEvidence = true
			return r, fmt.Errorf("preview: 宿主布置失败（证据保留在 %s）: %w", r.HostDir, err)
		}
		return r, err
	}
	r.HostDir = host.Dir
	r.hostToken = host.Token

	// ---- 阶段 6：宿主 go build（只构建，不运行原生窗口）----
	exe, buildOut, err := buildHost(host, opts)
	r.HostExe = exe
	if err != nil {
		// 构建失败：宿主目录保留为证据（go.mod/main.go/生成 View/资产
		// 都在），诊断带编译器原始输出。
		r.Phase = PhaseBuild
		r.KeptForEvidence = true
		return r, fmt.Errorf("preview: 宿主构建失败（证据保留在 %s）: %w\n%s", host.Dir, err, buildOut)
	}
	r.Phase = PhaseBuilt
	return r, nil
}

// Cleanup 清理本次运行的宿主目录（所有权核对通过后删除）。
//
// 调用时机由接线层决定：宿主窗口正常退出后调用；失败时目录已保留为
// 证据，调用方也可显式清理。返回错误表示所有权核对失败或删除失败。
func (r *Result) Cleanup() error {
	if r == nil || r.HostDir == "" {
		return nil
	}
	return cleanupHost(r.HostDir, r.hostToken)
}

// resolveInput 定位并读取输入，返回（绝对路径、源字节、module 根）。
func resolveInput(opts Options) (string, []byte, string, error) {
	if opts.File == "" {
		return "", nil, "", errors.New("preview: 输入文件为空；用法 goxml preview <file.gxml>")
	}
	if filepath.Ext(opts.File) != ".gxml" {
		return "", nil, "", fmt.Errorf("preview: 输入 %q 不是 .gxml 文件", opts.File)
	}
	base := opts.WorkDir
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, "", fmt.Errorf("preview: 无法确定工作目录: %w", err)
		}
		base = cwd
	}
	var abs string
	if filepath.IsAbs(opts.File) {
		abs = filepath.Clean(opts.File)
	} else {
		joined, jerr := filepath.Abs(filepath.Join(base, opts.File))
		if jerr != nil {
			return "", nil, "", fmt.Errorf("preview: 解析输入路径: %w", jerr)
		}
		abs = joined
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", nil, "", fmt.Errorf("preview: 输入 %s: %w", opts.File, err)
	}
	if !fi.Mode().IsRegular() {
		return "", nil, "", fmt.Errorf("preview: 输入 %s 不是普通文件", opts.File)
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return "", nil, "", fmt.Errorf("preview: 读取 %s: %w", opts.File, err)
	}
	if len(src) == 0 {
		return "", nil, "", fmt.Errorf("preview: 输入 %s 是空文件", opts.File)
	}
	moduleRoot, err := findModuleRoot(abs)
	if err != nil {
		return "", nil, "", err
	}
	pkg := opts.PackageName
	if pkg == "" {
		pkg = "preview"
	}
	if !token.IsIdentifier(pkg) {
		return "", nil, "", fmt.Errorf("preview: PackageName %q 不是合法 Go 包名", pkg)
	}
	return abs, src, moduleRoot, nil
}

// findModuleRoot 从 start 向上找 go.mod。
//
// 与 internal/cli 的同名逻辑语义一致；不复用是因为它未导出，而把它导出
// 需要修改共用文件。这里是**路径发现**，不做任何语义校验——不构成
// 第二套校验器。失败信息保留可执行的修复动作（与 CLI 口径一致）。
func findModuleRoot(start string) (string, error) {
	dir := start
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s 不在任何 Go module 内：预览需要读取 go.mod 构建类型环境；请先在工程根执行 `go mod init <module路径>`", start)
		}
		dir = parent
	}
}
