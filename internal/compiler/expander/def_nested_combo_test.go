package expander

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// PRODUCT-R4-B 切片 2：嵌套 Def、If 两分支与 Slot 调用方内容的 Prop 替换
// 一致性 + 全链（Parser→Expander→Analyzer→Codegen）真实 Go 编译、字节确定。
// 只用已批准合法输入：标量 Props、静态/绑定实参、<Slot/>、单层 For、行事件。

const nestedComboSrc = `<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel">
  <Defs>
    <Def Name="OuterCard" Props="Greeting:string">
      <Column>
        <If Condition="{Dirty}">
          <Then><InnerRow Label="{Greeting}"><Text Value="slot-then"/></InnerRow></Then>
          <Else><InnerRow Label="fallback"><Text Value="slot-else"/></InnerRow></Else>
        </If>
      </Column>
    </Def>
    <Def Name="InnerRow" Props="Label:string">
      <For Each="item" In="{Items}" Key="{item.ID}">
        <Column>
          <Text Value="{Label}"/>
          <Text Value="{item.Name}"/>
          <Slot/>
        </Column>
      </For>
    </Def>
  </Defs>
  <Page Route="/"><OuterCard Greeting="hello"/></Page>
</Goxml>`

// TestNestedDefSlotExpansionConsistency 展开后事实一致性：
// 无残留结构节点/RawAttrs；两分支各自替换链正确；For 绑定保留；模板不被污染。
func TestNestedDefSlotExpansionConsistency(t *testing.T) {
	doc := expand(t, nestedComboSrc)

	// 1) 页面下不得残留未展开的结构节点或未分类属性。
	var residue []string
	var scanLeft func(n *contracts.NodeIR)
	scanLeft = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		if n.Kind == contracts.KindDef || n.Kind == contracts.KindSlot {
			residue = append(residue, "struct:"+n.Tag)
		}
		if len(n.RawAttrs) != 0 {
			residue = append(residue, "rawattrs:"+n.Tag)
		}
		for _, c := range n.Children {
			scanLeft(c)
		}
		if n.If != nil {
			for _, c := range n.If.Then {
				scanLeft(c)
			}
			for _, c := range n.If.Else {
				scanLeft(c)
			}
		}
	}
	scanLeft(doc.Page)
	if len(residue) != 0 {
		t.Fatalf("展开残留: %v", residue)
	}

	// 2) 收集全部 Text Value，断言两分支替换链。
	var texts []string
	var walkTexts func(n *contracts.NodeIR)
	walkTexts = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		if n.Tag == "Text" {
			if a, ok := n.Attr("Value"); ok {
				texts = append(texts, a.Value.Raw)
			}
		}
		for _, c := range n.Children {
			walkTexts(c)
		}
		if n.If != nil {
			for _, c := range n.If.Then {
				walkTexts(c)
			}
			for _, c := range n.If.Else {
				walkTexts(c)
			}
		}
	}
	walkTexts(doc.Page)
	count := func(want string) int {
		n := 0
		for _, s := range texts {
			if s == want {
				n++
			}
		}
		return n
	}
	// 展开树是**循环形状**（For 节点不展开为逐行副本，运行期才循环），
	// 每份 Text 节点在树中出现一次：
	//   Then 分支：Label 链 "hello"（Outer→Inner 双层替换）+ "slot-then"；
	//   Else 分支：静态 "fallback" + "slot-else"；
	//   行级 {item.Name} 两分支各保留 1 份绑定（非静态化）。
	if count("hello") != 1 || count("slot-then") != 1 || count("fallback") != 1 || count("slot-else") != 1 {
		t.Fatalf("两分支替换链错误: texts=%q", texts)
	}
	if count("{item.Name}") != 2 {
		t.Fatalf("行级绑定应保留 2 份: texts=%q", texts)
	}

	// 3) 模板不被展开污染：Inner 模板仍是 {Items}/{Label}+<Slot/>；
	//    Outer 模板的 If.Condition 仍是 {Dirty}。
	if len(doc.Defs) != 2 {
		t.Fatalf("Defs 应保留两个模板: %d", len(doc.Defs))
	}
	for _, d := range doc.Defs {
		if d.Def == nil || d.Def.Body == nil {
			t.Fatal("模板 Body 为 nil")
		}
		body := d.Def.Body
		switch d.Def.Name {
		case "InnerRow":
			if body.For == nil || !body.For.In.IsBinding || body.For.In.Raw != "{Items}" {
				t.Fatalf("InnerRow 模板 For.In 被污染: %+v", body.For)
			}
			// For.Children[0] = Column，{Label} 在其首个 Text 子节点上。
			if len(body.Children) == 0 || len(body.Children[0].Children) == 0 {
				t.Fatal("InnerRow 模板结构缺失")
			}
			if a, ok := body.Children[0].Children[0].Attr("Value"); !ok || a.Value.Raw != "{Label}" {
				t.Fatalf("InnerRow 模板 {Label} 被污染: %+v", a)
			}
		case "OuterCard":
			// If 是 Column 的子节点（不是 body.If 子句）。
			if len(body.Children) != 1 || body.Children[0].Tag != "If" || body.Children[0].If == nil {
				t.Fatalf("OuterCard 模板 If 结构被破坏: %+v", body)
			}
			if body.Children[0].If.Condition.Raw != "{Dirty}" {
				t.Fatalf("OuterCard 模板 Condition 被污染: %q", body.Children[0].If.Condition.Raw)
			}
		}
	}
}
