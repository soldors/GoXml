package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnershipUsesFilesystemIdentity(t *testing.T) {
	root := tempProjectInRepo(t)
	for _, pkg := range []string{"ui", "admin"} {
		dir := filepath.Join(root, pkg)
		writeGoFile(t, dir, "vm.go", "package "+pkg+"\n")
		if err := os.WriteFile(filepath.Join(dir, "home.gxml"), []byte(`<Goxml Version="1"><Page Route="/`+pkg+`"><Text Value="`+pkg+`"/></Page></Goxml>`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "UI", "home.gxml")
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		// A case-sensitive filesystem must retain distinct case spellings.
		writeGoFile(t, filepath.Join(root, "UI"), "vm.go", "package other\n")
		if err := os.WriteFile(alias, []byte("distinct source"), 0600); err != nil {
			t.Fatal(err)
		}
		equal, err := sameProjectFile(root, "ui/home.gxml", "UI/home.gxml", os.Stat)
		if err != nil || equal {
			t.Fatalf("distinct case-sensitive sources conflated: %v %v", equal, err)
		}
		return
	} else if err != nil {
		t.Fatal(err)
	}
	run := func(source, out string, extra ...string) (int, string, string) {
		args := []string{filepath.Join(root, filepath.FromSlash(source)), "--out-dir", filepath.Join(root, out)}
		return b3Run(t, append(args, extra...)...)
	}
	if c, o, e := run("ui/home.gxml", "gen"); c != ExitOK {
		t.Fatalf("initial: %d %s %s", c, o, e)
	}
	if c, o, e := run("UI/home.gxml", "gen"); c != ExitOK {
		t.Fatalf("same source alias: %d %s %s", c, o, e)
	}
	m := mustManifest(t, root)
	if len(m.Entries) != 1 || !bytes.HasSuffix([]byte(m.Entries[0].Source), []byte("UI/home.gxml")) {
		t.Fatalf("source alias must replace one owner while retaining spelling: %+v", m.Entries)
	}
	product := filepath.Join(root, "gen", "home_gen.go")
	before, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(manifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	cacheBefore := countCacheRecords(t)
	if c, o, e := run("admin/home.gxml", "GEN"); c != ExitFailure {
		t.Fatalf("output alias must conflict: %d %s %s", c, o, e)
	}
	after, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	manifestAfter, err := os.ReadFile(manifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !bytes.Equal(manifestBefore, manifestAfter) || cacheBefore != countCacheRecords(t) {
		t.Fatal("rejected alias changed disk state")
	}
	// Deleting the product does not retire its live source's ownership.
	if err := os.Remove(product); err != nil {
		t.Fatal(err)
	}
	if c, o, e := run("admin/home.gxml", "GEN"); c != ExitFailure {
		t.Fatalf("missing output alias must fail closed while old source lives: %d %s %s", c, o, e)
	}
	if err := os.WriteFile(product, before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if c, o, e := run("admin/home.gxml", "GEN"); c != ExitOK {
		t.Fatalf("deleted source transfer: %d %s %s", c, o, e)
	}
	m = mustManifest(t, root)
	if len(m.Entries) != 1 || !bytes.HasSuffix([]byte(m.Entries[0].Output), []byte("GEN/home_gen.go")) {
		t.Fatalf("transfer must remove output alias, preserving new spelling: %+v", m.Entries)
	}
	if c, o, e := run("admin/home.gxml", "GEN", "--clean"); c != ExitOK {
		t.Fatalf("clean current owner: %d %s %s", c, o, e)
	}
	if _, err := os.Stat(product); err != nil {
		t.Fatalf("clean deleted current owner's product: %v", err)
	}
}

func TestOwnershipStatErrorFailsClosed(t *testing.T) {
	root := t.TempDir()
	man := &manifest{Entries: []entry{{Source: "old/home.gxml", Output: "gen/home_gen.go"}}}
	rs := []*resolvedInput{{sourceRel: "new/home.gxml", outputRel: "gen/home_gen.go"}}
	denied := &os.PathError{Op: "stat", Path: filepath.Join(root, "old", "home.gxml"), Err: os.ErrPermission}
	calls := 0
	err := checkOutputOwnershipWithStat(root, man, rs, func(path string) (os.FileInfo, error) {
		calls++
		if path != denied.Path {
			t.Fatalf("unexpected stat: %s", path)
		}
		return nil, denied
	})
	if calls != 1 || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("permission failure must remain error: calls=%d err=%v", calls, err)
	}
	err = checkOutputOwnershipWithStat(root, man, rs, func(string) (os.FileInfo, error) { return nil, &os.PathError{Op: "stat", Err: os.ErrNotExist} })
	if err != nil {
		t.Fatalf("missing old source must permit transfer: %v", err)
	}
}

func TestOwnershipMissingCaseAmbiguityFailsClosed(t *testing.T) {
	root := t.TempDir()
	equal, err := sameProjectFile(root, "gen/home_gen.go", "GEN/home_gen.go", os.Stat)
	if equal || err == nil {
		t.Fatalf("missing case variants require explicit resolution: equal=%v err=%v", equal, err)
	}
	equal, err = sameProjectFile(root, "gen/home_gen.go", "other/home_gen.go", os.Stat)
	if equal || err != nil {
		t.Fatalf("distinct missing paths must remain distinct: equal=%v err=%v", equal, err)
	}
}
