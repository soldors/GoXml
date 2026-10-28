package parser

import "testing"

func TestGridRejectsNonFiniteWeights(t *testing.T) {
	for _, weight := range []string{"Inf*", "+Inf*", "-Inf*", "NaN*"} {
		t.Run(weight, func(t *testing.T) {
			mustFail(t, gridSrc(weight, `<Text Value="a"/>`), CodeAttrCall)
		})
	}
}

func TestGridPreservesFiniteFractionalWeights(t *testing.T) {
	doc := mustParse(t, gridSrc("0.5* 2*", `<Text Value="a"/>`))
	cols := doc.Page.Children[0].Columns
	if len(cols) != 2 || cols[0].Value != 0.5 || cols[1].Value != 2 {
		t.Fatalf("finite weights changed: %+v", cols)
	}
}
