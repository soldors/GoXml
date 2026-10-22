// Package pipeline 把编译前端与 Codegen 串成一条管线，并守住失败门禁。
//
// 职责边界（编译编排契约 §1–§5）：
//   - 控制流归本包：任一阶段失败即中止，不进入后续阶段；
//   - 生成归 codegen：本包只决定「是否调用」「是否产出可提交产物」；
//   - 本包不写盘。写盘由 Commit 负责，且与 Run 分开建模——
//     Artifact 存在只表示「允许提交」，不代表已写入。
package pipeline

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// Config 是管线的外部输入。
//
// 包名与源路径由调用方按契约 §4.1/§4.4 确定，本包不推导、不猜测。
type Config struct {
	// ModuleRoot 是 go.mod 所在目录（绝对路径）。
	ModuleRoot string
	// SourceDir 是 .gxml 所在目录，用于解析同包 ViewModel。
	SourceDir string
	// PackageName 是生成文件的 package 子句。
	PackageName string
	// SourcePath 是 .gxml 相对目标包目录的路径。
	SourcePath string
}

// Artifact 是内存中的完整生成结果。
//
// 它的存在意味着「全部阶段成功、允许提交」，**不代表已写入磁盘**。
type Artifact struct {
	// PackageName 是生成文件的 package 子句。
	PackageName string
	// SourcePath 是 .gxml 相对目标包目录的路径。
	SourcePath string
	// Source 是已格式化的 Go 源码。
	Source []byte
	// OutputName 是生成文件的文件名。
	OutputName string
}

// Result 是一次管线运行的完整结果。
type Result struct {
	// Status 是 Analyzer 的结论。Parser/Expander 失败时为 StatusBlocked——
	// 它们的诊断意味着无法继续语义分析。
	Status analyzer.Status
	// Diags 汇总所有阶段产生的诊断。
	Diags []contracts.CompilerError
	// Artifact 只在全部阶段成功、且 Codegen 成功时非 nil。
	Artifact *Artifact
	// ModulePath 是模块路径，由前端构造类型环境时顺带带出。
	//
	// 为什么放在 Result 里：frontEnd 与 Run 拆开后，Codegen 需要的
	// ModulePath 必须有个载体；它属于前端产物，不是调用方输入。
	ModulePath string
}

// OK 报告是否产出了可提交的产物。
func (r *Result) OK() bool { return r != nil && r.Artifact != nil }

// Hooks 是阶段钩子。零值表示使用真实实现。
//
// 存在目的：让测试能观察「Codegen 是否被调用」，而不必改造生产架构。
// 这不是插件体系——不要扩展成通用的阶段注册机制（编译编排契约 §5）。
type Hooks struct {
	// Generate 替换 Codegen 阶段。
	Generate func(*analyzer.VerifiedDoc, codegen.Options) ([]byte, error)
	// Load 替换类型环境构造。
	Load func(cfg Config) (*analyzer.TypeEnv, error)
	// Analyze 替换 Analyzer 阶段。
	//
	// 存在的唯一理由：让测试能构造「StatusOK 但有诊断」这种正常路径
	// 无法产生的内部不变量违反（见契约 §3 的第 5 行）。
	Analyze func(file string, doc *contracts.DocumentIR, schemas expander.SchemaRegistry, env *analyzer.TypeEnv) *analyzer.Result
}

// Run 执行完整管线（前端 + Codegen）。
//
// 任一中止点之后不再执行任何后续阶段（编译编排契约 §2）。
//
// 只需跑一次前端的场景（预检要拿 bindSlots，随后还要生成同一个文件）请用
// Prepare + RunPrepared 复用，不要把 Run 跑两遍——实测三文件工程会多出
// ~0.86 s（+40%），见 dev-log `2026-10-22.md` §6。
func Run(file string, src []byte, cfg Config, hooks Hooks) *Result {
	return RunPrepared(Prepare(file, src, cfg, hooks), hooks)
}

// Prepared 是一次编译前端运行的结果，可在**预检**与**生成**之间复用。
//
// 存在理由：C5 的预检需要 `bindSlots`（只有跑完前端才知道），而阶段一还要
// 用同一个文件生成产物。若两边各跑一遍前端，光是类型环境构造就会让每次
// 生成多花 ~285 ms/文件。复用后总代价与 C4 持平。
//
// 不可变性：Prepared 一旦产出就不该被改写；它持有 VerifiedDoc，
// 而 VerifiedDoc 被 Facts freeze 保护（Analyzer 契约）。
type Prepared struct {
	file string
	cfg  Config
	// Doc 是已验证文档；前端失败时为 nil。
	Doc *analyzer.VerifiedDoc
	// Status / Diags 复制自前端结果，供调用方在失败时判断与回退。
	Status analyzer.Status
	Diags  []contracts.CompilerError
	// modulePath 由前端构造类型环境时带出，Codegen 需要。
	modulePath string
}

// Prepare 只跑编译前端（parse→expand→analyze→verify），不进入 Codegen。
//
// 失败不返回 error：前端失败是**正常输入**（语法/绑定错误），诊断在 Diags 里，
// 由调用方决定是「回退保守上界」还是「进入生成阶段报错」。
func Prepare(file string, src []byte, cfg Config, hooks Hooks) *Prepared {
	v, res := frontEnd(file, src, cfg, hooks)
	return &Prepared{
		file:       file,
		cfg:        cfg,
		Doc:        v,
		Status:     res.Status,
		Diags:      res.Diags,
		modulePath: res.ModulePath,
	}
}

// Slots 返回页面会分配的显示缓存槽位数。
//
// 前端未通过时返回 error —— 调用方必须**回退为保守上界**，不得把语法错误
// 伪装成命名冲突（阶段一才是报编译错误的地方）。
func (p *Prepared) Slots() (int, error) {
	if p == nil || p.Doc == nil {
		n := 0
		st := analyzer.StatusBlocked
		if p != nil {
			n, st = len(p.Diags), p.Status
		}
		return 0, fmt.Errorf("编译前端未通过（状态 %v，%d 条诊断），无法确定绑定槽位数", st, n)
	}
	return codegen.CountBindSlots(p.Doc)
}

// RunPrepared 在已准备好的前端结果上执行 Codegen 并产出 Artifact。
//
// 前端诊断会被原样带进结果：调用方看到的诊断与「前端是否复用」无关。
func RunPrepared(p *Prepared, hooks Hooks) *Result {
	res := &Result{Status: analyzer.StatusBlocked}
	if p == nil {
		return res
	}
	res.Status = p.Status
	res.Diags = append(res.Diags, p.Diags...)
	res.ModulePath = p.modulePath
	if p.Doc == nil {
		return res
	}
	file := p.file

	// ---- 5. Codegen ----
	//
	// ModulePath 必须传：跨包 ViewModel 的绑定页面要拼其 import 路径。
	// 缺它会报 "Options.ModulePath is empty"（同包 ViewModel 不需要，
	// 因此只有跨包场景会暴露——由 TestRunPassesModulePath 守住）。
	opts := codegen.Options{
		PackageName: p.cfg.PackageName,
		SourcePath:  p.cfg.SourcePath,
		ModulePath:  p.modulePath,
	}
	gen := hooks.Generate
	if gen == nil {
		gen = codegen.Generate
	}
	out, err := gen(p.Doc, opts)
	if err != nil {
		res.Diags = append(res.Diags, contracts.CompilerError{
			File:    file,
			Message: fmt.Sprintf("code generation failed: %v", err),
		})
		return res
	}
	// 防止某个 Generate 实现在失败语义上偷懒：空输出等同于失败。
	if len(out) == 0 {
		res.Diags = append(res.Diags, contracts.CompilerError{
			File:    file,
			Message: "code generation returned empty output",
		})
		return res
	}

	// ---- 6. 产物 ----
	res.Artifact = &Artifact{
		PackageName: p.cfg.PackageName,
		SourcePath:  p.cfg.SourcePath,
		Source:      out,
		OutputName:  outputName(p.cfg.SourcePath),
	}
	return res
}

// frontEnd 执行 parse→expand→analyze→verify，产出已验证文档。
//
// 与 Run 共用同一实现是刻意的：预检若另写一条前端链，就会出现
// 「预检认定的绑定」与「生成时认定的绑定」不一致的第二份真相。
//
// 返回 nil 文档表示前端已失败（诊断已累积在 res 中），调用方必须立即返回。
func frontEnd(file string, src []byte, cfg Config, hooks Hooks) (*analyzer.VerifiedDoc, *Result) {
	res := &Result{Status: analyzer.StatusBlocked}

	// ---- 1. Parser ----
	doc, pdiags := parser.Parse(file, src)
	if len(pdiags) > 0 {
		res.Diags = append(res.Diags, pdiags...)
		return nil, res
	}

	// ---- 2. Expander ----
	er := expander.Expand(file, doc)
	if len(er.Diags) > 0 {
		res.Diags = append(res.Diags, er.Diags...)
		return nil, res
	}

	// ---- 3. Analyzer ----
	env, err := loadEnv(cfg, hooks)
	if err != nil {
		res.Status = analyzer.StatusBlocked
		res.Diags = append(res.Diags, contracts.CompilerError{
			File:    file,
			Code:    "GXML-ROOT-002",
			Message: fmt.Sprintf("cannot prepare type environment: %v", err),
		})
		return nil, res
	}

	analyze := hooks.Analyze
	if analyze == nil {
		analyze = analyzer.Analyze
	}
	ar := analyze(file, er.Doc, er.Schemas, env)
	res.Status = ar.Status
	res.Diags = append(res.Diags, ar.Diags...)

	// 门禁：只有 StatusOK 且无诊断才继续。
	//
	// 「OK 但有诊断」是内部不变量违反——Verified() 本应拒绝，
	// 这里再加一道，避免依赖单点防护。
	if ar.Status != analyzer.StatusOK || len(ar.Diags) > 0 {
		return nil, res
	}

	// ---- 4. 凭证 ----
	v, err := ar.Verified()
	if err != nil {
		// 走到这里说明 Analyzer 的内部不变量被破坏。不改状态，
		// 只补一条诊断说明原因。
		res.Diags = append(res.Diags, contracts.CompilerError{
			File:    file,
			Message: fmt.Sprintf("analyzer reported OK but verification failed: %v", err),
		})
		return nil, res
	}

	// ModulePath 由前端顺带带出：Codegen 阶段要用，而 SlotsFor 用不到。
	res.ModulePath = env.ModulePath()
	return v, res
}

// loadEnv 构造类型环境。
func loadEnv(cfg Config, hooks Hooks) (*analyzer.TypeEnv, error) {
	if hooks.Load != nil {
		return hooks.Load(cfg)
	}
	return analyzer.NewTypeEnv(analyzer.Config{
		ModuleRoot: cfg.ModuleRoot,
		SourceDir:  cfg.SourceDir,
	})
}

// outputName 由源路径推导生成文件名。
//
// 规则：`ui/dashboard.gxml` → `dashboard_gen.go`，只取最后一段，
// 与源文件同目录（契约 §3）。
func outputName(sourcePath string) string {
	p := strings.ReplaceAll(sourcePath, "\\", "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	p = strings.TrimSuffix(p, ".gxml")
	if p == "" {
		p = "goxml"
	}
	return p + "_gen.go"
}

// Commit 把产物原子写入 dir。
//
// 顺序（编译编排契约 §4、代码生成契约 §9）：
//  1. 在 dir 写临时文件；
//  2. 重新读取并校验可解析；
//  3. 原子替换目标文件；
//  4. 任一步失败都删除临时文件，目标文件保持原样。
//
// v1 不处理 manifest 与孤儿清理（契约 §10）、增量缓存（§11）。
func Commit(a *Artifact, dir string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("pipeline: nil artifact; nothing to commit")
	}
	if len(a.Source) == 0 {
		return "", fmt.Errorf("pipeline: artifact has empty source")
	}
	if a.OutputName == "" {
		return "", fmt.Errorf("pipeline: artifact has no output name")
	}
	// 输出名必须是**纯文件名**。
	//
	// 不校验的后果（已实测）：OutputName 里的 `../` 会把文件写到目标目录
	// 之外。Artifact 可能由调用方构造，因此这里必须防守，不能信任上游。
	if err := validateOutputName(a.OutputName); err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("pipeline: output dir %q: %w", dir, err)
	} else if !fi.IsDir() {
		return "", fmt.Errorf("pipeline: output dir %q is not a directory", dir)
	}

	target := filepath.Join(dir, a.OutputName)

	tmp, err := os.CreateTemp(dir, ".goxml-*.tmp")
	if err != nil {
		return "", fmt.Errorf("pipeline: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	// 失败时清理临时文件；成功 rename 之后该路径已不存在，忽略错误。
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(a.Source); err != nil {
		tmp.Close()
		return "", fmt.Errorf("pipeline: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("pipeline: close temp file: %w", err)
	}

	// 重新读取校验：防止写入过程被截断或编码损坏。
	written, err := os.ReadFile(tmpName)
	if err != nil {
		return "", fmt.Errorf("pipeline: re-read temp file: %w", err)
	}
	if _, err := parserParseGo(written); err != nil {
		return "", fmt.Errorf("pipeline: generated file does not parse: %w", err)
	}

	if err := os.Rename(tmpName, target); err != nil {
		return "", fmt.Errorf("pipeline: replace %q: %w", target, err)
	}
	return target, nil
}

// parserParseGo 校验源码可解析。抽成变量便于测试注入失败。
var parserParseGo = func(src []byte) (any, error) {
	return format.Source(src)
}

// validateOutputName 校验输出名是单个普通文件名。
//
// 规则：
//   - 不含路径分隔符（/ 或 \），因此不可能穿越目录；
//   - 不是 "." 或 ".."；
//   - 不以 "." 开头（避免生成隐藏文件，也排除 "..foo" 之类歧义）；
//   - 不以 ".go" 之外的后缀？—— 不限制后缀，但要求以 .go 结尾
//     （生成物必然是 Go 源码）。
func validateOutputName(name string) error {
	if name == "" {
		return fmt.Errorf("pipeline: empty output name")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("pipeline: output name %q must not contain a path separator", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("pipeline: output name %q is a directory reference", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("pipeline: output name %q must not start with '.'", name)
	}
	if !strings.HasSuffix(name, ".go") {
		return fmt.Errorf("pipeline: output name %q must end with .go", name)
	}
	return nil
}
