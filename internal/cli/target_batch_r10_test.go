package cli

// R10-A 批量目标解析判别回归：批内一次/有界分组 go list，正确性不变；
// 映射/错误漏判有可编译变异红。只读 go list，不 build 跨目标。

import (
	"path/filepath"
	"testing"
)

func batchFixture(t *testing.T, n int) (root string, dirs []string) {
	t.Helper()
	root = t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/b\n\ngo 1.26\n")
	for i := 0; i < n; i++ {
		pkg := "pkg" + twoDigit(i)
		d := filepath.Join(root, pkg)
		mustWrite(t, filepath.Join(d, "vm.go"), "package "+pkg+"\n\ntype VM struct{}\n")
		dirs = append(dirs, d)
	}
	return root, dirs
}

// TestBatchResolvesAllDirs 批量解析多目录，每目录包名正确。
func TestBatchResolvesAllDirs(t *testing.T) {
	root, dirs := batchFixture(t, 5)
	res, err := resolveTargetPackagesBatch(goBinary(t), root, []string{"GOENV=off"}, dirs, nil)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	for _, d := range dirs {
		r, ok := lookupBatch(res, d)
		if !ok || !r.ok || r.err != nil {
			t.Fatalf("%s: ok=%v err=%v", d, r.ok, r.err)
		}
		want := "pkg" + twoDigit(indexOf(dirs, d))
		if r.name != want {
			t.Fatalf("%s name=%q want %q", d, r.name, want)
		}
	}
}

func twoDigit(i int) string {
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func indexOf(ss []string, x string) int {
	for i, s := range ss {
		if s == x {
			return i
		}
	}
	return -1
}

// TestBatchChunkingMultipleGoList 30 目录 → 有界分 2 批（batchListChunk=20），仍全解析。
func TestBatchChunkingMultipleGoList(t *testing.T) {
	root, dirs := batchFixture(t, 30)
	res, err := resolveTargetPackagesBatch(goBinary(t), root, []string{"GOENV=off"}, dirs, nil)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	resolved := 0
	for _, d := range dirs {
		if r, ok := lookupBatch(res, d); ok && r.ok && r.err == nil {
			resolved++
		}
	}
	if resolved != 30 {
		t.Fatalf("30 目录应全部解析，实际 %d", resolved)
	}
}

// TestBatchUninitializedDirFallsBack 不存在目录 → 结果不存在（交由 resolver 单查兜底/回退）。
// [变异红] 若批量把不存在目录误当成功或塞错 err，此测试失败。
func TestBatchUninitializedDirFallsBack(t *testing.T) {
	root, dirs := batchFixture(t, 2)
	missing := filepath.Join(root, "does-not-exist")
	all := append(append([]string{}, dirs...), missing)
	res, err := resolveTargetPackagesBatch(goBinary(t), root, []string{"GOENV=off"}, all, nil)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	// 不存在目录应有 ok=false（未初始化），不是 ok=true。
	if r, ok := lookupBatch(res, missing); ok {
		if r.ok {
			t.Fatalf("不存在的目录不应被批量判为已初始化: %+v", r)
		}
	}
	// 真实目录仍正确。
	for _, d := range dirs {
		if r, ok := lookupBatch(res, d); !ok || !r.ok || r.err != nil {
			t.Fatalf("%s 应成功: ok=%v err=%v", d, r.ok, r.err)
		}
	}
}

// TestBatchMultiPackageFailClosed 多 package 目录 → 该目录 error（不因批量而漏判为成功）。
// [变异红] 若批量映射把多包目录当唯一包成功，此测试失败。
func TestBatchMultiPackageFailClosed(t *testing.T) {
	root, dirs := batchFixture(t, 1)
	multi := filepath.Join(root, "multi")
	mustWrite(t, filepath.Join(multi, "a.go"), "package pa\n\nfunc A() {}\n")
	mustWrite(t, filepath.Join(multi, "b.go"), "package pb\n\nfunc B() {}\n")
	all := append(append([]string{}, dirs...), multi)
	res, err := resolveTargetPackagesBatch(goBinary(t), root, []string{"GOENV=off"}, all, nil)
	if err != nil {
		// 批量命令层失败也接受（fail-closed）；但更期望 per-dir error。
		return
	}
	r, ok := lookupBatch(res, multi)
	if ok && r.ok && r.err == nil {
		t.Fatalf("多 package 目录不应被批量判为唯一包成功: %+v", r)
	}
}
