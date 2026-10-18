package parser

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// Grid 语义（规范 §3.1.6）
// ---------------------------------------------------------------------------

// gridSrc 构造一个 Grid 文档。
func gridSrc(cols, body string) string {
	if cols == "" {
		return wrapSrc(`<Grid>` + body + `</Grid>`)
	}
	return wrapSrc(`<Grid Columns="` + cols + `">` + body + `</Grid>`)
}

// --- 排列 ---------------------------------------------------------------

func TestGridRowMajor(t *testing.T) {
	// 子节点按声明顺序保留，排列由 Codegen 按行优先切分。
	doc := mustParse(t, gridSrc("72 * 96", `
		<Text Value="a"/><Input Value="{X}"/><Button Text="b"/>
		<Text Value="c"/>`))

	g := doc.Page.Children[0]
	if g.Tag != "Grid" {
		t.Fatalf("标签 = %q", g.Tag)
	}
	if len(g.Children) != 4 {
		t.Fatalf("子节点数 = %d, 期望 4（最后一行不完整也应保留原样）", len(g.Children))
	}
	// 顺序必须与声明一致。Parser 阶段属性仍在 RawAttrs。
	if v, ok := g.Children[0].RawAttrs.Lookup("Value"); !ok || v.Value.Raw != "a" {
		t.Errorf("[0] = %+v", v.Value)
	}
	if v, ok := g.Children[3].RawAttrs.Lookup("Value"); !ok || v.Value.Raw != "c" {
		t.Errorf("[3] = %+v", v.Value)
	}
}

func TestGridChildrenNotPaddedInIR(t *testing.T) {
	// 补位只发生在 Codegen 的布局计算中，不得回写进 IR。
	doc := mustParse(t, gridSrc("100 * 100", `<Text Value="a"/><Text Value="b"/>`))
	g := doc.Page.Children[0]
	if len(g.Children) != 2 {
		t.Errorf("IR 中子节点数应为 2（不得补成 3），实际 %d", len(g.Children))
	}
}

// --- 列定义 -------------------------------------------------------------

func TestGridColumnDefinitions(t *testing.T) {
	doc := mustParse(t, gridSrc("72 * 96", `<Text Value="a"/>`))
	cols := doc.Page.Children[0].Columns
	if len(cols) != 3 {
		t.Fatalf("列数 = %d", len(cols))
	}
	if cols[0].Mode != contracts.GridColumnFixed || cols[0].Value != 72 {
		t.Errorf("col0 = %+v", cols[0])
	}
	if cols[1].Mode != contracts.GridColumnFlex || cols[1].Value != 1 {
		t.Errorf("col1 = %+v", cols[1])
	}
	if cols[2].Mode != contracts.GridColumnFixed || cols[2].Value != 96 {
		t.Errorf("col2 = %+v", cols[2])
	}
}

func TestGridWeightedColumns(t *testing.T) {
	// Columns="2* * 3*" → 权重 2:1:3
	doc := mustParse(t, gridSrc("2* * 3*", `<Text Value="a"/>`))
	cols := doc.Page.Children[0].Columns
	want := []float32{2, 1, 3}
	for i, w := range want {
		if cols[i].Value != w {
			t.Errorf("col%d 权重 = %v, 期望 %v", i, cols[i].Value, w)
		}
	}
}

func TestGridColumnsRejectsBadWeight(t *testing.T) {
	for _, bad := range []string{"0*", "-1*", "abc", "", "* * *! "} {
		_, ds := Parse("t.gxml", []byte(gridSrc(bad, `<Text Value="a"/>`)))
		if len(ds) == 0 {
			t.Errorf("Columns=%q 应被拒绝", bad)
		}
	}
}

// --- 空 Grid ------------------------------------------------------------

func TestEmptyGridRejected(t *testing.T) {
	mustFail(t, gridSrc("*", ""), CodeAttrCall)
}

// --- 子项 Width 禁止 ----------------------------------------------------

func TestGridChildWidthRejected(t *testing.T) {
	// 规范 §3.1.6：列定义是单元格宽度的唯一来源。
	mustFail(t, gridSrc("100 *", `<Text Value="a" Width="50"/>`), CodeAttrCall)
}

func TestGridChildWidthFillRejected(t *testing.T) {
	// Width="Fill" 同样禁止：单元格已是精确宽度，Fill 在此无意义且会误导。
	mustFail(t, gridSrc("100 *", `<Text Value="a" Width="Fill"/>`), CodeAttrCall)
}

func TestGridChildHeightAllowed(t *testing.T) {
	// 行高由内容决定，Height 合法。
	mustParse(t, gridSrc("100 *", `<Text Value="a" Height="32"/>`))
}

func TestGridCellContentMaySetWidth(t *testing.T) {
	// 直接子项（单元格本身）不能设 Width；
	// 但其内部的后代可以——宽度语义由单元格内的容器承担。
	mustParse(t, gridSrc("100 *", `<Row><Text Value="a" Width="50"/></Row>`))
}

func TestGridDirectChildContainerWithWidthRejected(t *testing.T) {
	// 容器作为单元格时同样是直接子项，设 Width 一样被拒绝。
	mustFail(t, gridSrc("100 *", `<Row Width="Fill"><Text Value="a"/></Row>`), CodeAttrCall)
}

// --- 对齐 ---------------------------------------------------------------

func TestGridAlignAllowed(t *testing.T) {
	doc := mustParse(t, gridSrc("100 *", `<Text Value="a"/>`))
	_ = doc
	doc2 := mustParse(t, wrapSrc(`<Grid Columns="100 *" Align="Center"><Text Value="a"/></Grid>`))
	if got := doc2.Page.Children[0].Align; got != contracts.AlignCenter {
		t.Errorf("Align = %v", got)
	}
}

func TestGridBaselineRejected(t *testing.T) {
	mustFail(t, wrapSrc(`<Grid Columns="100 *" Align="Baseline"><Text Value="a"/></Grid>`), CodeAttrCall)
}

// --- Gap ----------------------------------------------------------------

func TestGridGapAccepted(t *testing.T) {
	doc := mustParse(t, wrapSrc(`<Grid Columns="100 *" Gap="8"><Text Value="a"/></Grid>`))
	if got := doc.Page.Children[0].Gap; got != 8 {
		t.Errorf("Gap = %v", got)
	}
}

func TestGridGapRejectsBinding(t *testing.T) {
	mustFail(t, wrapSrc(`<Grid Columns="100 *" Gap="{G}"><Text Value="a"/></Grid>`), CodeAttrCall)
}
