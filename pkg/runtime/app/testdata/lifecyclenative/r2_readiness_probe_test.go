//go:build nativewindow

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentCloseBeforeReadyMarkerIsEarly(t *testing.T) {
	r := &recorder{scenario: scenarioShutdownFirst}
	path := filepath.Join(t.TempDir(), "marker.txt")
	r.noteShutdownObservation(0, false)
	r.writeMarker(path, "background-shutdown:unmounts=0 cleanup=false")

	// Model the exact handoff: the background goroutine marks the flag
	// and is then descheduled before writing the durable readiness marker.
	r.setReadyWritten()
	early, reason := r.checkCloseArrival()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !early {
		t.Fatalf("close accepted before ready-to-close was recorded: early=%t reason=%q markers=%q", early, reason, contents)
	}
}
