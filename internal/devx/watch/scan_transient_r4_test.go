package watch

// R29-A R4：无 Hydration 的既有 Watch —— 扫描遇到**瞬时**文件共享/锁冲突时有界重试保旧，不终止 Watch；
// 非瞬时错误仍终止（保留真实失败）。受控注入 scanFn，确定性复现组合门禁中的 broken.go 共享冲突后果。

import (
	"context"
	"os"

	"golang.org/x/sys/windows"
	"testing"
	"time"
)

type noopBuilder struct{}

func (noopBuilder) Build(context.Context, int, []string) GenerationResult {
	return GenerationResult{OK: true, Gen: 1}
}

type noopSwapper struct{}

func (noopSwapper) Swap(context.Context, int) error { return nil }

func newRunTestWatch(t *testing.T, dir string) *Watch {
	t.Helper()
	w, err := New(Config{ProjectDir: dir, PollIntervalMS: 1, DebounceMS: 1}, noopBuilder{}, noopSwapper{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestR4TransientScanSharingViolationKeepsWatch(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(dir+"/a.gxml", []byte("x"), 0o644)
	w := newRunTestWatch(t, dir)
	orig := scanFn
	defer func() { scanFn = orig }()
	transient := &os.PathError{Op: "open", Path: dir + "/broken.go", Err: windows.ERROR_SHARING_VIOLATION}
	calls := 0
	scanFn = func(cfg Config) (Snapshot, error) {
		calls++
		if calls <= 5 {
			return nil, transient // 连续瞬时共享冲突 -> 应跳过（保旧），不终止
		}
		return orig(cfg) // 恢复
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := w.Run(ctx) // ctx 超时应正常返回 nil（未因瞬时错误终止）
	if err != nil {
		t.Fatalf("transient sharing violation must NOT terminate watch, got: %v", err)
	}
	if calls < 5 {
		t.Fatalf("expected transient retries, got calls=%d", calls)
	}
}

func TestR4NonTransientScanErrorTerminates(t *testing.T) {
	dir := t.TempDir()
	w := newRunTestWatch(t, dir)
	orig := scanFn
	defer func() { scanFn = orig }()
	fatal := &os.PathError{Op: "open", Path: dir + "/x.go", Err: windows.ERROR_FILE_NOT_FOUND}
	scanFn = func(Config) (Snapshot, error) { return nil, fatal }
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := w.Run(ctx)
	if err == nil {
		t.Fatal("non-transient scan error must terminate watch with error")
	}
}
