package hydration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestCoordR27R1CancellationAfterCaptureKeepsState(t *testing.T) {
	p := testProto(t, "cancel-after")
	if _, e := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"x":1}`), nil }, 1); e != nil {
		t.Fatal(e)
	}
	old, e := os.ReadFile(p.Store.Path())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Now = func() time.Time { cancel(); return time.Unix(2, 0) } // deterministic cancellation between capture and envelope/commit
	_, err := p.Snapshot(ctx, func(context.Context) ([]byte, error) { return []byte(`{"x":2}`), nil }, 2)
	after, _ := os.ReadFile(p.Store.Path())
	if !errors.Is(err, context.Canceled) || !bytes.Equal(old, after) {
		t.Fatalf("canceled before commit but state changed: err=%v preserved=%v", err, bytes.Equal(old, after))
	}
}
func TestCoordR27R1ConcurrentCaptureBoundIsRaceFree(t *testing.T) {
	p := testProto(t, "concurrent")
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			_, _ = p.Snapshot(ctx, func(c context.Context) ([]byte, error) { <-c.Done(); return nil, c.Err() }, 1)
		}()
	}
	close(start)
	wg.Wait()
}
