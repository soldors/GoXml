package watch

import (
	"context"
	"errors"
	devxh "github.com/goxml/goxml/internal/devx/hydration"
	"path/filepath"
	"testing"
)

func coordR1Hook(t *testing.T, dir string, capture CaptureOld) *HydrationHook {
	t.Helper()
	s, e := devxh.NewStore(dir, "coord-r1")
	if e != nil {
		t.Fatal(e)
	}
	return &HydrationHook{Proto: &devxh.Protocol{Store: s, Schema: "s", AppID: "a"}, Capture: capture}
}
func coordR1Next(t *testing.T, w *Watch, at int) {
	t.Helper()
	if _, e := w.NotifyChange(context.Background(), at, []string{"ui/home.gxml"}); e != nil {
		t.Fatal(e)
	}
	if _, e := w.Tick(context.Background(), at+100); e != nil {
		t.Fatal(e)
	}
}
func TestCoordR27R1CaptureScanFailureCannotPublish(t *testing.T) {
	dir := mkProj(t)
	w, e := New(Config{ProjectDir: dir}, &fakeBuilder{}, &fakeSwapper{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	coordR1Next(t, w, 0)
	w.cfg.Hydration = coordR1Hook(t, dir, func(context.Context, int) ([]byte, error) {
		mustWrite(t, filepath.Join(dir, ".goxml", "generate-manifest.json"), "{broken")
		return []byte(`{}`), nil
	})
	coordR1Next(t, w, 200)
	if w.LastPublishedGen() != 1 {
		t.Fatalf("post-capture Scan failed but generation published: %d", w.LastPublishedGen())
	}
}

func TestCoordR27R1RealManagerLostServiceAfterFailedSwapCanRecover(t *testing.T) {
	dir := mkProj(t)
	bin := buildConsoleApp(t)
	pm := NewProcessManager(func(gen int) string {
		if gen == 2 {
			return filepath.Join(t.TempDir(), "missing.exe")
		}
		return bin
	})
	defer func() {
		if e := pm.Shutdown(); e != nil {
			t.Error(e)
		}
	}()
	w, e := New(Config{ProjectDir: dir}, &fakeBuilder{}, pm, nil)
	if e != nil {
		t.Fatal(e)
	}
	coordR1Next(t, w, 0)
	if pm.Current() == nil || !pm.Current().Running() {
		t.Fatal("real first service did not start")
	}
	deadCapture := 0
	w.cfg.Hydration = coordR1Hook(t, dir, func(_ context.Context, old int) ([]byte, error) {
		if pm.Current() == nil || !pm.Current().Running() {
			deadCapture++
			return nil, errors.New("capture requested nonexistent service")
		}
		return []byte(`{}`), nil
	})
	coordR1Next(t, w, 200)
	if pm.Current() != nil {
		t.Fatal("real gen2 start-failure control did not leave service empty")
	}
	coordR1Next(t, w, 400)
	current := pm.Current()
	gen := 0
	if current != nil {
		gen = current.Gen
	}
	if gen != 3 || w.LastPublishedGen() != 3 || deadCapture != 0 {
		t.Fatalf("real ProcessManager lost old service, but hook still targeted it: serving=%d published=%d capture-dead=%d", gen, w.LastPublishedGen(), deadCapture)
	}
}
