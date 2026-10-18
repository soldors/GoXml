package sizelayout

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

func natural() contracts.SizeSpec        { return contracts.NaturalSize() }
func fill() contracts.SizeSpec           { return contracts.FillSize() }
func fixed(v float32) contracts.SizeSpec { return contracts.FixedSize(v) }

// ---------------------------------------------------------------------------
// 无界判定
// ---------------------------------------------------------------------------

func TestUnboundedBoundaries(t *testing.T) {
	// 阈值前后各一像素。
	if IsUnbounded(UnboundedThreshold - 1) {
		t.Error("阈值减一不应判为无界")
	}
	if !IsUnbounded(UnboundedThreshold) {
		t.Error("阈值本身应判为无界")
	}
	if !IsUnbounded(UnboundedThreshold + 1) {
		t.Error("阈值加一应判为无界")
	}
}

func TestUnboundedDetectsMaxInt(t *testing.T) {
	// 明显的「无穷」标记必须触发。
	maxInt := int(^uint(0) >> 1)
	if !IsUnbounded(maxInt) {
		t.Error("MaxInt 应判为无界")
	}
	if !IsUnbounded(maxInt - 1) {
		t.Error("MaxInt-1 应判为无界")
	}
}

func TestUnboundedDoesNotMisjudgeRealSizes(t *testing.T) {
	// 真实设备的单边像素数永不应被判为无界。
	real := []int{
		320, 480, 640, 720, 768, 1080, 1280, 1440, 1600,
		1920, 2160, 2560, 2880, 3200, 3840, 5120, 6016, 7680,
	}
	for _, w := range real {
		if IsUnbounded(w) {
			t.Errorf("%d 是真实尺寸，不应判为无界", w)
		}
	}
}

func TestUnboundedDoesNotMisjudgeLargeFixedSizes(t *testing.T) {
	// 合法的超大固定尺寸（例如长文档页面高度）不应被误判为无界。
	// 注意：固定尺寸本身不参与无界判定，这里验证的是「即使把一个大值
	// 当作可用上限传入，只要未达阈值，Fill 仍应正常填充」。
	for _, h := range []int{10000, 50000, 100000, 500000, UnboundedThreshold - 1} {
		if IsUnbounded(h) {
			t.Errorf("可用高度 %d 未达阈值，不应判为无界", h)
		}
		if got := Resolve(fill(), AxisCross, h); got != DirectiveFillCross {
			t.Errorf("可用高度 %d 下交叉轴 Fill = %v, want FillCross", h, got)
		}
	}
}

func TestLargeFixedSizeAlwaysExact(t *testing.T) {
	// 固定尺寸与可用空间无关：即使可用空间无界，固定值仍精确。
	for _, v := range []float32{80, 5000, 500000, 2 << 20} {
		if got := Resolve(fixed(v), AxisCross, UnboundedThreshold); got != DirectiveExact {
			t.Errorf("固定 %v 在无界下 = %v, want Exact", v, got)
		}
	}
}

func TestUnboundedBoundaryIsInclusive(t *testing.T) {
	// 明确记录阈值判定的开闭区间，避免后续调整时语义漂移。
	if IsUnbounded(UnboundedThreshold-1) == IsUnbounded(UnboundedThreshold) {
		t.Error("阈值处必须发生状态切换")
	}
}

// ---------------------------------------------------------------------------
// 主轴
// ---------------------------------------------------------------------------

func TestMainAxisFillIsFlex(t *testing.T) {
	// 主轴 Fill 参与弹性分配，有界无界都一样。
	if got := Resolve(fill(), AxisMain, 600); got != DirectiveFlex {
		t.Errorf("主轴 Fill（有界）= %v, want Flex", got)
	}
	if got := Resolve(fill(), AxisMain, UnboundedThreshold); got != DirectiveFlex {
		t.Errorf("主轴 Fill（无界）= %v, want Flex", got)
	}
}

func TestMainAxisNatural(t *testing.T) {
	if got := Resolve(natural(), AxisMain, 600); got != DirectiveNatural {
		t.Errorf("主轴自然 = %v, want Natural", got)
	}
}

func TestMainAxisFixed(t *testing.T) {
	if got := Resolve(fixed(80), AxisMain, 600); got != DirectiveExact {
		t.Errorf("主轴固定 = %v, want Exact", got)
	}
}

// ---------------------------------------------------------------------------
// 交叉轴
// ---------------------------------------------------------------------------

func TestCrossAxisFillBounded(t *testing.T) {
	if got := Resolve(fill(), AxisCross, 100); got != DirectiveFillCross {
		t.Errorf("交叉轴 Fill（有界）= %v, want FillCross", got)
	}
}

func TestCrossAxisFillUnboundedDegrades(t *testing.T) {
	// 规范 §3.1.7：无界时退化，避免产生上亿像素的布局。
	if got := Resolve(fill(), AxisCross, UnboundedThreshold); got != DirectiveNatural {
		t.Errorf("交叉轴 Fill（无界）= %v, want Natural", got)
	}
}

func TestCrossAxisFixedIgnoresUnbounded(t *testing.T) {
	// 固定值不依赖可用空间，无界时仍精确。
	if got := Resolve(fixed(40), AxisCross, UnboundedThreshold); got != DirectiveExact {
		t.Errorf("交叉轴固定（无界）= %v, want Exact", got)
	}
}

func TestCrossAxisNatural(t *testing.T) {
	if got := Resolve(natural(), AxisCross, 100); got != DirectiveNatural {
		t.Errorf("交叉轴自然 = %v, want Natural", got)
	}
}

// ---------------------------------------------------------------------------
// 完整组合矩阵（规范 §3.1.7 最后一张表）
// ---------------------------------------------------------------------------

func TestCombinationMatrixRow(t *testing.T) {
	// 父容器为 Row（主轴=水平），交叉轴有界。
	cases := []struct {
		w, h      contracts.SizeSpec
		wantMain  Directive
		wantCross Directive
		name      string
	}{
		{natural(), natural(), DirectiveNatural, DirectiveNatural, "自然/自然"},
		{natural(), fill(), DirectiveNatural, DirectiveFillCross, "自然/Fill"},
		{natural(), fixed(40), DirectiveNatural, DirectiveExact, "自然/固定"},
		{fill(), natural(), DirectiveFlex, DirectiveNatural, "Fill/自然"},
		{fill(), fill(), DirectiveFlex, DirectiveFillCross, "Fill/Fill"},
		{fill(), fixed(40), DirectiveFlex, DirectiveExact, "Fill/固定"},
		{fixed(80), natural(), DirectiveExact, DirectiveNatural, "固定/自然"},
		{fixed(80), fill(), DirectiveExact, DirectiveFillCross, "固定/Fill"},
		{fixed(80), fixed(40), DirectiveExact, DirectiveExact, "固定/固定"},
	}
	for _, c := range cases {
		main, cross := ResolveMainCross(c.w, c.h, true, 600, 200)
		if main != c.wantMain || cross != c.wantCross {
			t.Errorf("%s: (main,cross) = (%v,%v), want (%v,%v)",
				c.name, main, cross, c.wantMain, c.wantCross)
		}
	}
}

func TestCombinationMatrixColumnSwapsAxes(t *testing.T) {
	// 父容器为 Column 时主轴是垂直：Width 变为交叉轴、Height 变为主轴。
	// Fill/自然 在 Row 中是 (Flex, Natural)；在 Column 中应为 (Natural, Flex)。
	main, cross := ResolveMainCross(fill(), natural(), false, 600, 200)
	if main != DirectiveNatural {
		t.Errorf("Column 内 main = %v, want Natural（Height=自然）", main)
	}
	if cross != DirectiveFillCross {
		t.Errorf("Column 内 cross = %v, want FillCross（Width=Fill）", cross)
	}
}

// ---------------------------------------------------------------------------
// 无界矩阵
// ---------------------------------------------------------------------------

func TestUnboundedMatrix(t *testing.T) {
	// 交叉轴无界时，Fill 退化；固定与自然不受影响。
	const ub = UnboundedThreshold
	cases := []struct {
		spec contracts.SizeSpec
		want Directive
	}{
		{natural(), DirectiveNatural},
		{fill(), DirectiveNatural}, // 退化
		{fixed(40), DirectiveExact},
	}
	for _, c := range cases {
		if got := Resolve(c.spec, AxisCross, ub); got != c.want {
			t.Errorf("%+v 交叉轴无界 = %v, want %v", c.spec, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 穷尽性：任意输入都必须有确定结果
// ---------------------------------------------------------------------------

func TestTotal(t *testing.T) {
	specs := []contracts.SizeSpec{natural(), fill(), fixed(0), fixed(80.5)}
	axes := []Axis{AxisMain, AxisCross}
	maxes := []int{0, 1, 600, UnboundedThreshold - 1, UnboundedThreshold}
	valid := map[Directive]bool{
		DirectiveNatural: true, DirectiveFlex: true,
		DirectiveFillCross: true, DirectiveExact: true,
	}
	for _, s := range specs {
		for _, a := range axes {
			for _, m := range maxes {
				got := Resolve(s, a, m)
				if !valid[got] {
					t.Errorf("Resolve(%+v,%v,%d) 返回未知指令 %v", s, a, m, got)
				}
			}
		}
	}
}

func TestDirectiveString(t *testing.T) {
	cases := map[Directive]string{
		DirectiveNatural: "Natural", DirectiveFlex: "Flex",
		DirectiveFillCross: "FillCross", DirectiveExact: "Exact",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Errorf("Directive(%d).String() = %q, want %q", d, got, want)
		}
	}
}
