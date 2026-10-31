package cli

import (
	"bytes"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// 本文件把 packageDirName 与**真实 go 工具**对同一目录、同一 GOFLAGS 的包选择
// 做正负对照：go 自己会消费 GOFLAGS 里的 -tags 做文件选择，所以 `go list` 报的
// 包名就是权威答案；CLI 必须与之一致。非法 GOFLAGS 必须 fail-closed。
//
// 与交付目录 raw/ 里那条 `go build ./...` 全量构建证据互补：这里用无外部依赖
// 的最小 fixture 走 `go list`（不受产物 import gioui 能否离线解析的影响），
// 把「包名判定与 go 的 -tags 选择一致」这一条钉死。

// goBinary 定位 go 可执行文件：优先 PATH，退回 GOROOT/bin。
func goBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	cand := filepath.Join(build.Default.GOROOT, "bin", "go")
	if _, err := os.Stat(cand); err == nil {
		return cand
	}
	if exe := cand + ".exe"; fileExists(exe) {
		return exe
	}
	t.Skipf("找不到 go 可执行文件（PATH 与 GOROOT=%s 均无）", build.Default.GOROOT)
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// goEnvFor 构造 go list 子进程的环境：先剔除会与我显式设置的值冲突的键，
// 再追加本用例指定的 GOFLAGS 等。若不剔除，测试进程里已有的 GOFLAGS（如
// CI 的 -mod=readonly）可能与显式 -tags 重复，Windows 下取哪一份不确定——
// 一旦取到 -mod=readonly，子进程就丢了 -tags，对照物本身失真。
func goEnvFor(goflags string) []string {
	drop := map[string]bool{"GOFLAGS": true, "GOPROXY": true, "GOWORK": true, "GO111MODULE": true}
	out := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 && drop[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	return append(out,
		"GOFLAGS="+goflags, "GOPROXY=off", "GOWORK=off", "GO111MODULE=on")
}

// goListPackage 用真实 go list 在独立 module 里报告 dir 的包名与加载状态。
// goflags 作为子进程的 GOFLAGS 传入，go 据此做 -tags 感知的文件选择。
// 返回 (name, incomplete)：incomplete=true 表示该目录在 go 里加载即报错
// （如多 package 冲突、非法构建约束），对应 listStruct 的 .Incomplete 字段。
func goListPackage(t *testing.T, moduleRoot, dir, goflags string) (name string, incomplete bool) {
	t.Helper()
	gobin := goBinary(t)
	rel, err := filepath.Rel(moduleRoot, dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(gobin, "list", "-e", "-f", "{{.Name}}\t{{.Incomplete}}", "./"+filepath.ToSlash(rel))
	cmd.Dir = moduleRoot
	cmd.Env = goEnvFor(goflags)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list 执行失败: %v\nstdout:%s\nstderr:%s", err, out.String(), errb.String())
	}
	line := strings.TrimRight(out.String(), "\r\n")
	// 多 package 目录在 -e 下可能输出多行；取首行判断加载是否完整。
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	name = line
	inc := false
	if j := strings.Index(line, "\t"); j >= 0 {
		name = line[:j]
		inc = strings.TrimSpace(line[j+1:]) == "true"
	}
	return strings.TrimSpace(name), inc
}

// newScratchModule 建一个 t.TempDir 下的独立 module（与仓库 go.mod 隔离），返回根目录。
func newScratchModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module scratch\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// writePairFixture 在 dir 写两枚**互斥**的构建标签文件：
//   - pair_base.go: //go:build !goxmlr3 → package pkgbase
//   - pair_r3.go:   //go:build goxmlr3 → package pkgtag
//
// 这枚 fixture 在 Go 里始终合法（任一构建配置下都只有一个 package）：
// 无 tag 时激活 pkgbase，带 goxmlr3 时激活 pkgtag。正因为合法，修复前
// CLI 用 build.Default（不读 GOFLAGS）恒得 pkgbase，与 go 在 -tags 下得到的
// pkgtag 分叉——产物会写成 package pkgbase，而真实包是 pkgtag → 提交后 go build
// 报「found packages pkgbase 和 pkgtag」。这就是被修的那个 bug 的合法反例。
func writePairFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, dir, "pair_base.go", "//go:build !goxmlr3\n\npackage pkgbase\n\ntype Base struct{ Title string }\n")
	writeGoFile(t, dir, "pair_r3.go", "//go:build goxmlr3\n\npackage pkgtag\n\ntype Tag struct{ Title string }\n")
}

// packageDirNameLegacy 复刻修复**前**的选文件循环（build.Default.MatchFile、
// 不读 GOFLAGS、不排除工具自有产物）。仅用于「正负对照」里的负例基线，
// 证明旧实现确实会得出与 go 不同的包名。
func packageDirNameLegacy(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var name string
	seen := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || filepath.Ext(n) != ".go" || strings.HasSuffix(n, "_test.go") {
			continue
		}
		match, err := build.Default.MatchFile(dir, n)
		if err != nil || !match {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		pkg, ok := parsePackageName(data)
		if !ok || seen[pkg] {
			continue
		}
		seen[pkg] = true
		if name == "" {
			name = pkg
		}
	}
	if name == "" {
		return "", fmt.Errorf("legacy: 无可定名文件")
	}
	return name, nil
}

// TestPackageDirNameMatchesRealGoListUnderTag 是核心回归：CLI 算出的包名
// 必须等于同一 GOFLAGS 下真实 go list 的包名（正例）。
func TestPackageDirNameMatchesRealGoListUnderTag(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "p")
	writePairFixture(t, dir)

	cases := []struct {
		goflags string
		want    string
	}{
		{"", "pkgbase"},             // 无 tag：激活 !goxmlr3 的 pkgbase
		{"-tags=goxmlr3", "pkgtag"}, // 带 tag：激活 goxmlr3 的 pkgtag
	}
	for _, c := range cases {
		authName, inc := goListPackage(t, root, dir, c.goflags)
		if inc {
			t.Fatalf("合法 fixture 在 goflags=%q 下 go list 不应报错，实际 Incomplete=true", c.goflags)
		}
		if authName != c.want {
			t.Fatalf("真实 go list 在 goflags=%q 下报 %q，期望 %q（对照物本身要成立）", c.goflags, authName, c.want)
		}
		t.Setenv("GOFLAGS", c.goflags)
		got, err := packageDirName(dir)
		if err != nil {
			t.Fatalf("packageDirName(goflags=%q) 报错: %v", c.goflags, err)
		}
		if got != authName {
			t.Errorf("CLI 包名 %q 与真实 go list %q 不一致（goflags=%q）", got, authName, c.goflags)
		}
	}
}

// TestPackageDirNameLegacyDivergesFromRealGoList 是负例：旧实现（build.Default）
// 在 -tags 下恒得 pkgbase，而 go list 得 pkgtag——把「修复前的真实错误行为」
// 固化成断言，避免回归被悄悄改回。
func TestPackageDirNameLegacyDivergesFromRealGoList(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "p")
	writePairFixture(t, dir)

	const goflags = "-tags=goxmlr3"
	authName, _ := goListPackage(t, root, dir, goflags)
	legacy, err := packageDirNameLegacy(dir) // 旧路径不看 GOFLAGS
	if err != nil {
		t.Fatal(err)
	}
	if authName != "pkgtag" {
		t.Fatalf("对照前提：go list 应得 pkgtag，实际 %q", authName)
	}
	if legacy != "pkgbase" {
		t.Fatalf("前提：旧实现应恒得 pkgbase，实际 %q（若变说明 fixture 前提漂移）", legacy)
	}
	if legacy == authName {
		t.Fatalf("旧实现本应与 go list 分叉，实际都 = %q（fixture 判别力丢失）", legacy)
	}

	// 新实现消除该分叉。
	t.Setenv("GOFLAGS", goflags)
	if got, err := packageDirName(dir); err != nil || got != authName {
		t.Fatalf("新实现应与 go list 一致：got=%q err=%v want=%q", got, err, authName)
	}
}

// TestPackageDirNameInvalidGOFLAGSFailsClosed 非法 GOFLAGS 必须报错，
// 而不是静默退回 build.Default 蒙混过关。
func TestPackageDirNameInvalidGOFLAGSFailsClosed(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, dir, "a.go", "package solo\n")

	for _, bad := range []string{"garbage", "-", "--", "---x", "-=v", "--=v", `"unterminated`, "-tags"} {
		t.Setenv("GOFLAGS", bad)
		got, err := packageDirName(dir)
		if err == nil {
			t.Errorf("非法 GOFLAGS %q 应报错，实际得包名 %q", bad, got)
			continue
		}
		if !strings.Contains(err.Error(), "GOFLAGS") {
			t.Errorf("非法 GOFLAGS %q 的错误应指向 $GOFLAGS，实际: %v", bad, err)
		}
	}
}

// TestTwoRealPackagesRejectedAndGoListAgrees 说明「两个真实生产包」这一拒绝
// 与 go 本身一致——那是 fixture 在 Go 里就非法，不是 CLI 回归（任务明确要分开）。
func TestTwoRealPackagesRejectedAndGoListAgrees(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, dir, "a_one.go", "package one\n")
	writeGoFile(t, dir, "b_two.go", "package two\n")

	t.Setenv("GOFLAGS", "")
	_, err := packageDirName(dir)
	if err == nil {
		t.Fatalf("两个真实生产包必须被拒")
	}
	authName, inc := goListPackage(t, root, dir, "")
	if !inc {
		t.Fatalf("对照前提：go list 也应对两包目录报错，实际 name=%q Incomplete=false（说明 fixture 前提漂移）", authName)
	}
}

// TestStaleGeneratedArtifactDoesNotBlockRename 证明排除工具自有产物能打破死锁：
// 目录里有正确的真实源文件（-tags 激活 pkgtag）+ 一枚带保护头但包名错误的旧
// 产物；定名应跳过该产物返回 pkgtag，用户才能重新生成把产物纠正过来。
// 注意这里 go list 会因那枚错误产物报错（go 不排除生成文件），所以本例断言的是
// 「CLI 不被自己的旧产物卡死」，而不是「与 go list 相等」——二者适用条件不同。
func TestStaleGeneratedArtifactDoesNotBlockRename(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "p")
	writePairFixture(t, dir)
	stale := "// Code generated by goxml. DO NOT EDIT.\n// Source: home.gxml\n// Goxml-Version: " +
		codegen.GoxmlVersion + "\n\npackage stalepkg\n"
	writeGoFile(t, dir, "home_gen.go", stale)
	// The header alone does not establish tool ownership.
	if err := saveManifest(manifestPath(root), &manifest{Version: manifestVersion, Entries: []entry{{Source: "p/home.gxml", Output: "p/home_gen.go"}}}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GOFLAGS", "-tags=goxmlr3")
	got, err := packageDirName(dir)
	if err != nil {
		t.Fatalf("存在带保护头的旧错误产物时不应被卡死，实际报错: %v", err)
	}
	if got != "pkgtag" {
		t.Errorf("应跳过工具自有产物、按激活的真实源定名 = pkgtag，实际 %q", got)
	}

	// 对照：若那枚错误文件是**人手写的真实源**（无保护头），两包仍必须被拒。
	if err := os.WriteFile(filepath.Join(dir, "home_gen.go"),
		[]byte("package stalepkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := packageDirName(dir); err == nil {
		t.Errorf("去掉保护头后它就是第二个真实生产包，必须被拒")
	}
}

// TestGenerateTaggedPackageMatchesGoList 端到端：真实 CLI 在不同 -tags 下
// 把同一页生成进**随标签切换而激活不同包名**的目录，产物的 package 必须等于
// 该标签下真实 go list 选的包名。修复前 build.Default 恒得 pkgbase，带 tag 时
// 与 go 激活的 pkgtag 冲突（go build 报 found packages）——这里用 go list 权威
// 对照 + 产物包名把该分叉钉死。
//
// 用独立 scratch module（非仓库 testdata）：源文件零外部 import，go list 干净；
// 生成只做发射与源码级 ViewModel 解析，不在测试里 go build 产物（产物 import
// gioui，离线解析留给交付目录 raw/ 的全量构建证据）。
func TestGenerateTaggedPackageMatchesGoList(t *testing.T) {
	root := newScratchModule(t)
	pkgDir := filepath.Join(root, "ui")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 两分支互斥、各自声明一个包与一个 VM，任一构建配置下恰好激活其一。
	writeGoFile(t, pkgDir, "pair_base.go", "//go:build !goxmlr3\n\npackage pkgbase\n\ntype VM struct{ Title string }\n")
	writeGoFile(t, pkgDir, "pair_r3.go", "//go:build goxmlr3\n\npackage pkgtag\n\ntype VM struct{ Title string }\n")
	gxml := `<Goxml Version="1" ViewModel="VM"><Page Route="/home"><Text Value="{Title}"/></Page></Goxml>`
	gxmlPath := filepath.Join(pkgDir, "home.gxml")

	for _, c := range []struct {
		goflags string
		want    string
	}{
		{"", "pkgbase"},
		{"-tags=goxmlr3", "pkgtag"},
	} {
		// 清掉上一轮产物，让 go list 的「源目录权威包名」不含生成文件。
		_ = os.Remove(filepath.Join(pkgDir, "home_gen.go"))
		if err := os.WriteFile(gxmlPath, []byte(gxml), 0o644); err != nil {
			t.Fatal(err)
		}
		authority, inc := goListPackage(t, root, pkgDir, c.goflags)
		if inc {
			t.Fatalf("goflags=%q 源目录本应单一包，go list 却 Incomplete=true", c.goflags)
		}
		if authority != c.want {
			t.Fatalf("goflags=%q go list 权威名=%q，期望 %q", c.goflags, authority, c.want)
		}

		t.Setenv("GOFLAGS", c.goflags)
		var out, errb bytes.Buffer
		code := Run([]string{"generate", pkgDir}, &out, &errb)
		if code != ExitOK {
			t.Fatalf("goflags=%q 生成失败: 退出码 %d\nstdout:%s\nstderr:%s", c.goflags, code, out.String(), errb.String())
		}
		gotPkg := productPackage(t, filepath.Join(pkgDir, "home_gen.go"))
		if gotPkg != authority {
			t.Errorf("goflags=%q 产物包名 %q 与真实 go list %q 不一致（正是要修的 bug）", c.goflags, gotPkg, authority)
		}
	}
}
