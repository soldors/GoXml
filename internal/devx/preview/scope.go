package preview

import (
	"github.com/goxml/goxml/contracts"
)

// 预览范围拒绝码。它们**不改变现行语言准入**：分析器已经拒绝的输入
// （无 VM 的绑定/事件等）仍按标准 GXML-*/CODEGEN-* 诊断透传；PREVIEW-*
// 只描述「语言允许、但静态预览首版范围未覆盖」的部分。
//
// PREVIEW-002/PREVIEW-003 当前是纵深防御：无 ViewModel 页面上的绑定与
// 事件在标准前端就会以 GXML-BIND-*/GXML-EVENT-* 被拒（analyze.go
// "no ViewModel is declared"）。门禁仍保留这两类检查并给出预览口径的
// 诊断，防止未来语言准入放宽后出现静默漏网。
const (
	// CodePreviewVM 页面声明了 ViewModel。
	CodePreviewVM = "PREVIEW-001"
	// CodePreviewBind 属性使用了动态绑定。
	CodePreviewBind = "PREVIEW-002"
	// CodePreviewEvent 节点绑定了事件处理器。
	CodePreviewEvent = "PREVIEW-003"
	// CodePreviewTheme 颜色使用了 $Theme.* 主题引用。
	CodePreviewTheme = "PREVIEW-004"
	// CodePreviewInteractive 页面含交互控件（Button/Input）。
	//
	// 现行 Codegen 把交互控件无条件判为动态页（binding.go isInteractiveTag，
	// 控件持有可交互状态），无 ViewModel 时无法生成。静态预览首版范围
	// （布局/Def/Slot/Image/Expanded）不含交互控件，此处显式拒绝并给出
	// 指向具体控件的诊断；codegen 的 "page is dynamic" 错误仍是兜底。
	CodePreviewInteractive = "PREVIEW-005"
)

// interactiveTags 镜像 codegen.isInteractiveTag 的标签集合。
//
// 不导出复用是为了不动共用文件；两处不同步时的后果是**fail-closed**：
// 新增交互标签会先落到 codegen 的 "page is dynamic" 生成期错误，
// 不会静默漏进预览。同步义务记录在 scope-matrix.md。
var interactiveTags = map[string]bool{
	"Button": true,
	"Input":  true,
}

// checkStaticScope 在**已通过标准前端**的展开 IR 上执行预览静态范围门禁。
//
// 输入是 Expander 展开后的 DocumentIR：
//   - Def props 已完成生成期替换，剩下的 Attrs 绑定都是真实数据绑定；
//   - 未被实例化引用的 Def 不会进入 Page 子树（Codegen 只从 Page 生成），
//     因此只遍历 Page；
//   - RawAttrs 已被 Expander 清空，无需再查。
//
// 返回空切片表示页面在预览首版范围内。
func checkStaticScope(file string, doc *contracts.DocumentIR) []contracts.CompilerError {
	if doc == nil {
		return nil
	}
	var out []contracts.CompilerError
	if !doc.ViewModel.IsZero() {
		// 纵深防御：Run 在 pre-parse 已拒绝 VM，正常流程到不了这里。
		out = append(out, contracts.NewError(file, doc.ViewModel.Span, CodePreviewVM,
			"page declares ViewModel %q; static preview supports ViewModel-free pages only (first-version scope)",
			doc.ViewModel.Value))
	}
	var walk func(n *contracts.NodeIR)
	walk = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		for _, e := range n.Events {
			out = append(out, contracts.NewError(file, e.Span, CodePreviewEvent,
				"event %q is bound to handler %q; static preview does not support events (first-version scope)",
				e.Name, e.Method))
		}
		for _, a := range n.Attrs {
			if a.Value.IsBinding {
				out = append(out, contracts.NewError(file, a.Span, CodePreviewBind,
					"attribute %q is a dynamic binding {%s}; static preview accepts static literals only (first-version scope)",
					a.Name, a.Value.Path()))
			}
		}
		if interactiveTags[n.Tag] {
			out = append(out, contracts.NewError(file, n.Span, CodePreviewInteractive,
				"<%s> is an interactive control; static preview scope covers layout/Def/Slot/Image/Expanded only (first-version scope)",
				n.Tag))
		}
		if n.Background != nil && n.Background.IsThemeRef {
			out = append(out, contracts.NewError(file, n.Span, CodePreviewTheme,
				"Background uses theme reference $Theme.%s; theme references are not approved for static preview (first-version scope)",
				n.Background.ThemeField))
		}
		if f := n.For; f != nil {
			// For 的 In 是必填绑定（语言规范 §6.2）：静态预览首版不支持循环。
			if f.In.IsBinding {
				out = append(out, contracts.NewError(file, n.Span, CodePreviewBind,
					"<For In> is a dynamic binding {%s}; static preview does not support loops (first-version scope)",
					f.In.Path()))
			}
		}
		if c := n.If; c != nil {
			// If 的 Condition 必须是布尔绑定（语言规范 §6.1）：静态预览首版不支持条件分支。
			if c.Condition.IsBinding {
				out = append(out, contracts.NewError(file, n.Span, CodePreviewBind,
					"<If Condition> is a dynamic binding {%s}; static preview does not support conditionals (first-version scope)",
					c.Condition.Path()))
			}
		}
		// If 的分支在 Then/Else 里；For 的循环体在 n.Children 里
		//（ForClause 无 body 字段），由下面的通用遍历覆盖。
		if c := n.If; c != nil {
			for _, child := range c.Then {
				walk(child)
			}
			for _, child := range c.Else {
				walk(child)
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(doc.Page)
	return out
}
