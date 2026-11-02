package codegen

import "testing"

// The first glyph proves the fixed width before Padding; the following sibling
// proves the fixed outer height. Merely checking the first glyph's left inset
// cannot distinguish a missing sizeWrap from a correct fixed-size container.
func TestReviewPaddingFixedOuterAndSibling(t *testing.T) {
	sizeRuntime(t, `<Column Width="120" Height="80" Padding="10"><Row Width="Fill" Justify="End"><Text Value="A"/></Row></Column><Text Value="Z" Height="40"/>`, `
func TestProbeReviewFixedOuter(t *testing.T) {
 th:=material.NewTheme();v:=&GoxmlDashboardView{};var ops op.Ops
 v.Layout(sexact(&ops,300,200),th)
 win:=snew(t);defer win.Release();img:=sshot(t,win,&ops)
 first:=sbbox(img,image.Rect(0,0,300,70))
 sibling:=sbbox(img,image.Rect(0,80,300,120))
 t.Logf("first=%v sibling=%v",first,sibling)
 if first.N==0 || first.MinX<90 || first.MaxX>111 {t.Fatalf("fixed outer width120 then Padding10 was not honored: %v",first)}
 if sibling.N==0 || sibling.MinY<80 || sibling.MinY>100 {t.Fatalf("fixed outer height80 did not place following sibling at80: %v",sibling)}
}
`)
}
