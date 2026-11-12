package watch

import (
	"github.com/goxml/goxml/internal/devx/process"
	"os/exec"
	"time"
)

type jobHandle = process.JobHandle

func startOwnedCommand(cmd *exec.Cmd) (jobHandle, error) { return process.StartOwnedCommand(cmd) }
func terminateAndWaitJob(job jobHandle, deadline time.Time) error {
	return process.TerminateAndWaitJob(job, deadline)
}
