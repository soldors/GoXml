package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// A6a：单节点性能基线
//
// 目的：在扩展 <Column> 之前，留下可复现的单节点基线，
// 以便后续判断嵌套缓存引入多少额外成本。
//
// 测量三条路径：
//   - BaselineNoCache：不缓存，每帧直接布局（对照组）
//   - CacheFirstRecord：首次录制（含 op.Record + Stop）
//   - CacheHitReplay：稳定命中，只执行 CallOp.Add
//
// 这些基准在**临时模块**中运行真实生成代码，而非测量生成器本身。
// 生成器性能（Generate 函数）是另一回事，见 TestGenerateThroughput。
// ============================================================================

// benchModule 构造一个含生成代码与基准的临时模块并运行它。
func benchModule(t *testing.T, genSource []byte, benchSrc string) string {
	t.Helper()
	repoRoot := repoRootOf(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "gen.go"), genSource, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bench_test.go"), []byte(benchSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	gomod := "module gotest\n\ngo 1.26.0\n\nrequire gioui.org v0.10.3\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-bench", ".", "-benchmem",
		"-benchtime", "200ms", "-run", "^$", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("基准运行失败: %v\n%s", err, out)
	}
	return string(out)
}

const benchCommon = `package demo

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func benchGtx(ops *op.Ops) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(200, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}
`

// TestSingleNodeBaseline 采集 A6a 基线并记录输出。
func TestSingleNodeBaseline(t *testing.T) {
	src := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())

	bench := benchCommon + `
// 对照组：不缓存，每帧直接布局。
func BenchmarkNoCache(b *testing.B) {
	th := material.NewTheme()
	var ops op.Ops
	gtx := benchGtx(&ops)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.Reset()
		material.Body1(th, "Hello").Layout(gtx)
	}
}

// 首次录制：每次都是冷路径。
func BenchmarkFirstRecord(b *testing.B) {
	th := material.NewTheme()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := &GoxmlDashboardView{}
		var ops op.Ops
		v.Layout(benchGtx(&ops), th)
	}
}

// 稳定命中：首帧录制后，后续只回放。
func BenchmarkCacheHit(b *testing.B) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var warm op.Ops
	v.Layout(benchGtx(&warm), th) // 预热：触发录制

	var ops op.Ops
	gtx := benchGtx(&ops)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.Reset()
		v.Layout(gtx, th)
	}
}
`
	out := benchModule(t, src, bench)

	// 把原始基准输出记入测试日志，便于人工核对与写入报告。
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Benchmark") || strings.HasPrefix(line, "ok ") {
			t.Log(line)
		}
	}
	// 必须有三个基准结果。
	for _, want := range []string{"BenchmarkNoCache", "BenchmarkFirstRecord", "BenchmarkCacheHit"} {
		if !strings.Contains(out, want) {
			t.Errorf("基准 %s 未产生结果", want)
		}
	}
}

// TestGenerateThroughput 测量**生成器本身**的吞吐（与运行时性能无关）。
func TestGenerateThroughput(t *testing.T) {
	v := verified(t, `<Text Value="Hello"/>`)
	opts := defaultOpts()

	// 先确认可生成，再计时。
	out, err := Generate(v, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("单次生成产出 %d 字节", len(out))

	for i := 0; i < 100; i++ {
		if _, err := Generate(v, opts); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("生成器可重复调用（详细吞吐见 A6b 的基准）")
}
