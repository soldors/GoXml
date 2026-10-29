package theme

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/widget/material"
)

func awaitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("attempt completion was not published")
	}
}

func TestLazyDoneAllTerminalPaths(t *testing.T) {
	for _, name := range []string{"default-ready", "default-failed", "selection-ready", "selection-parse-failed", "selection-source-failed"} {
		t.Run(name, func(t *testing.T) {
			var lz *Lazy
			var err error
			want := LoadReady
			switch name {
			case "default-ready":
				lz, _ = newLazySystemHit(t)
			case "default-failed":
				lz, _ = newLazyAllFail(t)
				want = LoadFailed
			default:
				sel := selTestSel()
				if name == "selection-parse-failed" {
					sel.Source.Bytes = []byte("invalid font")
					want = LoadFailed
				}
				if name == "selection-source-failed" {
					sel.Source = FontSource{Path: filepath.Join(t.TempDir(), "absent.ttf")}
					want = LoadFailed
				}
				lz, err = NewLazyWithSelection(sel, LazyOptions{Temp: material.NewTheme()})
				if err != nil {
					t.Fatal(err)
				}
			}
			done := lz.Done()
			if done == nil || lz.Done() != done || lz.State() != LoadUnloaded || lz.Theme() != nil {
				t.Fatal("Done must be stable and side-effect free before Request")
			}
			select {
			case <-done:
				t.Fatal("unrequested attempt completed")
			default:
			}
			notified := make(chan LoadReport, 1)
			if !lz.SetNotify(func() { notified <- lz.Report() }) {
				t.Fatal("Done must not consume the notification binding")
			}
			lz.Request()
			awaitDone(t, done)
			rep := lz.Report()
			if rep.State != want || rep.CompletedAt.IsZero() || rep.Attempts != 1 || (rep.Err != nil) != (want == LoadFailed) {
				t.Fatalf("completion preceded terminal publication: %+v", rep)
			}
			if (lz.Take() != nil) != (want == LoadReady) || lz.Take() != nil {
				t.Fatal("completion must preserve one-shot pending delivery")
			}
			select {
			case r := <-notified:
				if r.State != want {
					t.Fatal("original callback observed nonterminal state")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("original callback lost or deadlocked")
			}
			if lz.SetNotify(func() {}) || lz.Done() != done {
				t.Fatal("late SetNotify semantics or completion snapshot changed")
			}
			if want == LoadReady {
				lz.Request()
				if lz.Done() != done || lz.Report().Attempts != 1 {
					t.Fatal("Ready Request must remain a no-op")
				}
			}
		})
	}
}

func TestLazyDoneRetryAndConcurrentReaders(t *testing.T) {
	lz, _, release := newLazyBlocked(t)
	first := lz.Done()
	lz.Request()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lz.Done() != first {
				t.Error("concurrent readers received different channels")
			}
		}()
	}
	wg.Wait()
	select {
	case <-first:
		t.Fatal("blocked attempt signalled completion")
	default:
	}
	release()
	awaitDone(t, first)
	if lz.Report().Attempts != 1 || lz.State() != LoadFailed {
		t.Fatal("Done must not implicitly retry")
	}
	lz.Request()
	second := lz.Done()
	if second == first {
		t.Fatal("explicit retry reused previous closed channel")
	}
	awaitDone(t, second)
	awaitDone(t, first)
	if lz.Report().Attempts != 2 || lz.State() != LoadFailed {
		t.Fatal("retry terminal report incorrect")
	}
}

func TestLazyDoneAfterCompletion(t *testing.T) {
	lz, _ := newLazySystemHit(t)
	lz.Request()
	waitState(t, lz, LoadReady, 10*time.Second)
	awaitDone(t, lz.Done())
}
