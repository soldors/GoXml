package parser

import (
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/goxml/goxml/contracts"
)

// elementAttrs 把 xml.StartElement 的属性转换为带源码位置的 AttrList。
//
// encoding/xml 的 xml.Attr 不带位置，因此这里回到原始字节区间重新扫描：
// 起始标签原文是 src[tok.start:tok.end]，在其中按顺序查找属性名即可得到
// 每个属性的绝对偏移，再换算为行列。
func (p *parser) elementAttrs(tok token, se xml.StartElement) (contracts.AttrList, *contracts.CompilerError) {
	if len(se.Attr) == 0 {
		return nil, nil
	}

	raw, base := p.rawStartTag(tok)
	// searchFrom 保证同名属性按出现顺序匹配，不会重复定位到第一个。
	searchFrom := 0

	list := make(contracts.AttrList, 0, len(se.Attr))
	seen := make(map[string]bool, len(se.Attr))

	for _, a := range se.Attr {
		name := a.Name.Local

		// 重复属性检查（编码器不区分大小写地比较过，这里按 GXML 规范做精确检查）。
		if seen[name] {
			sp := p.smap.span(tok.start, tok.end)
			e := p.diag(sp, CodeAttrDup, "duplicate attribute %q on <%s>", name, se.Name.Local)
			return nil, &e
		}
		seen[name] = true

		nameIdx := strings.Index(raw[searchFrom:], name)
		var nameRel = searchFrom
		if nameIdx >= 0 {
			nameRel = searchFrom + nameIdx
			searchFrom = nameRel + len(name)
		}
		nameOff := base + int64(nameRel)

		// 属性值的原文区间：从 name 之后找到引号包裹的内容。
		valRelStart, valRelEnd := p.valueRange(raw, nameRel)
		valStart := base + int64(valRelStart)
		valEnd := base + int64(valRelEnd)
		span := p.smap.span(nameOff, valEnd)

		val, cerr := p.classifyValue(a.Value, int(valStart), int(valEnd), name, se.Name.Local)
		if cerr != nil {
			return nil, cerr
		}

		list = append(list, contracts.Attr{Name: name, Value: val, Span: span})
	}
	return list, nil
}

// rawStartTag 返回起始标签的原始字节（去掉结尾的 '>'），
// 并把 base 修正到 '<' 的实际偏移。
//
// tok.start 是上一个 token 的结束位置，因此可能包含前导空白与注释。
// 若不修正，属性名在原文中的相对偏移会整体偏移，导致诊断列号错位。
func (p *parser) rawStartTag(tok token) (raw string, base int64) {
	end := tok.end
	if end > tok.start && end <= int64(len(p.src)) {
		// InputOffset 指向 '>' 之后，故 end-1 是 '>'。
		if p.src[end-1] == '>' {
			end--
		}
	}
	start := tok.start
	// 定位真正的 '<'：从 tok.start 起找最后一个 '<'。
	for i := end - 1; i >= tok.start; i-- {
		if p.src[i] == '<' {
			start = i
			break
		}
	}
	return p.src[start:end], start
}

// valueRange 在起始标签原文中定位属性值（引号内内容）的相对区间。
// nameRelOff 是属性名在原文中的偏移。
func (p *parser) valueRange(raw string, nameRelOff int) (int, int) {
	i := nameRelOff + 1 // 跳过名字至少一个字符
	// 跳到 '='
	for i < len(raw) && raw[i] != '=' {
		i++
	}
	if i >= len(raw) {
		return nameRelOff, nameRelOff
	}
	i++ // 跳过 '='
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) {
		return nameRelOff, nameRelOff
	}
	quote := raw[i]
	if quote != '"' && quote != '\'' {
		return i, i
	}
	start := i + 1
	end := start
	for end < len(raw) && raw[end] != quote {
		end++
	}
	return start, end
}

// classifyValue 判断属性值是静态字面量还是绑定，并做语法层校验。
//
// 规范 §4.1：属性值只有两种语义——静态字面量或完整绑定，不允许混合。
// 规范 §4.2：绑定形式为 {Path} 或 {!Path}，Path 为标识符以 "." 分隔。
func (p *parser) classifyValue(raw string, valStart, valEnd int, attrName, tag string) (contracts.BindingVal, *contracts.CompilerError) {
	span := p.smap.span(int64(valStart), int64(valEnd))

	// 检测是否包含花括号（判断混合写法）。
	hasOpen := strings.ContainsRune(raw, '{')
	hasClose := strings.ContainsRune(raw, '}')

	if !hasOpen && !hasClose {
		// 纯静态值。
		return contracts.BindingVal{Raw: raw}, nil
	}

	// 必须是完整的单个绑定：以 { 开头、以 } 结尾、且内部不再有花括号。
	if !strings.HasPrefix(raw, "{") || !strings.HasSuffix(raw, "}") {
		e := p.diag(span, CodeAttrCall,
			"attribute %q mixes static text and binding; use either a literal or a single {Path}", attrName)
		return contracts.BindingVal{}, &e
	}
	inner := raw[1 : len(raw)-1]
	if strings.ContainsAny(inner, "{}") {
		e := p.diag(span, CodeAttrCall, "attribute %q must contain exactly one binding", attrName)
		return contracts.BindingVal{}, &e
	}

	neg := false
	if strings.HasPrefix(inner, "!") {
		neg = true
		inner = inner[1:]
	}
	if inner == "" {
		e := p.diag(span, CodeAttrCall, "empty binding path in attribute %q", attrName)
		return contracts.BindingVal{}, &e
	}

	segs, ok := splitPath(inner)
	if !ok {
		e := p.diag(span, CodeAttrCall,
			"invalid binding path %q in attribute %q; expected Identifier(.Identifier)*", inner, attrName)
		return contracts.BindingVal{}, &e
	}

	return contracts.BindingVal{
		IsBinding: true,
		IsNegated: neg,
		Raw:       raw,
		Segments:  segs,
	}, nil
}

// splitPath 把 "User.Profile.Name" 拆分为分段，并校验每段是合法标识符。
func splitPath(s string) ([]string, bool) {
	parts := strings.Split(s, ".")
	if len(parts) == 0 {
		return nil, false
	}
	for _, p := range parts {
		if !isIdentifier(p) {
			return nil, false
		}
	}
	return parts, true
}

// isIdentifier 校验 [_A-Za-z][_A-Za-z0-9]*。
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlpha := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		isDigit := c >= '0' && c <= '9'
		if i == 0 {
			if !isAlpha {
				return false
			}
			continue
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

// ---- 属性值解析辅助：供 layout / 控件属性使用 ----

// parseEdgeInsets 解析 1/2/4 个非负 dp 数值（规范 §4.3）。
func (p *parser) parseEdgeInsets(raw string, span Span) (contracts.EdgeInsets, *contracts.CompilerError) {
	fields := strings.Fields(raw)
	if len(fields) != 1 && len(fields) != 2 && len(fields) != 4 {
		e := p.diag(span, CodeAttrCall,
			"Padding expects 1, 2 or 4 non-negative numbers, got %d", len(fields))
		return contracts.EdgeInsets{}, &e
	}
	vals := make([]float32, len(fields))
	for i, f := range fields {
		v, err := parseNonNegative(f)
		if err != nil {
			e := p.diag(span, CodeAttrCall, "invalid Padding value %q: %v", f, err)
			return contracts.EdgeInsets{}, &e
		}
		vals[i] = v
	}
	switch len(vals) {
	case 1:
		return contracts.EdgeInsets{Top: vals[0], Right: vals[0], Bottom: vals[0], Left: vals[0]}, nil
	case 2:
		return contracts.EdgeInsets{Top: vals[0], Right: vals[1], Bottom: vals[0], Left: vals[1]}, nil
	default:
		return contracts.EdgeInsets{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: vals[3]}, nil
	}
}

// parseNonNegative 解析非负十进制数。
func parseNonNegative(s string) (float32, error) {
	// 规范 §3.1.5：只接受十进制表示，拒绝指数形式与单位后缀。
	// strconv.ParseFloat 会接受 "1e5"、"Inf"、"NaN" 等，必须在此拦掉。
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' {
			continue
		}
		return 0, strconv.ErrSyntax
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, strconv.ErrRange
	}
	return float32(v), nil
}

// parseColor 解析 #RRGGBB、#RRGGBBAA 或 $Theme.Field（规范 §4.4）。
func (p *parser) parseColor(raw string, span Span, attr string) (*contracts.NRGBAColor, *contracts.CompilerError) {
	switch {
	case strings.HasPrefix(raw, "$Theme."):
		field := strings.TrimPrefix(raw, "$Theme.")
		if !isIdentifier(field) {
			e := p.diag(span, CodeAttrCall, "invalid theme field %q in %q", field, attr)
			return nil, &e
		}
		return &contracts.NRGBAColor{IsThemeRef: true, ThemeField: field}, nil
	case strings.HasPrefix(raw, "#"):
		hexs := raw[1:]
		if len(hexs) != 6 && len(hexs) != 8 {
			e := p.diag(span, CodeAttrCall, "%q expects #RRGGBB or #RRGGBBAA, got %q", attr, raw)
			return nil, &e
		}
		v, err := strconv.ParseUint(hexs, 16, 32)
		if err != nil {
			e := p.diag(span, CodeAttrCall, "invalid hex color %q in %q", raw, attr)
			return nil, &e
		}
		c := &contracts.NRGBAColor{A: 0xFF}
		if len(hexs) == 6 {
			c.R = uint8(v >> 16)
			c.G = uint8(v >> 8)
			c.B = uint8(v)
		} else {
			c.R = uint8(v >> 24)
			c.G = uint8(v >> 16)
			c.B = uint8(v >> 8)
			c.A = uint8(v)
		}
		return c, nil
	default:
		e := p.diag(span, CodeAttrCall, "%q expects #RRGGBB, #RRGGBBAA or $Theme.Field, got %q", attr, raw)
		return nil, &e
	}
}
