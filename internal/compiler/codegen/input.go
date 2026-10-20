package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// <Input> 的生成（TASK-INPUT-001-D4-A）
//
// 本切片只做「结构与事件接线」，**不做外部值同步算法**（D4-B）：
//   - Editor 实例字段 + 属性透传
//   - 控件自持事件消费（Update 在控件位置，不进顶部轮询）
//   - ValueChanged / Submit 的载荷接线（经 D1 的 renderInvocationArgs）
//
// 明确禁止：每帧无条件 SetText。`Value` 在本切片完全不参与生成。
// ============================================================================

// inputSlot 是一个 <Input> 的生成信息。
type inputSlot struct {
	// index 对应生成字段 input<index> / applied<index>。
	index int
	// hint 是 material.Editor 的第三参（已引号化）。
	hint string
	// readOnly/singleLine/maxLen/filter 是属性透传语句（可为空）。
	setters []string
	// hasSubmit 表示声明了 Submit 事件（决定 Editor.Submit）。
	hasSubmit bool
	// change 是 ValueChanged 的处理器调用（含载荷），无声明时为空。
	change string
	// submit 是 Submit 的处理器调用（含载荷），无声明时为空。
	submit string
}

// inputExpr 生成 <Input> 的渲染表达式。
//
// 生成形态（自持事件消费 + 布局）：
//
//	func() layout.Dimensions {
//		ed := &v.input0
//		<属性透传>
//		for {
//			ev, ok := ed.Update(gtx)
//			if !ok { break }
//			switch e := ev.(type) {
//			case widget.ChangeEvent:  v.vm.OnChange(ed.Text())
//			case widget.SubmitEvent:  v.vm.OnSubmit(e.Text)
//			}
//		}
//		return material.Editor(th, ed, "hint").Layout(gtx)
//	}()
func (g *generator) inputExpr(n *contracts.NodeIR) (string, error) {
	if len(n.Children) > 0 {
		return "", fmt.Errorf("codegen: <Input> must not have children")
	}
	if len(n.Events) > 0 && g.currentFor != nil && g.inFor {
		// v1：<For> 内 Input 已由 Parser 拒绝（SCOPE-005），此处为防御。
		return "", fmt.Errorf("codegen: <Input> inside <For> is not supported in v1")
	}

	g.imports.add("gioui.org/widget", "widget")
	g.imports.add("gioui.org/widget/material", "material")
	g.imports.add("gioui.org/layout", "layout")

	slot := inputSlot{index: g.inputSlots, hint: `""`}
	g.inputSlots++
	g.usesVM = true

	// ---- 属性：Hint ----
	if raw, ok := staticAttr(n, "Hint"); ok {
		slot.hint = quote(raw)
	} else if _, bound := lookupAttr(n, "Hint", g.facts); bound {
		return "", fmt.Errorf("codegen: <Input Hint> accepts only a literal in v1; data binding is not supported")
	}

	// ---- 属性：Enabled 明确拒绝（沿用 Button 裁决）----
	for _, a := range n.Attrs {
		if a.Name == "Enabled" {
			return "", fmt.Errorf("codegen: <Input Enabled> is not supported in this slice")
		}
	}

	// ---- 属性：静态 bool / int（Analyzer 只校验绑定，此处必须自己守）----
	if v, err := g.boolAttr(n, "ReadOnly"); err != nil {
		return "", err
	} else if v != nil {
		slot.setters = append(slot.setters, fmt.Sprintf("ed.ReadOnly = %s", boolLit(*v)))
	}
	if v, err := g.boolAttr(n, "SingleLine"); err != nil {
		return "", err
	} else if v != nil {
		slot.setters = append(slot.setters, fmt.Sprintf("ed.SingleLine = %s", boolLit(*v)))
	}
	if v, err := g.intAttr(n, "MaxLen"); err != nil {
		return "", err
	} else if v != nil {
		slot.setters = append(slot.setters, fmt.Sprintf("ed.MaxLen = %d", *v))
	}
	if raw, ok := staticAttr(n, "Filter"); ok {
		slot.setters = append(slot.setters, fmt.Sprintf("ed.Filter = %s", quote(raw)))
	} else if _, bound := lookupAttr(n, "Filter", g.facts); bound {
		return "", fmt.Errorf("codegen: <Input Filter> accepts only a literal in v1; data binding is not supported")
	}

	// ---- 事件接线 ----
	for _, ev := range n.Events {
		resolved, ok := lookupEvent(n, ev.Name, g.facts)
		if !ok {
			return "", fmt.Errorf(
				"codegen: event %q on <%s> has no resolved facts; the analyzer result is inconsistent",
				ev.Name, n.Tag)
		}

		switch ev.Name {
		case "ValueChanged":
			// 载荷表达式 = 编辑器当前文本。
			args, err := g.renderInvocationArgs(resolved, func(int) (string, error) {
				return "ed.Text()", nil
			})
			if err != nil {
				return "", err
			}
			slot.change = "v.vm." + ev.Method + "(" + args + ")"

		case "Submit":
			// 载荷表达式 = SubmitEvent 的 Text 字段（循环变量 e）。
			args, err := g.renderInvocationArgs(resolved, func(int) (string, error) {
				return "e.Text", nil
			})
			if err != nil {
				return "", err
			}
			slot.submit = "v.vm." + ev.Method + "(" + args + ")"
			slot.hasSubmit = true

		default:
			return "", fmt.Errorf(
				"codegen: <Input> event %q is not supported in this slice; only ValueChanged and Submit", ev.Name)
		}
	}

	g.inputs = append(g.inputs, slot)

	// ---- 组装表达式 ----
	var b strings.Builder
	b.WriteString("func() layout.Dimensions {\n")
	b.WriteString(fmt.Sprintf("\t\t\t\ted := &v.input%d\n", slot.index))
	if slot.hasSubmit {
		b.WriteString("\t\t\t\ted.Submit = true\n")
	}
	for _, s := range slot.setters {
		b.WriteString("\t\t\t\t" + s + "\n")
	}
	if slot.change != "" || slot.submit != "" {
		b.WriteString("\t\t\t\tfor {\n")
		b.WriteString("\t\t\t\t\tev, ok := ed.Update(gtx)\n")
		b.WriteString("\t\t\t\t\tif !ok {\n\t\t\t\t\t\tbreak\n\t\t\t\t\t}\n")
		b.WriteString("\t\t\t\t\tswitch e := ev.(type) {\n")
		if slot.change != "" {
			b.WriteString("\t\t\t\t\tcase widget.ChangeEvent:\n")
			b.WriteString("\t\t\t\t\t\t" + slot.change + "\n")
		}
		if slot.submit != "" {
			b.WriteString("\t\t\t\t\tcase widget.SubmitEvent:\n")
			b.WriteString("\t\t\t\t\t\t" + slot.submit + "\n")
		}
		b.WriteString("\t\t\t\t\t}\n")
		b.WriteString("\t\t\t\t}\n")
	}
	b.WriteString(fmt.Sprintf("\t\t\t\treturn material.Editor(th, ed, %s).Layout(gtx)\n", slot.hint))
	b.WriteString("\t\t\t}()")
	return b.String(), nil
}

// boolAttr 读取静态 bool 属性；缺失返回 (nil, nil)，非法返回错误。
func (g *generator) boolAttr(n *contracts.NodeIR, name string) (*bool, error) {
	if _, bound := lookupAttr(n, name, g.facts); bound {
		return nil, fmt.Errorf("codegen: <Input %s> accepts only a literal in v1; data binding is not supported", name)
	}
	raw, ok := staticAttr(n, name)
	if !ok {
		return nil, nil
	}
	switch raw {
	case "true":
		v := true
		return &v, nil
	case "false":
		v := false
		return &v, nil
	default:
		return nil, fmt.Errorf(
			"codegen: <Input %s> expects \"true\" or \"false\", got %q", name, raw)
	}
}

// intAttr 读取静态非负整数属性；缺失返回 (nil, nil)，非法返回错误。
func (g *generator) intAttr(n *contracts.NodeIR, name string) (*int, error) {
	if _, bound := lookupAttr(n, name, g.facts); bound {
		return nil, fmt.Errorf("codegen: <Input %s> accepts only a literal in v1; data binding is not supported", name)
	}
	raw, ok := staticAttr(n, name)
	if !ok {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("codegen: <Input %s> expects a non-negative integer, got %q", name, raw)
	}
	if v < 0 {
		return nil, fmt.Errorf("codegen: <Input %s> expects a non-negative integer, got %d", name, v)
	}
	return &v, nil
}

// boolLit 渲染 Go 布尔字面量。
func boolLit(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
