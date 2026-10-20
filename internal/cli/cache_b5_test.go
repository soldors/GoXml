package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// TASK-GENERATE-CACHE-001 切片 B5：矩阵补齐（C5/C12/C13/C15/C17）+ 性能分账
//
// 性能部分**不断言时间**（时间断言必然抖动），只打印实测数字供 dev-log 记账；
// 「跳过是否真的发生」由 B3 的执行计数负责，不靠这里的耗时推断。
// ============================================================================

// b5ProjectWithInputs 建一个含 n 个输入、**每个输入一个独立包**的工程。
//
// 不能把 n 个 .gxml 塞进同一个包：产物里的包级符号（renderKey/textCache）
// 会重名，包无法编译（独立缺陷，见 dev-log §86）。
func b5ProjectWithInputs(t *testing.T, n int) string {
	t.Helper()
	root := tempProjectInRepo(t)
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg%d", i+1))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		copyFixture(t, dir)
	}
	return root
}

// TestCacheOutDirChangeInvalidates：换 --out-dir → options-changed，产物落到新目录（C5）。
func TestCacheOutDirChangeInvalidates(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")
	gen1 := filepath.Join(root, "gen1")
	gen2 := filepath.Join(root, "gen2")

	if code, _, e := b3Run(t, src, "--out-dir", gen1); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	if _, err := os.Stat(filepath.Join(gen1, "home_gen.go")); err != nil {
		t.Fatalf("产物未落在 gen1: %v", err)
	}

	code, out, e := b3Run(t, src, "--out-dir", gen2)
	if code != ExitOK {
		t.Fatalf("第二次生成失败: %s", e)
	}
	if !strings.Contains(out, "options-changed") {
		t.Fatalf("换输出目录应报 options-changed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(gen2, "home_gen.go")); err != nil {
		t.Fatalf("产物未落在 gen2: %v", err)
	}
}

// TestCacheNoCacheEquivalence：--no-cache 与缓存路径的产物逐字节相同（C12 / 契约 §13-13）。
func TestCacheNoCacheEquivalence(t *testing.T) {
	rootA := b3Project(t)
	// 第二个工程：不同目录名，避免与 A 共享 Manifest（已隔离，但输入路径不同更干净）。
	rootB := tempProjectInRepo(t)
	copyFixture(t, rootB)

	if code, _, e := b3Run(t, filepath.Join(rootA, "home.gxml")); code != ExitOK {
		t.Fatalf("缓存路径生成失败: %s", e)
	}
	if code, _, e := b3Run(t, filepath.Join(rootB, "home.gxml"), "--no-cache"); code != ExitOK {
		t.Fatalf("--no-cache 生成失败: %s", e)
	}

	a, err := os.ReadFile(filepath.Join(rootA, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(rootB, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("两条路径产物不一致:\n--- 缓存 ---\n%s\n--- --no-cache ---\n%s", a, b)
	}
}

// TestCacheNoWriteOnFailure：生成失败时不产生缓存与产物（C13）。
func TestCacheNoWriteOnFailure(t *testing.T) {
	root := b3Project(t)
	bad := filepath.Join(root, "bad.gxml")
	if err := os.WriteFile(bad, []byte("<Goxml><Page></Goxml>"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errs := b3Run(t, bad)
	if code == ExitOK {
		t.Fatalf("非法输入不应成功；stderr=%s", errs)
	}
	if _, err := os.Stat(filepath.Join(root, "bad_gen.go")); !os.IsNotExist(err) {
		t.Fatalf("失败时不得留下产物: %v", err)
	}
	entries, err := os.ReadDir(b3CacheDir(t))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if cacheRecordNamePattern.MatchString(e.Name()) {
			t.Fatalf("失败时不得写入缓存记录: %s", e.Name())
		}
	}
}

// TestCacheMixedBatch：一个命中一个生成，批次仍原子（C15）。
//
// 同样用两个不同包子目录（同包两个产物会触发独立的符号重名缺陷）。
func TestCacheMixedBatch(t *testing.T) {
	root := tempProjectInRepo(t)
	for _, sub := range []string{"pkg1", "pkg2"} {
		dir := filepath.Join(root, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		copyFixture(t, dir)
	}
	calls := b3CountCodegen(t)

	if code, out, e := b3Run(t, root); code != ExitOK {
		t.Fatalf("首次生成失败: %s\n%s", e, out)
	}
	if *calls != 2 {
		t.Fatalf("首次应跑 2 次 Codegen，实际 %d", *calls)
	}

	// 只改 pkg2 的源
	target := filepath.Join(root, "pkg2", "home.gxml")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, bytes.Replace(data, []byte("加一"), []byte("加三"), 1), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, root)
	if code != ExitOK {
		t.Fatalf("混合批次失败: %s", e)
	}
	if *calls != 3 {
		t.Fatalf("混合批次应只多跑 1 次 Codegen，实际总数 %d", *calls)
	}
	if !strings.Contains(out, "命中 1，生成 1") {
		t.Fatalf("应有「命中 1，生成 1」:\n%s", out)
	}
	if strings.Count(out, "跳过 ") != 1 || strings.Count(out, "已生成 ") != 1 {
		t.Fatalf("应恰好一命中一生成:\n%s", out)
	}
}

// TestCacheDirRemovalIsHarmless：删掉整个缓存目录不改变生成结果（C17 / 契约 §11）。
func TestCacheDirRemovalIsHarmless(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	before, err := os.ReadFile(filepath.Join(root, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(b3CacheDir(t)); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("删缓存后生成失败: %s", e)
	}
	if strings.Contains(out, "跳过") {
		t.Fatalf("缓存被删后不应命中:\n%s", out)
	}
	after, err := os.ReadFile(filepath.Join(root, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("删除缓存改变了生成结果（契约 §11 禁止）")
	}
}

// TestCachePerfReport 打印命中/未命中的墙钟时间，供 dev-log §86 记账。
//
// 明确不设时间阈值：阈值型断言在 CI 上必然变随机红灯。
// 「跳过是否真的发生」由 TestCacheHitSecondRunSkipsAndSkipsCodegen 的计数保证。
func TestCachePerfReport(t *testing.T) {
	if testing.Short() {
		t.Skip("性能测量在 -short 下跳过")
	}
	for _, n := range []int{1, 10} {
		root := b5ProjectWithInputs(t, n)
		calls := b3CountCodegen(t)

		// 第 1 轮：全未命中（冷缓存）
		start := time.Now()
		if code, _, e := b3Run(t, root); code != ExitOK {
			t.Fatalf("n=%d 首次生成失败: %s", n, e)
		}
		firstMiss := time.Since(start)
		if *calls != n {
			t.Fatalf("n=%d 首次应跑 %d 次 Codegen，实际 %d", n, n, *calls)
		}

		// 第 2 轮：全命中
		start = time.Now()
		code, out, errs := b3Run(t, root)
		hit := time.Since(start)
		if code != ExitOK {
			t.Fatalf("n=%d 命中轮失败: %s", n, errs)
		}
		if *calls != n {
			t.Fatalf("n=%d 命中轮仍有 Codegen 调用（次数=%d）", n, *calls)
		}
		if strings.Count(out, "跳过 ") != n {
			t.Fatalf("n=%d 命中轮应全部跳过:\n%s", n, out)
		}

		// 第 3 轮：改动全部源，再测一次未命中（避免把首次的磁盘冷启动当成常态）
		// 必须递归取源：输入在 pkg1/、pkg2/… 子目录里，只扫根目录会一个都改不到。
		found, err := discoverInputs(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != n {
			t.Fatalf("n=%d 发现 %d 个输入", n, len(found))
		}
		for _, in := range found {
			data, err := os.ReadFile(in.abs)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(in.abs, append(data, 10), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		start = time.Now()
		code, out, errs = b3Run(t, root)
		rewriteMiss := time.Since(start)
		if code != ExitOK {
			t.Fatalf("n=%d 重生成失败: %s\n%s", n, errs, out)
		}
		// 必须真的重生成：否则「未命中耗时」测的是空转。
		if got := strings.Count(out, "已生成 "); got != n {
			t.Fatalf("n=%d 改动源后应有 %d 个「已生成」，实际 %d:\n%s", n, n, got, out)
		}
		if *calls != 2*n {
			t.Fatalf("n=%d Codegen 总次数应为 %d，实际 %d", n, 2*n, *calls)
		}

		t.Logf("n=%d first_miss=%v hit=%v rewrite_miss=%v hit_lines=%d",
			n, firstMiss.Round(time.Millisecond), hit.Round(time.Millisecond),
			rewriteMiss.Round(time.Millisecond), len(nonEmptyLines(out)))
	}
}
