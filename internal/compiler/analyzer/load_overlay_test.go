package analyzer

import (
	"bytes"
	"github.com/goxml/goxml/contracts"
	"go/types"
	"os"
	"path/filepath"
	"testing"
)

func TestTypeEnvOverlaySnapshotIsPrivateAndReadOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/overlay\n\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "vm.go")
	onDisk := []byte("package overlay\ntype VM struct { Original string }\n")
	if err := os.WriteFile(path, onDisk, 0600); err != nil {
		t.Fatal(err)
	}
	data := []byte("package overlay\ntype VM struct { Snapshot string }\n")
	overlay := map[string][]byte{path: data}
	env, err := NewTypeEnv(Config{ModuleRoot: root, SourceDir: root, Overlay: overlay})
	if err != nil {
		t.Fatal(err)
	}
	for i := range data {
		data[i] = '!'
	}
	delete(overlay, path)
	vm, dg := env.ResolveViewModel(contracts.ViewModelDecl{Value: "VM"})
	if dg != nil || vm == nil {
		t.Fatalf("snapshot not loaded: %v", dg)
	}
	fields := vm.Named.Underlying().(*types.Struct)
	if fields.NumFields() != 1 || fields.Field(0).Name() != "Snapshot" {
		t.Fatal("loader did not consume private overlay")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, after) {
		t.Fatal("overlay changed disk source")
	}
}

func TestTypeEnvOverlayRejectsUnscopedPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/overlay\n\ngo 1.26.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative.go", filepath.Join(filepath.Dir(root), "outside.go")} {
		if _, err := NewTypeEnv(Config{ModuleRoot: root, Overlay: map[string][]byte{path: []byte("package p")}}); err == nil {
			t.Fatalf("accepted unscoped path: %s", path)
		}
	}
}
