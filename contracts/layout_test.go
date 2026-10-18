package contracts

import "testing"

// ---------------------------------------------------------------------------
// CrossAlign：默认值由标签决定，IR 用 AlignUnset 表示「未显式设置」
// ---------------------------------------------------------------------------

func TestParseCrossAlign(t *testing.T) {
	cases := map[string]CrossAlign{
		"Start":    AlignStart,
		"Center":   AlignCenter,
		"End":      AlignEnd,
		"Baseline": AlignBaseline,
	}
	for in, want := range cases {
		got, ok := ParseCrossAlign(in)
		if !ok || got != want {
			t.Errorf("ParseCrossAlign(%q) = %v, %v; want %v, true", in, got, ok, want)
		}
	}
}

func TestParseCrossAlignRejects(t *testing.T) {
	// 大小写敏感，且不接受非枚举值。
	for _, in := range []string{"center", "CENTRE", "Middle", "", "Stretch"} {
		if _, ok := ParseCrossAlign(in); ok {
			t.Errorf("ParseCrossAlign(%q) 应被拒绝", in)
		}
	}
}

func TestCrossAlignString(t *testing.T) {
	if got := AlignCenter.String(); got != "Center" {
		t.Errorf("AlignCenter.String() = %q, want Center", got)
	}
	// Unset 不是合法 GXML 取值，仅用于区分「未设置」。
	if got := AlignUnset.String(); got != "Unset" {
		t.Errorf("AlignUnset.String() = %q, want Unset", got)
	}
}

// ---------------------------------------------------------------------------
// MainJustify：与 Gio Spacing 的对应关系方向相反，必须逐项锁定
// ---------------------------------------------------------------------------

func TestParseMainJustify(t *testing.T) {
	cases := map[string]MainJustify{
		"Start":        JustifyStart,
		"Center":       JustifyCenter,
		"End":          JustifyEnd,
		"SpaceBetween": JustifySpaceBetween,
		"SpaceAround":  JustifySpaceAround,
		"SpaceEvenly":  JustifySpaceEvenly,
	}
	for in, want := range cases {
		got, ok := ParseMainJustify(in)
		if !ok || got != want {
			t.Errorf("ParseMainJustify(%q) = %v, %v; want %v, true", in, got, ok, want)
		}
	}
}

func TestParseMainJustifyRejects(t *testing.T) {
	for _, in := range []string{"space-between", "Between", "", "Evenly"} {
		if _, ok := ParseMainJustify(in); ok {
			t.Errorf("ParseMainJustify(%q) 应被拒绝", in)
		}
	}
}

func TestMainJustifyString(t *testing.T) {
	cases := map[MainJustify]string{
		JustifyStart:        "Start",
		JustifyCenter:       "Center",
		JustifyEnd:          "End",
		JustifySpaceBetween: "SpaceBetween",
		JustifySpaceAround:  "SpaceAround",
		JustifySpaceEvenly:  "SpaceEvenly",
		JustifyUnset:        "Unset",
	}
	for j, want := range cases {
		if got := j.String(); got != want {
			t.Errorf("MainJustify(%d).String() = %q, want %q", j, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// SizeSpec：缺省即自然尺寸，不存在 Auto 取值
// ---------------------------------------------------------------------------

func TestSizeSpecModes(t *testing.T) {
	natural := NaturalSize()
	if natural.IsSet() {
		t.Error("NaturalSize 不应被视为已设置")
	}
	if natural.Mode != SizeNatural {
		t.Errorf("NaturalSize().Mode = %v", natural.Mode)
	}

	fill := FillSize()
	if !fill.IsSet() || fill.Mode != SizeFill {
		t.Errorf("FillSize() = %+v", fill)
	}

	fixed := FixedSize(80)
	if !fixed.IsSet() || fixed.Mode != SizeFixed || fixed.Value != 80 {
		t.Errorf("FixedSize(80) = %+v", fixed)
	}
}

func TestSizeSpecZeroValueIsNatural(t *testing.T) {
	// 零值必须等价于自然尺寸，否则未写 Width 的节点会被误判为「已设置」。
	var s SizeSpec
	if s.IsSet() {
		t.Fatal("SizeSpec 零值应为自然尺寸")
	}
}

// ---------------------------------------------------------------------------
// GridColumn：只接受固定 dp、*、N*
// ---------------------------------------------------------------------------

func TestGridColumnValid(t *testing.T) {
	ok := []GridColumn{
		{Mode: GridColumnFixed, Value: 72},
		{Mode: GridColumnFixed, Value: 0},
		{Mode: GridColumnFlex, Value: 1},
		{Mode: GridColumnFlex, Value: 2.5},
	}
	for _, c := range ok {
		if !c.IsValid() {
			t.Errorf("%+v 应合法", c)
		}
	}

	bad := []GridColumn{
		{Mode: GridColumnFixed, Value: -1}, // 负宽度
		{Mode: GridColumnFlex, Value: 0},   // 权重必须为正
		{Mode: 0, Value: 1},                // 未设置模式
	}
	for _, c := range bad {
		if c.IsValid() {
			t.Errorf("%+v 应非法", c)
		}
	}
}

// ---------------------------------------------------------------------------
// NodeIR 布局字段：Gap 需要 HasGap 区分「未设置」与「设为 0」
// ---------------------------------------------------------------------------

func TestNodeIRGapPresence(t *testing.T) {
	n := &NodeIR{Kind: KindElement, Tag: "Row"}
	if n.HasGap {
		t.Fatal("未设置 Gap 时 HasGap 应为 false")
	}

	n.Gap = 0
	n.HasGap = true
	if !n.HasGap || n.Gap != 0 {
		t.Fatal("显式设为 0 时 HasGap 应为 true")
	}
}

func TestNodeIRAlignDefaultsAreUnset(t *testing.T) {
	// 轴向感知默认值由 Codegen 按 Tag 决定，IR 层保持 Unset。
	row := &NodeIR{Kind: KindElement, Tag: "Row"}
	col := &NodeIR{Kind: KindElement, Tag: "Column"}
	if row.Align != AlignUnset || col.Align != AlignUnset {
		t.Fatal("未设置 Align 时 IR 应为 AlignUnset，默认值由 Codegen 补")
	}
}

func TestNodeIRDefaultSizesAreNatural(t *testing.T) {
	n := &NodeIR{Kind: KindElement, Tag: "Text"}
	if n.Width.IsSet() || n.Height.IsSet() {
		t.Fatal("未设置宽高时应为自然尺寸")
	}
}

func TestNodeIRGridColumns(t *testing.T) {
	n := &NodeIR{
		Kind: KindElement,
		Tag:  "Grid",
		Columns: []GridColumn{
			{Mode: GridColumnFixed, Value: 72},
			{Mode: GridColumnFlex, Value: 1},
			{Mode: GridColumnFixed, Value: 96},
		},
	}
	if len(n.Columns) != 3 {
		t.Fatalf("Columns 长度 = %d", len(n.Columns))
	}
	var totalWeight float32
	for _, c := range n.Columns {
		if !c.IsValid() {
			t.Errorf("列定义非法: %+v", c)
		}
		if c.Mode == GridColumnFlex {
			totalWeight += c.Value
		}
	}
	if totalWeight != 1 {
		t.Errorf("弹性权重合计 = %v, want 1", totalWeight)
	}
}
