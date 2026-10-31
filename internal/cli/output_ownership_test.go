package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// PRODUCT-R2 切片 2B：产物所有权与输出移动的一致性（R2-qcode-03）
//
// 缺陷形态（真实 CLI 实测，见 dev-log §2）：单文件形态 + `--out-dir` 会把输入的
// 相对路径压平（discoverInputs 对单文件只给 basename），于是两个目录里的同名页
// 映射到**同一个产物文件**：
//
//	goxml generate ui/home.gxml   --out-dir gen   → gen/home_gen.go（package ui）
//	goxml generate admin/home.gxml --out-dir gen   → gen/home_gen.go（同一个文件）
//
// 修之前：第二次运行报「已生成」、退出码 0 静默毁掉第一页，Manifest 里两条登记
// 指向同一个 output，来回生成都判 output-modified 互相覆盖（ping-pong），
// `--clean` 按 output 撤两条登记却只删一个文件。
//
// 同一批次内早有 seen 映射拦着（TestNamingConflictNoWrites 一类）；跨批次没人拦。
// 本文件的边界：
//   - 拦在**写盘之前**（阶段零 B），所以旧产物、Manifest、缓存都不动；
//   - 另一条登记的源**已删除**时放行——那是失效登记，合法接管，Manifest 只留一条；
//   - 目录形态（保留输入相对路径结构）本来就不冲突，必须继续一次生成多页成功。
//
// 不用"无条件绕过缓存"来修正确性：接管后的第三次运行仍应命中缓存（跳过）。
// ============================================================================

// countCacheRecords 数缓存目录里的记录文件（*.json，键形态合法）。
func countCacheRecords(t *testing.T) int {
	t.Helper()
	dir := b3CacheDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("读取缓存目录: %v", err)
	}
	n := 0
	for _, e := range entries {
		if cacheRecordNamePattern.MatchString(e.Name()) {
			n++
		}
	}
	return n
}

// mustManifest 读隔离的 Manifest（用例都经 tempProjectInRepo → isolateManifest）。
func mustManifest(t *testing.T, root string) *manifest {
	t.Helper()
	m, err := loadManifest(manifestPath(root))
	if err != nil {
		t.Fatalf("读取 Manifest: %v", err)
	}
	return m
}

// TestCrossBatchOutputOwnershipConflictStopsBeforeWrite 两个源抢同一个产物 →
// 第二次运行失败，且第一次的产物 / Manifest / 缓存分毫未动。
func TestCrossBatchOutputOwnershipConflictStopsBeforeWrite(t *testing.T) {
	root := tempProjectInRepo(t)
	gen := filepath.Join(root, "gen")
	writePageInPackage(t, filepath.Join(root, "ui"), "ui", "home")
	writePageInPackage(t, filepath.Join(root, "admin"), "admin", "home")

	// 第一次：ui/home.gxml → gen/home_gen.go，正常登记。
	code, out, errb := b3Run(t, filepath.Join(root, "ui", "home.gxml"), "--out-dir", gen)
	if code != ExitOK {
		t.Fatalf("首次生成应成功: 退出码 %d\nstdout:%s\nstderr:%s", code, out, errb)
	}
	product := filepath.Join(gen, "home_gen.go")
	first, err := os.ReadFile(product)
	if err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
	if !strings.Contains(string(first), "package ui\n") {
		t.Fatalf("首页产物包名不对:\n%.200s", first)
	}
	m := mustManifest(t, root)
	if len(m.Entries) != 1 {
		t.Fatalf("Manifest 应登记 1 条，实际: %+v", m.Entries)
	}
	if cacheN := countCacheRecords(t); cacheN != 1 {
		t.Fatalf("首次生成后应有 1 条缓存记录，实际 %d", cacheN)
	}

	// 第二次：另一个源映射到同一个产物 → 必须失败。
	code, out, errb = b3Run(t, filepath.Join(root, "admin", "home.gxml"), "--out-dir", gen)
	if code != ExitFailure {
		t.Fatalf("抢同一个产物的第二次运行必须失败: 退出码 %d\nstdout:%s\nstderr:%s", code, out, errb)
	}
	for _, want := range []string{"输出冲突", "gen/home_gen.go", "admin/home.gxml", "ui/home.gxml"} {
		if !strings.Contains(errb, want) {
			t.Errorf("stderr 应含 %q（要能指出是哪个产物、哪两个源）:\n%s", want, errb)
		}
	}

	// 「拦在写盘之前」必须是真的：旧产物字节不变。
	second, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Errorf("被拦下的运行改写了旧产物（前 %d 字节 / 后 %d 字节）", len(first), len(second))
	}
	if !strings.Contains(string(second), "package ui\n") {
		t.Errorf("旧产物内容被换掉了:\n%.200s", second)
	}
	// Manifest 仍是 1 条、仍属于 ui——不能出现两条登记指向同一个 output。
	m = mustManifest(t, root)
	if len(m.Entries) != 1 || !strings.HasSuffix(m.Entries[0].Source, "ui/home.gxml") {
		t.Errorf("Manifest 不应被失败的运行改动，实际: %+v", m.Entries)
	}
	// 缓存也不得为失败的运行多写一条记录。
	if cacheN := countCacheRecords(t); cacheN != 1 {
		t.Errorf("失败的运行不应写缓存记录，现有 %d 条", cacheN)
	}
	if residue := countTempResidue(t, root); len(residue) > 0 {
		t.Errorf("失败后残留临时文件: %v", residue)
	}
}

// TestOutputOwnershipTransfersWhenOldSourceDeleted 源删除后的接管是合法的：
// 旧登记属于已经不存在的源时放行，并且 Manifest 只留新主人这一条。
//
// 两头都要钉住：
//   - 拦太宽 → 删了源也没法把产物让给另一页（用户被自己的历史操作锁死）；
//   - 拦太松 / 不撤旧条目 → 两条登记指向同一个文件，`--clean` 撤两条却只删一个文件。
func TestOutputOwnershipTransfersWhenOldSourceDeleted(t *testing.T) {
	root := tempProjectInRepo(t)
	gen := filepath.Join(root, "gen")
	writePageInPackage(t, filepath.Join(root, "ui"), "ui", "home")
	writePageInPackage(t, filepath.Join(root, "admin"), "admin", "home")
	uiPage := filepath.Join(root, "ui", "home.gxml")

	if code, out, errb := b3Run(t, uiPage, "--out-dir", gen); code != ExitOK {
		t.Fatalf("首次生成应成功: %d\nstdout:%s\nstderr:%s", code, out, errb)
	}
	// 源被删除（页面改名或移除）：旧登记成了失效登记。
	if err := os.Remove(uiPage); err != nil {
		t.Fatal(err)
	}

	code, out, errb := b3Run(t, filepath.Join(root, "admin", "home.gxml"), "--out-dir", gen)
	if code != ExitOK {
		t.Fatalf("源已删除时应允许接管: 退出码 %d\nstdout:%s\nstderr:%s", code, out, errb)
	}
	product := filepath.Join(gen, "home_gen.go")
	data, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package admin\n") {
		t.Errorf("接管后产物应属于 admin 包:\n%.200s", data)
	}
	// Manifest 只有一个主人：旧条目必须被撤下，不能双重所有权。
	m := mustManifest(t, root)
	if len(m.Entries) != 1 {
		t.Fatalf("接管后 Manifest 应只剩 1 条，实际: %+v", m.Entries)
	}
	if !strings.HasSuffix(m.Entries[0].Source, "admin/home.gxml") {
		t.Errorf("Manifest 主人应为 admin/home.gxml，实际 %q", m.Entries[0].Source)
	}
	if !strings.HasSuffix(m.Entries[0].Output, "gen/home_gen.go") {
		t.Errorf("Manifest 产物路径不对，实际 %q", m.Entries[0].Output)
	}

	// 接管后紧接着再跑一次：必须走缓存命中（跳过），而不是每次重生成。
	// 这条否掉了「用无条件绕过缓存来保正确性」的实现。
	code, out, errb = b3Run(t, filepath.Join(root, "admin", "home.gxml"), "--out-dir", gen)
	if code != ExitOK {
		t.Fatalf("重复生成应成功: %d\nstderr:%s", code, errb)
	}
	if !strings.Contains(out, "跳过") {
		t.Errorf("第二次运行应命中缓存（stdout 应含「跳过」），实际:\n%s", out)
	}
}

// TestDirectoryFormKeepsPerSourceOutputs 目录形态保留输入相对路径结构：
// 同名不同目录的两页各自有产物，一次生成必须继续成功。
//
// 这是上面那条门禁的**反向对照**：拦的只是「两个源抢同一个文件」，
// 不是「产物文件名重了」。修得太宽会让最常见的多页工程无法一次生成。
func TestDirectoryFormKeepsPerSourceOutputs(t *testing.T) {
	root := tempProjectInRepo(t)
	gen := filepath.Join(root, "gen")
	writePageInPackage(t, filepath.Join(root, "ui"), "ui", "home")
	writePageInPackage(t, filepath.Join(root, "admin"), "admin", "home")

	code, out, errb := b3Run(t, root, "--out-dir", gen)
	if code != ExitOK {
		t.Fatalf("目录形态一次生成两页应成功: 退出码 %d\nstdout:%s\nstderr:%s", code, out, errb)
	}
	for _, want := range []struct{ path, pkg string }{
		{filepath.Join(gen, "ui", "home_gen.go"), "package ui\n"},
		{filepath.Join(gen, "admin", "home_gen.go"), "package admin\n"},
	} {
		data, err := os.ReadFile(want.path)
		if err != nil {
			t.Fatalf("产物 %s: %v", want.path, err)
		}
		if !strings.Contains(string(data), want.pkg) {
			t.Errorf("%s 包名不对（期望 %q）:\n%.200s", filepath.Base(want.path), want.pkg, data)
		}
	}
	m := mustManifest(t, root)
	if len(m.Entries) != 2 {
		t.Fatalf("两页应登记 2 条，实际: %+v", m.Entries)
	}
	seenOut := map[string]bool{}
	for _, e := range m.Entries {
		if seenOut[e.Output] {
			t.Errorf("Manifest 出现两条指向同一个产物 %q: %+v", e.Output, m.Entries)
		}
		seenOut[e.Output] = true
	}
}
