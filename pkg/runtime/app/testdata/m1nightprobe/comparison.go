package main

import (
	"fmt"
	"time"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

// Probe-only intervals; the production M1 contract remains unchanged.
const fontComparisonContract = "font-init-v1"

type fontComparison struct {
	start time.Time
	ready time.Time
}

func (c *fontComparison) observeReady(at time.Time, rep *theme.ProbeReport, loadErr error) error {
	if loadErr != nil {
		return fmt.Errorf("font comparison: load failed: %w", loadErr)
	}
	if rep == nil {
		return fmt.Errorf("font comparison: missing probe report")
	}
	if rep.Err != nil {
		return fmt.Errorf("font comparison: probe failed: %w", rep.Err)
	}
	if !rep.ProbeRan {
		return fmt.Errorf("font comparison: cold-provider premise failed (ProbeRan=false)")
	}
	if c.start.IsZero() || at.IsZero() || at.Before(c.start) {
		return fmt.Errorf("font comparison: missing or reversed readiness interval")
	}
	c.ready = at
	return nil
}

type fontComparisonResult struct {
	initAt, readyAt time.Time
	readyElapsed    time.Duration
	frameElapsed    time.Duration
}

func (c *fontComparison) observeLazy(rep theme.LoadReport) error {
	switch rep.State {
	case theme.LoadFailed:
		return fmt.Errorf("font comparison: lazy load failed: %v", rep.Err)
	case theme.LoadReady:
		return c.observeReady(rep.LoadedAt, rep.Probe, rep.Err)
	default:
		return nil // More frames do not turn Loading into a readiness observation.
	}
}

func (c fontComparison) result(firstFrame time.Time) (fontComparisonResult, error) {
	if c.start.IsZero() || c.ready.IsZero() || firstFrame.IsZero() ||
		c.ready.Before(c.start) || firstFrame.Before(c.start) {
		return fontComparisonResult{}, fmt.Errorf("font comparison: incomplete or reversed interval")
	}
	// Readiness may precede or follow the first frame. Zero durations remain
	// measured observations; they are not silently replaced or discarded.
	return fontComparisonResult{c.start, c.ready, c.ready.Sub(c.start), firstFrame.Sub(c.start)}, nil
}

func (r fontComparisonResult) write() {
	fmt.Printf("RESULT_DATA fontComparisonContract=%s\n", fontComparisonContract)
	fmt.Println("RESULT_DATA fontComparisonMeasured=true fontComparisonColdProvider=true")
	fmt.Printf("RESULT_DATA fontInitAtNs=%d fontReadyAtNs=%d\n", r.initAt.UnixNano(), r.readyAt.UnixNano())
	fmt.Printf("RESULT_DATA fontReadyElapsedNs=%d fontToFirstFrameElapsedNs=%d\n", r.readyElapsed.Nanoseconds(), r.frameElapsed.Nanoseconds())
}
