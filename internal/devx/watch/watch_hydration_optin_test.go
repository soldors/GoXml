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

// capture 失败：保旧不切换（不 swap）。
func TestWatchHydrationCaptureFailureKeepsOld(t *testing.T) {
	hook := newHydHook(t, func(ctx context.Context, oldGen int) ([]byte, error) {
		return nil, errors.New("owner unavailable")
	})
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: mkProj(t), DebounceMS: 1, Hydration: hook}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	w.Tick(ctx, 100)
	if len(fs.swapped) != 0 {
		t.Fatalf("capture failure must NOT swap (keep old), got %v", fs.swapped)
	}
}

// capture 超时：不提交、不切换。
func TestWatchHydrationTimeoutKeepsOld(t *testing.T) {
	hook := newHydHook(t, func(ctx context.Context, oldGen int) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	hook.Proto.CaptureTimeout = 30 * time.Millisecond
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: mkProj(t), DebounceMS: 1, Hydration: hook}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	w.Tick(ctx, 100)
	if len(fs.swapped) != 0 {
		t.Fatalf("capture timeout must NOT swap, got %v", fs.swapped)
	}
}
