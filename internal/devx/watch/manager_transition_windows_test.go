package watch

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/goxml/goxml/internal/devx/process"
)

func TestManagerSwapWaitsForConcurrentShutdownProof(t *testing.T) {
	job, err := process.NewJobOwnership()
	if err != nil {
		t.Fatal(err)
	}
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		process.CloseJobOwnership(job)
		t.Fatal(err)
	}
	defer stdinRead.Close()
	defer stdinWrite.Close()

	// Keep the root wait pending after the empty Job has been cleaned up.
	old := &Child{Gen: 1, job: job, done: make(chan struct{}), stdinWrite: stdinWrite}
	defer close(old.done)
	manager := NewProcessManager(func(int) string { return "unused.exe" })
	manager.StopTimeout = time.Second
	manager.current = old
	spawned := make(chan struct{}, 1)
	manager.Spawn = func(int, string, []string) (*exec.Cmd, error) {
		spawned <- struct{}{}
		return nil, errors.New("test observed new generation start")
	}

	shutdown := make(chan error, 1)
	go func() { shutdown <- manager.Shutdown() }()
	eof := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := stdinRead.Read(b[:])
		eof <- err
	}()
	select {
	case err := <-eof:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected stdin close to signal stop in progress: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not enter the stop path")
	}

	swap := make(chan error, 1)
	go func() { swap <- manager.Swap(context.Background(), 2) }()
	select {
	case err := <-swap:
		select {
		case <-spawned:
			t.Fatalf("started the next generation before the previous stop was proven: %v", err)
		default:
		}
		if err == nil || !strings.Contains(err.Error(), "StopTimeout") {
			t.Fatalf("swap must preserve the previous stop failure: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("swap did not resolve after the stop deadline")
	}
	if err := <-shutdown; err == nil || !strings.Contains(err.Error(), "StopTimeout") {
		t.Fatalf("shutdown must report the incomplete stop: %v", err)
	}
	if manager.Current() != old {
		t.Fatal("manager discarded the generation whose stop is unproven")
	}
	if err := manager.Shutdown(); err == nil || !strings.Contains(err.Error(), "StopTimeout") {
		t.Fatalf("repeated shutdown must preserve the recorded stop failure: %v", err)
	}
}
