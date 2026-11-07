package watch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCoordR1OwnedConsoleHelper(t *testing.T) {
	if os.Getenv("COORD_STOP_HELPER") != "1" {
		return
	}
	time.Sleep(12 * time.Second)
}
func TestCoordR1ManagerStopTimeoutIncludesTreeCommand(t *testing.T) {
	realGo, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	bin := filepath.Join(root, "taskkill.exe")
	src := filepath.Join(root, "shim.go")
	code := `package main
import("os";"time")
func main(){
 os.WriteFile(os.Getenv("COORD_STOP_ENTERED"),[]byte("entered"),0600)
 end:=time.Now().Add(8*time.Second)
 for {if _,e:=os.Stat(os.Getenv("COORD_STOP_RELEASE"));e==nil{return};if time.Now().After(end){return};time.Sleep(time.Millisecond)}
}
`
	os.WriteFile(src, []byte(code), 0600)
	cmd := exec.Command(realGo, "build", "-o", bin, src)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("shim build: %v %s", e, out)
	}
	entered := filepath.Join(root, "entered")
	release := filepath.Join(root, "release")
	t.Setenv("COORD_STOP_ENTERED", entered)
	t.Setenv("COORD_STOP_RELEASE", release)
	t.Setenv("COORD_STOP_HELPER", "1")
	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	pm := NewProcessManager(func(int) string { return self })
	pm.Args = []string{"-test.run=^TestCoordR1OwnedConsoleHelper$"}
	pm.StopTimeout = 50 * time.Millisecond
	if e = pm.Swap(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer pm.Shutdown()
	t.Setenv("PATH", root)
	defer os.WriteFile(release, []byte("release"), 0600)
	done := make(chan error, 1)
	go func() { done <- pm.Shutdown() }()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, e = os.Stat(entered); e == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Shutdown did not invoke controlled taskkill")
		case <-tick.C:
		}
	}
	bounded := false
	select {
	case <-done:
		bounded = true
	case <-time.After(time.Second):
	}
	os.WriteFile(release, []byte("release"), 0600)
	if !bounded {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("owned process cleanup did not return after releasing tree helper")
		}
	}
	if !bounded {
		t.Fatal("StopTimeout=50ms did not bound taskkill: Shutdown waited for external helper release after >1s")
	}
}
