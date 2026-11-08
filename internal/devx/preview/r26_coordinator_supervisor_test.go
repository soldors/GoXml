package preview

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCoordHostKillFailureIsBounded(t *testing.T) {
	root, e := resolveToolRoot("")
	if e != nil {
		t.Fatal(e)
	}
	helper := filepath.Join(t.TempDir(), "owned-console.exe")
	cmd := exec.Command("go", "build", "-o", helper, "./internal/devx/preview/testdata/consolehelper")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("helper build: %v %s", e, out)
	}
	runner, e := StartHost(helper, "", "sleep", "30")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { runner.cmd.Process.Kill() })
	t.Setenv("PATH", t.TempDir()) // taskkill unavailable: operation failure is not proof of exit.
	done := make(chan struct{})
	go func() { defer close(done); runner.Wait(20 * time.Millisecond) }()
	select {
	case <-done:
		return
	case <-time.After(time.Second):
		runner.cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("coordinator cleanup failed")
		}
		t.Fatal("Wait timeout hung after Stop swallowed taskkill failure; required direct owned-PID cleanup to return")
	}
}
