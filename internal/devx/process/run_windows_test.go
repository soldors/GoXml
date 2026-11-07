package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	}
	os.Exit(m.Run())
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
