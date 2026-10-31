package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A Windows junction is ModeIrregular, not ModeSymlink, in Go 1.26. It must
// therefore have its own actual filesystem regression rather than a symlink
// stand-in. Junction creation does not require the symlink privilege.
func makeCLIWindowsJunction(t *testing.T, link, target string) {
	t.Helper()
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v\n%s", err, output)
	}
}

func TestGenerateRejectsWindowsJunctionOutputPaths(t *testing.T) {
	for _, via := range []string{"out-dir-missing", "out-dir-symlink-junction", "manifest-missing", "manifest-existing"} {
		t.Run(via, func(t *testing.T) {
			root := standaloneCLIProject(t)
			source := filepath.Join(root, "home.gxml")
			if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
				t.Fatalf("initial generation rc=%d: %s", code, stderr)
			}
			oldOutput := readCLIFile(t, filepath.Join(root, "home_gen.go"))
			outside := t.TempDir()
			canary := filepath.Join(outside, "home_gen.go")
			writeCLIFile(t, canary, oldOutput)
			redirect := filepath.Join(root, "redirect")
			makeCLIWindowsJunction(t, redirect, outside)
			if via == "out-dir-symlink-junction" {
				link := filepath.Join(root, "symlink to junction")
				if err := os.Symlink(redirect, link); err != nil {
					t.Skipf("directory symlink unavailable: %v", err)
				}
				redirect = link
			}
			args := []string{source, "--check", "--no-cache"}
			if via == "out-dir-missing" || via == "out-dir-symlink-junction" {
				args = append(args, "--out-dir", filepath.Join(redirect, "missing", "deep"))
			} else {
				m, err := loadManifest(manifestPath(root))
				if err != nil {
					t.Fatal(err)
				}
				output := "redirect/home_gen.go"
				if via == "manifest-missing" {
					output = "redirect/missing/deep/home_gen.go"
				}
				m.Entries = append(m.Entries, entry{Source: "gone.gxml", Output: output})
				if err := saveManifest(manifestPath(root), m); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--clean")
			}
			manifestBefore := readCLIFile(t, manifestPath(root))
			code, _, stderr := runStandaloneCLI(t, args...)
			if code != ExitFailure {
				t.Errorf("junction output must fail closed, rc=%d: %s", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(outside, "missing")); !os.IsNotExist(err) {
				t.Errorf("junction escape created outside directories: %v", err)
			}
			requireCLIBytes(t, canary, oldOutput)
			requireCLIBytes(t, filepath.Join(root, "home_gen.go"), oldOutput)
			requireCLIBytes(t, manifestPath(root), manifestBefore)
		})
	}
}

func TestGenerateAllowsModuleLocalDirectorySymlink(t *testing.T) {
	root := standaloneCLIProject(t)
	actual := filepath.Join(root, "actual")
	if err := os.MkdirAll(actual, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(actual, link); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
	code, _, stderr := runStandaloneCLI(t, filepath.Join(root, "home.gxml"), "--out-dir", filepath.Join(link, "new", "deep"), "--check", "--no-cache")
	if code != ExitOK {
		t.Fatalf("module-local directory symlink rejected, rc=%d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(actual, "new", "deep", "home_gen.go")); err != nil {
		t.Fatal(err)
	}
}
