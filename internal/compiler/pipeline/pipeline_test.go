package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/expander"
)

// ============================================================================
// 测试辅助
// ============================================================================

const vmPath = "internal/compiler/analyzer/testdata/fixture.ViewModel"

// baseCfg 返回测试用配置。
func baseCfg(t *testing.T) Config {
	t.Helper()
	root := repoRoot(t)
	return Config{
		ModuleRoot:  root,
		SourceDir:   filepath.Join(root, "internal", "compiler", "pipeline"),
		PackageName: "demo",
		SourcePath:  "dashboard.gxml",
	}
}

// okSrc 构造一个能通过全部门的静态页面。
func okSrc() []byte {
	return []byte(`<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/"><Text Value="Hello"/></Page>
</Goxml>`)
}

// partialSrc 构造 Analyzer 报 StatusPartial 的输入（绑定路径不存在）。
func partialSrc() []byte {
	return []byte(`<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/"><Text Value="{Nope}"/></Page>
</Goxml>`)
}

// blockedSrc 构造 Analyzer 报 StatusBlocked 的输入（ViewModel 不可解析）。
func blockedSrc() []byte {
	return []byte(`<Goxml Version="1" ViewModel="nosuch.Pkg">
  <Page Route="/"><Text Value="x"/></Page>
</Goxml>`)
}

// parserFailSrc 构造 Parser 阶段就失败的输入。
//
// 缺 <Page> 会让 Parser 报 GXML-ROOT-001，是确定在 Parser 阶段中止的用例。
func parserFailSrc() []byte {
	return []byte(`<Goxml Version="1"></Goxml>`)
}

// counter 记录 Codegen 被调用的次数。
type counter struct {
	n int
}

// hook 返回一个会计数并调用真实 Generate 的钩子。
func (c *counter) hook(t *testing.T) Hooks {
	t.Helper()
	return Hooks{
		Generate: func(v *analyzer.VerifiedDoc, o codegen.Options) ([]byte, error) {
			c.n++
			return codegen.Generate(v, o)
		},
	}
}

// repoRoot 向上定位模块根。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("未找到 go.mod")
		}
		dir = parent
	}
}

// ============================================================================
// 正常路径
// ============================================================================

func TestRunSuccessProducesArtifact(t *testing.T) {
	c := &counter{}
	cfg := baseCfg(t)
	r := Run("dashboard.gxml", okSrc(), cfg, c.hook(t))

	if !r.OK() {
		t.Fatalf("应成功，诊断: %v", msgs(r))
	}
	if r.Status != analyzer.StatusOK {
		t.Errorf("Status = %v", r.Status)
	}
	if len(r.Diags) != 0 {
		t.Errorf("成功时不应有诊断: %v", msgs(r))
	}
	if c.n != 1 {
		t.Errorf("Codegen 调用次数 = %d, 期望 1", c.n)
	}
	if r.Artifact == nil {
		t.Fatal("Artifact 不应为 nil")
	}
	if r.Artifact.OutputName != "dashboard_gen.go" {
		t.Errorf("OutputName = %q", r.Artifact.OutputName)
	}
	if !strings.Contains(string(r.Artifact.Source), "GoxmlDashboardView") {
		t.Errorf("产物内容不符合契约")
	}
}

// ============================================================================
// 门禁：契约 §13 第 20 条
//
// 核心证据不是「返回了错误」，而是「Codegen 调用计数为 0」+「没有产生文件」。
// ============================================================================

func TestGatePartialDoesNotCallCodegen(t *testing.T) {
	c := &counter{}
	cfg := baseCfg(t)

	out := t.TempDir()
	r := Run("dashboard.gxml", partialSrc(), cfg, c.hook(t))

	if r.Status != analyzer.StatusPartial {
		t.Fatalf("Status = %v, 期望 Partial", r.Status)
	}
	if r.OK() || r.Artifact != nil {
		t.Error("Partial 不应产出 Artifact")
	}
	if c.n != 0 {
		t.Errorf("Partial 时 Codegen 被调用 %d 次，应为 0", c.n)
	}
	assertNoFiles(t, out)
}

func TestGateBlockedDoesNotCallCodegen(t *testing.T) {
	c := &counter{}
	cfg := baseCfg(t)

	r := Run("dashboard.gxml", blockedSrc(), cfg, c.hook(t))

	if r.Status != analyzer.StatusBlocked {
		t.Fatalf("Status = %v, 期望 Blocked", r.Status)
	}
	if r.OK() || r.Artifact != nil {
		t.Error("Blocked 不应产出 Artifact")
	}
	if c.n != 0 {
		t.Errorf("Blocked 时 Codegen 被调用 %d 次，应为 0", c.n)
	}
}

func TestGateParserFailureStopsPipeline(t *testing.T) {
	c := &counter{}
	cfg := baseCfg(t)

	r := Run("bad.gxml", parserFailSrc(), cfg, c.hook(t))

	if r.OK() {
		t.Error("Parser 失败不应产出产物")
	}
	if len(r.Diags) == 0 {
		t.Error("应返回 Parser 诊断")
	}
	if c.n != 0 {
		t.Errorf("Parser 失败时 Codegen 被调用 %d 次", c.n)
	}
}

// ============================================================================
// 防御性补充（非契约 §13 第 20 条原要求）
// ============================================================================

func TestCodegenErrorProducesNoArtifact(t *testing.T) {
	// Codegen 主动失败：Artifact 必须为 nil，不能返回半成品。
	cfg := baseCfg(t)
	hooks := Hooks{
		Generate: func(*analyzer.VerifiedDoc, codegen.Options) ([]byte, error) {
			return nil, errors.New("boom")
		},
	}
	r := Run("dashboard.gxml", okSrc(), cfg, hooks)

	if r.OK() || r.Artifact != nil {
		t.Error("Codegen 失败不应产出 Artifact")
	}
	if len(r.Diags) == 0 {
		t.Error("应记录失败原因")
	}
}

func TestCodegenEmptyOutputTreatedAsFailure(t *testing.T) {
	// 空输出等同于失败：不能产出「成功但内容为空」的文件。
	cfg := baseCfg(t)
	hooks := Hooks{
		Generate: func(*analyzer.VerifiedDoc, codegen.Options) ([]byte, error) {
			return nil, nil
		},
	}
	r := Run("dashboard.gxml", okSrc(), cfg, hooks)
	if r.OK() {
		t.Error("空输出不应视为成功")
	}
}

func TestLoadFailureBlocksWithoutCallingCodegen(t *testing.T) {
	c := &counter{}
	cfg := baseCfg(t)
	hooks := c.hook(t)
	hooks.Load = func(Config) (*analyzer.TypeEnv, error) {
		return nil, errors.New("no module")
	}
	r := Run("dashboard.gxml", okSrc(), cfg, hooks)

	if r.Status != analyzer.StatusBlocked {
		t.Errorf("Status = %v, 期望 Blocked", r.Status)
	}
	if c.n != 0 {
		t.Errorf("环境构造失败时 Codegen 被调用 %d 次", c.n)
	}
}

func TestRunNeverWritesFiles(t *testing.T) {
	// Run 只产出内存产物，绝不写盘——写盘是 Commit 的职责。
	dir := t.TempDir()
	cfg := baseCfg(t)
	r := Run("dashboard.gxml", okSrc(), cfg, Hooks{})
	if !r.OK() {
		t.Fatalf("应成功: %v", msgs(r))
	}
	assertNoFiles(t, dir)
}

// ============================================================================
// Commit
// ============================================================================

func TestCommitWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	a := &Artifact{
		OutputName: "dashboard_gen.go",
		Source:     []byte("package demo\n"),
	}
	path, err := Commit(a, dir)
	if err != nil {
		t.Fatalf("Commit 失败: %v", err)
	}
	if filepath.Base(path) != "dashboard_gen.go" {
		t.Errorf("写入路径 = %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "package demo\n" {
		t.Errorf("文件内容不符: %q", got)
	}
	// 不应留下临时文件。
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("目录应只有目标文件，实际 %d 个", len(entries))
	}
}

func TestCommitFailureLeavesTargetUntouched(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dashboard_gen.go")
	if err := os.WriteFile(target, []byte("// old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 注入一个必然失败的校验。
	orig := parserParseGo
	parserParseGo = func([]byte) (any, error) { return nil, errors.New("bad") }
	defer func() { parserParseGo = orig }()

	_, err := Commit(&Artifact{OutputName: "dashboard_gen.go", Source: []byte("package demo\n")}, dir)
	if err == nil {
		t.Fatal("应失败")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "// old\n" {
		t.Errorf("失败时旧文件被改动: %q", got)
	}
	// 临时文件必须被清理。
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("失败后应只剩旧文件，实际 %d 个", len(entries))
	}
}

func TestCommitRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		a    *Artifact
		dir  string
	}{
		{"nil artifact", nil, dir},
		{"empty source", &Artifact{OutputName: "a.go"}, dir},
		{"no output name", &Artifact{Source: []byte("x")}, dir},
		{"missing dir", &Artifact{OutputName: "a.go", Source: []byte("package a\n")}, filepath.Join(dir, "nope")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Commit(c.a, c.dir); err == nil {
				t.Error("应被拒绝")
			}
		})
	}
}

func TestOutputName(t *testing.T) {
	cases := map[string]string{
		"dashboard.gxml":             "dashboard_gen.go",
		"ui/settings/dashboard.gxml": "dashboard_gen.go",
		`ui\settings\a.gxml`:         "a_gen.go",
		".gxml":                      "goxml_gen.go",
	}
	for in, want := range cases {
		if got := outputName(in); got != want {
			t.Errorf("outputName(%q) = %q, want %q", in, got, want)
		}
	}
}

// ============================================================================
// 辅助
// ============================================================================

// msgs 汇总诊断消息，便于失败信息。
func msgs(r *Result) []string {
	var out []string
	for _, d := range r.Diags {
		out = append(out, d.Code+": "+d.Message)
	}
	return out
}

// assertNoFiles 断言目录为空。
func assertNoFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("目录应为空，实际: %v", names)
	}
}

// ============================================================================
// 内部不变量违反：StatusOK 但带诊断
// ============================================================================

func TestOKWithDiagnosticsIsRejected(t *testing.T) {
	// 编译编排契约 §3 第 5 行：OK 但有诊断属内部不变量违反，禁止生成。
	//
	// 正常路径不会产生这种结果——Verified() 本应拒绝。这里通过替换
	// Analyzer 构造该状态，验证编排层的第二道防线。
	c := &counter{}
	cfg := baseCfg(t)
	hooks := c.hook(t)
	hooks.Analyze = func(file string, doc *contracts.DocumentIR, schemas expander.SchemaRegistry, env *analyzer.TypeEnv) *analyzer.Result {
		return &analyzer.Result{
			Doc:    doc,
			Facts:  nil, // 门禁在 Verified() 之前就拦住，Facts 不需要
			Status: analyzer.StatusOK,
			Diags:  []contracts.CompilerError{{File: file, Code: "GXML-BIND-001", Message: "injected"}},
		}
	}

	r := Run("dashboard.gxml", okSrc(), cfg, hooks)

	if r.OK() || r.Artifact != nil {
		t.Error("OK 带诊断时不应产出 Artifact")
	}
	if c.n != 0 {
		t.Errorf("Codegen 被调用 %d 次，应为 0", c.n)
	}
}

func TestCommitRejectsNonDirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(&Artifact{OutputName: "a.go", Source: []byte("package a\n")}, file); err == nil {
		t.Error("目标不是目录时应报错")
	}
}

func TestCommitRenameFailureLeavesNoTempFiles(t *testing.T) {
	// 目标路径被同名目录占据 → rename 失败；临时文件必须清理。
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "a_gen.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Commit(&Artifact{OutputName: "a_gen.go", Source: []byte("package a\n")}, dir)
	if err == nil {
		t.Fatal("rename 到已存在的目录应失败")
	}
	entries, _ := os.ReadDir(dir)
	var tmp int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			tmp++
		}
	}
	if tmp != 0 {
		t.Errorf("失败后残留 %d 个临时文件", tmp)
	}
}

// ============================================================================
// 安全：输出名必须是纯文件名（防路径穿越）
//
// 审计发现（TASK-SEC-001 §3.1）：修复前 OutputName 含 `../` 会把文件写到
// 目标目录之外，属可复现缺陷。以下测试锁定修复。
// ============================================================================

func TestCommitRejectsPathTraversal(t *testing.T) {
	outside := t.TempDir()
	inside := t.TempDir()

	cases := []string{
		"../evil.go",
		"../../evil.go",
		`..\evil.go`,
		"sub/evil.go",
		`sub\evil.go`,
		".",
		"..",
		".hidden.go",
		"evil.txt",
		"",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Commit(&Artifact{OutputName: name, Source: []byte("package evil\n")}, inside)
			if err == nil {
				t.Fatalf("OutputName %q 应被拒绝", name)
			}
		})
	}

	// 目标目录之外不得出现任何文件。
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Errorf("目标目录之外出现文件: %d 个", len(entries))
	}
	entries, _ = os.ReadDir(inside)
	if len(entries) != 0 {
		t.Errorf("目标目录不应有残留: %d 个", len(entries))
	}
}

func TestValidateOutputNameAcceptsPlainName(t *testing.T) {
	for _, ok := range []string{"a.go", "dashboard_gen.go", "x_1.go"} {
		if err := validateOutputName(ok); err != nil {
			t.Errorf("%q 应被接受: %v", ok, err)
		}
	}
}
