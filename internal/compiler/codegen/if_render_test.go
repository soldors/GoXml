package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// <If> 条件渲染（If 输出契约）
//
// 证据分层：
//   - 结构：内联展开、条件变量只求值一次、轮询守卫
//   - 行为：真→Then、假→Else；无 Else 时零高；分支按钮仅条件成立时可点
//   - 组合：If 在 For 内按行判定
// ============================================================================

// TestIfInlineExpansion 内联展开：分支内容直接进父 children，无嵌套 Flex。
func TestIfInlineExpansion(t *testing.T) {
	v := verified(t, `<If Condition="{Dirty}"><Then><Text Value="A"/></Then><Else><Text Value="B"/></Else></If>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if got := strings.Count(s, "layout.Flex{"); got != 1 {
		t.Errorf("<If> 不得引入嵌套容器，实际 %d 个 Flex:\n%s", got, s)
	}
	if !strings.Contains(s, "cond := v.vm.Dirty") {
		t.Errorf("条件应求值一次存入 cond:\n%s", s)
	}
	if !strings.Contains(s, "children = append(children, layout.Rigid(") {
		t.Errorf("分支内容应 append 到父 children:\n%s", s)
	}
}

// TestIfPollingGuard 分支内按钮的顶部轮询必须带条件守卫（契约 §2.2）。
func TestIfPollingGuard(t *testing.T) {
	v := verified(t, `<If Condition="{Dirty}"><Then><Button Text="保存" Click="Increment"/></Then><Else><Button Text="重置" Click="Reset"/></Else></If>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Then 分支按钮：守卫为条件本身
	if !strings.Contains(s, "if v.vm.Dirty {\n\t\tfor v.click0.Clicked(gtx)") {
		t.Errorf("Then 分支按钮的轮询应被条件守卫:\n%s", s)
	}
	// Else 分支按钮：守卫为条件否定
	if !strings.Contains(s, "if !(v.vm.Dirty) {") {
		t.Errorf("Else 分支按钮的轮询应以条件否定守卫:\n%s", s)
	}
}

// TestIfPageNotFrozen 含 <If> 的页面不得整页固化（契约 §3）。
func TestIfPageNotFrozen(t *testing.T) {
	v := verified(t, `<If Condition="{Dirty}"><Then><Text Value="A"/></Then></If>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"v.cacheCall.Add", "op.Record(&v.cacheOps)"} {
		if strings.Contains(s, bad) {
			t.Errorf("含 <If> 的页面不得整页固化，出现了 %q:\n%s", bad, s)
		}
	}
}

// TestIfRendersThenOrElse 行为：真→Then 内容、假→Else 内容（用宽度区分）。
func TestIfRendersThenOrElse(t *testing.T) {
	out := rtProbe(t, `<If Condition="{Dirty}"><Then><Text Value="AAAAAAAAAAAA"/></Then><Else><Text Value="B"/></Else></If>`, `
func TestProbeIfBranch(t *testing.T) {
	th := material.NewTheme()

	width := func(dirty bool) int {
		vm := &fixture.ViewModel{Dirty: dirty}
		v, err := NewGoxmlDashboardView(vm)
		if err != nil {
			t.Fatal(err)
		}
		dims := v.Layout(layout.Context{
			Ops:         new(op.Ops),
			Constraints: layout.Constraints{Max: image.Pt(400, 500)},
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		}, th)
		return dims.Size.X
	}

	wide := width(true)   // Then：长文本
	narrow := width(false) // Else：短文本
	if wide <= narrow {
		t.Fatalf("Then 分支宽度 %d 应大于 Else 宽度 %d——分支未按条件切换", wide, narrow)
	}
	t.Logf("条件下渲染正确: Then=%d, Else=%d", wide, narrow)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("分支渲染验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestIfNoElseRendersNothing 无 <Else> 时假分支不产生节点（高度为 0）。
func TestIfNoElseRendersNothing(t *testing.T) {
	out := rtProbe(t, `<If Condition="{Dirty}"><Then><Text Value="A"/></Then></If>`, `
func TestProbeIfNoElse(t *testing.T) {
	th := material.NewTheme()

	height := func(dirty bool) int {
		vm := &fixture.ViewModel{Dirty: dirty}
		v, err := NewGoxmlDashboardView(vm)
		if err != nil {
			t.Fatal(err)
		}
		dims := v.Layout(layout.Context{
			Ops:         new(op.Ops),
			Constraints: layout.Constraints{Max: image.Pt(400, 500)},
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		}, th)
		return dims.Size.Y
	}

	if h := height(true); h <= 0 {
		t.Fatalf("条件为真应渲染内容，实际高度 %d", h)
	}
	if h := height(false); h != 0 {
		t.Fatalf("无 Else 且条件为假应渲染零高，实际 %d", h)
	}
	t.Log("无 Else 时假分支不产生节点（零高）")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("无 Else 验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestIfButtonOnlyClickableWhenTrue 核心验收（契约 §8 第 5 项）：
// 分支内按钮仅在条件为真时可点击。
func TestIfButtonOnlyClickableWhenTrue(t *testing.T) {
	out := rtProbe(t, `<If Condition="{Dirty}"><Then><Button Text="保存" Click="Increment"/></Then></If>`, `
func TestProbeIfButtonGuard(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Dirty: false}
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
	click := func() {
		r.Queue(
			pointer.Event{Position: f32.Pt(20, 20), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(20, 20), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}

	// 条件为假：按钮未渲染，点击不得触发处理器。
	frame()
	click()
	if vm.Count != 0 {
		t.Fatalf("条件为假时点击不应触发，Count=%d", vm.Count)
	}

	// 条件为真：按钮渲染，点击应触发。
	vm.Dirty = true
	frame()
	click()
	if vm.Count != 1 {
		t.Fatalf("条件为真时点击应触发一次，Count=%d", vm.Count)
	}
	t.Log("分支按钮仅在条件为真时可点击（轮询守卫与渲染同源）")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("条件守卫验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestIfInsideFor 组合：If 在 For 内按行判定（契约 §4.1）。
func TestIfInsideFor(t *testing.T) {
	v := verified(t, `<For Each="item" In="{Items}" Key="{item.ID}"><If Condition="{item.Done}"><Then><Text Value="有名字"/></Then><Else><Text Value="无名"/></Else></If></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// 渲染期条件用 item，轮询期若含按钮则用 rowItem；这里无按钮，只验渲染。
	if !strings.Contains(s, "cond := item.Done") {
		t.Errorf("For 内的 If 条件应用循环变量渲染:\n%s", s)
	}
}

// TestIfInsideForWithButton 组合核心：For 内 If 分支里的按钮，
// 轮询守卫必须把 item 改写成 rowItem（契约 §4.1）。
func TestIfInsideForWithButtonGuard(t *testing.T) {
	v := verified(t, `<For Each="item" In="{Items}" Key="{item.ID}"><If Condition="{item.Done}"><Then><Button Text="撤销" Click="RecordNum(item.ID)"/></Then></If></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "if rowItem.Done {") {
		t.Errorf("行内分支按钮的轮询守卫应改写为 rowItem.Done:\n%s", s)
	}
	if strings.Contains(s, "if item.Done {") {
		t.Errorf("轮询作用域里不得出现渲染期变量 item:\n%s", s)
	}
}

// TestIfInsideForButtonRuntime 运行时：Done=false 的行不渲染按钮。
//
// 判据是**按钮数量**而不是固定坐标：Done=false 的行没有按钮，
// 后续行的按钮会顶上来——所以「点顶部命中谁」能证明前面那行是否渲染了。
func TestIfInsideForButtonRuntime(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}" Key="{item.ID}"><If Condition="{item.Done}"><Then><Button Text="撤销" Click="RecordNum(item.ID)"/></Then></If></For>`, `
func TestProbeIfInFor(t *testing.T) {
	th := material.NewTheme()

	clickTop := func(done []bool) int {
		items := make([]fixture.Item, len(done))
		for i, d := range done {
			items[i] = fixture.Item{ID: 11 + i*11, Name: "r", Done: d}
		}
		vm := &fixture.ViewModel{Items: items}
		v, err := NewGoxmlDashboardView(vm)
		if err != nil {
			t.Fatal(err)
		}
		var r input.Router
		gtx := layout.Context{
			Ops:         new(op.Ops),
			Constraints: layout.Exact(image.Pt(300, 300)),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Source:      r.Source(),
		}
		frame := func() {
			gtx.Reset()
			v.Layout(gtx, th)
			r.Frame(gtx.Ops)
		}
		frame()
		r.Queue(
			pointer.Event{Position: f32.Pt(20, 20), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(20, 20), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
		return vm.LastNum
	}

	// 两行都 Done：顶部按钮属于第 1 行（ID=11）。
	if got := clickTop([]bool{true, true}); got != 11 {
		t.Fatalf("两行都 Done 时点顶部应命中 ID=11, 实际 %d", got)
	}
	// 第 1 行 Done=false：它没有按钮，顶部按钮属于第 2 行（ID=22）。
	if got := clickTop([]bool{false, true}); got != 22 {
		t.Fatalf("第 1 行 Done=false 时点顶部应命中 ID=22（该行未渲染按钮）, 实际 %d", got)
	}
	t.Log("Done=false 的行确实未渲染按钮：顶部按钮的身份随条件变化（11 → 22）")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("行内条件按钮验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
