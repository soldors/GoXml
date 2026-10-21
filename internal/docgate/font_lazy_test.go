package docgate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// 门禁：异步路径不得触碰渲染线程的 sha(p)er（TASK-FONT-001-D 规则 R2）
//
// 为什么必须是**结构**门禁，而不是运行时测试：
//
// 跨 goroutine 共用一个 shaper 的后果是 Go 运行时 fatal error
// （`concurrent map …`，recover 无效，见 D 卡 §4.3.2 的 6/6 实测）。
// 而且它是**偶发**的——可能几千次渲染才炸一次。靠运行时用例抓这种错误，
// 只能是"这次没炸"，不是"不会炸"。
//
// 因此判据前移到这里：**异步加载路径 `lazy.go` 里不得出现任何 `.Shaper`
// 选择子**。构造主题需要给 Shaper 赋值，那不是异步路径的事——它被放在
// `theme_build.go`（同步构造，产出**新**主题对象）。
//
// 语言层面的比喻：让后台线程"根本不认识 sha(p)er 这个词"，
// 比让它"认识但承诺不乱动"可靠。
// ============================================================================

// asyncPathFile 是被门禁约束的异步路径文件（相对仓库根）。
const asyncPathFile = "pkg/runtime/theme/lazy.go"

// hasSelector 判定文件里是否出现 `<任意表达式>.Name` 形式的选择子。
//
// 用 AST 而不是字符串匹配：注释里写 "Shaper" 是合法的（本包的注释就要解释
// 为什么不碰它），字符串匹配会误报，进而逼人删掉有价值的注释。
func hasSelector(f *ast.File, name string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok && sel.Sel != nil && sel.Sel.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestAsyncPathNoRenderShaper 异步加载路径不得访问 Sha(p)er 字段。
func TestAsyncPathNoRenderShaper(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, filepath.FromSlash(asyncPathFile))

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("找不到异步路径文件 %s：D-IMPL-1 尚未实现时本门禁无法成立，"+
			"但**不得**以跳过代替（跳过等于没有门禁）: %v", asyncPathFile, err)
	}
	f := parseGoFile(t, path)

	if hasSelector(f, "Shaper") {
		t.Errorf("%s 出现了 `.Shaper` 选择子。异步路径只允许探测/解析/构造新主题，"+
			"触碰 sha(p)er 会与渲染线程竞态（跨 goroutine 共用是 Go 运行时 fatal，"+
			"recover 无效）。构造主题请放在 theme_build.go，通过 Take() 交给渲染线程",
			asyncPathFile)
	}

	t.Run("自检：检测器能识别真实违规", func(t *testing.T) {
		// 反向验证（与人工反向验证互补）：一段确实访问 .Shaper 的源码
		// 必须被判为存在，否则上面那条断言恒真——等于没有门禁。
		src := "package x\n\ntype T struct{ Shaper int }\n\nfunc f(th *T) { th.Shaper = 1 }\n"
		bad, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !hasSelector(bad, "Shaper") {
			t.Error("检测器漏掉了真实的 `.Shaper` 赋值——上一条断言是空的")
		}
	})

	t.Run("自检：构造函数名不算违规", func(t *testing.T) {
		// `text.NewShaper(...)` 是构造调用，不是字段访问。名字里带 Shaper
		// 不代表违规——否则门禁会逼人给正常的构造函数改名。
		src := "package x\n\nimport \"gioui.org/text\"\n\nfunc f() { _ = text.NewShaper() }\n"
		ok, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if hasSelector(ok, "Shaper") {
			t.Error("检测器把 text.NewShaper 误判为字段访问——判据过宽，会误报")
		}
	})
}
