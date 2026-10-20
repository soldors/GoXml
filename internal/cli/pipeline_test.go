package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// ============================================================================
// TASK-GENERATE-CLI-001-2：单文件真实管线接线
//
// 决策依据：GENERATE-CLI-001-短检查记录 §6（G2/G6/G7）。
// 验收场景对照评审清单 §三。
// ============================================================================

// fixturePath 返回最小夹具路径。
func fixturePath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("testdata", "miniproj", "home.gxml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("夹具缺失: %v", err)
	}
	return p
}

// tempProjectInRepo 在**仓库内**创建临时工程目录。
//
// 不能用 t.TempDir()：它在系统临时目录下，不在任何 go.mod 管辖内，
// 而 runPipeline 需要 ModuleRoot（向上找 go.mod）。放在本包 testdata
// 之下（.gitignore 的 .goxml/ 不适用），测试结束清理。
func tempProjectInRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "tmp-"+t.Name()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// copyFixture 把最小夹具复制到 dir。
func copyFixture(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"home.gxml", "vm.go"} {
		data, err := os.ReadFile(filepath.Join("testdata", "miniproj", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

}

// TestGenerateSingleFile 单个有效 .gxml：生成文件，退出码 0。
func TestGenerateSingleFile(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0；stderr:\n%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "已生成") {
		t.Errorf("stdout 应报告生成结果:\n%s", out.String())
	}

	// 产物存在且带保护头（G1 的 HasProtectedHeader 直接复用）。
	genPath := filepath.Join(tmp, "home_gen.go")
	got, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("生成文件不存在: %v", err)
	}
	if !codegen.HasProtectedHeader(got) {
		t.Errorf("生成文件保护头不符合契约 §3.1:\n%.120s", got)
	}
}

// TestGenerateMissingInput 输入路径不存在：明确错误，退出码 2。
func TestGenerateMissingInput(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run([]string{"generate", "no/such/file.gxml"}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitFailure)
	}
	if !strings.Contains(errb.String(), "输入路径不可用") {
		t.Errorf("stderr 应说明输入不可用:\n%s", errb.String())
	}
}

// TestGenerateEmptyDir 目录无 .gxml：明确结果，不静默成功。
func TestGenerateEmptyDir(t *testing.T) {
	empty := t.TempDir()
	var out, errb bytes.Buffer
	code := Run([]string{"generate", empty}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitFailure)
	}
	if !strings.Contains(errb.String(), "没有 .gxml") {
		t.Errorf("stderr 应说明目录为空:\n%s", errb.String())
	}
}

// TestGenerateSyntaxFailureDiag 语法失败：诊断 + 退出码 2，不提交产物。
func TestGenerateSyntaxFailureDiag(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)
	bad := filepath.Join(tmp, "bad.gxml")
	if err := os.WriteFile(bad, []byte("<Goxml Version=\"1\"><Unclosed>"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", bad}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitFailure)
	}
	if !strings.Contains(errb.String(), "GXML-") {
		t.Errorf("stderr 应包含 GXML 诊断码:\n%s", errb.String())
	}
	// 失败不得产出半成品。
	if _, err := os.Stat(filepath.Join(tmp, "bad_gen.go")); !os.IsNotExist(err) {
		t.Error("失败时不得提交生成文件")
	}
}

// TestGenerateDeterministic 同一输入重复生成：输出稳定（契约 §6）。
func TestGenerateDeterministic(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)

	genPath := filepath.Join(tmp, "home_gen.go")
	read := func() string {
		data, err := os.ReadFile(genPath)
		if err != nil {
			t.Fatalf("生成文件不存在: %v", err)
		}
		return string(data)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("第一次生成退出码 = %d:\n%s", code, errb.String())
	}
	first := read()

	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("第二次生成退出码 = %d:\n%s", code, errb.String())
	}
	second := read()

	if first != second {
		t.Error("同一输入两次生成结果不一致（违反契约 §6 确定性）")
	}
}

// TestGenerateMultiPathRejected 多路径在 1-2 明确不支持（G4 事务粒度 1-5 前冻结）。
func TestGenerateMultiPathRejected(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run([]string{"generate", "a.gxml", "b.gxml"}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitFailure)
	}
	if !strings.Contains(errb.String(), "多个独立输入参数未支持") {
		t.Errorf("stderr 应说明多路径未支持:\n%s", errb.String())
	}
}

// TestGenerateOutDirMapping --out-dir 映射：输出进 out 根，保护头 Source 不变。
func TestGenerateOutDirMapping(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)
	outRoot := filepath.Join(tmp, "out")

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(tmp, "home.gxml"), "--out-dir", outRoot}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("退出码 = %d:\n%s", code, errb.String())
	}
	got, err := os.ReadFile(filepath.Join(outRoot, "home_gen.go"))
	if err != nil {
		t.Fatalf("输出应落在 out 根: %v", err)
	}
	if !codegen.HasProtectedHeader(got) {
		t.Errorf("保护头不符合契约:\n%.120s", got)
	}
}
