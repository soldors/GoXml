package codegen

import (
	"strings"
	"testing"
)

// One generated module, one process, identical full page/row layout and lifecycle.
func TestR25R3MatchedBench(t *testing.T) {
	out := r25r3BenchProbe(t, r25r2SharedVM, `<Row Height="60"><Text Value="左"/><Expanded><Button Text="点我" Click="TapA"/></Expanded></Row>`, `
type manualMatched struct { state *GoxmlDashboardView; obj *gvm.VM }
func (v *manualMatched) Layout(gtx layout.Context,th *material.Theme) layout.Dimensions {
 for v.state.click0.Clicked(gtx) { v.obj.TapA();gtx.Execute(op.InvalidateCmd{}) }
 return layout.Flex{Axis:layout.Vertical}.Layout(gtx,layout.Rigid(func(gtx layout.Context)layout.Dimensions {
   gtx.Constraints.Min.Y=60;gtx.Constraints.Max.Y=60
   return layout.Flex{Axis:layout.Horizontal}.Layout(gtx,
     layout.Rigid(func(gtx layout.Context)layout.Dimensions{return material.Body1(th,"左").Layout(gtx)}),
     layout.Flexed(1,func(gtx layout.Context)layout.Dimensions{return material.Button(th,&v.state.click0,"点我").Layout(gtx)}))
 }))
}
var coordinatorSink layout.Dimensions
func benchMatched(b *testing.B,gen,frame bool) {
 th:=material.NewTheme();obj:=&gvm.VM{}
 v,err:=NewGoxmlDashboardView(obj);if err!=nil{b.Fatal(err)}
 m:=&manualMatched{state:&GoxmlDashboardView{},obj:obj}
 var r input.Router
 gtx:=layout.Context{Ops:new(op.Ops),Constraints:layout.Exact(image.Pt(300,60)),Metric:unit.Metric{PxPerDp:1,PxPerSp:1}}
 if frame {gtx.Source=r.Source()}
 draw:=func()layout.Dimensions{gtx.Reset();var d layout.Dimensions;if gen {d=v.Layout(gtx,th)} else {d=m.Layout(gtx,th)};if frame{r.Frame(gtx.Ops)};return d}
 gtx.Reset();dGen:=v.Layout(gtx,th)
 gtx.Reset();dManual:=m.Layout(gtx,th)
 if dGen!=dManual {b.Fatalf("non-equivalent geometry: generated=%v manual=%v",dGen,dManual)}
 for n:=0;n<10;n++ {draw()}
 b.ReportAllocs();b.ResetTimer()
 for n:=0;n<b.N;n++ {coordinatorSink=draw()}
}
func BenchmarkMatchedGeneratedLayoutOnly(b *testing.B){benchMatched(b,true,false)}
func BenchmarkMatchedManualLayoutOnly(b *testing.B){benchMatched(b,false,false)}
func BenchmarkMatchedGeneratedFrame(b *testing.B){benchMatched(b,true,true)}
func BenchmarkMatchedManualFrame(b *testing.B){benchMatched(b,false,true)}
`, "BenchmarkMatched")
	t.Logf("MATCHED BENCH:\n%s", out)
	if !strings.Contains(out, "[err=nil]") || strings.Contains(out, "FAIL") {
		t.Fatal("matched benchmark failed")
	}
	for _, name := range []string{"GeneratedLayoutOnly", "ManualLayoutOnly", "GeneratedFrame", "ManualFrame"} {
		if !strings.Contains(out, "BenchmarkMatched"+name+"-") {
			t.Fatalf("missing %s", name)
		}
	}
}
