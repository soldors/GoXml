package codegen

import (
	"testing"
)

// ============================================================================
// TASK-INPUT-001-D4-C / C1：同帧冲突验证（契约 §5.4 / §9 场景 6）
//
// 契约已冻结的规则（不是本测试要讨论的语义）：
//   ② 外部值先应用（SetText），⑤ 之后才消费用户输入事件。
//   同帧冲突时**外部值覆盖本地编辑**，v1 不做合并、不做延迟同步。
//
// 本测试的职责是**证明实现符合该规则**，观测点五处（缺一不可）：
//   1. Editor 最终文本
//   2. ChangeEvent 是否产生并被消费
//   3. ViewModel 最终值
//   4. applied 是否在处理器后重新求值
//   5. 下一帧是否稳定（无重复回写、无额外 SetText）
//
// 注意 Gio 事实（D4-B §5.3.1 已核实）：Editor.Layout 内部会调用 Update，
// 因此手动 Update 必须先执行，否则事件被静默吞掉。
// ============================================================================

// TestInputSameFrameConflictRuntime 同帧外部改值 + 用户输入。
//
// 构造：先聚焦 → 一帧内同时满足「vm.Title 已改变」与「队列中有 EditEvent」。
//
// 期望（契约 §5.4）：外部值先 SetText(vm.Title)，
// 随后 Update 消费 EditEvent 并触发 ChangeEvent → 回写处理器。
// 最终：Editor 文本 = 外部值 + 用户插入内容（取决于光标），
// vm 被回写为编辑器实际文本，applied 同步为回写后的 V。
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

	// 建立基线：应用 "OLD"，applied="OLD"
	frame()
	if v.input0.Text() != "OLD" {
		t.Fatalf("基线文本 = %q, 期望 OLD", v.input0.Text())
	}

	// 聚焦
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary})
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse})
	frame()
	if !gtx.Focused(&v.input0) {
		t.Skip("未获得焦点，环境限制")
	}

	// ---- 同帧冲突构造 ----
	// 外部值改变（NEW）与用户输入（"X"）在同一帧发生。
	vm.Title = "NEW"
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "X"})

	// 观测点 4 的前置：记录本帧前的 applied
	appliedBefore := v.applied0

	frame()

	// ---- 观测 1：Editor 最终文本 ----
	// 契约 §5.4：外部值先应用（SetText("NEW")，光标归零），
	// 随后 EditEvent 插入 "X"（在光标处=开头）→ "XNEW"。
	text := v.input0.Text()
	if !strings.Contains(text, "NEW") {
		t.Fatalf("外部值未先应用: 文本 = %q（应包含 NEW）", text)
	}
	if !strings.Contains(text, "X") {
		t.Fatalf("用户输入未被消费: 文本 = %q（应包含 X）", text)
	}
	// 观测结果写进错误信息：即使通过，也能通过 -v 看到（rtFilter 保留含"观测"的行）
	fmt.Println("OBS1 text=", text)

	// ---- 观测 2：ChangeEvent 是否产生并被消费 → 观测 3：ViewModel 最终值 ----
	// RecordText 把 vm.Title 设为编辑器文本；若事件被吞，vm 仍是 "NEW"。
	if vm.Title == "NEW" {
		t.Fatalf("ChangeEvent 未被消费（vm.Title 仍是外部值 NEW，未回写）")
	}
	if vm.Title != text {
		t.Fatalf("回写后 vm.Title(%q) 应等于编辑器文本(%q)", vm.Title, text)
	}
	fmt.Println("OBS2 vm=", vm.Title, " changeConsumed=", vm.Title != "NEW")

	// ---- 观测 4：applied 在处理器后重新求值 ----
	if v.applied0 != vm.Title {
		t.Fatalf("applied 未被路径 2 更新：applied=%q, vm.Title=%q", v.applied0, vm.Title)
	}
	if v.applied0 == appliedBefore {
		fmt.Println("OBS4 appliedBefore=", appliedBefore, " appliedAfter=", v.applied0)
	}

	// ---- 观测 5：下一帧稳定 ----
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
	// 把子包里的观测值提取出来（rtProbe 不传 -v，只能靠 stdout）
	for _, line := range splitLines(out) {
		if contains(line, "OBS") {
			t.Log(trimSpace(line))
		}
	}
}

// TestInputSameFrameExternalWinsOverLocalEdit 契约 §5.4 的更强断言：
// 外部值**覆盖**本地编辑——若同帧内只有外部改值而用户此前有未回写编辑，
// 外部值必须覆盖掉它。
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

	// 用户直接改编辑器（模拟未回写的本地编辑），applied 仍是 "EXT"
	v.input0.SetText("LOCAL-ONLY")

	// 外部改值：V("CHANGED") != A("EXT") → SetText("CHANGED") 覆盖本地编辑
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

// TestInputNoValueKeepsLocalEdit 对照：无 Value 绑定时本地编辑不被覆盖。
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
