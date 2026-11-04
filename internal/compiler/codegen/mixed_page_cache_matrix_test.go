package codegen

// ============================================================================
// R13-B MIXED-PAGE-CACHE-CORRECTNESS：同一动态页面同时包含
// 固定静态子树、绑定 Text、If 分支、带稳定 Key 的 For 行事件时，
// 局部缓存回放与动态更新互不干扰（R10 局部静态缓存 × R6 绑定缓存 ×
// For 行状态池的联合验收）。
//
// 判别矩阵（先在基线跑，若全绿则本文件即判别回归）：
//
//	SCEN mix-fixed-text      固定约束下只改绑定文本 → 静态槽不重录（M2）
//	SCEN mix-sibling-width   前序动态兄弟改变实际约束 → 静态子树重录（M1）
//	SCEN mix-if-reuse        If 隐藏再显示且键未变 → 复用（M3）
//	SCEN mix-for-events      For 重排/删除后事件仍命中正确 ID（M4）
//	SCEN mix-context-items   主题指针 / Locale / DPI 逐项变化 → 逐项重录（M5）
//	SCEN mix-two-instances   双 View 实例互不串写 + 同帧参考 View 几何对照（M6）
//
// 手段：真实链路（Analyzer→Codegen→go test→离屏 Router），计数仪表经
// injectProbeRecs 外部副本注入（R11-B 生产分离），assertProbeHealth 拒绝
// 内部 SKIP/缺完成标记。几何比较用同帧新建参考 View，避免把单像素差
// 归咎缓存。
// ============================================================================

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// mixedVM 是混合页面的探测 ViewModel。
const mixedVM = `package vm

// VM 是 R13-B 混合页面验收用 ViewModel。
type VM struct {
	Title      string
	Note       string
	Detail     string
	ShowA      bool
	Rows       []Row
	LastTapped string
}

// Row 是 <For> 行数据（Key=row.ID，行事件实参取 ID）。
type Row struct {
	ID   string
	Name string
}

// Increment 是固定 Button 的无参处理器。
func (vm *VM) Increment() { vm.Note = vm.Note + "+" }

// TapRow 是行事件处理器（实参 row.ID）。
func (vm *VM) TapRow(id string) { vm.LastTapped = id }
`

// mixedPage 是混合页面：
//
//	Row[ {Title} + 岛0 ]   —— 岛0 与动态文字同处水平行：Title 宽度变 → 岛0 约束变（M1）
//	{Note}                 —— 竖直流中的动态文字
//	岛1                    —— 竖直流中的纯静态 Column：兄弟文字变化不影响其约束（M2）
//	Button 确认            —— 动态交互控件
//	If{Then[ {Detail} + 岛2 ] Else[ 岛3 ]}（M3）
//	For Key=row.ID 行按钮（M4）
//
// 顶层不加 Padding/Gap，保证 For 行点击坐标可用「总高差」精确推导。
const mixedPage = `<Row>
  <Text Value="{Title}"/>
  <Column>
    <Text Value="标题面板A"/>
    <Text Value="标题面板B"/>
  </Column>
</Row>
<Text Value="{Note}"/>
<Column>
  <Text Value="中部静态甲"/>
  <Text Value="中部静态乙"/>
</Column>
<Button Text="确认" Click="Increment"/>
<If Condition="{ShowA}">
  <Then>
    <Text Value="{Detail}"/>
    <Column>
      <Text Value="分支A静态"/>
    </Column>
  </Then>
  <Else>
    <Column>
      <Text Value="分支B静态"/>
    </Column>
  </Else>
</If>
<For Each="row" In="{Rows}" Key="{row.ID}">
  <Button Text="{row.Name}" Click="TapRow(row.ID)"/>
</For>`

// mixedProbeHeaderTmpl 与 rtProbeVMHeaderTmpl 同体，但额外导入
// gioui.org/io/system（M5 需要构造 system.Locale 值逐项变化）。
const mixedProbeHeaderTmpl = `package demo

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
	gvm "github.com/goxml/goxml/internal/compiler/codegen/testdata/%s/vm"
	"github.com/goxml/goxml/pkg/runtime/app"
)

var (
	_ = f32.Pt
	_ = input.Router{}
	_ = pointer.Press
	_ = key.NameReturn
	_ = fmt.Sprint
	_ = app.RunWindow
	_ = time.Second
	_ = strings.Contains
	_ = material.NewTheme
	_ = gvm.VM{}
	_ = headless.NewWindow
	_ = unit.Metric{}
	_ = system.Locale{}
)
`

// rtMixedProbe 与 rtProbeVMMutated 同链路（Analyzer→Codegen→mutate→写盘→
// go test），仅使用带 system 导入的探针头。
func rtMixedProbe(t *testing.T, vmSrc, gxmlBody, probeBody string, mutate func(string) string) string {
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
	if mutate != nil {
		out = []byte(mutate(string(out)))
	}
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"),
		[]byte(fmt.Sprintf(mixedProbeHeaderTmpl, filepath.Base(dir))+probeBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-v", "-run", "TestProbe", "./"+filepath.ToSlash(rel))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	bout, err := cmd.CombinedOutput()
	return string(bout) + "\n[err=" + rtErr(err) + "]"
}

// mixedProbePrelude 是混合页面探针的公共装配（配合 4 槽位计数注入使用）。
const mixedProbePrelude = `
func mixSetup(title, note string) (*gvm.VM, *GoxmlDashboardView, *input.Router, *layout.Context, *material.Theme) {
	th := material.NewTheme()
	obj := &gvm.VM{Title: title, Note: note}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		panic(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(600, 400)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	return obj, v, &r, &gtx, th
}

func mixFrame(gtx *layout.Context, r *input.Router, v *GoxmlDashboardView, th *material.Theme) layout.Dimensions {
	gtx.Reset()
	d := v.Layout(*gtx, th)
	r.Frame(gtx.Ops)
	return d
}
`

// TestMixedPageFixedText M2：固定约束下只改绑定文本 → 静态槽不重录。
// 岛1 在竖直流中，兄弟 {Note} 文字变化不改变岛1收到的约束；几何逐帧一致。
func TestMixedPageFixedText(t *testing.T) {
	out := rtMixedProbe(t, mixedVM, mixedPage, mixedProbePrelude+`
func TestProbeMixFixedText(t *testing.T) {
	obj, v, r, gtx, th := mixSetup("短", "备注一")
	frame := func() layout.Dimensions { return mixFrame(gtx, r, v, th) }

	frame()
	if v.probeRecs1 != 1 {
		t.Fatalf("帧1: 岛1 recs=%d, want 1", v.probeRecs1)
	}
	d1 := v.staticDims1.Size
	k1 := v.staticKey1
	if !v.slots[1].valid || v.slots[1].text != "备注一" {
		t.Fatalf("初始备注未进入绑定布局: %q", v.slots[1].text)
	}

	// 改竖直流兄弟文字：画布 600px 宽、文本不换行 → 兄弟高度不变 →
	// 岛1 收到的约束逐帧相同（Flex 按剩余空间传导约束；若文字换行增高，
	// 后续子节点约束随之变化，重录属契约正确行为）→ 不重录
	obj.Note = "变得长了很多的备注内容"
	frame()
	if v.slots[1].text != obj.Note {
		t.Fatalf("备注未更新: got %q want %q", v.slots[1].text, obj.Note)
	}
	if !v.staticKey1.equal(k1) {
		t.Fatal("固定约束对照中岛1的缓存键发生变化")
	}
	if v.probeRecs1 != 1 {
		t.Fatalf("改兄弟绑定文本后岛1重录: recs1=%d, want 1（M2 违反）", v.probeRecs1)
	}
	if v.staticDims1.Size != d1 {
		t.Fatalf("岛1几何漂移: %v -> %v", d1, v.staticDims1.Size)
	}

	// 稳定帧再验证两次
	frame()
	frame()
	if v.probeRecs1 != 1 {
		t.Fatalf("稳定帧岛1重录: recs1=%d, want 1", v.probeRecs1)
	}
	fmt.Println("SCEN-DONE mix-fixed-text")
}
`, func(gen string) string { return injectProbeRecs(t, gen, 4) })
	assertProbeHealth(t, "mix-fixed-text", out)
}

// TestMixedPageSiblingWidth M1：水平 Row 中前序动态兄弟 {Title} 变宽，
// 岛0 实际收到的约束变窄 → 重录；恢复短标题 → 再次重录（键按值比较）。
// 同帧参考 View 几何对照：恢复后的岛0尺寸与全新实例一致。
func TestMixedPageSiblingWidth(t *testing.T) {
	out := rtMixedProbe(t, mixedVM, mixedPage, mixedProbePrelude+`
func TestProbeMixSiblingWidth(t *testing.T) {
	obj, v, r, gtx, th := mixSetup("宽", "")
	frame := func() layout.Dimensions { return mixFrame(gtx, r, v, th) }

	frame()
	if v.probeRecs0 != 1 {
		t.Fatalf("帧1: 岛0 recs=%d, want 1", v.probeRecs0)
	}
	dShort := v.staticDims0.Size

	// 变长 → Row 中岛0 的剩余宽度变 → 重录（M1 核心：实际约束入键）
	obj.Title = "非常非常长的标题文字内容"
	frame()
	if v.probeRecs0 != 2 {
		t.Fatalf("前序兄弟变宽后岛0未重录: recs0=%d, want 2（M1 违反）", v.probeRecs0)
	}

	// 恢复短标题 → 约束恢复 → 键与缓存（长标题态）不同 → 再次重录
	obj.Title = "宽"
	frame()
	if v.probeRecs0 != 3 {
		t.Fatalf("约束恢复后岛0未重录: recs0=%d, want 3", v.probeRecs0)
	}
	if v.staticDims0.Size != dShort {
		t.Fatalf("恢复后岛0几何应回到短标题态: %v -> %v", dShort, v.staticDims0.Size)
	}

	// 稳定帧：约束不变 → 不再重录
	frame()
	if v.probeRecs0 != 3 {
		t.Fatalf("稳定帧岛0重录: recs0=%d, want 3", v.probeRecs0)
	}

	// 同帧参考 View：全新实例只跑一帧（必然全量录制），岛0几何与主实例一致
	vRef, err := NewGoxmlDashboardView(&gvm.VM{Title: "宽", Note: ""})
	if err != nil {
		t.Fatal(err)
	}
	gtx.Reset()
	vRef.Layout(*gtx, th)
	if vRef.staticDims0.Size != dShort {
		t.Fatalf("同帧参考对照失败: 主=%v 参考=%v", dShort, vRef.staticDims0.Size)
	}
	_ = r
	fmt.Println("SCEN-DONE mix-sibling-width")
}
`, func(gen string) string { return injectProbeRecs(t, gen, 4) })
	assertProbeHealth(t, "mix-sibling-width", out)
}

// TestMixedPageIfReuse M3：If 隐藏再显示且键未变 → 分支岛复用（不重录）；
// 分支内动态文字变化不牵连分支岛。
func TestMixedPageIfReuse(t *testing.T) {
	out := rtMixedProbe(t, mixedVM, mixedPage, mixedProbePrelude+`
func TestProbeMixIfReuse(t *testing.T) {
	obj, v, r, gtx, th := mixSetup("页", "")
	obj.ShowA = true
	obj.Detail = "细节一"
	frame := func() layout.Dimensions { return mixFrame(gtx, r, v, th) }

	frame()
	if v.probeRecs2 != 1 {
		t.Fatalf("帧1: Then岛 recs2=%d, want 1", v.probeRecs2)
	}
	if v.probeRecs3 != 0 {
		t.Fatalf("帧1: Else岛不应被访问, recs3=%d", v.probeRecs3)
	}
	dThen := v.staticDims2.Size

	// 隐藏 → Else 录制；Then 岛不被访问
	obj.ShowA = false
	frame()
	if v.probeRecs3 != 1 {
		t.Fatalf("Else岛首现: recs3=%d, want 1", v.probeRecs3)
	}
	if v.probeRecs2 != 1 {
		t.Fatalf("隐藏时Then岛被触碰: recs2=%d", v.probeRecs2)
	}

	// 再显示 → 键未变 → 复用（不重录）
	obj.ShowA = true
	frame()
	if v.probeRecs2 != 1 {
		t.Fatalf("再显示后Then岛重录: recs2=%d, want 1（M3 违反）", v.probeRecs2)
	}
	if v.staticDims2.Size != dThen {
		t.Fatalf("复用后Then岛几何漂移: %v -> %v", dThen, v.staticDims2.Size)
	}

	// 分支内动态文字变化 → 分支岛仍复用
	obj.Detail = "细节变得很长很长"
	frame()
	if !v.slots[2].valid || v.slots[2].text != obj.Detail {
		t.Fatalf("分支文字未更新: got %q want %q", v.slots[2].text, obj.Detail)
	}
	if v.probeRecs2 != 1 {
		t.Fatalf("分支内动态文字变化牵连岛2: recs2=%d, want 1", v.probeRecs2)
	}
	_ = r
	fmt.Println("SCEN-DONE mix-if-reuse")
}
`, func(gen string) string { return injectProbeRecs(t, gen, 4) })
	assertProbeHealth(t, "mix-if-reuse", out)
}

// TestMixedPageForEvents M4：For 行事件按 Key 归属——重排、删除后
// 点击同一屏幕位置命中的是「当前在该位置的行」。
// 行高与行起始 y 用真实布局推导（N 行与 N-1 行的总高差），不靠猜。
func TestMixedPageForEvents(t *testing.T) {
	out := rtMixedProbe(t, mixedVM, mixedPage, `
func TestProbeMixForEvents(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{Rows: []gvm.Row{
		{ID: "A", Name: "行A"},
		{ID: "B", Name: "行B"},
		{ID: "C", Name: "行C"},
	}}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	// 高度不封顶：让竖直流按内容取高，行高才能用「总高差」推导
	// （Exact 画布会把总高钳在画布上，差分恒为 0）。
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Point{X: 300, Y: 100000}},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	lay := func() int {
		gtx.Reset()
		d := v.Layout(gtx, th)
		r.Frame(gtx.Ops)
		return d.Size.Y
	}
	// 行高推导：N 行与 N-1 行的布局总高差（行按钮等高，竖直流无间距）。
	obj.Rows = []gvm.Row{{ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}, {ID: "C", Name: "行C"}}
	h3 := lay()
	obj.Rows = obj.Rows[:2]
	h2 := lay()
	rowH := h3 - h2
	if rowH <= 0 {
		t.Fatalf("行高推导失败: h3=%d h2=%d", h3, h2)
	}
	obj.Rows = []gvm.Row{{ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}, {ID: "C", Name: "行C"}}
	y0 := h3 - 3*rowH
	lay()

	click := func(y float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Release, Source: pointer.Mouse},
		)
		lay()
	}
	yOf := func(i int) float32 { return float32(y0 + i*rowH + rowH/2) }

	// 基线：点第 2 行 → B
	click(yOf(1))
	if obj.LastTapped != "B" {
		t.Fatalf("点第2行: LastTapped=%q, want B", obj.LastTapped)
	}

	a, b, c := v.rowClicks0["A"], v.rowClicks0["B"], v.rowClicks0["C"]
	if a == nil || b == nil || c == nil || a == b || b == c || a == c {
		t.Fatal("三行应各自持有独立的事件状态")
	}

	// 重排 [C,A,B]：第 1 行位置现在是 C，原有状态仍属于原 Key。
	obj.Rows = []gvm.Row{{ID: "C", Name: "行C"}, {ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}}
	lay()
	if v.rowClicks0["A"] != a || v.rowClicks0["B"] != b || v.rowClicks0["C"] != c {
		t.Fatal("重排后行状态没有随 Key 保持")
	}
	click(yOf(0))
	if obj.LastTapped != "C" {
		t.Fatalf("重排后点第1行: LastTapped=%q, want C（M4 事件归属违反）", obj.LastTapped)
	}

	// 删除头部 C → [A,B]：原第 1 行位置现在是 A；状态池只保留存活行。
	obj.Rows = []gvm.Row{{ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}}
	lay()
	if len(v.rowClicks0) != 2 || v.rowClicks0["A"] != a || v.rowClicks0["B"] != b {
		t.Fatal("删除后行状态池没有精确保留 A/B")
	}
	click(yOf(0))
	if obj.LastTapped != "A" {
		t.Fatalf("删除后点第1行: LastTapped=%q, want A（残留状态串位）", obj.LastTapped)
	}
	click(yOf(1))
	if obj.LastTapped != "B" {
		t.Fatalf("删除后点第2行: LastTapped=%q, want B", obj.LastTapped)
	}
	fmt.Println("SCEN-DONE mix-for-events")
}
`, nil)
	assertProbeHealth(t, "mix-for-events", out)
}

// TestMixedPageContextItems M5：主题指针、Locale、DPI 逐项变化 →
// 静态岛逐项重录；未变化的项不触发重录。
func TestMixedPageContextItems(t *testing.T) {
	out := rtMixedProbe(t, mixedVM, mixedPage, mixedProbePrelude+`
func TestProbeMixContextItems(t *testing.T) {
	_, v, r, gtx, th1 := mixSetup("页", "")
	th2 := material.NewTheme()
	frame := func(th *material.Theme) layout.Dimensions { return mixFrame(gtx, r, v, th) }

	gtx.Locale = system.Locale{Language: "en-US"}
	frame(th1)
	if v.probeRecs1 != 1 {
		t.Fatalf("帧1: 岛1 recs=%d, want 1", v.probeRecs1)
	}
	dEn := v.staticDims1.Size

	// 主题指针变化 → 重录；同指针稳定帧不重录
	frame(th2)
	if v.probeRecs1 != 2 {
		t.Fatalf("换主题指针后岛1未重录: recs=%d, want 2（M5 违反）", v.probeRecs1)
	}
	frame(th2)
	if v.probeRecs1 != 2 {
		t.Fatalf("同主题稳定帧岛1重录: recs=%d, want 2", v.probeRecs1)
	}

	// Locale 变化 → 重录（主题指针不动、DPI 不动）
	gtx.Locale = system.Locale{Language: "zh-CN"}
	frame(th2)
	if v.probeRecs1 != 3 {
		t.Fatalf("换 Locale 后岛1未重录: recs=%d, want 3（M5 违反）", v.probeRecs1)
	}

	// Locale 换回历史值 → 键与缓存（zh 态）不同 → 重录
	gtx.Locale = system.Locale{Language: "en-US"}
	frame(th2)
	if v.probeRecs1 != 4 {
		t.Fatalf("Locale 换回后岛1未重录: recs=%d, want 4", v.probeRecs1)
	}

	// DPI 变化 → 重录，且几何随像素密度变化（Y 轴）
	gtx.Metric = unit.Metric{PxPerDp: 2, PxPerSp: 2}
	frame(th2)
	if v.probeRecs1 != 5 {
		t.Fatalf("换 DPI 后岛1未重录: recs=%d, want 5（M5 违反）", v.probeRecs1)
	}
	if v.staticDims1.Size.Y <= dEn.Y {
		t.Fatalf("DPI 翻倍后岛1高度应增长: en=%v dpi2=%v", dEn, v.staticDims1.Size)
	}
	_ = r
	fmt.Println("SCEN-DONE mix-context-items")
}
`, func(gen string) string { return injectProbeRecs(t, gen, 4) })
	assertProbeHealth(t, "mix-context-items", out)
}

// TestMixedPageTwoInstances M6：双 View 实例互不串写——
// 实例B 换 Locale 只重录实例B 的岛；同帧参考实例的岛几何与主实例一致。
func TestMixedPageTwoInstances(t *testing.T) {
	out := rtMixedProbe(t, mixedVM, mixedPage, mixedProbePrelude+`
func TestProbeMixTwoInstances(t *testing.T) {
	th := material.NewTheme()
	objA := &gvm.VM{Title: "A页", Note: "A注"}
	objB := &gvm.VM{Title: "B页面标题长一些", Note: "B注"}
	vA, err := NewGoxmlDashboardView(objA)
	if err != nil {
		t.Fatal(err)
	}
	vB, err := NewGoxmlDashboardView(objB)
	if err != nil {
		t.Fatal(err)
	}
	var rA, rB input.Router
	gtxA := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(600, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: rA.Source()}
	gtxB := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(600, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: rB.Source()}
	frameBoth := func() {
		gtxA.Reset()
		vA.Layout(gtxA, th)
		rA.Frame(gtxA.Ops)
		gtxB.Reset()
		vB.Layout(gtxB, th)
		rB.Frame(gtxB.Ops)
	}

	gtxA.Locale = system.Locale{Language: "en-US"}
	gtxB.Locale = system.Locale{Language: "en-US"}
	frameBoth()
	if vA.probeRecs1 != 1 || vB.probeRecs1 != 1 {
		t.Fatalf("帧1: recsA=%d recsB=%d, want 1/1", vA.probeRecs1, vB.probeRecs1)
	}
	// A/B 画布约束相同 → 岛1几何一致；兄弟动态文字不同不影响竖直流岛
	if vA.staticDims1.Size != vB.staticDims1.Size {
		t.Fatalf("同约束下两实例岛1几何应一致: A=%v B=%v", vA.staticDims1.Size, vB.staticDims1.Size)
	}

	// B 换 Locale → 只重录 B 的岛1；A 不被串写
	gtxB.Locale = system.Locale{Language: "zh-CN"}
	frameBoth()
	if vB.probeRecs1 != 2 {
		t.Fatalf("B 换 Locale 后未重录: recsB=%d, want 2", vB.probeRecs1)
	}
	if vA.probeRecs1 != 1 {
		t.Fatalf("B 换 Locale 串写了 A: recsA=%d, want 1（M6 违反）", vA.probeRecs1)
	}

	// B 的动态文字变化 → 两实例的岛1都不重录
	objB.Note = "B注二" // 等短文本：不换行 → 兄弟高度不变 → 岛1约束逐帧相同
	frameBoth()
	if vA.slots[1].text != objA.Note || vB.slots[1].text != objB.Note {
		t.Fatalf("绑定布局串写或未更新: A=%q B=%q", vA.slots[1].text, vB.slots[1].text)
	}
	if vA.probeRecs1 != 1 || vB.probeRecs1 != 2 {
		t.Fatalf("动态文字变化牵连静态岛: recsA=%d recsB=%d", vA.probeRecs1, vB.probeRecs1)
	}

	// 同帧参考实例：A 的状态 + A 的上下文，全新 View 一帧录制 → 岛1几何一致
	vRef, err := NewGoxmlDashboardView(&gvm.VM{Title: "A页", Note: "A注"})
	if err != nil {
		t.Fatal(err)
	}
	gtxRef := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(600, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Locale: system.Locale{Language: "en-US"}}
	vRef.Layout(gtxRef, th)
	if vRef.staticDims1.Size != vA.staticDims1.Size {
		t.Fatalf("同帧参考对照失败: 主=%v 参考=%v", vA.staticDims1.Size, vRef.staticDims1.Size)
	}
	fmt.Println("SCEN-DONE mix-two-instances")
}
`, func(gen string) string { return injectProbeRecs(t, gen, 4) })
	assertProbeHealth(t, "mix-two-instances", out)
}
