package codegen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// <Image> 静态资产：生成形态、路径归一、运行时解码
// ============================================================================

func TestImageCodegenEmbedsAsset(t *testing.T) {
	out := mustGen(t, `<Image Src="assets/logo.png" Width="32" Height="32"/>`, defaultOpts())
	src := string(out)

	for _, want := range []string{
		`//go:embed "assets/logo.png"`,
		`var goxmlAssetsDashboard embed.FS`,
		`img0 paint.ImageOp`,
		`widget.Image{Src: v.img0, Fit: widget.Contain, Position: layout.Center}.Layout(gtx)`,
		`goxmlAssetsDashboard.ReadFile("assets/logo.png")`,
		`image.Decode(bytes.NewReader(raw0))`,
		`v.img0 = paint.NewImageOp(img0)`,
		`_ "image/jpeg"`,
		`_ "image/png"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("生成源码缺少 %q\n---\n%s", want, src)
		}
	}
	if strings.Contains(src, "base64") {
		t.Error("生成源码不应把资产 Base64 内联；应使用 //go:embed")
	}
}

func TestImageFitMapping(t *testing.T) {
	cases := map[string]string{
		"":         "widget.Contain",
		"Contain":  "widget.Contain",
		"Cover":    "widget.Cover",
		" Cover ":  "widget.Cover",
		"Unscaled": "widget.Unscaled",
	}
	for fit, want := range cases {
		body := `<Image Src="a.png" Width="32" Height="32"/>`
		if fit != "" {
			body = `<Image Src="a.png" Width="32" Height="32" Fit="` + fit + `"/>`
		}
		out := mustGen(t, body, defaultOpts())
		if !strings.Contains(string(out), "Fit: "+want) {
			t.Errorf("Fit=%q 应映射为 %s\n%s", fit, want, out)
		}
	}
}

func TestImageFitRejectsInvalidAndBinding(t *testing.T) {
	if _, err := gen(t, `<Image Src="a.png" Fit="Stretch"/>`, defaultOpts()); err == nil {
		t.Fatal("非法 Fit 应被拒绝")
	} else if !strings.Contains(err.Error(), "Contain, Cover or Unscaled") {
		t.Errorf("错误信息应列出合法取值: %v", err)
	}
	if _, err := gen(t, `<Image Src="a.png" Fit="{Title}"/>`, defaultOpts()); err == nil {
		t.Fatal("绑定 Fit 应被拒绝")
	}
}

func TestImageDuplicateSrcSharesField(t *testing.T) {
	out := mustGen(t, `<Image Src="a.png"/><Image Src="./a.png"/>`, defaultOpts())
	src := string(out)

	if got := strings.Count(src, "//go:embed"); got != 1 {
		t.Errorf("//go:embed 指令数 = %d，期望 1（同一资产只嵌入一次）", got)
	}
	if got := strings.Count(src, "img0 paint.ImageOp"); got != 1 {
		t.Errorf("img0 字段数 = %d，期望 1", got)
	}
	if strings.Contains(src, "img1 paint.ImageOp") {
		t.Error("重复 Src 不应分配第二个 img 字段")
	}
	if got := strings.Count(src, "widget.Image{Src: v.img0"); got != 2 {
		t.Errorf("两个 Image 节点应复用 img0，实际引用数 = %d", got)
	}
}

func TestImageRejectsBindingSrc(t *testing.T) {
	_, err := gen(t, `<Image Src="{Title}"/>`, defaultOpts())
	if err == nil {
		t.Fatal("绑定 Src 应被拒绝")
	}
	ce, ok := err.(contracts.CompilerError)
	if !ok {
		t.Fatalf("错误类型 = %T，期望 contracts.CompilerError: %v", err, err)
	}
	if ce.Code != CodeAssetPath {
		t.Errorf("诊断码 = %s，期望 %s", ce.Code, CodeAssetPath)
	}
	if !strings.Contains(ce.Message, "static literal") {
		t.Errorf("诊断消息未说明 v1 只接受静态 Src: %q", ce.Message)
	}
}

func TestImageRejectsChildren(t *testing.T) {
	_, err := gen(t, `<Image Src="a.png"><Text Value="x"/></Image>`, defaultOpts())
	if err == nil {
		t.Fatal("<Image> 带子节点应被拒绝")
	}
}

func TestImageBindingPageReturnsDecodeError(t *testing.T) {
	// 绑定页构造函数签名已是 (*View, error)，解码失败必须走 error 而不是 panic。
	opts := defaultOpts()
	opts.ModulePath = "github.com/goxml/goxml"
	out := mustGen(t, `<Image Src="a.png"/><Text Value="{Title}"/>`, opts)
	src := string(out)
	for _, want := range []string{
		`return nil, fmt.Errorf("goxml: read embedded asset %q: %w"`,
		`return nil, fmt.Errorf("goxml: decode embedded asset %q: %w"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("绑定页生成源码缺少 error 路径 %q\n---\n%s", want, src)
		}
	}
}

func TestImageAssetPathWithSpaceIsQuoted(t *testing.T) {
	out := mustGen(t, `<Image Src="assets/my logo.png"/>`, defaultOpts())
	if !strings.Contains(string(out), `//go:embed "assets/my logo.png"`) {
		t.Errorf("含空格的资产路径必须加引号:\n%s", out)
	}
}

func TestAssetRefsWalksConditionalsAndLoops(t *testing.T) {
	v := verified(t, `
		<If Condition="{Dirty}">
			<Then><Image Src="a.png"/></Then>
		</If>
		<For Each="item" In="{Items}" Key="{item.ID}">
			<Image Src="b.png"/>
		</For>
		<Image Src="./a.png"/>
	`)

	refs := AssetRefs(v)
	if len(refs) != 2 {
		t.Fatalf("AssetRefs 数量 = %d，期望 2（a.png 去重）: %+v", len(refs), refs)
	}
	if refs[0].Path != "a.png" || refs[1].Path != "b.png" {
		t.Errorf("资产顺序/路径 = %q, %q，期望 a.png, b.png", refs[0].Path, refs[1].Path)
	}
	for _, ref := range refs {
		if ref.Span.IsZero() {
			t.Errorf("资产 %q 缺少源码位置", ref.Path)
		}
	}
}

func TestAssetRefsFindsImagesFromDefExpansion(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="` + vmPath + `">
  <Defs>
    <Def Name="Pic"><Image Src="a.png"/></Def>
  </Defs>
  <Page Route="/"><Pic/></Page>
</Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer 未通过: %v", codes(r))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatal(err)
	}
	refs := AssetRefs(v)
	if len(refs) != 1 || refs[0].Path != "a.png" {
		t.Fatalf("Def 展开后的资产引用 = %+v，期望 a.png", refs)
	}
}

func TestAssetRefsFindsStaticSrcThroughDefProp(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="` + vmPath + `">
  <Defs>
    <Def Name="Pic" Props="Source:string"><Image Src="{Source}"/></Def>
  </Defs>
  <Page Route="/"><Pic Source="a.png"/></Page>
</Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer 未通过: %v", codes(r))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatal(err)
	}
	refs := AssetRefs(v)
	if len(refs) != 1 || refs[0].Path != "a.png" {
		t.Fatalf("静态 Def 属性替换后的资产引用 = %+v，期望 a.png", refs)
	}
}

func TestNormalizeAssetSrc(t *testing.T) {
	valid := map[string]string{
		"logo.png":      "logo.png",
		"./a.png":       "a.png",
		"a//b.png":      "a/b.png",
		"a/./b.png":     "a/b.png",
		`dir\logo.png`:  "dir/logo.png",
		" assets/x.png": "assets/x.png",
	}
	for in, want := range valid {
		got, err := NormalizeAssetSrc(in)
		if err != nil {
			t.Errorf("NormalizeAssetSrc(%q) 意外失败: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeAssetSrc(%q) = %q，期望 %q", in, got, want)
		}
	}

	invalid := []string{
		"", "   ", "/abs.png", `C:\x.png`, "C:/x.png",
		"//server/share/x.png", "../x.png", "a/../b.png",
		"a/.hidden.png", "a/_hidden.png", "a:b.png", "a\x00b.png",
		"a*.png", "a?.png", "a[1].png", "assets/", "a//",
	}
	for _, in := range invalid {
		if got, err := NormalizeAssetSrc(in); err == nil {
			t.Errorf("NormalizeAssetSrc(%q) = %q，期望拒绝", in, got)
		}
	}
}

func TestValidateAssetBytesLimitsAndFormat(t *testing.T) {
	ref := AssetRef{Src: "logo.png", Path: "logo.png"}
	if _, invalid := ValidateAssetBytes("case.gxml", ref, testPNGBytes(t, 2, 2)); invalid {
		t.Fatal("合法 PNG 不应被拒绝")
	}
	if diag, invalid := ValidateAssetBytes("case.gxml", ref, []byte("not an image")); !invalid || diag.Code != CodeAssetLoad {
		t.Errorf("非图片字节应报 %s，实际 invalid=%v code=%s", CodeAssetLoad, invalid, diag.Code)
	}
	// 截断 PNG：DecodeConfig 可能仍能读到 IHDR，但完整 Decode 必须失败。
	// 否则静态构造函数会在运行期 panic——这正是本门禁要提前拦住的情形。
	full := testPNGBytes(t, 2, 2)
	if diag, invalid := ValidateAssetBytes("case.gxml", ref, full[:len(full)-4]); !invalid || diag.Code != CodeAssetLoad {
		t.Errorf("截断 PNG 应报 %s，实际 invalid=%v code=%s", CodeAssetLoad, invalid, diag.Code)
	}
	big := make([]byte, MaxAssetBytes+1)
	if diag, invalid := ValidateAssetBytes("case.gxml", ref, big); !invalid || diag.Code != CodeAssetLimit {
		t.Errorf("超限资产应报 %s，实际 invalid=%v code=%s", CodeAssetLimit, invalid, diag.Code)
	}
}

func TestGeneratedImageCompilesAndLaysOut(t *testing.T) {
	src := mustGen(t, `<Image Src="assets/logo.png" Width="32" Height="32"/>`, defaultOpts())
	probe := probeCommon + `
func TestProbeImage(t *testing.T) {
	v := NewGoxmlDashboardView()
	if got := v.img0.Size(); got != image.Pt(2, 2) {
		t.Fatalf("解码后图像尺寸 = %v，期望 (2,2)", got)
	}
	th := material.NewTheme()
	var ops op.Ops
	d := v.Layout(testGtx(&ops, 32, 32), th)
	if d.Size != image.Pt(32, 32) {
		t.Fatalf("布局尺寸 = %v，期望 (32,32)", d.Size)
	}
}
`
	out, err := genModuleWithProbeAssets(t, src, probe, map[string][]byte{
		"assets/logo.png": testPNGBytes(t, 2, 2),
	})
	if err != nil {
		t.Fatalf("生成代码未能编译/运行:\n%s", out)
	}
}

func TestGeneratedBindingPageWithImage(t *testing.T) {
	out := rtProbeAssets(t,
		`<Image Src="assets/logo.png" Width="16" Height="16"/><Text Value="{Title}"/>`,
		`
func TestProbeBindingImage(t *testing.T) {
	vm := &fixture.ViewModel{Title: "hello"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.img0.Size(); got != image.Pt(2, 2) {
		t.Fatalf("binding page image size = %v", got)
	}
	th := material.NewTheme()
	var ops op.Ops
	v.Layout(rtGtx(&ops, 64, 32), th)
}
`, map[string][]byte{"assets/logo.png": testPNGBytes(t, 2, 2)})
	if !strings.Contains(out, "[err=nil]") {
		t.Fatalf("绑定页 + Image 未能编译/运行:\n%s", out)
	}
}

func TestGeneratedTwoImagesCompile(t *testing.T) {
	src := mustGen(t, `<Image Src="a.png" Width="8" Height="8"/><Image Src="b.png" Width="8" Height="8"/>`, defaultOpts())
	probe := probeCommon + `
func TestProbeImage(t *testing.T) {
	v := NewGoxmlDashboardView()
	if v.img0.Size() != image.Pt(2, 2) || v.img1.Size() != image.Pt(3, 3) {
		t.Fatalf("图像尺寸 = %v / %v", v.img0.Size(), v.img1.Size())
	}
	th := material.NewTheme()
	var ops op.Ops
	v.Layout(testGtx(&ops, 32, 32), th)
}
`
	out, err := genModuleWithProbeAssets(t, src, probe, map[string][]byte{
		"a.png": testPNGBytes(t, 2, 2),
		"b.png": testPNGBytes(t, 3, 3),
	})
	if err != nil {
		t.Fatalf("多 Image 生成代码未能编译/运行:\n%s", out)
	}
}

func TestGeneratedImageAssetWithSpaceCompiles(t *testing.T) {
	src := mustGen(t, `<Image Src="assets/my logo.png" Width="8" Height="8"/>`, defaultOpts())
	probe := probeCommon + `
func TestProbeImage(t *testing.T) {
	v := NewGoxmlDashboardView()
	if got := v.img0.Size(); got != image.Pt(2, 2) {
		t.Fatalf("含空格路径的图像尺寸 = %v", got)
	}
}
`
	out, err := genModuleWithProbeAssets(t, src, probe, map[string][]byte{
		"assets/my logo.png": testPNGBytes(t, 2, 2),
	})
	if err != nil {
		t.Fatalf("含空格资产路径未能编译/运行:\n%s", out)
	}
}

func TestGeneratedImageInsideIf(t *testing.T) {
	out := rtProbeAssets(t,
		`<If Condition="{Dirty}"><Then><Image Src="assets/a.png" Width="16" Height="16"/></Then><Else><Text Value="empty"/></Else></If>`,
		`
func TestProbeImageIf(t *testing.T) {
	for _, dirty := range []bool{true, false} {
		vm := &fixture.ViewModel{Dirty: dirty}
		v, err := NewGoxmlDashboardView(vm)
		if err != nil {
			t.Fatal(err)
		}
		th := material.NewTheme()
		var ops op.Ops
		v.Layout(rtGtx(&ops, 32, 32), th)
	}
}
`, map[string][]byte{"assets/a.png": testPNGBytes(t, 2, 2)})
	if !strings.Contains(out, "[err=nil]") {
		t.Fatalf("<If> 内 Image 未能编译/运行:\n%s", out)
	}
}

func TestGeneratedImageDrawsPixels(t *testing.T) {
	out := rtProbeAssets(t,
		`<Image Src="assets/a.png" Width="32" Height="32"/>`,
		`
func TestProbeImagePixels(t *testing.T) {
	th := material.NewTheme()
	const w, h = 32, 32
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("headless: %v", err)
	}
	defer win.Release()

	v := NewGoxmlDashboardView()
	var o op.Ops
	v.Layout(rtGtx(&o, w, h), th)
	n := rtNonZero(rtShot(t, win, &o, w, h))
	if n == 0 {
		t.Skip("渲染全零，无 GPU 后端")
	}
	t.Logf("非零像素字节 = %d", n)
}
`, map[string][]byte{"assets/a.png": testPNGBytes(t, 2, 2)})
	if !strings.Contains(out, "[err=nil]") {
		t.Fatalf("Image 像素绘制测试失败:\n%s", out)
	}
}

func TestGeneratedImageInsideForWithItemUsed(t *testing.T) {
	// 用绑定引用 item，避免触发 BUG-026（循环体不引用 Each）；
	// 该用例只验证 Image 在 For 内的嵌入与共享字段。
	out := rtProbeAssets(t,
		`<For Each="item" In="{Items}" Key="{item.ID}"><Text Value="{item.Name}"/><Image Src="assets/a.png" Width="8" Height="8"/></For>`,
		`
func TestProbeImageFor(t *testing.T) {
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	th := material.NewTheme()
	var ops op.Ops
	v.Layout(rtGtx(&ops, 64, 32), th)
}
`, map[string][]byte{"assets/a.png": testPNGBytes(t, 2, 2)})
	if !strings.Contains(out, "[err=nil]") {
		t.Fatalf("<For> 内 Image（引用 item）未能编译/运行:\n%s", out)
	}
}

// testPNGBytes 生成一张真实可解码的 PNG，供 Codegen 与运行时测试共用。
func testPNGBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(20 + x), G: 60, B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码测试 PNG: %v", err)
	}
	return buf.Bytes()
}
