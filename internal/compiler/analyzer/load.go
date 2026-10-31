// Package analyzer —— Go 类型加载与 ViewModel 解析。
//
// 本文件实现契约 §4（输入 B：Go 类型环境）：
// 把 `ViewModel` 的原始声明解析为 Go 类型，供后续绑定与事件校验使用。
//
// 两步严格分开（契约 §5.1 的校验顺序 1）：
//
//  1. 声明解析：纯字符串文法，不接触 Go 工具链；
//  2. 包与类型解析：需要 go/packages，可能失败于多个环节。
//
// 分开的理由：文法错误的报错位置与语义错误不同，混在一起会让诊断
// 难以指向正确的原因。
package analyzer

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// 声明解析（纯文法，不接触 Go 工具链）
// ============================================================================

// TypeExpr 是解析后的 ViewModel 声明。
//
// 文法（契约 §4.2，已冻结）：
//
//	TypeExpr := Ident
//	          | PkgDir "." Ident
//	PkgDir   := Ident ("/" Ident)*
//
// 即**最后一个 `.`** 是包路径与类型名的分界，包路径内部只用 `/` 分隔。
type TypeExpr struct {
	// PkgDir 是模块根相对目录，如 "internal/models"。同包时为空串。
	PkgDir string
	// Name 是类型名。
	Name string
}

// String 返回规范形式，便于诊断与日志。
func (t TypeExpr) String() string {
	if t.PkgDir == "" {
		return t.Name
	}
	return t.PkgDir + "." + t.Name
}

// IsSamePackage 报告是否为「与 .gxml 同包」的形式。
func (t TypeExpr) IsSamePackage() bool { return t.PkgDir == "" }

// TypeExprError 是声明文法错误，携带可读原因。
type TypeExprError struct {
	// Input 是原始输入。
	Input string
	// Reason 说明违反了哪条规则。
	Reason string
}

func (e *TypeExprError) Error() string {
	return fmt.Sprintf("ViewModel %q: %s", e.Input, e.Reason)
}

// ParseTypeExpr 按冻结文法解析 ViewModel 声明。
//
// 明确拒绝（契约 §4.2 的表格）：绝对路径、空路径段、`.` 与 `..` 段、
// 反斜杠、完整导入路径、前导/尾随点、包路径内含点、非标识符段、
// 指针与泛型。
func ParseTypeExpr(s string) (TypeExpr, error) {
	fail := func(reason string) (TypeExpr, error) {
		return TypeExpr{}, &TypeExprError{Input: s, Reason: reason}
	}

	if s == "" {
		return fail("empty declaration")
	}
	// 指针、泛型、空白一律拒绝——它们不属于冻结文法。
	if strings.ContainsAny(s, "*[]<> \t\r\n") {
		return fail("only a type name or <package-dir>.<Type> is allowed; pointers, generics and spaces are not")
	}
	// 反斜杠：避免 Windows/Linux 路径歧义。
	if strings.ContainsRune(s, '\\') {
		return fail("backslash is not allowed; package directories use '/'")
	}
	// 绝对路径。
	if strings.HasPrefix(s, "/") {
		return fail("absolute paths are not allowed; package directories are relative to the module root")
	}

	// 以最后一个 '.' 分界。
	dot := strings.LastIndexByte(s, '.')
	var pkgDir, name string
	if dot < 0 {
		pkgDir, name = "", s
	} else {
		pkgDir, name = s[:dot], s[dot+1:]
	}

	// 类型名必须是非空合法标识符。
	if !isGoIdent(name) {
		return fail(fmt.Sprintf("type name %q is not a valid Go identifier", name))
	}
	if pkgDir == "" && dot >= 0 {
		return fail("empty package directory before '.'")
	}
	if dot >= 0 && strings.HasSuffix(s, ".") {
		return fail("trailing '.' with no type name")
	}

	// 包路径逐段校验。
	if pkgDir != "" {
		// 先识别完整导入路径：PkgDir 文法不允许内含点，而域名必然含点。
		// 放在段校验之前，才能给出针对性的提示（而不是笼统的「非法标识符」）。
		if strings.Contains(pkgDir, ".") {
			return fail("full import paths are not supported (v1); use a directory relative to the module root")
		}
		for _, seg := range strings.Split(pkgDir, "/") {
			switch seg {
			case "":
				return fail("empty path segment; use a single '/' between directories")
			case ".", "..":
				return fail("'.' and '..' segments are not allowed (path traversal)")
			}
			if !isGoIdent(seg) {
				return fail(fmt.Sprintf("path segment %q is not a valid Go identifier", seg))
			}
		}
	}

	return TypeExpr{PkgDir: pkgDir, Name: name}, nil
}

// isGoIdent 报告是否为合法的 Go 标识符。
func isGoIdent(s string) bool {
	if s == "" || s == "_" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// ============================================================================
// 加载配置
// ============================================================================

// Config 是加载器的外部配置。
//
// ModuleRoot 必须**显式提供**：不依赖进程当前工作目录恰好正确，
// 否则同一份代码在测试、CLI、IDE 三种入口下行为可能不同。
type Config struct {
	// ModuleRoot 是 go.mod 所在目录（绝对路径）。
	ModuleRoot string
	// SourceDir 是 .gxml 文件所在目录，用于解析「同包」声明。
	SourceDir string
}

// Validate 检查配置是否完整。
func (c Config) Validate() error {
	if c.ModuleRoot == "" {
		return fmt.Errorf("analyzer: ModuleRoot is required")
	}
	if !filepath.IsAbs(c.ModuleRoot) {
		return fmt.Errorf("analyzer: ModuleRoot must be absolute, got %q", c.ModuleRoot)
	}
	if fi, err := os.Stat(filepath.Join(c.ModuleRoot, "go.mod")); err != nil {
		return fmt.Errorf("analyzer: ModuleRoot %q has no go.mod: %w", c.ModuleRoot, err)
	} else if fi.IsDir() {
		return fmt.Errorf("analyzer: go.mod in %q is a directory", c.ModuleRoot)
	}
	return nil
}

// ============================================================================
// 类型环境
// ============================================================================

// TypeEnv 是一次分析的类型环境。
//
// 负责加载 Go 包并解析 ViewModel。同一实例内**缓存**已加载的包，
// 避免每个绑定表达式重复加载（契约 §4.3）。
type TypeEnv struct {
	cfg     Config
	modPath string
	// loaded 按导入路径缓存加载结果。
	loaded map[string]*packages.Package
}

// NewTypeEnv 创建类型环境。会读取 go.mod 以取得模块路径。
func NewTypeEnv(cfg Config) (*TypeEnv, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	modPath, err := readModulePath(cfg.ModuleRoot)
	if err != nil {
		return nil, err
	}
	return &TypeEnv{
		cfg:     cfg,
		modPath: modPath,
		loaded:  make(map[string]*packages.Package),
	}, nil
}

// ModulePath 返回模块导入路径。
func (e *TypeEnv) ModulePath() string { return e.modPath }

// readModulePath 读取 go.mod 的 module 行。
func readModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("analyzer: read go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", fmt.Errorf("analyzer: parse go.mod: %w", err)
	}
	if f.Module == nil || f.Module.Mod.Path == "" {
		return "", fmt.Errorf("analyzer: go.mod has no module directive")
	}
	return f.Module.Mod.Path, nil
}

// ViewModelType 是解析成功的 ViewModel 类型。
type ViewModelType struct {
	// Expr 是解析后的声明。
	Expr TypeExpr
	// Named 是 Go 类型。仅在 analyzer 内部使用，不进入 contracts。
	Named *types.Named
	// ImportPath 是所在包的导入路径。
	ImportPath string
}

// ResolveViewModel 解析 ViewModel 声明并定位 Go 类型。
//
// 返回 (nil, nil) 表示声明缺省（纯静态页面），这不是错误。
//
// 失败一律映射到 GXML-ROOT-002（诊断码目录 §2.1）。四类原因通过
// 消息区分，不各占一个码——它们的修复动作相同：修正 ViewModel 声明。
func (e *TypeEnv) ResolveViewModel(decl contracts.ViewModelDecl) (*ViewModelType, *contracts.CompilerError) {
	if decl.IsZero() {
		return nil, nil
	}

	resolveErr := func(format string, args ...any) *contracts.CompilerError {
		cerr := contracts.NewError("", decl.Span, CodeViewModelType, format, args...)
		return &cerr
	}

	// 阶段 1：文法。
	expr, err := ParseTypeExpr(decl.Value)
	if err != nil {
		return nil, resolveErr("%s", err.Error())
	}

	// 阶段 2：包定位。
	importPath, err := e.importPathFor(expr)
	if err != nil {
		return nil, resolveErr("ViewModel %q: %s", decl.Value, err.Error())
	}

	// 阶段 3：加载。
	pkg, err := e.load(importPath)
	if err != nil {
		return nil, resolveErr("ViewModel %q: %s", decl.Value, err.Error())
	}

	// 阶段 4：类型查找。只在目标包的作用域里找，不做全局搜索——
	// 全局搜索会把不同包里同名类型混淆。
	obj := pkg.Types.Scope().Lookup(expr.Name)
	if obj == nil {
		return nil, resolveErr("ViewModel %q: type %q not found in package %q",
			decl.Value, expr.Name, importPath)
	}
	// 类型别名（type A = Real）：Go1.22+ 的 obj.Type() 可能是 *types.Alias。
	// 先 Unalias 取底层类型，再判定命名类型——别名应解析为其目标命名类型，
	// 否则会把合法别名误报成 "not a named type"（R3 切片2 实测缺陷）。
	resolved := types.Unalias(obj.Type())
	named, ok := resolved.(*types.Named)
	if !ok {
		return nil, resolveErr("ViewModel %q: %q in package %q is not a named type (got %s)",
			decl.Value, expr.Name, importPath, resolved.String())
	}

	return &ViewModelType{Expr: expr, Named: named, ImportPath: importPath}, nil
}

// importPathFor 把声明转换为导入路径。
func (e *TypeEnv) importPathFor(expr TypeExpr) (string, error) {
	if !expr.IsSamePackage() {
		return e.modPath + "/" + expr.PkgDir, nil
	}
	// 同包：用 .gxml 所在目录相对模块根解析。
	if e.cfg.SourceDir == "" {
		return "", fmt.Errorf("same-package form requires SourceDir in Config")
	}
	rel, err := filepath.Rel(e.cfg.ModuleRoot, e.cfg.SourceDir)
	if err != nil {
		return "", fmt.Errorf("cannot relate source dir to module root: %w", err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return e.modPath, nil
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("source dir is outside the module root")
	}
	return e.modPath + "/" + rel, nil
}

// load 加载并缓存一个包。
//
// 关键判据（由 load_behavior_test.go 锁定）：
// packages.Load 的 error 返回值几乎总是 nil，真正的错误在 pkg.Errors；
// 且损坏的包 Types 可能非 nil。因此必须检查 Errors，不能看 Types。
func (e *TypeEnv) load(importPath string) (*packages.Package, error) {
	if p, ok := e.loaded[importPath]; ok {
		return p, nil
	}

	cfg := &packages.Config{
		Dir: e.cfg.ModuleRoot,
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, importPath)
	if err != nil {
		return nil, fmt.Errorf("cannot load package %q: %w", importPath, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("package %q not found", importPath)
	}
	p := pkgs[0]
	if len(p.Errors) > 0 {
		// 保留错误码/原因/归属不变（ROOT-002 由上层包装），这里只把
		// go/packages 的可靠位置（Error.Pos）结构化保留下来，避免丢失
		// 可定位的文件:行:列。位置不可靠时不伪造（见 load_diagnostic.go）。
		return nil, formatLoadFailure(importPath, e.cfg.ModuleRoot, p.Errors)
	}
	if p.Types == nil {
		return nil, fmt.Errorf("package %q has no type information", importPath)
	}

	e.loaded[importPath] = p
	return p, nil
}

// LookupNamed 返回包中指定名字的命名类型，用于类型身份比较。
//
// 用途：`color` / `insets` 属性必须按**精确类型**判定（契约 §11.5），
// 而 `types.Identical` 需要拿到对方包的 types.Type 对象。
//
// 结果按「包路径 + 名字」缓存，避免每次比较都重新加载。
func (e *TypeEnv) LookupNamed(pkgPath string, names []string) ([]types.Type, error) {
	pkg, err := e.load(pkgPath)
	if err != nil {
		return nil, err
	}
	out := make([]types.Type, 0, len(names))
	for _, n := range names {
		obj := pkg.Types.Scope().Lookup(n)
		if obj == nil {
			continue
		}
		out = append(out, obj.Type())
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("analyzer: none of %v found in package %q", names, pkgPath)
	}
	return out, nil
}
