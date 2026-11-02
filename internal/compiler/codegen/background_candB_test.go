package codegen

// R6-A 片2 候选B（OuterBox）判别：Background 覆盖**含 Padding 的外框**。
// 与候选A 的关键区别：Padding 列（0..10）**应有背景色**（外框覆盖），内容仍内缩。
// 仅候选分支 codex/r6-candB；未批准、不默认合入。真实 headless 像素。

import "testing"

// 外框：`<Text Padding="12" Background="#FF0000">` 最左 Padding 列(0..10)应有红（外框），内容也内缩。
func TestCandB_OuterBoxRegionRuntime(t *testing.T) {
	sizeRuntime(t, `<Text Padding="12" Background="#FF0000" Value="A"/>`, `
func TestProbeCandBRegion(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 200, 120), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	countRed := func(x0,x1 int) int { n:=0; for y:=0;y<80;y++{ for x:=x0;x<x1;x++{ i:=y*img.Stride+x*4; r:=img.Pix[i]; g:=img.Pix[i+1]; b:=img.Pix[i+2]; a:=img.Pix[i+3]; if a>128&&r>128&&g<100&&b<100 {n++} } }; return n }
	padLeft := countRed(0,10)
	body := countRed(12,120)
	// 文字像素：非红、非透明（红背景上的深色文字）——用于证明内容已内缩到 Padding 之后。
	countText := func(x0,x1 int) int { n:=0; for y:=0;y<80;y++{ for x:=x0;x<x1;x++{ i:=y*img.Stride+x*4; r:=img.Pix[i]; g:=img.Pix[i+1]; b:=img.Pix[i+2]; a:=img.Pix[i+3]; if a>128 && !(r>128&&g<100&&b<100) && (r<120||g<120||b<120) {n++} } }; return n }
	textInPad := countText(0,11)
	textInBody := countText(12,120)
	t.Logf("padLeft=%d body=%d textInPad=%d textInBody=%d", padLeft, body, textInPad, textInBody)
	if padLeft < 20 { t.Fatalf("候选B 外框：Padding 列应有背景色，padLeft=%d", padLeft) }
	if body < 20 { t.Fatalf("内容区应有背景: body=%d", body) }
	// 文字应在 Padding 之后（内容区）出现，Padding 列内不应有文字像素。
	if textInBody < 20 { t.Fatalf("内容区应有文字: textInBody=%d", textInBody) }
	if textInPad > 5 { t.Fatalf("Padding 列内不应有文字(内容应内缩): textInPad=%d", textInPad) }
}
`)
}

// 无 Padding 时 B 与 A 等价（外框=内容框）：区域仍正确、无 Padding 列概念。
func TestCandB_NoPaddingEquivRuntime(t *testing.T) {
	sizeRuntime(t, `<Text Background="#FF0000" Value="A"/>`, `
func TestProbeCandBNoPad(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	v.Layout(sexact(&ops, 120, 60), th)
	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)
	red := 0; for y:=0;y<40;y++{ for x:=0;x<60;x++{ i:=y*img.Stride+x*4; r:=img.Pix[i]; g:=img.Pix[i+1]; b:=img.Pix[i+2]; a:=img.Pix[i+3]; if a>128&&r>128&&g<100&&b<100 {red++} } }
	t.Logf("red=%d", red)
	if red < 20 { t.Fatalf("无 Padding 的 B 背景应绘制 red=%d", red) }
}
`)
}

// alpha 与静态重放（B 同样需正确）。
func TestCandB_AlphaAndReplayRuntime(t *testing.T) {
	sizeRuntime(t, `<Text Padding="8" Background="#FF000080" Value="A"/>`, `
func TestProbeCandBAlphaReplay(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops op.Ops
	gtx := sexact(&ops, 160, 80)
	win := snew(t)
	defer win.Release()
	red, maxA := 0, 0
	ops.Reset(); v.Layout(gtx, th); img:=sshot(t,win,&ops)
	for y:=0;y<60;y++{ for x:=0;x<80;x++{ i:=y*img.Stride+x*4; r:=img.Pix[i]; g:=img.Pix[i+1]; b:=img.Pix[i+2]; a:=int(img.Pix[i+3]); if a>0&&r>100&&g<120&&b<120 { red++; if a>maxA {maxA=a} } } }
	f1 := red
	ops.Reset(); v.Layout(gtx, th); img2:=sshot(t,win,&ops); f2:=0
	for y:=0;y<60;y++{ for x:=0;x<80;x++{ i:=y*img2.Stride+x*4; r:=img2.Pix[i]; g:=img2.Pix[i+1]; b:=img2.Pix[i+2]; a:=img2.Pix[i+3]; if a>0&&r>100&&g<120&&b<120 { f2++ } } }
	t.Logf("f1=%d f2=%d maxA=%d", f1, f2, maxA)
	if f1 < 10 || f2 < 10 { t.Fatalf("alpha 背景重放丢失 f1=%d f2=%d", f1, f2) }
	if maxA >= 250 { t.Fatalf("alpha=0x80 不应完全不透明 maxA=%d", maxA) }
}
`)
}
