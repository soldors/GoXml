package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceMeasuredContextStillUsableForPackageSelection(t *testing.T) {
	root := newScratchModule(t)
	work := filepath.Join(root, "go.work")
	if err := os.WriteFile(work, []byte("go 1.26.1\n\nuse .\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := measureContextValues(goBinary(t), root, "GOWORK="+work, "GOENV=off")
	if err != nil {
		t.Fatalf("valid workspace must remain measurable for naming: %v", err)
	}
	if m.GOWORK != work {
		t.Fatalf("workspace identity lost: %q", m.GOWORK)
	}
	if _, err := buildContextFromMeasured(m); err != nil {
		t.Fatalf("package context: %v", err)
	}
	if _, err := m.cacheString(); err == nil {
		t.Fatal("workspace cannot use single-module cache fingerprint")
	}
}

func TestWorkspaceGenerationConservativelyMissesAndPreservesFailure(t *testing.T) {
	root := newScratchModuleWithRepoDeps(t)
	dir := filepath.Join(root, "page")
	writeGoFile(t, dir, "vm.go", "package page\n\ntype VM struct { Title string }\n")
	work := filepath.Join(root, "go.work")
	if err := os.WriteFile(work, []byte("go 1.26.1\n\nuse .\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", work)
	t.Setenv("GOENV", "off")
	file := filepath.Join(dir, "home.gxml")
	valid := []byte(`<Goxml Version="1" ViewModel="VM"><Page Route="/home"><Text Value="{Title}"/></Page></Goxml>`)
	if err := os.WriteFile(file, valid, 0o644); err != nil {
		t.Fatal(err)
	}
	run := func() (int, string) {
		var out, errb bytes.Buffer
		rc := Run([]string{"generate", dir}, &out, &errb)
		return rc, out.String() + errb.String()
	}
	for i := 0; i < 2; i++ {
		rc, log := run()
		if rc != ExitOK {
			t.Fatalf("workspace generation %d failed: rc=%d %s", i, rc, log)
		}
		if strings.Contains(log, "缓存命中") {
			t.Fatalf("workspace must conservatively regenerate: %s", log)
		}
		if !strings.Contains(log, "已生成") {
			t.Fatalf("not a real regeneration: %s", log)
		}
	}
	gen := filepath.Join(dir, "home_gen.go")
	before, manifest := mustRead(t, gen), mustRead(t, manifestPath(root))
	mtime := fileModTime(t, gen)
	if err := os.WriteFile(file, []byte("<Goxml><Page"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, log := run(); rc == ExitOK {
		t.Fatalf("bad workspace input accepted: %s", log)
	}
	if !bytes.Equal(before, mustRead(t, gen)) || !bytes.Equal(manifest, mustRead(t, manifestPath(root))) || mtime != fileModTime(t, gen) {
		t.Fatal("failed workspace generation changed old outputs")
	}
}
