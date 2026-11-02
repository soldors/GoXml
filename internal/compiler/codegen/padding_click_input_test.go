package codegen

import "testing"

func TestFinalGridPaddingGeometry(t *testing.T) {
	sizeRuntime(t, `<Grid Columns="40 60" Padding="12"><Text Value="A"/></Grid>`, `
func TestProbeGridPaddingGeometry(t *testing.T) {
 th:=material.NewTheme();v:=&GoxmlDashboardView{};var ops op.Ops
 v.Layout(sexact(&ops,200,120),th)
 win:=snew(t);defer win.Release()
 b:=sbbox(sshot(t,win,&ops),image.Rect(0,0,200,120))
 if b.N==0||b.MinX<12||b.MinX>16||b.MinY<12||b.MinY>24 {t.Fatalf("Grid Padding geometry=%v",b)}
 t.Logf("Grid Padding=12 geometry=%v",b)
}
`)
}

// This filename is intentionally the same as the discarded probe's filename.
// go list must include it in TestGoFiles; _input is not a platform suffix.
func TestFinalPaddedButtonTwoViews(t *testing.T) {
	out := rtProbe(t, `<Column Padding="16"><Button Padding="8" Text="go" Click="Increment"/></Column>`, `
func TestProbePaddedButtonTwoViews(t *testing.T) {
 th:=material.NewTheme()
 var routers [2]input.Router
 vms:=[2]*fixture.ViewModel{{},{}}
 views:=[2]*GoxmlDashboardView{}
 for i:=range views { var err error;views[i],err=NewGoxmlDashboardView(vms[i]);if err!=nil {t.Fatal(err)} }
 var ops [2]op.Ops
 frame:=func(i int){ops[i].Reset();g:=rtGtx(&ops[i],260,140);g.Source=routers[i].Source();views[i].Layout(g,th);routers[i].Frame(&ops[i])}
 click:=func(i int,x,y float32){routers[i].Queue(pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Press,Source:pointer.Mouse,Buttons:pointer.ButtonPrimary},pointer.Event{Position:f32.Pt(x,y),Kind:pointer.Release,Source:pointer.Mouse});frame(i)}
 frame(0);frame(1)
 click(0,4,4);click(0,18,18)
 if vms[0].Count!=0||vms[1].Count!=0 {t.Fatal("outer/container and leaf padding must not be clickable")}
 click(0,40,40)
 if vms[0].Count!=1||vms[1].Count!=0 {t.Fatalf("first click leaked: %d/%d",vms[0].Count,vms[1].Count)}
 click(1,40,40)
 if vms[0].Count!=1||vms[1].Count!=1 {t.Fatalf("second view isolated hit failed: %d/%d",vms[0].Count,vms[1].Count)}
 frame(0);frame(1)
 if vms[0].Count!=1||vms[1].Count!=1 {t.Fatal("quiet frame repeated click")}
 t.Logf("OUTER_PADDING=not-hit LEAF_PADDING=not-hit CONTENT=1/1 INDEPENDENT_VIEWS=true")
}
`)
	if !contains(out, "[err=nil]") || !contains(out, "INDEPENDENT_VIEWS=true") {
		t.Fatal(out)
	}
}

func TestFinalPaddedInputSubmit(t *testing.T) {
	out := rtProbe(t, `<Column Padding="16"><Input Padding="8" Value="{Title}" ValueChanged="RecordText" Submit="RecordText"/></Column>`, `
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
