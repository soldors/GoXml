package watch

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failureBuild func(context.Context, int, []string) GenerationResult

func (f failureBuild) Build(c context.Context, g int, p []string) GenerationResult { return f(c, g, p) }

type failureObserver struct {
	results   []GenerationResult
	published []bool
}

func (o *failureObserver) OnGeneration(_ int, r GenerationResult, p bool) {
	o.results = append(o.results, r)
	o.published = append(o.published, p)
}

// The only directory moved is the unique absolute test project, to a sibling in
// its own test directory. Restore it before TempDir cleanup.
func parkFailureProject(t *testing.T, root string) {
	t.Helper()
	a, e := filepath.Abs(root)
	if e != nil {
		t.Fatal(e)
	}
	b := a + ".parked"
	if filepath.Dir(a) != filepath.Dir(b) || !filepath.IsAbs(a) {
		t.Fatal("unsafe project park")
	}
	if e := os.Rename(a, b); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.Rename(b, a); e != nil {
			t.Error(e)
		}
	})
}

func TestFailureBoundariesNeverPublishUnavailableOrCanceledInput(t *testing.T) {
	for _, scenario := range []string{"cancel-after-success", "gone-before-build", "gone-during-build", "manifest-damaged-during-build"} {
		t.Run(scenario, func(t *testing.T) {
			dir := mkProj(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			b := failureBuild(func(context.Context, int, []string) GenerationResult {
				calls++
				switch scenario {
				case "cancel-after-success":
					cancel()
				case "gone-during-build":
					parkFailureProject(t, dir)
				case "manifest-damaged-during-build":
					mustWrite(t, filepath.Join(dir, ".goxml", "generate-manifest.json"), "{")
					mustWrite(t, filepath.Join(dir, "ui", "vm.go"), "package ui // changed while building")
				}
				return GenerationResult{OK: true}
			})
			s := new(fakeSwapper)
			o := new(failureObserver)
			w, e := New(Config{ProjectDir: dir, DebounceMS: 1}, b, s, o)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := w.NotifyChange(ctx, 0, []string{"ui/home.gxml"}); e != nil {
				t.Fatal(e)
			}
			if scenario == "gone-before-build" {
				parkFailureProject(t, dir)
			}
			if ran, e := w.Tick(ctx, 2); e != nil || !ran {
				t.Fatalf("ran=%v err=%v", ran, e)
			}
			if len(s.swapped) != 0 || len(o.results) != 1 || o.published[0] {
				t.Fatalf("unsafe publish: swaps=%v results=%+v published=%v", s.swapped, o.results, o.published)
			}
			stage := "scan"
			if scenario == "cancel-after-success" {
				stage = "canceled"
			}
			if o.results[0].Stage != stage {
				t.Fatalf("failure stage=%s want=%s", o.results[0].Stage, stage)
			}
			wantCalls := 1
			if scenario == "gone-before-build" {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("builder calls=%d want=%d", calls, wantCalls)
			}
		})
	}
}

func TestRunSurfacesLostModuleAndExplicitStop(t *testing.T) {
	for _, lost := range []bool{true, false} {
		t.Run(map[bool]string{true: "lost-module", false: "explicit-stop"}[lost], func(t *testing.T) {
			dir := mkProj(t)
			b := new(fakeBuilder)
			w, e := New(Config{ProjectDir: dir, PollIntervalMS: 1}, b, new(fakeSwapper), nil)
			if e != nil {
				t.Fatal(e)
			}
			if lost {
				parkFailureProject(t, dir)
			} else {
				w.Stop()
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			e = w.Run(ctx)
			if lost && e == nil {
				t.Fatal("lost watched module reported success")
			}
			if !lost && e != nil {
				t.Fatal(e)
			}
			if len(b.calls) != 0 {
				t.Fatal("stopped/unavailable watch built inputs")
			}
		})
	}
}

func TestProcessLaunchFailureLeavesNoPublishedChild(t *testing.T) {
	want := errors.New("launch refused by owner")
	pm := NewProcessManager(func(int) string { return "never-start.exe" })
	pm.Spawn = func(int, string, []string) (*exec.Cmd, error) { return nil, want }
	if e := pm.Swap(context.Background(), 1); !errors.Is(e, want) {
		t.Fatalf("lost launch failure: %v", e)
	}
	if pm.Current() != nil || len(pm.History()) != 0 || pm.ActiveAfter() != 0 {
		t.Fatal("failed launch was published")
	}
	missing := filepath.Join(t.TempDir(), "missing.exe")
	pm = NewProcessManager(func(int) string { return missing })
	if e := pm.Swap(context.Background(), 1); e == nil || !strings.Contains(e.Error(), "missing.exe") {
		t.Fatalf("missing binary failure lost: %v", e)
	}
	if pm.Current() != nil || pm.Shutdown() != nil {
		t.Fatal("failed process start left active state")
	}
}

func TestRealBuilderCancellationBoundariesAndOutputFailure(t *testing.T) {
	for _, when := range []string{"generate", "build", "output-directory"} {
		t.Run(when, func(t *testing.T) {
			dir := mkProj(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			buildCalls := 0
			b := &RealBuilder{ProjectDir: dir, GoExe: "owned-go", Generate: func(context.Context, string) (int, string) {
				if when == "generate" {
					cancel()
				}
				return 0, "generator completed"
			}, BuildFn: func(context.Context, string, []string) (int, string) {
				buildCalls++
				cancel()
				return 0, "compiler completed"
			}}
			if when == "output-directory" {
				mustWrite(t, filepath.Join(dir, ".goxml"), "user file blocks output directory")
			}
			r := b.Build(ctx, 1, nil)
			if r.OK {
				t.Fatalf("canceled/unwritable build accepted: %+v", r)
			}
			wantStage := "build"
			if when == "generate" {
				wantStage = "generate"
			}
			if r.Stage != wantStage {
				t.Fatalf("stage=%s want=%s", r.Stage, wantStage)
			}
			wantCalls := 0
			if when == "build" {
				wantCalls = 1
			}
			if buildCalls != wantCalls {
				t.Fatalf("build calls=%d want=%d", buildCalls, wantCalls)
			}
		})
	}
}
