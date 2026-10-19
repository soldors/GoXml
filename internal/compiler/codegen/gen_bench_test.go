package codegen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
	"github.com/goxml/goxml/internal/compiler/expander"
	gxmlparser "github.com/goxml/goxml/internal/compiler/parser"
)

// ============================================================================
// A6b-2：`Generate` 生成器基准
//
// 重要边界：这里测的是**代码生成阶段本身**，不是完整编译流水线。
//
// 因此：
//   - 计时前先准备好合法的 `*analyzer.VerifiedDoc`（Parser/Expander/Analyzer
//     的开销不在计时范围内）；
//   - 循环内只调 `Generate`；
//   - 检查错误，避免测到「快速失败」的假数字；
//   - 输出写进 sink，防止被优化掉。
// ============================================================================

// genSink 防止编译器把 Generate 的返回值优化掉。
var genSink []byte

// benchSource 构造带 ViewModel 的完整 .gxml 源码。
func benchSource(body string) string {
	return `<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/">` + body + `</Page>
</Goxml>`
}

// mustVerifiedBench 为基准准备合法凭证。
//
// 复用与测试相同的管线，但计时前完成——Parser/Expander/Analyzer 的开销
// 不计入 Generate 基准。
func mustVerifiedBench(b *testing.B, body string) *analyzer.VerifiedDoc {
	b.Helper()
	p := runFrontendBench(b, benchSource(body))
	r := analyzer.Analyze("bench.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		b.Fatalf("Analyzer 未通过: %v", r.Diags)
	}
	v, err := r.Verified()
	if err != nil {
		b.Fatalf("取凭证失败: %v", err)
	}
	return v
}

// benchFrontend 是一次前端的产物（基准专用）。
type benchFrontend struct {
	doc     *contracts.DocumentIR
	schemas expander.SchemaRegistry
	env     *analyzer.TypeEnv
}

// runFrontendBench 为基准跑 Parser → Expander。
func runFrontendBench(b *testing.B, src string) benchFrontend {
	b.Helper()
	root := repoRootBench(b)
	env, err := analyzer.NewTypeEnv(analyzer.Config{
		ModuleRoot: root,
		SourceDir:  filepath.Join(root, "internal", "compiler", "codegen"),
	})
	if err != nil {
		b.Fatalf("TypeEnv: %v", err)
	}
	doc, pdiags := gxmlparser.Parse("bench.gxml", []byte(src))
	if len(pdiags) > 0 {
		b.Fatalf("Parser: %s", pdiags[0].Error())
	}
	er := expander.Expand("bench.gxml", doc)
	if len(er.Diags) > 0 {
		b.Fatalf("Expander: %s", er.Diags[0].Error())
	}
	return benchFrontend{doc: er.Doc, schemas: er.Schemas, env: env}
}

// repoRootBench 向上定位模块根。
func repoRootBench(b *testing.B) string {
	b.Helper()
	dir, err := os.Getwd()
	if err != nil {
		b.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			b.Fatal("未找到 go.mod")
		}
		dir = parent
	}
}

// runGenBench 是单场景基准的公共实现。
func runGenBench(b *testing.B, body string) {
	b.Helper()
	v := mustVerifiedBench(b, body)
	opts := defaultOpts()

	// 预热 + 合法性检查：若 Generate 有错，基准会明确失败而不是给出假数字。
	out, err := Generate(v, opts)
	if err != nil {
		b.Fatalf("预热失败: %v", err)
	}
	if len(out) == 0 {
		b.Fatal("预热输出为空")
	}
	// 记录产物大小，作为附加信息。
	b.Logf("产物 %d 字节", len(out))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := Generate(v, opts)
		if err != nil {
			b.Fatalf("第 %d 次生成失败: %v", i, err)
		}
		if len(out) == 0 {
			b.Fatalf("第 %d 次输出为空", i)
		}
		genSink = out
	}
}

// BenchmarkGenerateSingleText 测最小输入。
func BenchmarkGenerateSingleText(b *testing.B) {
	runGenBench(b, `<Text Value="Hello"/>`)
}

// BenchmarkGenerateColumn3 测多子节点输入（与运行时基准同结构）。
func BenchmarkGenerateColumn3(b *testing.B) {
	runGenBench(b, `<Column><Text Value="a"/><Text Value="b"/><Text Value="c"/></Column>`)
}

// BenchmarkGenerateNested 测嵌套输入。
func BenchmarkGenerateNested(b *testing.B) {
	runGenBench(b, `<Column><Text Value="a"/><Row><Text Value="b"/><Text Value="c"/></Row></Column>`)
}
