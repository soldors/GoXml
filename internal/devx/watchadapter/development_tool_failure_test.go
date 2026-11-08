package watchadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentToolBuildFailureDoesNotCacheIdentity(t *testing.T) {
	old, _ := ToolIdentity()
	t.Cleanup(func() { SetToolExe(old) })
	SetToolExe("")
	flags := os.Getenv("GOFLAGS")
	t.Setenv("GOFLAGS", "-r26-unknown-build-flag")
	if _, e := toolIdentityOrErr(); e == nil || !strings.Contains(e.Error(), "r26-unknown-build-flag") {
		t.Fatalf("actual tool build failure lost: %v", e)
	}
	if exe, _ := ToolIdentity(); exe != "" {
		t.Fatalf("failed tool build cached unusable identity %s", exe)
	}
	t.Setenv("GOFLAGS", flags)
	exe, e := toolIdentityOrErr()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Remove(exe) })
	b, e := os.ReadFile(exe)
	if e != nil || len(b) < 2 || string(b[:2]) != "MZ" {
		t.Fatalf("recovery did not build real Windows tool: %v", e)
	}
	if current, _ := ToolIdentity(); current != exe {
		t.Fatal("recovered identity not cached")
	}
}

func TestDevelopmentToolUnwritableTempFailsWithoutCaching(t *testing.T) {
	old, _ := ToolIdentity()
	t.Cleanup(func() { SetToolExe(old) })
	SetToolExe("")
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "not-created")
	t.Setenv("TMP", missing)
	t.Setenv("TEMP", missing)
	if _, _, e := resolveToolExe(); e == nil {
		t.Fatal("invalid temporary directory silently accepted")
	}
	if exe, _ := ToolIdentity(); exe != "" {
		t.Fatal("failed temporary file allocation cached a tool")
	}
}
