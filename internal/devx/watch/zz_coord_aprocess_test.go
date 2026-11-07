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

func TestCoordProcessManagerCountsDescendants(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	bin := filepath.Join(dir, "owned-family.exe")
	pidfile := filepath.Join(dir, "leaf.pid")
	const program = `package main
import("os";"os/exec";"strconv";"io";"time")
func main(){if os.Args[1]=="leaf"{os.WriteFile(os.Args[2],[]byte(strconv.Itoa(os.Getpid())),0600);time.Sleep(60*time.Second);return};self,_:=os.Executable();c:=exec.Command(self,"leaf",os.Args[2]);if e:=c.Start();e!=nil{os.Exit(2)};io.Copy(io.Discard,os.Stdin)}
`
	os.WriteFile(src, []byte(program), 0600)
	if out, e := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); e != nil {
		t.Fatalf("helper build: %v %s", e, out)
	}
	pm := NewProcessManager(func(int) string { return bin })
	pm.Args = []string{"parent", pidfile}
	pm.StopTimeout = time.Second
	if e := pm.Swap(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pm.Shutdown() })
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(pidfile); e == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("helper did not report owned descendant PID")
	}
	handle, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		windows.TerminateProcess(handle, 1)
		windows.WaitForSingleObject(handle, 3000)
		windows.CloseHandle(handle)
	})
	if e := pm.Shutdown(); e != nil {
		t.Fatal(e)
	}
	state, e := windows.WaitForSingleObject(handle, 0)
	if e != nil {
		t.Fatal(e)
	}
	if state == uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("Shutdown reports ActiveAfter=%d but owned leaf PID=%d is still alive", pm.ActiveAfter(), pid)
	}
}
