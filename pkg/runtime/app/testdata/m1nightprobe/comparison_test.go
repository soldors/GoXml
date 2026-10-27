package main

import (
	"errors"
	"testing"
	"time"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

func TestComparisonAllowsReadyOnEitherSideOfFirstFrame(t *testing.T) {
	start := time.Unix(100, 0)
	for _, readyDelay := range []time.Duration{0, 10 * time.Millisecond, 30 * time.Millisecond} {
		c := fontComparison{start: start}
		if err := c.observeReady(start.Add(readyDelay), &theme.ProbeReport{ProbeRan: true}, nil); err != nil {
			t.Fatal(err)
		}
		got, err := c.result(start.Add(20 * time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		if got.readyElapsed != readyDelay || got.frameElapsed != 20*time.Millisecond {
			t.Fatalf("ready=%v frame=%v: intervals must share the initialization origin", got.readyElapsed, got.frameElapsed)
		}
	}
}

func TestComparisonRejectsFailedWarmAndMissingEvidence(t *testing.T) {
	start := time.Unix(100, 0)
	failed := errors.New("font unavailable")
	for _, tt := range []struct {
		name string
		at   time.Time
		rep  *theme.ProbeReport
		err  error
	}{
		{"load failure", start, &theme.ProbeReport{ProbeRan: true}, failed},
		{"report failure", start, &theme.ProbeReport{ProbeRan: true, Err: failed}, nil},
		{"missing report", start, nil, nil},
		{"warm provider", start, &theme.ProbeReport{}, nil},
		{"unobserved readiness", time.Time{}, &theme.ProbeReport{ProbeRan: true}, nil},
		{"reversed readiness", start.Add(-time.Nanosecond), &theme.ProbeReport{ProbeRan: true}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := fontComparison{start: start}
			if err := c.observeReady(tt.at, tt.rep, tt.err); err == nil {
				t.Fatal("incomplete/failed/warm evidence accepted")
			}
			if _, err := c.result(start.Add(time.Second)); err == nil {
				t.Fatal("rejected readiness must not become a measured sample")
			}
		})
	}
}

func TestComparisonRequiresBothEndpointsAndPreservesMeasuredZero(t *testing.T) {
	start := time.Unix(100, 0)
	for _, c := range []fontComparison{{}, {start: start}, {ready: start}} {
		if _, err := c.result(start); err == nil {
			t.Fatal("missing endpoint accepted")
		}
	}
	c := fontComparison{start: start, ready: start}
	if _, err := c.result(time.Time{}); err == nil {
		t.Fatal("missing first frame accepted")
	}
	if _, err := c.result(start.Add(-time.Nanosecond)); err == nil {
		t.Fatal("first frame before initialization accepted")
	}
	got, err := c.result(start)
	if err != nil || got.readyElapsed != 0 || got.frameElapsed != 0 {
		t.Fatalf("measured zero lost: %+v, %v", got, err)
	}
}

func TestComparisonLazyFramesCannotStandInForReadiness(t *testing.T) {
	start := time.Unix(100, 0)
	rep := theme.LoadReport{State: theme.LoadLoading, LoadedAt: start.Add(time.Second), Probe: &theme.ProbeReport{ProbeRan: true}}
	c := fontComparison{start: start}
	if err := c.observeLazy(rep); err != nil {
		t.Fatal(err)
	}
	if _, err := c.result(start.Add(time.Second)); err == nil {
		t.Fatal("Loading must not become a complete sample even if frames and other fields exist")
	}
	rep.State = theme.LoadFailed // Failed is invalid even if a broken report omits Err.
	if err := c.observeLazy(rep); err == nil {
		t.Fatal("failed state accepted as ready")
	}
	rep.State = theme.LoadReady
	if err := c.observeLazy(rep); err != nil {
		t.Fatal(err)
	}
	if _, err := c.result(start.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
}
