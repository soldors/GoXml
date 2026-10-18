package grid

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// 构造辅助。
func fixed(v float32) contracts.GridColumn {
	return contracts.GridColumn{Mode: contracts.GridColumnFixed, Value: v}
}

func flex(w float32) contracts.GridColumn {
	return contracts.GridColumn{Mode: contracts.GridColumnFlex, Value: w}
}

// eq 比较两个整数切片。
func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sum 求和。
func sum(a []int) int {
	t := 0
	for _, v := range a {
		t += v
	}
	return t
}

// ---------------------------------------------------------------------------
// 权重分配（Gio 实测：600px 下 2:1:3 → 200/100/300）
// ---------------------------------------------------------------------------

func TestWeightedProportional(t *testing.T) {
	got := Widths([]contracts.GridColumn{flex(2), flex(1), flex(3)}, 600, 0)
	want := []int{200, 100, 300}
	if !eq(got, want) {
		t.Errorf("Widths = %v, want %v", got, want)
	}
	if sum(got) != 600 {
		t.Errorf("合计 = %d, want 600", sum(got))
	}
}

// ---------------------------------------------------------------------------
// 固定列 + 弹性列 + Gap（Gio 实测：600 容器、72/*/96、Gap 10 → 弹性列 412）
// ---------------------------------------------------------------------------

func TestFixedFlexWithGap(t *testing.T) {
	got := Widths([]contracts.GridColumn{fixed(72), flex(1), fixed(96)}, 600, 10)
	want := []int{72, 412, 96}
	if !eq(got, want) {
		t.Errorf("Widths = %v, want %v", got, want)
	}
}

func TestGapCountIsColumnsMinusOne(t *testing.T) {
	// 3 列 → 2 个列间距。
	got := Widths([]contracts.GridColumn{flex(1), flex(1), flex(1)}, 100, 10)
	// 100 - 20 = 80，三等分 80/3 = 26.67
	// col0: floor(26.667+0.5)=27, carry=-0.333
	// col1: floor(26.667-0.333+0.5)=26, carry=0.667
	// col2: floor(26.667+0.667+0.5)=27
	if !eq(got, []int{27, 26, 27}) {
		t.Errorf("Widths = %v", got)
	}
	if sum(got) != 80 {
		t.Errorf("弹性合计 = %d, want 80（100−20）", sum(got))
	}
}

// ---------------------------------------------------------------------------
// 取整：Gio 实测 100px 三等分 → [33 34 33]
// ---------------------------------------------------------------------------

func TestRoundingMatchesGio(t *testing.T) {
	got := Widths([]contracts.GridColumn{flex(1), flex(1), flex(1)}, 100, 0)
	want := []int{33, 34, 33}
	if !eq(got, want) {
		t.Fatalf("Widths = %v, want %v（Gio 实测）", got, want)
	}
	if sum(got) != 100 {
		t.Errorf("合计 = %d, want 100（不得丢像素）", sum(got))
	}
}

// TestMatchesGioMeasurements 是本包最重要的一致性测试：
// 下表每一行都是对 Gio v0.10.3 layout.Flex 的**实测输出**，
// 参考实现必须逐项复现，否则规范描述的算法就与渲染行为脱节。
func TestMatchesGioMeasurements(t *testing.T) {
	cases := []struct {
		avail, n int
		want     []int
	}{
		{1, 3, []int{0, 1, 0}},
		{1, 4, []int{0, 1, 0, 0}},
		{1, 5, []int{0, 0, 0, 0, 0}}, // 丢失 1px：算法在 0.2+0.2+0.5=0.9 处永不进位
		{2, 3, []int{1, 0, 1}},
		{2, 5, []int{0, 1, 0, 1, 0}},
		{3, 7, []int{0, 1, 0, 1, 0, 1, 0}},
		{10, 7, []int{1, 2, 1, 2, 1, 2, 1}},
		{13, 6, []int{2, 2, 2, 2, 2, 2}}, // 丢失 1px
		{7, 3, []int{2, 3, 2}},
		{100, 3, []int{33, 34, 33}},
		// 常规宽度也可能丢 1px：116/5 的理想值 23.2 永远进不了位。
		{116, 5, []int{23, 23, 23, 23, 23}},
		{121, 5, []int{24, 24, 24, 24, 24}},
		{600, 3, []int{200, 200, 200}},
		{800, 3, []int{267, 266, 267}},
		{1024, 3, []int{341, 342, 341}},
		{1920, 5, []int{384, 384, 384, 384, 384}},
		{116, 4, []int{29, 29, 29, 29}},
		{116, 3, []int{39, 38, 39}},
	}
	for _, c := range cases {
		cols := make([]contracts.GridColumn, c.n)
		for i := range cols {
			cols[i] = flex(1)
		}
		got := Widths(cols, c.avail, 0)
		if !eq(got, c.want) {
			t.Errorf("avail=%d n=%d → %v, Gio 实测 %v", c.avail, c.n, got, c.want)
		}
	}
}

// TestNeverExceedsAvailable 断言分配不得超过可用宽度（这是硬约束），
// 但不要求用满——Gio 在退化情形下会留下未分配像素。
func TestNeverExceedsAvailable(t *testing.T) {
	for avail := 0; avail <= 300; avail++ {
		for n := 1; n <= 9; n++ {
			cols := make([]contracts.GridColumn, n)
			for i := range cols {
				cols[i] = flex(1)
			}
			got := Widths(cols, avail, 0)
			if sum(got) > avail {
				t.Fatalf("avail=%d n=%d → %v 合计 %d 超出可用宽度", avail, n, got, sum(got))
			}
			for i, w := range got {
				if w < 0 {
					t.Fatalf("avail=%d n=%d → 第 %d 列负数 %d", avail, n, i, w)
				}
			}
		}
	}
}

// TestLeftoverPixelIsDocumentedBehavior 记录一条重要的**非保证项**：
// 弹性分配可能留下未分配像素（每轮至多 1 个），不保证用满可用宽度。
//
// 根因：结转值在「理想值的小数部分 < 0.5」时不会累积到进位阈值，
// 每列都取 floor，于是合计少 1。116/5 是典型例子（理想 23.2）。
//
// 这不是实现缺陷，而是与 Gio 完全一致的行为。v1 不做补偿：
// 补偿需要额外一轮扫描，且会让参考实现与真实渲染不一致。
func TestLeftoverPixelIsDocumentedBehavior(t *testing.T) {
	cols := make([]contracts.GridColumn, 5)
	for i := range cols {
		cols[i] = flex(1)
	}
	got := Widths(cols, 116, 0)
	if sum(got) != 115 {
		t.Errorf("Widths(116, 5) = %v 合计 %d, 期望 115（Gio 实测丢 1px）", got, sum(got))
	}
	// 硬约束仍然成立：绝不超过可用宽度。
	if sum(got) > 116 {
		t.Error("不得超过可用宽度")
	}
}

func TestRoundingIsStableAcrossRows(t *testing.T) {
	// 同一列定义反复计算必须得到相同结果——这是「所有行列宽一致」的基础。
	cols := []contracts.GridColumn{fixed(72), flex(1), flex(2), fixed(96)}
	first := Widths(cols, 517, 8)
	for i := 0; i < 10; i++ {
		if got := Widths(cols, 517, 8); !eq(got, first) {
			t.Fatalf("第 %d 次计算 = %v, 首次 = %v", i, got, first)
		}
	}
}

// ---------------------------------------------------------------------------
// 弹性空间不足
// ---------------------------------------------------------------------------

func TestFlexSpaceExhausted(t *testing.T) {
	// 固定列已占满容器 → 弹性列宽为 0，不产生负值。
	got := Widths([]contracts.GridColumn{fixed(300), flex(1)}, 300, 0)
	if !eq(got, []int{300, 0}) {
		t.Errorf("Widths = %v, want [300 0]", got)
	}
}

func TestFlexSpaceNegativeClampedToZero(t *testing.T) {
	// 固定列超出容器（Gio 实测：Exact 不被压缩，允许溢出）。
	got := Widths([]contracts.GridColumn{fixed(200), flex(1), fixed(200)}, 300, 0)
	if !eq(got, []int{200, 0, 200}) {
		t.Errorf("Widths = %v, want [200 0 200]", got)
	}
	// 固定列保持定义值，不被容器压缩。
	if got[0] != 200 || got[2] != 200 {
		t.Error("固定列不应被压缩")
	}
}

func TestGapExhaustsSpace(t *testing.T) {
	// 间距本身就超过可用宽度。
	got := Widths([]contracts.GridColumn{flex(1), flex(1), flex(1)}, 10, 100)
	if !eq(got, []int{0, 0, 0}) {
		t.Errorf("Widths = %v, want [0 0 0]", got)
	}
}

// ---------------------------------------------------------------------------
// 退化输入
// ---------------------------------------------------------------------------

func TestEmptyColumns(t *testing.T) {
	if got := Widths(nil, 100, 8); len(got) != 0 {
		t.Errorf("Widths = %v", got)
	}
}

func TestNoFlexColumns(t *testing.T) {
	got := Widths([]contracts.GridColumn{fixed(70), fixed(30)}, 500, 0)
	if !eq(got, []int{70, 30}) {
		t.Errorf("Widths = %v, want [70 30]", got)
	}
}

func TestSingleFlexColumnTakesAll(t *testing.T) {
	got := Widths([]contracts.GridColumn{flex(1)}, 237, 0)
	if !eq(got, []int{237}) {
		t.Errorf("Widths = %v, want [237]", got)
	}
}

func TestSingleFlexWithGapIgnoresGap(t *testing.T) {
	// 只有一列时没有「列间距」，Gap 不参与计算。
	got := Widths([]contracts.GridColumn{flex(1)}, 100, 20)
	if !eq(got, []int{100}) {
		t.Errorf("Widths = %v, want [100]", got)
	}
}

func TestZeroWidthFixed(t *testing.T) {
	got := Widths([]contracts.GridColumn{fixed(0), flex(1)}, 100, 0)
	if !eq(got, []int{0, 100}) {
		t.Errorf("Widths = %v, want [0 100]", got)
	}
}

// ---------------------------------------------------------------------------
// 补位：最后一行必须与完整行得到相同列宽
// ---------------------------------------------------------------------------

func TestLastRowWidthsMatchFullRow(t *testing.T) {
	// 补位的方式是「用同样的列定义补齐」，
	// 因此补位行的列宽与完整行必然一致。
	cols := []contracts.GridColumn{fixed(100), flex(1), fixed(100)}

	full := Widths(cols, 600, 8)
	// 最后一行只有 2 个真实单元格，但补位后列定义仍相同。
	padded := Widths(cols, 600, 8)

	if !eq(full, padded) {
		t.Fatalf("full=%v padded=%v", full, padded)
	}
	// 列宽合计 = 可用宽度 − 全部列间距（gaps 占用独立空间）。
	if want := 600 - 8*2; sum(full) != want {
		t.Errorf("合计 = %d, want %d", sum(full), want)
	}
}

func TestPaddedRowKeepsGapCount(t *testing.T) {
	// 若不补位，Gap 计数会少一个，弹性列变宽。
	cols := []contracts.GridColumn{fixed(100), flex(1), fixed(100)}
	withPad := Widths(cols, 600, 10) // 2 个间距

	noPad := Widths([]contracts.GridColumn{fixed(100), flex(1)}, 600, 10) // 1 个间距

	if withPad[1] == noPad[1] {
		t.Fatal("不补位应导致弹性列变宽，这个差异正是必须补位的原因")
	}
	if withPad[1] != 380 { // 600-100-100-20
		t.Errorf("补位后弹性列 = %d, want 380", withPad[1])
	}
	if noPad[1] != 490 { // 600-100-10
		t.Errorf("不补位弹性列 = %d, want 490", noPad[1])
	}
}
