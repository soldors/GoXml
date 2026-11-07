package watchadapter_test

import (
	"context"
	"fmt"
	"github.com/goxml/goxml/internal/devx/watchadapter"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestCoordR2GenerateFromUserWorkingDirectory(t *testing.T) {
	proj := makeRealProject(t)
	rc, detail := watchadapter.CLIGenerate(context.Background(), proj)
	if rc != 0 {
		t.Fatalf("repo cwd control failed: rc=%d %s", rc, detail)
	}
	t.Chdir(proj)
	rc, detail = watchadapter.CLIGenerate(context.Background(), proj)
	if rc != 0 {
		t.Fatalf("same valid project failed from normal user cwd: rc=%d %s", rc, detail)
	}
}
func TestCoordR2CancelStopsActualGeneratorDescendants(t *testing.T) {
	proj := makeRealProject(t)
	realGo, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "toolexec.go")
	bin := filepath.Join(dir, "toolexec.exe")
	code := `package main
import("fmt";"os";"os/exec";"time")
func main(){
 selected:=false
 for i:=1;i+1<len(os.Args);i++{if os.Args[i]=="-p"&&os.Args[i+1]=="example.com/watchdemo/ui"{selected=true}}
 if selected{
  os.WriteFile(os.Getenv("COORD_R2_ENTERED"),[]byte(fmt.Sprintf("%d %d",os.Getpid(),os.Getppid())),0600)
  end:=time.Now().Add(20*time.Second)
  for{if _,e:=os.Stat(os.Getenv("COORD_R2_RELEASE"));e==nil{break};if time.Now().After(end){os.Exit(88)};time.Sleep(time.Millisecond)}
 }
 if len(os.Args)<2{os.Exit(87)}
 cmd:=exec.Command(os.Args[1],os.Args[2:]...);cmd.Stdout=os.Stdout;cmd.Stderr=os.Stderr;cmd.Stdin=os.Stdin
 if e:=cmd.Run();e!=nil{if ee,ok:=e.(*exec.ExitError);ok{os.Exit(ee.ExitCode())};os.Exit(1)}
}
`
	if e = os.WriteFile(src, []byte(code), 0600); e != nil {
		t.Fatal(e)
	}
	build := exec.Command(realGo, "build", "-o", bin, src)
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("toolexec build: %v %s", e, out)
	}
	// Force a cold compile for this exact owned package, not a cached check.
	nonce := fmt.Sprintf("package ui\nvar CoordNonce = %d\n", time.Now().UnixNano())
	if e = os.WriteFile(filepath.Join(proj, "ui", "coord_nonce.go"), []byte(nonce), 0600); e != nil {
		t.Fatal(e)
	}
	entered := filepath.Join(dir, "entered")
	release := filepath.Join(dir, "release")
	t.Setenv("COORD_R2_ENTERED", entered)
	t.Setenv("COORD_R2_RELEASE", release)
	t.Setenv("GOFLAGS", strings.Join([]string{"-mod=readonly", strconv.Quote("-toolexec=" + bin)}, " "))
	handles := []windows.Handle{}
	t.Cleanup(func() {
		// Handles were opened only after verifying the complete parent chain leads
		// to this test process; no process-name kill or unowned PID is used.
		for i := len(handles) - 1; i >= 0; i-- {
			windows.TerminateProcess(handles[i], 1)
		}
		for _, p := range handles {
			windows.WaitForSingleObject(p, 1000)
			windows.CloseHandle(p)
		}
		os.WriteFile(release, []byte("release"), 0600)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() { rc, detail := watchadapter.CLIGenerate(ctx, proj); done <- fmt.Sprintf("rc=%d %s", rc, detail) }()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.After(45 * time.Second)
	var fields []string
	for {
		if data, e := os.ReadFile(entered); e == nil {
			fields = strings.Fields(string(data))
			if len(fields) == 2 {
				break
			}
		}
		select {
		case detail := <-done:
			t.Fatalf("generator ended before controlled compile: %s", detail)
		case <-deadline:
			t.Fatal("actual generator did not enter owned package compile")
		case <-tick.C:
		}
	}
	pidValue, e := strconv.Atoi(fields[0])
	if e != nil {
		t.Fatal(e)
	}
	snap, e := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		t.Fatal(e)
	}
	parents := map[uint32]uint32{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for e = windows.Process32First(snap, &entry); e == nil; e = windows.Process32Next(snap, &entry) {
		parents[entry.ProcessID] = entry.ParentProcessID
	}
	windows.CloseHandle(snap)
	ids := []uint32{}
	pid := uint32(pidValue)
	for n := 0; n < 10 && pid != uint32(os.Getpid()) && pid != 0; n++ {
		ids = append(ids, pid)
		pid = parents[pid]
	}
	if pid != uint32(os.Getpid()) {
		t.Fatalf("refuse to touch chain not proven owned by this test: %v parent=%d", ids, pid)
	}
	if len(ids) < 3 {
		t.Fatalf("control did not include toolexec -> Go build -> generator (optional go run): %v", ids)
	}
	for _, pid := range ids {
		p, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
		if e != nil {
			t.Fatal(e)
		}
		handles = append(handles, p)
	}
	if _, e = os.Stat(filepath.Join(proj, "ui", "home_gen.go")); !os.IsNotExist(e) {
		t.Fatal("cold control already has generated file")
	}
	cancel()
	select {
	case detail := <-done:
		t.Logf("adapter returned: %s", detail)
	case <-time.After(6 * time.Second):
		t.Fatal("CLIGenerate cancellation did not return within bound")
	}
	active := 0
	for _, p := range handles {
		if state, _ := windows.WaitForSingleObject(p, 0); state == uint32(windows.WAIT_TIMEOUT) {
			active++
		}
	}
	t.Logf("after cancellation return: active owned descendants=%d pids=%v", active, ids)
	os.WriteFile(release, []byte("release"), 0600)
	lateWrite := false
	lateDeadline := time.After(5 * time.Second)
lateLoop:
	for {
		if _, e = os.Stat(filepath.Join(proj, "ui", "home_gen.go")); e == nil {
			lateWrite = true
			break
		}
		select {
		case <-lateDeadline:
			break lateLoop
		case <-tick.C:
		}
	}
	if active != 0 || lateWrite {
		t.Fatalf("cancellation returned with %d owned descendants alive; generated file committed after return=%v", active, lateWrite)
	}
}
