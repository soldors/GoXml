package cli

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateImageAsset 验证 CLI 把 TargetDir 传给 pipeline，并生成 embed 指令。
func TestGenerateImageAsset(t *testing.T) {
	dir := tempProjectInRepo(t)
	copyFixture(t, dir)

	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
    <Image Src="assets/logo.png" Width="16" Height="16"/>
    <Text Value="{Title}"/>
  </Page>
</Goxml>`
	if err := os.WriteFile(filepath.Join(dir, "home.gxml"), []byte(gxml), 0o644); err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(dir, "assets", "logo.png")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, cliTestPNG(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(dir, "home.gxml")}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("退出码 = %d，期望 0；stderr:\n%s", code, errb.String())
	}
	gen, err := os.ReadFile(filepath.Join(dir, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gen), `//go:embed "assets/logo.png"`) {
		t.Errorf("产物缺少 //go:embed 指令:\n%s", gen)
	}
}

// TestGenerateImageAssetMissing 验证缺失资产在写盘前被 CLI 拒绝。
func TestGenerateImageAssetMissing(t *testing.T) {
	dir := tempProjectInRepo(t)
	copyFixture(t, dir)

	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
    <Image Src="assets/missing.png"/>
  </Page>
</Goxml>`
	if err := os.WriteFile(filepath.Join(dir, "home.gxml"), []byte(gxml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(dir, "home.gxml")}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d；stdout:\n%s\nstderr:\n%s",
			code, ExitFailure, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "CODEGEN-ASSET-002") {
		t.Errorf("stderr 应包含 CODEGEN-ASSET-002:\n%s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "home_gen.go")); !os.IsNotExist(err) {
		t.Errorf("失败时不应写出产物，Stat err = %v", err)
	}
}

func TestGenerateImageAssetOutDir(t *testing.T) {
	dir := tempProjectInRepo(t)
	copyFixture(t, dir)

	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
    <Image Src="assets/logo.png"/>
  </Page>
</Goxml>`
	if err := os.WriteFile(filepath.Join(dir, "home.gxml"), []byte(gxml), 0o644); err != nil {
		t.Fatal(err)
	}
	// --out-dir 下资产根是输出包目录，不是源目录。
	outRoot := filepath.Join(dir, "out")
	assetPath := filepath.Join(outRoot, "assets", "logo.png")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, cliTestPNG(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(dir, "home.gxml"), "--out-dir", outRoot}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("退出码 = %d，期望 0；stderr:\n%s", code, errb.String())
	}
	gen, err := os.ReadFile(filepath.Join(outRoot, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gen), `//go:embed "assets/logo.png"`) {
		t.Errorf("--out-dir 产物缺少 //go:embed，说明 TargetDir 未生效:\n%s", gen)
	}
}

func TestGenerateImageNamingConflict(t *testing.T) {
	dir := tempProjectInRepo(t)
	copyFixture(t, dir)

	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
    <Image Src="assets/logo.png"/>
  </Page>
</Goxml>`
	if err := os.WriteFile(filepath.Join(dir, "home.gxml"), []byte(gxml), 0o644); err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(dir, "assets", "logo.png")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, cliTestPNG(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	// 与生成文件的 goxmlAssetsHome 撞名；预检必须在写盘前报 CODEGEN-NAME-001。
	conflict := "package miniproj\n\nvar goxmlAssetsHome = 1\n"
	if err := os.WriteFile(filepath.Join(dir, "conflict.go"), []byte(conflict), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(dir, "home.gxml")}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d；stdout:\n%s\nstderr:\n%s",
			code, ExitFailure, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "CODEGEN-NAME-001") {
		t.Errorf("stderr 应包含 CODEGEN-NAME-001:\n%s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "home_gen.go")); !os.IsNotExist(err) {
		t.Errorf("命名冲突时不应写出产物，Stat err = %v", err)
	}
}

func cliTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 100, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// ---- R16-A A1：与 R14 导入别名消歧 / R15 缓存 v8 的组合覆盖 ----

// TestGenerateImageSamePackageMultiPage 同包多页各含 <Image>：view/asset 变量按源文件唯一，
// 两页同时生成不冲突；每个产物各自 embed 自己的资产。
func TestGenerateImageSamePackageMultiPage(t *testing.T) {
	dir := tempProjectInRepo(t)
	copyFixture(t, dir)
	for _, name := range []string{"a", "b"} {
		gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/` + name + `">
    <Image Src="assets/` + name + `.png" Width="8" Height="8"/>
    <Text Value="{Title}"/>
  </Page>
</Goxml>`
		if err := os.WriteFile(filepath.Join(dir, name+".gxml"), []byte(gxml), 0o644); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "assets", name+".png")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, cliTestPNG(t, 2, 2), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	code := Run([]string{"generate", dir}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("同包多页含 Image 应成功，rc=%d；stderr:\n%s", code, errb.String())
	}
	for _, name := range []string{"a", "b"} {
		gen, err := os.ReadFile(filepath.Join(dir, name+"_gen.go"))
		if err != nil {
			t.Fatalf("%s 产物缺失: %v", name, err)
		}
		if !strings.Contains(string(gen), `//go:embed "assets/`+name+`.png"`) {
			t.Errorf("%s 产物缺少自身 embed 指令", name)
		}
	}
}

// TestGenerateImageWithImportConflict 目标包已有与生成代码默认导入同名的包级标识符时，
// R14 的导入别名消歧仍成立（生成物用别名导入，包级同名标识符不被遮蔽）。
func TestGenerateImageWithImportConflict(t *testing.T) {
	dir := tempProjectInRepo(t)
	copyFixture(t, dir)
	// 目标包声明 `layout`/`paint`/`image` 等与生成代码导入包同名的包级标识符。
	user := `package miniproj

var image = 1
var layout = 2
var paint = 3
var widget = 4
var bytes = 5
var embed = 6
var fmt = 7
`
	if err := os.WriteFile(filepath.Join(dir, "user_conflict.go"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
    <Image Src="assets/logo.png" Width="8" Height="8"/>
    <Text Value="{Title}"/>
  </Page>
</Goxml>`
	if err := os.WriteFile(filepath.Join(dir, "home.gxml"), []byte(gxml), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "assets", "logo.png")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, cliTestPNG(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(dir, "home.gxml"), "--check"}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("含导入冲突的工程应成功（R14 别名消歧），rc=%d；stderr:\n%s", code, errb.String())
	}
	gen, err := os.ReadFile(filepath.Join(dir, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	// 生成物不得以裸 `image`/`layout`/`paint` 作为导入名（会与包级标识符冲突）：
	// 必须出现别名形式（`<alias> "…"`）。
	s := string(gen)
	if strings.Contains(s, "\t\"image\"\n") || strings.Contains(s, "\t\"gioui.org/layout\"\n") {
		t.Errorf("生成物以裸包名导入，未做别名消歧:\n%s", s)
	}
}
