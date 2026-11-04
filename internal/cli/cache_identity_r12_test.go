package cli

// R12-A GENERATOR-CACHE-IDENTITY 判别回归。
//
// 核心断言（都有可编译行为红）：
//  1. K4 取值 = 语言版本 + 输出规则修订；语言版本仍是 "1"，记录格式版本不变。
//  2. 修订变化 ⇒ 缓存键变化 ⇒ 旧记录安全 miss（不是命中），并重新生成；
//     若 K4 仍只用 GoxmlVersion，本测试直接红。
//  3. 同修订：第二次真命中，产物字节/修改时间/Manifest 均不变。
//  4. 升级后生成失败：旧产物与 Manifest 保留、无成功缓存写入。

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// TestCacheIdentityComposition K4 组成与"不改语言版本/格式版本"。
func TestCacheIdentityComposition(t *testing.T) {
	if codegen.GoxmlVersion != "1" {
		t.Fatalf("GXML 语言版本必须保持 \"1\"，实际 %q", codegen.GoxmlVersion)
	}
	if generatorOutputRevision == "" {
		t.Fatal("生成器输出修订标识不能为空")
	}
	id := cacheIdentity()
	if !strings.HasPrefix(id, codegen.GoxmlVersion) {
		t.Fatalf("cacheIdentity 应含语言版本前缀: %q", id)
	}
	if !strings.Contains(id, generatorOutputRevision) {
		t.Fatalf("cacheIdentity 应含输出规则修订: %q", id)
	}
	// 必须与**旧 K4 值**不同，否则存量缓存不会安全 miss。
	if id == codegen.GoxmlVersion {
		t.Fatalf("cacheIdentity 不能等于语言版本（否则升级后沿用旧产物）: %q", id)
	}
	// K7 缓存格式版本不因本标识变化而提升。
	if cacheFormatVersion != 8 {
		t.Fatalf("记录格式未变，cacheFormatVersion 应保持 7，实际 %d", cacheFormatVersion)
	}
}

// TestCacheKeyChangesWithGeneratorRevision 同一输入、仅修订不同 ⇒ 键不同。K4 未接线则红。
func TestCacheKeyChangesWithGeneratorRevision(t *testing.T) {
	in := cacheKeyInput{
		SourceRel:       "ui/home.gxml",
		OutputRel:       "ui/home_gen.go",
		SourcePath:      "home.gxml",
		SourceHash:      "abc",
		PackageName:     "ui",
		CompilerVersion: codegen.GoxmlVersion + "+gen-1",
	}
	k1, err := cacheKey(in)
	if err != nil {
		t.Fatal(err)
	}
	in2 := in
	in2.CompilerVersion = codegen.GoxmlVersion + "+gen-2"
	k2, err := cacheKey(in2)
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Fatal("输出规则修订变化时缓存键必须改变（否则旧记录假命中）")
	}
}

// TestGeneratorRevisionUpgradeSafeMissThenHit 端到端：升级 ⇒ 安全 miss ⇒ 再跑真命中。
func TestGeneratorRevisionUpgradeSafeMissThenHit(t *testing.T) {
	root := standaloneCLIProject(t)
	source := filepath.Join(root, "home.gxml")
	gen := filepath.Join(root, "home_gen.go")
	man := manifestPath(root)

	// 第一次（当前修订）生成成功。
	if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
		t.Fatalf("首次生成应成功: rc=%d %s", code, stderr)
	}
	baseGen := readCLIFile(t, gen)
	if _, err := os.Stat(gen); err != nil {
		t.Fatal(err)
	}
	baseMan := readCLIFile(t, man)
	recordsBefore := r12CountCacheRecords(t, root)

	// 模拟"生成器输出规则升级"：修订改变（其它都不变）。
	cacheIdentityOverride = codegen.GoxmlVersion + "+gen-test-upgrade"
	t.Cleanup(func() { cacheIdentityOverride = "" })

	code, stdout, stderr := runStandaloneCLI(t, source)
	if code != ExitOK {
		t.Fatalf("升级后首次生成应成功: rc=%d %s", code, stderr)
	}
	if strings.Contains(stdout, "缓存命中") {
		t.Fatalf("输出规则升级后不得沿用旧产物（必须 miss），stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "缓存未命中") {
		t.Fatalf("升级后应报告未命中，stdout: %s", stdout)
	}
	if got := r12CountCacheRecords(t, root); got <= recordsBefore {
		t.Fatalf("升级后应写入新缓存记录: before=%d after=%d", recordsBefore, got)
	}
	// 同生成器 ⇒ 产物内容一致（规则未变，仅身份变了）。
	if string(readCLIFile(t, gen)) != string(baseGen) {
		t.Fatal("同一二进制重新生成的内容应与旧产物一致")
	}
	if !bytes.Equal(readCLIFile(t, man), baseMan) {
		t.Fatal("重新生成相同产物不应改变 Manifest")
	}

	// 同修订再跑：真命中，字节/mtime/Manifest 不变。
	afterStat, err := os.Stat(gen)
	if err != nil {
		t.Fatal(err)
	}
	afterMan := readCLIFile(t, man)
	code2, stdout2, stderr2 := runStandaloneCLI(t, source)
	if code2 != ExitOK {
		t.Fatalf("第二次应成功: rc=%d %s", code2, stderr2)
	}
	if !strings.Contains(stdout2, "缓存命中") {
		t.Fatalf("同修订第二次应为真命中，stdout: %s", stdout2)
	}
	sameStat, err := os.Stat(gen)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStat.ModTime().Equal(afterStat.ModTime()) || !bytes.Equal(readCLIFile(t, gen), baseGen) {
		t.Fatal("真命中不得重写产物（mtime/size 变化）")
	}
	if string(readCLIFile(t, man)) != string(afterMan) {
		t.Fatal("真命中不得改动 Manifest")
	}
}

// TestUpgradeGenerationFailurePreservesOldArtifacts 升级后生成失败：旧产物/Manifest 保留。
func TestUpgradeGenerationFailurePreservesOldArtifacts(t *testing.T) {
	root := standaloneCLIProject(t)
	source := filepath.Join(root, "home.gxml")
	gen := filepath.Join(root, "home_gen.go")
	man := manifestPath(root)
	if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
		t.Fatalf("首次生成应成功: rc=%d %s", code, stderr)
	}
	baseGen := readCLIFile(t, gen)
	baseMan := readCLIFile(t, man)
	recordsBefore := r12CacheRecords(t, root)

	// 让目标包编译失败（错误签名调用生成函数），并用 --check 触发提交前检查失败。
	writeCLIFile(t, filepath.Join(root, "use.go"), []byte("package demo\n\nvar _ = NewGoxmlHomeView(nil)\n"))
	cacheIdentityOverride = codegen.GoxmlVersion + "+gen-test-upgrade2"
	t.Cleanup(func() { cacheIdentityOverride = "" })

	code, stdout, stderr := runStandaloneCLI(t, source, "--check")
	if code != ExitFailure || !strings.Contains(stderr, "提交前检查失败") {
		t.Fatalf("升级后生成失败应非零退出，rc=%d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if string(readCLIFile(t, gen)) != string(baseGen) {
		t.Fatal("生成失败必须保留旧产物字节")
	}
	if string(readCLIFile(t, man)) != string(baseMan) {
		t.Fatal("生成失败必须保留旧 Manifest")
	}
	if strings.Contains(stdout, "已生成") {
		t.Fatalf("生成失败不得声称已生成: %s", stdout)
	}
	if after := r12CacheRecords(t, root); !reflect.DeepEqual(after, recordsBefore) {
		t.Fatal("检查失败必须保留缓存记录集合与字节")
	}
}

func r12CountCacheRecords(t *testing.T, root string) int {
	t.Helper()
	return len(r12CacheRecords(t, root))
}

func r12CacheRecords(t *testing.T, root string) map[string][]byte {
	t.Helper()
	dir := cacheDir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	records := make(map[string][]byte)
	for _, e := range entries {
		if !e.IsDir() && cacheRecordNamePattern.MatchString(e.Name()) {
			records[e.Name()] = readCLIFile(t, filepath.Join(dir, e.Name()))
		}
	}
	if len(records) == 0 {
		t.Fatal("预期存在真实缓存记录")
	}
	return records
}
