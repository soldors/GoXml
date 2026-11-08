package process

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// StartOwnedCommand does not let user code run until job ownership is proven.
// The caller is responsible for exactly one Wait after a successful return.
func StartOwnedCommand(cmd *exec.Cmd) (JobHandle, error) {
	return (startupOps{NewJobOwnership, AssignJobOwnership, resumeOwnedCommand}).start(cmd)
}

// Value-scoped OS ports allow failure-path verification with real suspended
// processes. Production always uses the real APIs above; no global test modes.
type startupOps struct {
	create func() (JobHandle, error)
	assign func(JobHandle, int) error
	resume func(*exec.Cmd) error
}

func (o startupOps) start(cmd *exec.Cmd) (JobHandle, error) {
	job, err := o.create()
	if err != nil {
		return 0, err
	}
	prepareSuspended(cmd)
	if cmd.WaitDelay == 0 {
		cmd.WaitDelay = 250 * time.Millisecond
	}
	if err = cmd.Start(); err != nil {
		CloseJobOwnership(job)
		return 0, err
	}
	err = o.assign(job, cmd.Process.Pid)
	if err == nil {
		err = o.resume(cmd)
	}
	if err == nil {
		return job, nil
	}
	// No caller owns Wait on this error path. A failed ownership/resume attempt
	// kills only the exact just-created process and its own job, then reaps it.
	kerr := cmd.Process.Kill()
	deadline := time.Now().Add(commandReapGrace)
	jerr := TerminateAndWaitJob(job, deadline)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	_, joined := joinCommand(ctx, done)
	if !joined {
		err = errors.Join(err, errors.New("failed to reap suspended command"))
	}
	return 0, errors.Join(err, kerr, jerr)
}
