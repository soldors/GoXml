package codegen

// R25-R1 F-B3 成本对照：Expanded 生成与等价手写形态。
// 边界：本基准测的是 **Generate（代码生成层）** 的稳定开销——parser 前端
// 预热不计入；离屏 Layout 已由 r25_expanded_matrix 的点击命中判据承担。
// 「等价手写 Gio 对照」：Expanded(子项) 的生成产物即
// layout.Flexed(1, func(gtx){...}) 手工装配形态；两者的差值=生成器
// 决策+格式化成本，不是渲染性能验收。

import (
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// BenchmarkR25GenerateExpanded candidateRef：含 Expanded 的页面生成成本。
func BenchmarkR25GenerateExpanded(b *testing.B) {
	v := mustVerifiedBench(b, `<Row Height="60"><Text Value="L"/><Expanded><Button Text="点我" Click="OnWrongArity"/></Expanded></Row>`)
	opts := defaultOpts()
	opts.ModulePath = "github.com/goxml/goxml" // fixture VM 为跨包 ViewModel（B1 场景）
	out, err := Generate(v, opts)
	if err != nil {
		b.Fatalf("预热失败: %v", err)
	}
	if len(out) == 0 {
		b.Fatal("预热输出为空")
	}
	b.Logf("产物 %d 字节", len(out))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := Generate(v, opts)
		if err != nil {
			b.Fatal(err)
		}
		genSink = out
	}
}

// BenchmarkR25GeneratePlain 无 Expanded 的等价 Row（页宽相同）——两者差值
// 即 Expanded 支持本身的增量成本。
func BenchmarkR25GeneratePlain(b *testing.B) {
	v := mustVerifiedBench(b, `<Row Height="60"><Text Value="L"/></Row>`)
	opts := defaultOpts()
	out, err := Generate(v, opts)
	if err != nil {
		b.Fatalf("预热失败: %v", err)
	}
	if len(out) == 0 {
		b.Fatal("预热输出为空")
	}
	b.Logf("产物 %d 字节", len(out))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := Generate(v, opts)
		if err != nil {
			b.Fatal(err)
		}
		genSink = out
	}
}

var _ = analyzer.StatusOK
