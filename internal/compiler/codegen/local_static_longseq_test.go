package codegen

import (
	"testing"
)

// ============================================================================
// R11-B B2：局部静态子树长序列所有权验证（≥200 轮有界序列）。
//
// 每轮交替 Constraints/Metric/主题指针/If 显示隐藏；帧 Ops 每帧 Reset
//（gtx.Reset），缓存自持 Ops 只在失效重录时 Reset——由 recs 计数的增长
// 轨迹只证明重录次数受本次序列约束，不证明Ops内存/不可达记录无累积。
//
// 对照：独立未缓存参考 View（每检查点新建、首帧即真实布局）的几何。
// 断言 geometry/Dimensions，不断言 VM 字段、不只断言"不 panic"。
// 双实例交错渲染/失效；Padding+Background 既有外框一并覆盖。
// ============================================================================

// localStaticIfPage 是长序列页面：静态子树（含 Padding+Background 外框）
// + If 显示切换分支 + 动态绑定文字。
const localStaticIfPage = `<Column>
  <Column>
    <Text Value="静态标题A" Background="#EFEFEF" Padding="4"/>
    <Text Value="静态标题B" Background="#F7F7F7" Padding="4"/>
  </Column>
  <If Condition="{ShowA}">
    <Then>
      <Text Value="分支A可见" Background="#E3F2FD"/>
    </Then>
    <Else>
      <Text Value="分支B可见" Background="#FBE9E7"/>
    </Else>
  </If>
  <Text Value="{Title}"/>
</Column>`

// TestLocalStaticLongSequenceOwnership 200 轮有界序列：交替约束/DPI/主题/
// If 隐藏，断言 (1) 静态子树录制次数只随真实 key 变化增长（有界），
// (2) 每检查点几何与未缓存参考一致，(3) If 分支切换正确，(4) 双实例
// 交错失效互不影响，(5) 动态文字每轮更新。
func TestLocalStaticLongSequenceOwnership(t *testing.T) {
	vmSrc := localStaticVM
	out := rtProbeVMMutated(t, vmSrc, localStaticIfPage, `
func TestProbeLongSequence(t *testing.T) {
	th1 := material.NewTheme()
	th2 := material.NewTheme()
	obj := &gvm.VM{}
	obj.SetShowA(true)
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	// 参考视图：每个检查点新建，首帧布局即未缓存参考几何。
	refDims := func(w, h int, dpi int, language string) image.Point {
		rv, err := NewGoxmlDashboardView(&gvm.VM{})
		if err != nil {
			t.Fatal(err)
		}
		var rr input.Router
		rg := layout.Context{
			Ops:        new(op.Ops),
			Constraints: layout.Exact(image.Pt(w, h)),
			Metric:     unit.Metric{PxPerDp: float32(dpi), PxPerSp: float32(dpi)},
			Source:     rr.Source(),
		}
		rg.Locale.Language = language
		rg.Reset()
		rv.Layout(rg, th1)
		return rv.staticDims0.Size
	}
	var r input.Router
	gtx := layout.Context{
		Ops:   new(op.Ops),
		Source: r.Source(),
	}
	frame := func(w, h, dpi int, th *material.Theme, language string) {
		gtx.Reset()
		gtx.Constraints = layout.Exact(image.Pt(w, h))
		gtx.Metric = unit.Metric{PxPerDp: float32(dpi), PxPerSp: float32(dpi)}
		gtx.Locale.Language = language
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	fmt.Println("SCEN longseq start")
	// 200 轮：4 轮一组 × 50 组，交替宽/窄 × DPI 1/2 × 主题 × If 翻转。
	for round := 0; round < 200; round++ {
		w, h := 300, 200
		if round%2 == 1 {
			w, h = 500, 260
		}
		dpi := 1
		if round%4 >= 2 {
			dpi = 2
		}
		th := th1
		if round%4 >= 2 {
			th = th2
		}
		if round%10 == 9 {
			obj.SetShowA(!obj.ShowA)
		}
		language := "en"
		if round%3 == 1 { language = "ar" }
		beforeA,beforeB := v.probeRecs1,v.probeRecs2
		frame(w, h, dpi, th, language)
		obj.Title = fmt.Sprintf("动态文字第%03d轮", round)
		frame(w, h, dpi, th, language)
		if len(v.slots)!=1 || v.slots[0].text!=obj.Title { t.Fatalf("round%d actual display stale: slots=%v want=%q",round,v.slots,obj.Title) }
		if obj.ShowA {
		 if v.probeRecs1!=beforeA+1 || v.probeRecs2!=beforeB { t.Fatalf("round%d Then branch not isolated",round) }
		} else if v.probeRecs2!=beforeB+1 || v.probeRecs1!=beforeA { t.Fatalf("round%d Else branch not isolated",round) }
		// 每 20 轮一个检查点：几何对照 + recs 有界性
		if round%20 == 19 {
			want := refDims(w, h, dpi, language)
			got := v.staticDims0.Size
			fmt.Printf("SCEN longseq round=%d recs=%d dims=%v ref=%v showA=%v title=%q\n",
				round, v.probeRecs0, got, want, obj.ShowA, obj.Title)
			if got != want {
				t.Fatalf("轮%d: 静态子树几何与未缓存参考不一致: got=%v want=%v", round, got, want)
			}
		}
	}
	// 录制次数有界性：recs 只随真实 key 变化帧增长。
	// key 变化源：宽窄交替(100) + DPI 交替(100) + 主题交替(50) —— 每帧
	// 组合不同 → 理论上限 200；下界 > 10（证明序列确实触发了重录）。
	if v.probeRecs0 < 10 || v.probeRecs0 > 200 {
		t.Fatalf("录制次数超出有界区间: %d", v.probeRecs0)
	}
	fmt.Printf("SCEN longseq final recs=%d showA=%v title=%q\n",
		v.probeRecs0, obj.ShowA, obj.Title)
	fmt.Println("SCEN-DONE longseq")
}
`, func(gen string) string {
		return injectProbeRecs(t, gen, 3)
	})
	assertProbeHealth(t, "longseq", out)
	t.Log(out)
}

// TestLocalStaticTwoInstanceInterleaved 双实例交错渲染/失效：A/B 各自
// 独立的 staticOps 互不指向对方的 Ops 区间；A 失效重录不影响 B 的回放。
func TestLocalStaticTwoInstanceInterleaved(t *testing.T) {
	out := rtProbeVMMutated(t, localStaticVM, localStaticPage, `
func TestProbeTwoInstanceInterleaved(t *testing.T) {
	th := material.NewTheme()
	objA, objB := &gvm.VM{}, &gvm.VM{}
	vA, err := NewGoxmlDashboardView(objA)
	if err != nil {
		t.Fatal(err)
	}
	vB, err := NewGoxmlDashboardView(objB)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:        new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:     unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:     r.Source(),
	}
	frame := func() {
		gtx.Reset()
		vA.Layout(gtx, th)
		vB.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	frame()
	// 交错失效/渲染 60 轮：约束逐轮交替——两实例共享同一 gtx，各自独立
	// 计数应完全同步（recA == recB 逐轮相等），dims 各自正确跟随。
	// 若两实例共享同一 staticOps（串写），CallOp 回放会 panic 或 dims 错乱。
	for i := 0; i < 60; i++ {
		if i%2 == 0 {
			gtx.Constraints = layout.Exact(image.Pt(400, 200))
		} else {
			gtx.Constraints = layout.Exact(image.Pt(300, 200))
		}
		frame()
		if vA.probeRecs0 != vB.probeRecs0 {
			t.Fatalf("轮%d: 两实例计数失去同步: A=%d B=%d", i, vA.probeRecs0, vB.probeRecs0)
		}
		want := image.Pt(400, 44)
		if i%2 == 1 {
			want = image.Pt(300, 44)
		}
		if vA.staticDims0.Size.X != want.X || vB.staticDims0.Size.X != want.X {
			t.Fatalf("轮%d: dims 宽度未跟随约束: A=%v B=%v", i, vA.staticDims0.Size, vB.staticDims0.Size)
		}
	}
	fmt.Printf("SCEN interleaved recA=%d recB=%d dimsA=%v dimsB=%v\n",
		vA.probeRecs0, vB.probeRecs0, vA.staticDims0.Size, vB.staticDims0.Size)
	fmt.Println("SCEN-DONE interleaved")
}
`, func(gen string) string {
		return injectProbeRecs(t, gen, 1)
	})
	assertProbeHealth(t, "interleaved", out)
	t.Log(out)
}
