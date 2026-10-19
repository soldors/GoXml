package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// 带参事件（事件处理输出契约 v2 §5.1/§5.4）
//
// Facts 扩展 ResolvedArg：类型化字面量 + 已解析路径。
// Codegen 只渲染语法，不回读原始文本。
// ============================================================================

// TestEventArgsLiteralRendering 四种字面量的渲染。
func TestEventArgsLiteralRendering(t *testing.T) {
	cases := []struct {
		gxml string
		want string
	}{
		{`<Button Text="a" Click="OnLiteral(42)"/>`, `v.vm.OnLiteral(42)`},
		{`<Button Text="a" Click="OnLiteral(-3)"/>`, `v.vm.OnLiteral(-3)`},
		{`<Button Text="a" Click="OnEcho('draft')"/>`, `v.vm.OnEcho("draft")`},
		{`<Button Text="a" Click="OnToggle(true)"/>`, `v.vm.OnToggle(true)`},
		{`<Button Text="a" Click="OnToggle(false)"/>`, `v.vm.OnToggle(false)`},
	}
	for _, c := range cases {
		t.Run(c.gxml, func(t *testing.T) {
			v := verified(t, c.gxml)
			out, err := Generate(v, bindingOpts())
			if err != nil {
				t.Fatalf("生成失败: %v", err)
			}
			if !strings.Contains(string(out), c.want) {
				t.Errorf("期望 %q:\n%s", c.want, out)
			}
		})
	}
}

// TestEventArgsPathRendering 路径实参加 vm 前缀（与绑定取值同规则）。
func TestEventArgsPathRendering(t *testing.T) {
	v := verified(t, `<Button Text="a" Click="Delete(User.Age)"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	// 注意：形参名与属性名不同——这里断言**调用形态**。
	if !strings.Contains(string(out), "v.vm.Delete(v.vm.User.Age)") {
		t.Errorf("路径实参应渲染为 v.vm.User.Age:\n%s", out)
	}
}

// TestEventArgsMultipleInOrder 多实参按声明顺序传入。
func TestEventArgsMultipleInOrder(t *testing.T) {
	v := verified(t, `<Button Text="a" Click="OnOpen('x', User.Age)"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `v.vm.OnOpen("x", v.vm.User.Age)`) {
		t.Errorf("多实参顺序错误:\n%s", out)
	}
}

// TestEventArgsStringEscaping 字符串实参走同一套 Go 转义。
//
// 注意：XML 属性用双引号包裹，因此测试内容不能含裸 `"`（会截断属性）；
// 这里用反斜杠与内部单引号，仍能验证「内容按字节取出后再正确转义」。
func TestEventArgsStringEscaping(t *testing.T) {
	cases := []struct{ gxml, want string }{
		// 反斜杠：内容 a\b → Go 字面量 "a\\b"
		{`<Button Text="a" Click="OnEcho('a\b')"/>`, `v.vm.OnEcho("a\\b")`},
		// 内部单引号：内容 it''s → Go 字面量 "it''s"
		{`<Button Text="a" Click="OnEcho('it''s')"/>`, `v.vm.OnEcho("it''s")`},
	}
	for _, c := range cases {
		t.Run(c.gxml, func(t *testing.T) {
			v := verified(t, c.gxml)
			out, err := Generate(v, bindingOpts())
			if err != nil {
				t.Fatalf("生成失败: %v", err)
			}
			if !strings.Contains(string(out), c.want) {
				t.Errorf("期望 %q:\n%s", c.want, out)
			}
		})
	}
}

// TestEventArgsDeterministic 带参时生成仍确定。
func TestEventArgsDeterministic(t *testing.T) {
	v := verified(t, `<Button Text="a" Click="OnOpen('x', User.Age)"/>`)
	a, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("两次生成结果不一致")
	}
}

// TestEventArgsTypeMismatchRejectedByAnalyzer 类型不兼容仍由 Analyzer 拦下（EVENT-006）。
//
// Codegen 不为实参做类型校验——那是 Analyzer 的职责，重复校验会造成双源。
func TestEventArgsTypeMismatchRejectedByAnalyzer(t *testing.T) {
	cases := []struct {
		name string
		gxml string
	}{
		{"整数传给 string 形参", `<Button Text="a" Click="OnEcho(42)"/>`},
		{"字符串传给 int 形参", `<Button Text="a" Click="Delete('x')"/>`},
		{"布尔传给 int 形参", `<Button Text="a" Click="OnLiteral(true)"/>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := verifiedErr(t, c.gxml); err == nil {
				t.Errorf("%s 应被 Analyzer 拒绝", c.name)
			}
		})
	}
}

// TestEventArgsArityMismatchRejected 实参数量不匹配仍由 Analyzer 拦下（EVENT-004）。
func TestEventArgsArityMismatchRejected(t *testing.T) {
	if _, err := verifiedErr(t, `<Button Text="a" Click="OnLiteral(1, 2)"/>`); err == nil {
		t.Error("多余实参应被 Analyzer 拒绝")
	}
}
