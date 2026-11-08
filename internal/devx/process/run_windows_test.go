package process

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Only this owned test binary is executed. No user application or native window.
func TestMain(m *testing.M) {
	switch os.Getenv("GOXML_OWNED_STREAM_HELPER") {
	case "zero":
		fmt.Fprint(os.Stdout, `{"GOOS":"windows"}`)
		fmt.Fprint(os.Stderr, "diagnostic on stderr")
		os.Exit(0)
	case "nonzero":
		fmt.Fprint(os.Stdout, "partial stdout")
		fmt.Fprint(os.Stderr, "compiler diagnostic")
		os.Exit(7)
	case "held":
		if p := os.Getenv("GOXML_LEAF_PID"); p != "" {
			os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0600)
		}
		time.Sleep(30 * time.Second) // owned child lifetime, never used for ordering
		os.Exit(0)
	case "pipe-root":
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), "GOXML_OWNED_STREAM_HELPER=held")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			panic(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			if _, err := os.Stat(os.Getenv("GOXML_LEAF_PID")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(3)
			}
			time.Sleep(time.Millisecond)
		}
		if release := os.Getenv("GOXML_ROOT_RELEASE"); release != "" {
			for {
				if _, err := os.Stat(release); err == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(4)
				}
				time.Sleep(time.Millisecond)
			}
		}
		fmt.Fprintln(os.Stdout, "ROOT-EXIT-WITH-LIVE-PIPE-CHILD")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRootExitDoesNotHideInheritedPipeOrLeakChild(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	pidfile := filepath.Join(t.TempDir(), "leaf.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan ExecResult, 1)
	go func() {
		done <- RunCommandTree(ctx, exe, []string{"-test.run=^$"}, t.TempDir(), []string{"GOXML_OWNED_STREAM_HELPER=pipe-root", "GOXML_LEAF_PID=" + pidfile}, 0)
	}()
	var ph windows.Handle
	deadline := time.Now().Add(3 * time.Second)
	for ph == 0 {
		if b, e := os.ReadFile(pidfile); e == nil {
			pid, e := strconv.Atoi(string(b))
			if e != nil {
				t.Fatal(e)
			}
			ph, e = windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
			if e != nil {
				t.Fatal(e)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owned child did not report PID")
		}
		time.Sleep(time.Millisecond)
	}
	defer windows.CloseHandle(ph)
	defer windows.TerminateProcess(ph, 1)
	select {
	case r := <-done:
		if r.RC != 0 || !errors.Is(r.Err, exec.ErrWaitDelay) || !strings.Contains(r.Stdout, "ROOT-EXIT") {
			t.Fatalf("lost pipe/exit evidence: %+v", r)
		}
		// Job termination and the final process-handle signal are asynchronous
		// Windows states. Wait boundedly for the latter, rather than confusing a
		// zero-time transient with a surviving thirty-second helper.
		if state, e := windows.WaitForSingleObject(ph, 1000); e != nil || state != windows.WAIT_OBJECT_0 {
			t.Fatalf("owned descendant survived root exit: state=%d err=%v", state, e)
		}
	case <-ctx.Done():
		t.Fatal("root-exit pipe cleanup unbounded")
	}
}

func TestJobOwnershipRejectsBadHandlesAndCleansOnce(t *testing.T) {
	job, e := NewJobOwnership()
	if e != nil {
		t.Fatal(e)
	}
	defer func() { CloseJobOwnership(job) }()
	if e := AssignJobOwnership(job, -1); e == nil {
		t.Fatal("invalid PID accepted as owned")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "GOXML_OWNED_STREAM_HELPER=held")
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("owned helper was not reaped")
		}
	})
	if e := AssignJobOwnership(windows.InvalidHandle, cmd.Process.Pid); e == nil {
		t.Fatal("invalid job accepted as ownership proof")
	}
	if e := TerminateAndWaitJob(windows.InvalidHandle, time.Now().Add(time.Second)); e == nil {
		t.Fatal("invalid job termination reported success")
	}
	if e := AssignJobOwnership(job, cmd.Process.Pid); e != nil {
		t.Fatal(e)
	}
	if e := TerminateAndWaitJob(job, time.Now().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	job = 0
	// Deferred cleanup clears its handle after one real close, never reuses a stale
	// numeric OS handle. Zero is the documented optional-ownership sentinel.
	CloseJobOwnership(job)
	if e := TerminateAndWaitJob(job, time.Now()); e != nil {
		t.Fatalf("empty cleanup sentinel: %v", e)
	}
}

func TestCommandStreamsDoNotCorruptJSONAndExitIsObserved(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"zero", "nonzero"} {
		r := RunCommandTree(context.Background(), exe, []string{"-test.run=^$"}, t.TempDir(), []string{"GOXML_OWNED_STREAM_HELPER=" + mode}, 0)
		wantRC, wantOut, wantErr := 0, `{"GOOS":"windows"}`, "diagnostic on stderr"
		if mode == "nonzero" {
			wantRC, wantOut, wantErr = 7, "partial stdout", "compiler diagnostic"
		}
		if r.Err != nil || r.Cancelled || r.RC != wantRC || r.Stdout != wantOut || r.Stderr != wantErr || !strings.Contains(r.Output, wantOut) || !strings.Contains(r.Output, wantErr) {
			t.Fatalf("mode=%s lost observed exit/stream separation: %+v", mode, r)
		}
	}
}

func TestCommandRefusalsStartNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.exe")
	if r := RunCommandTree(nil, missing, nil, "", nil, 0); r.Err == nil || r.RC != -1 {
		t.Fatalf("nil context: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := RunCommandTree(ctx, missing, nil, "", nil, 0); !r.Cancelled || !errors.Is(r.Err, context.Canceled) || r.Output != "" {
		t.Fatalf("pre-cancel: %+v", r)
	}
	if r := RunCommandTree(context.Background(), missing, nil, "", nil, 0); r.Err == nil || r.Cancelled || r.RC != -1 || r.Output != "" {
		t.Fatalf("start failure: %+v", r)
	}
}

func TestIncompleteReapAndMissingExitStatusAreNeverSuccess(t *testing.T) {
	// A cleanup deadline does not prove that an unavailable waiter completed.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err, joined := joinCommand(ctx, make(chan error)); joined || !errors.Is(err, context.Canceled) {
		t.Fatalf("unavailable waiter reported reaped: joined=%v err=%v", joined, err)
	}
	// A start/wait failure without a ProcessState must retain an unknown/nonzero
	// status, rather than being mistaken for a successful command.
	cmd := exec.Command(filepath.Join(t.TempDir(), "never-started.exe"))
	if rc := exitCodeOf(cmd, os.ErrNotExist); rc != -1 {
		t.Fatalf("unknown failed command reported successful status %d", rc)
	}
}

func TestExitWitnessWaitRejectsTimeoutAndInvalidHandle(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "GOXML_OWNED_STREAM_HELPER=held")
	job, err := StartOwnedCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer TerminateAndWaitJob(job, time.Now().Add(3*time.Second))
	handles, err := jobProcessHandles(job)
	if err != nil || len(handles) != 1 {
		t.Fatalf("owned root not captured: handles=%d err=%v", len(handles), err)
	}
	defer func() {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
	}()
	if err := waitProcessHandles(handles, time.Now().Add(time.Millisecond)); err == nil {
		t.Fatal("live process reported exited")
	}
	if err := waitProcessHandles(handles, time.Time{}); err == nil {
		t.Fatal("expired witness deadline reported proof")
	}
	if err := waitProcessHandles([]windows.Handle{windows.InvalidHandle}, time.Now().Add(time.Second)); err == nil {
		t.Fatal("invalid process witness reported proof")
	}
}
