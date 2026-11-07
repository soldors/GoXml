package watch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// ExecResult separates the observed command exit from cancellation/cleanup errors.
type ExecResult struct {
	RC        int
	Output    string
	Cancelled bool
	Err       error
}

const commandReapGrace = 3 * time.Second

// commandOutput remains safe even when an OS cleanup failure prevents joining a copier.
type commandOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *commandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *commandOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }

// RunCommandTree runs an owned tool tree. timeout limits execution (0 inherits
// ctx). Cancellation has a separate bounded cleanup grace; cleanup errors remain
// visible and never imply that all descendants exited.
func RunCommandTree(ctx context.Context, exe string, args []string, dir string, extraEnv []string, timeout time.Duration) ExecResult {
	if ctx == nil {
		return ExecResult{RC: -1, Err: errors.New("nil command context")}
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return ExecResult{RC: -1, Cancelled: true, Err: err}
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	var buf commandOutput
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	// A descendant retaining an inherited pipe must not block root-exit detection.
	cmd.WaitDelay = 250 * time.Millisecond
	job, err := newJobOwnership()
	if err != nil {
		return ExecResult{RC: -1, Err: err}
	}
	if err = cmd.Start(); err != nil {
		closeJobOwnership(job)
		return ExecResult{RC: -1, Err: err}
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	if err = assignJobOwnership(job, cmd.Process.Pid); err != nil {
		closeJobOwnership(job)
		kerr := cmd.Process.Kill()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), commandReapGrace)
		defer cancel()
		_, joined := joinCommand(cleanupCtx, waitCh)
		if !joined {
			err = errors.Join(err, errors.New("failed to reap unassigned command"))
		}
		return ExecResult{RC: -1, Output: buf.String(), Err: errors.Join(err, kerr)}
	}
	select {
	case werr := <-waitCh:
		jerr := terminateAndWaitJob(job, time.Now().Add(commandReapGrace))
		var infra error
		if werr != nil {
			var ee *exec.ExitError
			if !errors.As(werr, &ee) {
				infra = werr
			}
		}
		return ExecResult{RC: exitCodeOf(cmd, werr), Output: buf.String(), Err: errors.Join(infra, jerr)}
	case <-ctx.Done():
		cleanupCtx, cancel := context.WithTimeout(context.Background(), commandReapGrace)
		defer cancel()
		// Use a live cleanup context, not the already-cancelled execution context.
		terr := killProcessTree(cleanupCtx, cmd.Process.Pid)
		deadline, _ := cleanupCtx.Deadline()
		jerr := terminateAndWaitJob(job, deadline)
		_, joined := joinCommand(cleanupCtx, waitCh)
		if !joined {
			kerr := cmd.Process.Kill()
			finalCtx, finalCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			_, joined = joinCommand(finalCtx, waitCh)
			finalCancel()
			if !joined {
				jerr = errors.Join(jerr, kerr, fmt.Errorf("command root not reaped within cleanup grace"))
			}
		}
		return ExecResult{RC: -1, Output: buf.String(), Cancelled: true, Err: errors.Join(ctx.Err(), terr, jerr)}
	}
}

func joinCommand(ctx context.Context, ch <-chan error) (error, bool) {
	select {
	case err := <-ch:
		return err, true
	case <-ctx.Done():
		return ctx.Err(), false
	}
}
func exitCodeOf(cmd *exec.Cmd, werr error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if werr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) {
		return ee.ExitCode()
	}
	return -1
}
