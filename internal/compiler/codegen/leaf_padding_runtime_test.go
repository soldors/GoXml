package codegen

// R5批准子集：Text Padding运行时几何。
// 复用 sizeRuntime 探针骨架。观测内容原点与像素颜色。

import "testing"

// TestLeafTextPaddingRuntime —— <Text Padding="20"> 内容右移/下移约 20px（修前忽略）。
func TestLeafTextPaddingRuntime(t *testing.T) {
	sizeRuntime(t, `<Text Padding="20" Value="A"/>`, `
func TestProbeLeafTextPad(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 200, 200), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 200, 200))
	t.Logf("A bbox=%v", b)
	if b.N == 0 {
		t.Fatal("A 未渲染")
	}
	if b.MinX < 16 {
		t.Fatalf("叶子 Text Padding=20 未生效：MinX=%d 应 >= 16", b.MinX)
	}
}
`)
}
