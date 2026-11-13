//go:build !nativewindow

package app

import (
	"errors"
	"testing"
	"time"
)

func TestCoordR29R4ZeroWindowExitAllWaitsForStartupVerdict(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	previous := mainFunc
	mainFunc = func() {}
	t.Cleanup(func() { mainFunc = previous })
	if err := NewWindowHost().Run(); err != nil {
		t.Fatal(err)
	}

	candidate := NewWindowHost()
	gioMain.mu.Lock()
	locked := true
	defer func() {
		if locked {
			gioMain.mu.Unlock()
		}
	}()
	run := make(chan error, 1)
	go func() { run <- candidate.Run() }()
	r1SpinUntil(t, "pending ownership", func() bool {
		candidate.mu.Lock()
		defer candidate.mu.Unlock()
		return candidate.state == hostStarting
	})

	candidate.ExitAll(7)
	var early *HostResult
	select {
	case <-candidate.finished:
		result := candidate.Wait()
		early = &result
	default:
	}
	gioMain.mu.Unlock()
	locked = false

	select {
	case err := <-run:
		if !errors.Is(err, ErrGioMainAlreadyOwned) {
			t.Fatalf("unexpected Run result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not finish after releasing the ownership lock")
	}
	final := candidate.Wait()
	if early != nil && early.Rejected != final.Rejected {
		t.Fatalf("Wait exposed a terminal result before the startup verdict: early.Rejected=%v final.Rejected=%v", early.Rejected, final.Rejected)
	}
}
