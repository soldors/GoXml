package cli

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewCheckUserOverlay(t *testing.T) {
	root := checkFixture(t, map[string]string{"pkgx/source.go": "package pkgx\nvar Number int = \"bad-on-disk\"\n"})
	replacement := filepath.Join(t.TempDir(), "source.go")
	mustWrite(t, replacement, "package pkgx\nvar Number int = 1\n")
	body, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(root, "pkgx/source.go"): replacement}})
	userOverlay := filepath.Join(t.TempDir(), "user.json")
	if err := os.WriteFile(userOverlay, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-mod=readonly -overlay="+userOverlay)
	cmd := exec.Command(goBinary(t), "build", "./pkgx")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("authoritative user overlay must build: %v %s", err, out)
	}
	err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{{dir: filepath.Join(root, "pkgx"), output: filepath.Join(root, "pkgx/home_gen.go"), content: []byte("package pkgx\nfunc Generated() {}\n")}})
	if err != nil {
		t.Fatalf("checker must retain user overlay: %v", err)
	}
}

func TestReviewCheckShadowLeak(t *testing.T) {
	root := checkFixture(t, map[string]string{})
	missing := filepath.Join(root, "newpkg")
	bad := filepath.Join(root, "invalid\x00dir")
	err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{{dir: missing, output: filepath.Join(missing, "one_gen.go"), content: []byte("package newpkg\n")}, {dir: bad, output: filepath.Join(bad, "two_gen.go"), content: []byte("package p\n")}})
	if err == nil {
		t.Fatal("expected failed second target")
	}
	dirs, e := filepath.Glob(filepath.Join(root, ".goxml-precommit-*"))
	if e != nil {
		t.Fatal(e)
	}
	if len(dirs) != 0 {
		t.Fatalf("shadow directories leaked: %v; err=%v", dirs, err)
	}
	t.Logf("shadow cleanup after second target error: %v", dirs)
}

func TestReviewCheckCauseLost(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.exe")
	err := runGoBuildCheck(missing, dir, "", []string{"."}, "")
	if err == nil {
		t.Fatal("expected launch failure")
	}
	var pe *os.PathError
	if !errors.As(err, &pe) {
		t.Fatal("launch cause not consumable as PathError")
	}
	t.Logf("launch cause retained: %v", err)
}

func TestReviewCheckVirtualDirectories(t *testing.T) {
	root := checkFixture(t, map[string]string{})
	backing := t.TempDir()
	a := filepath.Join(backing, "a.go")
	b := filepath.Join(backing, "b.go")
	mustWrite(t, a, "package newa\nconst Value=42\n")
	mustWrite(t, b, "package newb\nimport \"example.com/chk/newa\"\nvar Value=newa.Value\n")
	data, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(root, "newa/gen.go"): a, filepath.Join(root, "newb/gen.go"): b}})
	ov := filepath.Join(backing, "overlay.json")
	if err := os.WriteFile(ov, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goBinary(t), "build", "-overlay", ov, "-o", filepath.Join(backing, "out"), "./newb")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("virtual target build: %v %s", err, out)
	}
	if err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{
		{dir: filepath.Join(root, "newa"), output: filepath.Join(root, "newa/gen.go"), content: []byte("package newa\nconst Value=42\n")},
		{dir: filepath.Join(root, "newb"), output: filepath.Join(root, "newb/gen.go"), content: []byte("package newb\nimport \"example.com/chk/newa\"\nvar Value=newa.Value\n")},
	}); err != nil {
		t.Fatalf("candidate checker lost virtual package identity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "newa")); !os.IsNotExist(err) {
		t.Fatalf("overlay physically created target directory: %v", err)
	}
}

func TestReviewCheckGOENVOverlayAndRelativeFlags(t *testing.T) {
	root := checkFixture(t, map[string]string{"pkgx/source.go": "package pkgx\nvar Number int=\"bad\"\n"})
	mustWrite(t, filepath.Join(root, "replacement.go"), "package pkgx\nvar Number int=1\n")
	ov, _ := json.Marshal(map[string]any{"Replace": map[string]string{"pkgx/source.go": "replacement.go"}})
	mustWrite(t, filepath.Join(root, "user overlay.json"), string(ov))
	mustWrite(t, filepath.Join(root, "alternate.mod"), "module example.com/chk\n\ngo 1.26\n")
	flags := `-mod=readonly -modfile=alternate.mod "-overlay=user overlay.json"`
	config := filepath.Join(t.TempDir(), "goenv")
	mustWrite(t, config, "GOFLAGS="+flags+"\n")
	t.Setenv("GOENV", config)
	t.Setenv("GOFLAGS", "")
	err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{{dir: filepath.Join(root, "pkgx"), output: filepath.Join(root, "pkgx/gen.go"), content: []byte("package pkgx\nfunc Gen(){}\n")}})
	if err != nil {
		t.Fatalf("GOENV overlay/relative modfile context lost: %v", err)
	}
}

func TestReviewCheckHitCandidatePrecedesUserOverlay(t *testing.T) {
	root := checkFixture(t, map[string]string{"pkgx/gen.go": "package pkgx\nvar Number int=\"bad-on-disk\"\n"})
	path := filepath.Join(root, "pkgx/gen.go")
	replacement := filepath.Join(t.TempDir(), "replacement.go")
	mustWrite(t, replacement, "package pkgx\nvar Number int=1\n")
	data, _ := json.Marshal(map[string]any{"Replace": map[string]string{path: replacement}})
	ov := filepath.Join(t.TempDir(), "user.json")
	mustWrite(t, ov, string(data))
	t.Setenv("GOFLAGS", "-mod=readonly -overlay="+ov)
	cands := []precommitCandidate{{dir: filepath.Dir(path), output: path}}
	err := checkPrecommitCandidates(goBinary(t), root, cands)
	if err == nil || !strings.Contains(err.Error(), "bad-on-disk") {
		t.Fatalf("user overlay hid invalid hit artifact: %v", err)
	}
	mustWrite(t, path, "package pkgx\nvar Number int=1\n")
	mustWrite(t, replacement, "package pkgx\nvar Number int=\"bad-user-overlay\"\n")
	if err := checkPrecommitCandidates(goBinary(t), root, cands); err != nil {
		t.Fatalf("valid hit artifact lost candidate precedence: %v", err)
	}
}
