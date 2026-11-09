package app

import (
	"errors"
	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/pkg/runtime/theme"
	"testing"
	"time"
)

func TestCoordR27LazyProcessDoesNotResolveSyncTheme(t *testing.T) {
	exitStubMain(t)
	orig := newThemeWithReport
	defer func() { newThemeWithReport = orig }()
	syncCalls := 0
	newThemeWithReport = func() (*material.Theme, *theme.ProbeReport) {
		syncCalls++
		return nil, &theme.ProbeReport{Err: errors.New("unused synchronous provider failed")}
	}
	lz := newFailingLazy(t, nil)
	var result ExitResult
	RunSingleWindowProcess(&countingView{}, Config{StrictFontFailure: false}, ProcessOptions{Lazy: lz, next: func() event.Event { return gioapp.DestroyEvent{} }, Observer: func(r ExitResult) { result = r }, exit: func(int) {}})
	if syncCalls != 0 || result.Code != 0 {
		t.Fatalf("lazy process consulted synchronous font path before construction: calls=%d result=%+v", syncCalls, result)
	}
}

type coordInitWindow struct {
	entered, release chan struct{}
	unsafe           chan struct{}
}

func (w *coordInitWindow) Event() event.Event {
	close(w.entered)
	<-w.release
	return gioapp.DestroyEvent{}
}
func (w *coordInitWindow) Perform(system.Action) {
	select {
	case w.unsafe <- struct{}{}:
	default:
	}
}
func (w *coordInitWindow) Invalidate() {}
func TestCoordR27EarlyStopDoesNotPerformDuringDriverInit(t *testing.T) {
	w := &coordInitWindow{entered: make(chan struct{}), release: make(chan struct{}), unsafe: make(chan struct{}, 1)}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runWindowEvents(w, &countingView{}, material.NewTheme(), stop, nil) }()
	<-w.entered
	close(stop)
	bad := false
	select {
	case <-w.unsafe:
		bad = true
	case <-time.After(100 * time.Millisecond):
	}
	close(w.release)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("controlled loop did not return")
	}
	if bad {
		t.Fatal("stop watcher called Perform while Event was still initializing driver; same boundary as declared baseline race")
	}
}
