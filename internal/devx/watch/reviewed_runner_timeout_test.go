package watch

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestReviewedOwnedBlockingConsole(t *testing.T) {
	if os.Getenv("COORD_R3_BLOCK_HELPER") != "1" {
		return
	}
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	<-timer.C
}
func TestReviewedCommandTimeoutIsHonored(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan ExecResult, 1)
	go func() {
		done <- RunCommandTree(ctx, exe, []string{"-test.run=^TestReviewedOwnedBlockingConsole$"}, "", []string{"COORD_R3_BLOCK_HELPER=1"}, 40*time.Millisecond)
	}()
	select {
	case r := <-done:
		if !r.Cancelled || !errors.Is(r.Err, context.DeadlineExceeded) {
			t.Fatalf("timeout must be observed as DeadlineExceeded, got %+v", r)
		}
	case <-time.After(800 * time.Millisecond):
		cancel()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Fatal("cleanup after explicit cancellation did not finish")
		}
		t.Fatal("timeout=40ms did not end RunCommandTree; required external cancellation after 800ms")
	}
}
