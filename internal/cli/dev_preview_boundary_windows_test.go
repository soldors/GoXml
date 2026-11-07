package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func devSnapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	m := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		m[rel] = sha256.Sum256(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDevPreviewOwnedAssetUsesManifestProof(t *testing.T) {
	root, dir := devFixture(t)
	file := filepath.Join(dir, "ui", "home.gxml")
	var out, errout bytes.Buffer
	if rc := Run([]string{"generate", file}, &out, &errout); rc != 0 {
		t.Fatalf("setup generate rc=%d: %s", rc, errout.String())
	}
	gen := filepath.Join(dir, "ui", "home_gen.go")
	asset := filepath.Join(dir, "replacement.png")
	if e := os.WriteFile(asset, cliTestPNG(t, 2, 2), 0600); e != nil {
		t.Fatal(e)
	}
	mapping, e := json.Marshal(map[string]any{"Replace": map[string]string{gen: asset}})
	if e != nil {
		t.Fatal(e)
	}
	overlay := filepath.Join(dir, "overlay.json")
	mustWrite(t, overlay, string(mapping))
	t.Setenv("GOFLAGS", os.Getenv("GOFLAGS")+" "+strconv.Quote("-overlay="+overlay))
	mustWrite(t, file, `<Goxml Version="1"><Page><Image Src="home_gen.go" Width="16" Height="16"/></Page></Goxml>`)
	before := devSnapshot(t, dir)
	out.Reset()
	errout.Reset()
	rc := runContextWithTool(context.Background(), []string{"preview", file, "--build-only", "--tool-root", root}, &out, &errout, "unused.exe")
	if rc != ExitFailure || !strings.Contains(errout.String(), "CODEGEN-ASSET-002") || !strings.Contains(errout.String(), "owned") {
		t.Fatalf("owned candidate was not refused: rc=%d out=%s err=%s", rc, out.String(), errout.String())
	}
	if !reflect.DeepEqual(before, devSnapshot(t, dir)) {
		t.Fatal("refused preview modified user module")
	}
}

func TestDevPreviewManifestDamageFailsBeforeHost(t *testing.T) {
	root, dir := devFixture(t)
	file := filepath.Join(dir, "ui", "home.gxml")
	for _, content := range []string{`{"version":`, `{"version":1,"entries":[{"source":"ui/home.gxml","output":"../foreign.go"}]}`} {
		mustWrite(t, manifestPath(dir), content)
		before := devSnapshot(t, dir)
		var out, errout bytes.Buffer
		rc := runContextWithTool(context.Background(), []string{"preview", file, "--build-only", "--tool-root", root}, &out, &errout, "unused.exe")
		if rc != ExitFailure || !strings.Contains(errout.String(), "Manifest") {
			t.Fatalf("bad manifest rc=%d: %s", rc, errout.String())
		}
		if !reflect.DeepEqual(before, devSnapshot(t, dir)) {
			t.Fatal("manifest refusal modified user module")
		}
	}
}

func TestDevPreviewActualBinaryCopiesEffectiveAssetAndKeepsUserIsolated(t *testing.T) {
	root, dir := devFixture(t)
	tool := devTool(t, root)
	asset := filepath.Join(dir, "ui", "asset.png")
	replacement := filepath.Join(dir, "replacement.png")
	disk, effective := cliTestPNG(t, 2, 2), cliTestPNG(t, 3, 3)
	if e := os.WriteFile(asset, disk, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(replacement, effective, 0600); e != nil {
		t.Fatal(e)
	}
	mapping, e := json.Marshal(map[string]any{"Replace": map[string]string{asset: replacement}})
	if e != nil {
		t.Fatal(e)
	}
	overlay := filepath.Join(dir, "overlay.json")
	mustWrite(t, overlay, string(mapping))
	t.Setenv("GOFLAGS", os.Getenv("GOFLAGS")+" "+strconv.Quote("-overlay="+overlay))
	file := filepath.Join(dir, "ui", "home.gxml")
	mustWrite(t, file, `<Goxml Version="1"><Page><Image Src="asset.png" Width="16" Height="16"/></Page></Goxml>`)
	canary := filepath.Join(dir, "business-init-ran")
	mustWrite(t, filepath.Join(dir, "ui", "canary.go"), "package ui\nimport \"os\"\nfunc init(){os.WriteFile("+strconv.Quote(canary)+",[]byte(\"ran\"),0600)}\n")
	before := devSnapshot(t, dir)
	cmd := exec.Command(tool, "preview", file, "--build-only", "--keep-host-dir", "--tool-root", root)
	cmd.Dir = dir
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("actual binary failed: %v %s", e, out)
	}
	const marker = "宿主保留："
	text := string(out)
	at := strings.LastIndex(text, marker)
	if at < 0 {
		t.Fatalf("no host evidence: %s", out)
	}
	host := strings.TrimSpace(text[at+len(marker):])
	// Only delete the exact freshly returned owned host after checking its marker.
	t.Cleanup(func() {
		if _, e := os.Stat(filepath.Join(host, ".goxml-preview-owned")); e == nil && strings.HasPrefix(filepath.Base(host), "goxml-preview-") {
			os.RemoveAll(host)
		}
	})
	got, e := os.ReadFile(filepath.Join(host, "view", "asset.png"))
	if e != nil || !bytes.Equal(got, effective) || bytes.Equal(got, disk) {
		t.Fatalf("host did not copy effective asset: %v", e)
	}
	if _, e := os.Stat(canary); !os.IsNotExist(e) {
		t.Fatal("user init executed")
	}
	if !reflect.DeepEqual(before, devSnapshot(t, dir)) {
		t.Fatal("preview changed user module")
	}
}
