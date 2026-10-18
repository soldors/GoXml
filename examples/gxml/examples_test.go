// Package examples 验证 examples/gxml 下的示例文件与当前编译器一致。
//
// 目的：防止规范、示例与实现三者脱节。
//
//   - valid/*.gxml   必须无任何诊断，且能走完 Parser + Expander。
//   - invalid/*.gxml 必须以期望的诊断码失败；期望码来自文件名前缀，
//     例如 attr003-grid-child-width.gxml 期望 GXML-ATTR-003。
//
// 任何一例失败都说明「示例展示的语法」与「编译器接受/拒绝的语法」不一致，
// 需要先决定是改实现还是改示例，不能放任漂移。
package examples

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// root 是示例文件所在目录（go test 以包目录为工作目录）。
const root = "."

// codeFromName 从文件名前缀解出期望诊断码。
//
// attr003-foo.gxml          -> GXML-ATTR-003
// def007-foo.gxml           -> GXML-DEF-007
func codeFromName(name string) (string, bool) {
	base := strings.TrimSuffix(name, ".gxml")
	dash := strings.Index(base, "-")
	if dash <= 0 {
		return "", false
	}
	prefix := base[:dash] // 形如 attr003
	i := 0
	for i < len(prefix) && (prefix[i] < '0' || prefix[i] > '9') {
		i++
	}
	if i == 0 || i == len(prefix) {
		return "", false
	}
	group := strings.ToUpper(prefix[:i])
	digits := prefix[i:]
	if len(digits) != 3 {
		return "", false
	}
	// 组名映射到规范前缀。
	known := map[string]string{
		"ATTR": "GXML-ATTR", "DEF": "GXML-DEF", "EVENT": "GXML-EVENT",
		"SCOPE": "GXML-SCOPE", "SYNTAX": "GXML-SYNTAX", "ROOT": "GXML-ROOT",
		"BIND": "GXML-BIND",
	}
	full, ok := known[group]
	if !ok {
		return "", false
	}
	return full + "-" + digits, true
}

// readAll 读取目录下的 .gxml 文件。
func readAll(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录 %s 失败: %v", dir, err)
	}
	out := make(map[string][]byte)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gxml") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", p, err)
		}
		out[e.Name()] = b
	}
	return out
}

// ---------------------------------------------------------------------------
// 合法示例
// ---------------------------------------------------------------------------

func TestValidExamplesCompile(t *testing.T) {
	files := readAll(t, filepath.Join(root, "valid"))
	if len(files) == 0 {
		t.Fatal("valid 目录下没有示例文件")
	}

	for name, src := range files {
		t.Run(name, func(t *testing.T) {
			doc, diags := parser.Parse(name, src)
			if len(diags) > 0 {
				for _, d := range diags {
					t.Errorf("Parser 诊断: %s", d.Error())
				}
				return
			}
			if doc == nil {
				t.Fatal("Parser 返回 nil 文档")
			}

			r := expander.Expand(name, doc)
			if len(r.Diags) > 0 {
				for _, d := range r.Diags {
					t.Errorf("Expander 诊断: %s", d.Error())
				}
				return
			}
			if r.Doc == nil || r.Doc.Page == nil {
				t.Fatal("展开后文档不完整")
			}
		})
	}
}

// TestValidExamplesHavePage 确认每个合法示例都有页面（结构完整性）。
func TestValidExamplesHavePage(t *testing.T) {
	for name, src := range readAll(t, filepath.Join(root, "valid")) {
		doc, diags := parser.Parse(name, src)
		if len(diags) > 0 {
			t.Errorf("%s 解析失败", name)
			continue
		}
		if doc.Page == nil {
			t.Errorf("%s 缺少 <Page>", name)
		}
	}
}

// ---------------------------------------------------------------------------
// 诊断示例
// ---------------------------------------------------------------------------

func TestInvalidExamplesProduceExpectedCode(t *testing.T) {
	files := readAll(t, filepath.Join(root, "invalid"))
	if len(files) == 0 {
		t.Fatal("invalid 目录下没有示例文件")
	}

	for name, src := range files {
		want, ok := codeFromName(name)
		if !ok {
			t.Errorf("%s: 文件名未编码期望诊断码（应形如 attr003-xxx.gxml）", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			doc, pdiags := parser.Parse(name, src)

			// 收集 Parser 与 Expander 的全部诊断——错误可能由任一层报告。
			codes := make(map[string]bool)
			for _, d := range pdiags {
				codes[d.Code] = true
			}
			if doc != nil {
				for _, d := range expander.Expand(name, doc).Diags {
					codes[d.Code] = true
				}
			}

			if !codes[want] {
				t.Errorf("期望诊断 %s，实际得到 %v", want, keys(codes))
			}
		})
	}
}

// TestInvalidExamplesDoNotSucceed 确认每个诊断示例确实失败。
func TestInvalidExamplesDoNotSucceed(t *testing.T) {
	for name, src := range readAll(t, filepath.Join(root, "invalid")) {
		doc, pdiags := parser.Parse(name, src)
		total := len(pdiags)
		if doc != nil {
			total += len(expander.Expand(name, doc).Diags)
		}
		if total == 0 {
			t.Errorf("%s 位于 invalid/ 却解析成功——示例与实现不一致", name)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// 目录约定自检
// ---------------------------------------------------------------------------

func TestExampleCounts(t *testing.T) {
	valid := readAll(t, filepath.Join(root, "valid"))
	invalid := readAll(t, filepath.Join(root, "invalid"))
	if len(valid) < 5 {
		t.Errorf("合法示例应覆盖布局/事件/Grid/尺寸/组件，当前仅 %d 个", len(valid))
	}
	if len(invalid) < 5 {
		t.Errorf("诊断示例应覆盖多种错误，当前仅 %d 个", len(invalid))
	}
}

// TestEveryInvalidExampleNameIsParseable 确保文件名编码规则本身可用。
func TestEveryInvalidExampleNameIsParseable(t *testing.T) {
	for name := range readAll(t, filepath.Join(root, "invalid")) {
		if _, ok := codeFromName(name); !ok {
			t.Errorf("%s 的期望码无法从文件名解出", name)
		}
	}
}

func TestCodeFromName(t *testing.T) {
	cases := map[string]string{
		"attr003-grid-child-width.gxml": "GXML-ATTR-003",
		"def007-unknown-prop.gxml":      "GXML-DEF-007",
		"event002-bad-syntax.gxml":      "GXML-EVENT-002",
		"scope003-missing-key.gxml":     "GXML-SCOPE-003",
	}
	for in, want := range cases {
		got, ok := codeFromName(in)
		if !ok || got != want {
			t.Errorf("codeFromName(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	// 不合规的名字应被拒绝。
	for _, bad := range []string{"foo.gxml", "attr.gxml", "attr00-x.gxml", "zzz003-x.gxml"} {
		if _, ok := codeFromName(bad); ok {
			t.Errorf("codeFromName(%q) 应失败", bad)
		}
	}
}
