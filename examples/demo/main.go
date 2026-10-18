// Command demo 演示 Goxml 的 Parser + Expander 管线：
// 解析 .gxml 源码、按组件 Schema 分类属性与事件、内联展开 <Def>/<Slot>，
// 最后把展开后的渲染树打印出来。
//
// 这不是渲染器：Codegen 与运行时尚未实现。它只证明前端管线可用。
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

const src = `<Goxml Version="1" ViewModel="demo.ViewModel">
  <Defs>
    <Def Name="Field" Props="Label:string; Value:string">
      <Row Gap="12" Align="Center">
        <Text Value="{Label}" Width="72"/>
        <Input Value="{Value}" Width="Fill"/>
      </Row>
    </Def>
  </Defs>
  <Page Route="/settings">
    <Column Padding="24" Gap="16">
      <Text Value="账户设置"/>
      <Field Label="姓名" Value="{User.Name}"/>
      <Field Label="邮箱" Value="{User.Email}"/>
      <If Condition="{Dirty}">
        <Then>
          <Row Gap="8" Justify="End">
            <Button Text="保存" Click="Save" Enabled="{CanSave}"/>
          </Row>
        </Then>
      </If>
    </Column>
  </Page>
</Goxml>`

func main() {
	doc, diags := parser.Parse("demo.gxml", []byte(src))
	fmt.Printf("Parser   : 诊断 %d 条\n", len(diags))
	for _, d := range diags {
		fmt.Println("           ", d)
	}
	if doc == nil {
		fail()
	}

	r := expander.Expand("demo.gxml", doc)
	fmt.Printf("Expander : 诊断 %d 条\n", len(r.Diags))
	for _, d := range r.Diags {
		fmt.Println("           ", d)
	}
	if r.Doc == nil || r.Doc.Page == nil {
		fail()
	}

	fmt.Printf("\n展开后的渲染树（<Field> 已内联为 <Row>）：\n\n")
	dump(r.Doc.Page, 1)
}

func dump(n *contracts.NodeIR, depth int) {
	if n == nil {
		return
	}
	pad := strings.Repeat("  ", depth)

	// 结构节点先打印自身，再打印子句内容。
	switch n.Kind {
	case contracts.KindIf:
		fmt.Printf("%sIf %s\n", pad, n.If.Condition.Raw)
		for _, c := range n.If.Then {
			dump(c, depth+1)
		}
		return
	case contracts.KindFor:
		fmt.Printf("%sFor each=%s in=%s\n", pad, n.For.Each, n.For.In.Path())
		for _, c := range n.Children {
			dump(c, depth+1)
		}
		return
	}

	fmt.Printf("%s<%s>\n", pad, n.Tag)
	for _, line := range describe(n) {
		fmt.Printf("%s  %s\n", pad, line)
	}
	for _, c := range n.Children {
		dump(c, depth+1)
	}
}

// describe 汇总节点上已分类的属性、事件与布局信息。
func describe(n *contracts.NodeIR) []string {
	var out []string
	for _, a := range n.Attrs {
		out = append(out, fmt.Sprintf("attr   %-9s = %s", a.Name, a.Value.Raw))
	}
	for _, e := range n.Events {
		args := make([]string, 0, len(e.Args))
		for _, a := range e.Args {
			if a.Kind == contracts.ArgBinding {
				args = append(args, a.Binding.Path())
			} else {
				args = append(args, a.Literal)
			}
		}
		sig := e.Method
		if len(args) > 0 {
			sig += "(" + strings.Join(args, ", ") + ")"
		}
		out = append(out, fmt.Sprintf("event  %-9s -> %s", e.Name, sig))
	}
	if !n.Padding.IsZero() {
		out = append(out, fmt.Sprintf("layout Padding   = %v", n.Padding))
	}
	if n.HasGap {
		out = append(out, fmt.Sprintf("layout Gap       = %v", n.Gap))
	}
	if n.Align != contracts.AlignUnset {
		out = append(out, fmt.Sprintf("layout Align     = %v", n.Align))
	}
	if n.Justify != contracts.JustifyUnset {
		out = append(out, fmt.Sprintf("layout Justify   = %v", n.Justify))
	}
	if n.Width.IsSet() {
		out = append(out, fmt.Sprintf("layout Width     = %+v", n.Width))
	}
	if n.Background != nil {
		if n.Background.IsThemeRef {
			out = append(out, "layout Background = $Theme."+n.Background.ThemeField)
		} else {
			out = append(out, fmt.Sprintf("layout Background = #%02X%02X%02X%02X",
				n.Background.R, n.Background.G, n.Background.B, n.Background.A))
		}
	}
	return out
}

func fail() {
	fmt.Println("\n前端管线未通过，停止。")
	os.Exit(1)
}
