package codegen

import (
	"testing"
)

// ============================================================================
// C5：显示缓存槽位派生的一致性护栏
//
// slots.go 把「哪个 <Text> 占槽」抽成单一规则 textBindSlot，供两处使用：
//   - generator.nodeExpr（发射时分配序号并累加 g.bindSlots）；
//   - CountBindSlots（预检在不发射的情况下统计）。
//
// 规则是单一来源，但**遍历仍有两处**（发射递归 vs countSlots 递归）。
// 本文件是那条缝隙的护栏：对若干页面形态断言
//
//	CountBindSlots(v) > 0  ⟺  产物里声明了 goxmlTextCache<Source>
//
// 这两个判定走的路径完全不同（一个是纯遍历，一个是真的生成），
// 所以只要有一条不一致，就说明两处遍历在某类节点上分叉了——
// 那会让预检按错误的槽位数收窄，正是 C5 要防的回归。
//
// 变红方式（已实测，见 dev-log `2026-10-22.md` §6）：
//   - 把 countSlots 的 inFor 传播改成恒 false → 「For 内绑定」那条红
//     （预检会以为有槽位，而产物并不声明 TextCache）；
//   - 把 textBindSlot 的 takesSlot 改成 `!inFor || true` → 同上反向；
//   - 把 countSlots 的 children 递归去掉 → 嵌套绑定那条红。
// ============================================================================

// slotsAndArtifact 对一段页面片段同时给出槽位数与产物字节。
//
// 走的是生产路径：verified → CountBindSlots / Generate，不另造夹具。
func slotsAndArtifact(t *testing.T, body string) (int, []byte) {
	t.Helper()
	v := verified(t, body)
	n, err := CountBindSlots(v)
	if err != nil {
		t.Fatalf("CountBindSlots: %v", err)
	}
	out, err := Generate(v, Options{
		PackageName: "demo",
		ModulePath:  "github.com/goxml/goxml",
		SourcePath:  "home.gxml",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return n, out
}

// TestSlotCountAgreesWithDeclaredTextCache：槽位数为零 ⟺ 不声明 TextCache。
func TestSlotCountAgreesWithDeclaredTextCache(t *testing.T) {
	_, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	cases := []struct {
		name string
		body string
		// wantSlots 是期望槽位数；-1 表示「不断言具体值，只要求 >0」。
		wantSlots int
	}{
		// 静态文本不占槽（Body1 直接布局）。
		{"静态文本", `<Text Value="Hello"/>`, 0},
		// 纯事件页：有 VM、有 Clickable，但没有任何绑定 → 0 槽。
		{"纯事件页", `<Button Text="加一" Click="Increment"/>`, 0},
		// 单个绑定 <Text> → 1 槽。
		{"单个绑定", `<Text Value="{Title}"/>`, 1},
		// 静态与绑定混排：只有绑定那个占槽。
		{"静态+绑定混排", `<Column><Text Value="Static"/><Text Value="{Title}"/></Column>`, 1},
		// 两个绑定 → 2 槽（序号连续分配）。
		{"两个绑定", `<Column><Text Value="{Title}"/><Text Value="{Title}"/></Column>`, 2},
		// <For> 体内的绑定**不**占槽（<For> 列表契约 §6.2），
		// 但页面已因绑定而动态 —— 这正是「有 VM 却零槽位」的关键形态，
		// 也是收窄判据必须用 bindSlots 而不是 usesVM 的原因。
		{"For 内绑定", `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`, 0},
		// For 内绑定 + For 外绑定 → 只有外面那个占槽。
		{"For 内外各一", `<Column><Text Value="{Title}"/><For Each="item" In="{Items}"><Text Value="{item.Name}"/></For></Column>`, 1},
		// <If> 包裹的绑定照常占槽（分支不影响槽位分配）。
		{"If 内绑定", `<If Condition="{Dirty}"><Then><Text Value="{Title}"/></Then></If>`, 1},
	}

	for _, c := range cases {
		n, out := slotsAndArtifact(t, c.body)
		declared := declaresType(t, string(out), cacheType)

		if n != c.wantSlots {
			t.Errorf("%s：CountBindSlots = %d，期望 %d", c.name, n, c.wantSlots)
		}
		// 核心不变式：槽位数为零 ⟺ 不声明 TextCache。
		if wantDeclared := c.wantSlots > 0; declared != wantDeclared {
			t.Errorf("%s：槽位数 %d → 声明 %s = %v，期望 %v\n%s",
				c.name, n, cacheType, declared, wantDeclared, headOf(string(out), 40))
		}
	}
}
