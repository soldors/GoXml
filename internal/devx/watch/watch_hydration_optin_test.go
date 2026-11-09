package watch

// R27-A A2：Watch opt-in hydration 钩子的判别回归（默认 nil 路径不变；启用时快照失败保旧不切换、
// 成功才切换；取消/超时不提交）。

import (
	"context"
	"errors"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
)

func newHydHook(t *testing.T, capture CaptureOld) *HydrationHook {
	t.Helper()
	st, err := devxh.NewStore(t.TempDir(), "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	proto := &devxh.Protocol{Store: st, Schema: "demo.v1", AppID: "app-1", CaptureTimeout: time.Second}
	return &HydrationHook{Proto: proto, Capture: capture}
}

// 默认 nil 钩子：行为与 R26 一致（构建成功即交接），不写状态文件。
func TestWatchNoHydrationDefaultUnchanged(t *testing.T) {
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, err := New(Config{ProjectDir: mkProj(t), DebounceMS: 1}, fb, fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	w.Tick(ctx, 100)
	if len(fs.swapped) != 1 {
		t.Fatalf("default (nil hydration) must swap exactly once, got %v", fs.swapped)
	}
}

// 启用 hydration 且 capture 成功：先快照后交接。
func TestWatchHydrationSuccessSwaps(t *testing.T) {
	hook := newHydHook(t, func(ctx context.Context, oldGen int) ([]byte, error) {
		return []byte(`{"count":1}`), nil
	})
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: mkProj(t), DebounceMS: 1, Hydration: hook}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	w.Tick(ctx, 100)
	if len(fs.swapped) != 1 {
		t.Fatalf("hydration success must swap, got %v", fs.swapped)
	}
}

// capture 失败（**存在旧进程时**）：保旧不切换。F-A5：冷启动无旧代合法跳过捕获，故先暖起 gen1，
// 再启用 hydration 驱动 gen2，使 capture 在“有旧进程”时失败 => gen2 不切换（仍仅 gen1）。
func TestWatchHydrationCaptureFailureKeepsOld(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir, DebounceMS: 1}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	w.Tick(ctx, 100) // gen1（无 hydration）发布
	if w.LastPublishedGen() != 1 || len(fs.swapped) != 1 {
		t.Fatal("warmup gen1 expected")
	}
	w.cfg.Hydration = newHydHook(t, func(ctx context.Context, oldGen int) ([]byte, error) {
		return nil, errors.New("owner unavailable")
	})
	w.NotifyChange(ctx, 200, []string{"ui/home.gxml"})
	w.Tick(ctx, 300) // gen2 构建成功但 capture 失败 => 不切换
	if len(fs.swapped) != 1 || w.LastPublishedGen() != 1 {
		t.Fatalf("capture failure must NOT swap gen2 (keep old gen1): swaps=%v published=%d", fs.swapped, w.LastPublishedGen())
	}
}

// capture 超时（存在旧进程时）：不切换。先暖起 gen1，再以短 CaptureTimeout 驱动 gen2。
func TestWatchHydrationTimeoutKeepsOld(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir, DebounceMS: 1}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	w.Tick(ctx, 100)
	hook := newHydHook(t, func(ctx context.Context, oldGen int) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	hook.Proto.CaptureTimeout = 30 * time.Millisecond
	w.cfg.Hydration = hook
	w.NotifyChange(ctx, 200, []string{"ui/home.gxml"})
	w.Tick(ctx, 300)
	if len(fs.swapped) != 1 || w.LastPublishedGen() != 1 {
		t.Fatalf("capture timeout must NOT swap gen2: swaps=%v published=%d", fs.swapped, w.LastPublishedGen())
	}
}
