package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// Real Go emits dependency objects under GOFLAGS=-deps. They must not silently
// become a successful single target result (the single-directory path rejects).
func TestBatchReviewRejectsExtraDependencyObjects(t *testing.T) {
	root, dirs := batchFixture(t, 1)
	mustWrite(t, filepath.Join(dirs[0], "vm.go"), "package pkg00\nimport _ \"fmt\"\ntype VM struct{}\n")
	env := []string{"GOENV=off", "GOFLAGS=-deps"}
	_, _, singleErr := resolveTargetPackageGo(goBinary(t), root, dirs[0], env, nil, nil)
	if singleErr == nil {
		t.Fatal("fixture must emit extra package objects")
	}
	_, err := resolveTargetPackagesBatch(goBinary(t), root, env, dirs, nil)
	if err == nil || !strings.Contains(err.Error(), "包对象") {
		t.Fatalf("extra dependency objects accepted or wrong rejection: %v", err)
	}
}
