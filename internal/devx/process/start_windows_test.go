package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnedStartupFailuresNeverRunMainAndReapCreatedProcess(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, stage := range []string{"create-job", "assign-job", "resume-thread"} {
		t.Run(stage, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "main-executed.pid")
			cmd := exec.Command(exe, "-test.run=^$")
			cmd.Env = append(os.Environ(), "GOXML_OWNED_STREAM_HELPER=held", "GOXML_LEAF_PID="+marker)
			want := errors.New("OS " + stage + " failure")
			ops := startupOps{NewJobOwnership, AssignJobOwnership, resumeOwnedCommand}
			switch stage {
			case "create-job":
				ops.create = func() (JobHandle, error) { return 0, want }
			case "assign-job":
				ops.assign = func(JobHandle, int) error { return want }
			case "resume-thread":
				ops.resume = func(*exec.Cmd) error { return want }
			}
			h, err := ops.start(cmd)
			if h != 0 || !errors.Is(err, want) {
				t.Fatalf("failure/ownership lost: job=%d err=%v", h, err)
			}
			if _, e := os.Stat(marker); !os.IsNotExist(e) {
				t.Fatalf("user main ran before ownership/startup proof: %v", e)
			}
			if stage == "create-job" {
				if cmd.Process != nil {
					t.Fatal("created process after job creation failed")
				}
			} else if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
				t.Fatal("suspended startup failure was not reaped")
			}
		})
	}
}

func TestExpiredJobCleanupDeadlineIsNotReportedAsProven(t *testing.T) {
	job, e := NewJobOwnership()
	if e != nil {
		t.Fatal(e)
	}
	// The function owns/closes this handle even on its deadline error path.
	if e := TerminateAndWaitJob(job, time.Time{}); e == nil {
		t.Fatal("expired proof deadline reported success")
	}
}
