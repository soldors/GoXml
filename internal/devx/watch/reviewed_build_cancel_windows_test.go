package watch

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReviewedRealBuildCancellationOwnsDescendants(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	exe := filepath.Join(dir, "owned-build-wrapper.exe")
	pidfile := filepath.Join(dir, "pid")
	release := filepath.Join(dir, "release")
	late := filepath.Join(dir, "late")
	code := `package main
import("os";"os/exec";"strconv";"time")
func main(){if os.Args[1]=="leaf"{os.WriteFile(os.Args[2],[]byte(strconv.Itoa(os.Getpid())),0600);end:=time.Now().Add(8*time.Second);for{if _,e:=os.Stat(os.Args[3]);e==nil{break};if time.Now().After(end){return};time.Sleep(time.Millisecond)};os.WriteFile(os.Args[4],[]byte("late"),0600);return};self,_:=os.Executable();c:=exec.Command(self,append([]string{"leaf"},os.Args[2:]...)...);c.Stdout=os.Stdout;c.Stderr=os.Stderr;c.Run()}`
	os.WriteFile(src, []byte(code), 0600)
	if out, e := exec.Command("go", "build", "-o", exe, src).CombinedOutput(); e != nil {
		t.Fatalf("build wrapper: %v %s", e, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { rc, _ := realGoBuild(ctx, dir, []string{exe, "parent", pidfile, release, late}); done <- rc }()
	deadline := time.After(4 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	pid := 0
	for {
		if b, e := os.ReadFile(pidfile); e == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			if pid > 0 {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatal("no owned build descendant")
		case <-tick.C:
		}
	}
	handle, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		windows.TerminateProcess(handle, 1)
		windows.WaitForSingleObject(handle, 1000)
		windows.CloseHandle(handle)
	})
	cancel()
	bounded := true
	rc := 0
	select {
	case rc = <-done:
	case <-time.After(time.Second):
		bounded = false
		os.WriteFile(release, []byte("release"), 0600)
		select {
		case rc = <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("build did not join after controlled release")
		}
	}
	state, e := windows.WaitForSingleObject(handle, 0)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(release, []byte("release"), 0600)
	_, lateErr := os.Stat(late)
	if !bounded || rc == 0 || state == uint32(windows.WAIT_TIMEOUT) || lateErr == nil {
		t.Fatalf("build cancellation: bounded=%v rc=%d descendantAlive=%v lateWrite=%v", bounded, rc, state == uint32(windows.WAIT_TIMEOUT), lateErr == nil)
	}
}
