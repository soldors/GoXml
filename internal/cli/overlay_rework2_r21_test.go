package cli

// R21-A rework2 判别回归：两个新分叉。
// ① 归一化重复路径（target/doc.go 与 ./target/doc.go）→ fail-closed（不得静默覆写）。
// ② 空路径键 → fail-closed。
// ③ 未选中的缺失替换文件不阻断（overlayMap 只保留路径，不预读）；选中源缺失才失败。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestR21OverlayDuplicateNormalizedPathFailClosed ①：归一化重复 from → 错误。
func TestR21OverlayDuplicateNormalizedPathFailClosed(t *testing.T) {
	root := t.TempDir()
	r21Write(t, filepath.Join(root, "replacement.go"), "package q\n")
	r21Write(t, filepath.Join(root, "overlay.json"),
		`{"Replace":{"target/doc.go":"replacement.go","./target/doc.go":"replacement.go"}}`)
	_, err := userOverlayMap(`-mod=readonly "-overlay=overlay.json"`, root)
	if err == nil {
		t.Fatal("归一化重复路径必须 fail-closed（不得静默覆写）")
	}
}

// TestR21OverlayEmptyKeyFailClosed ②：空路径键 → 错误。
func TestR21OverlayEmptyKeyFailClosed(t *testing.T) {
	root := t.TempDir()
	r21Write(t, filepath.Join(root, "replacement.go"), "package q\n")
	r21Write(t, filepath.Join(root, "overlay.json"),
		`{"Replace":{"":"replacement.go"}}`)
	_, err := userOverlayMap(`-mod=readonly "-overlay=overlay.json"`, root)
	if err == nil {
		t.Fatal("空路径键必须 fail-closed")
	}
}

func TestR21OverlayPreservesPathCase(t *testing.T) {
	root := t.TempDir()
	r21Write(t, filepath.Join(root, "overlay.json"),
		`{"Replace":{"Other/file.go":"missing-a.go","other/file.go":"missing-b.go"}}`)
	got, err := userOverlayMap("-overlay=overlay.json", root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[filepath.Join(root, "Other", "file.go")] != filepath.Join(root, "missing-a.go") ||
		got[filepath.Join(root, "other", "file.go")] != filepath.Join(root, "missing-b.go") {
		t.Fatalf("overlay keys with different case must keep their own mappings: %v", got)
	}
}

// TestR21OverlayUnusedMissingReplacementNotBlocking ③：未选中缺失替换不阻断；返回路径映射不预读。
func TestR21OverlayUnusedMissingReplacementNotBlocking(t *testing.T) {
	root := t.TempDir()
	r21Write(t, filepath.Join(root, "replacement.go"), "package q\n")
	r21Write(t, filepath.Join(root, "overlay.json"),
		`{"Replace":{"target/doc.go":"replacement.go","unrelated/x.go":"absent.go"}}`)
	m, err := userOverlayMap(`-mod=readonly "-overlay=overlay.json"`, root)
	if err != nil {
		t.Fatalf("未选中缺失替换不应阻断解析: %v", err)
	}
	// 应保留路径（不读字节）；absent.go 的 to 成为绝对路径，但未被读取
	if _, ok := m[filepath.Join(root, "target", "doc.go")]; !ok {
		t.Fatal("应保留 target/doc.go 映射")
	}
	if _, statErr := os.Stat(filepath.Join(root, "absent.go")); !os.IsNotExist(statErr) {
		t.Fatal("前置：absent.go 确实缺失")
	}
}

// TestR21OverlaySelectedMissingReplacementFails ③b：选中的替换源缺失 → 失败。
func TestR21OverlaySelectedMissingReplacementFails(t *testing.T) {
	root := t.TempDir()
	r21Write(t, filepath.Join(root, "go.mod"), "module example.com/selmiss\n\ngo 1.26.0\n")
	r21Write(t, filepath.Join(root, "src", "home.gxml"),
		`<Goxml Version="1"><Page Route="/"><Text Value="x"/></Page></Goxml>`)
	r21Write(t, filepath.Join(root, "target", "doc.go"), "package src\n")
	// overlay 把被选中的 target/doc.go 指向不存在的 replacement.go
	r21Write(t, filepath.Join(root, "overlay.json"),
		`{"Replace":{"target/doc.go":"missing_repl.go"}}`)
	ov, owned, err := r21Effective(t, root, filepath.Join(root, "target"), `-mod=readonly "-overlay=overlay.json"`)
	if err != nil {
		t.Fatal(err)
	}
	_, _, rerr := resolveTargetPackageGo(r21Go(t), root, filepath.Join(root, "target"),
		[]string{"GOENV=off", "GOTOOLCHAIN=local"}, ov, owned)
	if rerr == nil {
		t.Fatal("选中文件的替换源缺失必须失败（不得静默降级）")
	}
}
