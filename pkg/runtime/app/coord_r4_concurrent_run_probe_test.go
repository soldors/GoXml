//go:build !nativewindow

package app

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestCoordR29R3StartingRunReportsSameOwnershipVerdict(t *testing.T) {
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
	first := make(chan error, 1)
	go func() { first <- candidate.Run() }()
	deadline := time.After(3 * time.Second)
	for {
		candidate.mu.Lock()
		starting := candidate.state == hostStarting
		candidate.mu.Unlock()
		if starting {
			break
		}
		select {
		case <-deadline:
			t.Fatal("first call did not reach ownership check")
		default:
			runtime.Gosched()
		}
	}
	second := make(chan error, 1)
	go func() { second <- candidate.Run() }()
	var early bool
	var repeated error
	select {
	case repeated = <-second:
		early = true
	case <-time.After(100 * time.Millisecond):
	}
	gioMain.mu.Unlock()
	locked = false
	select {
	case err := <-first:
		if !errors.Is(err, ErrGioMainAlreadyOwned) {
			t.Fatalf("first result: %v", err)
		}
	case <-deadline:
		t.Fatal("first Run did not complete")
	}
	if !early {
		select {
		case repeated = <-second:
		case <-deadline:
			t.Fatal("second Run did not complete")
		}
	}
	if !errors.Is(repeated, ErrGioMainAlreadyOwned) {
		t.Fatalf("repeat Run reported %v for a host whose initial Run was rejected", repeated)
	}
}
