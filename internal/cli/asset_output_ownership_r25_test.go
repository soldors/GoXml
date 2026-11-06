package cli

import (
	"path/filepath"
	"testing"
)

func TestR25ManifestClaimDoesNotOwnAsset(t *testing.T) {
	root := diskPNGAssetProject(t)
	r25Env(t)
	// Metadata alone cannot turn this ordinary PNG into a generated Go artifact.
	m := &manifest{Version: manifestVersion, Entries: []entry{{Source: "gone.gxml", Output: "ui/assets/a.png"}}}
	if err := saveManifest(manifestPath(root), m); err != nil {
		t.Fatal(err)
	}
	rc, out, se := runStandaloneCLI(t, filepath.Join(root, "ui", "home.gxml"), "--check")
	t.Logf("rc=%d out=%s err=%s", rc, out, se)
	if rc != ExitOK {
		t.Fatalf("MANIFEST-CLAIM: unproven registration must not override actual PNG bytes: %s", se)
	}
}
