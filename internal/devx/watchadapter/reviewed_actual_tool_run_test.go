package watchadapter_test

import (
	"context"
	"github.com/goxml/goxml/internal/devx/watch"
	"github.com/goxml/goxml/internal/devx/watchadapter"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewedRealBinaryDefaultOutputCanRun(t *testing.T) {
	root := repoRoot(t)
	tool := filepath.Join(t.TempDir(), "goxml.exe")
	cmd := exec.Command("go", "build", "-o", tool, "./cmd/goxml")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("build actual tool: %v %s", e, out)
	}
	proj := makeRealProject(t)
	t.Chdir(filepath.Dir(proj))
	rb := watchadapter.NewRealBuilder(filepath.Base(proj), "./cmd/app", "", tool)
	pm := watch.NewProcessManager(rb.BinaryPath)
	type event struct {
		res watch.GenerationResult
		pub bool
	}
	events := make(chan event, 4)
	w, e := watch.New(watch.Config{ProjectDir: proj, PollIntervalMS: 5, DebounceMS: 1}, rb, pm, obsFunc(func(_ int, r watch.GenerationResult, p bool) { events <- event{r, p} }))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop")
		}
		if e := pm.Shutdown(); e != nil {
			t.Error(e)
		}
	})
	writeFile(t, filepath.Join(proj, "ui", "home.gxml"), `<Goxml Version="1"><Page Route="/"><Text Value="changed"/></Page></Goxml>`)
	select {
	case ev := <-events:
		if !ev.pub {
			t.Fatalf("actual tool/default output did not publish: %+v", ev.res)
		}
		child := pm.Current()
		if child == nil || !child.Running() {
			t.Fatal("no live owned console generation")
		}
		data, e := os.ReadFile(ev.res.Detail)
		if e != nil {
			t.Fatal(e)
		}
		if len(data) < 2 || string(data[:2]) != "MZ" {
			t.Fatal("not a real Windows executable")
		}
	case <-ctx.Done():
		t.Fatal("real Run did not produce a generation")
	}
}
