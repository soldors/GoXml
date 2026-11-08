package watch

import (
	"context"
	"github.com/goxml/goxml/internal/devx/process"
	"os/exec"
	"time"
)

type jobHandle = process.JobHandle

func startOwnedCommand(cmd *exec.Cmd) (jobHandle, error) { return process.StartOwnedCommand(cmd) }
func terminateAndWaitJob(job jobHandle, deadline time.Time) error {
	return process.TerminateAndWaitJob(job, deadline)
}
func stopJobTree(job jobHandle, pid int, deadline time.Time) error {
	return process.StopJobTree(job, pid, deadline)
}
func killProcessTree(ctx context.Context, pid int) error { return process.KillProcessTree(ctx, pid) }
