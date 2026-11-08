package cli

import (
	"bytes"
	"context"
	"github.com/goxml/goxml/internal/devx/watch"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type devTestBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *devTestBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *devTestBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func devWait(t *testing.T, ctx context.Context, f func() bool, why string) {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !f() {
		select {
		case <-ctx.Done():
			t.Fatal(why)
		case <-tick.C:
		}
	}
}
func devFixture(t *testing.T) (string, string) {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	mod, e := os.ReadFile(filepath.Join(root, "go.mod"))
	if e != nil {
		t.Fatal(e)
	}
	text := strings.Replace(string(mod), "module github.com/goxml/goxml", "module example.com/devxcli", 1) + "\nreplace github.com/goxml/goxml => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	mustWrite(t, filepath.Join(dir, "go.mod"), text)
	sum, e := os.ReadFile(filepath.Join(root, "go.sum"))
	if e != nil {
		t.Fatal(e)
	}
	mustWrite(t, filepath.Join(dir, "go.sum"), string(sum))
	mustWrite(t, filepath.Join(dir, "ui", "doc.go"), "package ui\n")
	mustWrite(t, filepath.Join(dir, "ui", "home.gxml"), `<Goxml Version="1"><Page><Text Value="one"/></Page></Goxml>`)
	mustWrite(t, filepath.Join(dir, "cmd", "app", "main.go"), `package main
import("fmt";"io";"os";"strings";_ "example.com/devxcli/ui")
func main(){wd,_:=os.Getwd();fmt.Printf("DEVAPP CWD=%s ARGS=%s\n",wd,strings.Join(os.Args[1:],"|"));io.Copy(io.Discard,os.Stdin)}`)
	return root, dir
}
func devTool(t *testing.T, root string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "goxml.exe")
	cmd := exec.Command("go", "build", "-o", exe, "./cmd/goxml")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("tool build: %v %s", e, out)
	}
	return exe
}
func TestDevCommandsRejectInvalidInput(t *testing.T) {
	cases := [][]string{{"watch"}, {"watch", "x"}, {"watch", "x", "--entry", "../bad"}, {"watch", "x", "--entry", ".", "--poll-ms", "0"}, {"watch", "x", "--entry", ".", "--build-timeout-seconds", "-1"}, {"watch", " ", "--entry", "."}, {"watch", "x", "--entry"}, {"watch", "x", "--entry", ".", "--exclude-dir", "."}, {"watch", "x", "--unknown"}, {"preview"}, {"preview", "a", "b"}, {"preview", " "}, {"preview", "a", "--build-timeout-seconds", "0"}, {"preview", "a", "--unknown"}, {"preview", "a", "--", "bad"}}
	for i, args := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var out, err bytes.Buffer
			rc := runContextWithTool(context.Background(), args, &out, &err, "must-not-run.exe")
			if rc != ExitUsage || err.Len() == 0 {
				t.Fatalf("args=%v rc=%d out=%s err=%s", args, rc, out.String(), err.String())
			}
		})
	}
	for _, cmd := range []string{"watch", "preview"} {
		var out, err bytes.Buffer
		if rc := Run([]string{cmd, "--help"}, &out, &err); rc != 0 || !strings.Contains(out.String(), "用法") {
			t.Fatalf("help %s rc=%d", cmd, rc)
		}
	}
}
func TestDevWatchExclusionUsesRealSnapshot(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module local\n")
	mustWrite(t, filepath.Join(dir, "home.gxml"), "source")
	mustWrite(t, filepath.Join(dir, "logs", "run.txt"), "runtime output")
	o, e := parseWatch([]string{dir, "--entry", ".", "--exclude-dir", "logs"})
	if e != nil {
		t.Fatal(e)
	}
	snap, e := watch.Scan(makeWatchConfig(dir, o))
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := snap["logs/run.txt"]; ok {
		t.Fatal("explicit runtime-output exclusion ignored")
	}
	if _, ok := snap["home.gxml"]; !ok {
		t.Fatal("source excluded")
	}
}
func devPIDHandles(t *testing.T, ids []string) []windows.Handle {
	t.Helper()
	hs := []windows.Handle{}
	for _, id := range ids {
		n, e := strconv.Atoi(id)
		if e != nil {
			t.Fatal(e)
		}
		h, e := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(n))
		if e != nil {
			t.Fatal(e)
		}
		hs = append(hs, h)
	}
	t.Cleanup(func() {
		for _, h := range hs {
			windows.TerminateProcess(h, 1)
			windows.WaitForSingleObject(h, 1000)
			windows.CloseHandle(h)
		}
	})
	return hs
}
func TestDevWatchActualCLIRebuildFailureRecoveryAndCancel(t *testing.T) {
	root, dir := devFixture(t)
	tool := devTool(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var out, err devTestBuffer
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("watch stdout:\n%s\nwatch stderr:\n%s", out.String(), err.String())
		}
	})
	done := make(chan int, 1)
	go func() {
		done <- runContextWithTool(ctx, []string{"watch", dir, "--entry", "./cmd/app", "--poll-ms", "10", "--", "hello", "space arg"}, &out, &err, tool)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watch command did not finish")
		}
	})
	pidRE := regexp.MustCompile(`PID=(\d+)`)
	devWait(t, ctx, func() bool {
		return len(pidRE.FindAllStringSubmatch(out.String(), -1)) >= 1 && strings.Contains(out.String(), "DEVAPP CWD=")
	}, "first application not started: "+err.String())
	ids := pidRE.FindAllStringSubmatch(out.String(), -1)
	old := devPIDHandles(t, []string{ids[0][1]})[0]
	if !strings.Contains(out.String(), "CWD="+dir) || !strings.Contains(out.String(), "ARGS=hello|space arg") {
		t.Fatalf("working directory/argv mismatch: %s", out.String())
	}
	gen := filepath.Join(dir, "ui", "home_gen.go")
	before, e := os.ReadFile(gen)
	if e != nil {
		t.Fatal(e)
	}
	mustWrite(t, filepath.Join(dir, "ui", "home.gxml"), `<Goxml Version="1"><Page><Bogus/></Page></Goxml>`)
	devWait(t, ctx, func() bool { return strings.Contains(err.String(), "[generate]") }, "bad source did not report generation failure")
	if st, _ := windows.WaitForSingleObject(old, 0); st != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("old process stopped after generation failure")
	}
	after, e := os.ReadFile(gen)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("generation failure changed existing output")
	}
	mustWrite(t, filepath.Join(dir, "ui", "home.gxml"), `<Goxml Version="1"><Page><Text Value="recovered"/></Page></Goxml>`)
	devWait(t, ctx, func() bool { return len(pidRE.FindAllStringSubmatch(out.String(), -1)) >= 2 }, "recovery did not start new generation: "+err.String())
	ids = pidRE.FindAllStringSubmatch(out.String(), -1)
	latest := devPIDHandles(t, []string{ids[len(ids)-1][1]})[0]
	if ids[0][1] == ids[len(ids)-1][1] {
		t.Fatal("PID did not change")
	}
	if st, _ := windows.WaitForSingleObject(old, 0); st == uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("old generation survived handoff")
	}
	// A broken application entry is a build failure after successful generation;
	// it must preserve the currently serving process just like a frontend failure.
	badMain := filepath.Join(dir, "cmd", "app", "broken.go")
	mustWrite(t, badMain, "package main\nvar _ = missingApplicationSymbol\n")
	devWait(t, ctx, func() bool { return strings.Contains(err.String(), "[build]") }, "bad application did not report build failure")
	if st, _ := windows.WaitForSingleObject(latest, 0); st != uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("serving process stopped after application build failure")
	}
	previousCount := len(ids)
	if e := os.Remove(badMain); e != nil {
		t.Fatal(e)
	}
	devWait(t, ctx, func() bool { return len(pidRE.FindAllStringSubmatch(out.String(), -1)) > previousCount }, "entry repair did not restart application")
	ids = pidRE.FindAllStringSubmatch(out.String(), -1)
	prior := latest
	latest = devPIDHandles(t, []string{ids[len(ids)-1][1]})[0]
	if st, _ := windows.WaitForSingleObject(prior, 0); st == uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("entry recovery left old process running")
	}
	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("cancel rc=%d err=%s", rc, err.String())
		}
		done <- 0
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not finish")
	}
	if st, _ := windows.WaitForSingleObject(latest, 0); st == uint32(windows.WAIT_TIMEOUT) {
		t.Fatal("latest process survived CLI cancellation")
	}
}
func TestDevPreviewBuildOnlyAndStaticRefusal(t *testing.T) {
	root, dir := devFixture(t)
	file := filepath.Join(dir, "ui", "home.gxml")
	var out, err bytes.Buffer
	rc := runContextWithTool(context.Background(), []string{"preview", file, "--build-only", "--tool-root", root}, &out, &err, "unused.exe")
	if rc != 0 || !strings.Contains(out.String(), "已清理") {
		t.Fatalf("build-only rc=%d out=%s err=%s", rc, out.String(), err.String())
	}
	if _, e := os.Stat(filepath.Join(dir, "ui", "home_gen.go")); !os.IsNotExist(e) {
		t.Fatal("preview wrote generated file into user module")
	}
	mustWrite(t, file, `<Goxml Version="1"><Page><Button/></Page></Goxml>`)
	out.Reset()
	err.Reset()
	rc = runContextWithTool(context.Background(), []string{"preview", file, "--build-only", "--tool-root", root}, &out, &err, "unused.exe")
	if rc != 2 || !strings.Contains(err.String(), "PREVIEW-005") {
		t.Fatalf("interactive refusal rc=%d err=%s", rc, err.String())
	}
}
func TestDevCommandsCanceledBeforeWork(t *testing.T) {
	_, dir := devFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, args := range [][]string{{"watch", dir, "--entry", "."}, {"preview", filepath.Join(dir, "ui", "home.gxml"), "--build-only"}} {
		var out, err bytes.Buffer
		if rc := runContextWithTool(ctx, args, &out, &err, "must-not-start.exe"); rc != 0 {
			t.Fatalf("pre-cancel args=%v rc=%d err=%s", args, rc, err.String())
		}
	}
}
