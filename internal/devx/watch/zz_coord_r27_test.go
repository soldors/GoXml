package watch

import (
	"context"
	"errors"
	devxh "github.com/goxml/goxml/internal/devx/hydration"
	"path/filepath"
	"testing"
)

func coordHook(t *testing.T, dir string, capture CaptureOld) *HydrationHook {
	t.Helper()
	s, e := devxh.NewStore(dir, "coord")
	if e != nil {
		t.Fatal(e)
	}
	return &HydrationHook{Proto: &devxh.Protocol{Store: s, Schema: "s", AppID: "a"}, Capture: capture}
}
func coordNext(t *testing.T, w *Watch, at int) {
	t.Helper()
	if _, e := w.NotifyChange(context.Background(), at, []string{"ui/home.gxml"}); e != nil {
		t.Fatal(e)
	}
	if _, e := w.Tick(context.Background(), at+100); e != nil {
		t.Fatal(e)
	}
}
func TestCoordR27ColdStartHasNoOldGeneration(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	calls := 0
	hook := coordHook(t, dir, func(context.Context, int) ([]byte, error) { calls++; return nil, errors.New("no old process exists") })
	w, e := New(Config{ProjectDir: dir, Hydration: hook}, fb, fs, nil)
	if e != nil {
		t.Fatal(e)
	}
	coordNext(t, w, 0)
	if calls != 0 || w.LastPublishedGen() != 1 {
		t.Fatalf("cold start wrongly requested old snapshot: calls=%d published=%d", calls, w.LastPublishedGen())
	}
}
func TestCoordR27OldGenerationFollowsPublishedProcess(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{results: map[int]GenerationResult{2: {OK: false, Gen: 2}}}
	fs := &fakeSwapper{}
	w, e := New(Config{ProjectDir: dir}, fb, fs, nil)
	if e != nil {
		t.Fatal(e)
	}
	coordNext(t, w, 0)
	seen := -1
	w.cfg.Hydration = coordHook(t, dir, func(_ context.Context, old int) ([]byte, error) { seen = old; return []byte(`{}`), nil })
	coordNext(t, w, 200)
	coordNext(t, w, 400)
	if seen != 1 || w.LastPublishedGen() != 3 {
		t.Fatalf("failed build skipped a process generation: capture-old=%d want 1, published=%d", seen, w.LastPublishedGen())
	}
}
func TestCoordR27InputChangeDuringCaptureCannotPublish(t *testing.T) {
	dir := mkProj(t)
	w, e := New(Config{ProjectDir: dir}, &fakeBuilder{}, &fakeSwapper{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	coordNext(t, w, 0)
	w.cfg.Hydration = coordHook(t, dir, func(context.Context, int) ([]byte, error) {
		mustWrite(t, filepath.Join(dir, "ui", "vm.go"), "package ui\nvar Broken = undefinedName\n")
		return []byte(`{}`), nil
	})
	coordNext(t, w, 200)
	if w.LastPublishedGen() != 1 {
		t.Fatalf("published compiled generation after user input changed during capture: published=%d want old 1", w.LastPublishedGen())
	}
}
