package app

import (
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/widget/material"
)

type lifecycleWindow struct {
	events      chan event.Event
	actions     chan system.Action
	invalidates atomic.Int32
}

func (w *lifecycleWindow) Event() event.Event      { return <-w.events }
func (w *lifecycleWindow) Perform(a system.Action) { w.actions <- a }
func (w *lifecycleWindow) Invalidate()             { w.invalidates.Add(1) }

func TestWindowStopWatcherLifecycle(t *testing.T) {
	for _, order := range []string{"destroy", "stop", "both"} {
		t.Run(order, func(t *testing.T) {
			w := &lifecycleWindow{actions: make(chan system.Action, 1)}
			stop, loopDone := make(chan struct{}), make(chan struct{})
			if order != "stop" {
				close(loopDone)
			}
			if order != "destroy" {
				close(stop)
			}
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				watchWindowStop(w, stop, loopDone)
			}()
			select {
			case <-exited:
			case <-time.After(time.Second):
				// Release the old implementation too, preserving a clean red run.
				close(stop)
				<-exited
				t.Fatal("window destroyed while its stop watcher remained blocked")
			}
			if order == "stop" {
				select {
				case got := <-w.actions:
					if got != system.ActionClose || w.invalidates.Load() != 1 {
						t.Fatalf("stop did not request close and invalidate: action=%v invalidates=%d", got, w.invalidates.Load())
					}
				default:
					t.Fatal("stop did not request window close")
				}
			} else if len(w.actions) != 0 || w.invalidates.Load() != 0 {
				t.Fatal("stop watcher called the window after event-loop exit")
			}
		})
	}
}

type lifecycleView struct {
	frames int
}

func (v *lifecycleView) Layout(layout.Context, *material.Theme) layout.Dimensions {
	v.frames++
	return layout.Dimensions{}
}

func TestRunWindowEventsHeadlessLifecycle(t *testing.T) {
	w := &lifecycleWindow{
		events:  make(chan event.Event, 2),
		actions: make(chan system.Action, 1),
	}
	v := &lifecycleView{}
	r := &frameRecorder{}
	w.events <- synthFrameEvent(r)
	w.events <- gioapp.DestroyEvent{}
	stop := make(chan struct{})
	defer close(stop)
	invCalled := false
	if err := runWindowEvents(w, v, material.NewTheme(), stop, func(inv Invalidator) {
		if len(w.events) != 2 {
			t.Fatal("invalidator delivered after the event loop started")
		}
		invCalled = true
		inv.Invalidate()
	}); err != nil {
		t.Fatal(err)
	}
	if !invCalled || v.frames != 1 || r.submittedAt().IsZero() || w.invalidates.Load() != 1 {
		t.Fatalf("event pump did not preserve delivery/layout/submission: invalidator=%v frames=%d submitted=%v invalidates=%d",
			invCalled, v.frames, r.submittedAt(), w.invalidates.Load())
	}
}
