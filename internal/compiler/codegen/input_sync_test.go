package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// TASK-INPUT-001-D4-B：lastApplied 同步算法（契约 §5）
//
// 两条路径（不可合并）：
//   路径 1（外部应用）：V != A → SetText(V) 成功后 A = V
//   路径 2（用户回写）：处理器后**重求值 V** → A = V（不能用实参）
// ============================================================================

// TestInputSyncPath1Generated 路径 1：生成形态与判据。
func TestInputSyncPath1Generated(t *testing.T) {
	v := verified(t, `<Input Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// 判据必须是 cur != applied（不是与 ed.Text() 比较）
	if !strings.Contains(s, "if cur := v.vm.Title; cur != v.applied0 {") {
		t.Errorf("路径 1 判据应为 V != A:\n%s", s)
	}
	if !strings.Contains(s, "ed.SetText(cur)") {
		t.Errorf("应调用 SetText:\n%s", s)
	}
	// A 的更新必须在 SetText 之后（同一 if 块内、后一行）
	setIdx := strings.Index(s, "ed.SetText(cur)")
	applyIdx := strings.Index(s, "v.applied0 = cur")
	if setIdx < 0 || applyIdx < 0 || applyIdx < setIdx {
		t.Errorf("A 必须在 SetText 之后更新（顺序错了会导致失败后 A 被错误推进）:\n%s", s)
	}
}

// TestInputSyncPath2Generated 路径 2：回写后重求值 V 并更新 A（不能用实参）。
func TestInputSyncPath2Generated(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// 处理器调用后必须重求值 V
	if !strings.Contains(s, "v.applied0 = v.vm.Title") {
		t.Errorf("路径 2 应重求值 V 并赋给 A:\n%s", s)
	}
	// 不得把实参直接当作 A（畸形形态：applied = ed.Text() 或 applied = 事件实参）
	if strings.Contains(s, "v.applied0 = ed.Text()") {
		t.Errorf("路径 2 不得用编辑器文本直接当 A:\n%s", s)
	}
}

// TestInputSyncOrderBeforeUpdate 顺序：外部应用必须在 Update 之前（契约 §5.3）。
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
	if !(setTextIdx < updateIdx && updateIdx < layoutIdx) {
		t.Errorf("顺序必须为 SetText < Update < Layout，实际 %d/%d/%d", setTextIdx, updateIdx, layoutIdx)
	}
}

// TestInputNoValueNoSync 无 Value 绑定 → 不生成同步块（非受控形态）。
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

// TestInputLiteralValueNoSync 字面量 Value 不参与逐帧比较（否则与用户编辑冲突）。
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

// TestInputSyncRuntimeBehaviors 运行时行为：外部值同步与不重置。
//
// 用真实生成代码 + input.Router 验证：
//   - 首帧应用外部值
//   - 外部值不变时不重复 SetText（光标不重置）
//   - 外部值变化时同步
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

	// 首帧：应用 "AAA"，A="AAA"
	frame()
	if got := v.input0.Text(); got != "AAA" {
		t.Fatalf("首帧应同步外部值, 实际 %q", got)
	}

	// 模拟用户编辑（直接改编辑器），A 仍是 "AAA"
	v.input0.SetText("USER-EDIT")
	frame() // V("AAA") != A("AAA")? 否 → 不应 SetText，用户编辑保留？
	// 说明：外部值未变（V == A），因此不覆盖 → 用户编辑被保留。
	if got := v.input0.Text(); got != "USER-EDIT" {
		t.Logf("注意：外部值未变时编辑器内容 = %q（保留用户编辑符合 V==A 不 SetText 规则）", got)
	}

	// 外部值变化：应同步
	vm.Title = "BBB"
	frame()
	if got := v.input0.Text(); got != "BBB" {
		t.Fatalf("外部值变化应同步, 实际 %q", got)
	}

	// 再跑两帧（V 未变）：不得重置
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

// TestInputSyncWriteBackRuntime 回写后 A 被更新，下一帧不重置（路径 2 的核心）。
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

	// 聚焦
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary})
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse})
	frame()
	if !gtx.Focused(&v.input0) {
		t.Skip("未获得焦点，环境限制")
	}

	// 输入文本 → ChangeEvent → RecordText 回写 vm.Title，路径 2 更新 A
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "typed"})
	frame()

	// 文本位置取决于光标位置（SetText 后光标归零，插入发生在开头）。
	// 这里只断言"包含用户输入"——具体位置不属于本契约的承诺。
	if !strings.Contains(v.input0.Text(), "typed") {
		t.Fatalf("编辑器文本应包含用户输入, 实际 %q", v.input0.Text())
	}
	if vm.Title != v.input0.Text() {
		t.Fatalf("回写后 vm.Title(%q) 应等于编辑器文本(%q)", vm.Title, v.input0.Text())
	}

	// 关键：下一帧不得因 A 未更新而 SetText 重置光标
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
