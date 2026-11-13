//go:build !nativewindow

package app

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/widget/material"
)

type coordR29RejectedHostWindow struct {
	*s2Window
	entered chan struct{}
	release chan struct{}
}

func (w *coordR29RejectedHostWindow) Event() event.Event {
	close(w.entered)
	<-w.release
	return gioapp.DestroyEvent{}
}

func TestCoordR29R2RejectedHostDoesNotStartEventSource(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	original := mainFunc
	var mainCalls atomic.Int32
	mainFunc = func() { mainCalls.Add(1) }
	t.Cleanup(func() { mainFunc = original })
	first := NewWindowHost()
	if err := first.Run(); err != nil {
		t.Fatal(err)
	}
	w := &coordR29RejectedHostWindow{s2Window: newS2Window(), entered: make(chan struct{}), release: make(chan struct{})}
	r2UseWindow(t, w)
	second := NewWindowHost()
	handle, openErr := second.Open("rejected", &s1View{}, material.NewTheme(), Config{})
	if handle != nil {
		defer func() { close(w.release); r2WaitDone(t, handle) }()
	}
	if err := second.Run(); !errors.Is(err, ErrGioMainAlreadyOwned) {
		t.Fatalf("second Run should reject: %v", err)
	}
	if mainCalls.Load() != 1 {
		t.Fatalf("unexpected Main count %d", mainCalls.Load())
	}
	if openErr != nil {
		return
	}
	select {
	case <-w.entered:
		t.Fatal("rejected second host entered its window Event source while first host owns Main")
	case <-time.After(350 * time.Millisecond):
	}
}
