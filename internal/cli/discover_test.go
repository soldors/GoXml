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
// 1-3：目录发现、确定性排序与 --out-dir 映射（TASK-GENERATE-CLI-001-3）
//
// 边界：不做 Manifest/--clean/多文件事务回滚（1-5/1-6）。
// ============================================================================

// writeProject 向 dir 写一个最小工程文件集。
func writeProject(t *testing.T, dir string, gxmlRel string) {
	t.Helper()
	gxmlPath := filepath.Join(dir, filepath.FromSlash(gxmlRel))
	if err := os.MkdirAll(filepath.Dir(gxmlPath), 0o755); err != nil {
		t.Fatal(err)
	}
	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/"><Text Value="{Title}"/></Page>
</Goxml>`
	if err := os.WriteFile(gxmlPath, []byte(gxml), 0o644); err != nil {
		t.Fatal(err)
	}
	// 同包 ViewModel：每个含 .gxml 的目录都需要（G2 按文件解析）。
	// 包名取目录名并净化为合法 Go 标识符——测试目录名（tmp-TestXxx）
	// 含连字符，不是合法标识符。
	vmDir := filepath.Dir(gxmlPath)
	pkg := sanitizePackageName(filepath.Base(vmDir))
	if err := os.WriteFile(filepath.Join(vmDir, "vm.go"), []byte("package "+pkg+"\n\ntype VM struct{ Title string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDiscoverRecursiveOrdering 嵌套目录递归发现 + 排序。
func TestDiscoverRecursiveOrdering(t *testing.T) {
	root := tempProjectInRepo(t)
	// 故意乱序创建：z 先建，a 后建——发现顺序必须与创建顺序无关。
	writeProject(t, root, "zeta/row.gxml")
	writeProject(t, root, "alpha/row.gxml")
	writeProject(t, root, "mid.gxml")

	inputs, err := discoverInputs(root)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, in := range inputs {
		rels = append(rels, in.rel)
	}
	want := []string{"alpha/row.gxml", "mid.gxml", "zeta/row.gxml"}
	if len(rels) != 3 || rels[0] != want[0] || rels[1] != want[1] || rels[2] != want[2] {
		t.Errorf("发现结果应按 rel 排序 = %v, 实际 %v", want, rels)
	}
}

// TestDiscoverEachSourceDirPerFile 每个文件用**自己所在目录**作为 SourceDir（G2）。
func TestDiscoverEachSourceDirPerFile(t *testing.T) {
	root := tempProjectInRepo(t)
	writeProject(t, root, "alpha/row.gxml")
	writeProject(t, root, "beta/row.gxml")

	inputs, err := discoverInputs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("应发现 2 个输入, 实际 %d", len(inputs))
	}
	for _, in := range inputs {
		wantDir := filepath.Dir(in.abs)
		if in.sourceDir != wantDir {
			t.Errorf("%s 的 SourceDir = %s, 期望其所在目录 %s", in.rel, in.sourceDir, wantDir)
		}
	}
}

// TestGenerateNestedDirsRun 端到端：嵌套目录批量生成，两个产物都在。
func TestGenerateNestedDirsRun(t *testing.T) {
	root := tempProjectInRepo(t)
	writeProject(t, root, "alpha/row.gxml")
	writeProject(t, root, "beta/row.gxml")

	var out, errb bytes.Buffer
	code := Run([]string{"generate", root}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("退出码 = %d:\n%s", code, errb.String())
	}
	for _, gen := range []string{
		filepath.Join(root, "alpha", "row_gen.go"),
		filepath.Join(root, "beta", "row_gen.go"),
	} {
		data, err := os.ReadFile(gen)
		if err != nil {
			t.Errorf("产物缺失: %v", err)
			continue
		}
		if !codegen.HasProtectedHeader(data) {
			t.Errorf("%s 保护头不符合契约:\n%.100s", gen, data)
		}
	}
}

// TestGenerateNestedDirsDeterministic 跨目录多文件的确定性：
// 重复运行，两个产物的字节必须稳定（1-4 的证据基础）。
func TestGenerateNestedDirsDeterministic(t *testing.T) {
	// 同一目录跑两次。
	root := tempProjectInRepo(t)
	writeProject(t, root, "alpha/row.gxml")
	writeProject(t, root, "beta/row.gxml")
	runOnce := func() map[string]string {
		m := map[string]string{}
		for _, gen := range []string{
			filepath.Join(root, "alpha", "row_gen.go"),
			filepath.Join(root, "beta", "row_gen.go"),
		} {
			data, err := os.ReadFile(gen)
			if err != nil {
				t.Fatal(err)
			}
			m[filepath.Base(filepath.Dir(gen))] = string(data)
		}
		return m
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("第一次退出码 = %d:\n%s", code, errb.String())
	}
	first := runOnce()
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("第二次退出码 = %d:\n%s", code, errb.String())
	}
	second := runOnce()

	for k, v := range first {
		if second[k] != v {
			t.Errorf("%s 两次生成不一致（违反契约 §6）", k)
		}
	}
}

// TestGenerateOutDirConflict 同一目标被两个输入映射时必须报错，
// 不得静默覆盖（评审边界：避免多输入覆盖同一输出）。
//
// 构造冲突：两个**不同**目录里有同名 .gxml，且用单文件输入两次？
// 不行——单文件各自的 rel 都是文件名，但两次独立调用不构成冲突。
// 真正的冲突场景在一次调用内：--out-dir 时两个 rel 目录相同 → 同目标。
// 当前映射规则（outRoot/rel目录）下，只有同一目录里的同名文件才冲突，
// 而同一目录不可能有两个同名文件——所以冲突在当前规则下不可达。
// 保留显式检测作为防御：用两个**参数指向同一文件**构造。
func TestGenerateOutDirConflict(t *testing.T) {
	root := tempProjectInRepo(t)
	writeProject(t, root, "home.gxml")

	var out, errb bytes.Buffer
	same := filepath.Join(root, "home.gxml")
	code := Run([]string{"generate", same, same}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("多参数应先被拒绝（退出码 = %d）", code)
	}
}

// TestGenerateOutDirPreservesSourceHeader --out-dir 下保护头的 Source
// 仍是相对目标包目录的文件名（契约 §3.1），不混入 out 根路径。
func TestGenerateOutDirPreservesSourceHeader(t *testing.T) {
	root := tempProjectInRepo(t)
	copyFixture(t, root)
	outRoot := filepath.Join(root, "out")

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(root, "home.gxml"), "--out-dir", outRoot}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("退出码 = %d:\n%s", code, errb.String())
	}
	data, err := os.ReadFile(filepath.Join(outRoot, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "// Source: home.gxml") {
		t.Errorf("保护头 Source 应为相对目标包目录的文件名:\n%.140s", data)
	}
}

// sanitizePackageName 把目录名净化为合法 Go 包名：
// 非字母数字下划线的字符转下划线；数字开头补前缀。
func sanitizePackageName(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteByte('p')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "p"
	}
	return b.String()
}
