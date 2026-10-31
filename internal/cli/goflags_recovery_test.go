package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Exercise the actual command entry and compile its products. The two source
// files are mutually exclusive, so both configurations are valid Go inputs.
func TestGOFLAGSRegeneratesOwnedOutputAcrossPackageSwitch(t *testing.T) {
	root := goflagsProject(t, false)
	generate := func(flags string, want int) string {
		t.Helper()
		t.Setenv("GOFLAGS", flags)
		var out, errout bytes.Buffer
		rc := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out, &errout)
		if rc != want {
			t.Fatalf("GOFLAGS=%q rc=%d want=%d\n%s\n%s", flags, rc, want, out.String(), errout.String())
		}
		return out.String()
	}
	generate("", 0)
	goflagsBuild(t, root, "")
	base := mustRead(t, filepath.Join(root, "home_gen.go"))
	if !bytes.Contains(base, []byte("package basepkg")) {
		t.Fatal("base package not generated")
	}
	generate("-tags=r3recovery", 0)
	goflagsBuild(t, root, "-tags=r3recovery")
	tagged := mustRead(t, filepath.Join(root, "home_gen.go"))
	if !bytes.Contains(tagged, []byte("package taggedpkg")) {
		t.Fatal("tagged package not generated")
	}
	generate("", 0)
	goflagsBuild(t, root, "")
	if !bytes.Equal(base, mustRead(t, filepath.Join(root, "home_gen.go"))) {
		t.Fatal("switch back not deterministic")
	}
	before, err := os.Stat(filepath.Join(root, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generate("", 0), "缓存命中") {
		t.Fatal("unchanged context missed cache")
	}
	after, err := os.Stat(filepath.Join(root, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if before.ModTime() != after.ModTime() {
		t.Fatal("cache hit rewrote product")
	}
}

func TestGOFLAGSChangedContextCannotReuseSamePackageCache(t *testing.T) {
	root := goflagsProject(t, true)
	t.Setenv("GOFLAGS", "")
	var out, errs bytes.Buffer
	if rc := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out, &errs); rc != 0 {
		t.Fatalf("seed rc=%d: %s", rc, errs.String())
	}
	gen := mustRead(t, filepath.Join(root, "home_gen.go"))
	manifest := mustRead(t, manifestPath(root))
	t.Setenv("GOFLAGS", "-tags=r3recovery")
	out.Reset()
	errs.Reset()
	if rc := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out, &errs); rc != ExitFailure {
		t.Fatalf("invalid tagged model reused old cache: rc=%d\n%s\n%s", rc, out.String(), errs.String())
	}
	if !bytes.Equal(gen, mustRead(t, filepath.Join(root, "home_gen.go"))) || !bytes.Equal(manifest, mustRead(t, manifestPath(root))) {
		t.Fatal("failed context switch changed old output or manifest")
	}
}

func TestGOFLAGSFailedRegenerationKeepsOldOutput(t *testing.T) {
	root := goflagsProject(t, false)
	t.Setenv("GOFLAGS", "")
	var out, errs bytes.Buffer
	if rc := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out, &errs); rc != 0 {
		t.Fatalf("seed rc=%d: %s", rc, errs.String())
	}
	gen := mustRead(t, filepath.Join(root, "home_gen.go"))
	manifest := mustRead(t, manifestPath(root))
	source := mustRead(t, filepath.Join(root, "home.gxml"))
	if err := os.WriteFile(filepath.Join(root, "home.gxml"), bytes.ReplaceAll(source, []byte("{Title}"), []byte("{Missing}")), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-tags=r3recovery")
	out.Reset()
	errs.Reset()
	if rc := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out, &errs); rc != ExitFailure || !strings.Contains(errs.String(), "GXML-BIND-001") {
		t.Fatalf("binding failure obscured by stale output: rc=%d\n%s", rc, errs.String())
	}
	if !bytes.Equal(gen, mustRead(t, filepath.Join(root, "home_gen.go"))) || !bytes.Equal(manifest, mustRead(t, manifestPath(root))) {
		t.Fatal("failed regeneration changed old output or manifest")
	}
}

func TestGOFLAGSSinglePageDoesNotHideOtherStaleOwnedOutputs(t *testing.T) {
	root := goflagsProject(t, false)
	if err := os.Mkdir(filepath.Join(root, "vm"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vm", "vm.go"), []byte("package vm\ntype Model struct { Title string }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	page := bytes.ReplaceAll(mustRead(t, filepath.Join(root, "home.gxml")), []byte(`ViewModel="VM"`), []byte(`ViewModel="vm.Model"`))
	for _, name := range []string{"home.gxml", "other.gxml"} {
		if err := os.WriteFile(filepath.Join(root, name), page, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOFLAGS", "")
	var out, errs bytes.Buffer
	if rc := Run([]string{"generate", root}, &out, &errs); rc != 0 {
		t.Fatalf("seed rc=%d: %s", rc, errs.String())
	}
	before := mustRead(t, filepath.Join(root, "home_gen.go"))
	other := mustRead(t, filepath.Join(root, "other_gen.go"))
	manifest := mustRead(t, manifestPath(root))
	t.Setenv("GOFLAGS", "-tags=r3recovery")
	out.Reset()
	errs.Reset()
	if rc := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out, &errs); rc != ExitFailure {
		t.Fatalf("single page accepted another stale package: rc=%d\n%s", rc, errs.String())
	}
	if !bytes.Equal(before, mustRead(t, filepath.Join(root, "home_gen.go"))) || !bytes.Equal(other, mustRead(t, filepath.Join(root, "other_gen.go"))) || !bytes.Equal(manifest, mustRead(t, manifestPath(root))) {
		t.Fatal("partial package switch changed old products")
	}
	out.Reset()
	errs.Reset()
	if rc := Run([]string{"generate", root}, &out, &errs); rc != 0 {
		t.Fatalf("whole package switch rc=%d: %s", rc, errs.String())
	}
	goflagsBuild(t, root, "-tags=r3recovery")
}

func goflagsProject(t *testing.T, samePackage bool) string {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mod := strings.Replace(string(mustRead(t, filepath.Join(repo, "go.mod"))), "module github.com/goxml/goxml", "module example.com/recovery", 1)
	mod += "\nrequire github.com/goxml/goxml v0.0.0\nreplace github.com/goxml/goxml => " + strconv.Quote(filepath.ToSlash(repo)) + "\n"
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", mod)
	write("go.sum", string(mustRead(t, filepath.Join(repo, "go.sum"))))
	write("base.go", "//go:build !r3recovery\n\npackage basepkg\ntype VM struct { Title string }\n")
	tagged := "package taggedpkg\ntype VM struct { Title string }\n"
	if samePackage {
		tagged = "package basepkg\ntype VM struct { Other string }\n"
	}
	write("tagged.go", "//go:build r3recovery\n\n"+tagged)
	write("home.gxml", `<Goxml Version="1" ViewModel="VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	return root
}

func goflagsBuild(t *testing.T, root, flags string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBinary(t), "build", "-mod=readonly", "-p=2", ".")
	cmd.Dir = root
	cmd.Env = goEnvFor(flags)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build flags=%q: %v\n%s", flags, err, data)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
