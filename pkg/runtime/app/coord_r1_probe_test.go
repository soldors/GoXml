//go:build !nativewindow

package app

import (
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"
)

type coordR29InitWindow struct {
	*s2Window
	entered  chan struct{}
	initDone chan struct{}
	first    atomic.Bool
	early    atomic.Bool
}

func (w *coordR29InitWindow) Event() event.Event {
	if w.first.CompareAndSwap(false, true) {
		close(w.entered)
		<-w.initDone
		return synthFrameEvent(&frameRecorder{})
	}
	return w.s2Window.Event()
}

func (w *coordR29InitWindow) Perform(a system.Action) {
	select {
	case <-w.initDone:
	default:
		w.early.Store(true)
	}
	w.s2Window.Perform(a)
}

func TestCoordR29HostCloseWaitsForWindowDriver(t *testing.T) {
	w := &coordR29InitWindow{s2Window: newS2Window(), entered: make(chan struct{}), initDone: make(chan struct{})}
	orig := hostWindowFactory
	hostWindowFactory = func(Config) windowEvents { return w }
	t.Cleanup(func() { hostWindowFactory = orig })
	host := NewWindowHost()
	handle, err := host.Open("test", &s1View{}, material.NewTheme(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	// R3：Open 只注册，显式 Run 才启动事件泵（Main 用桩立即返回）。
	r3StartHost(t, host)
	<-w.entered
	err = handle.Close()
	close(w.initDone)
	select {
	case <-handle.Done():
	case <-time.After(time.Second):
		t.Fatal("close request was lost after the first event")
	}
	if err != nil || w.early.Load() || w.closes.Load() != 1 {
		t.Fatalf("expected exactly one close after the first event; early=%t closes=%d err=%v", w.early.Load(), w.closes.Load(), err)
	}
}

func TestCoordR29HostCloseAfterDestroyDoesNotTouchBackend(t *testing.T) {
	w := newS2Window()
	s2UseFakeWindows(t, map[string]*s2Window{"test": w})
	host := NewWindowHost()
	handle, err := host.Open("test", &s1View{}, material.NewTheme(), Config{Title: "test"})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host) // R3：注册与启动分离
	w.events <- gioapp.DestroyEvent{}
	<-handle.Done()
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if n := w.closes.Load(); n != 0 {
		t.Fatalf("Close reached a backend after its DestroyEvent: %d calls", n)
	}
}
