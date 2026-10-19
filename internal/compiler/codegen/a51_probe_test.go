package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// A5.1 —— 录制行为测量（只测量，不改生产代码）
//
// 为什么需要多个指标：三件事互不等价，不能互相替代。
//
//	① 缓存录制分支进入次数    —— 我们生成的 Layout 里 `cached == false` 的次数
//	② 子节点布局函数执行次数  —— 实际调用了多少次 material.Body1(...).Layout
//	③ Flex 内部录制次数      —— Gio 对每个 Rigid 子节点做的 op.Record
//
// ②与③由 Gio 机制决定，我们无法直接插桩；但可以通过
// 「把布局函数替换成计数器」在**生成的代码之外**观测 ②。
//
// 做法：不改生成器，而是在临时模块里手写等价形态的 View，
// 用同一套缓存骨架 + 带计数器的叶子节点，测量真实行为。
// ============================================================================

// runInModule 在临时模块中运行给定源码与测试。
func runInModule(t *testing.T, files map[string]string) string {
	t.Helper()
	repoRoot := repoRootOf(t)
	dir := t.TempDir()

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
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
	return string(out) + "\n[err=" + errString(err) + "]"
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

// probePrelude 是探测模块共用的骨架：与生成代码等价的缓存结构。
const probePrelude = `package demo

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

type renderKey struct {
	constraints layout.Constraints
	metric      unit.Metric
	theme       *material.Theme
	locale      system.Locale
}

func (k renderKey) equal(o renderKey) bool {
	return k.constraints == o.constraints &&
		math.Float32bits(k.metric.PxPerDp) == math.Float32bits(o.metric.PxPerDp) &&
		math.Float32bits(k.metric.PxPerSp) == math.Float32bits(o.metric.PxPerSp) &&
		k.theme == o.theme &&
		k.locale == o.locale
}

func testGtx(ops *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}

// counters 是三类独立指标。
type counters struct {
	recordBranch int // 进入录制分支的次数
	leafLayout   int // 叶子节点布局函数执行次数
}

var ctr counters

// leaf 是带计数的叶子节点，方法体与生成的 Text 等价。
func leaf(th *material.Theme, s string) func(layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		ctr.leafLayout++
		return material.Body1(th, s).Layout(gtx)
	}
}
`

// 场景 1：单节点（A4 基线复现）
func TestA51SingleNode(t *testing.T) {
	probe := probePrelude + `
type view struct {
	cacheOps  op.Ops
	cacheCall op.CallOp
	cacheDims layout.Dimensions
	cacheKey  renderKey
	cached    bool
}

func (v *view) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	k := renderKey{constraints: gtx.Constraints, metric: gtx.Metric, theme: th, locale: gtx.Locale}
	if v.cached && v.cacheKey.equal(k) {
		v.cacheCall.Add(gtx.Ops)
		return v.cacheDims
	}
	ctr.recordBranch++
	v.cacheOps.Reset()
	m := op.Record(&v.cacheOps)
	rec := gtx
	rec.Ops = &v.cacheOps
	v.cacheDims = layout.Flex{Axis: layout.Vertical}.Layout(rec,
		layout.Rigid(leaf(th, "hello")))
	v.cacheCall = m.Stop()
	v.cacheKey = k
	v.cached = true
	v.cacheCall.Add(gtx.Ops)
	return v.cacheDims
}

func TestProbeSingle(t *testing.T) {
	ctr = counters{}
	th := material.NewTheme()
	v := &view{}

	var f1 op.Ops
	v.Layout(testGtx(&f1, 200, 100), th)
	first := ctr

	var f2, f3 op.Ops
	v.Layout(testGtx(&f2, 200, 100), th)
	v.Layout(testGtx(&f3, 200, 100), th)
	after := ctr

	t.Logf("首帧:     录制分支=%d 叶子布局=%d", first.recordBranch, first.leafLayout)
	t.Logf("三帧累计: 录制分支=%d 叶子布局=%d", after.recordBranch, after.leafLayout)

	if after.recordBranch != 1 {
		t.Errorf("录制分支 = %d, 期望 1（后两帧应回放）", after.recordBranch)
	}
	if after.leafLayout != 1 {
		t.Errorf("叶子布局 = %d, 期望 1（回放不应重跑布局函数）", after.leafLayout)
	}
}
`
	out := runInModule(t, map[string]string{"probe_test.go": probe})
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "录制分支") || strings.Contains(line, "FAIL") || strings.Contains(line, "PASS") {
			t.Log(strings.TrimSpace(line))
		}
	}
}

// 场景 2：Column + 3 静态子节点
func TestA51ColumnThreeChildren(t *testing.T) {
	probe := probePrelude + `
type view struct {
	cacheOps  op.Ops
	cacheCall op.CallOp
	cacheDims layout.Dimensions
	cacheKey  renderKey
	cached    bool
}

func (v *view) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	k := renderKey{constraints: gtx.Constraints, metric: gtx.Metric, theme: th, locale: gtx.Locale}
	if v.cached && v.cacheKey.equal(k) {
		v.cacheCall.Add(gtx.Ops)
		return v.cacheDims
	}
	ctr.recordBranch++
	v.cacheOps.Reset()
	m := op.Record(&v.cacheOps)
	rec := gtx
	rec.Ops = &v.cacheOps
	v.cacheDims = layout.Flex{Axis: layout.Vertical}.Layout(rec,
		layout.Rigid(leaf(th, "a")),
		layout.Rigid(leaf(th, "b")),
		layout.Rigid(leaf(th, "c")))
	v.cacheCall = m.Stop()
	v.cacheKey = k
	v.cached = true
	v.cacheCall.Add(gtx.Ops)
	return v.cacheDims
}

func TestProbeColumn(t *testing.T) {
	ctr = counters{}
	th := material.NewTheme()
	v := &view{}

	var f1 op.Ops
	v.Layout(testGtx(&f1, 200, 300), th)
	first := ctr

	var f2, f3 op.Ops
	v.Layout(testGtx(&f2, 200, 300), th)
	v.Layout(testGtx(&f3, 200, 300), th)
	after := ctr

	t.Logf("首帧:     录制分支=%d 叶子布局=%d", first.recordBranch, first.leafLayout)
	t.Logf("三帧累计: 录制分支=%d 叶子布局=%d", after.recordBranch, after.leafLayout)

	if after.recordBranch != 1 {
		t.Errorf("录制分支 = %d, 期望 1", after.recordBranch)
	}
	if after.leafLayout != 3 {
		t.Errorf("叶子布局 = %d, 期望 3（3 个子节点各一次）", after.leafLayout)
	}
}
`
	out := runInModule(t, map[string]string{"probe_test.go": probe})
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "录制分支") || strings.Contains(line, "FAIL") || strings.Contains(line, "PASS") {
			t.Log(strings.TrimSpace(line))
		}
	}
}

// 场景 3：Column 套 Row，全静态
func TestA51NestedStatic(t *testing.T) {
	probe := probePrelude + `
type view struct {
	cacheOps  op.Ops
	cacheCall op.CallOp
	cacheDims layout.Dimensions
	cacheKey  renderKey
	cached    bool
}

func (v *view) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	k := renderKey{constraints: gtx.Constraints, metric: gtx.Metric, theme: th, locale: gtx.Locale}
	if v.cached && v.cacheKey.equal(k) {
		v.cacheCall.Add(gtx.Ops)
		return v.cacheDims
	}
	ctr.recordBranch++
	v.cacheOps.Reset()
	m := op.Record(&v.cacheOps)
	rec := gtx
	rec.Ops = &v.cacheOps
	v.cacheDims = layout.Flex{Axis: layout.Vertical}.Layout(rec,
		layout.Rigid(leaf(th, "a")),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(leaf(th, "b")),
				layout.Rigid(leaf(th, "c")))
		}))
	v.cacheCall = m.Stop()
	v.cacheKey = k
	v.cached = true
	v.cacheCall.Add(gtx.Ops)
	return v.cacheDims
}

func TestProbeNested(t *testing.T) {
	ctr = counters{}
	th := material.NewTheme()
	v := &view{}

	var f1 op.Ops
	v.Layout(testGtx(&f1, 300, 200), th)
	first := ctr

	var f2, f3 op.Ops
	v.Layout(testGtx(&f2, 300, 200), th)
	v.Layout(testGtx(&f3, 300, 200), th)
	after := ctr

	t.Logf("首帧:     录制分支=%d 叶子布局=%d", first.recordBranch, first.leafLayout)
	t.Logf("三帧累计: 录制分支=%d 叶子布局=%d", after.recordBranch, after.leafLayout)

	if after.recordBranch != 1 {
		t.Errorf("录制分支 = %d, 期望 1", after.recordBranch)
	}
	// 3 个叶子：a、b、c
	if after.leafLayout != 3 {
		t.Errorf("叶子布局 = %d, 期望 3", after.leafLayout)
	}
}
`
	out := runInModule(t, map[string]string{"probe_test.go": probe})
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "录制分支") || strings.Contains(line, "FAIL") || strings.Contains(line, "PASS") {
			t.Log(strings.TrimSpace(line))
		}
	}
}

// 场景 4：key 变化后的录制次数
func TestA51KeyChangeRecordCount(t *testing.T) {
	probe := probePrelude + `
type view struct {
	cacheOps  op.Ops
	cacheCall op.CallOp
	cacheDims layout.Dimensions
	cacheKey  renderKey
	cached    bool
}

func (v *view) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	k := renderKey{constraints: gtx.Constraints, metric: gtx.Metric, theme: th, locale: gtx.Locale}
	if v.cached && v.cacheKey.equal(k) {
		v.cacheCall.Add(gtx.Ops)
		return v.cacheDims
	}
	ctr.recordBranch++
	v.cacheOps.Reset()
	m := op.Record(&v.cacheOps)
	rec := gtx
	rec.Ops = &v.cacheOps
	v.cacheDims = layout.Flex{Axis: layout.Vertical}.Layout(rec,
		layout.Rigid(leaf(th, "a")),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(leaf(th, "b")))
		}))
	v.cacheCall = m.Stop()
	v.cacheKey = k
	v.cached = true
	v.cacheCall.Add(gtx.Ops)
	return v.cacheDims
}

func TestProbeKeyChange(t *testing.T) {
	ctr = counters{}
	th := material.NewTheme()
	v := &view{}

	// 同 key 三帧
	var f1, f2, f3 op.Ops
	v.Layout(testGtx(&f1, 300, 200), th)
	v.Layout(testGtx(&f2, 300, 200), th)
	v.Layout(testGtx(&f3, 300, 200), th)
	same := ctr

	// 改变约束一帧
	var f4 op.Ops
	v.Layout(testGtx(&f4, 100, 50), th)
	changed := ctr

	// 再回到原约束两帧
	var f5, f6 op.Ops
	v.Layout(testGtx(&f5, 300, 200), th)
	v.Layout(testGtx(&f6, 300, 200), th)
	back := ctr

	t.Logf("同 key 3 帧:   录制分支=%d 叶子布局=%d", same.recordBranch, same.leafLayout)
	t.Logf("约束变化 1 帧: 录制分支=%d 叶子布局=%d", changed.recordBranch, changed.leafLayout)
	t.Logf("回到原约束 2 帧: 录制分支=%d 叶子布局=%d", back.recordBranch, back.leafLayout)

	// 同 key 3 帧 = 1 次录制
	if same.recordBranch != 1 {
		t.Errorf("同 key 3 帧录制 = %d, 期望 1", same.recordBranch)
	}
	// 约束变化 = 第 2 次录制
	if changed.recordBranch != 2 {
		t.Errorf("约束变化后录制 = %d, 期望 2", changed.recordBranch)
	}
	// 回到原约束 = 第 3 次录制（不是回到旧缓存，§16 只保留最新一份）
	if back.recordBranch != 3 {
		t.Errorf("回到原约束后录制 = %d, 期望 3", back.recordBranch)
	}
	t.Log("每次 key 变化都重录；恢复旧 key 也会重录（只保留最新一份缓存）")
}
`
	out := runInModule(t, map[string]string{"probe_test.go": probe})
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "录制分支") || strings.Contains(line, "期望") || strings.Contains(line, "FAIL") || strings.Contains(line, "PASS") {
			t.Log(strings.TrimSpace(line))
		}
	}
}
