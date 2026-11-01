package expander

import (
	"github.com/goxml/goxml/contracts"
	"testing"
)

func TestReviewNestedPropBindingIsolation(t *testing.T) {
	doc := expand(t, `<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel"><Defs>
 <Def Name="Inner" Props="Label:string"><Text Value="{Label}"/></Def>
 <Def Name="Outer" Props="Title:string"><Column><Inner Label="{Title}"/><Inner Label="{Title}"/></Column></Def>
 </Defs><Page Route="/"><Outer Title="{User.Name}"/></Page></Goxml>`)
	var texts []*contracts.NodeIR
	var walk func(*contracts.NodeIR)
	walk = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		if n.Tag == "Text" {
			texts = append(texts, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Page)
	if len(texts) != 2 {
		t.Fatalf("want 2 text instances, got %d", len(texts))
	}
	a, _ := texts[0].Attr("Value")
	b, _ := texts[1].Attr("Value")
	if len(a.Value.Segments) != 2 || len(b.Value.Segments) != 2 {
		t.Fatal("binding replacement did not reach target")
	}
	a.Value.Segments[0] = "corrupted"
	if b.Value.Segments[0] != "User" {
		t.Fatalf("nested instances share substituted binding: %v", b.Value.Segments)
	}
}

func TestReviewDefSchemaParamsIsolation(t *testing.T) {
	n := &contracts.NodeIR{Def: &contracts.DefClause{Events: []contracts.EventSchema{{Name: "Changed", Params: []contracts.EventParam{{Name: "value", Type: contracts.PropString}}}}}}
	a, b := cloneNode(n), cloneNode(n)
	a.Def.Events[0].Params[0].Name = "corrupted"
	if n.Def.Events[0].Params[0].Name != "value" || b.Def.Events[0].Params[0].Name != "value" {
		t.Fatal("Def event schema params still shared")
	}
}

func TestReviewStaticBackgroundIsolation(t *testing.T) {
	doc := expand(t, `<Goxml Version="1"><Defs><Def Name="Card"><Column Background="#123456"><Text Value="hello"/></Column></Def></Defs><Page Route="/"><Card/><Card/></Page></Goxml>`)
	var nodes []*contracts.NodeIR
	var walk func(*contracts.NodeIR)
	walk = func(n *contracts.NodeIR) {
		if n == nil {
			return
		}
		if n.Background != nil {
			nodes = append(nodes, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Page)
	if len(nodes) != 2 {
		t.Fatalf("want 2 backgrounds, got %d", len(nodes))
	}
	before := *nodes[1].Background
	nodes[0].Background.R ^= 255
	if *nodes[1].Background != before {
		t.Fatal("static background shared between component instances")
	}
}
