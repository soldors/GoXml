// R8批准后回归：保留历史测试名；期望改为程序化静默、显示追随、事件时点Submit。
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

const probeVMSrc = `package vm

import "strings"

type VM struct {
	Title       string
	LastText    string // 最近一次 ValueChanged 载荷
	ChangeCount int
	SubmitCount int
	LastSubmit  string
	Other       string
}

func (vm *VM) IdentityText(s string) { vm.ChangeCount++; vm.LastText = s; vm.Title = s }

func (vm *VM) UpperText(s string) { vm.ChangeCount++; vm.LastText = s; vm.Title = strings.ToUpper(s) }

func (vm *VM) AppendText(s string) { vm.ChangeCount++; vm.LastText = s; vm.Title += s }

func (vm *VM) TouchOther(s string) { vm.ChangeCount++; vm.LastText = s; vm.Other = "X-" + s }

func (vm *VM) SubmitRecord(s string) { vm.SubmitCount++; vm.LastSubmit = s }
`

const rtProbeVMHeaderTmpl = `package demo

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
)
`

func rtProbeVM(t *testing.T, vmSrc, gxmlBody, probeBody string) string {
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
		t.Fatalf("Analyzer 未通过: %v", codes(r))
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
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"),
		[]byte(fmt.Sprintf(rtProbeVMHeaderTmpl, base)+probeBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-v", "-run", "TestProbe", "./"+filepath.ToSlash(rel))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	bout, err := cmd.CombinedOutput()
	return string(bout) + "\n[err=" + rtErr(err) + "]"
}

const frameProbePrelude = `
func divSetup(t *testing.T, title string) (*gvm.VM, *GoxmlDashboardView, *input.Router, func()) {
	th := material.NewTheme()
	obj := &gvm.VM{Title: title}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil { t.Fatal(err) }
	var r input.Router
	gtx := layout.Context{
		Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 40)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: r.Source(),
	}
	frame := func() { gtx.Reset(); v.Layout(gtx, th); r.Frame(gtx.Ops) }
	frame()
	r.Queue(pointer.Event{Position: f32.Pt(10,10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary})
	r.Queue(pointer.Event{Position: f32.Pt(10,10), Kind: pointer.Release, Source: pointer.Mouse})
	frame()
	if !gtx.Focused(&v.input0) { t.Fatal("no focus") }
	return obj, v, &r, frame
}`

func TestInputDivergenceNormalize(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText"/>`, frameProbePrelude+`
func TestProbeNormalizeDivergence(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abc"})
	frame()
	if obj.Title != "ABC" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want ABC/1", obj.Title, obj.ChangeCount)
	}
	if obj.LastText != "abc" {
		t.Fatalf("帧3: 载荷应为编辑器原文 abc, got %q", obj.LastText)
	}
	if ed := v.input0.Text(); ed != "ABC" {
		t.Fatalf("帧3: editor=%q, want ABC", ed)
	}
	if v.applied0 != "ABC" {
		t.Fatalf("帧3: applied=%q, want ABC（显示追随已归一化模型）", v.applied0)
	}
	for i := 4; i <= 6; i++ {
		frame()
		if ed := v.input0.Text(); ed != "ABC" {
			t.Fatalf("帧%d: divergence 被自愈? editor=%q", i, ed)
		}
		if obj.ChangeCount != 1 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
	}
	fmt.Printf("DIVERGENCE: editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "DIVERGENCE") {
		t.Fatalf("divergence 未实证:\n%s", out)
	}
	t.Log(strings.TrimSpace(out[strings.Index(out, "DIVERGENCE"):]))
}

func TestInputDivergenceNormalizePreset(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText"/>`, frameProbePrelude+`
func TestProbeNormalizePreset(t *testing.T) {
	obj, v, _, frame := divSetup(t, "a")
	if obj.Title != "a" || obj.ChangeCount != 0 {
		t.Fatalf("预设值应保持原文且程序化静默: model=%q count=%d", obj.Title, obj.ChangeCount)
	}
	if v.applied0 != "a" {
		t.Fatalf("applied 应已被 reapply 推进: %q", v.applied0)
	}
	for i := 3; i <= 5; i++ {
		frame()
		if ed := v.input0.Text(); ed != "a" {
			t.Fatalf("帧%d: editor=%q, want a（应保持一致）", i, ed)
		}
		if obj.ChangeCount != 0 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
	}
	fmt.Printf("DIVERGENCE-PRESET: editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "DIVERGENCE-PRESET") {
		t.Fatalf("预设分歧未实证:\n%s", out)
	}
	t.Log(strings.TrimSpace(out[strings.Index(out, "DIVERGENCE-PRESET"):]))
}

func TestInputDivergenceIdentity(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/>`, frameProbePrelude+`
func TestProbeIdentity(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abc"})
	frame()
	if obj.Title != "abc" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want abc/1", obj.Title, obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "abc" {
		t.Fatalf("帧3: editor=%q, want abc（恒等回写不应分歧）", ed)
	}
	for i := 4; i <= 6; i++ {
		frame()
		if ed := v.input0.Text(); ed != "abc" || obj.Title != "abc" {
			t.Fatalf("帧%d: editor=%q model=%q 应稳定一致", i, ed, obj.Title)
		}
		if obj.ChangeCount != 1 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
	}
	fmt.Printf("IDENTITY-OK: editor=%q model=%q count=%d\n", v.input0.Text(), obj.Title, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "IDENTITY-OK") {
		t.Fatalf("恒等锚点未实证:\n%s", out)
	}
}

func TestInputDivergenceAppend(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="AppendText"/>`, frameProbePrelude+`
func TestProbeAppend(t *testing.T) {
	obj, v, r, frame := divSetup(t, "A")
	if obj.Title != "A" {
		t.Fatalf("首帧同步应静默，不调用非幂等处理器: model=%q, want A", obj.Title)
	}
	if obj.ChangeCount != 0 {
		t.Fatalf("首帧回调应为0: %d", obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "A" {
		t.Fatalf("编辑器应只显示应用值 A: %q", ed)
	}
	r.Queue(key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: "b"})
	frame()
	if obj.Title != "AAb" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want AAb/1", obj.Title, obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "AAb" {
		t.Fatalf("帧3: editor=%q, want Ab", ed)
	}
	for i := 4; i <= 6; i++ {
		frame()
		if obj.ChangeCount != 1 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
		if ed := v.input0.Text(); ed != "AAb" {
			t.Fatalf("帧%d: editor=%q 应保持（基线不追赶显示）", i, ed)
		}
	}
	fmt.Printf("APPEND-DIVERGENCE: editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "APPEND-DIVERGENCE") {
		t.Fatalf("非幂等分歧未实证:\n%s", out)
	}
	t.Log(strings.TrimSpace(out[strings.Index(out, "APPEND-DIVERGENCE"):]))
}

func TestInputDivergenceExternalWrite(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/>`, frameProbePrelude+`
func TestProbeExternalWrite(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "hi"})
	frame()
	if obj.Title != "hi" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want hi/1", obj.Title, obj.ChangeCount)
	}
	obj.Title = "XYZ"
	frame()
	frame()
	if ed := v.input0.Text(); ed != "XYZ" {
		t.Fatalf("帧5: editor=%q, want XYZ（外部写入应同步显示）", ed)
	}
	if obj.ChangeCount != 1 || obj.LastText != "hi" {
		t.Fatalf("帧5: 外部写入不应触发处理器通知: count=%d last=%q", obj.ChangeCount, obj.LastText)
	}
	for i := 6; i <= 7; i++ {
		frame()
		if obj.ChangeCount != 1 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
	}
	fmt.Printf("EXTERNAL-WRITE: editor=%q model=%q count=%d\n", v.input0.Text(), obj.Title, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "EXTERNAL-WRITE") {
		t.Fatalf("外部写入事实未实证:\n%s", out)
	}
}

func TestInputDivergenceMaxLenNormalize(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" MaxLen="5" ValueChanged="UpperText"/>`, frameProbePrelude+`
func TestProbeMaxLenNormalize(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abcdefgh"})
	frame()
	if obj.LastText != "abcde" {
		t.Fatalf("帧3: 载荷应为截断后文本 abcde, got %q", obj.LastText)
	}
	if obj.Title != "ABCDE" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want ABCDE/1", obj.Title, obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "ABCDE" {
		t.Fatalf("帧3: editor=%q, want ABCDE", ed)
	}
	for i := 4; i <= 6; i++ {
		frame()
		if ed := v.input0.Text(); ed != "ABCDE" {
			t.Fatalf("帧%d: editor=%q（截断场景应保持一致）", i, ed)
		}
		if obj.ChangeCount != 1 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
	}
	fmt.Printf("MAXLEN-DIVERGENCE: editor=%q model=%q count=%d\n",
		v.input0.Text(), obj.Title, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "MAXLEN-DIVERGENCE") {
		t.Fatalf("MaxLen 叠加分歧未实证:\n%s", out)
	}
	t.Log(strings.TrimSpace(out[strings.Index(out, "MAXLEN-DIVERGENCE"):]))
}

func TestInputDivergenceSubmitOrder(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText" Submit="SubmitRecord"/>`, frameProbePrelude+`
func TestProbeSubmitOrder(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "hi"})
	frame()
	if obj.Title != "HI" || obj.ChangeCount != 1 || obj.SubmitCount != 0 {
		t.Fatalf("帧3: model=%q change=%d submit=%d, want HI/1/0", obj.Title, obj.ChangeCount, obj.SubmitCount)
	}
	r.Queue(key.Event{Name: key.NameReturn})
	frame()
	if obj.SubmitCount != 1 {
		t.Fatalf("帧4: Submit 应触发 1 次: %d", obj.SubmitCount)
	}
	if obj.LastSubmit != "HI" {
		t.Fatalf("帧4: 分帧Return应提交此前显示已归一化的 HI, got %q", obj.LastSubmit)
	}
	if obj.ChangeCount != 1 {
		t.Fatalf("帧4: Return 不应再触发 ChangeEvent: %d", obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "HI" {
		t.Fatalf("帧5: editor=%q, want HI", ed)
	}
	fmt.Printf("SUBMIT-ORDER: editor=%q model=%q submit=%q counts=%d/%d\n",
		v.input0.Text(), obj.Title, obj.LastSubmit, obj.ChangeCount, obj.SubmitCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "SUBMIT-ORDER") {
		t.Fatalf("提交顺序事实未实证:\n%s", out)
	}
	t.Log(strings.TrimSpace(out[strings.Index(out, "SUBMIT-ORDER"):]))
}

func TestInputDivergenceHandlerMutatesOther(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc,
		`<Input Value="{Title}" ValueChanged="TouchOther"/><Text Value="{Other}"/>`, frameProbePrelude+`
func TestProbeMutatesOther(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abc"})
	frame()
	if obj.Other != "X-abc" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: other=%q count=%d, want X-abc/1", obj.Other, obj.ChangeCount)
	}
	if obj.Title != "" {
		t.Fatalf("帧3: Title 不应被 TouchOther 修改: %q", obj.Title)
	}
	if v.applied0 != "" {
		t.Fatalf("帧3: applied 应为模型值空串（reapply 语义）: %q", v.applied0)
	}
	for i := 4; i <= 6; i++ {
		frame()
		if ed := v.input0.Text(); ed != "" {
			t.Fatalf("帧%d: editor=%q（处理器未回写时显示应恢复模型）", i, ed)
		}
		if obj.ChangeCount != 1 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
	}
	fmt.Printf("OTHER-FIELD: editor=%q title=%q other=%q count=%d\n",
		v.input0.Text(), obj.Title, obj.Other, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("编译/运行失败:\n%s", out)
	}
	if !strings.Contains(out, "OTHER-FIELD") {
		t.Fatalf("其它字段事实未实证:\n%s", out)
	}
	t.Log(strings.TrimSpace(out[strings.Index(out, "OTHER-FIELD"):]))
}

func TestInputTwoInputsSameField(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Title}" ValueChanged="IdentityText"/>`, frameProbePrelude+`
func TestProbeTwoInputs(t *testing.T) {
 obj,v,r,frame:=divSetup(t, "")
 r.Queue(key.EditEvent{Range:key.Range{},Text:"x"});frame();frame()
 if obj.Title!="x" || v.input0.Text()!="x" || v.input1.Text()!="x" {
  t.Fatalf("both real bound editors should follow model, got %q/%q/%q",obj.Title,v.input0.Text(),v.input1.Text())
 }
 fmt.Printf("TWO-INPUTS-REAL-METHODS: in0=%q in1=%q model=%q count=%d\n",v.input0.Text(),v.input1.Text(),obj.Title,obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") || !strings.Contains(out, "TWO-INPUTS-REAL-METHODS") || strings.Contains(out, "--- SKIP") {
		t.Fatalf("real shared-field proof failed:\n%s", out)
	}
	t.Log(out)
}

func TestInputTwoInputsDistinctFields(t *testing.T) {
	vm := probeVMSrc + "\nfunc (vm *VM) OtherText(s string) { vm.Other=s }\n"
	out := rtProbeVM(t, vm, `<Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Other}" ValueChanged="OtherText"/>`, frameProbePrelude+`
func TestProbeTwoDistinctFields(t *testing.T) {
 obj,v,r,frame:=divSetup(t, "")
 r.Queue(key.EditEvent{Range:key.Range{},Text:"x"});frame();frame()
 if obj.Title!="x" || obj.Other!="" || v.input1.Text()!="" {t.Fatalf("distinct fields crossed: title=%q other=%q editor1=%q",obj.Title,obj.Other,v.input1.Text())}
 fmt.Printf("TWO-INPUTS-DISTINCT: title=%q other=%q\n",obj.Title,obj.Other)
}
`)
	if !contains(out, "[err=nil]") || !strings.Contains(out, "TWO-INPUTS-DISTINCT") || strings.Contains(out, "--- SKIP") {
		t.Fatalf("distinct field control failed:\n%s", out)
	}
	t.Log(out)
}

func TestInputMissingMethodIsNotDuplicateBindingBan(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel"><Page Route="/"><Input Value="{Title}" ValueChanged="IdentityText"/></Page></Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status == analyzer.StatusOK {
		t.Fatal("unrelated fixture must not acquire a nonexistent method")
	}
	found := false
	for _, d := range r.Diags {
		if d.Code == "GXML-EVENT-003" && strings.Contains(d.Message, "IdentityText") {
			found = true
			t.Logf("one input missing method control: %s", d.Message)
		}
	}
	if !found {
		t.Fatalf("expected exact missing-method diagnostic, got %v", r.Diags)
	}
}
