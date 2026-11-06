package codegen

// R25-R2 S1：Expanded 自身属性盘点回归守卫（F-B4 + 属性全量显式拒绝）。
// 每个未定义语义的自身属性都必须**显式拒绝**，不得静默忽略（F-B2/F-B4 类）。
// Width/Height 是唯一支持的自身体（尺寸职责）。

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/expander"
	gxmlparser "github.com/goxml/goxml/internal/compiler/parser"
)

func TestR25R2ExpandedAttrRejections(t *testing.T) {
	cases := []struct {
		name, attr string
	}{
		{"background", `Background="#FF0000"`},
		{"padding", `Padding="8"`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			v := verified(t, `<Row Height="60"><Expanded `+tc.attr+`><Text Value="x"/></Expanded></Row>`)
			_, err := Generate(v, bindingOpts())
			if err == nil {
				t.Fatalf("Expanded 自身 %s 应显式拒绝（静默忽略=F-B4 类缺陷）", tc.attr)
			}
			if !strings.Contains(err.Error(), "不支持") {
				t.Fatalf("拒绝信息应说明不支持: %v", err)
			}
		})
	}
	// Expander 层已准入拒绝（GXML-ATTR-005）：Gap/Align/Justify 在 Expanded
	// 上不可用——runFrontend 会 t.Fatal，故此处直连 Parser→Expander 检查诊断。
	for _, tc := range []struct{ name, attr string }{
		{"gap", `Gap="4"`}, {"align", `Align="Center"`}, {"justify", `Justify="Center"`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src := `<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/"><Row Height="60"><Expanded ` + tc.attr + `><Text Value="x"/></Expanded></Row></Page>
</Goxml>`
			doc, pdiags := gxmlparser.Parse("case.gxml", []byte(src))
			if len(pdiags) > 0 {
				t.Fatalf("Parser 意外拒绝: %v", pdiags)
			}
			er := expander.Expand("case.gxml", doc)
			found := false
			for _, d := range er.Diags {
				if strings.Contains(d.Code, "GXML-ATTR-005") {
					found = true
				}
			}
			if !found {
				t.Fatalf("Expanded 自身 %s 应被 Expander 以 ATTR-005 拒绝，实际 diags=%v", tc.attr, er.Diags)
			}
		})
	}
	// 事件属于子节点：Expanded 自身事件 Expander 已拒（GXML-ATTR-004）。
	{
		src := `<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/"><Row Height="60"><Expanded Click="Increment"><Text Value="x"/></Expanded></Row></Page>
</Goxml>`
		doc, pdiags := gxmlparser.Parse("case.gxml", []byte(src))
		if len(pdiags) > 0 {
			t.Fatalf("Parser 意外拒绝: %v", pdiags)
		}
		er := expander.Expand("case.gxml", doc)
		found := false
		for _, d := range er.Diags {
			if strings.Contains(d.Code, "GXML-ATTR-004") {
				found = true
			}
		}
		if !found {
			t.Fatalf("Expanded 自身事件应被 Expander 以 ATTR-004 拒绝，实际 diags=%v", er.Diags)
		}
	}
}

// TestR25R2ExpandedWidthHeightStillSupported 尺寸职责保持支持（不消除报错凑绿）。
func TestR25R2ExpandedWidthHeightStillSupported(t *testing.T) {
	for _, gxml := range []string{
		`<Row Height="60"><Expanded><Text Value="x"/></Expanded></Row>`,
		`<Row Height="60"><Expanded Width="120"><Text Value="x"/></Expanded></Row>`,
		`<Column Height="200"><Expanded Height="80"><Text Value="x"/></Expanded></Column>`,
	} {
		v := verified(t, gxml)
		if _, err := Generate(v, bindingOpts()); err != nil {
			t.Fatalf("尺寸职责应保持支持: %s: %v", gxml, err)
		}
	}
}
