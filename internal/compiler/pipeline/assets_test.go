package pipeline

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

// ============================================================================
// <Image> 静态资产门禁（ADR-0015）
// ============================================================================

func imageSrc(asset string) []byte {
	return []byte(`<Goxml Version="1"><Page Route="/"><Image Src="` + asset + `" Width="16" Height="16"/></Page></Goxml>`)
}

func TestRunImageAssetGate(t *testing.T) {
	cfg := baseCfg(t)
	cfg.TargetDir = t.TempDir()
	cfg.SourceDir = filepath.Join(repoRoot(t), "internal", "compiler", "pipeline")

	// 1) 缺失资产：必须在 Codegen 之前报 CODEGEN-ASSET-002。
	r := Run("dashboard.gxml", imageSrc("assets/logo.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("缺失资产不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetLoad)

	// 2) 放入合法 PNG 后应成功，并生成 embed 指令。
	assetPath := filepath.Join(cfg.TargetDir, "assets", "logo.png")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, pipelineTestPNG(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	r = Run("dashboard.gxml", imageSrc("assets/logo.png"), cfg, Hooks{})
	if !r.OK() {
		t.Fatalf("合法资产应生成成功，诊断: %v", msgs(r))
	}
	if !strings.Contains(string(r.Artifact.Source), `//go:embed "assets/logo.png"`) {
		t.Errorf("产物缺少 //go:embed 指令:\n%s", r.Artifact.Source)
	}

	// 3) 损坏/不支持的格式 → CODEGEN-ASSET-002。
	if err := os.WriteFile(assetPath, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = Run("dashboard.gxml", imageSrc("assets/logo.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("损坏资产不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetLoad)

	// 4) 超过体积预算 → CODEGEN-ASSET-003；stat 应在 ReadFile 之前拦住。
	if err := os.WriteFile(assetPath, make([]byte, codegen.MaxAssetBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	r = Run("dashboard.gxml", imageSrc("assets/logo.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("超限资产不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetLimit)
}

func TestRunImageAssetRejectsTraversal(t *testing.T) {
	cfg := baseCfg(t)
	cfg.TargetDir = t.TempDir()
	cfg.SourceDir = filepath.Join(repoRoot(t), "internal", "compiler", "pipeline")

	r := Run("dashboard.gxml", imageSrc("../outside.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("路径越界不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
}

func TestRunImageAssetRejectsAbsolute(t *testing.T) {
	cfg := baseCfg(t)
	cfg.TargetDir = t.TempDir()
	cfg.SourceDir = filepath.Join(repoRoot(t), "internal", "compiler", "pipeline")

	r := Run("dashboard.gxml", imageSrc("/etc/passwd"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("绝对路径不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
}

// writeAssetImage 在目标包目录下写一个合法 PNG，返回其绝对路径。
func writeAssetImage(t *testing.T, dir, name string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, pipelineTestPNG(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func assetImageCfg(t *testing.T) Config {
	t.Helper()
	cfg := baseCfg(t)
	cfg.TargetDir = t.TempDir()
	cfg.SourceDir = filepath.Join(repoRoot(t), "internal", "compiler", "pipeline")
	return cfg
}

// TestRunImageAssetRejectsCaseMismatch 关闭「声明拼写与磁盘拼写不一致」的晚失败：
// 大小写不敏感的平台会照常 Stat/ReadFile 成功并生成 //go:embed，而大小写敏感的
// 平台在 go build 阶段才报 embed 无匹配。门禁必须提前给出 CODEGEN-ASSET-001，
// 消息里同时含声明路径与磁盘实际拼写（目录层级 + 文件名）。
func TestRunImageAssetRejectsCaseMismatch(t *testing.T) {
	cfg := assetImageCfg(t)
	writeAssetImage(t, cfg.TargetDir, filepath.Join("assets", "logo.png"))

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"文件名段", "assets/Logo.PNG"},
		{"目录段", "Assets/logo.png"},
		{"两段都错", "ASSETS/LOGO.Png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Run("dashboard.gxml", imageSrc(tc.src), cfg, Hooks{})
			if r.OK() {
				t.Fatalf("%q 与磁盘拼写不一致，不应生成成功", tc.src)
			}
			if r.Artifact != nil {
				t.Fatal("大小写错配不得产出 Artifact")
			}
			assertDiagCode(t, r, codegen.CodeAssetPath)
			for _, d := range r.Diags {
				if d.Code != codegen.CodeAssetPath {
					continue
				}
				if !strings.Contains(d.Message, "assets/logo.png") {
					t.Errorf("诊断应给出磁盘实际拼写 assets/logo.png，实际: %s", d.Message)
				}
				if d.Line == 0 {
					t.Errorf("诊断应带源码位置: %+v", d)
				}
			}
		})
	}

	// 对照：声明与磁盘完全一致时必须放行。
	r := Run("dashboard.gxml", imageSrc("assets/logo.png"), cfg, Hooks{})
	if !r.OK() {
		t.Fatalf("正确拼写的合法资产应生成成功，诊断: %v", msgs(r))
	}

	// 对照：Unicode + 空格路径同样放行（门禁不得因非 ASCII 或空格误判）。
	writeAssetImage(t, cfg.TargetDir, filepath.Join("assets", "图标 24.png"))
	r = Run("dashboard.gxml", imageSrc("assets/图标 24.png"), cfg, Hooks{})
	if !r.OK() {
		t.Fatalf("Unicode/空格路径的合法资产应生成成功，诊断: %v", msgs(r))
	}
	// 其大小写错配同样被拒（Unicode 段不走特例）。
	r = Run("dashboard.gxml", imageSrc("assets/图标 24.PNG"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("Unicode 文件名的错误大小写不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
}

// TestRunImageAssetRejectsLinkComponents 用包内文件符号链接验证「链接组件在读取前
// 就被拒绝」。无法证明边界时结构化失败，不得先读包外内容再报错。
func TestRunImageAssetRejectsLinkComponents(t *testing.T) {
	cfg := assetImageCfg(t)
	writeAssetImage(t, cfg.TargetDir, filepath.Join("assets", "logo.png"))

	outside := t.TempDir()
	outsidePNG := filepath.Join(outside, "valid.png")
	if err := os.WriteFile(outsidePNG, pipelineTestPNG(t, 4, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cfg.TargetDir, "assets", "alias.png")
	if err := os.Symlink(outsidePNG, link); err != nil {
		t.Skipf("符号链接不可用（缺少权限）: %v", err)
	}

	r := Run("dashboard.gxml", imageSrc("assets/alias.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("包内符号链接指向包外资产不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
	// 读取后才报错会给出 ASSET-002/003；出现即说明边界判定晚于读取。
	for _, d := range r.Diags {
		if d.Code == codegen.CodeAssetLoad || d.Code == codegen.CodeAssetLimit {
			t.Fatalf("拒绝必须先于资产内容读取，实际诊断: %v", msgs(r))
		}
	}

	// 悬空链接：目标是不存在的路径。修复前会落到 Stat 的「文件不存在」，
	// 即 ASSET-002；边界门禁应先于存在性判断，报 ASSET-001。
	dangling := filepath.Join(cfg.TargetDir, "assets", "dangling.png")
	if err := os.Symlink(filepath.Join(outside, "does-not-exist.png"), dangling); err != nil {
		t.Skipf("符号链接不可用（缺少权限）: %v", err)
	}
	r = Run("dashboard.gxml", imageSrc("assets/dangling.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("悬空符号链接不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
}

// TestValidateAssetsRejectsLinkBeforeReading 直接验证「先证明边界、后读字节」的
// 顺序：包外文件刻意超过体积预算。若实现先读取，得到的会是 CODEGEN-ASSET-003；
// 正确的实现只能在读取之前给出 CODEGEN-ASSET-001。
func TestValidateAssetsRejectsLinkBeforeReading(t *testing.T) {
	cfg := assetImageCfg(t)
	outside := t.TempDir()
	huge := filepath.Join(outside, "huge.png")
	if err := os.WriteFile(huge, make([]byte, codegen.MaxAssetBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cfg.TargetDir, "esc.png")
	if err := os.Symlink(huge, link); err != nil {
		t.Skipf("符号链接不可用（缺少权限）: %v", err)
	}

	r := Run("dashboard.gxml", imageSrc("esc.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("包外链接资产不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
	for _, d := range r.Diags {
		if d.Code == codegen.CodeAssetLimit {
			t.Fatalf("越界判定晚于读取（先读后拒）: %v", msgs(r))
		}
	}
}

func assertDiagCode(t *testing.T, r *Result, code string) {
	t.Helper()
	for _, d := range r.Diags {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("诊断中缺少 %s，实际: %v", code, msgs(r))
}

func pipelineTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 90, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
