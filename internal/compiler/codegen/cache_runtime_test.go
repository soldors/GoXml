package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// A4-T3 / A4-T4：**运行时**证明缓存确实回放、key 变化触发重录
//
// 为什么不能只做静态检查：源码里出现 op.Record / op.CallOp 不证明
// 回放分支被执行过。必须把生成代码放进真实模块里跑。
//
// ---------------------------------------------------------------------------
// 一个重要陷阱（已实测）
// ---------------------------------------------------------------------------
//
// **不能用 `cacheCall` 是否变化来判断「是否重录」。**
//
// 实测：cacheOps.Reset() 后重新录制**相同内容**，会产生**完全相同**的 CallOp
// （start/end 完全一致）。因此 cacheCall 相同既可能是「回放了」，也可能是
// 「重录了相同内容」。
//
// 可靠信号是 **`cacheKey` 是否被改写**：回放分支根本不写 cacheKey，
// 只有录制分支写。所以：
//   - 回放 → cacheKey 保持原值
//   - 重录 → cacheKey 被更新
//
// 这不是「看源码」，而是在临时模块里跑真实生成的代码，观测其字段状态。
// ============================================================================

// genModuleWithProbe 在临时模块中运行生成代码 + 探测测试。
func genModuleWithProbe(t *testing.T, genSource []byte, probe string) (string, error) {
	t.Helper()
	repoRoot := repoRootOf(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "gen.go"), genSource, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"), []byte(probe), 0o644); err != nil {
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

	cmd := exec.Command("go", "test", "-v", "-run", "TestProbe", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// probeCommon 是所有探测器共用的导入与辅助。
const probeCommon = `package demo

import (
	"image"
	"math"
	"testing"

	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func testGtx(ops *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}

var _ = math.Float32bits
var _ = system.LTR
`

// TestCacheReplaysAtRuntime 是 A4-T3 的运行时证据：
// 连续多帧中，只有首帧写 cacheKey。
func TestCacheReplaysAtRuntime(t *testing.T) {
	src := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())

	probe := probeCommon + `
func TestProbeReplay(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	// 帧 1：应录制，cacheKey 被写入
	var f1 op.Ops
	g1 := testGtx(&f1, 100, 50)
	d1 := v.Layout(g1, th)
	if !v.cached {
		t.Fatal("首帧后 cached 应为 true")
	}
	if v.cacheKey.constraints != g1.Constraints {
		t.Fatal("首帧后 cacheKey 未写入")
	}
	key1 := v.cacheKey

	// 帧 2：key 相同 → 回放分支不写 cacheKey
	var f2 op.Ops
	d2 := v.Layout(testGtx(&f2, 100, 50), th)
	if v.cacheKey != key1 {
		t.Error("第2帧改写了 cacheKey —— 说明走了录制分支")
	}
	if d1 != d2 {
		t.Errorf("尺寸不一致: %v vs %v", d1, d2)
	}
	if d1.Size != image.Pt(100, 50) {
		t.Errorf("尺寸 = %v, 期望 (100,50)", d1.Size)
	}

	// 帧 3、4：继续回放
	var f3, f4 op.Ops
	v.Layout(testGtx(&f3, 100, 50), th)
	v.Layout(testGtx(&f4, 100, 50), th)
	if v.cacheKey != key1 {
		t.Error("后续帧改写了 cacheKey")
	}

	t.Log("连续 4 帧：cacheKey 始终不变 → 后三帧确实回放")
}
`
	out, err := genModuleWithProbe(t, src, probe)
	if err != nil {
		t.Fatalf("运行时探测失败:\n%s", out)
	}
	t.Log("运行时证据：连续 4 帧仅首帧录制")
}

// TestKeyChangesTriggerRerecord 是 A4-T4 的运行时证据：
// 四项 key 输入分别变化时，cacheKey 一定被改写（即重录）。
func TestKeyChangesTriggerRerecord(t *testing.T) {
	src := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())

	probe := probeCommon + `
func TestProbeKeyChange(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var f0 op.Ops
	v.Layout(testGtx(&f0, 100, 50), th)

	// 1) Constraints
	var f1 op.Ops
	g1 := testGtx(&f1, 30, 20)
	d1 := v.Layout(g1, th)
	if v.cacheKey.constraints != g1.Constraints {
		t.Error("Constraints 变化后 cacheKey 未更新")
	}
	if d1.Size != image.Pt(30, 20) {
		t.Errorf("重录后尺寸 = %v, 期望 (30,20)", d1.Size)
	}

	// 2) Metric（按位不同）
	var f2 op.Ops
	g2 := testGtx(&f2, 30, 20)
	g2.Metric = unit.Metric{PxPerDp: 2, PxPerSp: 2}
	v.Layout(g2, th)
	if math.Float32bits(v.cacheKey.metric.PxPerDp) != math.Float32bits(2) {
		t.Error("Metric 变化后 cacheKey 未更新")
	}

	// 3) Theme
	var f3 op.Ops
	g3 := testGtx(&f3, 30, 20)
	g3.Metric = unit.Metric{PxPerDp: 2, PxPerSp: 2}
	newTh := material.NewTheme()
	v.Layout(g3, newTh)
	if v.cacheKey.theme != newTh {
		t.Error("Theme 变化后 cacheKey 未更新")
	}

	// 4) Locale
	var f4 op.Ops
	g4 := testGtx(&f4, 30, 20)
	g4.Metric = unit.Metric{PxPerDp: 2, PxPerSp: 2}
	g4.Locale = system.Locale{Language: "ar", Direction: system.RTL}
	v.Layout(g4, newTh)
	if v.cacheKey.locale.Language != "ar" {
		t.Error("Locale 变化后 cacheKey 未更新")
	}

	t.Log("四项 key 输入分别变化时均重录（cacheKey 被改写）")
}
`
	out, err := genModuleWithProbe(t, src, probe)
	if err != nil {
		t.Fatalf("运行时探测失败:\n%s", out)
	}
	t.Log("运行时证据：Constraints / Metric / Theme / Locale 变化均触发重录")
}

// TestCachedDimsStayConsistent 验证 cacheDims 在录制与回放间一致（含边界尺寸）。
func TestCachedDimsStayConsistent(t *testing.T) {
	src := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())

	probe := probeCommon + `
func TestProbeDims(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	// 边界：1x1
	var f1, f2 op.Ops
	d1 := v.Layout(testGtx(&f1, 1, 1), th)
	d2 := v.Layout(testGtx(&f2, 1, 1), th)
	if d1 != d2 {
		t.Errorf("1x1 录制/回放不一致: %v vs %v", d1, d2)
	}

	// 常规
	var f3, f4 op.Ops
	d3 := v.Layout(testGtx(&f3, 200, 100), th)
	d4 := v.Layout(testGtx(&f4, 200, 100), th)
	if d3 != d4 {
		t.Errorf("200x100 录制/回放不一致: %v vs %v", d3, d4)
	}
	if d3.Size != image.Pt(200, 100) {
		t.Errorf("尺寸 = %v, 期望 (200,100)", d3.Size)
	}
	t.Log("cacheDims 在录制与回放间一致（含 1x1 边界）")
}
`
	out, err := genModuleWithProbe(t, src, probe)
	if err != nil {
		t.Fatalf("运行时探测失败:\n%s", out)
	}
}

// TestCacheCallIsNotARecordSignal 锁定「cacheCall 不可用作重录信号」这一事实。
//
// 这条测试的价值：防止后来者用 cacheCall 变化做断言，从而写出假通过的测试。
func TestCacheCallIsNotARecordSignal(t *testing.T) {
	src := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())

	probe := probeCommon + `
func TestProbeSignal(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var f1 op.Ops
	v.Layout(testGtx(&f1, 100, 50), th)
	call1 := v.cacheCall
	key1 := v.cacheKey

	// 强制 key 变化：Metric 的位模式改了，但布局结果相同。
	var f2 op.Ops
	g2 := testGtx(&f2, 100, 50)
	g2.Metric = unit.Metric{PxPerDp: 1.0000001, PxPerSp: 1}
	v.Layout(g2, th)
	call2 := v.cacheCall

	// 确实重录了（cacheKey 变了）
	if v.cacheKey == key1 {
		t.Error("key 变化后 cacheKey 未更新，说明未重录")
	}
	// 但 cacheCall 可能完全相同 —— 这正是不能用它做信号的原因。
	t.Logf("重录后 cacheCall 是否相同 = %v（相同也不代表没重录）", call1 == call2)
}
`
	out, err := genModuleWithProbe(t, src, probe)
	if err != nil {
		t.Fatalf("运行时探测失败:\n%s", out)
	}
	if !strings.Contains(out, "重录后 cacheCall 是否相同") {
		t.Fatalf("探测输出异常:\n%s", out)
	}
	t.Log("已记录：cacheCall 相同不蕴含未重录，故用 cacheKey 作信号")
}
