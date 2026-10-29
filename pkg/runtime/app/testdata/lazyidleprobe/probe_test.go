package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// Synthetic protocol fixtures are explicitly NOT native-window observations.
func validFixture(c string) facts {
	t := time.Unix(1, 0)
	f := facts{Layouts: 2, FirstAt: t, ReleasedAt: t.Add(time.Second), ReadEnteredAt: t.Add(-time.Second), ReadFinishedAt: t.Add(2 * time.Second), CompletedAt: t.Add(3 * time.Second), TerminalLayoutAt: t.Add(4 * time.Second), SubmittedAt: t.Add(5 * time.Second), LoadedAt: t.Add(3 * time.Second), FirstState: "loading", TerminalState: "ready", FirstTemp: true, FirstM1Measured: true, FinalShaperSeen: true, Changes: 1, Attempts: 1, NotifyLateBound: c != "bound-first", CompletionMeasured: true, LoadMeasured: true, SubmissionMeasured: true, LayoutAfterSwapMeasured: true, ReadCalls: 1}
	if c == "early-prior-notify" {
		// Prior callbacks are independent of the native window wake mechanism.
		f.PriorNotifyCount = 1
		f.PriorNotifyState = "ready"
		f.PriorNotifyAt = t.Add(3 * time.Second)
	}
	if c == "early-worker-failure" {
		f.ErrorIsFontNotFound = true
		f.TerminalState = "failed"
		f.TerminalTemp = true
		f.FinalShaperSeen = false
		f.Changes = 0
		f.RawReadError = controlledReadFailure.Error()
		f.EmbeddedError = "controlled fallback failure"
		f.ReportError = "font not found"
		f.LoadedAt = time.Time{}
		f.SubmittedAt = time.Time{}
		f.LoadMeasured = false
		f.SubmissionMeasured = false
		f.LayoutAfterSwapMeasured = false
	}
	f.LastLayoutBeforeRelease = f.FirstAt
	f.LastLayoutAt = f.TerminalLayoutAt
	f.LayoutsBeforeRelease = 1
	f.QuietMeasured = true
	return f
}
func TestProtocol(t *testing.T) {
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			f := validFixture(c)
			if err := validateFacts(c, f); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []func(*facts){func(f *facts) { f.FirstState = "ready" }, func(f *facts) { f.FirstTemp = false }, func(f *facts) { f.FirstM1Measured = false }, func(f *facts) { f.ReleasedAt = f.FirstAt }, func(f *facts) { f.TerminalLayoutAt = time.Time{} }, func(f *facts) { f.ReadCalls = 2 }, func(f *facts) { f.Attempts = 2 }, func(f *facts) { f.CompletionMeasured = false }, func(f *facts) { f.NotifyLateBound = !f.NotifyLateBound }} {
				copy := f
				bad(&copy)
				if validateFacts(c, copy) == nil {
					t.Fatal("accepted missing evidence")
				}
			}
			var b bytes.Buffer
			if emit(&b, newResult(c, 0, "synthetic fixture", f)) != 0 {
				t.Fatal("emit")
			}
			if strings.Contains(b.String(), "RESULT") || strings.Contains(b.String(), "ENV ") {
				t.Fatal("legacy protocol contamination")
			}
			var r result
			if err := json.Unmarshal(b.Bytes(), &r); err != nil || r.Protocol != protocol {
				t.Fatal(err)
			}
		})
	}
}
func TestParameterFailures(t *testing.T) {
	for _, args := range [][]string{nil, {"-case=bad"}, {"-case=bound-first", "-font-source=x", "-timeout=0s"}, {"-case=bound-first", "-font-source=x", "-timeout=3m"}, {"-unknown"}, {"-case=bound-first", "-font-source=x", "extra"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, c := range cases {
		if _, err := parse([]string{"-case=" + c, "-font-source=x"}); err != nil {
			t.Fatal(err)
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestOutputFailure(t *testing.T) {
	if emit(brokenWriter{}, newResult("bound-first", 0, "", facts{})) != 1 {
		t.Fatal("output failure must be nonzero")
	}
}

type blockedWriter struct{ gate chan struct{} }

func (b blockedWriter) Write([]byte) (int, error) { <-b.gate; return 0, io.ErrClosedPipe }
func TestOutputBlockIsBounded(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)
	if emitBounded(blockedWriter{gate}, newResult("bound-first", 0, "", facts{}), 5*time.Millisecond) != 1 {
		t.Fatal("blocked writer must fail")
	}
}
func TestSupervisorAcceptsFourSyntheticFixtures(t *testing.T) {
	for _, c := range cases {
		v := newObserver()
		v.f = validFixture(c)
		v.changed()
		var b bytes.Buffer
		got := -1
		supervise(options{Case: c, Timeout: time.Second}, v, make(chan error), &b, func(rc int) { got = rc })
		if got != 0 {
			t.Fatalf("%s: %s", c, b.String())
		}
	}
}

// Review-derived counterexample, now tests the real author source (no overlay).
// Channels fix the schedule: settle snapshot entered -> enqueue Run error ->
// allow snapshot/validation to complete. Timers only bound a broken test.
func TestPendingRunFailureDuringSettleSnapshot(t *testing.T) {
	v := newObserver()
	v.f = validFixture("bound-first")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	calls := 0 // exclusively the supervisor goroutine
	v.beforeSnapshot = func() {
		calls++
		if calls == 2 {
			close(entered)
			<-release
		}
	}
	errs, done := make(chan error, 1), make(chan int, 1)
	var b bytes.Buffer
	v.changed()
	go supervise(options{Case: "bound-first", Timeout: time.Second}, v, errs, &b, func(rc int) { done <- rc })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("settle snapshot was not entered")
	}
	original := "controlled original App.Run failure queued during settle snapshot"
	errs <- errors.New(original)
	once.Do(func() { close(release) })
	select {
	case rc := <-done:
		if rc != 1 {
			t.Fatalf("pending Run failure lost: rc=%d output=%s", rc, b.String())
		}
		var r result
		if err := json.Unmarshal(b.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.ExitCode != 1 || r.NativeWindowVerified || r.Reason != "App.Run/prepare returned: "+original {
			t.Fatalf("failure evidence lost: %s", b.String())
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish")
	}
}

func TestSupervisorWaitsForQuietBeforeReleasingWorker(t *testing.T) {
	v := newObserver()
	now := time.Now()
	v.f = facts{Layouts: 1, FirstAt: now, LastLayoutAt: now, ReadEnteredAt: now, FirstState: "loading", FirstTemp: true}
	v.changed()
	errs := make(chan error, 1)
	done := make(chan int, 1)
	go supervise(options{Case: "early-no-notify", Timeout: 2 * time.Second}, v, errs, io.Discard, func(code int) { done <- code })
	select {
	case <-v.gate:
		t.Fatal("released before quiet interval")
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-v.gate:
	case <-time.After(time.Second):
		t.Fatal("quiet observer did not release")
	}
	f := v.snapshot()
	if !f.QuietMeasured || f.ReleasedAt.Sub(f.LastLayoutBeforeRelease) < idleQuiet {
		t.Fatal("quiet measurement absent")
	}
	errs <- errors.New("test-only supervisor stop")
	if <-done != 1 {
		t.Fatal("supervisor did not stop with run error")
	}
}
func TestSupervisorTimeoutAndRunError(t *testing.T) {
	for _, fail := range []bool{false, true} {
		v := newObserver()
		errs := make(chan error, 1)
		want := 3
		if fail {
			errs <- errors.New("original run failure")
			want = 1
		}
		var b bytes.Buffer
		got := -1
		supervise(options{Case: "bound-first", Timeout: 5 * time.Millisecond}, v, errs, &b, func(rc int) { got = rc })
		if got != want {
			t.Fatalf("exit=%d want=%d", got, want)
		}
		if fail && !strings.Contains(b.String(), "original run failure") {
			t.Fatal("original error lost")
		}
	}
}

// Constructs production NewLazyApp but NEVER calls Run/Event. Direct Layout and
// manual Take below are mechanism checks only, not the native probe event path.
func TestHeadlessWorkerGateAndLayout(t *testing.T) {
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			v := newObserver()
			defer v.release()
			o := options{Case: c, Font: filepath.Join("..", "..", "..", "theme", "embedded", "GoxmlSansSC-Regular.otf"), Timeout: 5 * time.Second}
			_, err := prepare(o, v)
			if err != nil {
				t.Fatal(err)
			}
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(300, 90))}
			returned := make(chan struct{})
			go func() { v.Layout(gtx, v.lazy.Theme()); close(returned) }()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("Layout blocked on worker IO")
			}
			before := v.snapshot()
			if before.FirstState != "loading" || !before.FirstTemp || before.FirstM1Measured {
				t.Fatal("headless first Layout facts incorrect")
			}
			v.release()
			select {
			case <-v.lazy.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not complete")
			}
			rep := v.lazy.Report()
			if c == "early-worker-failure" {
				if rep.State != theme.LoadFailed || !errors.Is(rep.Err, contracts.ErrFontNotFound) || v.lazy.Take() != nil || v.snapshot().RawReadError != controlledReadFailure.Error() {
					t.Fatal("failure evidence lost")
				}
			} else {
				next := v.lazy.Take()
				if rep.State != theme.LoadReady || next == nil || next.Shaper == v.temp || v.lazy.Take() != nil {
					t.Fatalf("pending/Take incorrect: %s %v", rep.State, rep.Err)
				}
				th := material.NewTheme()
				th.Shaper = next.Shaper
				v.Layout(gtx, th)
				if !v.snapshot().FinalShaperSeen {
					t.Fatal("final Shaper not observed")
				}
			}
			if c == "early-prior-notify" {
				deadline := time.NewTimer(time.Second)
				defer deadline.Stop()
				for v.snapshot().PriorNotifyCount != 1 {
					select {
					case <-v.signal:
					case <-deadline.C:
						t.Fatal("prior callback lost")
					}
				}
			}
		})
	}
}
