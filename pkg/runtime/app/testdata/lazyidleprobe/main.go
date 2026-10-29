// lazyidleprobe is opt-in native-window evidence, never an M1 performance probe.
// App.Run stays on the main goroutine. One supervisor owns all JSON/exit paths.
// Forced process exit destroys the window; graceful destruction is NOT tested.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const idleQuiet = 250 * time.Millisecond

func parse(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("lazyidleprobe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Case, "case", "", "one of the four fixed cases")
	fs.StringVar(&o.Font, "font-source", "", "fixed CJK font path; no fallback success")
	fs.DurationVar(&o.Timeout, "timeout", 20*time.Second, "whole process observation bound, max 2m")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected positional arguments")
	}
	return o, o.validate()
}

// supervise owns the watchdog, settlement timer and exit decision. finish is
// injected in tests; production uses os.Exit and therefore never returns.
func supervise(o options, v *observer, runErr <-chan error, w io.Writer, finish func(int)) {
	deadline := time.NewTimer(o.Timeout)
	defer deadline.Stop()
	var settle *time.Timer
	var tick <-chan time.Time
	quiet := time.NewTimer(idleQuiet)
	quiet.Stop()
	defer quiet.Stop()
	var quietTick <-chan time.Time
	defer func() {
		if settle != nil {
			settle.Stop()
		}
	}()
	end := func(code int, reason string) {
		finish(emitBounded(w, newResult(o.Case, code, reason, v.snapshot()), 500*time.Millisecond))
	}
	for {
		select {
		case <-deadline.C:
			end(3, "whole-process timeout; native idle wake unverified: "+errorText(validateFacts(o.Case, v.snapshot())))
			return
		case err := <-runErr:
			end(1, "App.Run/prepare returned: "+errorText(err))
			return
		case <-v.signal:
			f := v.snapshot()
			if !f.FirstAt.IsZero() && !f.ReadEnteredAt.IsZero() && f.ReleasedAt.IsZero() {
				quiet.Reset(idleQuiet)
				quietTick = quiet.C
			}
			if !f.TerminalLayoutAt.IsZero() && settle == nil {
				settle = time.NewTimer(time.Millisecond)
				tick = settle.C
			}
		case <-quietTick:
			f := v.snapshot()
			if time.Since(f.LastLayoutAt) >= idleQuiet {
				v.release()
				quietTick = nil
			} else {
				quiet.Reset(time.Until(f.LastLayoutAt.Add(idleQuiet)))
			}
		case <-tick:
			f := v.snapshot()
			if err := validateFacts(o.Case, f); err == nil {
				// Independent review's minimal fix: a Run return made ready
				// during snapshot/validation takes priority over success.
				select {
				case err := <-runErr:
					end(1, "App.Run/prepare returned: "+errorText(err))
					return
				default:
				}
				end(0, "native Layout and production terminal evidence observed; no probe-driven invalidations")
				return
			}
			// Observe submission/callback publication after Layout returns. This timer
			// never calls Invalidate and cannot generate a frame or cure a missing wake.
			settle.Reset(time.Millisecond)
		}
	}
}
func main() {
	started := time.Now()
	o, err := parse(os.Args[1:])
	if err != nil {
		os.Exit(emitBounded(os.Stdout, newResult(o.Case, 2, errorText(err), facts{}), 500*time.Millisecond))
	}
	v := newObserver()
	runErr := make(chan error, 1)
	// Supervisor starts before constructor/Request, bounding even setup stalls.
	// lazy/owner initialization is completed before observing snapshots by setupReady.
	setupReady := make(chan struct{})
	go func() {
		timer := time.NewTimer(o.Timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			os.Exit(emitBounded(os.Stdout, newResult(o.Case, 3, "setup timeout", facts{}), 500*time.Millisecond))
		case <-setupReady:
			remaining := o.Timeout - time.Since(started)
			o.Timeout = remaining
			supervise(o, v, runErr, os.Stdout, os.Exit)
		}
	}()
	a, err := prepare(o, v)
	close(setupReady)
	if err != nil {
		runErr <- err
		select {}
	}
	runErr <- a.Run()
	select {}
}
