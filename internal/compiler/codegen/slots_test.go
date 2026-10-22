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

// TestSlotCountEqualsEmittedSlots：**槽位绝对数**必须与发射侧累加值逐一相等。
//
// 这是独立复审指出的缺口（P1-1）：既有护栏只断言「零 ⟺ 不声明 TextCache」，
// 只能证明**零与非零**一致——3 个绑定被统计成 2 个，那条护栏是绿的，
// 但 CountBindSlots 这个名字承诺的是「统计槽位数」。本用例把它兑现：
// 以生成器发射时实际累加的 g.bindSlots（经 generateWithSlots 暴露）为基准。
//
// 覆盖形态刻意包含「规则相同但遍历形状不同」的所有已知分支点：
// 嵌套容器、If 的 Then/Else、嵌套 If、For 内的 If、For 内外混排。
func TestSlotCountEqualsEmittedSlots(t *testing.T) {
	opts := Options{
		PackageName: "demo",
		ModulePath:  "github.com/goxml/goxml",
		SourcePath:  "home.gxml",
	}

	cases := []struct {
		name string
		body string
		// want 是**期望的绝对槽位数**（-1 表示只要求与发射值相等）。
		want int
	}{
		{"静态文本", `<Text Value="Hello"/>`, 0},
		{"纯事件页", `<Button Text="加一" Click="Increment"/>`, 0},
		{"单个绑定", `<Text Value="{Title}"/>`, 1},
		{"两个绑定", `<Column><Text Value="{Title}"/><Text Value="{Title}"/></Column>`, 2},
		// 三个绑定：抓「3 被统计成 2」这类绝对值误差。
		{"三个绑定", `<Column><Text Value="{Title}"/><Text Value="{User.Name}"/><Text Value="{User.Email}"/></Column>`, 3},
		// 两层容器嵌套。
		{"两层容器嵌套", `<Column><Row><Column><Text Value="{Title}"/></Column></Row></Column>`, 1},
		// 嵌套容器里多个绑定 + 静态文本混排。
		{"嵌套+混排", `<Column><Text Value="s"/><Row><Text Value="{Title}"/><Text Value="{User.Name}"/></Row><Column><Text Value="s2"/><Text Value="{User.Email}"/></Column></Column>`, 3},
		// If 的 Then 与 Else 各一个绑定 → 2（分支不影响分配）。
		{"If 两分支各一", `<If Condition="{Dirty}"><Then><Text Value="{Title}"/></Then><Else><Text Value="{User.Name}"/></Else></If>`, 2},
		// 嵌套 If。
		{"嵌套 If", `<If Condition="{Dirty}"><Then><If Condition="{Dirty}"><Then><Text Value="{Title}"/></Then></If></Then></If>`, 1},
		// For 内绑定：不占槽（<For> 列表契约 §6.2）。
		{"For 内绑定", `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`, 0},
		// For 内的 If 必须继承 inFor → 分支里的绑定同样不占槽。
		{"For 内 If 的绑定", `<For Each="item" In="{Items}"><If Condition="{item.Done}"><Then><Text Value="{item.Name}"/></Then></If></For>`, 0},
		// For 内外混排。
		{"For 内外混排", `<Column><Text Value="{Title}"/><For Each="item" In="{Items}"><Text Value="{item.Name}"/></For><Text Value="{User.Name}"/></Column>`, 2},
		// 容器里的 For（for 的 inFor 只覆盖其子树，不影响兄弟）。
		{"For 与兄弟绑定", `<Column><Row><For Each="item" In="{Items}"><Text Value="{item.Name}"/></For></Row><Text Value="{Title}"/></Column>`, 1},
	}

	for _, c := range cases {
		v := verified(t, c.body)
		counted, err := CountBindSlots(v)
		if err != nil {
			t.Fatalf("%s: CountBindSlots: %v", c.name, err)
		}
		_, emitted, err := generateWithSlots(v, opts)
		if err != nil {
			t.Fatalf("%s: generateWithSlots: %v", c.name, err)
		}
		if counted != emitted {
			t.Errorf("%s：CountBindSlots = %d，发射实际累加 = %d（必须相等，否则预检会按错误槽位数收窄）",
				c.name, counted, emitted)
		}
		if c.want >= 0 && emitted != c.want {
			t.Errorf("%s：发射实际累加 = %d，期望 %d", c.name, emitted, c.want)
		}
	}
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
