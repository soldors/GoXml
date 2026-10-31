package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// standaloneCLIProject exercises the production manifest/cache paths in an
// actual module, including Unicode and spaces in its absolute path.
func standaloneCLIProject(t *testing.T) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	root := filepath.Join(t.TempDir(), "项目 空格")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data := readCLIFile(t, filepath.Join(repoRoot(t), name))
		writeCLIFile(t, filepath.Join(root, name), data)
	}
	writeCLIFile(t, filepath.Join(root, "vm.go"), []byte("package demo\n"))
	writeCLIFile(t, filepath.Join(root, "home.gxml"), []byte(`<Goxml Version="1"><Page><Text Value="home"/></Page></Goxml>`))
	return root
}

func readCLIFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeCLIFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func runStandaloneCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"generate"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func requireCLIBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	if got := readCLIFile(t, path); !bytes.Equal(got, want) {
		t.Errorf("%s changed: got %q, want %q", path, got, want)
	}
}

func TestCleanRejectsUnsafeManifestBeforeAnyChanges(t *testing.T) {
	for _, mode := range []string{"cache", "no-cache"} {
		t.Run(mode, func(t *testing.T) {
			root := standaloneCLIProject(t)
			source := filepath.Join(root, "home.gxml")
			if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
				t.Fatalf("initial generation rc=%d: %s", code, stderr)
			}
			output := filepath.Join(root, "home_gen.go")
			oldOutput := readCLIFile(t, output)
			orphan := filepath.Join(root, "old_gen.go")
			canary := filepath.Join(filepath.Dir(root), "outside_gen.go")
			writeCLIFile(t, orphan, oldOutput)
			writeCLIFile(t, canary, oldOutput)
			m, err := loadManifest(manifestPath(root))
			if err != nil {
				t.Fatal(err)
			}
			// A valid entry before the escape must not get deleted either.
			m.Entries = append(m.Entries,
				entry{Source: "old.gxml", Output: "old_gen.go"},
				entry{Source: "outside.gxml", Output: "../outside_gen.go"},
			)
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, manifestPath(root), data)
			writeCLIFile(t, source, bytes.Replace(readCLIFile(t, source), []byte("home"), []byte("changed"), 1))
			args := []string{source, "--clean", "--check"}
			if mode == "no-cache" {
				args = append(args, "--no-cache")
			}
			code, _, stderr := runStandaloneCLI(t, args...)
			if code != ExitFailure || !strings.Contains(stderr, "Manifest") {
				t.Errorf("unsafe manifest must fail closed, rc=%d: %s", code, stderr)
			}
			requireCLIBytes(t, canary, oldOutput)
			requireCLIBytes(t, orphan, oldOutput)
			requireCLIBytes(t, output, oldOutput)
			requireCLIBytes(t, manifestPath(root), data)
		})
	}
}

func TestCleanPreservesUnselectedLiveSources(t *testing.T) {
	root := standaloneCLIProject(t)
	source := filepath.Join(root, "home.gxml")
	other := filepath.Join(root, "other.gxml")
	writeCLIFile(t, other, bytes.Replace(readCLIFile(t, source), []byte("home"), []byte("other"), 1))
	if code, _, stderr := runStandaloneCLI(t, root); code != ExitOK {
		t.Fatalf("initial generation rc=%d: %s", code, stderr)
	}
	otherOutput := filepath.Join(root, "other_gen.go")
	want := readCLIFile(t, otherOutput)
	for _, flags := range [][]string{{"--clean"}, {"--clean", "--check", "--no-cache"}} {
		code, _, stderr := runStandaloneCLI(t, append([]string{source}, flags...)...)
		if code != ExitOK {
			t.Fatalf("partial generation rc=%d: %s", code, stderr)
		}
		requireCLIBytes(t, otherOutput, want)
	}
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runStandaloneCLI(t, source, "--clean"); code != ExitOK {
		t.Fatalf("orphan cleanup rc=%d: %s", code, stderr)
	}
	if _, err := os.Stat(otherOutput); !os.IsNotExist(err) {
		t.Fatalf("deleted source's output must be cleaned: %v", err)
	}
}

func TestGenerateRejectsMalformedManifestPaths(t *testing.T) {
	for _, tc := range []struct{ name, source, output string }{
		{"parent output", "old.gxml", "../outside_gen.go"},
		{"backslash parent", "old.gxml", `..\outside_gen.go`},
		{"absolute output", "old.gxml", "/outside_gen.go"},
		{"drive relative output", "old.gxml", "C:outside_gen.go"},
		{"parent source", "../old.gxml", "old_gen.go"},
		{"empty source", "", "old_gen.go"},
		{"empty output", "old.gxml", ""},
		{"directory output", "old.gxml", "."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := standaloneCLIProject(t)
			source := filepath.Join(root, "home.gxml")
			if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
				t.Fatalf("initial generation rc=%d: %s", code, stderr)
			}
			output := filepath.Join(root, "home_gen.go")
			want := readCLIFile(t, output)
			m := manifest{Version: manifestVersion, Entries: []entry{{Source: tc.source, Output: tc.output}}}
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, manifestPath(root), data)
			// Even ordinary generate must reject malformed ownership metadata;
			// it is also used to exclude sources from the cache fingerprint.
			code, _, stderr := runStandaloneCLI(t, source, "--no-cache")
			if code != ExitFailure || !strings.Contains(stderr, "Manifest") {
				t.Errorf("malformed manifest must fail closed, rc=%d: %s", code, stderr)
			}
			requireCLIBytes(t, output, want)
			requireCLIBytes(t, manifestPath(root), data)
		})
	}
}

func TestGenerateRejectsOutputAcrossModuleBoundary(t *testing.T) {
	for _, kind := range []string{"directory-link", "nested-module"} {
		for _, via := range []string{"out-dir", "manifest"} {
			t.Run(kind+"/"+via, func(t *testing.T) {
				root := standaloneCLIProject(t)
				source := filepath.Join(root, "home.gxml")
				if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
					t.Fatalf("initial generation rc=%d: %s", code, stderr)
				}
				want := readCLIFile(t, filepath.Join(root, "home_gen.go"))
				dir := filepath.Join(root, "boundary")
				actual := dir
				if kind == "directory-link" {
					actual = t.TempDir()
					if err := os.Symlink(actual, dir); err != nil {
						t.Skipf("directory symlink unavailable: %v", err)
					}
				} else {
					if err := os.MkdirAll(actual, 0o755); err != nil {
						t.Fatal(err)
					}
					writeCLIFile(t, filepath.Join(actual, "go.mod"), []byte("module example.org/other\n\ngo 1.26.0\n"))
				}
				target := filepath.Join(actual, "home_gen.go")
				writeCLIFile(t, target, want)
				args := []string{source, "--no-cache", "--check"}
				if via == "out-dir" {
					args = append(args, "--out-dir", dir)
				} else {
					m, err := loadManifest(manifestPath(root))
					if err != nil {
						t.Fatal(err)
					}
					m.Entries = append(m.Entries, entry{Source: "old.gxml", Output: "boundary/home_gen.go"})
					if err := saveManifest(manifestPath(root), m); err != nil {
						t.Fatal(err)
					}
					args = append(args, "--clean")
				}
				manifestBefore := readCLIFile(t, manifestPath(root))
				writeCLIFile(t, source, bytes.Replace(readCLIFile(t, source), []byte("home"), []byte("changed"), 1))
				code, _, stderr := runStandaloneCLI(t, args...)
				if code != ExitFailure || !strings.Contains(stderr, "module") {
					t.Errorf("output crossing module boundary must fail, rc=%d: %s", code, stderr)
				}
				requireCLIBytes(t, target, want)
				requireCLIBytes(t, filepath.Join(root, "home_gen.go"), want)
				requireCLIBytes(t, manifestPath(root), manifestBefore)
			})
		}
	}
}

func TestManifestCannotHideUserSourceFromCacheFingerprint(t *testing.T) {
	root := standaloneCLIProject(t)
	source := filepath.Join(root, "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
		t.Fatalf("initial generation rc=%d: %s", code, stderr)
	}
	m, err := loadManifest(manifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	// Metadata must not be able to claim a hand-written Go file and thereby
	// exclude it from the fingerprint used to decide whether diagnostics run.
	m.Entries = append(m.Entries, entry{Source: "unrelated.gxml", Output: "vm.go"})
	if err := saveManifest(manifestPath(root), m); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
		t.Fatalf("prime cache with extra manifest entry rc=%d: %s", code, stderr)
	}
	output := filepath.Join(root, "home_gen.go")
	wantOutput := readCLIFile(t, output)
	wantManifest := readCLIFile(t, manifestPath(root))
	userFile := filepath.Join(root, "vm.go")
	badGo := []byte("package demo\nfunc (\n")
	writeCLIFile(t, userFile, badGo)
	for _, flags := range [][]string{nil, {"--no-cache"}} {
		code, stdout, stderr := runStandaloneCLI(t, append([]string{source}, flags...)...)
		if code != ExitFailure || strings.Contains(stdout, "缓存命中") {
			t.Errorf("bad hand-written source must not be hidden by cache, rc=%d stdout=%s stderr=%s", code, stdout, stderr)
		}
		requireCLIBytes(t, output, wantOutput)
		requireCLIBytes(t, manifestPath(root), wantManifest)
		requireCLIBytes(t, userFile, badGo)
	}
}
