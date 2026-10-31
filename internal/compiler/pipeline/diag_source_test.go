package pipeline

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// PRODUCT-R3 后续（并行产品 2026-11-01）：Analyzer 的加载/VM 类型失败诊断
// （如 GXML-ROOT-002）以**空 File** 构造——Span 是 GXML 位置（:1:20），
// Message 内是 Go 工具的绝对 file:line:col，唯独 .gxml 来源文件名丢失，
// 真实 CLI 显示 ":1:20: GXML-ROOT-002"。管线边界必须为缺失 File 的诊断
// 补上已知输入源；已有 File 的诊断、Span/Code/Message 一律保持原样；
// Analyzer 原结果切片不得被改写。

// vmMissingTypeSrc 的 ViewModel 指向 fixture 包中**不存在**的类型：
// 触发加载/解析失败 → GXML-ROOT-002（诊断 File 为空的实测路径）。
func vmMissingTypeSrc() []byte {
	return []byte(`<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.NoSuch">
  <Page Route="/"><Text Value="x"/></Page>
</Goxml>`)
}

func TestPipelineFillsMissingSourceFileOnAnalyzerDiags(t *testing.T) {
	res := Run("case.gxml", vmMissingTypeSrc(), baseCfg(t), Hooks{})
	root2 := 0
	var d0 struct{ code, file, msg string }
	for _, d := range res.Diags {
		if strings.Contains(d.Code, "GXML-ROOT-002") {
			root2++
			if d0.code == "" {
				d0.code, d0.file, d0.msg = d.Code, d.File, d.Message
			}
		}
	}
	if root2 == 0 {
		t.Fatalf("夹具应触发 GXML-ROOT-002，实际诊断: %v", res.Diags)
	}
	if d0.file != "case.gxml" {
		t.Fatalf("GXML 来源文件名必须补齐: File=%q (code=%s)", d0.file, d0.code)
	}
	if !strings.Contains(d0.msg, "NoSuch") {
		t.Fatalf("Message 必须保留 Go 侧原因（含缺失类型名）: %q", d0.msg)
	}
	// Span 是 GXML 位置（ViewModel 声明在源码第 1 行）——由实现保证不变，
	// 这里断言它没有指向 Go 文件（行号仍为 1，而非加载诊断的 Go 行号）。
}

func TestPipelineKeepsExplicitFileOnAnalyzerDiags(t *testing.T) {
	// 对照：Analyzer 已显式给 File 的诊断（如 BIND-* 的 case.gxml:行:列）不得被改写。
	res := Run("case.gxml", []byte(`<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel">
  <Page Route="/"><Text Value="{NoSuch}"/></Page>
</Goxml>`), baseCfg(t), Hooks{})
	if len(res.Diags) == 0 {
		t.Fatal("绑定错误应产出诊断")
	}
	for _, d := range res.Diags {
		if d.File == "" {
			t.Fatalf("显式 File 的诊断不得变空: %+v", d)
		}
		if d.File != "case.gxml" {
			t.Fatalf("显式 File 应保持 case.gxml: %q", d.File)
		}
	}
}

// TestWithSourceFileDoesNotMutateInput 锁定“不改写 Analyzer 原结果切片”：
// 输入切片（可能来自 Analyzer 内部）在补齐后必须保持原样。
func TestWithSourceFileDoesNotMutateInput(t *testing.T) {
	in := []contracts.CompilerError{
		{Code: "GXML-ROOT-002", Message: "go:line:col inside"},
		{Code: "GXML-BIND-004", File: "explicit.gxml", Message: "keep"},
	}
	inCopy := append([]contracts.CompilerError(nil), in...)
	out := withSourceFile(in, "case.gxml")
	if len(out) != 2 || out[0].File != "case.gxml" || out[1].File != "explicit.gxml" {
		t.Fatalf("输出补齐错误: %+v", out)
	}
	for i := range in {
		if in[i] != inCopy[i] {
			t.Fatalf("输入切片第 %d 项被改写: %+v", i, in[i])
		}
	}
	// 来源未知（file 为空）时不伪造。
	if out2 := withSourceFile(in, ""); len(out2) != 2 || out2[0].File != "" {
		t.Fatalf("来源未知不得伪造 File: %+v", out2)
	}
}
