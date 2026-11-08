package preview

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGeneratedHostEventLoopPreservesFrameAndDestroyError(t *testing.T) {
	proj := copyProject(t)
	r, err := Run(Options{File: filepath.Join(proj, "static_basic.gxml"), SkipHost: true})
	if err != nil || !r.OK() {
		t.Fatalf("prepare: %v %+v", err, r)
	}
	root, err := resolveToolRoot("")
	if err != nil {
		t.Fatal(err)
	}
	host, err := arrangeHost(r, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Cleanup(); err != nil {
			t.Error(err)
		}
	})
	code := `package main
import("errors";"image";"testing";gioapp "gioui.org/app";"gioui.org/io/event";"gioui.org/layout";"gioui.org/op";"gioui.org/widget/material")
type sentinelView struct{ calls int }
func(v *sentinelView) Layout(gtx layout.Context, _ *material.Theme) layout.Dimensions{v.calls++;return layout.Dimensions{Size:gtx.Constraints.Max}}
func TestActualHostLoop(t *testing.T){
 for _,cause:=range []error{nil,errors.New("native destroy failed")} {
  v:=new(sentinelView);frames:=0;index:=0
  events:=[]event.Event{gioapp.FrameEvent{Size:image.Pt(200,100),Frame:func(ops *op.Ops){if ops==nil{t.Fatal("nil frame")};frames++}},gioapp.DestroyEvent{Err:cause}}
  err:=runPreviewWindow(v,material.NewTheme(),func()event.Event{e:=events[index];index++;return e})
  if !errors.Is(err,cause)||frames!=1||v.calls!=1||index!=2{t.Fatalf("err=%v want=%v frames=%d layouts=%d consumed=%d",err,cause,frames,v.calls,index)}
 }
}`
	if err := os.WriteFile(filepath.Join(host.Dir, "host_loop_test.go"), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-v", "-run", "^TestActualHostLoop$", ".")
	cmd.Dir = host.Dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOSUMDB=off")
	out, e := cmd.CombinedOutput()
	if e != nil || !bytes.Contains(out, []byte("--- PASS: TestActualHostLoop")) {
		t.Fatalf("generated loop regression: %v %s", e, out)
	}
}

func TestHostForwardsStdoutAndStderr(t *testing.T) {
	helper := buildConsoleHelper(t)
	for _, mode := range []string{"ready", "unknown"} {
		var out, errout bytes.Buffer
		h, err := StartHostWithOutput(helper, "", &out, &errout, mode)
		if err != nil {
			t.Fatal(err)
		}
		exit, wait := h.Wait(5 * time.Second)
		if wait != nil {
			t.Fatal(wait)
		}
		if mode == "ready" {
			if exit != nil || !strings.Contains(out.String(), "READY") {
				t.Fatalf("lost stdout: %v %s", exit, out.String())
			}
		} else if exit == nil || !strings.Contains(errout.String(), "unknown mode") {
			t.Fatalf("lost error stream/exit: %v %s", exit, errout.String())
		}
	}
}
