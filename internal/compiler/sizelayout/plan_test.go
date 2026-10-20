package sizelayout

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// PlanFor 的契约测试
//
// PlanFor 是「编译期视图」：不读运行时约束，只给出「运行时需要做什么」。
// 这些测试锁住三条容易被悄悄破坏的性质：
//   1. 传 0 给 Resolve 确实代表「有界」，不是碰巧；
//   2. Plan 的语义与 Resolve 在有界输入下逐项相等（不复制判断分支）；
//   3. 交叉轴的维与阈值都来自本包，不是调用方的知识。
// ============================================================================

// TestZeroIsBounded 锁住 PlanFor 的前提：0 是「有界」的代表值。
//
// 若将来 UnboundedThreshold 调整成 0 或负数，这条会失败——那正是需要的信号：
// PlanFor 传 0 的假设不再成立，必须改实现，而不是让它静默改变决策。
func TestZeroIsBounded(t *testing.T) {
	if IsUnbounded(0) {
		t.Fatal("0 必须被判为有界：PlanFor 以 0 作为「假定有界」的输入")
	}
	// 负值不可能出现在 Gio 约束里，但同样不应被判为无界。
	if IsUnbounded(-1) {
		t.Error("负值不应被判为无界")
	}
}

// TestPlanForMatchesResolveWhenBounded 是核心一致性断言：
// Plan 的 Main/Cross 必须与 Resolve 在「有界」输入下的结果逐项相等。
//
// 这条测试的作用不是「证明当前实现正确」，而是**防止 PlanFor 偷偷长出第二套判断**：
// 一旦有人在 PlanFor 里加 if/switch 改写语义，这里立刻不等。
func TestPlanForMatchesResolveWhenBounded(t *testing.T) {
	specs := []struct {
		name string
		spec contracts.SizeSpec
	}{
		{"natural", natural()},
		{"fill", fill()},
		{"fixed", fixed(80)},
	}

	for _, dir := range []struct {
		name             string
		mainIsHorizontal bool
	}{
		{"Row(主轴X)", true},
		{"Column(主轴Y)", false},
	} {
		for _, ws := range specs {
			for _, hs := range specs {
				t.Run(dir.name+"/W="+ws.name+"/H="+hs.name, func(t *testing.T) {
					p := PlanFor(ws.spec, hs.spec, dir.mainIsHorizontal)

					var wantMain, wantCross Directive
					if dir.mainIsHorizontal {
						wantMain = Resolve(ws.spec, AxisMain, 0)
						wantCross = Resolve(hs.spec, AxisCross, 0)
					} else {
						wantMain = Resolve(hs.spec, AxisMain, 0)
						wantCross = Resolve(ws.spec, AxisCross, 0)
					}

					if p.Main != wantMain {
						t.Errorf("Main = %v, want %v（必须与 Resolve 一致）", p.Main, wantMain)
					}
					if p.Cross != wantCross {
						t.Errorf("Cross = %v, want %v（必须与 Resolve 一致）", p.Cross, wantCross)
					}
				})
			}
		}
	}
}

// TestPlanForAxes 两个方向的具体维必须由 Plan 给出，且与容器方向一致。
//
// 依据实测（dev-log §8.1）：Horizontal 容器的交叉轴是 Y，Vertical 是 X。
// 生成代码若自己推导轴向，就等于把「无界识别」拆成了两处定义——
// 所以这里断言 Plan 无论是否 FillCross 都给出两个轴。
func TestPlanForAxes(t *testing.T) {
	// Row：主轴 X、交叉轴 Y（与是否设置尺寸无关）
	for _, tc := range []struct {
		name string
		w, h contracts.SizeSpec
	}{
		{"都未设置", natural(), natural()},
		{"交叉轴 Fill", natural(), fill()},
		{"主轴 Fill", fill(), natural()},
	} {
		p := PlanFor(tc.w, tc.h, true)
		if p.MainAxis != AxisX || p.CrossAxis != AxisY {
			t.Errorf("Row/%s: 主轴应 X、交叉轴应 Y，实际 %v/%v",
				tc.name, p.MainAxis, p.CrossAxis)
		}
	}

	// Column：主轴 Y、交叉轴 X
	for _, tc := range []struct {
		name string
		w, h contracts.SizeSpec
	}{
		{"都未设置", natural(), natural()},
		{"交叉轴 Fill", fill(), natural()},
		{"主轴 Fill", natural(), fill()},
	} {
		p := PlanFor(tc.w, tc.h, false)
		if p.MainAxis != AxisY || p.CrossAxis != AxisX {
			t.Errorf("Column/%s: 主轴应 Y、交叉轴应 X，实际 %v/%v",
				tc.name, p.MainAxis, p.CrossAxis)
		}
	}
}

// TestBoundedCheckComplementsIsUnbounded 锁住生成模板与 IsUnbounded 的互补关系。
//
// 生成器输出的是「有界」判据 `Max < Threshold`，而 IsUnbounded 输出的是
// 「无界」判据 `Max >= Threshold`。两者必须互为否定，否则生成代码会在边界上
// 与 sizelayout 的语义分叉——这正是「内联展开」允许成立的前提。
func TestBoundedCheckComplementsIsUnbounded(t *testing.T) {
	p := PlanFor(natural(), fill(), true)
	if p.Fill == nil {
		t.Fatal("Row/Height=Fill 应产生 CrossFillPlan")
	}
	th := p.Fill.Threshold

	for _, max := range []int{0, 200, th - 2, th - 1, th, th + 1, th * 2} {
		// 生成代码用的判据：max < Threshold 即「有界，可以抬 Min」
		codeTreatsAsBounded := max < th
		// sizelayout 的判据
		wantBounded := !IsUnbounded(max)
		if codeTreatsAsBounded != wantBounded {
			t.Errorf("max=%d: 生成判据(有界=%v) 与 IsUnbounded(无界=%v) 不互补",
				max, codeTreatsAsBounded, IsUnbounded(max))
		}
	}
}

// TestPlanForFillPointerOnlyForFillCross 用 nil/非 nil 表达「是否需要运行时检查」，
// 不用零值字段——否则调用方无法区分「不需要检查」与「阈值为 0」。
func TestPlanForFillPointerOnlyForFillCross(t *testing.T) {
	cases := []struct {
		name             string
		w, h             contracts.SizeSpec
		mainIsHorizontal bool
		wantCross        Directive
		wantFill         bool
	}{
		// 交叉轴 Fill：需要运行时检查
		{"Row/Height=Fill", natural(), fill(), true, DirectiveFillCross, true},
		{"Column/Width=Fill", fill(), natural(), false, DirectiveFillCross, true},
		// 主轴 Fill：Flexed，不需要运行时检查
		{"Row/Width=Fill", fill(), natural(), true, DirectiveNatural, false},
		{"Column/Height=Fill", natural(), fill(), false, DirectiveNatural, false},
		// 自然与固定：都不需要
		{"自然", natural(), natural(), true, DirectiveNatural, false},
		{"固定", fixed(80), fixed(32), true, DirectiveExact, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := PlanFor(tc.w, tc.h, tc.mainIsHorizontal)
			if p.Cross != tc.wantCross {
				t.Errorf("Cross = %v, want %v", p.Cross, tc.wantCross)
			}
			if got := p.Fill != nil; got != tc.wantFill {
				t.Errorf("Fill 非 nil = %v, want %v（%+v）", got, tc.wantFill, p.Fill)
			}
			// 不变式：非 nil 当且仅当 Cross == FillCross
			if (p.Fill != nil) != (p.Cross == DirectiveFillCross) {
				t.Errorf("不变式破坏：Cross=%v 但 Fill=%+v", p.Cross, p.Fill)
			}
		})
	}
}

// TestPlanForThresholdIsSingleSource 阈值必须原样来自 UnboundedThreshold。
//
// 生成器要把它写成字面量，因此这里是那条字面量的唯一来源。
func TestPlanForThresholdIsSingleSource(t *testing.T) {
	p := PlanFor(natural(), fill(), true)
	if p.Fill == nil {
		t.Fatal("Row/Height=Fill 应产生 CrossFillPlan")
	}
	if p.Fill.Threshold != UnboundedThreshold {
		t.Errorf("Threshold = %d, want %d（必须等于 UnboundedThreshold）",
			p.Fill.Threshold, UnboundedThreshold)
	}
}

// TestPlanForIsDeterministic 同一输入恒得同一计划（编译期纯度）。
func TestPlanForIsDeterministic(t *testing.T) {
	a := PlanFor(fixed(120), fill(), false)
	b := PlanFor(fixed(120), fill(), false)
	if a.Main != b.Main || a.Cross != b.Cross {
		t.Fatalf("同一输入两次结果不同: %+v vs %+v", a, b)
	}
	if (a.Fill == nil) != (b.Fill == nil) {
		t.Fatalf("Fill 存在性不一致: %+v vs %+v", a.Fill, b.Fill)
	}
	if a.Fill != nil && *a.Fill != *b.Fill {
		t.Fatalf("Fill 内容不一致: %+v vs %+v", *a.Fill, *b.Fill)
	}
}
