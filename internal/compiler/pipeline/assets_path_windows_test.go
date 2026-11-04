//go:build windows

package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// ============================================================================
// Windows junction（reparse point）下的资产边界
//
// 本机实测（go1.26.1 windows/amd64，见交付 handoff）：
//   - 目录 junction 的 os.Lstat 模式位是 ModeIrregular，**不是** ModeSymlink，
//     也不是 IsDir；
//   - 经由 junction 的路径上 filepath.EvalSymlinks **会失败**
//     （"The system cannot find the path specified."），
//     而随后的 os.Stat / os.ReadFile **照样成功**并读到包外字节；
//   - 创建 junction 不需要符号链接特权，因此本文件不在无特权环境跳过。
//
// 这三条一起构成原实现的失效开放：EvalSymlinks 报错 → 跳过边界判定 →
// Stat/ReadFile 读取包外文件 → 生成 //go:embed。
// ============================================================================

func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("创建 junction 失败（Windows 门禁用例需要它）: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

// TestRunImageAssetRejectsJunctionEscapeWithValidPNG 用**包外合法 PNG**验证：
// 内容完全可用也不得绕过包边界。
func TestRunImageAssetRejectsJunctionEscapeWithValidPNG(t *testing.T) {
	cfg := assetImageCfg(t)
	writeAssetImage(t, cfg.TargetDir, filepath.Join("assets", "logo.png"))

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "valid.png"), pipelineTestPNG(t, 4, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, filepath.Join(cfg.TargetDir, "esc"), outside)

	r := Run("dashboard.gxml", imageSrc("esc/valid.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("经 junction 读取包外资产不应生成成功")
	}
	if r.Artifact != nil {
		t.Fatal("越界资产不得产出 Artifact")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
	for _, d := range r.Diags {
		if d.Code == codegen.CodeAssetLoad || d.Code == codegen.CodeAssetLimit {
			t.Fatalf("拒绝必须先于资产内容读取，实际诊断: %v", msgs(r))
		}
		if d.Code == codegen.CodeAssetPath && !strings.Contains(d.Message, "esc") {
			t.Errorf("诊断应指出越界的链接组件 esc，实际: %s", d.Message)
		}
	}

	// 对照：同一包目录下的真实文件仍然放行。
	if r := Run("dashboard.gxml", imageSrc("assets/logo.png"), cfg, Hooks{}); !r.OK() {
		t.Fatalf("包内合法资产应生成成功，诊断: %v", msgs(r))
	}
}

// TestRunImageAssetRejectsJunctionBeforeReading 把包外文件做成**超过体积预算**的
// 字节。若实现先读取再判边界，得到的是 CODEGEN-ASSET-003；正确实现只给 001。
func TestRunImageAssetRejectsJunctionBeforeReading(t *testing.T) {
	cfg := assetImageCfg(t)
	outside := t.TempDir()
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "huge.png"),
		make([]byte, codegen.MaxAssetBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	makeJunction(t, filepath.Join(cfg.TargetDir, "esc"), outside)

	r := Run("dashboard.gxml", imageSrc("esc/huge.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("包外超限资产不应生成成功")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
	for _, d := range r.Diags {
		if d.Code == codegen.CodeAssetLimit {
			t.Fatalf("先读后拒：超限诊断来自包外文件，实际诊断: %v", msgs(r))
		}
	}
}

// TestRunImageAssetAllowsJunctionRoot 守住不误伤：包目录**本身**经 junction 到达时，
// 资产仍在包内，必须照常生成。边界判断只针对包目录以下的组件。
func TestRunImageAssetAllowsJunctionRoot(t *testing.T) {
	realPkg := t.TempDir()
	writeAssetImage(t, realPkg, filepath.Join("assets", "logo.png"))

	via := filepath.Join(t.TempDir(), "junction-root")
	makeJunction(t, via, realPkg)

	cfg := assetImageCfg(t)
	cfg.TargetDir = via

	r := Run("dashboard.gxml", imageSrc("assets/logo.png"), cfg, Hooks{})
	if !r.OK() {
		t.Fatalf("经 junction 到达的包目录内的合法资产应生成成功，诊断: %v", msgs(r))
	}
	if !strings.Contains(string(r.Artifact.Source), `//go:embed "assets/logo.png"`) {
		t.Errorf("产物缺少 //go:embed 指令:\n%s", r.Artifact.Source)
	}
}

// TestRunImageAssetRejectsJunctionPointingInside 记录一个**刻意失败开放**的取舍：
// 包内 junction 即便指向包内，也无法用 EvalSymlinks 证明边界（它可能返回未解析的
// junction 叶而不报错），因此按「无法证实即拒绝」处理。
func TestRunImageAssetRejectsJunctionPointingInside(t *testing.T) {
	cfg := assetImageCfg(t)
	writeAssetImage(t, cfg.TargetDir, filepath.Join("inner", "logo.png"))
	makeJunction(t, filepath.Join(cfg.TargetDir, "loop"), filepath.Join(cfg.TargetDir, "inner"))

	r := Run("dashboard.gxml", imageSrc("loop/logo.png"), cfg, Hooks{})
	if r.OK() {
		t.Fatal("经 junction 的资产路径应因无法证实边界而被拒")
	}
	assertDiagCode(t, r, codegen.CodeAssetPath)
}
