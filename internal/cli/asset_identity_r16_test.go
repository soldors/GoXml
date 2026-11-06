package cli

// R16-A A3 判别回归：静态 <Image> 资产文件内容进入缓存身份（K12），
// 使"资产被删除/改坏/改错大小写"由缓存假命中变为安全 miss，进而由资产门禁拒绝。
//
// 历史反例（未接 K12）：删掉资产后二次 generate 仍报"缓存命中"，旧产物沿用、门禁不执行。
// 若移除 K12（assetsFingerprint 不再并入键），本文件相关断言变红。

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

func r16PNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: 0x22, B: 0x33, A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func r16ImageProject(t *testing.T) string {
	t.Helper()
	root := standaloneCLIProject(t)
	_ = os.Remove(filepath.Join(root, "home.gxml"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "doc.go"), []byte("package ui\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "home.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/"><Image Src="a/logo.png"/><Text Value="x"/></Page></Goxml>`))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "a", "logo.png"), r16PNG(t, 4, 4))
	return root
}

// TestAssetCacheIdentity_DeleteInvalidatesEnsuresGateRuns 删除资产 -> 安全 miss -> 拒绝并保旧。
func TestAssetCacheIdentity_DeleteInvalidatesEnsuresGateRuns(t *testing.T) {
	root := r16ImageProject(t)
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	src := filepath.Join(root, "ui", "home.gxml")
	gen := filepath.Join(root, "ui", "home_gen.go")

	if code, _, stderr := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("预热生成应成功: %s", stderr)
	}
	before := readCLIFile(t, gen)
	st1, _ := os.Stat(gen)
	manBefore := readCLIFile(t, manifestPath(root))

	// 删除资产文件（Go 源码不变）——缓存必须安全 miss，门禁随后拒绝。
	if err := os.Remove(filepath.Join(root, "ui", "a", "logo.png")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code == ExitOK {
		t.Fatalf("资产被删除后必须拒绝，实际成功\nstdout:%s", stdout)
	}
	if strings.Contains(stdout, "缓存命中") {
		t.Fatalf("资产删除后不得缓存命中（K12 未生效）: %s", stdout)
	}
	st2, _ := os.Stat(gen)
	if string(readCLIFile(t, gen)) != string(before) || st1.ModTime() != st2.ModTime() {
		t.Fatal("失败时不得改动旧产物字节/mtime")
	}
	if string(readCLIFile(t, manifestPath(root))) != string(manBefore) {
		t.Fatal("失败时不得改动 Manifest")
	}
	_ = stderr
}

// TestAssetCacheIdentity_ContentChangeStillRegenerates 资产内容改变（Go 源码等价）-> 安全 miss 并成功。
func TestAssetCacheIdentity_ContentChangeStillRegenerates(t *testing.T) {
	root := r16ImageProject(t)
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	src := filepath.Join(root, "ui", "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("预热生成应成功: %s", stderr)
	}
	// 换成不同尺寸的合法 PNG：Go 源（embed 指令）不变，但资产内容变了。
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "a", "logo.png"), r16PNG(t, 6, 6))
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("合法资产内容改变应可重新生成: %s", stderr)
	}
	if strings.Contains(stdout, "缓存命中") {
		t.Fatalf("资产内容变化应安全 miss，stdout: %s", stdout)
	}
}

// TestAssetCacheIdentity_SpellingChangeInvalidates 大小写错拼 -> 安全 miss -> 门禁拒绝。
func TestAssetCacheIdentity_SpellingChangeInvalidates(t *testing.T) {
	root := r16ImageProject(t)
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	src := filepath.Join(root, "ui", "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("预热生成应成功: %s", stderr)
	}
	// 磁盘改名为 Logo.PNG（Src 仍是 a/logo.png）：Windows 大小写不敏感下 ReadFile 仍成功。
	oldP := filepath.Join(root, "ui", "a", "logo.png")
	newP := filepath.Join(root, "ui", "a", "Logo.PNG")
	if err := os.Rename(oldP, newP); err != nil {
		t.Skipf("rename unavailable: %v", err)
	}
	code, stdout, _ := runStandaloneCLI(t, src)
	if code == ExitOK && strings.Contains(stdout, "缓存命中") {
		t.Fatalf("大小写错拼不得缓存命中（spelling 未入键）: %s", stdout)
	}
	if code == ExitOK {
		t.Fatalf("大小写错拼应被资产门禁拒绝（ASSET-001）")
	}
}

// TestAssetFingerprintUnit assetsFingerprint 纯函数行为。
func TestAssetFingerprintUnit(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a", "logo.png"), "AAAA")
	src := []byte(`<Goxml Version="1"><Page Route="/"><Image Src="a/logo.png"/></Page></Goxml>`)
	f1, ok := assetsFingerprint("home.gxml", src, root, nil)
	if !ok || f1 == "" {
		t.Fatal("含静态资产应有指纹")
	}
	// 无资产
	if got, ok := assetsFingerprint("home.gxml", []byte(`<Goxml Version="1"><Page Route="/"><Text Value="x"/></Page></Goxml>`), root, nil); !ok || got != "" {
		t.Fatalf("无资产应为空指纹: %q", got)
	}
	// 内容变化 -> 指纹变化
	mustWrite(t, filepath.Join(root, "a", "logo.png"), "BBBB")
	if f2, ok := assetsFingerprint("home.gxml", src, root, nil); !ok || f2 == f1 {
		t.Fatal("资产内容变化应改变指纹")
	}
}

func TestAssetCacheIdentity_OversizedAssetIsUnsafe(t *testing.T) {
	root := t.TempDir()
	writeCLIFile(t, filepath.Join(root, "large.png"), make([]byte, codegen.MaxAssetBytes+1))
	src := []byte(`<Goxml Version="1"><Page Route="/"><Image Src="large.png"/></Page></Goxml>`)
	if fp, ok := assetsFingerprint("home.gxml", src, root, nil); ok || fp != "" {
		t.Fatalf("oversized asset must disable cache lookup: ok=%v fp=%q", ok, fp)
	}
}
