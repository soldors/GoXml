package codegen

import (
	"fmt"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// 步骤 1：消费 Facts，生成绑定取值表达式
//
// 依据：Analyzer 已验证的事实，Codegen **不得**自行解析绑定路径。
// 绑定路径、目标类型、最终类型都来自 Facts。若 Facts 查不到 → 报错，
// 不得回退到「当作字符串常量」。

// bindingExpr 生成一个绑定属性的取值表达式。
//
// 参数 vm 是 ViewModel 变量名（生成代码里为 "vm"）。
func bindingExpr(ra analyzer.ResolvedAttr, vm string) (string, error) {
	if ra.Path == nil {
		return "", fmt.Errorf("codegen: attr %s has no resolved path", ra.Name)
	}
	path := ra.Path

	// 起点：ViewModel 根 或 <For> 循环变量。
	var base string
	switch path.RootKind {
	case analyzer.RootViewModel:
		if vm == "" {
			return "", fmt.Errorf("codegen: attr %s binds to ViewModel but no vm parameter available", ra.Name)
		}
		base = vm
	case analyzer.RootForVar:
		if path.Root == "" {
			return "", fmt.Errorf("codegen: attr %s binds to a <For> variable but root is empty", ra.Name)
		}
		base = path.Root
	default:
		return "", fmt.Errorf("codegen: attr %s has unknown root kind %v", ra.Name, path.RootKind)
	}

	expr := base
	for _, step := range path.Steps {
		expr += "." + step.Name
	}

	if path.Negated {
		expr = "!" + expr
	}

	return expr, nil
}

// valueRef 返回一个属性值的 Go 表达式。
//
// 静态 → 字符串字面量。
// 绑定 → 取值表达式（来自 Facts）。
// 查不到 Facts → 报错。
func valueRef(n *contracts.NodeIR, name string, vm string, facts *analyzer.Facts) (string, bool, error) {
	for _, a := range n.Attrs {
		if a.Name != name {
			continue
		}

		if !a.Value.IsBinding {
			return quote(a.Value.Raw), false, nil
		}

		// 绑定：必须查到 Facts。
		if facts == nil {
			return "", false, fmt.Errorf("codegen: attr %s is a binding but no facts available", name)
		}
		nf, ok := facts.Lookup(n)
		if !ok {
			return "", false, fmt.Errorf("codegen: attr %s is a binding but no facts recorded for node <%s>", name, n.Tag)
		}

		for _, ra := range nf.Attrs() {
			if ra.Name != name {
				continue
			}
			expr, err := bindingExpr(ra, vm)
			if err != nil {
				return "", false, err
			}
			return expr, true, nil
		}
		return "", false, fmt.Errorf("codegen: attr %s is a binding but not found in attrs", name)
	}
	return "", false, nil
}

// propFacts 从 Facts 取一个节点的属性事实列表。
func propFacts(n *contracts.NodeIR, facts *analyzer.Facts) ([]analyzer.ResolvedAttr, bool) {
	if facts == nil {
		return nil, false
	}
	nf, ok := facts.Lookup(n)
	if !ok {
		return nil, false
	}
	return nf.Attrs(), true
}

// ============================================================================
// 绑定节点的生成

// bindingTextExpr 生成绑定 <Text> 的渲染表达式。
//
// 与静态版本的区别：值来自 vm 字段，不是字符串字面量。
func bindingTextExpr(n *contracts.NodeIR, vm string, facts *analyzer.Facts) (string, error) {
	expr, isBinding, err := valueRef(n, "Value", vm, facts)
	if err != nil {
		return "", err
	}
	if !isBinding {
		// 静态：直接渲染。
		return fmt.Sprintf("material.Body1(th, %s).Layout(gtx)", expr), nil
	}

	// 绑定：值需要转换为显示字符串。
	return fmt.Sprintf("material.Body1(th, %s).Layout(gtx)", expr), nil
}

// lookupAttr 在 Facts 中查找属性。
func lookupAttr(n *contracts.NodeIR, name string, facts *analyzer.Facts) (analyzer.ResolvedAttr, bool) {
	attrs, ok := propFacts(n, facts)
	if !ok {
		return analyzer.ResolvedAttr{}, false
	}
	for _, ra := range attrs {
		if ra.Name == name {
			return ra, true
		}
	}
	return analyzer.ResolvedAttr{}, false
}

// hasBinding 递归检查子树是否含任何绑定。
//
// 用途：决定页面是否需要 vm 参数、以及能否走静态子树固化。
// 含绑定的子树**不是**静态子树（契约 §16.7）。
func hasBinding(n *contracts.NodeIR) bool {
	if n == nil {
		return false
	}
	for _, a := range n.Attrs {
		if a.Value.IsBinding {
			return true
		}
	}
	for _, c := range n.Children {
		if hasBinding(c) {
			return true
		}
	}
	return false
}
