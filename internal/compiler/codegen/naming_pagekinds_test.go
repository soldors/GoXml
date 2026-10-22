package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// C4-PRE-2：预检对三类页面的判定规则（卡 §10.4.3 第 2 条前置验收项）
//
// 接线之后，预检的每一个"保守"之处都会变成"合法工程生成不了"，所以
// 必须先把「谁声明什么」钉成表。规则来源（读生成器得出的两个事实）：
//
//   - renderKeyDecl：**无条件**发射（codegen.go 两条返回路径都写它）
//     → 三类页面都声明 goxmlRenderKey<Source>；
//   - bindCacheDecl：调用点在 `if g.usesVM` 之内（静态页不进），
//     函数首行又是 `if slots == 0 { return "" }`（binding.go:219）
//     → 只有**含绑定槽位**的页面才声明 goxmlTextCache<Source>。
//
// | 页面形态                    | View/New | RenderKey | TextCache |
// |-----------------------------|:--------:|:---------:|:---------:|
// | 静态页（无 VM、无绑定、无事件）|    ✓     |     ✓     |     ✗     |
// | 纯事件页（有 VM、有 Click）   |    ✓     |     ✓     |     ✗     |
// | 绑定页（有 `{...}` 绑定）     |    ✓     |     ✓     |     ✓     |
//
// 预检集合（generatedPackageSymbols）的四项，是**槽位数已知时**的上界；
// C5 起按 `bindSlots > 0` 收窄：静态页与纯事件页不再计入 textCache
// （判据是槽位数，不是 usesVM——纯事件页 usesVM 为真但零槽位）。
// CLI 侧证据：`TestNamingStaticPageTextCacheNotOverreported`、
// `TestNamingEventOnlyPageTextCacheNotOverreported`。
// 槽位**未知**（前端未通过）时仍按四项全含的保守上界处理。
//
// 变红方式（已实测，见 dev-log `2026-10-22.md` §5.4）：
//   - 把 renderKeyDecl 的调用挪进 `if g.usesVM` → 静态页那一格红（真冲突会漏报）。
// ============================================================================

// declaresType 报告产物是否声明了该**类型**（不是出现该子串就完事：
// 字段引用、方法接收者也会让子串命中）。
func declaresType(t *testing.T, src, typeName string) bool {
	t.Helper()
	return strings.Contains(src, "type "+typeName+" struct")
}

// TestNamingPageKindsSymbolSets：**四类页面** × 两个包级类型的对照表。
//
// 第 4 行「绑定全在 <For> 内」来自契约 §16.5.1 的同一张表：这类页面有 VM、
// 有绑定，但 `<For>` 体内不分配槽位（列表契约 §6.2），所以槽位数为 0、
// 不声明 TextCache。它必须进表——只靠槽位测试间接证明，会让
// 「页面分类」与「槽位统计」两套口径的漂移无人察觉（C1 核对 G2）。
func TestNamingPageKindsSymbolSets(t *testing.T) {
	keyType, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	staticSrc := string(mustGen(t, `<Text Value="Hello"/>`,
		Options{PackageName: "demo", SourcePath: "home.gxml"}))
	eventSrc := string(mustGen(t, `<Button Text="加一" Click="Increment"/>`,
		Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "home.gxml"}))
	bindingSrc := string(mustGen(t, `<Column><Text Value="Static"/><Text Value="{Title}"/></Column>`,
		Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "home.gxml"}))
	// 绑定全部位于 <For> 体内：绑定语法存在，但槽位数为 0。
	forOnlySrc := string(mustGen(t, `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`,
		Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "home.gxml"}))

	cases := []struct {
		name      string
		src       string
		wantKey   bool
		wantCache bool
	}{
		{"静态页", staticSrc, true, false},
		{"纯事件页", eventSrc, true, false},
		{"绑定页", bindingSrc, true, true},
		// 与纯事件页的区别：它有绑定（不是"无绑定"），判据落在槽位数上。
		{"绑定全在 For 内", forOnlySrc, true, false},
	}
	for _, c := range cases {
		gotKey := declaresType(t, c.src, keyType)
		gotCache := declaresType(t, c.src, cacheType)
		if gotKey != c.wantKey {
			t.Errorf("%s 声明 %s = %v，期望 %v", c.name, keyType, gotKey, c.wantKey)
		}
		if gotCache != c.wantCache {
			t.Errorf("%s 声明 %s = %v，期望 %v（契约 §16.5.1 第 4 行）",
				c.name, cacheType, gotCache, c.wantCache)
		}
	}
}

// TestNamingPrecheckSetIsUpperBoundOfAllKinds：预检集合必须是每类产物
// 实际声明集合的**超集**——漏一项就可能漏报真冲突。
func TestNamingPrecheckSetIsUpperBoundOfAllKinds(t *testing.T) {
	declared, err := generatedPackageSymbols("home.gxml", 0, false)
	if err != nil {
		t.Fatalf("generatedPackageSymbols: %v", err)
	}

	keyType, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}
	// 预检集合必须同时含两者：少任何一个都不再是"上界"。
	if !containsSymbol(declared, keyType) {
		t.Errorf("预检集合缺少 %q（三类页面都声明它，缺了会漏报）: %v", keyType, declared)
	}
	if !containsSymbol(declared, cacheType) {
		t.Errorf("预检集合缺少 %q（绑定页声明它，缺了会漏报）: %v", cacheType, declared)
	}
}
