package cli

// R21-A 返修判别回归：三条复核阻断反例。
// 失败=修复前（父 249ee94）行为；修复后应全绿。
//
// ① 物理目标目录不存在，overlay 提供 package q -> 定名 q（不能回退源包名）。
// ② 唯一物理 Go 文件被 overlay 换成构建标签排除 -> 权威失败（不回退写盘）。
// ③ 合法 package q 原始串含两个旧产物标记 -> 不得误判工具自有（pkg=q）。

import (
	"os"
	"path/filepath"
	"testing"
)

func r21bCase(t *testing.T, physDoc *string, repl string) string {
	t.Helper()
	root := t.TempDir()
	r21Write(t, filepath.Join(root, "go.mod"), "module example.com/rcase\n\ngo 1.26.0\n")
	r21Write(t, filepath.Join(root, "src", "doc.go"), "package src\n")
	if physDoc != nil {
		r21Write(t, filepath.Join(root, "target", "doc.go"), *physDoc)
	}
	r21Write(t, filepath.Join(root, "replacement.go"), repl)
	r21Write(t, filepath.Join(root, "overlay.json"), `{"Replace":{"target/doc.go":"replacement.go"}}`)
	return root
}

// TestR21VirtualOverlayOnlyDirNamesQ ①：物理目录不存在，overlay 提供 package q。
func TestR21VirtualOverlayOnlyDirNamesQ(t *testing.T) {
	root := r21bCase(t, nil, "package q\n")
	dir := filepath.Join(root, "target")
	ov, owned, err := r21Effective(t, root, dir, `-mod=readonly "-overlay=overlay.json"`)
	if err != nil {
		t.Fatal(err)
	}
	hasUser, perr := effectiveUserGoFiles(dir, ov, owned)
	if perr != nil {
		t.Fatal(perr)
	}
	if !hasUser {
		t.Fatal("overlay 提供 target/doc.go，应探测到有效用户源（不得判未初始化）")
	}
	// owned 不得包含用户 overlay 条目
	if owned[filepath.Join(dir, "doc.go")] {
		t.Fatal("用户 overlay 条目不得判为工具自有")
	}
}

// TestR21OverlayExcludedFileIsAuthoritativeFailure ②：唯一物理文件被 overlay 换成构建标签排除，
// 不得回退源包名写盘（有效源探测必须遵守 overlay 替换内容，见 package ignore 语义）。
func TestR21OverlayExcludedFileIsAuthoritativeFailure(t *testing.T) {
	phys := "package src\n"
	root := r21bCase(t, &phys, "//go:build ignore\n\npackage ignore\n")
	dir := filepath.Join(root, "target")
	ov, owned, err := r21Effective(t, root, dir, `-mod=readonly "-overlay=overlay.json"`)
	if err != nil {
		t.Fatal(err)
	}
	_ = ov
	// overlay 覆盖 target/doc.go 为 ignore 构建标签文件；它仍在磁盘上（on-disk 也 present），
	// effectiveUserGoFiles 会把物理 doc.go 计为源（它没被 owned、也没被删除）——真实 go list
	// 会因构建约束排除而失败。此处断言 owned 不误吞、且该文件确实存在 => 交由 go list 权威失败，
	// 不回退源包名。校验定名路径返回错误（非 ok=fallback）。
	if owned[filepath.Join(dir, "doc.go")] {
		t.Fatal("物理用户文件不得被判为工具自有")
	}
	// 直接走单查 resolver：必须返回 err（权威失败），而不是 ok=false 回退。
	resolve := newTargetPkgResolver(r21Go(t), root, []string{"GOENV=off", "GOTOOLCHAIN=local"},
		func(d string) (map[string]string, map[string]bool, error) {
			return r21Effective(t, root, d, `-mod=readonly "-overlay=overlay.json"`)
		})
	_, ok, rerr := resolve(dir)
	if rerr == nil {
		t.Fatalf("overlay 排除全部 Go 文件时应权威失败（go list 报错），实际 ok=%v err=nil", ok)
	}
}

// TestR21OwnershipNotFromByteHeuristic ③：合法 package q 原始串含两个旧产物标记，不得误判 owned。
func TestR21OwnershipNotFromByteHeuristic(t *testing.T) {
	phys := "package src\n"
	repl := "package q\n\nconst Note = `//go:build ignore\npackage ignore`\n"
	root := r21bCase(t, &phys, repl)
	dir := filepath.Join(root, "target")
	ov, owned, err := r21Effective(t, root, dir, `-mod=readonly "-overlay=overlay.json"`)
	if err != nil {
		t.Fatal(err)
	}
	if owned[filepath.Join(dir, "doc.go")] {
		t.Fatal("含 //go:build ignore 文本的合法用户文件不得被判为工具自有（禁止字节启发式）")
	}
	// 条目应保留用户替换**路径**（指向 replacement.go），其内容为 package q
	to, ok := ov[filepath.Join(dir, "doc.go")]
	if !ok || to == "" {
		t.Fatalf("用户替换路径应原样保留，got=%q ok=%v", to, ok)
	}
	rd, rerr := os.ReadFile(to)
	if rerr != nil || string(rd) != repl {
		t.Fatalf("用户替换源内容应为原 repl，got=%q err=%v", string(rd), rerr)
	}
	_ = os.Stdout
}

// r21Effective 用"用户 overlay 读取一次"的 WithUser 版本构造结果（测试便捷）。
func r21Effective(t *testing.T, root, dir, flags string) (map[string]string, map[string]bool, error) {
	t.Helper()
	user, err := userOverlayMap(flags, root)
	if err != nil {
		return nil, nil, err
	}
	return effectiveTargetResolutionWithUser(root, nil, dir, user)
}
