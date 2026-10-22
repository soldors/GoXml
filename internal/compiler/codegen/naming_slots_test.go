package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// C5：预检符号集合按槽位数收窄（三态表）
//
// | BindSlotsKnown | BindSlots | cacheType 计入 | 理由                            |
// |----------------|-----------|----------------|---------------------------------|
// | false          | 任意      | 是             | 前端未通过，保守上界             |
// | true           | 0         | 否             | 产物不声明它（静态/纯事件/全在For）|
// | true           | >0        | 是             | 产物真声明，漏算就是漏报         |
//
// 符号名从生产函数派生的 generatedTypeNames 取，不写死字面量。
// ============================================================================

func TestGeneratedSymbolsNarrowBySlots(t *testing.T) {
	view, err := viewSymbol("home.gxml")
	if err != nil {
		t.Fatalf("viewSymbol: %v", err)
	}
	keyType, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	cases := []struct {
		name       string
		slots      int
		known      bool
		wantCache  bool
		wantOthers []string
	}{
		{"未知槽位（保守）", 0, false, true, nil},
		{"已知 0 槽（静态/纯事件）", 0, true, false, nil},
		{"已知 2 槽（绑定页）", 2, true, true, nil},
	}

	for _, c := range cases {
		syms, err := generatedPackageSymbols("home.gxml", c.slots, c.known)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		joined := "\n" + strings.Join(syms, "\n") + "\n"
		hasCache := strings.Contains(joined, "\n"+cacheType+"\n")
		if hasCache != c.wantCache {
			t.Errorf("%s：cacheType %q 计入 = %v，期望 %v（符号集合 %v）",
				c.name, cacheType, hasCache, c.wantCache, syms)
		}
		// 其余三个符号**任何时候**都必须在：收窄只允许影响 cacheType。
		for _, must := range []string{view, "New" + view, keyType} {
			if !strings.Contains(joined, "\n"+must+"\n") {
				t.Errorf("%s：符号集合缺 %q（收窄不应影响它）：%v", c.name, must, syms)
			}
		}
	}
}
