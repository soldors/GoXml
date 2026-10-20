package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// ============================================================================
// TASK-GENERATE-CACHE-001 切片 B3：命中判定、失效原因、跳过接线
//
// 证据标准（任务卡 §8）：「缓存命中真的没跑生成」必须由**执行计数**证明，
// 因此这里通过 pipelineHooks.Generate 统计 Codegen 调用次数，
// 而不是拿耗时下降来推断。
//
// 变红方式（已实测）：
//   - 去掉命中分支（永远当 miss）→ TestCacheHitSecondRunSkipsAndSkipsCodegen 红；
//   - 去掉保护头检查 → TestCacheHitUserOwnedIsError 红；
//   - 排除集漏掉 batchTargets → TestCacheMultiInputSamePackageHitsTwice 红；
//   - 命中时跳过写盘判断失效 → mtime 断言红。
// ============================================================================

// b3Project 建一个夹具工程（Manifest 与缓存均已隔离）。
func b3Project(t *testing.T) string {
	t.Helper()
	root := tempProjectInRepo(t)
	copyFixture(t, root)
	return root
}

// b3Run 执行一次 generate。
func b3Run(t *testing.T, args ...string) (code int, out, errs string) {
	t.Helper()
	var o, e bytes.Buffer
	code = Run(append([]string{"generate"}, args...), &o, &e)
	return code, o.String(), e.String()
}

// b3CountCodegen 注入 Codegen 计数钩子，返回计数指针。
//
// 钩子里调用真实的 codegen.Generate：pipeline 的默认实现也是它
// （pipeline.go:152-155），所以计数与真实路径等价。
func b3CountCodegen(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := pipelineHooks
	pipelineHooks = pipeline.Hooks{
		Generate: func(doc *analyzer.VerifiedDoc, opts codegen.Options) ([]byte, error) {
			n++
			return codegen.Generate(doc, opts)
		},
	}
	t.Cleanup(func() { pipelineHooks = orig })
	return &n
}

// b3CacheDir 返回本用例的缓存目录（跟随被隔离的 Manifest）。
func b3CacheDir(t *testing.T) string {
	t.Helper()
	if manifestPathOverride == "" {
		t.Fatal("Manifest 未隔离：cacheDir 会指到仓库根的共享缓存")
	}
	return filepath.Join(filepath.Dir(manifestPathOverride), cacheDirName)
}

// b3CacheFile 返回缓存目录里唯一的记录文件路径。
func b3CacheFile(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(b3CacheDir(t))
	if err != nil {
		t.Fatalf("读取缓存目录: %v", err)
	}
	var files []string
	for _, e := range entries {
		if cacheRecordNamePattern.MatchString(e.Name()) {
			files = append(files, filepath.Join(b3CacheDir(t), e.Name()))
		}
	}
	if len(files) != 1 {
		t.Fatalf("期望 1 个缓存记录，实际 %d: %v", len(files), files)
	}
	return files[0]
}

// TestCacheHitSecondRunSkipsAndSkipsCodegen：第二次运行命中，且 Codegen 计数不增。
func TestCacheHitSecondRunSkipsAndSkipsCodegen(t *testing.T) {
	root := b3Project(t)
	calls := b3CountCodegen(t)
	src := filepath.Join(root, "home.gxml")
	gen := filepath.Join(root, "home_gen.go")

	code, out1, errs1 := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("首次生成失败: code=%d stderr=%s", code, errs1)
	}
	if *calls != 1 {
		t.Fatalf("首次生成 Codegen 调用次数 = %d, 期望 1", *calls)
	}
	if !strings.Contains(out1, "已生成") || strings.Contains(out1, "跳过") {
		t.Fatalf("首次输出应只有「已生成」:\n%s", out1)
	}

	before, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	statBefore, err := os.Stat(gen)
	if err != nil {
		t.Fatal(err)
	}

	code, out2, errs2 := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("第二次生成失败: code=%d stderr=%s", code, errs2)
	}
	if *calls != 1 {
		t.Fatalf("命中时 Codegen 仍被调用：次数=%d（命中必须完全不跑生成）", *calls)
	}
	if !strings.Contains(out2, "跳过") || !strings.Contains(out2, "缓存命中") {
		t.Fatalf("命中应输出「跳过 …（缓存命中）」:\n%s", out2)
	}
	if !strings.Contains(out2, "命中 1，生成 0") {
		t.Fatalf("缺少命中汇总行:\n%s", out2)
	}

	after, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("命中时产物内容被改动（契约 §13 第 12 条）")
	}
	statAfter, err := os.Stat(gen)
	if err != nil {
		t.Fatal(err)
	}
	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Fatalf("命中时产物 mtime 被改动: %v → %v", statBefore.ModTime(), statAfter.ModTime())
	}
}

// TestCacheHitOutputMissingRegenerates：产物被删 → 未命中并重建（H4）。
func TestCacheHitOutputMissingRegenerates(t *testing.T) {
	root := b3Project(t)
	calls := b3CountCodegen(t)
	src := filepath.Join(root, "home.gxml")
	gen := filepath.Join(root, "home_gen.go")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	if err := os.Remove(gen); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("重新生成失败: %s", e)
	}
	if !strings.Contains(out, "output-missing") {
		t.Fatalf("应报 output-missing:\n%s", out)
	}
	if _, err := os.Stat(gen); err != nil {
		t.Fatalf("产物未重建: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("Codegen 次数 = %d, 期望 2（产物缺失必须真生成）", *calls)
	}
}

// TestCacheHitOutputModifiedIsRebuilt：正文被手改但保护头仍在 → 重建覆盖（D-2 裁决）。
func TestCacheHitOutputModifiedIsRebuilt(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")
	gen := filepath.Join(root, "home_gen.go")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	data, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append(append([]byte{}, data...), []byte("\n// TAMPERED-BY-USER\n")...)
	if err := os.WriteFile(gen, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("重建失败: %s", e)
	}
	if !strings.Contains(out, "output-modified") {
		t.Fatalf("应报 output-modified:\n%s", out)
	}
	after, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "TAMPERED-BY-USER") {
		t.Fatal("产出应被重新生成覆盖，手改内容仍在")
	}
}

// TestCacheHitUserOwnedIsError：保护头被改 = 用户接管 → 报错且不覆盖（D-3 裁决）。
func TestCacheHitUserOwnedIsError(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")
	gen := filepath.Join(root, "home_gen.go")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	user := []byte("// 我接管了这个文件\npackage miniproj\n")
	if err := os.WriteFile(gen, user, 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errs := b3Run(t, src)
	if code != ExitFailure {
		t.Fatalf("用户接管时应失败，退出码 = %d", code)
	}
	if !strings.Contains(errs, "拒绝覆盖") {
		t.Fatalf("stderr 应说明拒绝覆盖:\n%s", errs)
	}
	after, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(user) {
		t.Fatal("用户接管的文件被覆盖了（契约 §13 第 3 条）")
	}
}

// TestCacheHitInputChangeInvalidates：改 .gxml → input-changed。
func TestCacheHitInputChangeInvalidates(t *testing.T) {
	root := b3Project(t)
	calls := b3CountCodegen(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, bytes.Replace(data, []byte("加一"), []byte("加二"), 1), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("重新生成失败: %s", e)
	}
	if !strings.Contains(out, "input-changed") {
		t.Fatalf("应报 input-changed:\n%s", out)
	}
	if *calls != 2 {
		t.Fatalf("Codegen 次数 = %d, 期望 2", *calls)
	}
}

// TestCacheHitPackageSourcesChangeInvalidates：同包新增 .go → package-sources-changed。
func TestCacheHitPackageSourcesChangeInvalidates(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.go"),
		[]byte("package miniproj\n\n// Extra 只为改变 K3 指纹。\ntype Extra struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("重新生成失败: %s", e)
	}
	if !strings.Contains(out, "package-sources-changed") {
		t.Fatalf("应报 package-sources-changed:\n%s", out)
	}
}

// TestCacheHitCorruptCacheFallsBack：缓存坏 → cache-corrupt，但仍成功重建。
func TestCacheHitCorruptCacheFallsBack(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	if err := os.WriteFile(b3CacheFile(t), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("坏缓存不得让生成失败: code=%d stderr=%s", code, e)
	}
	if !strings.Contains(out, "cache-corrupt") {
		t.Fatalf("应报 cache-corrupt:\n%s", out)
	}
	if !strings.Contains(out, "已生成") {
		t.Fatalf("坏缓存后应重新生成:\n%s", out)
	}
}

// TestCacheHitNoCacheBypassesRead：--no-cache 只生成、不报未命中原因，且仍写缓存。
func TestCacheHitNoCacheBypassesRead(t *testing.T) {
	root := b3Project(t)
	calls := b3CountCodegen(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}

	code, out, e := b3Run(t, src, "--no-cache")
	if code != ExitOK {
		t.Fatalf("--no-cache 生成失败: %s", e)
	}
	if strings.Contains(out, "跳过") {
		t.Fatalf("--no-cache 不得命中:\n%s", out)
	}
	if strings.Contains(out, "缓存未命中") {
		t.Fatalf("--no-cache 不应解释未命中原因（用户主动绕过）:\n%s", out)
	}
	if !strings.Contains(out, "已生成") {
		t.Fatalf("--no-cache 应真生成:\n%s", out)
	}
	if *calls != 2 {
		t.Fatalf("Codegen 次数 = %d, 期望 2", *calls)
	}
	if _, err := os.Stat(b3CacheFile(t)); err != nil {
		t.Fatalf("--no-cache 仍应更新缓存: %v", err)
	}
}

// TestCacheMultiInputSamePackageHitsTwice 是陷阱 T1 的端到端验证。
//
// 同一包内两个 .gxml：第一次生成两个产物；第二次必须**两个都命中**。
// 若 K3 排除集漏掉「本批计划 target」，第二次会因包内多出产物而永不命中。
func TestCacheMultiInputSamePackageHitsTwice(t *testing.T) {
	root := b3Project(t)
	calls := b3CountCodegen(t)

	home, err := os.ReadFile(filepath.Join(root, "home.gxml"))
	if err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "second.gxml")
	if err := os.WriteFile(second, home, 0o644); err != nil {
		t.Fatal(err)
	}

	code, out1, e := b3Run(t, root)
	if code != ExitOK {
		t.Fatalf("首次生成失败: %s\n%s", e, out1)
	}
	if *calls != 2 {
		t.Fatalf("两个输入应各跑一次 Codegen，实际 %d", *calls)
	}

	code, out2, e := b3Run(t, root)
	if code != ExitOK {
		t.Fatalf("第二次生成失败: %s", e)
	}
	if *calls != 2 {
		t.Fatalf("第二次仍有 Codegen 调用（次数=%d）：同包多文件未命中，T1 排除集有问题", *calls)
	}
	if strings.Count(out2, "跳过") != 2 {
		t.Fatalf("两个产物都应命中:\n%s", out2)
	}
	if !strings.Contains(out2, "命中 2，生成 0") {
		t.Fatalf("缺少命中汇总:\n%s", out2)
	}
}

// TestCacheHitCompilerVersionChangedInvalidates：生成器版本变化 → 正确失效。
//
// 模拟方式：把 Manifest 与缓存记录都改成「旧版本 0 生成」的形态
// （缓存记录的键也换成另一个，因为真实升版本必定改变键）。
func TestCacheHitCompilerVersionChangedInvalidates(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}

	// 1) Manifest 版本改成 0
	raw, err := os.ReadFile(manifestPathOverride)
	if err != nil {
		t.Fatal(err)
	}
	var man manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	for i := range man.Entries {
		man.Entries[i].CompilerVersion = "0"
	}
	updated, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPathOverride, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2) 缓存记录改成「旧键 + 版本 0」
	cachePath := b3CacheFile(t)
	cdata, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var rec cacheRecord
	if err := json.Unmarshal(cdata, &rec); err != nil {
		t.Fatal(err)
	}
	oldKey := strings.Repeat("d", 64)
	rec.KeyHash = oldKey
	rec.CompilerVersion = "0"
	newData, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cachePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheRecordPath(root, oldKey), append(newData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("重新生成失败: %s", e)
	}
	if !strings.Contains(out, "compiler-version-changed") {
		t.Fatalf("应报 compiler-version-changed:\n%s", out)
	}
}

// TestCacheHitManifestLossKeepsKeyStable 是 K3 排除集（含本批 target）的实证。
//
// 场景：Manifest 登记丢了，但产物与缓存都还在。此时若 K3 不排除「本批 target」，
// 包目录里残留的 home_gen.go 会进入指纹 → 本次键与缓存里的旧键不同 →
// 未命中原因被误报成 package-sources-changed（其实是 Manifest 丢了）。
//
// 排除本批 target 后键保持稳定，原因准确报 cache-missing，
// 缓存记录也不会因为「自己的产物」而失效。
func TestCacheHitManifestLossKeepsKeyStable(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}

	// 清空 Manifest 登记，保留产物与缓存。
	empty := []byte("{\n  \"version\": 1,\n  \"entries\": []\n}\n")
	if err := os.WriteFile(manifestPathOverride, empty, 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("重新生成失败: %s", e)
	}
	if strings.Contains(out, "package-sources-changed") {
		t.Fatalf("Manifest 丢失被误报成包源码变化（K3 排除集漏了本批 target）:\n%s", out)
	}
	if !strings.Contains(out, "cache-missing") {
		t.Fatalf("应报 cache-missing:\n%s", out)
	}
}
