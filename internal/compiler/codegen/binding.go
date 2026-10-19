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
		// 方法段必须生成调用：`x.M()`。
		//
		// 若写成 `x.M` 会得到函数值，类型不匹配（实测：编译失败）。
		if step.IsMethod {
			expr += "()"
		}
	}

	if path.Negated {
		expr = "!" + expr
	}

	return expr, nil
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
