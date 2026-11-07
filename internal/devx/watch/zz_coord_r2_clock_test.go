package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type coordR2ClockBuilder struct {
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (b *coordR2ClockBuilder) Build(ctx context.Context, gen int, paths []string) GenerationResult {
	b.calls++
	if b.calls == 1 {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return GenerationResult{Stage: "canceled"}
		}
	}
	return GenerationResult{OK: true, Gen: gen}
}

type coordR2ClockObserver struct{ events chan bool }

func (o *coordR2ClockObserver) OnGeneration(_ int, _ GenerationResult, p bool) { o.events <- p }
func TestCoordR2DelayedRunStillPublishesLatest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gxml")
	if e := os.WriteFile(path, []byte("base"), 0600); e != nil {
		t.Fatal(e)
	}
	b := &coordR2ClockBuilder{entered: make(chan struct{}), release: make(chan struct{})}
	o := &coordR2ClockObserver{events: make(chan bool, 8)}
	w, e := New(Config{ProjectDir: dir, PollIntervalMS: 5, DebounceMS: 1}, b, &fakeSwapper{}, o)
	if e != nil {
		t.Fatal(e)
	}
	// The field documents deterministic clock override. Represent a Watch
	// created earlier and started later without an hour of wall-clock sleep.
	w.runStart = time.Now().Add(-time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Run did not exit")
		}
	}()
	if e = os.WriteFile(path, []byte("first"), 0600); e != nil {
		t.Fatal(e)
	}
	select {
	case <-b.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Run never entered first build")
	}
	if e = os.WriteFile(path, []byte("second"), 0600); e != nil {
		t.Fatal(e)
	}
	close(b.release)
	select {
	case published := <-o.events:
		if published {
			t.Fatal("stale generation published")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no stale result")
	}
	select {
	case published := <-o.events:
		if !published {
			t.Fatal("latest stable generation was not published")
		}
	case <-time.After(time.Second):
		t.Fatal("latest stable input stalled: stale Note uses time since New, while Run Tick uses time since Run (1h clock offset)")
	}
}
