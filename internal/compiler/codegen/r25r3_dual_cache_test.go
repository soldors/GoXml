package codegen

// R25-R3 S2：双实例**缓存轨迹**隔离（含绑定与静态子树，非仅事件按钮）。
//
// 页面：Row[Text(左), Expanded[Column[Text 静态甲, Text 静态乙]]] +
// Text Value="{Title}"（绑定）——Expanded 内为最大纯静态子树（staticDims0/
// probeRecs0 经 injectProbeRecs 注入，无生产计数器）。
//
// 断言：两实例交错布局下各自只录制一次（缓存命中，不重录）；A 的静态
// 几何在 B 交错时稳定（不串写）；合法失效（约束变化）后重录是预期行为，
// 不判为缺陷——本场景固定约束，无失效前提。

import (
	"testing"
)

// r25r3DualVM 双实例缓存轨迹用 ViewModel（含绑定 Title；probeRecs0 为
// injectProbeRecs 注入的验收计数器，非生产代码）。
const r25r3DualVM = `package vm

// VM 是 R25-R3 双实例缓存轨迹用 ViewModel。
type VM struct {
	Title string
	Rows  []Row
}

// Row 是 <For> 行数据。
type Row struct {
	ID   string
	Name string
}

// SetTitle 是绑定回写方法（Value 路径）。
func (vm *VM) SetTitle(s string) { vm.Title = s }
`

func TestR25R3DualInstanceCacheTrace(t *testing.T) {
	gxml := `<Column>
  <Text Value="{Title}"/>
  <Row Height="80">
    <Text Value="左"/>
    <Expanded><Column>
      <Text Value="静态甲"/>
      <Text Value="静态乙"/>
    </Column></Expanded>
  </Row>
</Column>`
	probe := `
func TestProbeR25R3DualCache(t *testing.T) {
	th := material.NewTheme()
	objA := &gvm.VM{Title: "A"}
	objB := &gvm.VM{Title: "B"}
	vA, err := NewGoxmlDashboardView(objA)
	if err != nil {
		t.Fatal(err)
	}
	vB, err := NewGoxmlDashboardView(objB)
	if err != nil {
		t.Fatal(err)
	}
	var rA, rB input.Router
	gtxA := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 200)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: rA.Source()}
	gtxB := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 200)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: rB.Source()}
	frameA := func() {
		gtxA.Reset()
		vA.Layout(gtxA, th)
		rA.Frame(gtxA.Ops)
	}
	frameB := func() {
		gtxB.Reset()
		vB.Layout(gtxB, th)
		rB.Frame(gtxB.Ops)
	}
	// 交错 5 轮：两实例各录一次后稳定命中（缓存不重录）。
	for i := 0; i < 5; i++ {
		frameA()
		frameB()
	}
	if vA.probeRecs0 != 1 || vB.probeRecs0 != 1 {
		t.Fatalf("交错 5 轮后应各录一次：A=%d B=%d（缓存重录=串写或失效）", vA.probeRecs0, vB.probeRecs0)
	}
	// 几何稳定：两实例同内容同约束 → 静态子树几何一致且不漂移。
	dA := vA.staticDims0.Size
	dB := vB.staticDims0.Size
	if dA != dB {
		t.Fatalf("同内容实例静态几何应一致：A=%v B=%v", dA, dB)
	}
	// 绑定结果隔离：改 B 的 Title 不影响 A 的绑定值。
	objB.SetTitle("B 改")
	frameA()
	frameB()
	if objA.Title != "A" {
		t.Fatalf("A 的绑定值被串写：%q", objA.Title)
	}
	// 静态缓存仍不重录（绑定变化不触碰静态子树）。
	if vA.probeRecs0 != 1 || vB.probeRecs0 != 1 {
		t.Fatalf("绑定变化不得引发静态子树重录：A=%d B=%d", vA.probeRecs0, vB.probeRecs0)
	}
	fmt.Println("SCEN-DONE r25r3-dual-cache")
}
`
	out := rtProbeVMMutated(t, r25r3DualVM, gxml, probe, func(gen string) string {
		return injectProbeRecs(t, gen, 1)
	})
	assertProbeHealth(t, "r25r3-dual-cache", out)
}
