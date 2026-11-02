package codegen

// R5-A 片A1 补充：DPI（Metric 缩放）运行时 + For/If/Def 下落点的生成断言。
// 运行时 DPI 走 headless 探针；For/If 落点用生成文本断言（容器 Padding 统一在
// nodeExpr 包裹，For/If 子项经 sizeWrap，不引入第二处判定）。

import (
	"strings"
	"testing"
)

// TestPaddingDPIScaleRuntime —— Metric=PxPerDp=2 时 16dp Padding 应为 32px。
func TestPaddingDPIScaleRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Padding="16"><Text Value="A"/></Column>`, `
func TestProbePaddingDPI(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Exact(image.Pt(400, 400)),
		Metric:      unit.Metric{PxPerDp: 2, PxPerSp: 1},
	}
	v.Layout(gtx, th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 400, 200))
	t.Logf("A bbox=%v", b)
	if b.N == 0 {
		t.Fatal("A 未渲染")
	}
	if b.MinX < 26 || b.MinX > 40 {
		t.Fatalf("DPI=2 左 Padding 应约 32：MinX=%d 应在 26..40", b.MinX)
	}
}
`)
}

// TestPaddingGeneratedOnceForContainers —— 生成文本断言：
// 容器 Padding 生成 `layout.Inset{...}.Layout(gtx, func` 且四边值来自解析后数值。
// 同时验证零 Padding 不生成 Inset（避免无意义装饰）。
func TestPaddingGeneratedOnceForContainers(t *testing.T) {
	gen := func(body string) string {
		// fixture ViewModel 是跨包（vmPath），For/绑定会触发 VM import，需要 ModulePath。
		return string(mustGen(t, body, Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "dashboard.gxml"}))
	}
	// 1 值 16 → 四边 16
	out := gen(`<Column Padding="16"><Text Value="A"/></Column>`)
	if !strings.Contains(out, "layout.Inset{") || !strings.Contains(out, "unit.Dp(16)") {
		t.Fatalf("Column Padding=16 未生成 layout.Inset/unit.Dp(16):\n%s", out)
	}
	// 2 值 "8 12" → 上/下 8，左/右 12（规范 §4.3：2 值=上下,左右）
	out2 := gen(`<Row Padding="8 12"><Text Value="A"/></Row>`)
	if !strings.Contains(out2, "Top: unit.Dp(8)") || !strings.Contains(out2, "Left: unit.Dp(12)") {
		t.Fatalf("2 值 Padding 解析不符（期望 上下8 左右12）:\n%s", out2)
	}
	// 零 Padding 不生成 Inset
	out0 := gen(`<Column Padding="0"><Text Value="A"/></Column>`)
	if strings.Contains(out0, "layout.Inset{") {
		t.Fatalf("Padding=0 不应生成 Inset:\n%s", out0)
	}
	// For 内 Column Padding 同样生成 Inset（逐项一致）
	outFor := gen(`<For Each="item" In="{Items}"><Column Padding="20"><Text Value="{item.Name}"/></Column></For>`)
	if !strings.Contains(outFor, "layout.Inset{") || !strings.Contains(outFor, "unit.Dp(20)") {
		t.Fatalf("For 内 Column Padding=20 未生成 Inset:\n%s", outFor)
	}
}
