package watchadapter_test

import (
	"context"
	"fmt"
	"github.com/goxml/goxml/internal/devx/watchadapter"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func reviewedSentinelTool(t *testing.T, label string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "tool.go")
	exe := filepath.Join(dir, "tool.exe")
	code := fmt.Sprintf("package main\nimport \"fmt\"\nfunc main(){fmt.Println(%q)}\n", label)
	if e := os.WriteFile(src, []byte(code), 0600); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("go", "build", "-o", exe, src)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("build owned tool: %v %s", e, out)
	}
	return exe
}
func TestReviewedBuilderUsesInjectedTool(t *testing.T) {
	proj := makeRealProject(t)
	tool := reviewedSentinelTool(t, "COORD_TOOL_A")
	b := watchadapter.NewRealBuilder(proj, "./cmd/app", "", tool)
	rc, out := b.Generate(context.Background(), proj)
	if rc != 0 || !strings.Contains(out, "COORD_TOOL_A") {
		t.Fatalf("explicit tool was not executed: rc=%d output=%s", rc, out)
	}
}
func TestReviewedMissingInjectedToolFailsBeforeGenerating(t *testing.T) {
	proj := makeRealProject(t)
	tool := filepath.Join(t.TempDir(), "no-such-owned-tool.exe")
	b := watchadapter.NewRealBuilder(proj, "./cmd/app", "", tool)
	rc, out := b.Generate(context.Background(), proj)
	if rc == 0 || !strings.Contains(out, "no-such-owned-tool") {
		t.Fatalf("missing explicit tool was ignored: rc=%d output=%s", rc, out)
	}
	if _, e := os.Stat(filepath.Join(proj, "ui", "home_gen.go")); !os.IsNotExist(e) {
		t.Fatal("must not generate with a different tool when explicit identity is missing")
	}
}
func TestReviewedBuilderToolIdentityIsPerInstance(t *testing.T) {
	proj := makeRealProject(t)
	a := reviewedSentinelTool(t, "COORD_TOOL_A")
	b := reviewedSentinelTool(t, "COORD_TOOL_B")
	ba := watchadapter.NewRealBuilder(proj, ".", "", a)
	bb := watchadapter.NewRealBuilder(proj, ".", "", b)
	for _, tc := range []struct {
		f    func(context.Context, string) (int, string)
		want string
	}{{ba.Generate, "COORD_TOOL_A"}, {bb.Generate, "COORD_TOOL_B"}} {
		rc, out := tc.f(context.Background(), proj)
		if rc != 0 || !strings.Contains(out, tc.want) {
			t.Fatalf("builder identity leaked: want=%s rc=%d output=%s", tc.want, rc, out)
		}
	}
}
