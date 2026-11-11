package hydrationchannel

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoordinatorRealEndpointMismatchError(t *testing.T) {
	want := []byte(`{"value":"only-the-right-generation"}`)
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) {
		return want, 2, nil
	}, "coord-session", "coord-app", "v1", "fixture-token", 1)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	c, err := NewClient(addr, "coord-session", "coord-app", "v1", "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if got, err := c.Capture(ctx, 2); err != nil || string(got) != string(want) {
		t.Fatalf("matching generation control failed: err=%v payload=%q", err, got)
	}
	got, err := c.Capture(ctx, 1)
	if !errors.Is(err, ErrMismatch) || len(got) != 0 {
		t.Fatalf("wrong generation: want ErrMismatch and no payload, got err=%v payloadLen=%d", err, len(got))
	}
}
