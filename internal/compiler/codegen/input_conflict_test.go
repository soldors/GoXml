// R8批准后：事件消费先于外部应用，事件后A=E；程序化静默、用户优先。
package codegen

import (
	"testing"
)

func TestInputSameFrameConflictRuntime(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}" ValueChanged="RecordText"/>`, `
func TestProbeSameFrame(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "OLD"}
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

	frame()
	if v.input0.Text() != "OLD" {
		t.Fatalf("基线文本 = %q, 期望 OLD", v.input0.Text())
	}

	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary})
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse})
	frame()
	if !gtx.Focused(&v.input0) {
		t.Fatal("未获得焦点")
	}

	vm.Title = "NEW"
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "X"})

	appliedBefore := v.applied0

	frame()

	text := v.input0.Text()
	if text != "XOLD" {
		t.Fatalf("用户编辑未优先: 文本 = %q（应精确为 XOLD）", text)
	}
	if !strings.Contains(text, "X") {
		t.Fatalf("用户输入未被消费: 文本 = %q（应包含 X）", text)
	}
	fmt.Println("OBS1 text=", text)

	if vm.Title == "NEW" {
		t.Fatalf("ChangeEvent 未被消费（vm.Title 仍是外部值 NEW，未回写）")
	}
	if vm.Title != text {
		t.Fatalf("回写后 vm.Title(%q) 应等于编辑器文本(%q)", vm.Title, text)
	}
	fmt.Println("OBS2 vm=", vm.Title, " changeConsumed=", vm.Title != "NEW")

	if v.applied0 != vm.Title {
		t.Fatalf("applied 未被路径 2 更新：applied=%q, vm.Title=%q", v.applied0, vm.Title)
	}
	if v.applied0 == appliedBefore {
		fmt.Println("OBS4 appliedBefore=", appliedBefore, " appliedAfter=", v.applied0)
	}

	stable := v.input0.Text()
	frame()
	frame()
	if got := v.input0.Text(); got != stable {
		t.Fatalf("稳定帧被重置：%q → %q", stable, got)
	}
	if vm.Title != stable {
		t.Fatalf("稳定帧后 vm.Title 再次改变：%q → %q（存在重复回写）", stable, vm.Title)
	}
	fmt.Println("OBS5 stable=", stable, " applied=", v.applied0)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("同帧冲突验证失败:\n%s", out)
	}
	for _, line := range splitLines(out) {
		if contains(line, "OBS") {
			t.Log(trimSpace(line))
		}
	}
}

func TestInputSameFrameExternalWinsOverLocalEdit(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}"/>`, `
func TestProbeExternalWins(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "EXT"}
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

	v.input0.SetText("LOCAL-ONLY")

	vm.Title = "CHANGED"
	frame()

	if got := v.input0.Text(); got != "CHANGED" {
		t.Fatalf("外部值应覆盖本地编辑, 实际 %q", got)
	}
	t.Log("外部值覆盖本地编辑（契约 §5.4：不合并、不延迟）")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("外部覆盖验证失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

func TestInputNoValueKeepsLocalEdit(t *testing.T) {
	out := rtProbe(t, `<Input Hint="h" ValueChanged="RecordText"/>`, `
func TestProbeNoValueKeeps(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
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
	v.input0.SetText("LOCAL")
	frame()
	frame()
	if got := v.input0.Text(); got != "LOCAL" {
		t.Fatalf("无 Value 绑定时本地编辑应保留, 实际 %q", got)
	}
	t.Log("非受控形态：无 Value 绑定，本地编辑不被覆盖")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("非受控形态验证失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
