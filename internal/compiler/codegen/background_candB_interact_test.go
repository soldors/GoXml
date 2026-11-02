package codegen

// R6-A 片3 候选B 交互与稳定帧（真实 Router 事件 + AllocsPerRun 硬断言）。
// 仅候选分支 codex/r6-candB；验证外框 Background 不吞点击（尤其 Padding 外框区也可点击吗？
// —— Background 绘制不改变事件命中，Padding 区仍不命中）、两实例隔离、稳定帧 0 分配。

import "testing"

// 外框 Background 不吞事件：Padding 区仍不命中（背景绘制 ≠ 事件区域），内容命中。
func TestCandB_BackgroundDoesNotSwallowClick(t *testing.T) {
	out := rtProbe(t, `<Column Padding="16"><Button Padding="8" Background="#22CC55" Text="go" Click="Increment"/></Column>`, `
func TestProbeCandBClick(t *testing.T) {
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
 if vms[0].Count!=0||vms[1].Count!=0 {t.Fatal("padding area must not be clickable even with outer-box Background")}
 click(0,40,40)
 if vms[0].Count!=1||vms[1].Count!=0 {t.Fatalf("content click failed: %d/%d",vms[0].Count,vms[1].Count)}
 click(1,40,40)
 if vms[0].Count!=1||vms[1].Count!=1 {t.Fatalf("two-view isolation failed: %d/%d",vms[0].Count,vms[1].Count)}
 t.Logf("BG_NO_SWALLOW=ok PAD_NOT_HIT=ok CONTENT=1/1 VIEWS_ISOLATED=ok")
}
`)
	if !contains(out, "[err=nil]") || !contains(out, "BG_NO_SWALLOW=ok") {
		t.Fatal(out)
	}
}

// 稳定帧 AllocsPerRun 硬断言：外框 Background 精确绘制不引入每帧堆分配。
func TestCandB_StableFrameAllocsRuntime(t *testing.T) {
	sizeRuntime(t, `<Text Padding="10" Background="#FF0000" Value="A"/>`, `
func TestProbeCandBAllocs(t *testing.T) {
 th:=material.NewTheme();v:=&GoxmlDashboardView{};var ops op.Ops
 gtx:=sexact(&ops,200,120)
 _=v.Layout(gtx,th)
 a:=testing.AllocsPerRun(50, func(){ ops.Reset(); v.Layout(gtx,th) })
 t.Logf("ALLOCS=%v", a)
 if a != 0 { t.Fatalf("稳定帧外框装饰引入堆分配: ALLOCS=%v", a) }
}
`)
}

// 两种 Metric：外框尺寸随 Padding 缩放（Padding 列红区起点不变=0，但内容原点 +4）。
func TestCandB_TwoMetricsRuntime(t *testing.T) {
	sizeRuntime(t, `<Text Padding="16" Background="#FF0000" Value="A"/>`, `
func TestProbeCandBTwoMetrics(t *testing.T) {
 th:=material.NewTheme();v:=&GoxmlDashboardView{};win:=snew(t);defer win.Release()
 var ops op.Ops
 gtx:=sexact(&ops,200,120);v.Layout(gtx,th);img1:=sshot(t,win,&ops)
 ops.Reset();gtx.Metric.PxPerDp=1.25;v.Layout(gtx,th);img2:=sshot(t,win,&ops)
 // 文字像素在 Padding 之后：m1 起点约16，m1.25 约20（差4）。
 textMin := func(img *image.RGBA) int { for x:=0;x<200;x++{ for y:=0;y<100;y++{ i:=y*img.Stride+x*4; r:=img.Pix[i]; g:=img.Pix[i+1]; b:=img.Pix[i+2]; a:=img.Pix[i+3]; if a>128 && !(r>128&&g<100&&b<100) && (r<120||g<120||b<120) { return x } } }; return -1 }
 t1:=textMin(img1);t2:=textMin(img2)
 t.Logf("textMin m1=%d m1.25=%d", t1, t2)
 if t2-t1 != 4 { t.Fatalf("外框 Metric 缩放不符: m1=%d m1.25=%d", t1, t2) }
}
`)
}
