package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// 步骤 1：纯展示 <For> 的内联展开（<For> 列表契约 §2/§6）
//
// 证据分层（互不替代）：
//   - 结构：生成的子项 append 位于父容器 children 列表内，循环体里没有 Flex
//   - 行为：真实离屏渲染，用**尺寸**证明迭代次数、用**像素**证明取值
// ============================================================================

// TestForInlineExpansion 内联展开：循环体内不得出现新的 Flex 容器。
//
// 若 For 生成嵌套 Flex，交叉轴对齐会因「Rigid 子项宽度=内容宽」而偏移
// （契约 §2.1）。
func TestForInlineExpansion(t *testing.T) {
	v := verified(t, `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)

	// 页面根 Flex 一个。
	if got := strings.Count(s, "layout.Flex{"); got != 1 {
		t.Errorf("应只有一个 Flex（页面根），实际 %d 个——<For> 不得引入嵌套容器", got)
	}
	// 循环体内必须是 append 到父容器的 children。
	if !strings.Contains(s, "items := v.vm.Items") {
		t.Errorf("数据源应求值一次并存入 items:\n%s", s)
	}
	if !strings.Contains(s, "children = append(children, layout.Rigid(") {
		t.Errorf("循环体应 append 到父容器 children:\n%s", s)
	}
	if !strings.Contains(s, "Layout(gtx, children...)") {
		t.Errorf("父容器应以 children... 布局:\n%s", s)
	}
}

// TestForNoSlotCacheInsideLoop 循环体内不得分配显示缓存槽位（契约 §6.2）。
func TestForNoSlotCacheInsideLoop(t *testing.T) {
	v := verified(t, `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "bindText") {
		t.Errorf("循环体内不应使用槽位缓存（每行复用会互相覆写）:\n%s", s)
	}
	if !strings.Contains(s, "material.Body1(th, item.Name)") {
		t.Errorf("循环体内应直接布局文本:\n%s", s)
	}
}

// TestForIndexVariable Index 只在被引用时才生成循环变量。
//
// Go 对 `for i, item := range` 中未使用的 i 会报 "declared and not used"，
// 而 Index 是可选的——声明了却不引用是合法写法。
func TestForIndexVariable(t *testing.T) {
	v := verified(t, `<For Each="item" In="{Items}" Index="i"><Text Value="{item.Name}"/></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "for i, item :=") {
		t.Errorf("Index 未被引用时不应生成 i（会编译失败）:\n%s", s)
	}
	if !strings.Contains(s, "for _, item := range items {") {
		t.Errorf("应生成 for _, item:\n%s", s)
	}
}

// TestForLoopVarCollision 循环变量与生成代码标识符冲突必须显式报错（契约 §6.1）。
func TestForLoopVarCollision(t *testing.T) {
	for _, name := range []string{"gtx", "v", "th", "items", "children"} {
		t.Run(name, func(t *testing.T) {
			v := verified(t, `<For Each="`+name+`" In="{Items}"><Text Value="{`+name+`.Name}"/></For>`)
			if _, err := Generate(v, bindingOpts()); err == nil {
				t.Errorf("Each=%q 与生成代码标识符冲突，应报错", name)
			}
		})
	}
}

// TestForStaticPageNotFrozen 含 <For> 的页面必须动态（不得整页固化）。
func TestForStaticPageNotFrozen(t *testing.T) {
	// 循环体完全是静态文本，但行数来自数据 → 整页仍必须动态。
	v := verified(t, `<For Each="item" In="{Items}"><Text Value="row"/></For>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"v.cacheCall.Add", "op.Record(&v.cacheOps)"} {
		if strings.Contains(s, bad) {
			t.Errorf("含 <For> 的页面不得整页固化，但出现了 %q:\n%s", bad, s)
		}
	}
}

// TestForEmptyAndNilSlice 空与 nil 切片渲染为空、不 panic（契约 §1）。
func TestForEmptyAndNilSlice(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`, `
func TestProbeForEmpty(t *testing.T) {
	th := material.NewTheme()

	// nil 切片
	vm := &fixture.ViewModel{Items: nil}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	dims := v.Layout(layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Pt(400, 1000)},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}, th)
	if dims.Size.Y != 0 {
		t.Fatalf("nil 切片应渲染为零高，实际高度 %d", dims.Size.Y)
	}

	// 空切片
	vm2 := &fixture.ViewModel{Items: []fixture.Item{}}
	v2, err := NewGoxmlDashboardView(vm2)
	if err != nil {
		t.Fatal(err)
	}
	dims2 := v2.Layout(layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Pt(400, 1000)},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}, th)
	if dims2.Size.Y != 0 {
		t.Fatalf("空切片应渲染为零高，实际高度 %d", dims2.Size.Y)
	}
	t.Log("nil 与空切片均渲染为零高且不 panic")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("空/nil 切片渲染失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestForRendersEveryItem 用尺寸证明迭代次数正确（契约 §9 第 3/4 项）。
func TestForRendersEveryItem(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`, `
func TestProbeForCount(t *testing.T) {
	th := material.NewTheme()

	height := func(n int) int {
		items := make([]fixture.Item, n)
		for i := range items {
			items[i] = fixture.Item{ID: i, Name: "row"}
		}
		vm := &fixture.ViewModel{Items: items}
		v, err := NewGoxmlDashboardView(vm)
		if err != nil {
			t.Fatal(err)
		}
		dims := v.Layout(layout.Context{
			Ops:         new(op.Ops),
			Constraints: layout.Constraints{Max: image.Pt(400, 1000)},
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		}, th)
		return dims.Size.Y
	}

	h0, h1, h3 := height(0), height(1), height(3)
	if h0 != 0 {
		t.Fatalf("0 项高度应为 0，实际 %d", h0)
	}
	if h1 <= 0 {
		t.Fatalf("1 项高度应大于 0，实际 %d", h1)
	}
	if h3 <= h1 {
		t.Fatalf("3 项高度 %d 应大于 1 项高度 %d——迭代次数不对", h3, h1)
	}
	t.Logf("高度随项数增长: 0=%d 1=%d 3=%d（证明逐项迭代）", h0, h1, h3)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("行数渲染失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestForBindingReadsCorrectValue 证明每行取到了**自己的**值（契约 §9 第 4 项）。
//
// 观测手段的选择：不用像素相等——离屏窗口的字形图集状态会让同一份输入的
// 两次截图存在差异（实测不稳定），那样的断言是脆弱证据。
// 改用**布局尺寸**：竖直 Flex 的交叉轴尺寸 = 最宽子项，
// 而文本宽度随内容变化，因此宽度能稳定反映「元素值真的进了布局」。
func TestForBindingReadsCorrectValue(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`, `
func TestProbeForValues(t *testing.T) {
	th := material.NewTheme()

	width := func(names []string) int {
		items := make([]fixture.Item, len(names))
		for i, n := range names {
			items[i] = fixture.Item{ID: i, Name: n}
		}
		vm := &fixture.ViewModel{Items: items}
		v, err := NewGoxmlDashboardView(vm)
		if err != nil {
			t.Fatal(err)
		}
		dims := v.Layout(layout.Context{
			Ops:         new(op.Ops),
			Constraints: layout.Constraints{Max: image.Pt(400, 1000)},
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		}, th)
		return dims.Size.X
	}

	short := width([]string{"A"})
	long := width([]string{"AAAAAAAAAAAA"})
	again := width([]string{"A"})

	if short <= 0 {
		t.Fatalf("单项文本宽度应大于 0，实际 %d", short)
	}
	if long <= short {
		t.Fatalf("长文本宽度 %d 应大于短文本宽度 %d——元素值未进入布局", long, short)
	}
	if again != short {
		t.Errorf("同一输入两次布局宽度应一致: %d vs %d", again, short)
	}
	t.Logf("宽度随内容变化: 1 字符=%d, 12 字符=%d（逐行取值生效）", short, long)
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("像素验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestForIndexCompiles 带 Index 的生成代码必须可编译（`i` 在循环体内未使用）。
func TestForIndexCompiles(t *testing.T) {
	out := rtProbe(t, `<For Each="item" In="{Items}" Index="i"><Text Value="{item.Name}"/></For>`, `
func TestProbeForIndex(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	dims := v.Layout(layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Pt(400, 1000)},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}, th)
	if dims.Size.Y <= 0 {
		t.Fatalf("带 Index 的循环应渲染出内容，实际高度 %d", dims.Size.Y)
	}
	t.Log("带 Index 的生成代码可编译并正常渲染")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("Index 编译验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
