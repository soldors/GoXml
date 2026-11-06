package cli

// R25-A repair-r2 判别回归（F-A4 父目录删除/替换、F-A5 owned/候选输出冲突）。
//
// 复用既有夹具/助手（diskPNGAssetProject/writeOverlayJSON/goFlagsWithOverlay/r25PNG）。
// 默认生成与 --check 两条路径都要验证；不能靠 --check 的 Go 编译失败替默认门禁兜底。
// 保留旧 10 个 R25 用例与 embed-truth。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-A4：overlay 删除**父目录** ui/assets（Go 支持祖先删除）→ 被引用 a.png 有效字节缺失。
// 默认生成与 --check 都必须 fail-closed 且不写产物。
func TestR25OverlayAncestorDeleteRejected(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	ov := writeOverlayJSON(t, root, map[string]string{filepath.Join(ui, "assets"): ""})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))

	// 默认生成
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code == ExitOK {
		t.Fatalf("ancestor-delete must reject default generate, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "CODEGEN-ASSET-002") {
		t.Fatalf("default must fail with CODEGEN-ASSET-002, stderr=%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(ui, "home_gen.go")); !os.IsNotExist(err) {
		t.Fatal("ancestor-delete default must not write artifact")
	}
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Fatal("ancestor-delete default must not write Manifest")
	}

	// --check
	code, stdout, stderr = runStandaloneCLI(t, src, "--check")
	if code == ExitOK {
		t.Fatalf("ancestor-delete must reject --check, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "CODEGEN-ASSET-002") {
		t.Fatalf("--check must fail with CODEGEN-ASSET-002 (asset gate, not Go compile), stderr=%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(ui, "home_gen.go")); !os.IsNotExist(err) {
		t.Fatal("ancestor-delete --check must not write artifact")
	}
}

// F-A4：overlay 把**祖先目录**替换为非空其它目录（无法等价定位后代字节）→ 保守 fail-closed。
func TestR25OverlayAncestorReplaceUnprovableRejected(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	otherDir := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(otherDir, "a.png"), r25PNG(t, 4, 4))
	ov := writeOverlayJSON(t, root, map[string]string{filepath.Join(ui, "assets"): otherDir})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code == ExitOK {
		t.Fatalf("ancestor-replace (unprovable) must fail-closed, stdout=%s", stdout)
	}
	// 该用例在本机甚至更早 fail-closed（go list 报 overlay maps child assets to directory），
	// 仍属写盘前拒绝；本质要求是“不写盘 + 非 0”。若落到资产门禁则以 CODEGEN-ASSET 呈现。
	if _, err := os.Stat(filepath.Join(ui, "home_gen.go")); !os.IsNotExist(err) {
		t.Fatal("ancestor-replace must not write artifact")
	}
	_ = stderr
}

// F-A4 正控制：overlay **只映射无关未用路径**，不影响被引用资产 → 正常生成成功（不误拒）。
func TestR25OverlayUnusedPathNotRejected(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	unused := filepath.Join(root, "unused-dir")
	if err := os.MkdirAll(unused, 0o755); err != nil {
		t.Fatal(err)
	}
	// 映射到与本页无关的路径（既非资产也非祖先）
	ov := writeOverlayJSON(t, root, map[string]string{filepath.Join(root, "some_other_file.go"): ""})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("unrelated overlay must NOT be rejected; rc=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
}

// F-A5：Src 指向本页**自己生成的输出** home_gen.go（owned/本批候选）；用户 overlay 把它映射到
// 合法 PNG 也不能绕过——最终构建候选优先，有效字节是生成 Go 源（非图片）→ 默认生成与 --check
// 都必须在写盘前拒绝。
func TestR25OwnedOutputAsSourceRejected(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	// 先正常生成一次，建立 Manifest/所有权
	if code, _, se := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("seed generate: %s", se)
	}
	old := readCLIFile(t, filepath.Join(ui, "home_gen.go"))
	// 页面改成引用自身产物
	writeCLIFile(t, src, []byte(`<Goxml Version="1"><Page Route="/"><Image Src="home_gen.go"/></Page></Goxml>`))
	// 用户 overlay 把 owned 输出映射到合法 PNG（企图绕过）
	repl := filepath.Join(root, "valid.png")
	writeCLIFile(t, repl, r25PNG(t, 4, 4))
	ov := writeOverlayJSON(t, root, map[string]string{filepath.Join(ui, "home_gen.go"): repl})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))

	// 默认生成：写盘前拒绝，旧产物保留
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code == ExitOK {
		t.Fatalf("owned-output Src must reject default generate, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "CODEGEN-ASSET-002") {
		t.Fatalf("default must fail with CODEGEN-ASSET-002, stderr=%s", stderr)
	}
	if string(readCLIFile(t, filepath.Join(ui, "home_gen.go"))) != string(old) {
		t.Fatal("old artifact must be preserved on reject")
	}

	// --check：同样在写盘前拒绝（不是靠 Go 编译失败）
	code, stdout, stderr = runStandaloneCLI(t, src, "--check")
	if code == ExitOK {
		t.Fatalf("owned-output Src must reject --check, stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "CODEGEN-ASSET-002") {
		t.Fatalf("--check must fail with CODEGEN-ASSET-002 (asset gate), stderr=%s", stderr)
	}
	if string(readCLIFile(t, filepath.Join(ui, "home_gen.go"))) != string(old) {
		t.Fatal("old artifact must be preserved on --check reject")
	}
}
