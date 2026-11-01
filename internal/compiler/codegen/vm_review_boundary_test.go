package codegen

import (
	"github.com/goxml/goxml/internal/compiler/analyzer"
	"strings"
	"testing"
)

func TestReviewVMRootMoved(t *testing.T) {
	g := vmGen(Options{ModulePath: "example.com/root", ViewModelPackageDir: ".", OutputPackageDir: "out", ViewModelPackageName: "rootpkg"}, analyzer.ViewModelRef{Name: "VM"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "rootpkg.VM" || !strings.Contains(g.imports.render(), `rootpkg "example.com/root"`) {
		t.Fatalf("root import/type mismatch: %s %s", got, g.imports.render())
	}
}

func TestReviewVMImportCollision(t *testing.T) {
	for _, name := range []string{"material", "layout", "unit", "widget", "math"} {
		t.Run(name, func(t *testing.T) {
			g := vmGen(Options{ModulePath: "example.com/root", ViewModelPackageDir: "src", OutputPackageDir: "out", ViewModelPackageName: name}, analyzer.ViewModelRef{Name: "VM"})
			g.imports.add("gioui.org/"+name, name)
			got, err := g.vmTypeExpr()
			if err != nil {
				t.Fatal(err)
			}
			if got == name+".VM" {
				t.Fatalf("VM reuses existing qualifier %s: %s", name, g.imports.render())
			}
			if !strings.Contains(g.imports.render(), strings.TrimSuffix(got, ".VM")+` "example.com/root/src"`) {
				t.Fatalf("VM qualifier is not emitted alias: %s %s", got, g.imports.render())
			}
			again, _ := g.vmTypeExpr()
			if got != again {
				t.Fatalf("unstable: %s %s", got, again)
			}
		})
	}
}

func TestReviewVMUnicodeQualifier(t *testing.T) {
	g := vmGen(Options{ModulePath: "example.com/root", ViewModelPackageDir: "src", OutputPackageDir: "out", ViewModelPackageName: "中文"}, analyzer.ViewModelRef{Name: "VM"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "中文.VM" || !strings.Contains(g.imports.render(), `中文 "example.com/root/src"`) {
		t.Fatalf("Unicode qualifier lost: %s %s", got, g.imports.render())
	}
	for _, s := range []string{"_", "type", "a-b"} {
		if isValidGoIdent(s) {
			t.Fatalf("invalid qualifier accepted: %q", s)
		}
	}
}
