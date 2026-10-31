package analyzer

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// R3 切片1：包加载失败的可靠输入定位（R2-qcode-09）。
// 用真实临时 module 复现缺包 / Go 语法错 / 类型错 / 未知 VM；
// 路径包含空格与 Unicode，验证不按“冒号首段”切分、盘符与行列正确。

func TestParseErrorPosRobust(t *testing.T) {
	cases := []struct {
		in   string
		file string
		line int
		col  int
		ok   bool
	}{
		{`C:\work dir\文 件.go:12:3`, `C:\work dir\文 件.go`, 12, 3, true},
		{`C:/work dir/文件.go:7`, `C:/work dir/文件.go`, 7, 0, true},
		{`relative/x.go:5:9`, `relative/x.go`, 5, 9, true},
		{`C:\a.go:0:3`, "", 0, 0, false},   // 0 列不可靠
		{`C:\a.go:abc`, "", 0, 0, false},   // 非数字
		{`just-a-string`, "", 0, 0, false}, // 无数字段
		{``, "", 0, 0, false},
	}
	for _, c := range cases {
		f, l, col, ok := parseErrorPos(c.in)
		if ok != c.ok || (ok && (f != c.file || l != c.line || col != c.col)) {
			t.Errorf("parseErrorPos(%q) = (%q,%d,%d,%v), want (%q,%d,%d,%v)", c.in, f, l, col, ok, c.file, c.line, c.col, c.ok)
		}
	}
}

func writeModule(t *testing.T) string {
	t.Helper()
	// 目录名特意含空格与 Unicode，覆盖 Windows 路径解析。
	root := filepath.Join(t.TempDir(), "有 空格 模块")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.24\n")
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func newModEnv(t *testing.T) (string, *TypeEnv) {
	root := writeModule(t)
	env, err := NewTypeEnv(Config{ModuleRoot: root})
	if err != nil {
		t.Fatalf("NewTypeEnv: %v", err)
	}
	return root, env
}

func TestLoadFailureSyntaxErrorHasRealPosition(t *testing.T) {
	root, env := newModEnv(t)
	badPath := filepath.Join(root, "bad", "bad.go")
	writeFile(t, badPath, "package bad\n\nfunc Broken( {\n")

	_, err := env.load("example.com/m/bad")
	if err == nil {
		t.Fatal("语法错误包应加载失败")
	}
	le, ok := err.(*PackageLoadError)
	if !ok {
		t.Fatalf("应为 *PackageLoadError，实际 %T: %v", err, err)
	}
	if !le.HasPos {
		t.Fatalf("语法错误应带可靠位置，实际 RawPos=%q msg=%q", le.RawPos, le.Msg)
	}
	if !samePath(le.File, badPath) {
		t.Fatalf("定位文件 = %q, want %q", le.File, badPath)
	}
	if le.Line != 3 || le.Col <= 0 {
		t.Fatalf("位置 = %d:%d, want 3:>0", le.Line, le.Col)
	}
	// 消息里必须出现可定位的 file:line:col，且不是伪造的 GXML 位置。
	wantLoc := le.File + ":" + strconv.Itoa(le.Line) + ":" + strconv.Itoa(le.Col)
	if !strings.Contains(le.Error(), wantLoc) {
		t.Fatalf("错误消息缺少真实位置 %q: %q", wantLoc, le.Error())
	}
}

func TestLoadFailureTypeErrorHasRealPosition(t *testing.T) {
	root, env := newModEnv(t)
	p := filepath.Join(root, "typed", "typed.go")
	writeFile(t, p, "package typed\n\nvar X int = \"s\"\n")

	_, err := env.load("example.com/m/typed")
	if err == nil {
		t.Fatal("类型错误包应加载失败")
	}
	le, ok := err.(*PackageLoadError)
	if !ok || !le.HasPos {
		t.Fatalf("类型错误应带可靠位置，实际 %T %+v", err, err)
	}
	if !samePath(le.File, p) {
		t.Fatalf("定位文件 = %q, want %q", le.File, p)
	}
	if le.Line != 3 {
		t.Fatalf("位置行 = %d, want 3", le.Line)
	}
}

func TestLoadFailureMissingPackageNoFakePosition(t *testing.T) {
	_, env := newModEnv(t)

	_, err := env.load("example.com/m/nosuchpkg")
	if err == nil {
		t.Fatal("缺包应加载失败")
	}
	le, ok := err.(*PackageLoadError)
	if !ok {
		t.Fatalf("应为 *PackageLoadError，实际 %T", err)
	}
	if le.HasPos {
		t.Fatalf("缺包不应伪造位置，却得到 %s:%d:%d", le.File, le.Line, le.Col)
	}
	if strings.Contains(le.Error(), ".go:") {
		t.Fatalf("缺包错误不得伪造 .go 位置: %q", le.Error())
	}
	if le.ImportPath != "example.com/m/nosuchpkg" {
		t.Fatalf("归属包路径 = %q", le.ImportPath)
	}
}

func TestResolveViewModelLoadFailureKeepsCodeAndGXMLSpan(t *testing.T) {
	root, env := newModEnv(t)
	writeFile(t, filepath.Join(root, "bad", "bad.go"), "package bad\n\nfunc Broken( {\n")

	span := contracts.SourceSpan{Start: contracts.Position{Line: 7, Col: 11}, End: contracts.Position{Line: 7, Col: 30}}
	d := contracts.ViewModelDecl{Value: "bad.VM", Span: span}
	_, cerr := env.ResolveViewModel(d)
	if cerr == nil {
		t.Fatal("应报错")
	}
	if cerr.Code != CodeViewModelType {
		t.Fatalf("码 = %s, want %s", cerr.Code, CodeViewModelType)
	}
	// 归属：GXML 属性位置保持不变（准确指出是哪个 ViewModel 声明）。
	if cerr.Line != 7 || cerr.Col != 11 {
		t.Fatalf("GXML 位置 = %d:%d, want 7:11", cerr.Line, cerr.Col)
	}
	// 原因与可定位 Go 文件都要保留在消息里。
	if !strings.Contains(cerr.Message, "failed to load") || !strings.Contains(cerr.Message, "bad.go:") {
		t.Fatalf("消息应保留原因与 Go 文件位置: %q", cerr.Message)
	}
}

func TestResolveViewModelUnknownTypeNotLoadError(t *testing.T) {
	root, env := newModEnv(t)
	writeFile(t, filepath.Join(root, "good", "good.go"), "package good\n\ntype Good struct{}\n")

	_, cerr := env.ResolveViewModel(contracts.ViewModelDecl{Value: "good.Missing"})
	if cerr == nil {
		t.Fatal("未知类型应报错")
	}
	if cerr.Code != CodeViewModelType {
		t.Fatalf("码 = %s", cerr.Code)
	}
	if !strings.Contains(cerr.Message, "not found") {
		t.Fatalf("未知类型应报 not found 而非加载失败: %q", cerr.Message)
	}
}
