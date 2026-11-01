package expander

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// PRODUCT-R4-B 切片 1：Def/Slot 实例隔离判别探针。
//
// 方法（任务书要求）：对展开实例的已有**切片元素**做原地写入——这比 append
// 容量/长度变化强：append 只能碰运气，原地写直接暴露共享后备数组。
// 模板与兄弟实例必须不受影响；模板本体也必须未被展开过程污染。

const isolationSrc = `<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel">
  <Defs>
    <Def Name="Card" Props="Title:string">
      <For Each="item" In="{Items}" Key="{item.ID}">
        <Column>
          <Text Value="{Title}"/>
          <Text Value="{item.Name}"/>
          <Button Text="pick" Click="RecordText(item.Name)"/>
        </Column>
      </For>
    </Def>
  </Defs>
  <Page Route="/">
    <Card Title="A"/>
    <Card Title="B"/>
  </Page>
</Goxml>`

// findFirstFor 返回子树中第一个 For 节点。
func findFirstFor(n *contracts.NodeIR) *contracts.NodeIR {
	if n == nil {
		return nil
	}
	if n.For != nil {
		return n
	}
	for _, c := range n.Children {
		if f := findFirstFor(c); f != nil {
			return f
		}
	}
	if n.If != nil {
		for _, c := range n.If.Then {
			if f := findFirstFor(c); f != nil {
				return f
			}
		}
		for _, c := range n.If.Else {
			if f := findFirstFor(c); f != nil {
				return f
			}
		}
	}
	return nil
}

// firstButtonEvent 返回子树中第一个按钮事件的实参切片（原地写目标）。
func firstButtonEvent(n *contracts.NodeIR) *contracts.EventBinding {
	if n == nil {
		return nil
	}
	for i := range n.Events {
		if n.Events[i].Name == "Click" {
			return &n.Events[i]
		}
	}
	for _, c := range n.Children {
		if ev := firstButtonEvent(c); ev != nil {
			return ev
		}
	}
	if n.If != nil {
		for _, c := range n.If.Then {
			if ev := firstButtonEvent(c); ev != nil {
				return ev
			}
		}
		for _, c := range n.If.Else {
			if ev := firstButtonEvent(c); ev != nil {
				return ev
			}
		}
	}
	return nil
}

// collectButtons 收集子树中全部 Button 节点（按出现顺序）。
func collectButtons(n *contracts.NodeIR, out *[]*contracts.NodeIR) {
	if n == nil {
		return
	}
	if n.Tag == "Button" {
		*out = append(*out, n)
	}
	for _, c := range n.Children {
		collectButtons(c, out)
	}
	if n.If != nil {
		for _, c := range n.If.Then {
			collectButtons(c, out)
		}
		for _, c := range n.If.Else {
			collectButtons(c, out)
		}
	}
}

// TestDefInstanceIsolationUnderInPlaceMutation 判别探针：
// 实例 1 的 For.In / 事件实参绑定做**原地元素写入**后，模板与实例 2 必须不受影响。
// 若失败 = cloneNode 深拷贝注释不成立（共享后备数组），需要最小修复。
func TestDefInstanceIsolationUnderInPlaceMutation(t *testing.T) {
	doc := expand(t, isolationSrc)

	if len(doc.Defs) == 0 || doc.Defs[0].Def == nil || doc.Defs[0].Def.Body == nil {
		t.Fatal("模板 Def 不在 DocumentIR.Defs 中，无法判别模板隔离")
	}
	tplFor := findFirstFor(doc.Defs[0].Def.Body)
	if tplFor == nil || tplFor.For == nil || !tplFor.For.In.IsBinding {
		t.Fatal("模板 For 未找到或 In 非绑定")
	}

	// 页面按顺序应有两个实例的 For。
	var forNodes []*contracts.NodeIR
	var walkAll func(n *contracts.NodeIR)
	walkAll = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		if n.For != nil {
			forNodes = append(forNodes, n)
		}
		for _, c := range n.Children {
			walkAll(c)
		}
		if n.If != nil {
			for _, c := range n.If.Then {
				walkAll(c)
			}
			for _, c := range n.If.Else {
				walkAll(c)
			}
		}
	}
	walkAll(doc.Page)
	if len(forNodes) != 2 {
		t.Fatalf("展开后应有两个实例 For，got %d", len(forNodes))
	}
	inst1, inst2 := forNodes[0], forNodes[1]
	if !inst1.For.In.IsBinding || !inst2.For.In.IsBinding {
		t.Fatal("两个实例的 For.In 都应是绑定")
	}

	// 原地写入实例 1 的 For.In 首段（最强的隔离判别：不经过任何 append）。
	inst1.For.In.Segments[0] = "corrupted-by-inst1"

	if tplFor.For.In.Segments[0] == "corrupted-by-inst1" {
		t.Errorf("模板 For.In 被实例 1 的原地写入污染：cloneNode 对 ForClause.In 共享 Segments（别名实锤）")
	}
	if inst2.For.In.Segments[0] == "corrupted-by-inst1" {
		t.Errorf("兄弟实例 For.In 被实例 1 的原地写入污染（共享后备数组）")
	}
	// 探针自证：写入确实生效在 inst1 上（不是写了个寂寞）。
	if inst1.For.In.Segments[0] != "corrupted-by-inst1" {
		t.Fatalf("探针写入未生效（用例无效）: %q", inst1.For.In.Segments[0])
	}
}

// TestDefEventArgsIsolationUnderInPlaceMutation 事件实参绑定同判别：
// 实例 1 的事件实参绑定原地写入后，模板与实例 2 的事件不得变。
func TestDefEventArgsIsolationUnderInPlaceMutation(t *testing.T) {
	doc := expand(t, isolationSrc)

	var tplEv *contracts.EventBinding
	if len(doc.Defs) > 0 && doc.Defs[0].Def != nil {
		tplEv = firstButtonEvent(doc.Defs[0].Def.Body)
	}
	var buttons []*contracts.NodeIR
	collectButtons(doc.Page, &buttons)
	if len(buttons) != 2 {
		t.Fatalf("应有两个实例的按钮，got %d", len(buttons))
	}
	ev1 := firstButtonEvent(buttons[0])
	ev2 := firstButtonEvent(buttons[1])
	if ev1 == nil || ev2 == nil || len(ev1.Args) == 0 || len(ev2.Args) == 0 {
		t.Fatal("按钮事件实参缺失")
	}
	if !ev1.Args[0].Binding.IsBinding || !ev2.Args[0].Binding.IsBinding {
		t.Fatal("事件实参应是绑定")
	}

	ev1.Args[0].Binding.Segments[0] = "corrupted-by-inst1"

	if tplEv != nil && len(tplEv.Args) > 0 && tplEv.Args[0].Binding.Segments[0] == "corrupted-by-inst1" {
		t.Errorf("模板事件实参被实例 1 的原地写入污染")
	}
	if ev2.Args[0].Binding.Segments[0] == "corrupted-by-inst1" {
		t.Errorf("兄弟实例事件实参被污染（Args/Binding 共享后备数组）")
	}
	if ev1.Args[0].Binding.Segments[0] != "corrupted-by-inst1" {
		t.Fatalf("探针写入未生效（用例无效）")
	}
}
