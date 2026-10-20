package codegen

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// TASK-EVENT-PAYLOAD-001：通用载荷组装的独立验收
//
// 独立性要求（任务卡 §3）：**不依赖任何具体控件**。
// 测试直接构造 ResolvedEvent（Facts 层的真实类型），注入载荷表达式，
// 断言生成的调用形态——不经过 Input、不经过 gxml。
// ============================================================================

// ev 构造一个用于测试的 ResolvedEvent。
func ev(name, method string, params []analyzer.ResolvedParam,
	payload []analyzer.ResolvedParam, callArgs []analyzer.ResolvedArg,
	mapping []analyzer.ParamOrigin) analyzer.ResolvedEvent {
	return analyzer.ResolvedEvent{
		Name:     name,
		Method:   analyzer.ResolvedMethod{Name: method, Params: params},
		Payload:  payload,
		CallArgs: callArgs,
		Mapping:  mapping,
	}
}

// p 是形参/载荷的简写构造。
func p(name string) analyzer.ResolvedParam {
	return analyzer.ResolvedParam{Name: name, Type: analyzer.TypeInfo{Kind: analyzer.ValueString}}
}

// lit 构造字符串字面量实参。
func lit(s string) analyzer.ResolvedArg {
	return analyzer.ResolvedArg{
		Kind:    analyzer.ArgLiteral,
		Literal: analyzer.LiteralValue{Kind: analyzer.LiteralString, Str: s},
	}
}

// TestPayloadZeroArgs 场景 1：零参数方法（Button/Click 形态）→ 空实参。
func TestPayloadZeroArgs(t *testing.T) {
	g := &generator{}
	e := ev("Click", "Save", nil, nil, nil, nil)

	got, err := g.renderInvocationArgs(e, nil)
	if err != nil {
		t.Fatalf("不应失败: %v", err)
	}
	if got != "" {
		t.Errorf("零参数应收敛为空串, 实际 %q", got)
	}
}

// TestPayloadSingleArg 场景 2：单载荷 → 使用控件注入的表达式。
func TestPayloadSingleArg(t *testing.T) {
	g := &generator{}
	e := ev("ValueChanged", "OnNameChanged",
		[]analyzer.ResolvedParam{p("value")},
		[]analyzer.ResolvedParam{p("value")},
		nil,
		[]analyzer.ParamOrigin{analyzer.FromPayload})

	got, err := g.renderInvocationArgs(e, func(i int) (string, error) {
		if i != 0 {
			t.Fatalf("载荷序号应为 0, 实际 %d", i)
		}
		return "v.input0.Text()", nil
	})
	if err != nil {
		t.Fatalf("不应失败: %v", err)
	}
	if got != "v.input0.Text()" {
		t.Errorf("实参 = %q, 期望注入的表达式", got)
	}
}

// TestPayloadThenCallArgs 场景 3：载荷在前、调用点实参在后（顺序由 Mapping 决定）。
func TestPayloadThenCallArgs(t *testing.T) {
	g := &generator{}
	// OnOpen(id string /*载荷*/, cardID string /*实参*/)
	e := ev("Open", "OnOpen",
		[]analyzer.ResolvedParam{p("id"), p("cardID")},
		[]analyzer.ResolvedParam{p("id")},
		[]analyzer.ResolvedArg{lit("card-1")},
		[]analyzer.ParamOrigin{analyzer.FromPayload, analyzer.FromCallArg})

	got, err := g.renderInvocationArgs(e, func(int) (string, error) {
		return "v.input0.Text()", nil
	})
	if err != nil {
		t.Fatalf("不应失败: %v", err)
	}
	want := `v.input0.Text(), "card-1"`
	if got != want {
		t.Errorf("实参 = %q, 期望 %q（载荷在前、实参在后）", got, want)
	}
}

// TestPayloadMappingOrderIsAuthoritative 顺序完全由 Mapping 决定，
// 即使形参位置上载荷不在第一位也照 Mapping 走。
func TestPayloadMappingOrderIsAuthoritative(t *testing.T) {
	g := &generator{}
	// 形参：cardID(实参) 在前，id(载荷) 在后——Mapping 决定交错顺序。
	e := ev("Open", "OnOpen",
		[]analyzer.ResolvedParam{p("cardID"), p("id")},
		[]analyzer.ResolvedParam{p("id")},
		[]analyzer.ResolvedArg{lit("card-1")},
		[]analyzer.ParamOrigin{analyzer.FromCallArg, analyzer.FromPayload})

	got, err := g.renderInvocationArgs(e, func(int) (string, error) {
		return "v.input0.Text()", nil
	})
	if err != nil {
		t.Fatalf("不应失败: %v", err)
	}
	want := `"card-1", v.input0.Text()`
	if got != want {
		t.Errorf("实参 = %q, 期望 %q（严格按 Mapping）", got, want)
	}
}

// TestPayloadTwoEventsIndependent 场景 4：两个事件映射到不同方法，各自独立。
//
// 载荷表达式由调用方按事件分别提供——不存在跨事件的隐式回退。
func TestPayloadTwoEventsIndependent(t *testing.T) {
	g := &generator{}

	change := ev("ValueChanged", "OnNameChanged",
		[]analyzer.ResolvedParam{p("value")},
		[]analyzer.ResolvedParam{p("value")},
		nil,
		[]analyzer.ParamOrigin{analyzer.FromPayload})
	submit := ev("Submit", "OnNameSubmit",
		[]analyzer.ResolvedParam{p("text")},
		[]analyzer.ResolvedParam{p("text")},
		nil,
		[]analyzer.ParamOrigin{analyzer.FromPayload})

	gotChange, err := g.renderInvocationArgs(change, func(int) (string, error) {
		return "v.input0.Text()", nil
	})
	if err != nil {
		t.Fatalf("change 不应失败: %v", err)
	}
	if gotChange != "v.input0.Text()" {
		t.Errorf("change 实参 = %q", gotChange)
	}

	gotSubmit, err := g.renderInvocationArgs(submit, func(int) (string, error) {
		return "ev2.Text", nil
	})
	if err != nil {
		t.Fatalf("submit 不应失败: %v", err)
	}
	if gotSubmit != "ev2.Text" {
		t.Errorf("submit 实参 = %q（不得复用 change 的表达式）", gotSubmit)
	}
	if gotChange == gotSubmit {
		t.Error("两个事件不应产出相同实参——存在隐式回退")
	}
}

// TestPayloadMappingInconsistencyIsInternalError 场景 5：
// Mapping 与形参数量不符 → 明确内部错误，不伪装成用户诊断。
func TestPayloadMappingInconsistencyIsInternalError(t *testing.T) {
	cases := []struct {
		name string
		e    analyzer.ResolvedEvent
		want string
	}{
		{
			name: "Mapping 长度与形参数不符",
			e: ev("E", "M",
				[]analyzer.ResolvedParam{p("a"), p("b")}, // 2 个形参
				nil, nil,
				[]analyzer.ParamOrigin{analyzer.FromPayload}), // 只有 1 条映射
			want: "mapping",
		},
		{
			name: "Mapping 声明的载荷数与 Payload 不符",
			e: ev("E", "M",
				[]analyzer.ResolvedParam{p("a")},
				nil, // Payload 为空
				nil,
				[]analyzer.ParamOrigin{analyzer.FromPayload}),
			want: "payload",
		},
		{
			name: "Mapping 声明的实参数与 CallArgs 不符",
			e: ev("E", "M",
				[]analyzer.ResolvedParam{p("a")},
				nil,
				nil, // CallArgs 为空
				[]analyzer.ParamOrigin{analyzer.FromCallArg}),
			want: "call arguments",
		},
		{
			name: "控件未提供载荷表达式",
			e: ev("E", "M",
				[]analyzer.ResolvedParam{p("a")},
				[]analyzer.ResolvedParam{p("a")},
				nil,
				[]analyzer.ParamOrigin{analyzer.FromPayload}),
			want: "no payload expression",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := &generator{}
			_, err := g.renderInvocationArgs(c.e, nil)
			if err == nil {
				t.Fatal("应返回内部错误")
			}
			msg := err.Error()
			if !strings.Contains(msg, "codegen: internal:") {
				t.Errorf("应标记为内部错误（不是用户诊断）: %v", err)
			}
			if !strings.Contains(msg, c.want) {
				t.Errorf("错误信息应包含 %q: %v", c.want, err)
			}
			// 不得出现任何 GXML 诊断码——内部缺陷不是用户错误。
			if strings.Contains(msg, "GXML-") {
				t.Errorf("内部错误不应伪装成 GXML 诊断: %v", err)
			}
		})
	}
}

// TestPayloadUnknownOrigin 未知的 ParamOrigin → 内部错误。
func TestPayloadUnknownOrigin(t *testing.T) {
	g := &generator{}
	e := ev("E", "M",
		[]analyzer.ResolvedParam{p("a")},
		nil, nil,
		[]analyzer.ParamOrigin{analyzer.ParamOrigin(99)})

	if _, err := g.renderInvocationArgs(e, nil); err == nil {
		t.Fatal("未知来源应返回内部错误")
	} else if !strings.Contains(err.Error(), "unknown parameter origin") {
		t.Errorf("错误信息应说明未知来源: %v", err)
	}
}

// TestPayloadButtonRegression Button 的零载荷路径经通用层后行为不变。
//
// 这是 D1 的回归护栏：接入通用层不得改变既有生成结果。
func TestPayloadButtonRegression(t *testing.T) {
	v := verified(t, `<Button Text="保存" Click="Increment"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"v.vm.Increment()",              // 零参调用形态不变
		"for v.click0.Clicked(gtx) {",   // 轮询不变
		"material.Button(th, &v.click0", // 绘制不变
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Button 回归失败，缺 %q:\n%s", want, s)
		}
	}
	// 不得出现空实参的畸形调用。
	if strings.Contains(s, "v.vm.Increment(,)") || strings.Contains(s, "v.vm.Increment( )") {
		t.Errorf("生成了畸形调用:\n%s", s)
	}
}

// TestPayloadGeneratedCodeCompiles 生成的调用形态必须可编译（端到端）。
//
// 经 rtProbe 在仓库内临时包中真实构建——不只是字符串断言。
func TestPayloadGeneratedCodeCompiles(t *testing.T) {
	out := rtProbe(t, `<Button Text="a" Click="OnOpen('x', User.Age)"/>`, `
func TestProbePayloadCompiles(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(200, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	_ = v.Layout(gtx, th)
	t.Log("含多实参事件调用的生成代码编译并运行")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("多实参调用编译验证失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
