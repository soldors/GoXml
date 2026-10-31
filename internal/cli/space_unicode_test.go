package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// PRODUCT-R2 切片 3：路径里的空格与中文，以及失败后重跑的可定位性
//
// 先说清边界（任务书：不扩大 junction/硬链接/TOCTOU 保证，不新加退出码/选项/
// 诊断编号）。本轮实测把「Unicode 工程」拆成两种**结果完全不同**的形态：
//
//   A. 模块根路径、页面**文件名**含空格与中文 → 合法 Go 工程，必须全流程可用：
//      发现、生成、Manifest 的 `/` 分隔相对路径、缓存命中、`--clean`。
//      （真实 CLI 与 `go build ./...` rc=0 的证据在交付目录 raw/。）
//   B. **包目录名**含空格或中文 → Go 自己就拒绝该 import path
//      （`malformed import path …: invalid char ' '`），`go build ./...` 报同一句。
//      这一类不是 goxml 的缺陷，但**必须**在写盘前失败、诊断指名是哪个输入文件，
//      且同一工程重复运行的诊断逐字节一致——否则用户只能对着 `:1:20:` 猜。
//
// 另有 R2-qcode-04：`已生成` 一行此前打绝对路径，而同轮的 `跳过 <rel>`、
// `缓存未命中 <rel>：<原因>` 都打项目相对路径（任务卡 §5 冻结 `已生成 <rel-output>`）。
// 绝对路径把用户机器目录结构写进日志，路径含空格时脚本更难比对。
// ============================================================================

// tempModuleInSpaceyDir 在临时区建一个**路径含空格与中文**的独立 module。
//
// 不能复用 tempProjectInRepo：那条路径挂在仓库 go.mod 之下，模块根与包目录
// 都是 ASCII，测不到 A 形态；而把中文塞进仓库下的包目录会变成 B 形态
// （Go 拒绝该 import path）。所以这里连 go.mod 一起造，moduleRoot 就是临时目录，
// Manifest 与缓存天然落在里面，不碰仓库的 .goxml/。
func tempModuleInSpaceyDir(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "r2 工程甲")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module r2uni\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestGenerateLineUsesRelativeOutput 成功行必须是 `已生成 <rel-output>`。
//
// 判别力：绝对路径形态会同时命中「含 root」这一条。
func TestGenerateLineUsesRelativeOutput(t *testing.T) {
	root := tempModuleInSpaceyDir(t)
	writePageInPackage(t, filepath.Join(root, "ui"), "ui", "home")

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("退出码 = %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "已生成 ui/home_gen.go") {
		t.Fatalf("应打印 `已生成 <rel-output>`（任务卡 §5 冻结措辞），实际:\n%s", out.String())
	}
	if strings.Contains(out.String(), root) {
		t.Errorf("stdout 不应泄漏项目绝对路径 %q:\n%s", root, out.String())
	}
}

// TestGenerateInSpaceAndUnicodePath A 形态的正例：空格 + 中文一路走通。
func TestGenerateInSpaceAndUnicodePath(t *testing.T) {
	root := tempModuleInSpaceyDir(t)
	ui := filepath.Join(root, "ui")
	// 创建顺序故意与排序顺序**相反**：中文文件先落盘。
	writePageInPackage(t, ui, "ui", "深度页面")
	writePageInPackage(t, ui, "ui", "home page")

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("空格/中文路径应能生成: 退出码 %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}

	// 产物名沿用源名 + _gen.go（契约 §3.2 命名规则），空格与中文都保留。
	for _, name := range []string{"home page_gen.go", "深度页面_gen.go"} {
		data, err := os.ReadFile(filepath.Join(ui, name))
		if err != nil {
			t.Fatalf("产物 %s: %v", name, err)
		}
		if !strings.Contains(string(data), "package ui\n") {
			t.Errorf("产物 %s 包名不对:\n%.160s", name, data)
		}
	}

	// 输出行按路径排序，且用相对路径（`/` 分隔）。
	lines := successLines(out.String(), "已生成 ")
	want := []string{"ui/home page_gen.go", "ui/深度页面_gen.go"} // 'h'(0x68) < 深(UTF-8 E6…)
	if len(lines) != len(want) {
		t.Fatalf("输出行 = %v，期望 %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("第 %d 行 = %q，期望 %q（排序顺序必须由路径决定，与创建顺序无关）", i, lines[i], want[i])
		}
	}

	// Manifest：项目相对路径、`/` 分隔、中文与空格原样保留（契约 §10）。
	man, err := os.ReadFile(filepath.Join(root, manifestDir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(man), "\\") {
		t.Errorf("Manifest 不得出现反斜杠路径:\n%s", man)
	}
	for _, wantStr := range []string{"ui/home page.gxml", "ui/深度页面.gxml", "ui/home page_gen.go", "ui/深度页面_gen.go"} {
		if !strings.Contains(string(man), wantStr) {
			t.Errorf("Manifest 应含 %q，实际:\n%s", wantStr, man)
		}
	}

	// 第二轮：两页都命中，产物字节不变（命中不写文件由 mtime 用例在
	// cache_wire_test.go 里钉住，这里只断言「跳过」与内容一致）。
	first := map[string][]byte{}
	for _, name := range []string{"home page_gen.go", "深度页面_gen.go"} {
		first[name], _ = os.ReadFile(filepath.Join(ui, name))
	}
	out.Reset()
	errb.Reset()
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("第二轮应成功: %d\n%s", code, errb.String())
	}
	if n := strings.Count(out.String(), "跳过 "); n != 2 {
		t.Fatalf("第二轮应命中 2 页，实际:\n%s", out.String())
	}
	if strings.Contains(out.String(), "已生成") {
		t.Errorf("命中轮不应出现「已生成」:\n%s", out.String())
	}
	for _, name := range []string{"home page_gen.go", "深度页面_gen.go"} {
		again, err := os.ReadFile(filepath.Join(ui, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first[name], again) {
			t.Errorf("命中轮改写了 %s", name)
		}
	}
}

// TestGenerateFailsBeforeWriteOnNonASCIIPackageDir B 形态：包目录名带空格/中文时
// Go 本身不接受该 import path，CLI 必须在写盘前失败并**指名输入文件**。
//
// 这条不测「为什么失败」（那是 Go 的规则），只测 goxml 的失败姿态：
// 退出码非零、诊断里有那个带空格/中文的源路径、磁盘零痕迹、重跑诊断逐字节一致。
func TestGenerateFailsBeforeWriteOnNonASCIIPackageDir(t *testing.T) {
	for _, dirName := range []string{"页面 一", "admin panel"} {
		t.Run(dirName, func(t *testing.T) {
			root := tempModuleInSpaceyDir(t)
			writePageInPackage(t, filepath.Join(root, dirName), "ui", "home")

			var out1, errb1 bytes.Buffer
			code1 := Run([]string{"generate", root}, &out1, &errb1)
			if code1 != ExitFailure {
				t.Fatalf("退出码 = %d，期望 %d\nstdout:\n%s", code1, ExitFailure, out1.String())
			}
			// 可定位性：必须点出是哪个输入坏了（文件名里就带空格/中文）。
			if !strings.Contains(errb1.String(), filepath.ToSlash(filepath.Join(dirName, "home.gxml"))) {
				t.Errorf("诊断应指名坏掉的输入文件 %q，实际:\n%s", dirName, errb1.String())
			}
			if !strings.Contains(errb1.String(), "failed to load") {
				t.Errorf("诊断应说明是包加载失败，实际:\n%s", errb1.String())
			}
			// 「磁盘无任何变更」必须是真的。
			if _, err := os.Stat(filepath.Join(root, dirName, "home_gen.go")); !os.IsNotExist(err) {
				t.Errorf("失败时不得留下产物: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, manifestDir)); !os.IsNotExist(err) {
				t.Errorf("失败时不得创建 .goxml 目录: %v", err)
			}
			// 重跑：诊断逐字节一致（否则同一工程的报错无法被脚本或人复现）。
			var out2, errb2 bytes.Buffer
			code2 := Run([]string{"generate", root}, &out2, &errb2)
			if code2 != code1 {
				t.Fatalf("重跑退出码应一致: %d vs %d", code2, code1)
			}
			if errb1.String() != errb2.String() {
				t.Errorf("重跑诊断不一致:\n--- 首轮 ---\n%s\n--- 次轮 ---\n%s", errb1.String(), errb2.String())
			}
		})
	}
}

// TestFailedRerunDiagnosticsAreStableAndRecoverable 多文件批次：坏页在中间时
// 两次的字节级诊断必须一致；修掉坏页后同一命令恢复成功，产物与另建工程的
// 同输入产物逐字节相同（失败不留下半截状态，也不影响后续确定性）。
func TestFailedRerunDiagnosticsAreStableAndRecoverable(t *testing.T) {
	root := tempProjectInRepo(t)
	writePageInPackage(t, root, "ui", "alpha page")
	writePageInPackage(t, root, "ui", "zeta page")
	writeGoFile(t, root, "mid bad.gxml", `<Goxml Version="1" ViewModel="VM">
  <Page Route="/mid"><Text Value="{Missing}"/></Page>
</Goxml>`)

	var errb1, errb2 bytes.Buffer
	code1 := Run([]string{"generate", root}, &bytes.Buffer{}, &errb1)
	code2 := Run([]string{"generate", root}, &bytes.Buffer{}, &errb2)
	if code1 != ExitFailure || code2 != ExitFailure {
		t.Fatalf("两轮都应失败: %d / %d", code1, code2)
	}
	if errb1.String() != errb2.String() {
		t.Errorf("失败诊断必须逐字节可复现:\n--- 1 ---\n%s\n--- 2 ---\n%s", errb1.String(), errb2.String())
	}
	// 指名到那个**文件名带空格**的输入，且只指名排序在前的那一条。
	if !strings.Contains(errb1.String(), "mid bad.gxml:") {
		t.Errorf("诊断应带 文件名:行号，实际:\n%s", errb1.String())
	}
	for _, name := range []string{"alpha page_gen.go", "zeta page_gen.go"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("失败批次不得留下产物 %s: %v", name, err)
		}
	}

	// 修好坏页（把绑定改对）→ 同一命令恢复，三页都出。
	writeGoFile(t, root, "mid bad.gxml", `<Goxml Version="1" ViewModel="VM">
  <Page Route="/mid"><Text Value="{Title}"/></Page>
</Goxml>`)
	var out, errb bytes.Buffer
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("修复后重跑应成功: 退出码 %d\nstderr:\n%s", code, errb.String())
	}
	repaired := map[string][]byte{}
	for _, name := range []string{"alpha page_gen.go", "mid bad_gen.go", "zeta page_gen.go"} {
		repaired[name], _ = os.ReadFile(filepath.Join(root, name))
	}

	// 对照：同一批输入、**清空 Manifest 与缓存后**重跑，产物必须逐字节相同。
	// 这直接证明「失败的那几轮」没有留下任何影响后续确定性的状态——
	// 而不是只证明第二次成功。
	for _, name := range []string{"alpha page_gen.go", "mid bad_gen.go", "zeta page_gen.go"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Dir(manifestPath(root))); err != nil {
		t.Fatal(err) // isolateManifest 的重定向目录，整体清空即回到首次生成形态
	}
	var out3, errb3 bytes.Buffer
	if code := Run([]string{"generate", root}, &out3, &errb3); code != ExitOK {
		t.Fatalf("清空后重跑应成功: 退出码 %d\nstderr:\n%s", code, errb3.String())
	}
	if strings.Contains(out3.String(), "跳过") {
		t.Errorf("Manifest 与缓存都清空后不应有命中:\n%s", out3.String())
	}
	for _, name := range []string{"alpha page_gen.go", "mid bad_gen.go", "zeta page_gen.go"} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("清空重跑后缺产物 %s: %v", name, err)
		}
		if !bytes.Equal(repaired[name], got) {
			t.Errorf("%s 在「失败后重跑」与「清空重跑」两份结果里字节不同（%d vs %d 字节）",
				name, len(repaired[name]), len(got))
		}
	}
}

// successLines 取 stdout 里所有以 prefix 开头的行的其余部分（保序）。
func successLines(out, prefix string) []string {
	var got []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			got = append(got, strings.TrimSpace(rest))
		}
	}
	return got
}
