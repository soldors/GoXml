package codegen

import (
	"github.com/goxml/goxml/contracts"
	"testing"
)

func TestReviewInteractiveClassification(t *testing.T) {
	g := &generator{}
	for _, tag := range []string{"Button", "Input"} {
		n := &contracts.NodeIR{Tag: tag}
		if g.localStaticSubtree(n) {
			t.Fatalf("interactive %s classified static", tag)
		}
	}
}

func TestReviewLocalStaticDynamicSiblingAndLocale(t *testing.T) {
	out := rtProbeVM(t, localStaticVM, `<Column><Column Padding="10"><Text Value="island" Background="#FF0000"/></Column><Text Value="{Title}"/><Button Text="plain"/></Column>`, `
func TestProbeIndependent(t *testing.T) {
 th:=material.NewTheme()
 obj:=&gvm.VM{Title:"old"}
 v,err:=NewGoxmlDashboardView(obj);if err!=nil {t.Fatal(err)}
 var r input.Router
 gtx:=layout.Context{Ops:new(op.Ops),Constraints:layout.Exact(image.Pt(400,300)),Metric:unit.Metric{PxPerDp:1,PxPerSp:1},Source:r.Source()}
 frame:=func(){gtx.Reset();v.Layout(gtx,th);r.Frame(gtx.Ops)}
 frame();first:=v.staticDims0
 if v.staticRecs0!=1||len(v.slots)!=1||v.slots[0].text!="old" {t.Fatalf("initial recs=%d slots=%v",v.staticRecs0,v.slots)}
 obj.Title="actually changed"
 frame()
 if v.staticRecs0!=1||v.slots[0].text!="actually changed"||v.staticDims0!=first {t.Fatalf("sibling stale or island changed recs=%d text=%q",v.staticRecs0,v.slots[0].text)}
 gtx.Locale.Language="ar";frame()
 if v.staticRecs0!=2||v.staticKey0.locale.Language!="ar" {t.Fatalf("locale did not invalidate recs=%d",v.staticRecs0)}
 frame();if v.staticRecs0!=2 {t.Fatal("locale stable rerecorded")}
 cached:=testing.Benchmark(func(b *testing.B){b.ReportAllocs();for i:=0;i<b.N;i++{gtx.Reset();v.Layout(gtx,th)}})
 forced:=testing.Benchmark(func(b *testing.B){b.ReportAllocs();for i:=0;i<b.N;i++{v.staticCached0=false;gtx.Reset();v.Layout(gtx,th)}})
 fmt.Printf("LOCALSTATIC-BENCH cached ns/op=%d allocs/op=%d forced ns/op=%d allocs/op=%d\n",cached.NsPerOp(),cached.AllocsPerOp(),forced.NsPerOp(),forced.AllocsPerOp())
 if cached.AllocsPerOp()!=0 {t.Fatalf("cached allocs/op=%d",cached.AllocsPerOp())}
 fmt.Println("SCEN-DONE review-local-static")
}
`)
	assertProbeHealth(t, "review-local-static", out)
	t.Log(out)
}
