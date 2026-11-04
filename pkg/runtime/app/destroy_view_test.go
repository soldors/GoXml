package app

import (
	"context"
	"errors"
	"testing"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/router"
)

func TestDestroyEventShutsDownWrappedRouter(t *testing.T) {
	loops := []struct {
		name string
		run  func(*lifecycleWindow, View, *material.Theme, <-chan struct{})
	}{
		{"app-loop", func(w *lifecycleWindow, v View, th *material.Theme, _ <-chan struct{}) {
			a := &App{view: v, th: th}
			a.loopEvents(w.Event)
		}},
		{"integration-loop", func(w *lifecycleWindow, v View, th *material.Theme, stop <-chan struct{}) {
			if err := runWindowEvents(w, v, th, stop, nil); err != nil {
				t.Error(err)
			}
		}},
	}
	for _, loop := range loops {
		t.Run(loop.name, func(t *testing.T) {
			r := router.New()
			other := router.New()
			page := &lifecycleView{}
			var pageCtx context.Context
			if err := r.Register("/page", func(ctx context.Context) (router.View, error) {
				pageCtx = ctx
				return page, nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := r.Navigate("/page"); err != nil {
				t.Fatal(err)
			}
			calls := 0
			view, err := WithOnDestroy(r.View(), func() {
				calls++
				r.Shutdown()
			})
			if err != nil {
				t.Fatal(err)
			}
			w := &lifecycleWindow{events: make(chan event.Event, 3)}
			w.events <- synthFrameEvent(&frameRecorder{})
			w.events <- gioapp.DestroyEvent{}
			w.events <- gioapp.DestroyEvent{}
			stop := make(chan struct{})
			defer close(stop)
			loop.run(w, view, material.NewTheme(), stop)
			if calls != 1 || len(w.events) != 1 || page.frames != 1 {
				t.Fatalf("callbacks=%d remainingEvents=%d frames=%d", calls, len(w.events), page.frames)
			}
			if !errors.Is(r.Context().Err(), context.Canceled) ||
				pageCtx == nil || !errors.Is(pageCtx.Err(), context.Canceled) ||
				other.Context().Err() != nil {
				t.Fatal("destroy must cancel only the wrapped Router and its page")
			}
			if err := r.Navigate("/page"); !errors.Is(err, context.Canceled) {
				t.Fatalf("Navigate after DestroyEvent = %v", err)
			}
			r.Dispatch(func() { t.Error("closed Router ran a dispatch") })
			if got := r.DroppedCount(); got != 2 {
				t.Fatalf("drop count = %d, want 2", got)
			}
		})
	}
}

func TestWithOnDestroyRejectsInvalidOrRepeatedWrapping(t *testing.T) {
	page := &lifecycleView{}
	callback := func() {}
	wrapped, err := WithOnDestroy(page, callback)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		view View
		fn   func()
	}{
		{nil, callback},
		{page, nil},
		{wrapped, callback},
	} {
		if view, err := WithOnDestroy(tc.view, tc.fn); view != nil || err == nil {
			t.Fatal("invalid destroy callback registration was accepted")
		}
	}
}
