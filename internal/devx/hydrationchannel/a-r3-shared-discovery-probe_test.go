package hydrationchannel

import (
	"context"
	"errors"
	"testing"
)

func TestCoordR29R3SameNameRunsCanCaptureTheirOwnEndpoints(t *testing.T) {
	cfg := SessionConfig{ProjectDir: t.TempDir(), SessionID: "shared", AppID: "app", Schema: "v1", Token: "run-one-test-token"}
	first, err := NewSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	firstToken := cfg.Token
	cfg.Token = "run-two-test-token"
	second, err := NewSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	start := func(s *Session, label string) {
		t.Helper()
		ep, err := s.NewEndpointForGen(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(label), 1, nil }, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ep.Stop() })
	}
	read := func(s *Session, token, want string) {
		t.Helper()
		client, ok := s.NewClientFromPort(token)
		if !ok {
			t.Fatal("no discovery record for the running endpoint")
		}
		payload, err := client.Capture(context.Background(), 1)
		if errors.Is(err, ErrUnauthorized) {
			t.Fatalf("discovery sent the first run to the other live endpoint: %v", err)
		}
		if err != nil || string(payload) != want {
			t.Fatalf("capture failed: err=%v want=%s got=%s", err, want, payload)
		}
	}
	start(first, `{"run":1}`)
	read(first, firstToken, `{"run":1}`)
	start(second, `{"run":2}`)
	read(second, cfg.Token, `{"run":2}`)
	read(first, firstToken, `{"run":1}`)
}
