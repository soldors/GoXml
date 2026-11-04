package cli

// R13-A BATCH-PRECOMMIT-CHECK 判别回归。
//
// 证明：有界分组把「每目标包一次 go build」降为「每 <=precommitBatchSize 包一次」，
// 同时保留既有失败语义（整批拒绝、不落产物/Manifest/缓存、--clean 未执行）、
// 诊断、同名 main 不冲突、跨包候选互引、全命中/混合命中、用户 overlay 合并优先级。
// 调用次数用 precommitBuildFunc 计数（执行计数，不靠耗时推断）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckBatchPreservesQuotedBuildFlags(t *testing.T) {
	root := pkgProject(t, 2)
	mod := filepath.Join(root, "go.mod")
	writeCLIFile(t, filepath.Join(root, "alternate module.mod"), readCLIFile(t, mod))
	writeCLIFile(t, filepath.Join(root, "alternate module.sum"), readCLIFile(t, filepath.Join(root, "go.sum")))
	t.Setenv("GOFLAGS", `-mod=readonly "-modfile=alternate module.mod"`)
	rc, _, stderr := runStandaloneCLI(t, root, "--check")
	if rc != ExitOK {
		t.Fatalf("quoted relative modfile failed: rc=%d stderr=%s", rc, stderr)
	}
}

func TestCheckBatchDoesNotUseCallerOutput(t *testing.T) {
	root := pkgProject(t, 2)
	t.Setenv("GOFLAGS", "-mod=readonly -o=caller-output")
	rc, _, stderr := runStandaloneCLI(t, root, "--check")
	if rc != ExitOK {
		t.Fatalf("batch must override caller -o: rc=%d stderr=%s", rc, stderr)
	}
	if _, err := os.Lstat(filepath.Join(root, "caller-output")); !os.IsNotExist(err) {
		t.Fatalf("unexpected caller output: %v", err)
	}
}

func TestCheckBatchStillLinksMain(t *testing.T) {
	root := pkgProject(t, 2)
	writeCLIFile(t, filepath.Join(root, "pkg01", "doc.go"), []byte(`package main
import _ "unsafe"
//go:linkname missing example.invalid.missingR13
func missing()
func main() { missing() }
`))
	rc, _, stderr := runStandaloneCLI(t, root, "--check")
	if rc != ExitFailure || !strings.Contains(stderr, "missingR13") || !strings.Contains(stderr, "relocation target") {
		t.Fatalf("must reject an unlinked main before publication: rc=%d stderr=%s", rc, stderr)
	}
	for _, path := range []string{"pkg00/page_gen.go", "pkg01/page_gen.go", ".goxml"} {
		if _, err := os.Lstat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("check failure must not publish %s: %v", path, err)
		}
	}
}

// withBuildCounter 替换 precommitBuildFunc 统计调用，返回计数指针（t.Cleanup 复原）。
func withBuildCounter(t *testing.T) (*int, *[]int) {
	t.Helper()
	count := 0
	sizes := []int{}
	orig := precommitBuildFunc
	precommitBuildFunc = func(goExe, workDir, overlayPath string, pkgs []string, outputFile string) error {
		count++
		sizes = append(sizes, len(pkgs))
		return orig(goExe, workDir, overlayPath, pkgs, outputFile)
	}
	t.Cleanup(func() { precommitBuildFunc = orig })
	return &count, &sizes
}

// pkgProject 建 n 个目标包，每个含 doc.go + page.gxml（生成 GoxmlPageView）。
func pkgProject(t *testing.T, n int) string {
	t.Helper()
	root := standaloneCLIProject(t)
	// standaloneCLIProject 自带根目录的 home.gxml/vm.go；删掉，只留本用例的 n 个目标包，
	// 使「目标包数」与 go build 覆盖的包数一一对应（便于断言调用次数）。
	_ = os.Remove(filepath.Join(root, "home.gxml"))
	for i := 0; i < n; i++ {
		d := "pkg" + twoDigitR13(i)
		mustWrite(t, filepath.Join(root, d, "doc.go"), "package "+d+"\n")
		writeCLIFileMkdir(t, filepath.Join(root, d, "page.gxml"),
			[]byte(`<Goxml Version="1"><Page Route="/"><Text Value="p`+twoDigitR13(i)+`"/></Page></Goxml>`))
	}
	return root
}

func twoDigitR13(i int) string {
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestCheckBatchReducesBuildCalls 多包 => 有界分组，调用数远小于包数。
func TestCheckBatchReducesBuildCalls(t *testing.T) {
	root := pkgProject(t, 10)
	count, sizes := withBuildCounter(t)
	code, stdout, stderr := runStandaloneCLI(t, root, "--check", "--no-cache")
	if code != ExitOK {
		t.Fatalf("10 包 --check 应成功: rc=%d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if *count != 1 {
		t.Fatalf("10 包（<=batchSize）应只调用 1 次 go build，实际 %d 次 %v", *count, *sizes)
	}
	if len(*sizes) != 1 || (*sizes)[0] != 10 {
		t.Fatalf("单次构建应覆盖 10 个包，实际 %v", *sizes)
	}
	// 10 个产物确实写出。
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(root, "pkg"+twoDigitR13(i), "page_gen.go")); err != nil {
			t.Fatalf("pkg%02d 产物缺失: %v", i, err)
		}
	}
}

// TestCheckBatchBoundedGroups 30 包 => ceil(30/batchSize) 次，且每次不超过上限。
func TestCheckBatchBoundedGroups(t *testing.T) {
	root := pkgProject(t, 30)
	count, sizes := withBuildCounter(t)
	code, _, stderr := runStandaloneCLI(t, root, "--check", "--no-cache")
	if code != ExitOK {
		t.Fatalf("30 包 --check 应成功: rc=%d\nstderr:%s", code, stderr)
	}
	want := (30 + precommitBatchSize - 1) / precommitBatchSize
	if *count != want {
		t.Fatalf("30 包应调用 %d 次 go build（batchSize=%d），实际 %d 次 %v", want, precommitBatchSize, *count, *sizes)
	}
	for _, s := range *sizes {
		if s > precommitBatchSize {
			t.Fatalf("单次构建包数 %d 超过上限 %d", s, precommitBatchSize)
		}
	}
}

// TestCheckBatchSinglePackage 单包：一次调用、产物正确。
func TestCheckBatchSinglePackage(t *testing.T) {
	root := pkgProject(t, 1)
	count, _ := withBuildCounter(t)
	code, _, stderr := runStandaloneCLI(t, root, "--check", "--no-cache")
	if code != ExitOK {
		t.Fatalf("单包 --check 应成功: rc=%d %s", code, stderr)
	}
	if *count != 1 {
		t.Fatalf("单包应调用 1 次，实际 %d", *count)
	}
}

// TestCheckBatchSameNamedMains 两个同 basename 的 main 包同批：不冲突、都通过。
func TestCheckBatchSameNamedMains(t *testing.T) {
	root := standaloneCLIProject(t)
	if err := os.Remove(filepath.Join(root, "home.gxml")); err != nil {
		t.Fatal(err)
	}
	// 两个不同父目录、同名子目录 x，各自 package main + 一个 .gxml。
	for _, parent := range []string{"d1", "d2"} {
		dir := filepath.Join(root, parent, "x")
		writeCLIFileMkdir(t, filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"))
		writeCLIFileMkdir(t, filepath.Join(dir, "page.gxml"),
			[]byte(`<Goxml Version="1"><Page Route="/"><Text Value="`+parent+`"/></Page></Goxml>`))
	}
	code, stdout, stderr := runStandaloneCLI(t, root, "--check", "--no-cache")
	if code != ExitOK {
		t.Fatalf("两个同名 main 同批应通过（不得输出冲突）: rc=%d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	for _, parent := range []string{"d1", "d2"} {
		if _, err := os.Stat(filepath.Join(root, parent, "x", "page_gen.go")); err != nil {
			t.Fatalf("%s/x 产物缺失: %v", parent, err)
		}
	}
	// 用户目录不得残留 go build 生成的可执行文件。
	for _, parent := range []string{"d1", "d2"} {
		entries, _ := os.ReadDir(filepath.Join(root, parent, "x"))
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".exe") {
				t.Fatalf("用户目录出现编译产物 %s", filepath.Join(parent, "x", e.Name()))
			}
		}
	}
	// An explicit build mode can require one main per invocation. Preserve
	// the old check in that case while still checking the entire candidate set.
	t.Setenv("GOFLAGS", "-mod=readonly -buildmode=exe")
	count, sizes := withBuildCounter(t)
	if rc, _, stderr := runStandaloneCLI(t, root, "--check", "--no-cache"); rc != ExitOK {
		t.Fatalf("explicit executable build mode failed: rc=%d stderr=%s", rc, stderr)
	}
	if *count != 2 || len(*sizes) != 2 || (*sizes)[0] != 1 || (*sizes)[1] != 1 {
		t.Fatalf("explicit executable mode must check one target at a time: %v", *sizes)
	}
}

// TestCheckBatchCrossCandidateImport 同批两包互引：v2 引用 v1 的生成类型，overlay 下通过。
func TestCheckBatchCrossCandidateImport(t *testing.T) {
	root := standaloneCLIProject(t)
	writeCLIFileMkdir(t, filepath.Join(root, "v1", "doc.go"), []byte("package v1\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "v1", "page.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/"><Text Value="V1"/></Page></Goxml>`))
	writeCLIFileMkdir(t, filepath.Join(root, "v2", "doc.go"), []byte("package v2\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "v2", "page.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/"><Text Value="V2"/></Page></Goxml>`))
	// v2 引用 v1 的生成类型（首次生成磁盘上还不存在，只能靠候选 overlay 可见）。
	writeCLIFileMkdir(t, filepath.Join(root, "v2", "use.go"),
		[]byte("package v2\n\nimport v1 \"github.com/goxml/goxml/v1\"\n\nvar _ v1.GoxmlPageView\n"))
	code, stdout, stderr := runStandaloneCLI(t, root, "--check", "--no-cache")
	if code != ExitOK {
		t.Fatalf("跨包互引候选应通过检查: rc=%d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
}

// TestCheckBatchAllHitsStillRuns 全命中批次仍执行检查（且仍是最少调用）。
func TestCheckBatchAllHitsStillRuns(t *testing.T) {
	root := pkgProject(t, 3)
	if code, _, stderr := runStandaloneCLI(t, root); code != ExitOK {
		t.Fatalf("首次生成应成功: %d %s", code, stderr)
	}
	count, _ := withBuildCounter(t)
	code, stdout, stderr := runStandaloneCLI(t, root, "--check")
	if code != ExitOK {
		t.Fatalf("全命中 --check 应通过: rc=%d %s", code, stderr)
	}
	if !strings.Contains(stdout, "缓存命中") {
		t.Fatalf("应为全命中: %s", stdout)
	}
	if !strings.Contains(stdout, "提交前检查通过") {
		t.Fatalf("全命中仍须执行检查: %s", stdout)
	}
	if *count != 1 {
		t.Fatalf("全命中 3 包应 1 次构建，实际 %d", *count)
	}
}

// TestCheckBatchMixedHitAndMiss 混合批次：命中也进候选 overlay，检查看到本批最终内容。
func TestCheckBatchMixedHitAndMiss(t *testing.T) {
	root := pkgProject(t, 2)
	if code, _, stderr := runStandaloneCLI(t, root); code != ExitOK {
		t.Fatalf("首次生成应成功: %d %s", code, stderr)
	}
	// 只改 pkg01 的 gxml => pkg00 命中、pkg01 未命中。
	writeCLIFileMkdir(t, filepath.Join(root, "pkg01", "page.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/"><Text Value="changed01"/></Page></Goxml>`))
	code, stdout, stderr := runStandaloneCLI(t, root, "--check")
	if code != ExitOK {
		t.Fatalf("混合命中 --check 应通过: rc=%d %s", code, stderr)
	}
	if !strings.Contains(stdout, "缓存命中") || !strings.Contains(stdout, "缓存未命中") {
		t.Fatalf("应为混合命中: %s", stdout)
	}
	if !strings.Contains(stdout, "提交前检查通过") {
		t.Fatalf("混合批次须执行检查: %s", stdout)
	}
}

// TestCheckBatchOneBadRejectsWholeBatch 一个坏目标 => 整批拒绝，不落产物/Manifest/缓存。
func TestCheckBatchOneBadRejectsWholeBatch(t *testing.T) {
	root := pkgProject(t, 4)
	// 让 pkg02 与生成类型冲突而编译失败（错误签名调用）。
	writeCLIFileMkdir(t, filepath.Join(root, "pkg02", "use.go"), []byte("package pkg02\n\nvar _ = NewGoxmlPageView(nil)\n"))

	code, stdout, stderr := runStandaloneCLI(t, root, "--check", "--no-cache")
	if code == ExitOK {
		t.Fatalf("坏目标应使整批拒绝，实际成功\nstdout:%s", stdout)
	}
	if !strings.Contains(stderr, "提交前检查失败") {
		t.Fatalf("应报提交前检查失败，实际: %s", stderr)
	}
	// 整批不落产物 / Manifest / 缓存。
	for i := 0; i < 4; i++ {
		if _, err := os.Stat(filepath.Join(root, "pkg"+twoDigitR13(i), "page_gen.go")); !os.IsNotExist(err) {
			t.Fatalf("pkg%02d 不应写入产物", i)
		}
	}
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Fatalf("不应创建 Manifest: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, ".goxml", "cache")); err == nil && len(entries) > 0 {
		t.Fatalf("不应写成功缓存记录: %d", len(entries))
	}
	if strings.Contains(stdout, "已生成") {
		t.Fatalf("失败时不得声称已生成: %s", stdout)
	}
}

// TestCheckBatchCleanNotRunOnFailure 检查失败时 --clean 不得执行删除（旧产物保留）。
func TestCheckBatchCleanNotRunOnFailure(t *testing.T) {
	root := pkgProject(t, 2)
	if code, _, stderr := runStandaloneCLI(t, root); code != ExitOK {
		t.Fatalf("首次生成应成功: %d %s", code, stderr)
	}
	// 造一个"孤儿"旧产物（Manifest 登记的旧源已不存在）+ 让 pkg01 编译失败。
	keep := filepath.Join(root, "pkg00", "page_gen.go")
	before := readCLIFile(t, keep)
	writeCLIFileMkdir(t, filepath.Join(root, "pkg01", "use.go"), []byte("package pkg01\n\nvar _ = NewGoxmlPageView(nil)\n"))

	code, _, stderr := runStandaloneCLI(t, root, "--check", "--clean", "--no-cache")
	if code == ExitOK {
		t.Fatalf("坏目标 + --clean 仍应失败: %s", stderr)
	}
	if got := readCLIFile(t, keep); string(got) != string(before) {
		t.Fatal("检查失败时 --clean 不得删除/改动既有产物")
	}
}

// TestCheckBatchUserOverlayPrecedence 用户 overlay 提供上下文，但候选产物优先于用户 overlay 同路径。
func TestCheckBatchUserOverlayPrecedence(t *testing.T) {
	root := checkFixture(t, map[string]string{"pkgx/source.go": "package pkgx\nvar Number int=\"bad\"\n"})
	mustWrite(t, filepath.Join(root, "replacement.go"), "package pkgx\nvar Number int=1\n")
	mustWrite(t, filepath.Join(root, "user overlay.json"),
		`{"Replace":{"pkgx/source.go":"replacement.go"}}`)
	orig := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", `-mod=readonly "-overlay=user overlay.json"`)
	t.Cleanup(func() { os.Setenv("GOFLAGS", orig) })

	// 用户 overlay 让 source.go 可用；候选 overlay 提供 gen.go。应通过。
	gen := filepath.Join(root, "pkgx", "gen.go")
	if err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{
		{dir: filepath.Join(root, "pkgx"), output: gen, content: []byte("package pkgx\nfunc Gen(){}\n")},
	}); err != nil {
		t.Fatalf("用户 overlay 合并后应通过: %v", err)
	}

	// 候选内容与用户 overlay 对同一路径冲突时，候选优先（用户 overlay 不能掩盖坏候选）。
	if err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{
		{dir: filepath.Join(root, "pkgx"), output: gen, content: []byte("package pkgx\nvar X int=\"badCandidate\"\n")},
	}); err == nil {
		t.Fatal("坏候选不应被用户 overlay 掩盖")
	}
}

// writeCLIFileMkdir 建父目录后写文件（standaloneCLIProject 的根已存在，新建子包目录需要）。
func writeCLIFileMkdir(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, path, data)
}
