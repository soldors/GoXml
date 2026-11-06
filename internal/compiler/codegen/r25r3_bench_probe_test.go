package codegen

// R25-R3 S3：generated 稳定 Layout 微基准（重建——R25-R2 声称的
// r25r2_bench_probe_test.go 未随冻结 ref 提交，原始数字 3687/1615 已
// 不可复跑，如实登记；本文件为**新证据**，非旧数字复用）。
//
// 口径：Row[Text("左"), Expanded[Button]]；预热 3 帧（构造+首帧+稳态）
// 在计时外；计时内仅 gtx.Reset + View.Layout（不含 Router 事件轮询——
// 与手写对照的 Ops 范围一致，见 r25r2_s3_bench_test.go 的手写形态）。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

func r25r3BenchProbe(t *testing.T, vmSrc, gxmlBody, benchBody, benchRegex string) string {
	t.Helper()
	repoRoot := repoRootOf(t)

	base := "tmprt" + randomSuffix()
	dir := filepath.Join(repoRoot, "internal", "compiler", "codegen", "testdata", base)
	if err := os.MkdirAll(filepath.Join(dir, "vm"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "vm", "vm.go"), []byte(vmSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	rel := "internal/compiler/codegen/testdata/" + base
	src := `<Goxml Version="1" ViewModel="` + rel + `/vm.VM">
  <Page Route="/">` + gxmlBody + `</Page>
</Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer 未通过: %v", r.Diags)
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("取凭证失败: %v", err)
	}
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bench_probe_test.go"),
		[]byte(fmt.Sprintf(mixedProbeHeaderTmpl, base)+benchBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-run", "^$",
		"-bench", benchRegex, "-benchtime", "30000x", "-count", "3",
		"./"+filepath.ToSlash(rel))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	bout, err := cmd.CombinedOutput()
	return string(bout) + "\n[err=" + rtErr(err) + "]"
}

// TestR25R3BenchGeneratedExpanded generated 稳定 Layout 微基准。
func TestR25R3BenchGeneratedExpanded(t *testing.T) {
	out := r25r3BenchProbe(t, r25r2SharedVM, `<Row Height="60">
  <Text Value="左"/>
  <Expanded><Button Text="点我" Click="TapA"/></Expanded>
</Row>`, `
var benchSink layout.Dimensions

func BenchmarkR25R3GenExpandedStableLayout(b *testing.B) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		b.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
	}
	frame()
	frame()
	frame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtx.Reset()
		benchSink = v.Layout(gtx, th)
	}
}
`, "BenchmarkR25R3GenExpandedStableLayout")
	t.Logf("BENCH OUTPUT:\n%s", out)
	if !strings.Contains(out, "[err=nil]") || strings.Contains(out, "FAIL") || !regexp.MustCompile(`(?m)^BenchmarkR25R3GenExpandedStableLayout-\d+\s+[1-9]\d*\s+[0-9.]+ ns/op\s+\d+ B/op\s+\d+ allocs/op`).MatchString(out) {
		t.Fatalf("基准未产出数字:\n%s", out)
	}
}
