package watch

import (
	"context"
	"github.com/goxml/goxml/internal/devx/process"
	"time"
)

type jobHandle = process.JobHandle

func newJobOwnership() (jobHandle, error)             { return process.NewJobOwnership() }
func assignJobOwnership(job jobHandle, pid int) error { return process.AssignJobOwnership(job, pid) }
func closeJobOwnership(job jobHandle)                 { process.CloseJobOwnership(job) }
func terminateAndWaitJob(job jobHandle, deadline time.Time) error {
	return process.TerminateAndWaitJob(job, deadline)
}
func killProcessTree(ctx context.Context, pid int) error { return process.KillProcessTree(ctx, pid) }
