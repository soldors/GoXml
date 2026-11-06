package cli

import (
	"bytes"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandedRevisionGen3SafeMissThenHit(t *testing.T) {
	root := standaloneCLIProject(t)
	src := filepath.Join(root, "home.gxml")
	out := filepath.Join(root, "home_gen.go")
	old := codegen.GoxmlVersion + "+gen-3"
	if cacheIdentity() == old {
		t.Fatal("Expanded output rules must invalidate gen-3 identity")
	}
	cacheIdentityOverride = old
	t.Cleanup(func() { cacheIdentityOverride = "" })
	if rc, _, se := runStandaloneCLI(t, src); rc != ExitOK {
		t.Fatal(se)
	}
	original := readCLIFile(t, out)
	cacheIdentityOverride = ""
	rc, stdout, se := runStandaloneCLI(t, src)
	if rc != ExitOK || !strings.Contains(stdout, "compiler-version-changed") || strings.Contains(stdout, "缓存命中") {
		t.Fatalf("gen-3 must safely miss: rc=%d out=%s err=%s", rc, stdout, se)
	}
	if !bytes.Equal(original, readCLIFile(t, out)) {
		t.Fatal("unrelated plain output changed on identity upgrade")
	}
	before, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	manifest := readCLIFile(t, manifestPath(root))
	rc, stdout, se = runStandaloneCLI(t, src)
	if rc != ExitOK || !strings.Contains(stdout, "缓存命中") {
		t.Fatalf("same revision must hit: %d %s %s", rc, stdout, se)
	}
	after, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(original, readCLIFile(t, out)) || !bytes.Equal(manifest, readCLIFile(t, manifestPath(root))) {
		t.Fatal("cache hit rewrote output or manifest")
	}
}
