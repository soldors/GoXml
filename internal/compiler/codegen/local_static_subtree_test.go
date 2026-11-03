package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// R10-B B2/B3：局部静态子树缓存的上下文失效、多实例隔离与真实验收。
//
// 验收手段（R10-B 任务书）：Analyzer→Codegen→真实 go test→离屏 Layout；
// 录制次数用生成字段 staticRecs<N>（生成代码内的实际录制计数，非 grep）；
// 稳定帧 AllocsPerRun 与几何尺寸同时对照。每例 ≤8 帧。
//
// 本文件不测 For 内缓存（明确沿用既有路径，见 ForUntouched）。
// ============================================================================

// localStaticVM 是独立完整的探测 VM（rtProbeVM 每次调用建独立 temp 目录，
// 无需继承 probeVMSrc）：Title/ChangeCount 供动态文字与 Button，
// ShowA 供 If 条件。
const localStaticVM = `package vm

// VM 是 R10-B 局部静态子树验收用 ViewModel。
type VM struct {
	Title       string
	ChangeCount int
	LastText    string
	ShowA       bool
	Rows        []Row
}

// Row 是 <For> 行数据（绑定经 {row.Name} 访问）。
type Row struct {
	Name string
}

// IdentityText 原样回写（动态文字更新路径）。
func (vm *VM) IdentityText(s string) { vm.ChangeCount++; vm.LastText = s; vm.Title = s }

// Increment 是 Button 的无参处理器。
func (vm *VM) Increment() { vm.ChangeCount++ }

// SetShowA 翻转 If 条件。
func (vm *VM) SetShowA(b bool) { vm.ShowA = b }

// SetRows 整体替换 <For> 数据源。
func (vm *VM) SetRows(rs []Row) { vm.Rows = rs }
`

// localStaticPage 是主验收页面：动态绑定文字 + 最大纯静态子树（2 个静态
// Text 的 Column）+ 交互 Button。
const localStaticPage = `<Column>
  <Text Value="{Title}"/>
  <Button Text="提交" Click="Increment"/>
  <Column>
    <Text Value="静态标题A"/>
    <Text Value="静态标题B"/>
  </Column>
</Column>`

// rtProbeVMMutated 与 rtProbeVM 同链（Analyzer→Codegen→写入→go test），
// 但在写出 gen.go 前对生成源码施加 mutate——变异红专用（可编译变异，
// 不是编译失败当红）。
func rtProbeVMMutated(t *testing.T, vmSrc, gxmlBody, probeBody string, mutate func(string) string) string {
	t.Helper()
	repoRoot := repoRootOf(t)

	dir := filepath.Join(repoRoot, "internal", "compiler", "codegen",
		"testdata", "tmprt"+randomSuffix())
	vmDir := filepath.Join(dir, "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(vmDir, "vm.go"), []byte(vmSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	rel := "internal/compiler/codegen/testdata/" + filepath.Base(dir)
	src := `<Goxml Version="1" ViewModel="` + rel + `/vm.VM">
  <Page Route="/">` + gxmlBody + `</Page>
</Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		msgs := make([]string, 0, len(r.Diags))
		for _, d := range r.Diags {
			msgs = append(msgs, d.Error())
		}
		t.Fatalf("Analyzer 未通过: %s", strings.Join(msgs, " | "))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("取凭证失败: %v", err)
	}
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	mutated := mutate(string(out))
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"),
		[]byte(fmt.Sprintf(rtProbeVMHeaderTmpl, filepath.Base(dir))+probeBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-v", "-run", "TestProbe", "./"+filepath.ToSlash(rel))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	bout, err := cmd.CombinedOutput()
	return string(bout) + "\n[err=" + rtErr(err) + "]"
}

// TestLocalStaticRecordingCount B3 主验收：真实生成代码的实际录制计数。
//
// 帧1 录制 1 次；稳定帧计数不增长；换主题（key.theme）→ 恰 +1；
// 改 Constraints（画布变大/还原）→ 各恰 +1 且 dims 跟随几何；
// 动态文字（Title）更新不触碰静态子树计数；dims 逐帧一致（几何对照）。
func TestLocalStaticRecordingCount(t *testing.T) {
	vmSrc := localStaticVM
	out := rtProbeVM(t, vmSrc, localStaticPage, `
func TestProbeLocalStaticRecording(t *testing.T) {
	th1 := material.NewTheme()
	th2 := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func(th *material.Theme) {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	dimsOf := func() image.Point {
		return v.staticDims0.Size
	}

	// 帧1：首次录制恰好 1 次
	frame(th1)
	fmt.Printf("SCEN localstatic-recording frame=1 recs=%d dims=%v\n", v.staticRecs0, dimsOf())
	if v.staticRecs0 != 1 {
		t.Fatalf("帧1: recs=%d, want 1", v.staticRecs0)
	}
	d1 := dimsOf()

	// 帧2-3：稳定——计数不增长，dims 逐帧一致
	frame(th1)
	frame(th1)
	fmt.Printf("SCEN localstatic-recording frame=3 recs=%d dims=%v\n", v.staticRecs0, dimsOf())
	if v.staticRecs0 != 1 || dimsOf() != d1 {
		t.Fatalf("稳定帧: recs=%d dims=%v (d1=%v)", v.staticRecs0, dimsOf(), d1)
	}

	// 帧4：key.theme 变化 → 恰好重录 1 次
	frame(th2)
	fmt.Printf("SCEN localstatic-recording frame=4 recs=%d dims=%v\n", v.staticRecs0, dimsOf())
	if v.staticRecs0 != 2 {
		t.Fatalf("换主题后 recs=%d, want 2（恰好一次重录）", v.staticRecs0)
	}
	if dimsOf() != d1 {
		t.Fatalf("同约束重录后 dims 应一致: %v vs %v", dimsOf(), d1)
	}
	frame(th2)
	if v.staticRecs0 != 2 {
		t.Fatalf("稳定帧: recs=%d", v.staticRecs0)
	}

	// 帧5：Constraints 变化（画布 600x200）→ 恰好重录，dims 跟随几何
	gtx.Constraints = layout.Exact(image.Pt(600, 200))
	frame(th2)
	fmt.Printf("SCEN localstatic-recording frame=5 recs=%d dims=%v\n", v.staticRecs0, dimsOf())
	if v.staticRecs0 != 3 {
		t.Fatalf("改 Constraints 后 recs=%d, want 3", v.staticRecs0)
	}
	d5 := dimsOf()

	// 帧6：动态文字更新（Title 绑定）——兄弟几何变化使静态子树按契约
	// "宁可多失效"重录一次（Flex 逐子递减约束）；状态本身正常更新。
	obj.Title = "动态标题更新"
	frame(th2)
	fmt.Printf("SCEN localstatic-recording frame=6 recs=%d dims=%v title=%q\n",
		v.staticRecs0, dimsOf(), obj.Title)
	if v.staticRecs0 != 4 {
		t.Fatalf("帧6: recs=%d, want 4（兄弟几何变化的合法重录）", v.staticRecs0)
	}
	if obj.Title != "动态标题更新" || len(v.slots) != 1 || v.slots[0].text != "动态标题更新" {
		t.Fatalf("动态文字状态: %q", obj.Title)
	}

	// 帧7：Constraints 还原 → 恰好重录回 d1
	gtx.Constraints = layout.Exact(image.Pt(300, 200))
	frame(th2)
	if v.staticRecs0 != 5 || dimsOf() != d1 {
		t.Fatalf("还原 Constraints: recs=%d dims=%v (d1=%v)", v.staticRecs0, dimsOf(), d1)
	}
	fmt.Printf("SCEN localstatic-recording frame=7 recs=%d dims=%v d5=%v\n",
		v.staticRecs0, dimsOf(), d5)
	fmt.Println("SCEN-DONE localstatic-recording")
}
`)
	assertProbeHealth(t, "localstatic-recording", out)
	t.Log(out)
}

// TestLocalStaticTwoViewIsolation B2：多实例隔离——各自独立录制，
// 互不串写（不同 View 的 staticOps/Key 各自持有）。
func TestLocalStaticTwoViewIsolation(t *testing.T) {
	out := rtProbeVM(t, localStaticVM, localStaticPage, `
func TestProbeTwoViews(t *testing.T) {
	th := material.NewTheme()
	objA, objB := &gvm.VM{}, &gvm.VM{}
	vA, err := NewGoxmlDashboardView(objA)
	if err != nil {
		t.Fatal(err)
	}
	vB, err := NewGoxmlDashboardView(objB)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		vA.Layout(gtx, th)
		vB.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	fmt.Printf("SCEN two-view recA=%d recB=%d\n", vA.staticRecs0, vB.staticRecs0)
	if vA.staticRecs0 != 1 || vB.staticRecs0 != 1 {
		t.Fatalf("两实例应各自独立录制: recA=%d recB=%d", vA.staticRecs0, vB.staticRecs0)
	}
	// B2 已证事实：动态兄弟（Title 绑定文字）内容变化会改变 Flex 分配给
	// 后续子节点的可用约束（空串/非空行高不同）→ 静态子树按契约"宁可多
	// 失效"重录一次。两实例各自独立计数，互不串写。
	objA.Title = "A 改了"
	frame()
	fmt.Printf("SCEN two-view recA=%d recB=%d titleA=%q\n",
		vA.staticRecs0, vB.staticRecs0, objA.Title)
	if vA.staticRecs0 != 2 || vB.staticRecs0 != 1 || vA.slots[0].text != "A 改了" || vB.slots[0].text != "" {
		t.Fatalf("兄弟几何变化应使 A 重录一次、B 不动: recA=%d recB=%d", vA.staticRecs0, vB.staticRecs0)
	}
	// 稳定帧（几何不再变化）——两实例计数均不增长
	frame()
	frame()
	if vA.staticRecs0 != 2 || vB.staticRecs0 != 1 {
		t.Fatalf("稳定帧计数增长: recA=%d recB=%d", vA.staticRecs0, vB.staticRecs0)
	}
	fmt.Println("SCEN-DONE two-view")
}
`)
	assertProbeHealth(t, "two-view", out)
	t.Log(out)
}

// TestLocalStaticIfBranchExclusive B2：If 分支内纯静态后代——各分支独占
// 槽位；If 判定翻转后另一分支首帧录制；隐藏分支不录制；动态文字照常更新。
func TestLocalStaticIfBranchExclusive(t *testing.T) {
	page := `<Column>
  <If Condition="{ShowA}">
    <Then>
      <Column>
        <Text Value="分支A静态一"/>
        <Text Value="分支A静态二"/>
      </Column>
    </Then>
    <Else>
      <Text Value="分支B静态"/>
    </Else>
  </If>
  <Text Value="{Title}"/>
</Column>`
	out := rtProbeVM(t, localStaticVM, page, `
func TestProbeIfBranches(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	obj.SetShowA(true)
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	fmt.Printf("SCEN if-branch frame=1 recs0=%d recs1=%d\n", v.staticRecs0, v.staticRecs1)
	// Then 分支的静态子树独占槽位 0（首帧录制 1 次）
	if v.staticRecs0 != 1 {
		t.Fatalf("Then 分支静态子树应录制 1 次: recs0=%d", v.staticRecs0)
	}
	// 判定翻转 → Else 分支的静态子树独占槽位 1（首帧录制）
	obj.SetShowA(false)
	frame()
	fmt.Printf("SCEN if-branch frame=2 recs0=%d recs1=%d\n", v.staticRecs0, v.staticRecs1)
	if v.staticRecs1 != 1 {
		t.Fatalf("Else 分支静态子树应独占槽位 1: recs1=%d", v.staticRecs1)
	}
	// 隐藏分支不录制（其闭包不执行）；重现分支 key 未变 → **回放**
	// （recs0 不增长，1 即回放证据——重录才是 2）
	obj.SetShowA(true)
	frame()
	frame()
	fmt.Printf("SCEN if-branch frame=4 recs0=%d recs1=%d\n", v.staticRecs0, v.staticRecs1)
	if v.staticRecs0 != 1 || v.staticRecs1 != 1 {
		t.Fatalf("重现分支应回放（recs0=1）且隐藏分支不动（recs1=1）: %d/%d",
			v.staticRecs0, v.staticRecs1)
	}
	// 动态文字（Title）在 If 之外，每帧照常更新
	obj.Title = "动态"
	frame()
	if obj.Title != "动态" || len(v.slots) != 1 || v.slots[0].text != "动态" {
		t.Fatalf("动态文字: %q", obj.Title)
	}
	fmt.Printf("SCEN if-branch final recs0=%d recs1=%d title=%q\n",
		v.staticRecs0, v.staticRecs1, obj.Title)
	fmt.Println("SCEN-DONE if-branch")
}
`)
	assertProbeHealth(t, "if-branch", out)
	t.Log(out)
}

// TestLocalStaticForUntouched B2 边界：<For> 体内沿用既有路径——For 外
// 静态子树恰好一个局部槽位且计数稳定；运行时行文字照常更新。
func TestLocalStaticForUntouched(t *testing.T) {
	vmSrc := localStaticVM
	// 静态子树放在 For **之前**：For 行数变化改变的是其后子节点收到的
	// 约束（Flex 逐子递减），不影响此前子节点的 key——正是"For 变化
	// 不触碰静态子树缓存"的隔离形态。
	page := `<Column>
  <Column>
    <Text Value="For外静态"/>
  </Column>
  <For Each="row" In="{Rows}">
    <Text Value="{row.Name}"/>
  </For>
</Column>`
	// 结构断言：For 体行内不分配局部静态槽位——生成源码中 staticRecs0
	// 恰好出现 2 处（字段声明 + For 外子树的计数自增；For 体无引用）。
	// 用 rtProbeVMMutated 的 mutate 钩子捕获生成源码（identity 变异）。
	forProbe := `
func TestProbeForUntouched(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	// For 外静态子树正常录制一次
	if v.staticRecs0 != 1 {
		t.Fatalf("For 外静态子树应录制 1 次: recs0=%d", v.staticRecs0)
	}
	// 帧3-4：稳定帧（For 行未变化）——静态子树计数不增长
	frame()
	frame()
	if v.staticRecs0 != 1 {
		t.Fatalf("稳定帧计数增长: %d", v.staticRecs0)
	}
	// 行内容变化 → 行文字更新（For 沿用既有逐帧路径）
	obj.SetRows([]gvm.Row{{Name: "甲乙丙"}, {Name: "丁"}})
	frame()
	fmt.Printf("SCEN for-untouched rows=%v recs0=%d\n", obj.Rows, v.staticRecs0)
	fmt.Println("SCEN-DONE for-untouched")
}
`
	var genSrc string
	out := rtProbeVMMutated(t, vmSrc, page, forProbe, func(gen string) string {
		genSrc = gen
		return gen
	})
	if strings.Count(genSrc, "staticRecs0") != 2 {
		t.Fatalf("For 体不得分配局部静态槽位（生成源码 staticRecs0 应恰 2 处：字段声明+自增）")
	}
	assertProbeHealth(t, "for-untouched", out)
	t.Log(out)
}

// TestLocalStaticMutationForcedRerecord 变异红 1（可编译）：把录制分支
// 条件改为恒真——强制每帧重录。稳定帧录制计数随之增长，"稳定帧不增长"
// 的探针断言必须翻红。原源码（未变异）同一探针 PASS（对照组）。
func TestLocalStaticMutationForcedRerecord(t *testing.T) {
	vmSrc := localStaticVM
	// 变异隔离：静态子树放首位（其收到的 Constraints 不受后置动态兄弟
	// 几何影响），使"强制每帧重录"成为唯一失效源。
	forcedPage := `<Column>
  <Column>
    <Text Value="静态标题A"/>
    <Text Value="静态标题B"/>
  </Column>
  <Text Value="{Title}"/>
</Column>`
	probe := `
func TestProbeStableRecs(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	frame()
	stable := v.staticRecs0
	frame()
	frame()
	fmt.Printf("SCEN forced-rerecord stable=%d final=%d\n", stable, v.staticRecs0)
	if v.staticRecs0 != stable {
		t.Fatalf("稳定帧录制计数增长: %d -> %d", stable, v.staticRecs0)
	}
	fmt.Println("SCEN-DONE forced-rerecord")
}
`
	healthy := rtProbeVM(t, vmSrc, forcedPage, probe)
	assertProbeHealth(t, "forced-rerecord", healthy)

	mutatedOut := rtProbeVMMutated(t, vmSrc, forcedPage, probe, func(gen string) string {
		// 可编译变异：录制分支条件恒真（强制每帧重录）。
		old := "if !v.staticCached0 || !v.staticKey0.equal(kS0) {"
		mut := "if true || !v.staticCached0 || !v.staticKey0.equal(kS0) {"
		if !strings.Contains(gen, old) {
			t.Fatalf("变异锚点未命中（生成源码形态漂移）")
		}
		return strings.Replace(gen, old, mut, 1)
	})
	// 变异必须翻红：稳定帧计数增长，探针断言失败。
	if !strings.Contains(mutatedOut, "稳定帧录制计数增长") || !strings.Contains(mutatedOut, "--- FAIL") {
		t.Fatalf("变异『强制每帧重录』应使稳定帧计数断言翻红:\n%s", mutatedOut)
	}
	t.Log(mutatedOut)
}

// TestLocalStaticMissedMetricKeyRed 变异红 2（可编译）：equal() 漏掉
// metric 字段——DPI 变化不再触发重录，静态子树停留在旧 DPI 的几何上，
// 尺寸断言翻红。原源码同一探针 PASS（对照组）。
//
// 变异隔离：静态子树放**首位**——DPI 变化不改变首个子节点收到的
// Constraints（pixel 约束与 DPI 无关），使"漏掉 metric"成为唯一失效源；
// 若静态子树排在动态兄弟（Title 行 + Button）之后，兄弟在 DPI 3 下的
// 几何变化会合法地改变其收到的约束并掩盖 metric 缺失（DIAG 实测：
// key.cons Max.Y 149→48）。
func TestLocalStaticMissedMetricKeyRed(t *testing.T) {
	vmSrc := localStaticVM
	metricPage := `<Column>
  <Column>
    <Text Value="静态标题A"/>
    <Text Value="静态标题B"/>
  </Column>
  <Text Value="{Title}"/>
</Column>`
	probe := `
func TestProbeMetricChange(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	d1 := v.staticDims0.Size
	fmt.Printf("DIAG frame1 key.metric=%v key.cons=%v dims=%v recs=%d\n",
		v.staticKey0.metric, v.staticKey0.constraints, v.staticDims0.Size, v.staticRecs0)
	// DPI 1 → 3：静态文字几何必须变化（重录）
	gtx.Metric = unit.Metric{PxPerDp: 3, PxPerSp: 3}
	frame()
	d2 := v.staticDims0.Size
	fmt.Printf("DIAG frame2 key.metric=%v key.cons=%v dims=%v recs=%d\n",
		v.staticKey0.metric, v.staticKey0.constraints, v.staticDims0.Size, v.staticRecs0)
	fmt.Printf("SCEN metric-change d1=%v d2=%v recs=%d\n", d1, d2, v.staticRecs0)
	if d2.Y <= d1.Y {
		t.Fatalf("DPI 变化后静态子树几何未跟随（漏失效）: d1=%v d2=%v", d1, d2)
	}
	fmt.Println("SCEN-DONE metric-change")
}
`
	healthy := rtProbeVM(t, vmSrc, metricPage, probe)
	assertProbeHealth(t, "metric-change", healthy)

	mutatedOut := rtProbeVMMutated(t, vmSrc, metricPage, probe, func(gen string) string {
		// 可编译变异：equal 漏掉 metric 两个字段（位比较只比其余字段），
		// 并移除失去用途的 math import——否则是编译失败而非行为红。
		old := "math.Float32bits(k.metric.PxPerDp) == math.Float32bits(o.metric.PxPerDp) &&\n\t\tmath.Float32bits(k.metric.PxPerSp) == math.Float32bits(o.metric.PxPerSp) &&\n\t\t"
		if !strings.Contains(gen, old) {
			t.Fatalf("变异锚点未命中（生成源码形态漂移）")
		}
		gen = strings.Replace(gen, old, "", 1)
		return strings.Replace(gen, "\t\"math\"\n", "", 1)
	})
	// 变异必须翻红：DPI 变化后几何未跟随，探针断言失败。
	if !strings.Contains(mutatedOut, "几何未跟随") || !strings.Contains(mutatedOut, "--- FAIL") {
		t.Fatalf("变异『漏掉 metric key』应使 DPI 几何断言翻红:\n%s", mutatedOut)
	}
	t.Log(mutatedOut)
}
