package codegen

// R5-A 片A1：容器 Padding（Column/Row/Grid）进入真实布局的运行时判别回归。
// 复用 size_runtime_test.go 的 headless 探针骨架（sizeRuntime/snew/sshot/sbbox/sexact）。
//
// 观测原则：不能只 grep "layout.Inset"；必须观测生成代码在真实 Gio 布局下的
// 内容原点与非透明像素包围盒，证明 Padding 真的把内容推开了。

import "testing"

// TestPaddingColumnContentOriginRuntime —— 片A1 行为红（修前失败）。
// <Column Padding="16">A/B</Column>，容器从 (0,0) 起；若 Padding 落成真实 Inset，
// A 的非透明像素应下移约 16px（且左移约 16px）。修前 Codegen 忽略 Padding，
// A 落在 y≈0（容忍字体基线 ~2px），应低于阈值。
func TestPaddingColumnContentOriginRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Padding="16"><Text Value="A"/></Column>`, `
func TestProbePaddingColumn(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	d := v.Layout(sexact(&ops, 200, 300), th)
	if d.Size != image.Pt(200, 300) {
		t.Errorf("根尺寸 = %v, 期望 (200,300)", d.Size)
	}
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 200, 120))
	t.Logf("A bbox=%v", b)
	if b.N == 0 {
		t.Fatal("A 未渲染")
	}
	// Padding=16dp @PxPerDp=1 → 内容原点应在 (16,16) 附近。
	// 修前忽略 Padding → MinX≈0, MinY≈0/基线小值。
	if b.MinX < 12 {
		t.Fatalf("Padding 未生效：MinX=%d 应 >= 12（约 16）", b.MinX)
	}
	if b.MinY < 10 {
		t.Fatalf("Padding 未生效：MinY=%d 应 >= 10（约 16 减字体基线）", b.MinY)
	}
}
`)
}

// TestPaddingColumnAsymmetricRuntime —— 四边不对称 1 值/4 值。
// Padding="4 20 8 30" → Top4 Right20 Bottom8 Left30。内容原点左≈30、上≈4。
func TestPaddingColumnAsymmetricRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Padding="4 20 8 30"><Text Value="A"/></Column>`, `
func TestProbePaddingAsym(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 300, 300), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 300, 120))
	t.Logf("A bbox=%v", b)
	if b.N == 0 {
		t.Fatal("A 未渲染")
	}
	if b.MinX < 24 {
		t.Fatalf("左 Padding(30) 未生效：MinX=%d 应 >= 24", b.MinX)
	}
	if b.MinY < 2 || b.MinY > 12 {
		t.Fatalf("上 Padding(4) 不符：MinY=%d 应在 2..12", b.MinY)
	}
}
`)
}

// TestPaddingNestedRuntime —— 嵌套 Column，内层 Padding 相对于外层内容原点累加。
func TestPaddingNestedRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Padding="10"><Column Padding="20"><Text Value="A"/></Column></Column>`, `
func TestProbePaddingNested(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 300, 300), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 300, 120))
	t.Logf("A bbox=%v", b)
	if b.N == 0 {
		t.Fatal("A 未渲染")
	}
	// 外 10 + 内 20 = 30
	if b.MinX < 26 {
		t.Fatalf("嵌套 Padding 未累加：MinX=%d 应 >= 26（约 30）", b.MinX)
	}
}
`)
}

// TestPaddingZeroRuntime —— Padding="0" 等价无 Padding（原点≈0）。
func TestPaddingZeroRuntime(t *testing.T) {
	sizeRuntime(t, `<Column Padding="0"><Text Value="A"/></Column>`, `
func TestProbePaddingZero(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 200, 100), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 200, 100))
	t.Logf("A bbox=%v", b)
	if b.N == 0 {
		t.Fatal("A 未渲染")
	}
	if b.MinX > 4 {
		t.Fatalf("Padding=0 应无左偏移：MinX=%d 应 <= 4", b.MinX)
	}
}
`)
}

// TestPaddingRowRuntime —— Row Padding 同样生效（轴向无关）。
func TestPaddingRowRuntime(t *testing.T) {
	sizeRuntime(t, `<Row Padding="12"><Text Value="A"/></Row>`, `
func TestProbePaddingRow(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 300, 120), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 300, 120))
	t.Logf("A bbox=%v", b)
	if b.N == 0 {
		t.Fatal("A 未渲染")
	}
	if b.MinX < 8 {
		t.Fatalf("Row Padding 未生效：MinX=%d 应 >= 8", b.MinX)
	}
}
`)
}
