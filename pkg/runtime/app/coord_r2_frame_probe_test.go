//go:build !nativewindow

package app

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

type coordR29FrameSource struct {
	*s2Window
	calls         atomic.Int32
	frameDone     atomic.Bool
	readAhead     atomic.Bool
	nextRead      chan struct{}
	frameReturned chan struct{}
}

func (s *coordR29FrameSource) Event() event.Event {
	switch s.calls.Add(1) {
	case 1:
		e := synthFrameEvent(&frameRecorder{})
		e.Frame = func(*op.Ops) { s.frameDone.Store(true); close(s.frameReturned) }
		return e
	case 2:
		s.readAhead.Store(!s.frameDone.Load())
		close(s.nextRead)
		<-s.frameReturned
		return gioapp.DestroyEvent{}
	default:
		panic("unexpected event")
	}
}

type coordR29HoldFrame struct {
	s       *coordR29FrameSource
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (v *coordR29HoldFrame) Layout(_ layout.Context, _ *material.Theme) layout.Dimensions {
	v.once.Do(func() { close(v.started); <-v.release })
	return layout.Dimensions{}
}

func TestCoordR29R1HostDoesNotReadNextEventBeforeSubmittingFrame(t *testing.T) {
	src := &coordR29FrameSource{s2Window: newS2Window(), nextRead: make(chan struct{}), frameReturned: make(chan struct{})}
	v := &coordR29HoldFrame{s: src, started: make(chan struct{}), release: make(chan struct{})}
	orig := hostWindowFactory
	hostWindowFactory = func(Config) windowEvents { return src }
	t.Cleanup(func() { hostWindowFactory = orig })
	h := NewWindowHost()
	wh, err := h.Open("A", v, material.NewTheme(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, h) // R3：Open 只注册，Run 才启动事件泵（断言未改）
	<-v.started
	// Hold Layout briefly, so an independent reader has an opportunity to
	// consume the next backend event while the first frame remains outstanding.
	select {
	case <-src.nextRead:
	case <-time.After(100 * time.Millisecond):
	}
	close(v.release)
	<-wh.Done()
	if src.readAhead.Load() {
		t.Fatal("next Event was entered before the previous Frame callback ran")
	}
}
