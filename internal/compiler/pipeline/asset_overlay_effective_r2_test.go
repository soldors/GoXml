package pipeline

// R25-A repair-r2 F-A4 单元：resolveEffectiveAsset / readAssetBytes 对 overlay 祖先删除/替换的
// 有效字节判定（直接测门禁分支，不经 go list；CLI 集成在 internal/cli/asset_overlay_r2_test.go）。
// 同时覆盖：精确删除、精确替换文件、未 overlay（读盘）、祖先删除（删）、祖先替换（不可等价=fail-closed）。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

func mustWriteBytes(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveEffectiveAssetKinds(t *testing.T) {
	root := t.TempDir()
	assetDir := filepath.Join(root, "assets")
	asset := filepath.Join(assetDir, "a.png")
	other := filepath.Join(root, "other")
	mustWriteBytes(t, asset, []byte("PNG"))
	mustWriteBytes(t, filepath.Join(other, "a.png"), []byte("IM2"))

	cases := []struct {
		name    string
		overlay map[string]string
		wantK   effectiveAssetKind
	}{
		{"not-overlaid", nil, effNotOverlaid},
		{"exact-delete", map[string]string{asset: ""}, effDeleted},
		{"exact-file", map[string]string{asset: filepath.Join(other, "a.png")}, effFile},
		{"ancestor-delete", map[string]string{assetDir: ""}, effDeleted},
		{"ancestor-replace", map[string]string{assetDir: other}, effUnprovable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, _ := resolveEffectiveAsset(filepath.Clean(asset), c.overlay)
			if k != c.wantK {
				t.Fatalf("kind=%v want %v", k, c.wantK)
			}
		})
	}
}

// readAssetBytes 必须对祖先删除/替换 fail-closed（非 nil diag），对精确替换校验目标字节，
// 对未 overlay 读磁盘。
func TestReadAssetBytesOverlayFailClosed(t *testing.T) {
	root := t.TempDir()
	assetDir := filepath.Join(root, "assets")
	asset := filepath.Join(assetDir, "a.png")
	mustWriteBytes(t, asset, []byte("PNG"))
	other := filepath.Join(root, "other")
	mustWriteBytes(t, filepath.Join(other, "a.png"), []byte("GOOD"))

	// 祖先删除 -> diag
	if _, diag := readAssetBytes(root, map[string]string{assetDir: ""}, nil, "", codegen.AssetRef{Path: "assets/a.png"}); diag == nil {
		t.Fatal("ancestor delete must fail-closed")
	}
	// 祖先替换为目录 -> unprovable -> diag
	if _, diag := readAssetBytes(root, map[string]string{assetDir: other}, nil, "", codegen.AssetRef{Path: "assets/a.png"}); diag == nil {
		t.Fatal("ancestor replace must fail-closed (unprovable)")
	}
	// 精确替换到合法文件 -> 读目标字节
	data, diag := readAssetBytes(root, map[string]string{asset: filepath.Join(other, "a.png")}, nil, "", codegen.AssetRef{Path: "assets/a.png"})
	if diag != nil || string(data) != "GOOD" {
		t.Fatalf("exact overlay file: data=%q diag=%v", data, diag)
	}
	// outputPaths 命中 -> fail-closed（F-A5 门禁）
	if _, diag := readAssetBytes(root, nil, map[string]bool{filepath.Clean(asset): true}, "", codegen.AssetRef{Path: "assets/a.png"}); diag == nil {
		t.Fatal("output-path Src must fail-closed")
	}
	// 未 overlay 且非输出 -> 读磁盘
	data, diag = readAssetBytes(root, nil, nil, "", codegen.AssetRef{Path: "assets/a.png"})
	if diag != nil || string(data) != "PNG" {
		t.Fatalf("disk read: data=%q diag=%v", data, diag)
	}
}
