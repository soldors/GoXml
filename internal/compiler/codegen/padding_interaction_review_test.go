package codegen

import "testing"

func TestReviewPaddedButtonRealHit(t *testing.T) {
	out := rtProbe(t, `<Column Padding="16"><Button Padding="8" Text="go" Click="Increment"/></Column>`, `
func TestProbePaddingButton(t *testing.T) {
 th:=material.NewTheme();vm:=&fixture.ViewModel{};v,err:=NewGoxmlDashboardView(vm);if err!=nil { t.Fatal(err) }
 var r input.Router
 gtx:=layout.Context{Ops:new(op.Ops),Constraints:layout.Exact(image.Pt(260,140)),Metric:unit.Metric{PxPerDp:1,PxPerSp:1},Source:r.Source()}
 frame:=func(){gtx.Reset();v.Layout(gtx,th);r.Frame(gtx.Ops)}
 click:=func(x,y float32){r.Queue(pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Press,Source:pointer.Mouse,Buttons:pointer.ButtonPrimary},pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Release,Source:pointer.Mouse});frame()}
 frame();click(4,4);click(18,18)
 if vm.Count!=0 {t.Fatalf("padding unexpectedly clickable: %d",vm.Count)}
 click(40,40)
 if vm.Count!=1 {t.Fatalf("content click count=%d want1",vm.Count)}
 frame();frame();if vm.Count!=1 {t.Fatal("duplicate click")}
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatal(out)
	}
}

func TestReviewPaddedInputRealEdit(t *testing.T) {
	out := rtProbe(t, `<Column Padding="16"><Input Padding="8" Value="{Title}" ValueChanged="RecordText"/></Column>`, `
func TestProbePaddingInput(t *testing.T) {
 th:=material.NewTheme();vm:=&fixture.ViewModel{};v,err:=NewGoxmlDashboardView(vm);if err!=nil {t.Fatal(err)}
 var r input.Router
 gtx:=layout.Context{Ops:new(op.Ops),Constraints:layout.Exact(image.Pt(260,140)),Metric:unit.Metric{PxPerDp:1,PxPerSp:1},Source:r.Source()}
 frame:=func(){gtx.Reset();v.Layout(gtx,th);r.Frame(gtx.Ops)}
 click:=func(x,y float32){r.Queue(pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Press,Source:pointer.Mouse,Buttons:pointer.ButtonPrimary},pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Release,Source:pointer.Mouse});frame()}
 frame();click(4,4)
 if gtx.Focused(&v.input0) {t.Fatal("outer padding acquired focus")}
 click(35,35)
 if !gtx.Focused(&v.input0) {t.Fatal("padded editor not focused")}
 r.Queue(key.EditEvent{Range:key.Range{},Text:"abc"});frame()
 if v.input0.Text()!="abc" || vm.LastText!="abc" {t.Fatalf("edit chain editor=%q payload=%q",v.input0.Text(),vm.LastText)}
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatal(out)
	}
}

func TestReviewPaddingMetricChange(t *testing.T) {
	sizeRuntime(t, `<Column Padding="16"><Text Value="A"/></Column>`, `
func TestProbeMetricChange(t *testing.T) {
 th:=material.NewTheme();v:=&GoxmlDashboardView{};win:=snew(t);defer win.Release()
 var ops op.Ops
 gtx:=sexact(&ops,300,120);v.Layout(gtx,th);first:=sbbox(sshot(t,win,&ops),image.Rect(0,0,300,120))
 ops.Reset();gtx.Metric.PxPerDp=1.25;v.Layout(gtx,th);second:=sbbox(sshot(t,win,&ops),image.Rect(0,0,300,120))
 if first.N==0||second.N==0 {t.Fatal("blank observation")}
 if second.MinX-first.MinX!=4 || second.MinY-first.MinY!=4 {t.Fatalf("Metric cache not refreshed: first=%v second=%v",first,second)}
}
`)
}
