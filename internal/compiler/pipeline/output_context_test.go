package pipeline

import (
	"path/filepath"
	"testing"
)

func TestOutputContextModuleRootIsNotUnknown(t *testing.T) {
	root := t.TempDir()
	if got := relToModule(root, root); got != "." {
		t.Fatalf("root context=%q, want dot", got)
	}
	if got := relToModule(root, filepath.Join(root, "out")); got != "out" {
		t.Fatalf("output context=%q", got)
	}
	if got := relToModule("", root); got != "" {
		t.Fatalf("legacy unknown context=%q", got)
	}
}
