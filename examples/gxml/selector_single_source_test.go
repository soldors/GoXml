package examples

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestExamplesSelectorSingleSource 检查"示例名只在脚本里注册一处"这条约定，
// 以及四个示例目录与注册表**互为全集**。
//
// 它替代不了选择器的行为测试（那要真跑脚本，见 scripts/test-verify-examples-*.py），
// 但能挡住最常见的漂移：往 testdata 里加了第五个示例却忘了注册，或者
// 在脚本别处又抄了一份示例名列表（那样 `all` 与 `-Example` 就可能对不上）。
//
// 判据全部落在**可数字节/集合比较**上，不靠"文档里写过"。
func TestExamplesSelectorSingleSource(t *testing.T) {
	repo := filepath.Join("..", "..")
	ps1 := filepath.Join(repo, "scripts", "verify-examples.ps1")
	raw, err := os.ReadFile(ps1)
	if err != nil {
		t.Fatal(err)
	}

	// 1) .ps1 契约：UTF-8 with BOM + 纯 LF（Windows PowerShell 5.1 读中文注释的前提）。
	if !bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("verify-examples.ps1 缺 UTF-8 BOM：PS 5.1 会把中文读成乱码")
	}
	if n := bytes.Count(raw, []byte("\r")); n != 0 {
		t.Fatalf("verify-examples.ps1 含 %d 个 CR：仓库 .gitattributes 要求 LF", n)
	}

	// 2) 注册点唯一：`$known = @(...)` 只能出现一次。
	def := regexp.MustCompile(`\$known\s*=\s*@\(([^)]*)\)`)
	all := def.FindAllSubmatch(raw, -1)
	if len(all) != 1 {
		t.Fatalf("$known 定义应恰好 1 处，实际 %d 处", len(all))
	}
	var names []string
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(all[0][1], -1) {
		names = append(names, string(m[1]))
	}
	want := []string{"starter", "tasks", "forms", "workbench"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("$known 应为 %v（顺序也固定），实际 %v", want, names)
	}

	// 3) 每个示例名在脚本里**只出现一次**（就是 $known 那次）：
	//    再抄一份列表，`all` 与 `-Example` 就可能各自漂移。
	for _, name := range want {
		if n := bytes.Count(raw, []byte("'"+name+"'")); n != 1 {
			t.Fatalf("示例名 %q 在脚本里出现 %d 次（应只在 $known 出现 1 次）", name, n)
		}
	}

	// 4) testdata 目录集合与注册表互为全集。
	entries, err := os.ReadDir(filepath.Join("testdata"))
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	sortedWant := append([]string(nil), want...)
	sort.Strings(sortedWant)
	if strings.Join(dirs, ",") != strings.Join(sortedWant, ",") {
		t.Fatalf("testdata 目录应恰为 %v，实际 %v（多了没注册，或注册了没材料）", sortedWant, dirs)
	}

	// 5) 三个必备文件都在；starter 的页面名是 home.gxml（不能从目录名推导）。
	for _, name := range want {
		dir := filepath.Join("testdata", name)
		page := name + ".gxml"
		if name == "starter" {
			page = "home.gxml"
		}
		for _, f := range []string{page, "vm.go", "main.go.txt"} {
			if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Size() == 0 {
				t.Fatalf("%s 缺文件或为空：%s（stat=%v）", name, f, err)
			}
		}
		// .gxml 必须唯一：脚本按"目录里恰好一个 .gxml"定位页面。
		gxmls, err := filepath.Glob(filepath.Join(dir, "*.gxml"))
		if err != nil {
			t.Fatal(err)
		}
		if len(gxmls) != 1 {
			t.Fatalf("%s 应恰好有 1 个 .gxml，实际 %v", name, gxmls)
		}
	}
}
