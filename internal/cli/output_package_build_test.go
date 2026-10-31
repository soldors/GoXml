package cli

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildExcludedPackageFixture(t *testing.T, dir string) {
	t.Helper()
	writeGoFile(t, dir, "a_view.go", "package ui\n")
	writeGoFile(t, dir, "zz_generate.go", "//go:build ignore\n\npackage main\n\nfunc main() {}\n")
	otherOS := "windows"
	if build.Default.GOOS == "windows" {
		otherOS = "linux"
	}
	writeGoFile(t, dir, "zz_platform_"+otherOS+".go", "package other\n")
	writeGoFile(t, dir, "_ignored.go", "package hidden\n")
}

func TestPackageDirNameIgnoresBuildExcludedFiles(t *testing.T) {
	dir := t.TempDir()
	buildExcludedPackageFixture(t, dir)
	name, err := packageDirName(dir)
	if err != nil || name != "ui" {
		t.Fatalf("one active production package must remain usable: name=%q err=%v", name, err)
	}
}

func TestGenerateIgnoresBuildExcludedFiles(t *testing.T) {
	root := tempProjectInRepo(t)
	buildExcludedPackageFixture(t, root)
	source := filepath.Join(root, "home.gxml")
	if err := os.WriteFile(source, []byte(`<Goxml Version="1"><Page Route="/home"><Text Value="hello"/></Page></Goxml>`), 0600); err != nil {
		t.Fatal(err)
	}
	if c, o, e := b3Run(t, source); c != ExitOK {
		t.Fatalf("valid package with generator scripts: %d %s %s", c, o, e)
	}
	if got := productPackage(t, filepath.Join(root, "home_gen.go")); got != "ui" {
		t.Fatalf("generated package=%s", got)
	}
}

func TestPackageDirNameReportsInvalidBuildConstraint(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "a_view.go", "package ui\n")
	writeGoFile(t, dir, "zz_bad.go", "//go:build (\n\npackage ignored\n")
	_, err := packageDirName(dir)
	if err == nil || !strings.Contains(err.Error(), "zz_bad.go") {
		t.Fatalf("bad build constraint must identify file: %v", err)
	}
}
