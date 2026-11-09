package hydration

import (
	"bytes"
	"context"
	"errors"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
	"os"
	"testing"
	"time"
)

func TestCoordR27UncooperativeCaptureIsBounded(t *testing.T) {
	p := testProto(t, "bounded")
	p.CaptureTimeout = 20 * time.Millisecond
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) { close(entered); <-release; return []byte(`{}`), nil }, 1)
		done <- err
	}()
	<-entered
	select {
	case err := <-done:
		close(release)
		if err == nil {
			t.Fatal("timeout must reject")
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("CaptureTimeout expired but Snapshot still joined uncooperative callback")
	}
}
func TestCoordR27CanceledRestoreDoesNotApply(t *testing.T) {
	p := testProto(t, "cancel")
	if _, e := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"count":7}`), nil }, 1); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	applied, err := p.Restore(ctx, 1, func(context.Context, []byte) error { calls++; return nil })
	if applied || calls != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled restore applied state: applied=%v calls=%d err=%v", applied, calls, err)
	}
}
func TestCoordR27ForeignExistingFilePreserved(t *testing.T) {
	p := testProto(t, "session")
	if e := os.MkdirAll(p.Store.Dir(), 0700); e != nil {
		t.Fatal(e)
	}
	foreign, e := rst.NewEnvelope("other", "other-app", "foreign-session", 8, time.Now(), []byte(`{"foreign":true}`))
	if e != nil {
		t.Fatal(e)
	}
	old, _ := foreign.Encode()
	if e = os.WriteFile(p.Store.Path(), old, 0600); e != nil {
		t.Fatal(e)
	}
	own, _ := rst.NewEnvelope("demo.v1", "app-1", "session", 1, time.Now(), []byte(`{}`))
	raw, _ := own.Encode()
	err := p.Store.WriteAtomic(raw)
	after, _ := os.ReadFile(p.Store.Path())
	if err == nil || !bytes.Equal(old, after) {
		t.Fatalf("unproven existing state overwritten: err=%v preserved=%v", err, bytes.Equal(old, after))
	}
}
