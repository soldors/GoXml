package parser

import (
	"math"
	"strconv"
	"strings"

	"github.com/goxml/goxml/contracts"
)

// 布局属性名。
const (
	attrPadding    = "Padding"
	attrGap        = "Gap"
	attrBackground = "Background"
	attrAlign      = "Align"
	attrJustify    = "Justify"
	attrWidth      = "Width"
	attrHeight     = "Height"
	attrColumns    = "Columns"
)

// isLayoutTag 报告标签是否为内置布局标签。
func isLayoutTag(name string) bool {
	switch name {
	case tagColumn, tagRow, tagGrid, tagSpacer, tagExpand:
		return true
	}
	return false
}

// applyLayoutAttrs 把布局属性写入节点。
//
// 返回已消费的属性名集合，供调用方判断剩余属性是否为控件属性。
func (p *parser) applyLayoutAttrs(n *contracts.NodeIR, attrs contracts.AttrList) (map[string]bool, *contracts.CompilerError) {
	consumed := make(map[string]bool, len(attrs))

	for _, a := range attrs {
		span := a.Span
		switch a.Name {
		case attrPadding:
			if a.Value.IsBinding {
				e := p.diag(span, CodeAttrCall, "Padding must be a static value, not a binding")
				return nil, &e
			}
			ins, cerr := p.parseEdgeInsets(a.Value.Raw, span)
			if cerr != nil {
				return nil, cerr
			}
			n.Padding = ins
			consumed[a.Name] = true

		case attrGap:
			v, cerr := p.parseDp(a, "Gap")
			if cerr != nil {
				return nil, cerr
			}
			n.Gap = v
			n.HasGap = true
			consumed[a.Name] = true

		case attrBackground:
			c, cerr := p.parseColorStatic(a, "Background")
			if cerr != nil {
				return nil, cerr
			}
			n.Background = c
			consumed[a.Name] = true

		case attrAlign:
			v, ok := contracts.ParseCrossAlign(a.Value.Raw)
			if a.Value.IsBinding || !ok {
				e := p.diag(span, CodeAttrCall,
					"Align expects one of Start|Center|End|Baseline, got %q", a.Value.Raw)
				return nil, &e
			}
			// Baseline 只在存在文字基线的水平排列中有意义（规范 §3.1.3）。
			// v1 仅允许 <Row Align="Baseline">，其余容器一律编译错误。
			if v == contracts.AlignBaseline && n.Tag != tagRow {
				e := p.diag(span, CodeAttrCall,
					"Align=\"Baseline\" is only allowed on <%s>; <%s> does not support baseline alignment",
					tagRow, n.Tag)
				return nil, &e
			}
			n.Align = v
			consumed[a.Name] = true

		case attrJustify:
			v, ok := contracts.ParseMainJustify(a.Value.Raw)
			if a.Value.IsBinding || !ok {
				e := p.diag(span, CodeAttrCall,
					"Justify expects one of Start|Center|End|SpaceBetween|SpaceAround|SpaceEvenly, got %q", a.Value.Raw)
				return nil, &e
			}
			n.Justify = v
			consumed[a.Name] = true

		case attrWidth:
			v, cerr := p.parseSize(a, "Width")
			if cerr != nil {
				return nil, cerr
			}
			n.Width = v
			consumed[a.Name] = true

		case attrHeight:
			v, cerr := p.parseSize(a, "Height")
			if cerr != nil {
				return nil, cerr
			}
			n.Height = v
			consumed[a.Name] = true

		case attrColumns:
			cols, cerr := p.parseColumns(a)
			if cerr != nil {
				return nil, cerr
			}
			n.Columns = cols
			consumed[a.Name] = true
		}
	}
	return consumed, nil
}

// parseDp 解析单个非负 dp 数值。
func (p *parser) parseDp(a contracts.Attr, name string) (float32, *contracts.CompilerError) {
	if a.Value.IsBinding {
		e := p.diag(a.Span, CodeAttrCall, "%s must be a static value, not a binding", name)
		return 0, &e
	}
	v, err := parseNonNegative(a.Value.Raw)
	if err != nil {
		e := p.diag(a.Span, CodeAttrCall, "invalid %s value %q", name, a.Value.Raw)
		return 0, &e
	}
	return v, nil
}

// parseSize 解析 Width/Height：Fill 或非负 dp。
func (p *parser) parseSize(a contracts.Attr, name string) (contracts.SizeSpec, *contracts.CompilerError) {
	if a.Value.IsBinding {
		e := p.diag(a.Span, CodeAttrCall, "%s must be a static value, not a binding", name)
		return contracts.SizeSpec{}, &e
	}
	if a.Value.Raw == "Fill" {
		return contracts.FillSize(), nil
	}
	v, err := parseNonNegative(a.Value.Raw)
	if err != nil {
		e := p.diag(a.Span, CodeAttrCall, "%s expects Fill or a non-negative number, got %q", name, a.Value.Raw)
		return contracts.SizeSpec{}, &e
	}
	return contracts.FixedSize(v), nil
}

// parseColorStatic 解析颜色属性。
//
// 颜色属性接受三种静态形式（#RRGGBB / #RRGGBBAA / $Theme.Field），
// 也接受绑定语法——因为在 <Def> 体内它可能是对 color 类型 Prop 的引用：
//
//	<Def Name="Card" Props="Accent:color">
//	  <Column Background="{Accent}">
//
// Parser 不知道哪些标识符是 Prop（那是 Expander 的知识），因此这里
// 允许绑定通过，由 Expander 在展开时替换为实际颜色；若最终仍未被替换，
// 说明它不是 Prop 引用，由 Analyzer 报告类型错误。
//
// Background 字段只在静态/主题引用时有值；绑定形式保持 nil，
// 真实颜色在展开后写入。
func (p *parser) parseColorStatic(a contracts.Attr, name string) (*contracts.NRGBAColor, *contracts.CompilerError) {
	if a.Value.IsBinding {
		// 留待 Expander 替换；此处不产生诊断。
		return nil, nil
	}
	return p.parseColor(a.Value.Raw, a.Span, name)
}

// parseColumns 解析 Grid 的 Columns：空格分隔的 数字 / * / N*。
func (p *parser) parseColumns(a contracts.Attr) ([]contracts.GridColumn, *contracts.CompilerError) {
	if a.Value.IsBinding {
		e := p.diag(a.Span, CodeAttrCall, "Columns must be a static value, not a binding")
		return nil, &e
	}
	fields := strings.Fields(a.Value.Raw)
	if len(fields) == 0 {
		e := p.diag(a.Span, CodeAttrCall, "Columns must declare at least one column")
		return nil, &e
	}
	cols := make([]contracts.GridColumn, 0, len(fields))
	flexSeen := false
	for _, f := range fields {
		if strings.HasSuffix(f, "*") {
			flexSeen = true
			num := strings.TrimSuffix(f, "*")
			weight := float32(1)
			if num != "" {
				v, err := strconv.ParseFloat(num, 32)
				if err != nil || v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) {
					e := p.diag(a.Span, CodeAttrCall, "invalid flex column %q; expected * or N* with finite N > 0", f)
					return nil, &e
				}
				weight = float32(v)
			}
			cols = append(cols, contracts.GridColumn{Mode: contracts.GridColumnFlex, Value: weight})
			continue
		}
		v, err := parseNonNegative(f)
		if err != nil {
			e := p.diag(a.Span, CodeAttrCall,
				"invalid column %q; expected a non-negative dp number, * or N*", f)
			return nil, &e
		}
		cols = append(cols, contracts.GridColumn{Mode: contracts.GridColumnFixed, Value: v})
	}
	_ = flexSeen
	return cols, nil
}
