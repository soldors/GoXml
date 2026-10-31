//go:build windows

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// holdNoDelete 打开 path 的一个只读句柄，其共享模式不含 FILE_SHARE_DELETE。
// Windows 下 syscall.Open 固定用 FILE_SHARE_READ|FILE_SHARE_WRITE
// （GOROOT/src/syscall/syscall_windows.go:395），与 os.Open 额外带 DELETE 不同；
// 于是持有期间 os.Remove(path) 会因共享冲突**真实**失败。句柄由 t.Cleanup 关闭，
// 关闭后 t.TempDir 的 RemoveAll 才能清场。这是真实文件系统语义复现：
// 不改写任何包级函数、不破坏主仓权限。
func holdNoDelete(t *testing.T, path string) {
	t.Helper()
	h, err := syscall.Open(path, syscall.O_RDONLY, 0o644)
	if err != nil {
		t.Fatalf("打开占位句柄 %s: %v", path, err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
}

// TestStageWriteFailureReportsResidue 写入失败 + 临时文件删不掉时，
// 错误必须诚实列出残留——而不是吞掉 cleanup 结果后声称一切干净。
//
// 修前：stageBatch 的写入失败分支 `_ = os.Remove(tmpName)` 与 `cleanup(staged)`
// 都丢弃返回值，错误只有「写入暂存 b_gen.go: boom」，对躺在目录里的残留只字不提。
// 修后：*txnError.residue 收集删不掉的临时文件，Error() 逐条列出。
func TestStageWriteFailureReportsResidue(t *testing.T) {
	dir := t.TempDir()
	targets := []string{filepath.Join(dir, "a_gen.go"), filepath.Join(dir, "b_gen.go")}
	boom := errors.New("write boom")
	var held string
	_, err := stageBatch(targets, func(tmp string, i int) error {
		if i == 1 {
			holdNoDelete(t, tmp) // 真实共享冲突：此后 os.Remove(tmp) 必失败
			held = tmp
			return boom
		}
		return os.WriteFile(tmp, []byte("x"), 0o644)
	})
	if err == nil {
		t.Fatal("第二个写入失败应返回错误")
	}
	if !errors.Is(err, boom) {
		t.Errorf("应保留原始 write cause: %v", err)
	}
	// 故障必须确实到达目标分支：被占用的临时文件删不掉，仍躺在目录里。
	if _, statErr := os.Stat(held); statErr != nil {
		t.Fatalf("共享冲突注入未生效（临时文件竟被删除）: %v", statErr)
	}
	// 核心断言（修前红）：错误必须诚实报告删不掉的残留临时文件路径。
	if !strings.Contains(err.Error(), held) {
		t.Errorf("错误应报告残留临时文件 %s，实际: %v", held, err)
	}
}

// TestCommitRollbackRealRenameFailure 真实文件系统复现提交期回滚（非 ops 注入）：
// 第三个目标的临时文件被无 DELETE 共享句柄占用 → rename 真实失败（共享冲突）。
// 第一个目标是**只读**文件，用以在 Windows 上具体证明权限（只读属性）随
// 备份/恢复原样保留；第二个新建目标撤回。被占用的 tmpC 删不掉 → residue 诚实收住。
func TestCommitRollbackRealRenameFailure(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a_gen.go")
	b := filepath.Join(dir, "b_gen.go")
	c := filepath.Join(dir, "c_gen.go")

	if err := os.WriteFile(a, []byte("OLD-A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("OLD-C"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	for _, p := range []string{a, c} {
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	// 设只读：Windows 上对应 FILE_ATTRIBUTE_READONLY，rename 必须保留它。
	if err := os.Chmod(a, 0o444); err != nil {
		t.Fatal(err)
	}
	statA0, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	statC0, err := os.Stat(c)
	if err != nil {
		t.Fatal(err)
	}

	staged, err := stageBatch([]string{a, b, c}, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte("NEW"), 0o644)
	})
	if err != nil {
		t.Fatalf("暂存应成功（故障要落在提交期）: %v", err)
	}
	holdNoDelete(t, staged[2].tmp) // tmpC 被占用 → rename(tmpC, c) 真实失败

	err = commitBatch(staged)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if te.op != "提交" || te.path != c {
		t.Errorf("op/path = %q/%q，期望 提交/%s", te.op, te.path, c)
	}
	if len(te.restored) != 2 || te.restored[0] != a || te.restored[1] != c {
		t.Errorf("restored 应为 [%s %s]，实际 %v", a, c, te.restored)
	}
	if len(te.withdrawn) != 1 || te.withdrawn[0] != b {
		t.Errorf("withdrawn 应为 [%s]，实际 %v", b, te.withdrawn)
	}
	if len(te.unrestored) != 0 {
		t.Errorf("恢复都应成功，unrestored 应为空: %v", te.unrestored)
	}
	// 被占用的 tmpC 删不掉，residue 诚实收住它。
	if len(te.residue) != 1 || te.residue[0] != staged[2].tmp {
		t.Errorf("residue 应为 [%s]，实际 %v", staged[2].tmp, te.residue)
	}

	// 只读目标 a：字节 + 只读权限 + mtime 原样恢复。
	data, err := os.ReadFile(a)
	if err != nil || string(data) != "OLD-A" {
		t.Fatalf("只读目标应恢复 OLD-A: %q err=%v", data, err)
	}
	stA, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	if stA.Mode() != statA0.Mode() {
		t.Errorf("只读权限变了: %v → %v", statA0.Mode(), stA.Mode())
	}
	if !stA.ModTime().Equal(statA0.ModTime()) {
		t.Errorf("a mtime 变了: %v → %v", statA0.ModTime(), stA.ModTime())
	}
	// c 字节 + mtime 恢复。
	data, err = os.ReadFile(c)
	if err != nil || string(data) != "OLD-C" {
		t.Fatalf("c 应恢复 OLD-C: %q err=%v", data, err)
	}
	stC, err := os.Stat(c)
	if err != nil {
		t.Fatal(err)
	}
	if !stC.ModTime().Equal(statC0.ModTime()) {
		t.Errorf("c mtime 变了: %v → %v", statC0.ModTime(), stC.ModTime())
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Errorf("新建目标 b 应被撤回: %v", err)
	}
}
