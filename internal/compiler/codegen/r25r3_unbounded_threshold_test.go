package codegen

import (
	"fmt"
	"github.com/goxml/goxml/internal/compiler/sizelayout"
	"testing"
)

// The implementation has an explicit unbounded sentinel. 100000 is below it.
func TestR25R3UnboundedThresholdGeometry(t *testing.T) {
	probe := fmt.Sprintf(`
func TestProbeR25R3Threshold(t *testing.T) {
 obj:=&gvm.VM{};v,err:=NewGoxmlDashboardView(obj);if err!=nil {t.Fatal(err)}
 th:=material.NewTheme();var r input.Router
 gtx:=layout.Context{Ops:new(op.Ops),Metric:unit.Metric{PxPerDp:1,PxPerSp:1},Source:r.Source()}
 for _,maxY:=range []int{%d,%d,%d} {
  gtx.Reset();gtx.Constraints=layout.Constraints{Max:image.Pt(300,maxY)}
  d:=v.Layout(gtx,th);r.Frame(gtx.Ops)
  if d.Size.Y!=maxY {t.Fatalf("maxY=%%d dims=%%v",maxY,d.Size)}
 }
 fmt.Println("SCEN-DONE r25r3-threshold")
}
`, sizelayout.UnboundedThreshold-1, sizelayout.UnboundedThreshold, sizelayout.UnboundedThreshold+1)
	out := rtProbeVM(t, r25r2SharedVM, `<Column><Expanded><Button Text="B" Click="TapB"/></Expanded></Column>`, probe)
	assertProbeHealth(t, "r25r3-threshold", out)
}
