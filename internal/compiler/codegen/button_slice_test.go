package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// 事件切片：<Button Click="Method"/> 最小实现（事件处理输出契约）
//
// 覆盖：
//   - 生成结构（控件字段、顶部轮询、InvalidateCmd）
//   - 事件页不固化（动态判定）
//   - 明确失败（带参、Enabled、不支持的事件名）
//   - 真实点击（button_click_test.go，真实窗口合成指针事件）
// ============================================================================

// TestButtonGeneratesStructure 结构断言：字段 + 顶部轮询 + 保险命令。
func TestButtonGeneratesStructure(t *testing.T) {
	v := verified(t, `<Button Text="Save" Click="Increment"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	s := string(out)

	want := []string{
		// 控件状态字段，跨帧稳定（契约 §1）
		"click0 widget.Clickable",
		// 顶部轮询（契约 §2.1）：必须是 `for` 循环消费队列
		"for v.click0.Clicked(gtx) {",
		// 处理器调用（无参）
		"v.vm.Increment()",
		// 保险失效（契约 §3）
		"gtx.Execute(op.InvalidateCmd{})",
		// 绘制绑定到同一 Clickable
		"material.Button(th, &v.click0,",
		// 需要 widget 包
		`"gioui.org/widget"`,
	}
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("生成代码缺 %q:\n%s", w, s)
		}
	}
}

// TestButtonPollBeforeDraw 轮询必须出现在绘制之前（契约 §2.2）。
func TestButtonPollBeforeDraw(t *testing.T) {
	v := verified(t, `<Button Text="Save" Click="Increment"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	pollAt := strings.Index(s, "for v.click0.Clicked(gtx)")
	drawAt := strings.Index(s, "material.Button(th, &v.click0")
	if pollAt < 0 {
		t.Fatal("未生成轮询")
	}
	if drawAt < 0 {
		t.Fatal("未生成按钮绘制")
	}
	if pollAt > drawAt {
		t.Error("轮询出现在绘制之后——会导致同帧撕裂（部分节点用旧状态）")
	}
}

// TestButtonPageNotFrozen 事件页禁止整页固化（契约 §4.3 的核心回归）。
//
// 若生成整页 cacheCall 回放，Clicked 只在录制帧轮询一次，按钮点不动。
func TestButtonPageNotFrozen(t *testing.T) {
	v := verified(t, `<Button Text="Save" Click="Increment"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"v.cacheCall.Add", "cacheDims", "v.cached &&", "op.Record(&v.cacheOps)"} {
		if strings.Contains(s, bad) {
			t.Errorf("事件页不得整页固化，但出现了 %q:\n%s", bad, s)
		}
	}
}

// TestButtonTextForms 标签的三种形态。
func TestButtonTextForms(t *testing.T) {
	t.Run("静态", func(t *testing.T) {
		v := verified(t, `<Button Text="保存" Click="Increment"/>`)
		out, _ := Generate(v, bindingOpts())
		if !strings.Contains(string(out), `"保存"`) {
			t.Errorf("静态标签应生成字面量:\n%s", out)
		}
	})
	t.Run("绑定", func(t *testing.T) {
		v := verified(t, `<Button Text="{Title}" Click="Increment"/>`)
		out, err := Generate(v, bindingOpts())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), "v.vm.Title") {
			t.Errorf("绑定标签应生成 v.vm.Title:\n%s", out)
		}
	})
	t.Run("缺省", func(t *testing.T) {
		v := verified(t, `<Button Click="Increment"/>`)
		out, err := Generate(v, bindingOpts())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `material.Button(th, &v.click0, "")`) {
			t.Errorf("缺省标签应为空串:\n%s", out)
		}
	})
}

// TestButtonMultipleButtons 多按钮各自独立槽位与处理器。
func TestButtonMultipleButtons(t *testing.T) {
	v := verified(t, `<Column><Button Text="a" Click="Increment"/><Button Text="b" Click="Reset"/></Column>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"click0 widget.Clickable", "click1 widget.Clickable",
		"for v.click0.Clicked(gtx) {", "for v.click1.Clicked(gtx) {",
		"v.vm.Increment()", "v.vm.Reset()",
		"&v.click0,", "&v.click1,",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("多按钮生成缺 %q:\n%s", want, s)
		}
	}
}

// TestButtonRejectsArguments 带参事件必须显式报错（契约 §5.3）。
func TestButtonRejectsArguments(t *testing.T) {
	v := verified(t, `<Button Text="x" Click="Delete(User.Age)"/>`)
	out, err := Generate(v, bindingOpts())
	if err == nil {
		t.Fatal("带参事件应报错")
	}
	if len(out) != 0 {
		t.Error("失败时不得产出部分源码")
	}
	if !strings.Contains(err.Error(), "argument") {
		t.Errorf("错误信息应说明实参不支持: %v", err)
	}
}

// TestButtonRejectsEnabled Enabled 不在 MVP 范围，必须显式报错而非静默忽略。
func TestButtonRejectsEnabled(t *testing.T) {
	v := verified(t, `<Button Text="x" Click="Increment" Enabled="false"/>`)
	_, err := Generate(v, bindingOpts())
	if err == nil {
		t.Fatal("Enabled 应报错（静默忽略会让「禁用」变成可点击）")
	}
	if !strings.Contains(err.Error(), "Enabled") {
		t.Errorf("错误信息应点名 Enabled: %v", err)
	}
}

// TestButtonDeterministic 含事件时生成仍确定。
func TestButtonDeterministic(t *testing.T) {
	v := verified(t, `<Column><Button Text="a" Click="Increment"/><Text Value="{Title}"/></Column>`)
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
