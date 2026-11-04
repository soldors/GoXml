//go:build windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAssetCacheIdentity_SameBytesJunction(t *testing.T) {
	root := r16ImageProject(t)
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	target := filepath.Join(root, "ui")
	source := filepath.Join(target, "home.gxml")
	gen := filepath.Join(target, "home_gen.go")
	outDir := t.TempDir()
	data := readCLIFile(t, filepath.Join(target, "a", "logo.png"))
	writeCLIFile(t, filepath.Join(outDir, "logo.png"), data)
	rc, out, se := runStandaloneCLI(t, source)
	if rc != ExitOK {
		t.Fatalf("prime rc=%d stdout=%s stderr=%s", rc, out, se)
	}
	oldGen, oldMan := readCLIFile(t, gen), readCLIFile(t, manifestPath(root))
	oldCache := r12CacheRecords(t, root)
	oldStat, err := os.Stat(gen)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(target, "a"), filepath.Join(target, "a-real")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(target, "a")
	if data, err := exec.Command("cmd", "/c", "mklink", "/J", link, outDir).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, data)
	}
	defer os.Remove(link)
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("junction Lstat=%v; external target has identical PNG bytes", fi.Mode())
	rc, out, se = runStandaloneCLI(t, source)
	newStat, err := os.Stat(gen)
	if err != nil {
		t.Fatal(err)
	}
	if string(oldGen) != string(readCLIFile(t, gen)) || string(oldMan) != string(readCLIFile(t, manifestPath(root))) || !oldStat.ModTime().Equal(newStat.ModTime()) {
		t.Fatal("unexpected writes")
	}
	if !reflect.DeepEqual(r12CacheRecords(t, root), oldCache) {
		t.Fatal("unsafe asset altered cache records")
	}
	t.Logf("normal rc=%d stdout=%q stderr=%q", rc, out, se)
	forced, fOut, fErr := runStandaloneCLI(t, source, "--no-cache")
	t.Logf("forced rc=%d stdout=%q stderr=%q", forced, fOut, fErr)
	if forced != ExitFailure || !strings.Contains(fErr, "CODEGEN-ASSET-001") || !strings.Contains(fErr, "junction or reparse point") {
		t.Fatal("forced-miss control not rejected by actual boundary gate")
	}
	if rc != ExitFailure || !strings.Contains(se, "CODEGEN-ASSET-001") || strings.Contains(out, "缓存命中") {
		t.Fatal("same-byte package-external junction must not be accepted on cache path")
	}
}
