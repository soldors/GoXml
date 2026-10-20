package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// TASK-INPUT-001-D4-A：Input 结构与事件接线
//
// 本切片不实现 lastApplied 同步算法（D4-B）。因此测试**不断言**外部值同步，
// 只断言：字段持久、自持消费位置、载荷接线、属性透传、错误守卫、可编译。
// ============================================================================

// TestInputCodegenStructure 结构断言（A1/A2/A3/A6）。
//
// 注意：本用例**不声明事件**，因此不生成 Update/消费块——
// 「无事件不消费」本身是正确行为（契约 §4.2）。
func TestInputCodegenStructure(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" Hint="请输入" SingleLine="false" ReadOnly="true" MaxLen="20" Filter="abc"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	for _, want := range []string{
		// A1：Editor 是实例字段（跨帧稳定），不是 Layout 局部变量
		"input0   widget.Editor",
		"applied0 string", // A6：lastApplied 占位
		// A2：属性透传
		`ed.SingleLine = false`,
		`ed.ReadOnly = true`,
		`ed.MaxLen = 20`,
		`ed.Filter = "abc"`,
		// Hint 走 material.Editor 第三参
		`material.Editor(th, ed, "请输入")`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("生成代码缺 %q:\n%s", want, s)
		}
	}
}

// TestInputNoEventsNoUpdate 未声明事件时不生成消费块（契约 §4.2：都不声明则只读镜像）。
func TestInputNoEventsNoUpdate(t *testing.T) {
	v := verified(t, `<Input Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "ed.Update(gtx)") {
		t.Errorf("未声明事件时不应生成事件消费:\n%s", out)
	}
}

// TestInputWithEventHasUpdate 声明事件时必须生成控件自持消费。
func TestInputWithEventHasUpdate(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "ed.Update(gtx)") {
		t.Errorf("声明事件应生成 ed.Update(gtx):\n%s", s)
	}
	if !strings.Contains(s, "case widget.ChangeEvent:") {
		t.Errorf("应生成 ChangeEvent 分支:\n%s", s)
	}
}

// TestInputNotPolledAtTop 关键约束：Input 事件**不得**进 Layout 顶部轮询。
//
// 判据：Layout 顶部（首个 return 之前）不得出现 ed.Update / ChangeEvent。
func TestInputNotPolledAtTop(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	layoutStart := strings.Index(s, "func (v *GoxmlDashboardView) Layout(")
	if layoutStart < 0 {
		t.Fatal("未找到 Layout")
	}
	// 页面根 return 之前的部分即"顶部"
	bodyStart := strings.Index(s[layoutStart:], "return ")
	if bodyStart < 0 {
		t.Fatal("未找到 return")
	}
	top := s[layoutStart : layoutStart+bodyStart]

	if strings.Contains(top, "ed.Update") || strings.Contains(top, "ChangeEvent") {
		t.Errorf("Input 事件不得进顶部轮询（应在控件位置）:\n%s", top)
	}
}

// TestInputChangePayloadIsEditorText ValueChanged 载荷 = ed.Text()。
func TestInputChangePayloadIsEditorText(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "v.vm.OnNameChanged(ed.Text())") {
		t.Errorf("ValueChanged 应传 ed.Text():\n%s", s)
	}
}

// TestInputSubmitPayloadIsEventText Submit 载荷 = 事件自身的 Text，且不等于 Change 的。
func TestInputSubmitPayloadIsEventText(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged" Submit="OnEcho"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	if !strings.Contains(s, "v.vm.OnNameChanged(ed.Text())") {
		t.Errorf("Change 载荷应为 ed.Text():\n%s", s)
	}
	if !strings.Contains(s, "v.vm.OnEcho(e.Text)") {
		t.Errorf("Submit 载荷应为事件 e.Text:\n%s", s)
	}
	// 两事件分别接线，不得混淆
	if strings.Contains(s, "v.vm.OnEcho(ed.Text())") {
		t.Errorf("Submit 不得复用 Change 的载荷:\n%s", s)
	}
}

// TestInputSubmitEnablesEditorSubmit Submit 声明才开启 Editor.Submit。
func TestInputSubmitEnablesEditorSubmit(t *testing.T) {
	t.Run("声明Submit", func(t *testing.T) {
		v := verified(t, `<Input Value="{Title}" Submit="OnEcho"/>`)
		out, err := Generate(v, bindingOpts())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "ed.Submit = true") {
			t.Errorf("声明 Submit 事件应置 ed.Submit=true:\n%s", out)
		}
	})
	t.Run("未声明Submit", func(t *testing.T) {
		v := verified(t, `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`)
		out, err := Generate(v, bindingOpts())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "ed.Submit = true") {
			t.Errorf("未声明 Submit 时不得开启提交:\n%s", out)
		}
	})
}

// TestInputRejectsEnabled Enabled 沿用 Button 裁决：明确报错。
func TestInputRejectsEnabled(t *testing.T) {
	v := verified(t, `<Input Value="{Title}" Enabled="false"/>`)
	_, err := Generate(v, bindingOpts())
	if err == nil {
		t.Fatal("Enabled 应报错")
	}
	if !strings.Contains(err.Error(), "Enabled") {
		t.Errorf("错误应点名 Enabled: %v", err)
	}
}

// TestInputRejectsBadLiterals 静态字面量必须校验（Analyzer 不校验非绑定属性）。
func TestInputRejectsBadLiterals(t *testing.T) {
	cases := []struct{ gxml, want string }{
		{`<Input Value="{Title}" ReadOnly="yes"/>`, "true"},
		{`<Input Value="{Title}" SingleLine="1"/>`, "true"},
		{`<Input Value="{Title}" MaxLen="-1"/>`, "non-negative"},
		{`<Input Value="{Title}" MaxLen="abc"/>`, "non-negative"},
	}
	for _, c := range cases {
		t.Run(c.gxml, func(t *testing.T) {
			v := verified(t, c.gxml)
			_, err := Generate(v, bindingOpts())
			if err == nil {
				t.Fatal("非法字面量应报错")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("错误信息应含 %q: %v", c.want, err)
			}
		})
	}
}

// TestInputGeneratedCodeCompiles A6：生成代码真实编译（含事件接线）。
func TestInputGeneratedCodeCompiles(t *testing.T) {
	out := rtProbe(t, `<Column><Input Value="{Title}" Hint="请输入" MaxLen="20" ValueChanged="OnNameChanged" Submit="OnEcho"/></Column>`, `
func TestProbeInputCompiles(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "hello"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 120)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	dims := v.Layout(gtx, th)
	if dims.Size.Y <= 0 {
		t.Fatalf("Input 应占据高度, 实际 %d", dims.Size.Y)
	}
	t.Log("含 Input 与两个事件的生成代码编译并渲染")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("Input 生成代码编译/渲染失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestInputPageNotFrozen A9：含 Input 的页面不得整页固化。
func TestInputPageNotFrozen(t *testing.T) {
	v := verified(t, `<Input Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"v.cacheCall.Add", "op.Record(&v.cacheOps)"} {
		if strings.Contains(s, bad) {
			t.Errorf("含 Input 的页面不得整页固化，出现 %q", bad)
		}
	}
}

// TestButtonPollingUnaffected A8：Button 的顶部轮询不受 Input 接入影响。
func TestButtonPollingUnaffected(t *testing.T) {
	v := verified(t, `<Column><Button Text="保存" Click="Increment"/><Input Value="{Title}"/></Column>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	layoutStart := strings.Index(s, "func (v *GoxmlDashboardView) Layout(")
	bodyStart := strings.Index(s[layoutStart:], "return ")
	top := s[layoutStart : layoutStart+bodyStart]

	if !strings.Contains(top, "for v.click0.Clicked(gtx)") {
		t.Errorf("Button 仍应在 Layout 顶部轮询:\n%s", top)
	}
	// Input 的 Update 不得出现在顶部
	if strings.Contains(top, "ed.Update") {
		t.Errorf("Input 不得进顶部轮询:\n%s", top)
	}
}
