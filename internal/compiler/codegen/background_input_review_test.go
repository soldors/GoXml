package codegen

import "testing"

func TestReviewBackgroundInputEvents(t *testing.T) {
	out := rtProbe(t, `<Column Padding="16"><Input Background="#FF000080" Padding="8" Value="{Title}" ValueChanged="RecordText" Submit="RecordText"/></Column>`, `
func TestProbePaddedInputSubmit(t *testing.T) {
 th:=material.NewTheme();vm:=&fixture.ViewModel{};v,err:=NewGoxmlDashboardView(vm);if err!=nil {t.Fatal(err)}
 var r input.Router;var ops op.Ops
 gtx:=rtGtx(&ops,260,140);gtx.Source=r.Source()
 frame:=func(){ops.Reset();v.Layout(gtx,th);r.Frame(&ops)}
 click:=func(x,y float32){r.Queue(pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Press,Source:pointer.Mouse,Buttons:pointer.ButtonPrimary},pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Release,Source:pointer.Mouse});frame()}
 frame();click(4,4)
 if gtx.Focused(&v.input0) {t.Fatal("outer padding acquired focus")}
 click(18,18)
 if gtx.Focused(&v.input0) {t.Fatal("leaf padding acquired focus")}
 click(35,35)
 if !gtx.Focused(&v.input0) {t.Fatal("content failed to acquire focus")}
 r.Queue(key.EditEvent{Range:key.Range{},Text:"abc"});frame()
 if v.input0.Text()!="abc"||vm.Title!="abc"||vm.LastText!="abc" {t.Fatalf("edit chain failed: editor=%q model=%q payload=%q",v.input0.Text(),vm.Title,vm.LastText)}
 vm.LastText="not-submitted";frame()
 if vm.LastText!="not-submitted" {t.Fatal("quiet frame emitted submit/change")}
 r.Queue(key.Event{Name:key.NameReturn,State:key.Press},key.Event{Name:key.NameReturn,State:key.Release});frame()
 if vm.LastText!="abc" {t.Fatalf("Return payload=%q",vm.LastText)}
 vm.LastText="not-submitted";frame();frame()
 if vm.LastText!="not-submitted" {t.Fatal("quiet frames repeated submit")}
 t.Log("PADDING_FOCUS=false CONTENT_FOCUS=true EDIT=abc RETURN_PAYLOAD=abc")
}
`)
	if !contains(out, "[err=nil]") || !contains(out, "RETURN_PAYLOAD=abc") {
		t.Fatal(out)
	}
}
