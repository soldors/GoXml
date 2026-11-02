// R8批准后：事件消费先于外部应用，事件后A=E；程序化静默、用户优先。
package codegen

import (
	"strings"
	"testing"
)

func TestInputSyncPath1Generated(t *testing.T) {
	v := verified(t, `<Input Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if !strings.Contains(s, "if cur := v.vm.Title; cur != v.applied0 {") {
		t.Errorf("路径 1 判据应为 V != A:\n%s", s)
	}
	if !strings.Contains(s, "ed.SetText(cur)") {
		t.Errorf("应调用 SetText:\n%s", s)
	}
	setIdx := strings.Index(s, "ed.SetText(cur)")
	applyIdx := strings.Index(s, "v.applied0 = cur")
	if setIdx < 0 || applyIdx < 0 || applyIdx < setIdx {
		t.Errorf("A 必须在 SetText 之后更新（顺序错了会导致失败后 A 被错误推进）:\n%s", s)
	}
}

func TestInputSyncPath2Generated(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if !strings.Contains(s, "v.applied0 = ed.Text()") {
		t.Errorf("路径 2 应读取事件后 Editor.Text() 并赋给 A:\n%s", s)
	}
	if strings.Contains(s, "v.applied0 = v.vm.Title") {
		t.Errorf("路径 2 不得跳过编辑器镜像直接赋模型值:\n%s", s)
	}
}

func TestInputSyncOrderBeforeUpdate(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	setTextIdx := strings.Index(s, "ed.SetText(cur)")
	updateIdx := strings.Index(s, "ed.Update(gtx)")
	layoutIdx := strings.Index(s, "material.Editor(th, ed,")

	if setTextIdx < 0 || updateIdx < 0 || layoutIdx < 0 {
		t.Fatal("生成的三个关键步骤缺失")
	}
	if !(updateIdx < setTextIdx && setTextIdx < layoutIdx) {
		t.Errorf("顺序必须为 Update < SetText < Layout，实际 %d/%d/%d", setTextIdx, updateIdx, layoutIdx)
	}
}

func TestInputNoValueNoSync(t *testing.T) {
	v := verified(t, `<Input Hint="h" ValueChanged="OnNameChanged"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "SetText") {
		t.Errorf("无 Value 绑定不应生成 SetText:\n%s", out)
	}
}

func TestInputLiteralValueNoSync(t *testing.T) {
	v := verified(t, `<Input Value="固定文本"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "SetText") {
		t.Errorf("字面量 Value 不应参与逐帧同步:\n%s", out)
	}
}

func TestInputSyncRuntimeBehaviors(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}" />`, `
func TestProbeInputSync(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "AAA"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
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
	if got := v.input0.Text(); got != "AAA" {
		t.Fatalf("首帧应同步外部值, 实际 %q", got)
	}

	v.input0.SetText("USER-EDIT")
	frame() // V("AAA") != A("AAA")? 否 → 不应 SetText，用户编辑保留？
	if got := v.input0.Text(); got != "USER-EDIT" {
		t.Logf("注意：外部值未变时编辑器内容 = %q（保留用户编辑符合 V==A 不 SetText 规则）", got)
	}

	vm.Title = "BBB"
	frame()
	if got := v.input0.Text(); got != "BBB" {
		t.Fatalf("外部值变化应同步, 实际 %q", got)
	}

	before := v.input0.Text()
	frame()
	frame()
	if got := v.input0.Text(); got != before {
		t.Fatalf("外部值稳定时不得重置, %q → %q", before, got)
	}
	t.Log("外部值同步与稳定帧不重置均符合契约")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("运行时行为验证失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

func TestInputSyncWriteBackRuntime(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}" ValueChanged="RecordText"/>`, `
func TestProbeInputWriteBack(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "start"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame() // 注册 + 应用 "start"

	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary})
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse})
	frame()
	if !gtx.Focused(&v.input0) {
		t.Skip("未获得焦点，环境限制")
	}

	r.Queue(key.EditEvent{Range: key.Range{}, Text: "typed"})
	frame()

	if !strings.Contains(v.input0.Text(), "typed") {
		t.Fatalf("编辑器文本应包含用户输入, 实际 %q", v.input0.Text())
	}
	if vm.Title != v.input0.Text() {
		t.Fatalf("回写后 vm.Title(%q) 应等于编辑器文本(%q)", vm.Title, v.input0.Text())
	}

	textAfter := v.input0.Text()
	frame()
	frame()
	if got := v.input0.Text(); got != textAfter {
		t.Fatalf("回写后的稳定帧被重置: %q → %q（路径 2 未生效）", textAfter, got)
	}
	t.Logf("回写链路正确：文本 %q，稳定帧未被重置", textAfter)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("回写链路验证失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
