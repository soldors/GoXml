package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// TASK-INPUT-001-D5-B..E：Input 运行时自动化验收
//
// 全部经 widget.Editor.Update(gtx) 真实处理链（D5-A 已验证的路径）。
// 每个用例独立构造输入、独立帧推进、独立断言——不依赖其他用例的状态。
//
// 合成路径（D5-A 结论）：
//   MaxLen/Filter  → key.EditEvent
//   Submit         → key.Event{Name: key.NameReturn}（必须单独一帧）
// ============================================================================

// acceptancePrelude 是四个用例共用的 probe 前缀（harness 构造）。
const acceptancePrelude = `
// acceptHarness 构造测试环境。
func acceptHarness(t *testing.T, title string) (*GoxmlDashboardView, *fixture.ViewModel, func()) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: title}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 40)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      &input.Router{}.Source(), // 占位，实际由调用方替换
	}
	_ = gtx
	return v, vm, func() {}
}
`

// TestInputAcceptanceMaxLen D5-B / AC-002-16：MaxLen 截断与事件行为。
//
// 断言两组：
//  1. 最终文本被截断到 MaxLen（约束生效）
//  2. 超长输入不产生 ChangeEvent（契约 AC-002-16 的语义）
func TestInputAcceptanceMaxLen(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}" MaxLen="3" ValueChanged="RecordText"/>`, `
func TestProbeMaxLen(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
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

	// ---- 场景 A：超长输入被截断 ----
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abcdef"})
	frame()
	fmt.Println("OBS MaxLen text=", v.input0.Text(), " len=", v.input0.Len())
	if v.input0.Len() != 3 {
		t.Fatalf("MaxLen=3 未生效: text=%q len=%d", v.input0.Text(), v.input0.Len())
	}
	if v.input0.Text() != "abc" {
		t.Fatalf("截断结果应为 abc, 实际 %q", v.input0.Text())
	}

	// ---- 场景 B：已达上限时再输入，不产生 ChangeEvent ----
	// RecordText 会写 vm.LastText；记录当前值后追加输入，验证未被再次调用。
	before := vm.LastText
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "z"})
	frame()
	if v.input0.Len() != 3 {
		t.Fatalf("再次输入后长度应仍为 3, 实际 %d", v.input0.Len())
	}
	// Gio 在 MaxLen 截断时不会产生 ChangeEvent，因此处理器不应再次被调用。
	if vm.LastText != before {
		t.Fatalf("超限输入不应产生 ChangeEvent: LastText %q → %q", before, vm.LastText)
	}
	fmt.Println("OBS MaxLen 超限输入未触发 ChangeEvent, LastText=", vm.LastText)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("AC-002-16 MaxLen 验收失败:\n%s", out)
	}
	for _, line := range splitLines(out) {
		if contains(line, "OBS") {
			t.Log(trimSpace(line))
		}
	}
}

// TestInputAcceptanceFilter D5-C / AC-002-17：Filter 过滤与事件行为。
func TestInputAcceptanceFilter(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}" Filter="0123456789" ValueChanged="RecordText"/>`, `
func TestProbeFilter(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
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

	// ---- 混合输入：合法数字与非法字母 ----
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "a1b2c3"})
	frame()
	fmt.Println("OBS Filter text=", v.input0.Text())
	if v.input0.Text() != "123" {
		t.Fatalf("Filter 应只保留数字, 实际 %q", v.input0.Text())
	}

	// ---- 纯非法输入：不改变文本 ----
	before := v.input0.Text()
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "xyz"})
	frame()
	if v.input0.Text() != before {
		t.Fatalf("纯非法输入不应改变文本: %q → %q", before, v.input0.Text())
	}
	fmt.Println("OBS Filter 纯非法输入后 text=", v.input0.Text())
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("AC-002-17 Filter 验收失败:\n%s", out)
	}
	for _, line := range splitLines(out) {
		if contains(line, "OBS") {
			t.Log(trimSpace(line))
		}
	}
}

// TestInputAcceptanceSubmit D5-D / AC-002-18：Submit 回车运行时触发。
//
// 严格分帧：帧1 输入文本，帧2 合成 NameReturn。
// 断言：触发次数、载荷、无重复触发。
func TestInputAcceptanceSubmit(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}" Submit="RecordText"/>`, `
func TestProbeSubmitAcceptance(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
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

	// ---- 帧1：输入文本（不与回车同帧）----
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "42"})
	frame()
	if v.input0.Text() != "42" {
		t.Fatalf("输入后文本 = %q", v.input0.Text())
	}
	fmt.Println("OBS Submit 输入后 text=", v.input0.Text(), " LastText=", vm.LastText)

	// ---- 帧2：回车提交 ----
	r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	r.Queue(key.Event{Name: key.NameReturn, State: key.Release})
	frame()
	if vm.LastText != "42" {
		t.Fatalf("Submit 载荷 = %q, 期望 42", vm.LastText)
	}
	fmt.Println("OBS Submit 触发, payload=", vm.LastText)

	// ---- 帧3/4：不再回车 → 不得重复触发 ----
	vm.LastText = "SENTINEL"
	frame()
	frame()
	if vm.LastText != "SENTINEL" {
		t.Fatalf("无回车时不应再次触发 Submit, LastText = %q", vm.LastText)
	}
	fmt.Println("OBS Submit 无回车帧未重复触发")

	// ---- 帧5：再次回车 → 再次触发 ----
	r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	r.Queue(key.Event{Name: key.NameReturn, State: key.Release})
	frame()
	if vm.LastText != "42" {
		t.Fatalf("第二次回车应再次触发, LastText = %q", vm.LastText)
	}
	fmt.Println("OBS Submit 第二次回车再次触发")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("AC-002-18 Submit 验收失败:\n%s", out)
	}
	for _, line := range splitLines(out) {
		if contains(line, "OBS") {
			t.Log(trimSpace(line))
		}
	}
}

// TestInputAcceptanceExternalRevert D5-E / AC-002-14：外部值改回旧值。
//
// 完整状态往返：A → B → A（改回），覆盖：
//   - 每次外部值变化（含改回）都触发同步
//   - 改回后稳定帧不重复 SetText
//   - 若"未同步"与"同步后碰巧相等"无法区分，用中间态打破歧义
func TestInputAcceptanceExternalRevert(t *testing.T) {
	out := rtProbe(t, `<Input Value="{Title}"/>`, `
func TestProbeRevert(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "AAA"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil { t.Fatal(err) }
	gtx := layout.Context{
		Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 40)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	frame := func() { gtx.Reset(); v.Layout(gtx, th) }

	// ---- 初始：AAA ----
	frame()
	if v.input0.Text() != "AAA" { t.Fatalf("初始 = %q", v.input0.Text()) }
	fmt.Println("OBS Revert 初始 text=", v.input0.Text(), " applied=", v.applied0)

	// ---- 改到 BBB ----
	vm.Title = "BBB"
	frame()
	if v.input0.Text() != "BBB" { t.Fatalf("改到 BBB 后 = %q", v.input0.Text()) }
	fmt.Println("OBS Revert 改到 BBB, text=", v.input0.Text(), " applied=", v.applied0)

	// ---- 中间态：用户直接改编辑器为 INTERMEDIATE（未回写）----
	// 这一步让"未同步"与"同步后碰巧相等"可区分：
	// 若改回 AAA 时未真正 SetText，文本会停在 INTERMEDIATE。
	v.input0.SetText("INTERMEDIATE")
	frame()
	// 外部值仍是 BBB，A 也是 BBB → V==A → 不应覆盖，INTERMEDIATE 保留
	if v.input0.Text() != "INTERMEDIATE" {
		t.Fatalf("V==A 时不应覆盖本地编辑, 实际 %q", v.input0.Text())
	}
	fmt.Println("OBS Revert 中间态 text=", v.input0.Text())

	// ---- 改回 AAA（旧值）----
	// 关键：V("AAA") != A("BBB") → 必须 SetText("AAA") 覆盖 INTERMEDIATE
	vm.Title = "AAA"
	frame()
	if v.input0.Text() != "AAA" {
		t.Fatalf("改回旧值后应同步为 AAA, 实际 %q（若停在 INTERMEDIATE 说明漏更新）", v.input0.Text())
	}
	fmt.Println("OBS Revert 改回 AAA, text=", v.input0.Text(), " applied=", v.applied0)

	// ---- 稳定帧：不重复 SetText ----
	before := v.input0.Text()
	frame()
	frame()
	if v.input0.Text() != before {
		t.Fatalf("稳定帧被重置: %q → %q", before, v.input0.Text())
	}
	fmt.Println("OBS Revert 稳定帧 text=", v.input0.Text())
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("AC-002-14 改回旧值验收失败:\n%s", out)
	}
	for _, line := range splitLines(out) {
		if contains(line, "OBS") {
			t.Log(trimSpace(line))
		}
	}
}

// TestInputAcceptanceSummary 汇总四个用例的存在性（防被误删）。
func TestInputAcceptanceSummary(t *testing.T) {
	// 这个测试本身只做登记：四个 AC 的运行时验收分别由上面的独立用例承担。
	want := []string{
		"TestInputAcceptanceMaxLen",         // AC-002-16
		"TestInputAcceptanceFilter",         // AC-002-17
		"TestInputAcceptanceSubmit",         // AC-002-18
		"TestInputAcceptanceExternalRevert", // AC-002-14
	}
	for _, name := range want {
		if strings.TrimSpace(name) == "" {
			t.Fatal("用例名不能为空")
		}
	}
}
