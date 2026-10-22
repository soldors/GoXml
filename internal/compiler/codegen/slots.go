package codegen

import (
	"fmt"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// 显示缓存槽位的**单一派生**（TASK-CODEGEN-NAMING-001 / C5）
//
// 为什么需要这个文件：预检要判断一个源文件是否声明 `goxmlTextCache<Source>`
// （判据 `bindSlots > 0`），而 bindSlots 原本只是 generator 的私有累加字段，
// 只有真的跑完发射才知道。若预检自己写一遍「哪个 <Text> 占槽」的判定，
// 就成了两份真相——C2 已经为此踩过坑（符号名必须由生产函数派生）。
//
// 因此把规则抽到 textBindSlot 一处：
//   - generator.nodeExpr 用它决定「发不发 v.bindText / 分配不分配槽位」；
//   - CountBindSlots 用它统计槽位数。
//
// 诚实声明边界：这里是**规则**单一来源，遍历仍有两处（发射走 nodeExpr 的
// 递归，计数走 countSlots）。两者是否一致的护栏是
// TestSlotCountAgreesWithDeclaredTextCache —— 对一组夹具断言
// 「CountBindSlots > 0」与「产物里声明了 TextCache 类型」互为充要条件。
// 只抽规则、不抽遍历，是因为发射侧必须边走边分配序号，无法退化成计数。
// ============================================================================

// textBindSlot 判定一个节点在显示缓存上的角色。
//
// 返回值：
//   - ra：<Text> 的 Value 已解析属性（仅在 isBinding 时有意义）；
//   - isBinding：Value 来自数据绑定（而非静态字面量）；
//   - takesSlot：本次发射会**分配**一个显示缓存槽位。
//
// 规则（卡 §10.7.4 三类页面表的底层判据，冻结）：
//  1. 只有 <Text> 且 Value 是绑定才算绑定（静态文本走 Body1 直接布局，不占槽）；
//  2. inFor 为真时不占槽——槽位按节点分配，N 行复用同一槽位会在同一帧内
//     互相覆写，命中率恒为 0（<For> 列表契约 §6.2）。
func textBindSlot(n *contracts.NodeIR, facts *analyzer.Facts, inFor bool) (ra analyzer.ResolvedAttr, isBinding bool, takesSlot bool) {
	if n == nil || n.Tag != "Text" {
		return analyzer.ResolvedAttr{}, false, false
	}
	ra, ok := lookupAttr(n, "Value", facts)
	if !ok || ra.Path == nil {
		return analyzer.ResolvedAttr{}, false, false
	}
	// 绑定成立；循环体内不分配槽位。
	return ra, true, !inFor
}

// CountBindSlots 统计页面会分配的显示缓存槽位数。
//
// 语义与 generator 发射时累加的 bindSlots 一致（因为用的是同一条规则
// textBindSlot）。纯函数：只读 VerifiedDoc，不产生任何副作用。
//
// 返回 error 只用于 VerifiedDoc 不变量被破坏（nil、无文档）。Page 缺失时
// 返回 0 —— 是否「没有 Page 就失败」由调用方（pipeline）判断，本函数不越权。
func CountBindSlots(v *analyzer.VerifiedDoc) (int, error) {
	if v == nil {
		return 0, fmt.Errorf("codegen: nil VerifiedDoc")
	}
	doc := v.Document()
	if doc == nil {
		return 0, fmt.Errorf("codegen: VerifiedDoc has no document")
	}
	if doc.Page == nil {
		return 0, nil
	}
	facts := v.Facts()
	n := 0
	countSlots(doc.Page, facts, false, &n)
	return n, nil
}

// countSlots 递归统计槽位。
//
// 必须与发射侧的遍历形状一致，否则预检会按错误的槽位数收窄。两处已暴露过的
// 差异点：**`<If>` 的分支不在 Children 里**（在 If.Then / If.Else），
// 只走 Children 会漏掉分支内的绑定——由
// TestSlotCountAgreesWithDeclaredTextCache 的「If 内绑定」用例守住。
//
// <For> 的子节点处于循环体内 → 以 inFor=true 继续下探
// （循环体里的 <If> 同样继承 inFor）。
func countSlots(node *contracts.NodeIR, facts *analyzer.Facts, inFor bool, n *int) {
	if node == nil {
		return
	}
	if _, _, takesSlot := textBindSlot(node, facts, inFor); takesSlot {
		*n++
	}
	childInFor := inFor || isForNode(node)
	for _, c := range node.Children {
		countSlots(c, facts, childInFor, n)
	}
	if node.If != nil {
		for _, c := range node.If.Then {
			countSlots(c, facts, childInFor, n)
		}
		for _, c := range node.If.Else {
			countSlots(c, facts, childInFor, n)
		}
	}
}
