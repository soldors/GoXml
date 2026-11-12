package hydrationchannel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCoordinatorConnectedCancellationRejectsPayload(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) {
		close(entered)
		<-release
		return []byte(`{"value":"arrived-after-cancel"}`), 1, nil
	}, "coord-session", "coord-app", "v1", "fixture-token", 1)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer e.Stop()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	c, err := NewClient(addr, "coord-session", "coord-app", "v1", "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		payload []byte
		err     error
	}
	done := make(chan result, 1)
	go func() {
		b, err := c.Capture(ctx, 1)
		done <- result{b, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("capture did not reach the real endpoint")
	}
	cancel()
	unblock()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || len(got.payload) != 0 {
			t.Fatalf("canceled capture: want context.Canceled and no payload, got err=%v payloadLen=%d", got.err, len(got.payload))
		}
	case <-time.After(time.Second):
		t.Fatal("connected capture did not return promptly after cancellation")
	}
}
