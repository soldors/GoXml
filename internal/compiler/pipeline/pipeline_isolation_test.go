package pipeline

import (
	"bytes"
	"strings"
	"testing"
)

// 本文件是 PRODUCT-R2-ZCODE 切片 3：同一进程内的失败/成功隔离与
// 跨源确定性。此前没有任何测试覆盖「先失败后成功」与「A→B→A 交替生成」，
// 包级状态泄漏（诊断残留、符号串写、缓存槽串扰）会在这类序列中暴露。

// badSrc 通过前端后触发 Codegen 的不支持组件错误（当前诊断无编号）。
func badSrc() []byte {
	return []byte(`<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/"><NoSuchWidget/></Page>
</Goxml>`)
}

// namedSrc 构造带独有文本标记的页面，用于交叉断言「各源只含自己的内容」。
func namedSrc(marker string) []byte {
	return []byte(`<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/"><Text Value="` + marker + `"/></Page>
</Goxml>`)
}

// TestPipelineFailThenSuccessIsolation 同进程序列：
// 失败 → 成功 → 失败 → 成功。每一步的诊断与产物都必须只反映当次输入；
// 失败不得污染后续成功，成功也不得被缓存进失败路径。
func TestPipelineFailThenSuccessIsolation(t *testing.T) {
	cfg := baseCfg(t)

	c := &counter{}
	bad := Run("dashboard.gxml", badSrc(), cfg, c.hook(t))
	if len(bad.Diags) == 0 {
		t.Fatal("坏源必须产出诊断")
	}
	if bad.Artifact != nil {
		t.Fatalf("坏源不得产出 Artifact: %+v", bad.Artifact)
	}
	if c.n != 1 || len(bad.Diags) != 1 || bad.Diags[0].Code != "" || !strings.Contains(bad.Diags[0].Message, "codegen: <NoSuchWidget> is not supported") {
		t.Fatalf("坏源应到达 Codegen 并报告既有不支持组件错误: calls=%d, diags=%v", c.n, bad.Diags)
	}

	good1 := Run("dashboard.gxml", okSrc(), cfg, Hooks{})
	if len(good1.Diags) != 0 {
		t.Fatalf("好源不应携带诊断（前次失败不得泄漏）: %v", good1.Diags)
	}
	if good1.Artifact == nil || len(good1.Artifact.Source) == 0 {
		t.Fatal("好源必须产出 Artifact")
	}

	bad2 := Run("dashboard.gxml", badSrc(), cfg, c.hook(t))
	if len(bad2.Diags) == 0 || bad2.Artifact != nil {
		t.Fatal("重复的坏源必须重复失败，不得被任何成功状态掩盖")
	}
	if c.n != 2 || len(bad2.Diags) != 1 || bad2.Diags[0].Message != bad.Diags[0].Message || bad2.Diags[0].Code != bad.Diags[0].Code {
		t.Fatalf("重复坏源应到达同一 Codegen 失败: calls=%d, diags=%v", c.n, bad2.Diags)
	}

	good2 := Run("dashboard.gxml", okSrc(), cfg, Hooks{})
	if len(good2.Diags) != 0 || good2.Artifact == nil {
		t.Fatal("第二次成功必须与第一次同等干净")
	}
	if !bytes.Equal(good1.Artifact.Source, good2.Artifact.Source) {
		t.Fatal("同源两次生成的字节必须一致")
	}
}

// TestPipelineAlternatingSourcesDeterministic 同进程交替生成两个不同源
// （A→B→A），断言：各自字节确定、互不串写（A 不含 B 的文本，反之亦然）。
// 符号（View/Key/Cache 类型）由 SourcePath 派生，交替时不得相互污染。
func TestPipelineAlternatingSourcesDeterministic(t *testing.T) {
	cfgA := baseCfg(t)
	cfgA.SourcePath = "alpha.gxml"
	cfgB := baseCfg(t)
	cfgB.SourcePath = "beta.gxml"

	run := func(cfg Config, marker string) *Result {
		return Run(cfg.SourcePath, namedSrc(marker), cfg, Hooks{})
	}

	a1 := run(cfgA, "alpha-marker")
	b1 := run(cfgB, "beta-marker")
	a2 := run(cfgA, "alpha-marker")

	if len(a1.Diags) != 0 || len(b1.Diags) != 0 || len(a2.Diags) != 0 {
		t.Fatalf("两个源都必须干净: %v / %v / %v", a1.Diags, b1.Diags, a2.Diags)
	}
	ab := a1.Artifact.Source
	bb := b1.Artifact.Source
	a2b := a2.Artifact.Source
	if !bytes.Contains(ab, []byte("alpha-marker")) || bytes.Contains(ab, []byte("beta-marker")) {
		t.Fatal("A 的产物只应包含 A 的文本")
	}
	if !bytes.Contains(bb, []byte("beta-marker")) || bytes.Contains(bb, []byte("alpha-marker")) {
		t.Fatal("B 的产物只应包含 B 的文本")
	}
	if !bytes.Equal(ab, a2b) {
		t.Fatal("A→B→A 后 A 的生成字节必须与首次一致")
	}
}
