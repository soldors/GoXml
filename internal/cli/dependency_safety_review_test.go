package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Stable sentinels are explanations, not proof that a partial closure is reusable.
func TestReviewDependencyUnsafeCannotAuthorizeHit(t *testing.T) {
	for _, kind := range []string{"missing", "external", "truncated", "nested-module", "single-quoted-vm"} {
		t.Run(kind, func(t *testing.T) {
			root, src, write := depProject(t)
			source := `<Goxml Version="1" ViewModel="VM"><Page/></Goxml>`
			switch kind {
			case "missing":
				source = `<Goxml Version="1" ViewModel="gone.VM"><Page/></Goxml>`
			case "external":
				write("src/vm.go", "package vm\nimport ext \"example.net/external\"\ntype VM struct { X ext.Value }\n")
			case "truncated":
				for i := 0; i < maxDependencyDirs+1; i++ {
					next := ""
					if i < maxDependencyDirs {
						next = fmt.Sprintf("import _ \"example.org/r5b/p%d\"\n", i+1)
					}
					write(fmt.Sprintf("p%d/p.go", i), "package p\n"+next)
				}
				write("src/vm.go", "package vm\nimport _ \"example.org/r5b/p0\"\ntype VM struct{}\n")
			case "nested-module":
				write("src/go.mod", "module example.net/nested\n")
			case "single-quoted-vm":
				source = `<Goxml Version='1' ViewModel='VM'><Page/></Goxml>`
				write("src/vm.go", "package vm\nimport ext \"example.net/external\"\ntype VM struct{X ext.Value}\n")
			}
			r := depInput(root, src, "VM", source)
			if _, err := collectDependencySources(r, "example.org/r5b", nil); err != nil {
				t.Fatal(err)
			}
			if !r.dependencyUnsafe {
				t.Fatal("incomplete dependency closure did not disable reuse")
			}
			r.target = filepath.Join(src, "page_gen.go")
			if hit, _, err := decideCacheHit(root, r, "unused", &manifest{}, nil, false); err != nil || hit {
				t.Fatalf("unsafe consumer hit=%v err=%v", hit, err)
			}
		})
	}
}

func TestReviewDependencyLocalAndStandardRemainCacheable(t *testing.T) {
	root, src, write := depProject(t)
	write("src/vm.go", "package vm\nimport \"strings\"\ntype VM struct{}\nfunc F()string{return strings.TrimSpace(\"x\")}\n")
	r := depInput(root, src, "VM", `<Goxml Version="1" ViewModel="VM"><Page/></Goxml>`)
	if _, err := collectDependencySources(r, "example.org/r5b", nil); err != nil || r.dependencyUnsafe {
		t.Fatalf("normal dependency closure unsafe=%v err=%v", r.dependencyUnsafe, err)
	}
}

func TestReviewUnsafeNoCacheRetainsBypass(t *testing.T) {
	root := t.TempDir()
	r := &resolvedInput{target: filepath.Join(root, "missing_gen.go"), dependencyUnsafe: true}
	if hit, reason, err := decideCacheHit(root, r, "key", &manifest{}, nil, true); hit || reason != reasonBypassed || err != nil {
		t.Fatalf("explicit bypass drift: %v %s %v", hit, reason, err)
	}
}

func TestReviewDependencyRootImportAndQuotedModule(t *testing.T) {
	root, src, write := depProject(t)
	write("go.mod", "module \"example.org/r5b\"\n\ngo 1.26\n")
	path, err := readModulePath(root)
	if err != nil || path != "example.org/r5b" {
		t.Fatalf("quoted module: %q %v", path, err)
	}
	write("root.go", "package root\ntype Thing struct{}\n")
	write("src/vm.go", "package vm\nimport root \"example.org/r5b\"\ntype VM struct{T root.Thing}\n")
	r := depInput(root, src, "VM", `<!-- <Goxml ViewModel="decoy.VM"> --><Goxml Version='1' ViewModel='VM'><Page/></Goxml>`)
	deps, err := collectDependencySources(r, path, nil)
	if err != nil || r.dependencyUnsafe || !depsContain(deps, "root.go") {
		t.Fatalf("root dependency missing: unsafe=%v deps=%v err=%v", r.dependencyUnsafe, deps, err)
	}
}

func TestReviewGoContextUsesProjectDirectory(t *testing.T) {
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module example.org/workspace\n\ngo 1.26\n", "go.work": "go 1.26\n\nuse .\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOWORK", "")
	if _, err := measuredCacheContext(goExe, root); err == nil || !strings.Contains(err.Error(), "go.work") {
		t.Fatalf("project workspace was not observed: %v", err)
	}
}

func TestReviewCleanReadFailurePreservesEarlierDeleteFailure(t *testing.T) {
	es := []entry{{Source: "a.gxml", Output: "a_gen.go"}, {Source: "b.gxml", Output: "b_gen.go"}, {Source: "c.gxml", Output: "c_gen.go"}}
	root, _ := cleanFixture(t, es, map[string]string{"a_gen.go": genBody("a.gxml"), "b_gen.go": genBody("b.gxml"), "c_gen.go": genBody("c.gxml")})
	removeBoom := errors.New("prior remove failed")
	readBoom := errors.New("later read failed")
	ops := realCleanOps
	ops.remove = func(path string) error {
		if filepath.Base(path) == "a_gen.go" {
			return removeBoom
		}
		return os.Remove(path)
	}
	ops.readFile = func(path string) ([]byte, error) {
		if filepath.Base(path) == "b_gen.go" {
			return nil, readBoom
		}
		return os.ReadFile(path)
	}
	_, _, err := cleanOrphansOps(root, nil, ops)
	var failure *cleanFailure
	if !errors.As(err, &failure) || !errors.Is(err, removeBoom) || !errors.Is(err, readBoom) {
		t.Fatalf("earlier/later causes lost: %v", err)
	}
	if len(failure.failed) != 1 || failure.failed[0] != "a_gen.go" {
		t.Fatalf("earlier deletion failure lost: %+v", failure)
	}
	if len(failure.pending) != 2 || failure.pending[0] != "b_gen.go" || failure.pending[1] != "c_gen.go" {
		t.Fatalf("unattempted cleanup not recorded: %+v", failure)
	}
}
