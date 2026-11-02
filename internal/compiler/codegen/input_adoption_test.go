package codegen

import (
	"strings"
	"testing"
)

func independentR8(t *testing.T, body, probe string) {
	t.Helper()
	out := rtProbeVM(t, probeVMSrc, body, frameProbePrelude+probe)
	t.Log(out)
	if !strings.Contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") || strings.Contains(out, "--- SKIP") {
		t.Fatalf("independent generated probe failed: %s", out)
	}
}

func TestIndependentR8ReadOnlyEcho(t *testing.T) {
	independentR8(t, `<Input Value="{Title}" ReadOnly="true" ValueChanged="IdentityText"/>`, `
func TestProbeReadOnly(t *testing.T) {
 obj,v,r,frame:=divSetup(t,"OLD")
 obj.Title="NEW";frame()
 r.Queue(key.EditEvent{Range:key.Range{},Text:"X"});frame();frame()
 fmt.Printf("READONLY E=%q V=%q count=%d\n",v.input0.Text(),obj.Title,obj.ChangeCount)
 if v.input0.Text()!="NEW" || obj.Title!="NEW" || obj.ChangeCount!=0 {t.Fatal("readonly/echo mismatch")}
}
`)
}

func TestIndependentR8DisabledEcho(t *testing.T) {
	probe := strings.Replace(frameProbePrelude, "v.Layout(gtx, th)", "v.Layout(gtx.Disabled(), th)", 1)
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/>`, strings.Replace(probe, `if !gtx.Focused(&v.input0) { t.Fatal("no focus") }`, "", 1)+`
func TestProbeDisabled(t *testing.T) {
 obj,v,_,frame:=divSetup(t,"OLD")
 obj.Title="NEW";frame();frame()
 fmt.Printf("DISABLED E=%q V=%q count=%d\n",v.input0.Text(),obj.Title,obj.ChangeCount)
 if v.input0.Text()!="NEW" || obj.Title!="NEW" || obj.ChangeCount!=0 { t.Fatal("disabled echo escaped") }
}
`)
	t.Log(out)
	if !strings.Contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") || strings.Contains(out, "--- SKIP") {
		t.Fatal(out)
	}
}

func TestIndependentR8NormalizeAndReturnSameFrame(t *testing.T) {
	independentR8(t, `<Input Value="{Title}" ValueChanged="UpperText" Submit="SubmitRecord"/>`, `
func TestProbeNormalizeReturn(t *testing.T) {
 obj,v,r,frame:=divSetup(t,"")
 r.Queue(key.EditEvent{Range:key.Range{},Text:"abc"},key.Event{Name:key.NameReturn})
 frame()
 fmt.Printf("NORMALIZE-RETURN E=%q V=%q payload=%q changes=%d submits=%d\n",v.input0.Text(),obj.Title,obj.LastSubmit,obj.ChangeCount,obj.SubmitCount)
 if obj.ChangeCount!=1 || obj.SubmitCount!=1 || obj.LastSubmit!="abc" || v.input0.Text()!="ABC" || obj.Title!="ABC" {t.Fatal("unexpected normalize/Return ordering")}
 for i:=4;i<=6;i++ {frame();if obj.ChangeCount!=1 || obj.SubmitCount!=1 {t.Fatal("quiet growth")}}
}
`)
}

func TestIndependentR8SharedLaterInput(t *testing.T) {
	probe := strings.Replace(frameProbePrelude, "image.Pt(300, 40)", "image.Pt(300, 100)", 1)
	out := rtProbeVM(t, probeVMSrc, `<Column><Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Title}" ValueChanged="IdentityText"/></Column>`, probe+`
func TestProbeSharedLater(t *testing.T) {
 obj,v,r,frame:=divSetup(t,"OLD")
 r.Source().Execute(key.FocusCmd{Tag:&v.input1});frame()
 if !r.Source().Focused(&v.input1) {t.Fatal("input1 not focused")}
 r.Queue(key.EditEvent{Range:key.Range{},Text:"X"});frame()
 fmt.Printf("SHARED-FRAME E0=%q E1=%q V=%q count=%d\n",v.input0.Text(),v.input1.Text(),obj.Title,obj.ChangeCount)
 if v.input0.Text()!="OLD" || v.input1.Text()!="XOLD" || obj.Title!="XOLD" {t.Fatal("same-frame layout order changed")}
 frame()
 if v.input0.Text()!=obj.Title || v.input1.Text()!=obj.Title {t.Fatal("must converge on the next frame")}
 for i:=7;i<=8;i++ {frame()}
 fmt.Printf("SHARED-SETTLED E0=%q E1=%q V=%q count=%d\n",v.input0.Text(),v.input1.Text(),obj.Title,obj.ChangeCount)
 if v.input0.Text()!=obj.Title || v.input1.Text()!=obj.Title || obj.ChangeCount!=1 {t.Fatal("shared later editor did not converge")}
}
`)
	t.Log(out)
	if !strings.Contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") || strings.Contains(out, "--- SKIP") {
		t.Fatal(out)
	}
}
