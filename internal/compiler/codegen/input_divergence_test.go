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
// R6-B 片 1：Input 标准化 / 非幂等回写 divergence 边界回归。
//
// 与旧 D3 矩阵（input_sync/acceptance/conflict）不重复：D3 覆盖
// MaxLen/Filter/Submit/外部回退/同帧冲突；这里只补「处理器改写模型」
// 的三种代表——原样（幂等恒等）、转大写（幂等标准化）、非幂等追加。
//
// 证据链与 D3 相同：真实 Analyzer→Codegen→生成的 View→Router/Editor
// 逐帧推进。区别在 ViewModel：rtProbeVM 用探测自带的 vm 子包，
// 让 UpperText/AppendText 这类 fixture 没有的**真实处理器**走完全链，
// 而不是在探针里手抄 reapply 算法。
//
// 基线缺陷链（待片 2/3 验证候选）：
//   用户 Edit → 处理器标准化 → reapply 令 A=V → 下一帧 V==A → 不 SetText
//   → Editor 仍显示用户原文，且多帧不自愈、无自纠路径。
// ============================================================================

// probeVMSrc 是探测专用 ViewModel（每例真实经 Analyzer 解析）。
const probeVMSrc = `package vm

import "strings"

// VM 是 R6-B 片 1 探测专用 ViewModel。
type VM struct {
	Title       string
	LastText    string // 最近一次 ValueChanged 载荷
	ChangeCount int
	SubmitCount int
	LastSubmit  string
	Other       string
}

// IdentityText 原样回写（代表 1：幂等恒等）。
func (vm *VM) IdentityText(s string) { vm.ChangeCount++; vm.LastText = s; vm.Title = s }

// UpperText 转大写标准化回写（代表 2：幂等标准化）。
func (vm *VM) UpperText(s string) { vm.ChangeCount++; vm.LastText = s; vm.Title = strings.ToUpper(s) }

// AppendText 非幂等追加回写（代表 3）：同一载荷再次调用结果不同。
func (vm *VM) AppendText(s string) { vm.ChangeCount++; vm.LastText = s; vm.Title += s }

// TouchOther 处理器只修改其它绑定字段、不写 Value 字段。
func (vm *VM) TouchOther(s string) { vm.ChangeCount++; vm.LastText = s; vm.Other = "X-" + s }

// SubmitRecord 记录 Submit 载荷。
func (vm *VM) SubmitRecord(s string) { vm.SubmitCount++; vm.LastSubmit = s }
`

// rtProbeVMHeaderTmpl 与 rtProbeHeader 同构，但 import 探测自带的 vm 子包。
// %s = testdata 下的临时目录名。
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

// rtProbeVM 与 rtProbe 同链（真实 Analyzer→Codegen→go test），但 ViewModel
// 指向探测自带的 vm 子包。vm 源先落盘再分析，保证 Analyzer 能解析类型。
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

// frameProbePrelude 是本文件探针共用的装配函数：构造 View、聚焦 input0，
// 推进 2 帧。返回值只暴露探针合法的观测面（vm / View / Router / 帧推进器）。
const frameProbePrelude = `
// divSetup 装配探测环境并聚焦唯一 Input。title 是 ViewModel 预设值
// （经真实首帧同步进入编辑器，探针不手工改 applied）。
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
	if !gtx.Focused(&v.input0) { t.Skip("no focus") }
	return obj, v, &r, frame
}`

// TestInputDivergenceNormalize 代表 2（幂等标准化 UpperText）：
// 用户 Edit → 模型 ABC / 编辑器 abc 的 divergence，且 3 个安静帧不自愈、
// 不重复回调、applied 已被 reapply 推进到 V（这正是不再 SetText 的原因）。
func TestInputDivergenceNormalize(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText"/>`, frameProbePrelude+`
func TestProbeNormalizeDivergence(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	// 帧3：用户输入 "abc" → ChangeEvent → UpperText
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abc"})
	frame()
	if obj.Title != "ABC" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want ABC/1", obj.Title, obj.ChangeCount)
	}
	if obj.LastText != "abc" {
		t.Fatalf("帧3: 载荷应为编辑器原文 abc, got %q", obj.LastText)
	}
	if ed := v.input0.Text(); ed != "abc" {
		t.Fatalf("帧3: editor=%q, want abc", ed)
	}
	if v.applied0 != "ABC" {
		t.Fatalf("帧3: applied=%q, want ABC（reapply 后 A=V）", v.applied0)
	}
	// 帧4-6：安静帧——divergence 持久、无重复回调
	for i := 4; i <= 6; i++ {
		frame()
		if ed := v.input0.Text(); ed != "abc" {
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

// TestInputDivergenceNormalizePreset 预设值 + 标准化处理器：
// 无任何用户输入，首帧同步（SetText→echo ChangeEvent→UpperText）后即分歧
// ——证明分歧不依赖用户 Edit，是外部应用路径自身的缺陷。
func TestInputDivergenceNormalizePreset(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText"/>`, frameProbePrelude+`
func TestProbeNormalizePreset(t *testing.T) {
	obj, v, _, frame := divSetup(t, "a")
	// divSetup 已推进 2 帧：帧1 SetText("a") → echo ChangeEvent → UpperText("a")
	// → Title="A"（count=1）→ reapply A="A"；帧2 V==A 不再 SetText。
	if obj.Title != "A" || obj.ChangeCount != 1 {
		t.Fatalf("预设值应经 UpperText 标准化: model=%q count=%d", obj.Title, obj.ChangeCount)
	}
	if v.applied0 != "A" {
		t.Fatalf("applied 应已被 reapply 推进: %q", v.applied0)
	}
	// 帧3-5：安静帧——编辑器仍显示预设原文 "a"
	for i := 3; i <= 5; i++ {
		frame()
		if ed := v.input0.Text(); ed != "a" {
			t.Fatalf("帧%d: editor=%q, want a（分歧应持续）", i, ed)
		}
		if obj.ChangeCount != 1 {
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

// TestInputDivergenceIdentity 代表 1（原样回写 IdentityText，锚点）：
// 恒等处理器不产生分歧——证明分歧来自「处理器改写模型」，不是探针伪影。
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

// TestInputDivergenceAppend 代表 3（非幂等追加 AppendText）：
// 预设 Title="A" 时首帧同步即双重应用（SetText 的 echo ChangeEvent 携带
// 同一载荷再次进处理器），模型被写坏为 "AA" 而编辑器只显示 "A"。
func TestInputDivergenceAppend(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="AppendText"/>`, frameProbePrelude+`
func TestProbeAppend(t *testing.T) {
	obj, v, r, frame := divSetup(t, "A")
	// divSetup 的帧1：SetText("A") → echo → AppendText("A") → Title="AA"
	if obj.Title != "AA" {
		t.Fatalf("首帧同步应双重应用非幂等处理器: model=%q, want AA", obj.Title)
	}
	if obj.ChangeCount != 1 {
		t.Fatalf("首帧回调应恰好 1 次: %d", obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "A" {
		t.Fatalf("编辑器应只显示应用值 A: %q", ed)
	}
	// 帧3：用户在词尾追加 "b"（编辑器 "Ab"）→ AppendText("Ab") → Title="AAAb"
	// 注意：key.EditEvent 的 Range 是替换区间，空 Range 是在 0 位插入（前缀），
	// 词尾插入必须显式给 {Start:1, End:1}。
	r.Queue(key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: "b"})
	frame()
	if obj.Title != "AAAb" || obj.ChangeCount != 2 {
		t.Fatalf("帧3: model=%q count=%d, want AAAb/2", obj.Title, obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "Ab" {
		t.Fatalf("帧3: editor=%q, want Ab", ed)
	}
	// 帧4-6：安静帧——分歧持续、回调不再发生（基线不会无限追赶，只是不再同步）
	for i := 4; i <= 6; i++ {
		frame()
		if obj.ChangeCount != 2 {
			t.Fatalf("帧%d: 回调重复触发 count=%d", i, obj.ChangeCount)
		}
		if ed := v.input0.Text(); ed != "Ab" {
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

// TestInputDivergenceExternalWrite 外部写入（VM 侧直接改模型）：
// 基线下外部写入 → SetText → echo ChangeEvent → 处理器**也被通知**
// （程序化写入触发业务回调，count=2）。这是基线契约事实，片 3 对照用。
func TestInputDivergenceExternalWrite(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/>`, frameProbePrelude+`
func TestProbeExternalWrite(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "hi"})
	frame()
	if obj.Title != "hi" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want hi/1", obj.Title, obj.ChangeCount)
	}
	// 帧4：外部写入（模拟 VM 侧其它逻辑改模型）
	obj.Title = "XYZ"
	frame()
	// 帧5：同步帧——editor 跟随模型；echo ChangeEvent 再通知处理器一次
	frame()
	if ed := v.input0.Text(); ed != "XYZ" {
		t.Fatalf("帧5: editor=%q, want XYZ（外部写入应同步显示）", ed)
	}
	if obj.ChangeCount != 2 || obj.LastText != "XYZ" {
		t.Fatalf("帧5: 外部写入应触发处理器通知: count=%d last=%q", obj.ChangeCount, obj.LastText)
	}
	for i := 6; i <= 7; i++ {
		frame()
		if obj.ChangeCount != 2 {
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

// TestInputDivergenceMaxLenNormalize MaxLen 与标准化叠加：
// 载荷是 MaxLen 截断后的编辑器文本（"abcde"），模型是它的大写（"ABCDE"），
// 编辑器停留 "abcde" ——基线在截断场景同样分歧。
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
	if ed := v.input0.Text(); ed != "abcde" {
		t.Fatalf("帧3: editor=%q, want abcde", ed)
	}
	for i := 4; i <= 6; i++ {
		frame()
		if ed := v.input0.Text(); ed != "abcde" {
			t.Fatalf("帧%d: editor=%q（截断场景分歧应持续）", i, ed)
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

// TestInputDivergenceSubmitOrder 回车提交载荷与先后顺序（Edit 与 Return 分帧）：
// 帧3 Edit 产生 ChangeEvent（标准化后 model=HI）；帧4 Return 产生 SubmitEvent，
// 载荷是**编辑器原文** "hi"（不是标准化后的模型值）——基线事实。
func TestInputDivergenceSubmitOrder(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText" Submit="SubmitRecord"/>`, frameProbePrelude+`
func TestProbeSubmitOrder(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	// 帧3：用户输入
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "hi"})
	frame()
	if obj.Title != "HI" || obj.ChangeCount != 1 || obj.SubmitCount != 0 {
		t.Fatalf("帧3: model=%q change=%d submit=%d, want HI/1/0", obj.Title, obj.ChangeCount, obj.SubmitCount)
	}
	// 帧4：Return（独立帧）
	r.Queue(key.Event{Name: key.NameReturn})
	frame()
	if obj.SubmitCount != 1 {
		t.Fatalf("帧4: Submit 应触发 1 次: %d", obj.SubmitCount)
	}
	if obj.LastSubmit != "hi" {
		t.Fatalf("帧4: Submit 载荷应为编辑器原文 hi, got %q", obj.LastSubmit)
	}
	if obj.ChangeCount != 1 {
		t.Fatalf("帧4: Return 不应再触发 ChangeEvent: %d", obj.ChangeCount)
	}
	// 帧5：安静帧——编辑器与模型的分歧仍在
	if ed := v.input0.Text(); ed != "hi" {
		t.Fatalf("帧5: editor=%q, want hi", ed)
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

// TestInputDivergenceHandlerMutatesOther 处理器只改其它绑定字段：
// Value 字段（Title）未被处理器写 → reapply 令 A=""（模型值），编辑器停留
// "abc" 且永不回同步；另一绑定字段（Other）的显示在下一帧拾取 "X-abc"。
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
		if ed := v.input0.Text(); ed != "abc" {
			t.Fatalf("帧%d: editor=%q（未被写回的 Value 字段应持续分歧）", i, ed)
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

// TestInputTwoInputsSameField uses methods that actually exist on the same
// probe VM used for analysis. An EVENT-003 from an unrelated fixture's missing
// method must not be mistaken for a ban on two editors binding one field.
func TestInputTwoInputsSameField(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Title}" ValueChanged="IdentityText"/>`, strings.ReplaceAll(frameProbePrelude, `t.Skip("no focus")`, `t.Fatal("no focus")`)+`
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
	out := rtProbeVM(t, vm, `<Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Other}" ValueChanged="OtherText"/>`, strings.ReplaceAll(frameProbePrelude, `t.Skip("no focus")`, `t.Fatal("no focus")`)+`
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
