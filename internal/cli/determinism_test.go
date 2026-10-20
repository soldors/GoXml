package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// ============================================================================
// 1-4：生成确定性（TASK-GENERATE-CLI-001-4，契约 §6）
//
// 证据升级：1-2/1-3 用「字符串相等」比较两次输出；本切片把判据升级为
// **SHA-256 逐字节一致**，并补「不同创建顺序 → 相同产物」的正式证明。
//
// 验收命令（任务卡）：TestGenerationDeterminism 系列在本文件。
// ============================================================================

// sha256Of 计算文件内容的 SHA-256（hex）。
func sha256Of(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// hashesOf 收集一批产物文件的 SHA-256（键为相对路径）。
func hashesOf(t *testing.T, root string, rels []string) map[string]string {
	t.Helper()
	m := make(map[string]string, len(rels))
	for _, rel := range rels {
		m[rel] = sha256Of(t, filepath.Join(root, filepath.FromSlash(rel)))
	}
	return m
}

// TestGenerationDeterminism 核心验收：同一输入**连续两次生成**，
// 所有产物 SHA-256 逐字节一致（文件内容、保护头、import 别名、符号名）。
func TestGenerationDeterminism(t *testing.T) {
	relOutputs := []string{"alpha/row_gen.go", "beta/row_gen.go"}

	runTwice := func() map[string]string {
		root := tempProjectInRepo(t)
		writeProject(t, root, "beta/row.gxml")  // 故意先建 beta
		writeProject(t, root, "alpha/row.gxml") // 再建 alpha（顺序与输出无关）

		var out, errb bytes.Buffer
		for i := 0; i < 2; i++ {
			if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
				t.Fatalf("第 %d 次生成退出码 = %d:\n%s", i+1, code, errb.String())
			}
		}
		return hashesOf(t, root, relOutputs)
	}

	first := runTwice()
	second := runTwice()

	for rel, want := range first {
		got, ok := second[rel]
		if !ok {
			t.Fatalf("第二次运行缺少产物 %s", rel)
		}
		if got != want {
			t.Errorf("%s 的 SHA-256 不一致：\n第一次 %s\n第二次 %s", rel, want, got)
		}
	}
	t.Logf("两次独立运行的 %d 个产物 SHA-256 全部一致", len(first))
}

// TestGenerationDeterministicAcrossCreationOrder 决定性证明：
// **同一组输入在不同创建顺序的两个目录里**，产物必须完全相同。
//
// 这是「顺序由规范化相对路径决定，不依赖文件系统返回顺序」的直接证据——
// 文件系统的 ReadDir 顺序不保证稳定，若发现逻辑依赖它，
// 两个独立目录的产物哈希就会分叉。
func TestGenerationDeterministicAcrossCreationOrder(t *testing.T) {
	relOutputs := []string{"alpha/row_gen.go", "mid_gen.go", "zeta/row_gen.go"}

	// 目录 A：zeta → alpha → mid（创建顺序）。
	rootA := tempProjectInRepo(t)
	writeProject(t, rootA, "zeta/row.gxml")
	writeProject(t, rootA, "alpha/row.gxml")
	writeProject(t, rootA, "mid.gxml")

	// 目录 B：alpha → mid → zeta（相反顺序）。
	rootB := tempProjectInRepo(t)
	writeProject(t, rootB, "alpha/row.gxml")
	writeProject(t, rootB, "mid.gxml")
	writeProject(t, rootB, "zeta/row.gxml")

	generate := func(root string) map[string]string {
		var out, errb bytes.Buffer
		if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
			t.Fatalf("退出码 = %d:\n%s", code, errb.String())
		}
		return hashesOf(t, root, relOutputs)
	}

	hashA := generate(rootA)
	hashB := generate(rootB)

	for rel, want := range hashA {
		got, ok := hashB[rel]
		if !ok {
			t.Fatalf("目录 B 缺少产物 %s", rel)
		}
		if got != want {
			t.Errorf("不同创建顺序下 %s 的哈希不同：\n顺序A %s\n顺序B %s", rel, want, got)
		}
	}
	t.Logf("两种创建顺序（zeta→alpha→mid 与 alpha→mid→zeta）的 %d 个产物哈希完全一致", len(hashA))
}

// TestGenerationHeaderStableAcrossRuns 保护头跨运行稳定：
// Source 字段不含绝对路径、机器名或时间戳（契约 §3.1/§6）。
func TestGenerationHeaderStableAcrossRuns(t *testing.T) {
	rootA := tempProjectInRepo(t)
	rootB := tempProjectInRepo(t)
	for _, root := range []string{rootA, rootB} {
		writeProject(t, root, "row.gxml")
	}

	headers := make([]string, 0, 2)
	for _, root := range []string{rootA, rootB} {
		var out, errb bytes.Buffer
		if code := Run([]string{"generate", filepath.Join(root, "row.gxml")}, &out, &errb); code != ExitOK {
			t.Fatalf("退出码 = %d:\n%s", code, errb.String())
		}
		data, err := os.ReadFile(filepath.Join(root, "row_gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !codegen.HasProtectedHeader(data) {
			t.Fatalf("保护头不符合契约:\n%.120s", data)
		}
		// 取 Source 行（保护头第 2 行）。
		lines := strings.SplitN(string(data), "\n", 3)
		headers = append(headers, lines[1])
	}

	// 两个不同绝对路径目录（t.TempDir 的 tempProjectInRepo 名称不同），
	// Source 行必须相同——证明它不含目录绝对路径。
	if headers[0] != headers[1] {
		t.Errorf("Source 行随目录变化（含了不该有的路径信息）：\n%s\n%s", headers[0], headers[1])
	}
}
