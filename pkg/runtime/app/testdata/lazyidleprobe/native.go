package main

import (
	"errors"
	"os"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/contracts"
	runtimeapp "github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

var controlledReadFailure = errors.New("lazyidleprobe: controlled candidate read failure")

type unavailableEmbedded struct{}

func (unavailableEmbedded) Load() ([]byte, string, error) {
	return nil, "", errors.New("lazyidleprobe: controlled embedded fallback unavailable")
}

// Every observation is mutex owned. Signals only announce a changed snapshot.
// Layout never waits for worker IO and never drives another frame.
type observer struct {
	// Test scheduling seam, installed before supervise starts and otherwise nil.
	// Derived from independent review's snapshot hook; no event-loop behavior.
	beforeSnapshot func()
	mu             sync.Mutex
	f              facts
	signal         chan struct{}
	gate           chan struct{}
	releaseOnce    sync.Once
	temp, previous *text.Shaper
	lazy           *theme.Lazy
	owner          *runtimeapp.App
}

func newObserver() *observer {
	return &observer{signal: make(chan struct{}, 1), gate: make(chan struct{})}
}
func (v *observer) changed() {
	select {
	case v.signal <- struct{}{}:
	default:
	}
}
func (v *observer) release() {
	v.releaseOnce.Do(func() {
		v.mu.Lock()
		v.f.ReleasedAt = time.Now()
		v.f.LastLayoutBeforeRelease = v.f.LastLayoutAt
		v.f.LayoutsBeforeRelease = v.f.Layouts
		v.f.QuietMeasured = !v.f.LastLayoutAt.IsZero() && v.f.ReleasedAt.Sub(v.f.LastLayoutAt) >= idleQuiet
		v.mu.Unlock()
		close(v.gate)
	})
}
func (v *observer) read(path string, fail bool) ([]byte, error) {
	v.mu.Lock()
	v.f.ReadCalls++
	v.f.ReadEnteredAt = time.Now()
	v.mu.Unlock()
	v.changed()
	<-v.gate // only this worker waits; never View.Layout.
	var data []byte
	var err error
	if fail {
		err = controlledReadFailure
	} else {
		data, err = os.ReadFile(path)
	}
	v.mu.Lock()
	v.f.ReadFinishedAt = time.Now()
	v.f.RawReadError = errorText(err)
	v.mu.Unlock()
	v.changed()
	return data, err
}
func (v *observer) priorNotify() {
	rep := v.lazy.Report()
	v.mu.Lock()
	v.f.PriorNotifyCount++
	v.f.PriorNotifyState = rep.State.String()
	v.f.PriorNotifyAt = time.Now()
	v.mu.Unlock()
	v.changed()
}
func (v *observer) snapshot() facts {
	if v.beforeSnapshot != nil {
		v.beforeSnapshot()
	}
	v.mu.Lock()
	f := v.f
	v.mu.Unlock()
	if v.lazy != nil {
		rep := v.lazy.Report()
		f.Attempts = rep.Attempts
		f.NotifyLateBound = rep.NotifyLateBound
		f.CompletedAt = rep.CompletedAt
		f.LoadedAt = rep.LoadedAt
		f.SubmittedAt = rep.FrameSubmittedAt
		f.ReportError = errorText(rep.Err)
		f.ErrorIsFontNotFound = errors.Is(rep.Err, contracts.ErrFontNotFound)
		f.CompletionMeasured = !rep.CompletedAt.IsZero()
		f.LoadMeasured = !rep.LoadedAt.IsZero()
		f.SubmissionMeasured = !rep.FrameSubmittedAt.IsZero()
		// A zero duration is valid: completed submission is the public completion evidence.
		f.LayoutAfterSwapMeasured = f.SubmissionMeasured
		if rep.Probe != nil && rep.Probe.Embedded != nil {
			f.EmbeddedError = errorText(rep.Probe.Embedded.Err)
		}
	}
	return f
}
func (v *observer) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	state := v.lazy.State().String()
	now := time.Now()
	v.mu.Lock()
	v.f.Layouts++
	if v.f.Layouts == 1 {
		v.f.FirstAt = now
		v.f.FirstState = state
		v.f.FirstTemp = th.Shaper == v.temp
		v.f.FirstM1Measured = v.owner.FirstFrame().Measured()
	}
	if v.previous != nil && v.previous != th.Shaper {
		v.f.Changes++
	}
	v.previous = th.Shaper
	if ((state == "ready" && th.Shaper != v.temp) || state == "failed") && v.f.TerminalLayoutAt.IsZero() {
		v.f.TerminalLayoutAt = now
		v.f.TerminalState = state
		v.f.TerminalTemp = th.Shaper == v.temp
		v.f.FinalShaperSeen = th.Shaper != v.temp
	}
	v.mu.Unlock()
	dims := material.Body1(th, "Lazy idle wake probe").Layout(gtx)
	v.mu.Lock()
	v.f.LastLayoutAt = time.Now()
	v.mu.Unlock()
	v.changed()
	return dims
}
func prepare(o options, v *observer) (*runtimeapp.App, error) {
	temp := material.NewTheme()
	v.temp = temp.Shaper
	provider := theme.NewProvider(theme.ProviderOptions{
		Candidates: []string{o.Font}, Timeout: 2 * time.Minute, Embedded: unavailableEmbedded{},
		Read: func(path string) ([]byte, error) { return v.read(path, o.Case == "early-worker-failure") },
	})
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: provider, Temp: temp})
	if err != nil {
		return nil, err
	}
	v.lazy = lz
	if o.Case == "early-prior-notify" && !lz.SetNotify(v.priorNotify) {
		return nil, errors.New("prior notify binding failed")
	}
	if o.Case != "bound-first" {
		lz.Request()
	}
	a, err := runtimeapp.NewLazyApp(v, lz, runtimeapp.Config{Title: "BUG-013 lazy idle / " + o.Case, Width: unit.Dp(300), Height: unit.Dp(90), StrictFontFailure: true})
	v.owner = a
	return a, err
}
