package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// R3 切片 1：结构化事务错误（*txnError）与受控故障注入（fileOps）。
//
// 这些用例跨平台：通过按值注入 fileOps 的某个函数制造失败，**从不改写包级
// 变量或全局函数**，因此不会污染其它用例。Close 失败在本地 NTFS 上无法用真实
// 手段强制，只能受控注入；rename/remove 失败另有真实文件系统复现（见
// commit_txn_windows_test.go 与 TestStageDirUnavailableCleansPriorTemps）。
//
// 锁住的事实：
//   - 每个失败分支都把清理不掉的临时文件收进 residue，不再吞掉 cleanup 结果；
//   - cause 经 Unwrap 保留，errors.Is/As 可穿透；
//   - reportStageFailure 按结构化字段（residue/createdDirs）给诚实措辞，
//     不匹配任何中文串。
// ============================================================================

func containsPath(list []string, p string) bool {
	for _, x := range list {
		if x == p {
			return true
		}
	}
	return false
}

// TestStageWriteFailureCollectsResidue 写入失败且所有临时文件都删不掉时，
// residue 必须收齐两个临时文件，cause 保留，op/path 指明失败点。
func TestStageWriteFailureCollectsResidue(t *testing.T) {
	dir := t.TempDir()
	targets := []string{filepath.Join(dir, "a_gen.go"), filepath.Join(dir, "b_gen.go")}
	boom := errors.New("write boom")
	var tmpNames []string
	ops := realOps
	ops.remove = func(string) error { return errors.New("remove blocked") }

	_, err := stageBatchOps(targets, func(tmp string, i int) error {
		tmpNames = append(tmpNames, tmp)
		if i == 1 {
			return boom
		}
		return os.WriteFile(tmp, []byte("x"), 0o644)
	}, ops)

	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if te.op != "暂存" {
		t.Errorf("op = %q，期望 暂存", te.op)
	}
	if !errors.Is(err, boom) {
		t.Errorf("errors.Is 应保留 write cause: %v", err)
	}
	if len(te.residue) != 2 {
		t.Fatalf("两个临时文件都删不掉，residue 应为 2，实际 %v", te.residue)
	}
	for _, tn := range tmpNames {
		if !containsPath(te.residue, tn) {
			t.Errorf("residue 应含 %s，实际 %v", tn, te.residue)
		}
	}
	if te.path != tmpNames[1] {
		t.Errorf("path 应为失败项临时文件 %s，实际 %s", tmpNames[1], te.path)
	}
	if !strings.Contains(err.Error(), "残留") {
		t.Errorf("错误信息应列出残留: %v", err)
	}
	for _, target := range targets {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Errorf("暂存失败不得创建目标 %s", target)
		}
	}
}

// TestStageCloseFailureCollectsResidue Close 失败分支同样要收 residue、保留 cause。
// 注入的 close 先真正关闭 fd 再返回错误，避免句柄泄漏影响 t.TempDir 清场。
func TestStageCloseFailureCollectsResidue(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a_gen.go")
	closeBoom := errors.New("close boom")
	var tmpName string
	ops := realOps
	ops.close = func(f *os.File) error { _ = f.Close(); return closeBoom }
	ops.remove = func(string) error { return errors.New("remove blocked") }

	_, err := stageBatchOps([]string{target}, func(tmp string, i int) error {
		tmpName = tmp
		return os.WriteFile(tmp, []byte("x"), 0o644)
	}, ops)

	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if !errors.Is(err, closeBoom) {
		t.Errorf("errors.Is 应保留 close cause: %v", err)
	}
	if len(te.residue) != 1 || te.residue[0] != tmpName {
		t.Errorf("residue 应为 [%s]，实际 %v", tmpName, te.residue)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("暂存失败不得创建目标 %s", target)
	}
}

// TestStageDirUnavailableCleansPriorTemps 真实复现：第二个目标的目录是个普通文件，
// 阶段一在 dir 校验分支失败；此前已暂存的临时文件必须被真实清掉（residue 为空）。
func TestStageDirUnavailableCleansPriorTemps(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a_gen.go")
	blocker := filepath.Join(dir, "b_gen.go")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(blocker, "x_gen.go") // 其目录 = blocker（文件）

	_, err := stageBatch([]string{first, second}, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte("x"), 0o644)
	})
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if te.op != "暂存" || te.path != blocker {
		t.Errorf("op/path = %q/%q，期望 暂存/%s", te.op, te.path, blocker)
	}
	if len(te.residue) != 0 {
		t.Errorf("真实 remove 成功，residue 应为空，实际 %v", te.residue)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".goxml-") {
			t.Errorf("dir 校验失败后残留临时文件: %s", e.Name())
		}
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Errorf("不得创建第一个目标 %s", first)
	}
	data, _ := os.ReadFile(blocker)
	if string(data) != "not a dir" {
		t.Errorf("占位文件被改动: %q", data)
	}
}

// TestCommitFailureCollectsResidue 提交期 rename 失败 + 失败项临时文件删不掉：
// residue 收住它，cause 保留，op=提交；已提交的新建目标被进程内回滚撤回。
func TestCommitFailureCollectsResidue(t *testing.T) {
	dir := t.TempDir()
	t1 := filepath.Join(dir, "a_gen.go")
	t2 := filepath.Join(dir, "b_gen.go")
	tmp1 := filepath.Join(dir, ".goxml-1.tmp")
	tmp2 := filepath.Join(dir, ".goxml-2.tmp")
	for _, f := range []struct{ p, c string }{{tmp1, "NEW1"}, {tmp2, "NEW2"}} {
		if err := os.WriteFile(f.p, []byte(f.c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staged := []stagedFile{{target: t1, tmp: tmp1}, {target: t2, tmp: tmp2}}
	renameBoom := errors.New("rename boom")
	ops := realOps
	ops.rename = func(old, new string) error {
		if old == tmp2 {
			return renameBoom
		}
		return os.Rename(old, new)
	}
	ops.remove = func(p string) error {
		if p == tmp2 {
			return errors.New("remove blocked")
		}
		return os.Remove(p)
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
		t.Errorf("errors.Is 应保留 rename cause: %v", err)
	}
	if len(te.residue) != 1 || te.residue[0] != tmp2 {
		t.Errorf("residue 应为 [%s]，实际 %v", tmp2, te.residue)
	}
	// 进程内回滚：第一个新建目标已撤回，第二个从未创建；无已有目标故 restored 为空。
	if len(te.withdrawn) != 1 || te.withdrawn[0] != t1 {
		t.Errorf("withdrawn 应为 [%s]，实际 %v", t1, te.withdrawn)
	}
	if len(te.restored) != 0 || len(te.unrestored) != 0 {
		t.Errorf("无已有目标，restored/unrestored 应为空: %v / %v", te.restored, te.unrestored)
	}
	if _, err := os.Stat(t1); !os.IsNotExist(err) {
		t.Errorf("第一个新建目标应被撤回: %v", err)
	}
	if _, err := os.Stat(t2); !os.IsNotExist(err) {
		t.Errorf("第二个目标不应被创建: %v", err)
	}
	if _, err := os.Stat(tmp2); err != nil {
		t.Errorf("删不掉的临时文件应仍在: %v", err)
	}
}

// TestTxnErrorUnwrapPreservesCause 结构化错误的 Unwrap 链可被 errors.Is/As 穿透。
func TestTxnErrorUnwrapPreservesCause(t *testing.T) {
	root := errors.New("root cause")
	te := &txnError{op: "暂存", path: "/x/.goxml-1.tmp", cause: root, residue: []string{"/x/.goxml-1.tmp"}}
	wrapped := errors.Join(errors.New("外层"), te)
	if !errors.Is(wrapped, root) {
		t.Error("errors.Is 应穿透到 root cause")
	}
	var got *txnError
	if !errors.As(wrapped, &got) || got != te {
		t.Error("errors.As 应取回同一个 *txnError")
	}
	if !strings.Contains(te.Error(), "/x/.goxml-1.tmp") {
		t.Errorf("Error() 应含残留路径: %s", te.Error())
	}
}

// TestReportStageFailureClaims 三态措辞必须与磁盘现状一致，且分类不靠中文串。
func TestReportStageFailureClaims(t *testing.T) {
	boom := errors.New("boom")
	pristine := &txnError{op: "暂存", path: "/x/.goxml-1.tmp", cause: boom}

	// 无残留、无新建目录 → 真正的「磁盘无任何变更」。
	err := reportStageFailure(pristine, nil)
	if !strings.Contains(err.Error(), "磁盘无任何变更") {
		t.Errorf("零副作用应声称无变更: %v", err)
	}
	if strings.Contains(err.Error(), "已创建输出目录") {
		t.Errorf("没建目录不该点名目录: %v", err)
	}

	// 无残留但建过目录 → 如实点名，不再声称无变更。
	err = reportStageFailure(pristine, []string{"/x/gen"})
	if !strings.Contains(err.Error(), "已创建输出目录") || !strings.Contains(err.Error(), "/x/gen") {
		t.Errorf("建过目录应点名: %v", err)
	}
	if strings.Contains(err.Error(), "磁盘无任何变更") {
		t.Errorf("建过目录后不得声称无变更: %v", err)
	}

	// 有残留 → 透传 txnError 的残留清单，绝不说无变更；cause 仍可穿透。
	withResidue := &txnError{op: "暂存", cause: boom, residue: []string{"/x/.goxml-9.tmp"}}
	err = reportStageFailure(withResidue, []string{"/x/gen"})
	if strings.Contains(err.Error(), "磁盘无任何变更") {
		t.Errorf("有残留不得声称无变更: %v", err)
	}
	if !strings.Contains(err.Error(), "/x/.goxml-9.tmp") {
		t.Errorf("应列出残留: %v", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("reportStageFailure 应保留 cause: %v", err)
	}
	var te *txnError
	if !errors.As(err, &te) || len(te.residue) != 1 {
		t.Errorf("应能 errors.As 取回带 residue 的 txnError: %v", err)
	}
}

// TestStageAndCommitEmptyBatch 空批次不退化：不触发 write、不报错、无副作用。
func TestStageAndCommitEmptyBatch(t *testing.T) {
	called := false
	staged, err := stageBatch(nil, func(string, int) error { called = true; return nil })
	if err != nil || len(staged) != 0 {
		t.Fatalf("空暂存应成功且为空: staged=%v err=%v", staged, err)
	}
	if called {
		t.Error("空批次不应调用 write")
	}
	if err := commitBatch(nil); err != nil {
		t.Errorf("空提交应成功: %v", err)
	}
	if _, err := commitBatchOps(nil, realOps); err != nil {
		t.Errorf("空提交（ops）应成功: %v", err)
	}
}

// ============================================================================
// R3 切片 2：可检测提交失败的进程内回滚（输出契约 §9「磁盘写入失败…旧生成
// 文件必须保持不变」）。单文件 rename 是原子替换，但批次跨多文件：第 k 个失败
// 时前 k-1 个已被新产物覆盖，必须用同目录备份恢复旧目标、撤回新建目标。
// 这是进程内可检测错误的回滚，不是多文件 OS 原子，也不承诺断电恢复。
// ============================================================================

// TestCommitRollbackRestoresExistingAndWithdrawsNew 三目标：第一个已有、第二个
// 新建、第三个 rename 失败。核对旧目标字节/权限/mtime 原样恢复、新建目标撤回、
// 只清自己的临时/备份文件，回滚状态结构化可查。
func TestCommitRollbackRestoresExistingAndWithdrawsNew(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a_gen.go") // 已有
	b := filepath.Join(dir, "b_gen.go") // 新建
	c := filepath.Join(dir, "c_gen.go") // 已有，第三个 rename 失败
	tmpA := filepath.Join(dir, ".goxml-a.tmp")
	tmpB := filepath.Join(dir, ".goxml-b.tmp")
	tmpC := filepath.Join(dir, ".goxml-c.tmp")

	if err := os.WriteFile(a, []byte("OLD-A"), 0o600); err != nil {
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
	statA0, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	statC0, err := os.Stat(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct{ p, content string }{{tmpA, "NEW-A"}, {tmpB, "NEW-B"}, {tmpC, "NEW-C"}} {
		if err := os.WriteFile(f.p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	staged := []stagedFile{{target: a, tmp: tmpA}, {target: b, tmp: tmpB}, {target: c, tmp: tmpC}}
	renameBoom := errors.New("rename C boom")
	ops := realOps
	ops.rename = func(old, new string) error {
		if old == tmpC {
			return renameBoom
		}
		return os.Rename(old, new)
	}

	_, err = commitBatchOps(staged, ops)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if !errors.Is(err, renameBoom) {
		t.Errorf("errors.Is 应保留 rename cause: %v", err)
	}
	if te.op != "提交" || te.path != c {
		t.Errorf("op/path = %q/%q，期望 提交/%s", te.op, te.path, c)
	}
	// 回滚状态：a、c 恢复原样，b 撤回，无未恢复、无残留。
	if len(te.restored) != 2 || te.restored[0] != a || te.restored[1] != c {
		t.Errorf("restored 应为 [%s %s]，实际 %v", a, c, te.restored)
	}
	if len(te.withdrawn) != 1 || te.withdrawn[0] != b {
		t.Errorf("withdrawn 应为 [%s]，实际 %v", b, te.withdrawn)
	}
	if len(te.unrestored) != 0 || len(te.residue) != 0 {
		t.Errorf("回滚应彻底，unrestored/residue 应为空: %v / %v", te.unrestored, te.residue)
	}

	// 旧目标逐字节 + 权限 + mtime 原样。
	for _, chk := range []struct {
		path, content string
		stat0         os.FileInfo
	}{{a, "OLD-A", statA0}, {c, "OLD-C", statC0}} {
		data, err := os.ReadFile(chk.path)
		if err != nil {
			t.Fatalf("旧目标应恢复存在: %v", err)
		}
		if string(data) != chk.content {
			t.Errorf("%s 内容 = %q，期望 %q", chk.path, data, chk.content)
		}
		st, err := os.Stat(chk.path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode() != chk.stat0.Mode() {
			t.Errorf("%s 权限变了: %v → %v", chk.path, chk.stat0.Mode(), st.Mode())
		}
		if !st.ModTime().Equal(chk.stat0.ModTime()) {
			t.Errorf("%s mtime 变了: %v → %v", chk.path, chk.stat0.ModTime(), st.ModTime())
		}
	}
	// 新建目标撤回。
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Errorf("新建目标 b 应被撤回: %v", err)
	}
	// 只清自己的临时/备份文件：目录里一个 .goxml- 痕迹都不留。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".goxml-") {
			t.Errorf("回滚后残留临时/备份文件: %s", e.Name())
		}
	}
}

// TestGenerateDirTargetFailsClosedBeforeWrite 真实 CLI：第三个输出目标被一个
// 已存在目录占据。缓存判定 decideCacheHit 先 readOutput（用户接管/可读检查），
// 读目录失败 → 在**任何写盘之前**就 fail-closed。核对：退出码 2、点名坏目标、
// 前两页不写产物、目录原样、Manifest/缓存不登记、无临时残留。
//
// 这说明目录占位这类目标异常在提交期之前就被拦下；提交期 rename 失败的进程内
// 回滚另由事务层用例证明（含真实文件系统复现 TestCommitRollbackRealRenameFailure，
// 以及跨平台 TestCommitRollbackRestoresExistingAndWithdrawsNew）。提交失败 →
// 不登记 Manifest/缓存由冻结顺序（产物→Manifest→缓存）与 reportCommitFailure 保证。
func TestGenerateDirTargetFailsClosedBeforeWrite(t *testing.T) {
	root := tempProjectInRepo(t)
	writeSemanticPage(t, filepath.Join(root, "alpha"), "row", "Title")
	writeSemanticPage(t, filepath.Join(root, "beta"), "row", "Title")
	writeSemanticPage(t, filepath.Join(root, "gamma"), "row", "Title")

	outRoot := filepath.Join(root, "gen")
	blocked := filepath.Join(outRoot, "gamma", "row_gen.go")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "keep.txt"), []byte("KEEP"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", root, "--out-dir", outRoot}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d；stdout:\n%s\nstderr:\n%s", code, ExitFailure, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "读取产物") || !strings.Contains(errb.String(), "gamma") {
		t.Errorf("stderr 应点名读产物失败的坏目标:\n%s", errb.String())
	}
	// 任何产物都没写（失败在写盘之前）。
	for _, p := range []string{
		filepath.Join(outRoot, "alpha", "row_gen.go"),
		filepath.Join(outRoot, "beta", "row_gen.go"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("失败前不得写产物 %s: %v", p, err)
		}
	}
	// 目录目标原样保留。
	keep, err := os.ReadFile(filepath.Join(blocked, "keep.txt"))
	if err != nil || string(keep) != "KEEP" {
		t.Errorf("目录目标应保持原样: %q err=%v", keep, err)
	}
	// Manifest 与缓存不被误登记。
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Errorf("失败不得写 Manifest: %v", err)
	}
	if _, err := os.Stat(cacheDir(root)); !os.IsNotExist(err) {
		t.Errorf("失败不得创建缓存目录: %v", err)
	}
	if residue := countTempResidue(t, outRoot); len(residue) > 0 {
		t.Errorf("残留临时文件: %v", residue)
	}
}

// TestReportCommitFailureClaims 提交失败的措辞必须与回滚的实际结果一致。
func TestReportCommitFailureClaims(t *testing.T) {
	boom := errors.New("rename boom")
	clean := &txnError{
		op: "提交", path: "/x/c_gen.go", cause: boom,
		restored: []string{"/x/a_gen.go"}, withdrawn: []string{"/x/b_gen.go"},
	}

	// 彻底回滚、没建目录 → 磁盘恢复原状。
	err := reportCommitFailure(clean, nil)
	if !strings.Contains(err.Error(), "已回滚") || !strings.Contains(err.Error(), "磁盘恢复原状") {
		t.Errorf("彻底回滚应声称恢复原状: %v", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("应保留 cause: %v", err)
	}

	// 彻底回滚但建过目录 → 点名目录保留，不再说恢复原状。
	err = reportCommitFailure(clean, []string{"/x/gen"})
	if !strings.Contains(err.Error(), "/x/gen") || strings.Contains(err.Error(), "磁盘恢复原状") {
		t.Errorf("建过目录应点名且不说恢复原状: %v", err)
	}

	// 回滚不彻底（unrestored + residue）→ 绝不声称已回滚/恢复原状，透传清单。
	bad := &txnError{
		op: "提交", cause: boom,
		unrestored: []string{"/x/a_gen.go"}, residue: []string{"/x/a_gen.go.bak"},
	}
	err = reportCommitFailure(bad, nil)
	if strings.Contains(err.Error(), "磁盘恢复原状") || strings.Contains(err.Error(), "已回滚") {
		t.Errorf("回滚不彻底不得声称恢复原状: %v", err)
	}
	if !strings.Contains(err.Error(), "/x/a_gen.go") || !strings.Contains(err.Error(), "/x/a_gen.go.bak") {
		t.Errorf("应列出未恢复目标与残留: %v", err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("应保留 cause: %v", err)
	}
}

// ============================================================================
// R3 切片 3：恢复/清理本身也失败时的诚实残留报告 + 成功路径不退化。
//
// 切片 2 已实现 rollbackCommit 的 unrestored/residue 与 reportCommitFailure 的
// 诚实措辞；本切片用**真实 commitBatchOps 流程**（而非手搓 txnError）证明：
//   - 恢复（备份→目标 rename）失败 → 该目标进 unrestored、备份进 residue，
//     绝不谎称已恢复；原始提交 cause 仍经 errors.Is 保留（不被恢复错误顶替）；
//   - 撤回（remove 新建目标）失败 → 进 unrestored（即便 residue 为空也触发诚实措辞）；
//   - 成功路径删旧备份；删不掉时按警告（successResidue）上报而非致命错误，
//     不阻断后续 Manifest/缓存登记。
//
// 判别力（mutation red）：把 rollbackCommit 换成「吞掉恢复/撤回错误、一律记为
// 已恢复/已撤回」的朴素实现，下面两个失败用例行为变红（unrestored 该有却空）；
// 真实实现下变绿。这是行为失败，不是删符号造编译失败。
// ============================================================================

// TestCommitRollbackRestoreFailureReportsUnrestored 三目标 a 已有/b 新建/c 已有：
// c 的提交 rename 失败触发回滚，而 a 的**恢复** rename（备份→a）也失败。
// 核对：a 进 unrestored、a 的备份进 residue、c 仍恢复、b 仍撤回；原始提交 cause
// 经 errors.Is 保留；reportCommitFailure 绝不声称已回滚/恢复原状。
func TestCommitRollbackRestoreFailureReportsUnrestored(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a_gen.go") // 已有，恢复将失败
	b := filepath.Join(dir, "b_gen.go") // 新建
	c := filepath.Join(dir, "c_gen.go") // 已有，第三个提交 rename 失败
	tmpA := filepath.Join(dir, ".goxml-a.tmp")
	tmpB := filepath.Join(dir, ".goxml-b.tmp")
	tmpC := filepath.Join(dir, ".goxml-c.tmp")
	backupA := tmpA + ".bak"

	for _, f := range []struct{ p, content string }{
		{a, "OLD-A"}, {c, "OLD-C"},
		{tmpA, "NEW-A"}, {tmpB, "NEW-B"}, {tmpC, "NEW-C"},
	} {
		if err := os.WriteFile(f.p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staged := []stagedFile{{target: a, tmp: tmpA}, {target: b, tmp: tmpB}, {target: c, tmp: tmpC}}
	renameBoom := errors.New("rename C boom")
	restoreBoom := errors.New("restore A boom")
	ops := realOps
	ops.rename = func(old, new string) error {
		if old == tmpC {
			return renameBoom // c 提交失败
		}
		if old == backupA {
			return restoreBoom // a 恢复失败
		}
		return os.Rename(old, new)
	}

	_, err := commitBatchOps(staged, ops)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	// 原始提交 cause 必须保留，不被恢复错误顶替。
	if !errors.Is(err, renameBoom) {
		t.Errorf("errors.Is 应保留原始提交 cause: %v", err)
	}
	if te.op != "提交" || te.path != c {
		t.Errorf("op/path = %q/%q，期望 提交/%s", te.op, te.path, c)
	}
	// a 恢复失败 → unrestored；其备份删不掉（仍在）→ residue。c 恢复、b 撤回不受影响。
	if len(te.unrestored) != 1 || te.unrestored[0] != a {
		t.Errorf("unrestored 应为 [%s]，实际 %v", a, te.unrestored)
	}
	if !containsPath(te.residue, backupA) {
		t.Errorf("residue 应含恢复失败的备份 %s，实际 %v", backupA, te.residue)
	}
	if len(te.restored) != 1 || te.restored[0] != c {
		t.Errorf("restored 应为 [%s]，实际 %v", c, te.restored)
	}
	if len(te.withdrawn) != 1 || te.withdrawn[0] != b {
		t.Errorf("withdrawn 应为 [%s]，实际 %v", b, te.withdrawn)
	}
	// 措辞诚实：列出未恢复目标与残留，绝不说恢复原状。
	if !strings.Contains(err.Error(), "未恢复") || !strings.Contains(err.Error(), a) {
		t.Errorf("错误应点名未恢复目标: %v", err)
	}
	if !strings.Contains(err.Error(), backupA) || !strings.Contains(err.Error(), "残留") {
		t.Errorf("错误应列出残留备份: %v", err)
	}

	// 磁盘实际状态：a 恢复失败 → 仍带着已提交的新字节 NEW-A，原字节 OLD-A 滞留在
	// 残留备份 backupA 里（用户可据此手工恢复）；c 恢复原样、b 撤回。
	if data, err := os.ReadFile(a); err != nil || string(data) != "NEW-A" {
		t.Errorf("恢复失败时 a 应仍带新字节 NEW-A（原字节滞留备份）: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(backupA); err != nil || string(data) != "OLD-A" {
		t.Errorf("恢复失败的备份应保留原字节 OLD-A: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(c); err != nil || string(data) != "OLD-C" {
		t.Errorf("c 应恢复 OLD-C: %q err=%v", data, err)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Errorf("b 应被撤回: %v", err)
	}

	// reportCommitFailure：unrestored/residue 非空 → 透传清单，绝不声称已回滚。
	rep := reportCommitFailure(err, nil)
	if strings.Contains(rep.Error(), "磁盘恢复原状") || strings.Contains(rep.Error(), "已回滚") {
		t.Errorf("恢复失败不得声称已回滚/恢复原状: %v", rep)
	}
	if !strings.Contains(rep.Error(), a) || !strings.Contains(rep.Error(), backupA) {
		t.Errorf("报告应列出未恢复目标与残留: %v", rep)
	}
	if !errors.Is(rep, renameBoom) {
		t.Errorf("报告应保留原始 cause: %v", rep)
	}
}

// TestCommitRollbackWithdrawFailureReportsUnrestored 三目标 a 已有/b 新建/c 已有：
// c 提交 rename 失败触发回滚，而 b 的**撤回** remove 也失败。核对：b 进 unrestored
// （residue 为空——b 是目标不是临时/备份文件），a/c 仍恢复；原始 cause 保留；
// 仅 unrestored 非空也足以让 reportCommitFailure 拒绝声称恢复原状。
func TestCommitRollbackWithdrawFailureReportsUnrestored(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a_gen.go")
	b := filepath.Join(dir, "b_gen.go") // 新建，撤回将失败
	c := filepath.Join(dir, "c_gen.go")
	tmpA := filepath.Join(dir, ".goxml-a.tmp")
	tmpB := filepath.Join(dir, ".goxml-b.tmp")
	tmpC := filepath.Join(dir, ".goxml-c.tmp")

	for _, f := range []struct{ p, content string }{
		{a, "OLD-A"}, {c, "OLD-C"},
		{tmpA, "NEW-A"}, {tmpB, "NEW-B"}, {tmpC, "NEW-C"},
	} {
		if err := os.WriteFile(f.p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staged := []stagedFile{{target: a, tmp: tmpA}, {target: b, tmp: tmpB}, {target: c, tmp: tmpC}}
	renameBoom := errors.New("rename C boom")
	withdrawBoom := errors.New("withdraw B boom")
	ops := realOps
	ops.rename = func(old, new string) error {
		if old == tmpC {
			return renameBoom
		}
		return os.Rename(old, new)
	}
	ops.remove = func(p string) error {
		if p == b {
			return withdrawBoom // b 撤回失败
		}
		return os.Remove(p)
	}

	_, err := commitBatchOps(staged, ops)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应返回 *txnError，实际 %T: %v", err, err)
	}
	if !errors.Is(err, renameBoom) {
		t.Errorf("errors.Is 应保留原始提交 cause: %v", err)
	}
	if len(te.unrestored) != 1 || te.unrestored[0] != b {
		t.Errorf("unrestored 应为 [%s]，实际 %v", b, te.unrestored)
	}
	if len(te.withdrawn) != 0 {
		t.Errorf("撤回失败，withdrawn 应为空，实际 %v", te.withdrawn)
	}
	if len(te.restored) != 2 || te.restored[0] != a || te.restored[1] != c {
		t.Errorf("restored 应为 [%s %s]，实际 %v", a, c, te.restored)
	}
	// b 是目标而非临时/备份文件：进 unrestored，不进 residue。
	if len(te.residue) != 0 {
		t.Errorf("撤回失败的目标不算残留临时/备份，residue 应为空，实际 %v", te.residue)
	}
	if !strings.Contains(err.Error(), "未恢复") || !strings.Contains(err.Error(), b) {
		t.Errorf("错误应点名未恢复的新建目标: %v", err)
	}

	// 磁盘实际状态：b 撤回失败仍带着新字节，a/c 恢复原样。
	if data, err := os.ReadFile(b); err != nil || string(data) != "NEW-B" {
		t.Errorf("撤回失败的 b 应仍在（NEW-B）: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(a); err != nil || string(data) != "OLD-A" {
		t.Errorf("a 应恢复 OLD-A: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(c); err != nil || string(data) != "OLD-C" {
		t.Errorf("c 应恢复 OLD-C: %q err=%v", data, err)
	}

	// 仅 unrestored 非空（residue 空）也必须拒绝声称恢复原状。
	rep := reportCommitFailure(err, nil)
	if strings.Contains(rep.Error(), "磁盘恢复原状") || strings.Contains(rep.Error(), "已回滚") {
		t.Errorf("撤回失败不得声称已回滚/恢复原状: %v", rep)
	}
	if !strings.Contains(rep.Error(), b) {
		t.Errorf("报告应点名未恢复目标 b: %v", rep)
	}
}

// TestCommitSuccessRemovesBackups 反向护栏：真实成功路径下，已有目标被新产物覆盖、
// 旧备份全部删除、不留任何 .goxml- 痕迹，successResidue 为空。备份机制不得在成功
// 路径上残留备份或破坏正常覆盖。
func TestCommitSuccessRemovesBackups(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a_gen.go") // 已有
	b := filepath.Join(dir, "b_gen.go") // 新建
	c := filepath.Join(dir, "c_gen.go") // 已有
	for _, f := range []struct{ p, content string }{{a, "OLD-A"}, {c, "OLD-C"}} {
		if err := os.WriteFile(f.p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	newContent := []string{"NEW-A", "NEW-B", "NEW-C"}
	staged, err := stageBatch([]string{a, b, c}, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte(newContent[i]), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	successResidue, err := commitBatchOps(staged, realOps)
	if err != nil {
		t.Fatalf("成功路径不应失败: %v", err)
	}
	if len(successResidue) != 0 {
		t.Errorf("成功且备份可删，successResidue 应为空，实际 %v", successResidue)
	}
	for i, p := range []string{a, b, c} {
		data, err := os.ReadFile(p)
		if err != nil || string(data) != newContent[i] {
			t.Errorf("%s 应为 %q，实际 %q err=%v", p, newContent[i], data, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".goxml-") {
			t.Errorf("成功路径残留临时/备份文件: %s", e.Name())
		}
	}
}

// TestCommitSuccessBackupCleanupFailureIsWarning 成功路径上旧备份删不掉时：
// 产物已正确落盘，应按警告（successResidue）诚实上报残留，而**非**致命错误——
// 否则会阻断后续 Manifest/缓存登记（与缓存写失败同策）。核对 err==nil、
// successResidue 收住删不掉的备份、新内容已落盘、备份确实仍在。
func TestCommitSuccessBackupCleanupFailureIsWarning(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a_gen.go") // 已有，其备份将删不掉
	b := filepath.Join(dir, "b_gen.go") // 新建
	tmpA := filepath.Join(dir, ".goxml-a.tmp")
	tmpB := filepath.Join(dir, ".goxml-b.tmp")
	backupA := tmpA + ".bak"
	for _, f := range []struct{ p, content string }{
		{a, "OLD-A"}, {tmpA, "NEW-A"}, {tmpB, "NEW-B"},
	} {
		if err := os.WriteFile(f.p, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	staged := []stagedFile{{target: a, tmp: tmpA}, {target: b, tmp: tmpB}}
	cleanupBoom := errors.New("backup cleanup boom")
	ops := realOps
	ops.remove = func(p string) error {
		if p == backupA {
			return cleanupBoom
		}
		return os.Remove(p)
	}

	successResidue, err := commitBatchOps(staged, ops)
	if err != nil {
		t.Fatalf("备份清理失败不应让成功批次致命（会阻断 Manifest/缓存）: %v", err)
	}
	if len(successResidue) != 1 || successResidue[0] != backupA {
		t.Errorf("successResidue 应诚实收住删不掉的备份 [%s]，实际 %v", backupA, successResidue)
	}
	// 新产物已正确落盘（成功语义不因备份残留而退化）。
	if data, err := os.ReadFile(a); err != nil || string(data) != "NEW-A" {
		t.Errorf("a 应落盘 NEW-A: %q err=%v", data, err)
	}
	if data, err := os.ReadFile(b); err != nil || string(data) != "NEW-B" {
		t.Errorf("b 应落盘 NEW-B: %q err=%v", data, err)
	}
	// 删不掉的备份确实仍在（诚实，而非假装删掉）。
	if _, err := os.Stat(backupA); err != nil {
		t.Errorf("删不掉的备份应仍在: %v", err)
	}
}
