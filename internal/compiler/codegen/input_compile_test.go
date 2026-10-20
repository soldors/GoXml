package codegen

import (
	"testing"
)

// ============================================================================
// TASK-INPUT-001-D4-C / C2：生成代码的编译门禁
//
// 背景：D4-B 出现过两次**只有 go build 才能发现**的缺陷
//   （receiver 遮蔽、`e declared and not used`）。
// 因此每种事件组合都必须真实编译，不能只做字符串断言。
//
// 覆盖四种组合：仅 ValueChanged / 仅 Submit / 两者都有 / 都没有。
// ============================================================================

// TestInputCompileMatrix 四种事件组合全部真实编译。
func TestInputCompileMatrix(t *testing.T) {
	cases := []struct {
		name string
		gxml string
	}{
		{"仅Change", `<Input Value="{Title}" ValueChanged="OnNameChanged"/>`},
		{"仅Submit", `<Input Value="{Title}" Submit="OnEcho"/>`},
		{"两者都有", `<Input Value="{Title}" ValueChanged="OnNameChanged" Submit="OnEcho"/>`},
		{"都没有", `<Input Value="{Title}"/>`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// rtProbe 会在仓库内临时包中真实 go build + go test
			out := rtProbe(t, c.gxml, `
func TestProbeCompile(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "t"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	v.Layout(gtx, th)
}
`)
			if !contains(out, "[err=nil]") {
				t.Fatalf("%s 组合编译失败:\n%s", c.name, out)
			}
		})
	}
}

// TestInputCompileWithAttrs 属性透传组合的编译门禁。
//
// 覆盖 Hint/ReadOnly/SingleLine/MaxLen/Filter 全部同时出现。
func TestInputCompileWithAttrs(t *testing.T) {
	out := rtProbe(t,
		`<Input Value="{Title}" Hint="提示" ReadOnly="false" SingleLine="true" MaxLen="30" Filter="abc" ValueChanged="OnNameChanged"/>`,
		`
func TestProbeCompileAttrs(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "t"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	v.Layout(gtx, th)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("全属性组合编译失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestInputCompileInContainers 组合回归：Input 在 Column/Row/If 中的编译。
func TestInputCompileInContainers(t *testing.T) {
	cases := []struct {
		name string
		gxml string
	}{
		{"Column内", `<Column><Input Value="{Title}" ValueChanged="OnNameChanged"/><Text Value="x"/></Column>`},
		{"Row内", `<Row><Input Value="{Title}"/><Text Value="x"/></Row>`},
		{"If内", `<If Condition="{Dirty}"><Then><Input Value="{Title}" ValueChanged="OnNameChanged"/></Then></If>`},
		{"IfElse内", `<If Condition="{Dirty}"><Then><Text Value="a"/></Then><Else><Input Value="{Title}"/></Else></If>`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := rtProbe(t, c.gxml, `
func TestProbeCompileContainer(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "t", Dirty: true}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 120)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	v.Layout(gtx, th)
}
`)
			if !contains(out, "[err=nil]") {
				t.Fatalf("%s 组合编译失败:\n%s", c.name, out)
			}
		})
	}
}

// TestInputMultipleInputsCompile 多个 Input 各自独立槽位，编译通过。
func TestInputMultipleInputsCompile(t *testing.T) {
	out := rtProbe(t,
		`<Column><Input Value="{Title}" ValueChanged="OnNameChanged"/><Input Value="{User.Name}"/></Column>`,
		`
func TestProbeMultiInput(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "a"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 160)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	v.Layout(gtx, th)
	// 两个 Input 的 applied 字段互相独立
	if v.applied0 != "a" {
		t.Fatalf("input0.applied0 = %q, 期望 a", v.applied0)
	}
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("多 Input 编译失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
