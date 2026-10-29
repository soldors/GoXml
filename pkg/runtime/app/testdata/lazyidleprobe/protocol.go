// Internal preparation candidate; not a desktop DeepSeek delivery.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const protocol = "lazy-idle-window-v1"

var cases = []string{"bound-first", "early-no-notify", "early-prior-notify", "early-worker-failure"}

type options struct {
	Case, Font string
	Timeout    time.Duration
}

func (o options) validate() error {
	valid := false
	for _, c := range cases {
		valid = valid || o.Case == c
	}
	if !valid || o.Font == "" || o.Timeout <= 0 || o.Timeout > 2*time.Minute {
		return errors.New("require a fixed -case, -font-source, and 0 < -timeout <= 2m")
	}
	return nil
}

// All times are observation times, not performance phase attribution.
type facts struct {
	ErrorIsFontNotFound                                                           bool
	LastLayoutAt, LastLayoutBeforeRelease                                         time.Time
	LayoutsBeforeRelease                                                          int
	QuietMeasured                                                                 bool
	Layouts                                                                       int
	FirstAt, ReleasedAt, ReadEnteredAt, ReadFinishedAt, TerminalLayoutAt          time.Time
	FirstState, TerminalState, RawReadError, ReportError, EmbeddedError           string
	FirstTemp, TerminalTemp, FinalShaperSeen, FirstM1Measured                     bool
	PriorNotifyCount                                                              int
	PriorNotifyState                                                              string
	PriorNotifyAt                                                                 time.Time
	Changes                                                                       int
	Attempts                                                                      int
	NotifyLateBound                                                               bool
	CompletedAt, LoadedAt, SubmittedAt                                            time.Time
	CompletionMeasured, LoadMeasured, SubmissionMeasured, LayoutAfterSwapMeasured bool
	ReadCalls                                                                     int
}
type result struct {
	Protocol             string `json:"protocol"`
	Case                 string `json:"case"`
	NativeWindowVerified bool   `json:"nativeWindowVerified"`
	ExitCode             int    `json:"exitCode"`
	Reason               string `json:"reason"`
	Facts                facts  `json:"facts"`
}

func validateFacts(c string, f facts) error {
	if f.CompletionMeasured != !f.CompletedAt.IsZero() || f.LoadMeasured != !f.LoadedAt.IsZero() || f.SubmissionMeasured != !f.SubmittedAt.IsZero() || f.LayoutAfterSwapMeasured != f.SubmissionMeasured {
		return errors.New("measurement flags contradict production endpoints")
	}
	valid := false
	for _, known := range cases {
		valid = valid || c == known
	}
	if !valid {
		return errors.New("unknown case")
	}
	if !f.QuietMeasured || f.LayoutsBeforeRelease < 1 || f.Layouts <= f.LayoutsBeforeRelease || f.ReleasedAt.Sub(f.LastLayoutBeforeRelease) < idleQuiet {
		return errors.New("no measured quiet Layout interval before release")
	}
	if f.Layouts < 2 || f.FirstAt.IsZero() || f.FirstState != "loading" || !f.FirstTemp || !f.FirstM1Measured {
		return errors.New("first Layout must be Loading/temp with production M1 measured")
	}
	if f.ReadCalls != 1 || f.ReadEnteredAt.IsZero() || !f.ReleasedAt.After(f.FirstAt) || f.ReadFinishedAt.Before(f.ReleasedAt) || f.TerminalLayoutAt.Before(f.ReadFinishedAt) || f.TerminalLayoutAt.IsZero() {
		return errors.New("missing ordered first Layout -> release -> worker -> terminal Layout")
	}
	if f.Attempts != 1 || !f.CompletionMeasured || f.CompletedAt.Before(f.ReadFinishedAt) {
		return errors.New("terminal publication missing or unexpected retry")
	}
	if f.NotifyLateBound != (c != "bound-first") {
		return errors.New("binding path mismatch")
	}
	if c == "early-prior-notify" {
		if f.PriorNotifyCount != 1 || f.PriorNotifyState != "ready" || f.PriorNotifyAt.Before(f.CompletedAt) {
			return errors.New("prior non-window notify not preserved")
		}
	} else if f.PriorNotifyCount != 0 {
		return errors.New("unexpected prior callback")
	}
	if c == "early-worker-failure" {
		if f.TerminalState != "failed" || !f.ErrorIsFontNotFound || !f.TerminalTemp || f.FinalShaperSeen || f.Changes != 0 || f.ReportError == "" || f.RawReadError != controlledReadFailure.Error() || f.EmbeddedError == "" || f.LoadMeasured || f.SubmissionMeasured || f.LayoutAfterSwapMeasured {
			return errors.New("failure must retain temp and original error evidence without success measurements")
		}
	} else if f.TerminalState != "ready" || f.TerminalTemp || !f.FinalShaperSeen || f.Changes != 1 || f.ReportError != "" || f.RawReadError != "" || !f.LoadMeasured || !f.SubmissionMeasured || !f.LayoutAfterSwapMeasured || f.SubmittedAt.Before(f.TerminalLayoutAt) {
		return errors.New("Ready requires one Shaper change and production submission after terminal Layout")
	}
	return nil
}
func emit(w io.Writer, r result) int {
	if err := json.NewEncoder(w).Encode(r); err != nil {
		return 1
	}
	return r.ExitCode
}
func newResult(c string, code int, reason string, f facts) result {
	return result{protocol, c, code == 0, code, reason, f}
}

// Only the caller decides process exit, including a blocked output deadline.
func emitBounded(w io.Writer, r result, limit time.Duration) int {
	done := make(chan int, 1)
	go func() { done <- emit(w, r) }()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case code := <-done:
		return code
	case <-timer.C:
		return 1
	}
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
