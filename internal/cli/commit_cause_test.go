package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// R3-FOLLOWUP 切片 1（N1/N2）：暂存/提交失败时，close 与 cleanup remove 的
// **原始 cause** 必须经 errors.Is/As 可达，且不被主错误顶替。
//
// 缺陷（DeepSeek 审 d13ccf5 §N1/N2）：
//   - N1：stageBatchOps 写入失败分支 `_ = ops.close(tmp)` 丢弃 close 错误；
//   - N2：cleanup 只返回残留路径，丢弃每个 remove 的原始错误。
// 于是「写入失败 + close 也失败 + 多个 remove 失败」的组合里，用户与调用方
// 只能看到主错误（写入/提交），close 与 remove 的真实成因彻底丢失。
//
// 判别力：下面两条用例断言 errors.Is 能穿透到 close/remove 的哨兵错误。
// 修前这些 cause 不可达 → 行为变红；修后变绿。不是删符号造编译失败。
// 受控注入按值替换 fileOps，从不改写包级 realOps（与既有事务用例同策）。
// ============================================================================

// TestStageWriteFailurePreservesCloseAndRemoveCauses N1+N2 组合：
// 第二个临时文件写入失败（writeBoom），其 close 也失败（closeBoom），
// 清理两个临时文件时 remove 各自失败（removeBoomA/removeBoomB）。
// 核对：主错误仍是写入失败（op/path/cause 不被顶替），close 与两个 remove
// 的原始 cause 都经 errors.Is 可达，残留收齐两个临时文件，磁盘上临时文件
// 确实仍在、目标未被创建。
func TestStageWriteFailurePreservesCloseAndRemoveCauses(t *testing.T) {
	dir := t.TempDir()
	targets := []string{filepath.Join(dir, "a_gen.go"), filepath.Join(dir, "b_gen.go")}
	writeBoom := errors.New("write boom")
	closeBoom := errors.New("close boom")
	removeBoomA := errors.New("remove boom A")
	removeBoomB := errors.New("remove boom B")

	var tmpNames []string
	closeCalls := 0
	ops := realOps
	// 第一次 close（首个暂存成功项）正常；第二次 close（写入失败项）返回 closeBoom。
	// 始终真正关闭 fd，避免句柄泄漏影响 t.TempDir 清场。
	ops.close = func(f *os.File) error {
		closeCalls++
		_ = f.Close()
		if closeCalls >= 2 {
			return closeBoom
		}
		return nil
	}
	ops.remove = func(p string) error {
		switch p {
		case tmpNames[0]:
			return removeBoomA
		case tmpNames[1]:
			return removeBoomB
		}
		return os.Remove(p)
	}

	_, err := stageBatchOps(targets, func(tmp string, i int) error {
		tmpNames = append(tmpNames, tmp)
		if i == 1 {
			return writeBoom
		}
		return os.WriteFile(tmp, []byte("x"), 0o644)
	}, ops)

	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if te.op != "暂存" || te.path != tmpNames[1] {
		t.Errorf("op/path = %q/%q，期望 暂存/%s", te.op, te.path, tmpNames[1])
	}
	// 主错误不被顶替：cause 仍是写入失败，且不是 close/remove。
	if !errors.Is(err, writeBoom) {
		t.Errorf("errors.Is 应保留主写入 cause: %v", err)
	}
	if !errors.Is(te.cause, writeBoom) || errors.Is(te.cause, closeBoom) {
		t.Errorf("te.cause 应是写入失败而非被 close 顶替: %v", te.cause)
	}
	// N1：close 原始 cause 可达。
	if !errors.Is(err, closeBoom) {
		t.Errorf("N1：errors.Is 应穿透到 close cause: %v", err)
	}
	// N2：两个 remove 原始 cause 都可达。
	if !errors.Is(err, removeBoomA) {
		t.Errorf("N2：errors.Is 应穿透到第一个 remove cause: %v", err)
	}
	if !errors.Is(err, removeBoomB) {
		t.Errorf("N2：errors.Is 应穿透到第二个 remove cause: %v", err)
	}
	// 残留收齐两个删不掉的临时文件。
	if len(te.residue) != 2 || !containsPath(te.residue, tmpNames[0]) || !containsPath(te.residue, tmpNames[1]) {
		t.Errorf("residue 应含两个临时文件 %v，实际 %v", tmpNames, te.residue)
	}
	// 错误文本应如实 surfaced close/remove 成因（不只丢给 errors.Is）。
	if !strings.Contains(err.Error(), closeBoom.Error()) {
		t.Errorf("错误文本应含 close 成因: %v", err)
	}
	if !strings.Contains(err.Error(), removeBoomA.Error()) || !strings.Contains(err.Error(), removeBoomB.Error()) {
		t.Errorf("错误文本应含两个 remove 成因: %v", err)
	}
	// 磁盘实际状态：两个临时文件删不掉仍在，目标未被创建。
	for _, tn := range tmpNames {
		if _, err := os.Stat(tn); err != nil {
			t.Errorf("删不掉的临时文件应仍在 %s: %v", tn, err)
		}
	}
	for _, target := range targets {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("暂存失败不得创建目标 %s: %v", target, err)
		}
	}
}

// TestCommitFailurePreservesCleanupRemoveCauses N2 在提交路径：
// 三目标批次在第二个目标提交 rename 失败（renameBoom）。此时 tmp1 已 rename
// 落盘（随后回滚撤回），tmp2（失败项）与 tmp3（尚未处理）都仍躺在磁盘上；
// 清理这两个临时文件时 remove 各自失败（removeBoom2/removeBoom3）。核对：
// 主错误仍是提交 rename 失败，两个 remove 原始 cause 经 errors.Is 可达，
// 已提交的新建目标被撤回，残留收住 tmp2/tmp3 且确实仍在磁盘上。
func TestCommitFailurePreservesCleanupRemoveCauses(t *testing.T) {
	dir := t.TempDir()
	t1 := filepath.Join(dir, "a_gen.go")
	t2 := filepath.Join(dir, "b_gen.go")
	t3 := filepath.Join(dir, "c_gen.go")
	tmp1 := filepath.Join(dir, ".goxml-1.tmp")
	tmp2 := filepath.Join(dir, ".goxml-2.tmp")
	tmp3 := filepath.Join(dir, ".goxml-3.tmp")
	for _, f := range []struct{ p, c string }{{tmp1, "NEW1"}, {tmp2, "NEW2"}, {tmp3, "NEW3"}} {
		if err := os.WriteFile(f.p, []byte(f.c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staged := []stagedFile{{target: t1, tmp: tmp1}, {target: t2, tmp: tmp2}, {target: t3, tmp: tmp3}}
	renameBoom := errors.New("rename boom")
	removeBoom2 := errors.New("remove boom 2")
	removeBoom3 := errors.New("remove boom 3")
	ops := realOps
	ops.rename = func(old, new string) error {
		if old == tmp2 {
			return renameBoom
		}
		return os.Rename(old, new)
	}
	ops.remove = func(p string) error {
		switch p {
		case tmp2:
			return removeBoom2
		case tmp3:
			return removeBoom3
		}
		return os.Remove(p) // 撤回新建目标 t1 走真实 remove，成功；tmp1 已 rename 走，IsNotExist 被忽略
	}

	_, err := commitBatchOps(staged, ops)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if te.op != "提交" || te.path != t2 {
		t.Errorf("op/path = %q/%q，期望 提交/%s", te.op, te.path, t2)
	}
	if !errors.Is(err, renameBoom) {
		t.Errorf("errors.Is 应保留主提交 rename cause: %v", err)
	}
	if !errors.Is(te.cause, renameBoom) || errors.Is(te.cause, removeBoom2) {
		t.Errorf("te.cause 应是提交 rename 失败而非被 remove 顶替: %v", te.cause)
	}
	// N2：两个 cleanup remove 原始 cause 可达。
	if !errors.Is(err, removeBoom2) {
		t.Errorf("N2：errors.Is 应穿透到失败项 remove cause: %v", err)
	}
	if !errors.Is(err, removeBoom3) {
		t.Errorf("N2：errors.Is 应穿透到未处理项 remove cause: %v", err)
	}
	if len(te.residue) != 2 || !containsPath(te.residue, tmp2) || !containsPath(te.residue, tmp3) {
		t.Errorf("residue 应含 tmp2/tmp3，实际 %v", te.residue)
	}
	// 第一个新建目标已撤回（真实 remove 成功）。
	if len(te.withdrawn) != 1 || te.withdrawn[0] != t1 {
		t.Errorf("withdrawn 应为 [%s]，实际 %v", t1, te.withdrawn)
	}
	for _, target := range []string{t1, t2, t3} {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("目标不应存在 %s: %v", target, err)
		}
	}
	// tmp1 已 rename 落盘再撤回，故不在；tmp2/tmp3 删不掉仍在。
	if _, err := os.Stat(tmp1); !os.IsNotExist(err) {
		t.Errorf("tmp1 已 rename 走，不应作为临时文件存在: %v", err)
	}
	for _, tn := range []string{tmp2, tmp3} {
		if _, err := os.Stat(tn); err != nil {
			t.Errorf("删不掉的临时文件应仍在 %s: %v", tn, err)
		}
	}
}

// TestStageWriteFailureNoExtraCausesWhenCleanupSucceeds 反向护栏（无失败新增 cause）：
// 写入失败但 close 与 cleanup remove 都成功时，不得凭空多出次错误——extraCauses
// 为空、residue 为空，主错误仍是写入失败。保住「无失败不改成功/缓存路径」：
// 次错误只在真有 close/remove 失败时才出现。
func TestStageWriteFailureNoExtraCausesWhenCleanupSucceeds(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a_gen.go")
	writeBoom := errors.New("write boom")
	var tmpName string
	// close/remove 均用真实操作（成功）：写入失败项的临时文件能被真正清掉。
	_, err := stageBatchOps([]string{target}, func(tmp string, i int) error {
		tmpName = tmp
		return writeBoom
	}, realOps)

	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if !errors.Is(err, writeBoom) {
		t.Errorf("errors.Is 应保留主写入 cause: %v", err)
	}
	if len(te.extraCauses) != 0 {
		t.Errorf("close/remove 都成功时不应有次错误，实际 %v", te.extraCauses)
	}
	if len(te.residue) != 0 {
		t.Errorf("真实清理成功，residue 应为空，实际 %v", te.residue)
	}
	if _, err := os.Stat(tmpName); !os.IsNotExist(err) {
		t.Errorf("写入失败项的临时文件应被真正清掉: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("暂存失败不得创建目标: %v", err)
	}
}
