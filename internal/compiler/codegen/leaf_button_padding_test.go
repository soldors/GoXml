package codegen

// R5-A 同根修复轮：补上轮未闭环的「Button 运行时像素几何」。
// 用 genModuleWithRepo（带 replace 到本树）使真实跨包 VM import 可编译。
// 断言Button Padding内容偏移；事件命中另由非作者探针验证。

import "testing"

// TestLeafButtonPaddingRuntimeRepo —— <Button Padding="20"> 按钮内容右移约 20px。
func TestLeafButtonPaddingRuntimeRepo(t *testing.T) {
	sizeRuntimeRepo(t, Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "dashboard.gxml"},
		`<Button Padding="20" Text="B"/>`, `
func TestProbeButtonPad(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 260, 140), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	b := sbbox(img, image.Rect(0, 0, 260, 140))
	t.Logf("B bbox=%v", b)
	if b.N == 0 {
		t.Fatal("按钮未渲染")
	}
	if b.MinX < 16 {
		t.Fatalf("Button Padding=20 未生效：MinX=%d 应 >= 16", b.MinX)
	}
}
`)
}
