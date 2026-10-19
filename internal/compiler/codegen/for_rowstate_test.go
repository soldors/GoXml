package codegen

import (
	"strings"
	"testing"

	gxmlparser "github.com/goxml/goxml/internal/compiler/parser"
)

// ============================================================================
// 步骤 3+4：<For> 行状态池与数据驱动轮询（<For> 列表契约 §3/§4/§5）
//
// 证据分层：
//   - 结构：池字段、rowClick 辅助、数据驱动轮询、键清理
//   - 行为：真实指针点击第 N 行，处理器收到的必须是第 N 行的 ID
//   - 生命周期：行删除后池被清理（白盒检查池长度）
// ============================================================================

// TestForRowPoolStructure 结构断言（契约 §3/§4/§5）。
func TestForRowPoolStructure(t *testing.T) {
	v := verified(t, `<For Each="item" In="{Items}" Key="{item.ID}"><Button Text="选" Click="Delete(item.ID)"/></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		// 池字段：map[K]*widget.Clickable（map 元素不可寻址 → 指针）
		"rowClicks0 map[int]*widget.Clickable",
		// 惰性创建辅助
		"func (v *GoxmlDashboardView) rowClick0(key int) *widget.Clickable",
		// 数据驱动轮询：同源同序
		"rowItems := v.vm.Items",
		"v.rowClicks0[rowItem.ID]",
		"v.vm.Delete(rowItem.ID)",
		// 渲染期从池取
		"material.Button(th, v.rowClick0(item.ID)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("生成代码缺 %q:\n%s", want, s)
		}
	}
	// 键清理存在
	if !strings.Contains(s, "delete(v.rowClicks0, k)") {
		t.Errorf("应生成键清理:\n%s", s)
	}
	// 轮询里不得出现渲染期变量 item（它只在渲染作用域可见）
	if strings.Contains(s, "v.vm.Delete(item.ID)") {
		t.Errorf("轮询作用域里不得出现渲染期变量名 item（未定义）:\n%s", s)
	}
}

// TestForRowPoolRequiresKey 循环内按钮无 Key 必须被拦下（契约 §3.1）。
//
// 防线在 Parser：SCOPE-003 先于 Analyzer/Codegen 拒绝，因此 Codegen 的
// 防御性兜底在正常管线下不可达——这正是「层层设防」的预期形态。
// 本测试断言 Parser 的拦截确实存在且错误码正确。
func TestForRowPoolRequiresKey(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="` + vmPath + `">
  <Page Route="/"><For Each="item" In="{Items}"><Button Text="x" Click="Delete(item.ID)"/></For></Page>
</Goxml>`
	doc, pdiags := gxmlparser.Parse("case.gxml", []byte(src))
	_ = doc
	found := false
	for _, d := range pdiags {
		if d.Code == "GXML-SCOPE-003" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Parser 应以 GXML-SCOPE-003 拒绝含交互控件但缺 Key 的 <For>，诊断: %v", pdiags)
	}
	t.Log("防线在 Parser（SCOPE-003）；Codegen 的兜底在正常管线下不可达——层层设防的预期形态")
}

// TestForRowClickHitsCorrectRow 核心验收：点击第 3 行只触发第 3 行的处理器
// （契约 §9 第 5 项——列表正确性的分水岭）。
func TestForRowClickHitsCorrectRow(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}" Key="{item.ID}"><Button Text="选" Click="RecordNum(item.ID)"/></For>`, `
func TestProbeRowClick(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Items: []fixture.Item{
		{ID: 101, Name: "row1"},
		{ID: 202, Name: "row2"},
		{ID: 303, Name: "row3"},
	}}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(200, 300)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()

	// material.Button 高度约 38-40px；点第 2 行（y≈50）。
	click := func(y float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(20, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(20, y), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}

	click(50)
	if vm.LastNum != 202 {
		t.Fatalf("点第 2 行: 期望 202, 实际 %d——行身份串位", vm.LastNum)
	}
	click(90)
	if vm.LastNum != 303 {
		t.Fatalf("点第 3 行: 期望 303, 实际 %d——行身份串位", vm.LastNum)
	}
	t.Logf("点击第 2/3 行分别收到 202/303——行点击命中正确行")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("行点击验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestForRowStateCleanupOnDelete 行删除后池被清理（契约 §4，白盒）。
func TestForRowStateCleanupOnDelete(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}" Key="{item.ID}"><Button Text="选" Click="RecordNum(item.ID)"/></For>`, `
func TestProbeCleanup(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Items: []fixture.Item{
		{ID: 1, Name: "a"},
		{ID: 2, Name: "b"},
		{ID: 3, Name: "c"},
	}}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(200, 300)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
	}

	frame()
	frame()
	if len(v.rowClicks0) != 3 {
		t.Fatalf("3 行应建 3 个状态，实际 %d", len(v.rowClicks0))
	}

	// 删到只剩 1 行 → 下一帧池应收缩到 1。
	vm.Items = vm.Items[:1]
	frame()
	if len(v.rowClicks0) != 1 {
		t.Fatalf("删行后池应收缩到 1，实际 %d——存在无界增长", len(v.rowClicks0))
	}

	// 清空 → 池应为空（map 本体保留以复用容量）。
	vm.Items = nil
	frame()
	if len(v.rowClicks0) != 0 {
		t.Fatalf("清空后池应为 0，实际 %d", len(v.rowClicks0))
	}
	t.Log("行删除 → 池收缩；清空 → 池清空（无界增长被关闭）")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("键清理验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestForRowStateStableAcrossFrames 同一行跨帧状态稳定（Key 的意义所在）。
//
// 列表重排后同 ID 行的状态必须跟随数据，而不是留在旧位置。
func TestForRowStateFollowsData(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}" Key="{item.ID}"><Button Text="选" Click="RecordNum(item.ID)"/></For>`, `
func TestProbeStable(t *testing.T) {
	th := material.NewTheme()
	items := []fixture.Item{
		{ID: 1, Name: "a"},
		{ID: 2, Name: "b"},
	}
	vm := &fixture.ViewModel{Items: items}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(200, 300)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
	}
	frame()
	frame()

	// 记录 ID=1 的状态对象指针。
	c1 := v.rowClicks0[1]

	// 重排：ID=2 挪到最前。
	vm.Items = []fixture.Item{{ID: 2, Name: "b"}, {ID: 1, Name: "a"}}
	frame()

	// 同一 ID 的状态对象必须仍是同一个（状态跟随数据）。
	if v.rowClicks0[1] != c1 {
		t.Fatal("重排后 ID=1 的状态对象被重建——Key 未生效，状态会串位")
	}
	t.Log("列表重排后同 Key 的行状态保持同一对象（状态跟随数据）")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("状态稳定验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestForTwoButtonsPerRowSeparatePools 同一行的多个按钮必须各持独立 Clickable。
//
// 缺陷背景（步骤 5 前发现）：池原先按 <For> 组织，同一行的两个按钮共享
// 一个 Clickable——点击只命中声明顺序里的第一个处理器，第二个永远收不到；
// 且两者的悬停/按下状态互相串。
func TestForTwoButtonsPerRowSeparatePools(t *testing.T) {
	v := verified(t, `<For Each="item" In="{Items}" Key="{item.ID}"><Button Text="删" Click="Delete(item.ID)"/><Button Text="选" Click="RecordNum(item.ID)"/></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"rowClicks0 map[int]*widget.Clickable",
		"rowClicks1 map[int]*widget.Clickable",
		"v.rowClick0(item.ID)",
		"v.rowClick1(item.ID)",
		"v.vm.Delete(rowItem.ID)",
		"v.vm.RecordNum(rowItem.ID)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("生成代码缺 %q:\n%s", want, s)
		}
	}
}

// TestForTwoButtonsClickIndependent 运行时：点行的「选」只触发选，不触发删。
func TestForTwoButtonsClickIndependent(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}" Key="{item.ID}"><Button Text="删" Click="Delete(item.ID)"/><Button Text="选" Click="RecordNum(item.ID)"/></For>`, `
func TestProbeTwoButtons(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 7, Name: "row"}}}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()

	// 竖直 Flex 里两个按钮上下排列：第一个 y≈20，第二个 y≈60。
	r.Queue(
		pointer.Event{Position: f32.Pt(20, 20), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(20, 20), Kind: pointer.Release, Source: pointer.Mouse},
	)
	frame()
	// 点第一个（删）不应触发第二个（选）的处理器。
	if vm.LastNum == 7 {
		t.Fatalf("点「删」却触发了「选」的处理器——两个按钮共享了 Clickable")
	}

	r.Queue(
		pointer.Event{Position: f32.Pt(20, 60), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(20, 60), Kind: pointer.Release, Source: pointer.Mouse},
	)
	frame()
	if vm.LastNum != 7 {
		t.Fatalf("点「选」应收到 7, 实际 %d", vm.LastNum)
	}
	t.Log("同一行两个按钮各自独立：点「删」不触发「选」，点「选」正确派发")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("多按钮独立性验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
