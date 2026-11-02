package codegen

import (
	"fmt"
	"strings"
	"testing"
)

func TestReviewBackgroundExactAlphaInputAndState(t *testing.T) {
	for _, tag := range []string{"Text", "Input"} {
		t.Run(tag, func(t *testing.T) {
			body := fmt.Sprintf(`<%s Width="80" Height="40" Padding="8" Background="#FF000080" Value=""/><Text Width="80" Height="40" Value=""/>`, tag)
			src := mustGen(t, body, Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "dashboard.gxml"})
			header := strings.Replace(sizeProbeHeader, `"image"`, `"image"
 "image/color"
 "gioui.org/op/clip"
 "gioui.org/op/paint"`, 1)
			out, err := genModuleWithRepo(t, src, header+`
func TestProbeReviewAlphaRegion(t *testing.T) {
 th:=material.NewTheme();v:=&GoxmlDashboardView{};var ops op.Ops
 gtx:=sexact(&ops,200,120);win:=snew(t);defer win.Release()
 var control op.Ops;stack:=clip.Rect{Max:image.Pt(30,30)}.Push(&control)
 paint.Fill(&control,color.NRGBA{R:255,A:128});stack.Pop()
 expected:=sshot(t,win,&control).RGBAAt(10,10)
 if expected.A!=128 {t.Fatalf("direct Gio half-alpha control unexpected: %v",expected)}
 for frame:=0;frame<2;frame++ {
  ops.Reset();v.Layout(gtx,th);img:=sshot(t,win,&ops)
  for _,p:=range []struct{x,y,want int}{{10,10,128},{1,1,128},{10,50,0}} {
   c:=img.RGBAAt(p.x,p.y)
   t.Logf("frame=%d point=(%d,%d) rgba=%v wantAlpha=%d",frame,p.x,p.y,c,p.want)
   if int(c.A)<p.want-1||int(c.A)>p.want+1 {t.Fatalf("alpha/region mismatch at%v: %v",p,c)}
   if p.want>0 && c!=expected {t.Fatalf("generated color differs from direct Gio control: got%v want%v",c,expected)}
  }
 }
}
`)
			if err != nil || strings.Contains(out, "--- SKIP") {
				t.Fatalf("probe failed or skipped: %v\n%s", err, out)
			}
			t.Log(out)
		})
	}
}
