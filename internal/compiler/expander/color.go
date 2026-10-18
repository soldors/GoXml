package expander

import (
	"strconv"
	"strings"

	"github.com/goxml/goxml/contracts"
)

// resolveColorAttr 在展开时解析颜色属性的 Prop 引用。
//
// Parser 阶段若颜色属性写成绑定形式（如 Background="{Accent}"），会留空
// 并保留在 RawAttrs 中——因为在 <Def> 体内它可能是对 color 类型 Prop 的
// 引用，而 Parser 不知道哪些标识符是 Prop。
//
// 展开时：若该绑定确实命中某个 Prop，则替换为调用方传入的颜色值并写入
// 专用字段；否则保持原样，留给 Analyzer 报「颜色属性不接受任意绑定」。
func (e *expander) resolveColorAttr(n *contracts.NodeIR, name string, env expandEnv) {
	if n.Background != nil {
		return // 已是静态颜色或主题引用
	}
	ra, ok := n.RawAttrs.Lookup(name)
	if !ok || !ra.Value.IsBinding {
		return
	}
	repl, ok := env.lookup(ra.Value.Segments[0])
	if !ok {
		return
	}

	c, ok := colorFromValue(repl)
	if !ok {
		return
	}
	n.Background = c
}

// colorFromValue 把 BindingVal 解析为颜色。
//
// 接受 #RRGGBB、#RRGGBBAA 与 $Theme.Field 三种静态形式。
func colorFromValue(v contracts.BindingVal) (*contracts.NRGBAColor, bool) {
	if v.IsBinding {
		return nil, false
	}
	raw := strings.TrimSpace(v.Raw)

	if strings.HasPrefix(raw, "$Theme.") {
		field := strings.TrimPrefix(raw, "$Theme.")
		if field == "" {
			return nil, false
		}
		return &contracts.NRGBAColor{IsThemeRef: true, ThemeField: field}, true
	}
	if !strings.HasPrefix(raw, "#") {
		return nil, false
	}
	hexs := raw[1:]
	if len(hexs) != 6 && len(hexs) != 8 {
		return nil, false
	}
	n, err := strconv.ParseUint(hexs, 16, 32)
	if err != nil {
		return nil, false
	}
	c := &contracts.NRGBAColor{A: 0xFF}
	if len(hexs) == 6 {
		c.R = uint8(n >> 16)
		c.G = uint8(n >> 8)
		c.B = uint8(n)
	} else {
		c.R = uint8(n >> 24)
		c.G = uint8(n >> 16)
		c.B = uint8(n >> 8)
		c.A = uint8(n)
	}
	return c, true
}
