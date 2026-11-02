package codegen

// R6-A 片1：Padding 与「固定尺寸 / Fill / 无界」的次序组合边界（R5 未覆盖的组合）。
// 语义依据：sizeWrap 先建立**外部**约束（Exact/Fill），nodeExpr 内的 layout.Inset
// 再收缩**内容**区域——所以「固定尺寸」指含 Padding 在内的外框，内容在框内再内缩。
// 用真实生成→独立 module→headless 离屏几何判别内容原点与容器尺寸。

import (
	"strings"
	"testing"
)

// 固定 Width/Height + Padding：外框=固定尺寸，内容原点内缩到 Padding。
func TestPaddingWithFixedSizeRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Width="120" Height="80" Padding="10"><Text Value="A"/></Column>`, `
func TestProbePadFixed(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	d := v.Layout(sexact(&ops, 300, 200), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	content := sbbox(img, image.Rect(0, 0, 300, 200))
	t.Logf("dims=%v content=%v", d.Size, content)
	if content.N == 0 { t.Fatal("no content") }
	// 内容原点应在 Padding(10) 之内（约 10，减字体基线）。
	if content.MinX < 6 || content.MinY < 0 {
		t.Fatalf("固定尺寸下 Padding 未内缩内容：MinX=%d MinY=%d", content.MinX, content.MinY)
	}
	if content.MinX > 16 {
		t.Fatalf("内容原点过大，疑似 Padding 重复叠加：MinX=%d", content.MinX)
	}
}
`)
}

// Fill 宽 + Padding：容器占满父宽，内容在 Padding 内。
func TestPaddingWithFillWidthRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Width="Fill" Padding="16"><Text Value="A"/></Column>`, `
func TestProbePadFill(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 300, 120), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	content := sbbox(img, image.Rect(0, 0, 300, 120))
	t.Logf("content=%v", content)
	if content.N == 0 { t.Fatal("no content") }
	if content.MinX < 12 {
		t.Fatalf("Fill+Padding 内容未内缩：MinX=%d 应 >= 12", content.MinX)
	}
}
`)
}

// 无界约束 + Padding：不应塌成 0 或异常溢出。
func TestPaddingUnderUnboundedRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Padding="8"><Text Value="A"/></Column>`, `
func TestProbePadUnbounded(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	// 无界（超大 Max）。
	gtx := sg(&ops, image.Pt(1<<30, 1<<30))
	d := v.Layout(gtx, th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	content := sbbox(img, image.Rect(0, 0, 400, 200))
	t.Logf("dims=%v content=%v", d.Size, content)
	if content.N == 0 { t.Fatal("无界下内容未渲染") }
	if content.MinX < 4 {
		t.Fatalf("无界+Padding 内容未内缩：MinX=%d", content.MinX)
	}
}
`)
}

// 零 Padding 不产生额外 Inset（生成文本对照）+ Fixed 尺寸仍精确。
func TestPaddingZeroNoInsetWithFixed(t *testing.T) {
	opts := Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "dashboard.gxml"}
	out := string(mustGen(t, `<Column Width="120" Height="80" Padding="0"><Text Value="A"/></Column>`, opts))
	if strings.Contains(out, "layout.Inset{") {
		t.Fatalf("Padding=0 不应生成 Inset:\n%s", out)
	}
	if !strings.Contains(out, "gtx.Dp(120)") || !strings.Contains(out, "gtx.Dp(80)") {
		t.Fatalf("固定尺寸应生成精确约束:\n%s", out)
	}
}
