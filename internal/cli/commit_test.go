package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// 1-5：两阶段事务提交（G4 已冻结：全或无 + 回滚对象限本批次目标）
// ============================================================================

// TestStageBatchWritesAllTemps 阶段一：全部写入临时文件，目标文件不存在。
func TestStageBatchWritesAllTemps(t *testing.T) {
	dir := t.TempDir()
	targets := []string{
		filepath.Join(dir, "a_gen.go"),
		filepath.Join(dir, "b_gen.go"),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	staged, err := stageBatch(targets, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte(targets[i]+"-content"), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 2 {
		t.Fatalf("应暂存 2 个文件, 实际 %d", len(staged))
	}
	for i, sf := range staged {
		if _, err := os.Stat(sf.tmp); err != nil {
			t.Fatalf("临时文件 %s 不存在: %v", sf.tmp, err)
		}
		if sf.target != targets[i] {
			t.Errorf("target = %q, 期望 %q", sf.target, targets[i])
		}
	}
	// 目标文件此刻必须不存在（阶段一不触碰）。
	for _, t2 := range targets {
		if _, err := os.Stat(t2); !os.IsNotExist(err) {
			t.Errorf("阶段一不得创建目标文件 %s", t2)
		}
	}
}

// TestStageBatchRollsBackOnFailure 任一文件写入失败：
// 全部临时文件被清理，目标文件不存在，错误明确。
func TestStageBatchRollsBackOnFailure(t *testing.T) {
	dir := t.TempDir()
	targets := []string{
		filepath.Join(dir, "a_gen.go"),
		filepath.Join(dir, "b_gen.go"),
	}

	boom := errors.New("boom on second file")
	_, err := stageBatch(targets, func(tmp string, i int) error {
		if i == 1 {
			return boom
		}
		return os.WriteFile(tmp, []byte("x"), 0o644)
	})
	if err == nil {
		t.Fatal("第二个文件失败应返回错误")
	}
	if !strings.Contains(err.Error(), "boom on second file") {
		t.Errorf("错误应保留原始原因: %v", err)
	}
	// 全部临时文件已清理。
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".goxml-") {
			t.Errorf("回滚后仍残留临时文件: %s", e.Name())
		}
	}
	// 目标文件不存在。
	for _, t2 := range targets {
		if _, err := os.Stat(t2); !os.IsNotExist(err) {
			t.Errorf("失败后目标文件不得存在: %s", t2)
		}
	}
}

// TestCommitBatchRenamesAll 阶段二：全部原子替换到目标。
func TestCommitBatchRenamesAll(t *testing.T) {
	dir := t.TempDir()
	targets := []string{
		filepath.Join(dir, "a_gen.go"),
		filepath.Join(dir, "b_gen.go"),
	}

	staged, err := stageBatch(targets, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte(targets[i]+"-new"), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := commitBatch(staged); err != nil {
		t.Fatal(err)
	}

	for i, t2 := range targets {
		data, err := os.ReadFile(t2)
		if err != nil {
			t.Fatalf("目标 %s 不存在: %v", t2, err)
		}
		if string(data) != targets[i]+"-new" {
			t.Errorf("%s 内容 = %q", t2, data)
		}
	}
	// 临时文件全部消失（已 rename）。
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".goxml-") {
			t.Errorf("提交后仍残留临时文件: %s", e.Name())
		}
	}
}

// TestCommitBatchOverwritesExisting 覆盖已有文件（本批次成功时才允许）。
func TestCommitBatchOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a_gen.go")
	if err := os.WriteFile(target, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, err := stageBatch([]string{target}, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte("NEW"), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := commitBatch(staged); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "NEW" {
		t.Errorf("成功批次应覆盖旧文件, 实际 %q", data)
	}
}

// TestExistingFileUntouchedOnFailure 核心语义（G4）：**批次失败时已有文件
// 内容、mtime 保持原始未动**。
func TestExistingFileUntouchedOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a_gen.go")
	oldContent := "OLD CONTENT"
	if err := os.WriteFile(target, []byte(oldContent), 0o644); err != nil {
		t.Fatal(err)
	}
	oldInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	// 批次第二个文件失败。
	_, err = stageBatch([]string{target, filepath.Join(dir, "b_gen.go")}, func(tmp string, i int) error {
		if i == 1 {
			return errors.New("boom")
		}
		return os.WriteFile(tmp, []byte("NEW"), 0o644)
	})
	if err == nil {
		t.Fatal("应失败")
	}

	// 已有文件必须原样。
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != oldContent {
		t.Errorf("失败后已有文件被改动：内容 = %q", data)
	}
	newInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !newInfo.ModTime().Equal(oldInfo.ModTime()) {
		t.Errorf("失败后已有文件 mtime 被改动: %v → %v", oldInfo.ModTime(), newInfo.ModTime())
	}
}

// TestEndToEndBatchAllOrNothing 端到端：批次的第二个文件生成失败时，
// 第一个文件的产物也**不得**落盘（全或无）。
func TestEndToEndBatchAllOrNothing(t *testing.T) {
	root := tempProjectInRepo(t)
	writeProject(t, root, "alpha/row.gxml")
	// beta 的 gxml 语法错误 → 管线失败。
	// beta 也要有合法的 vm.go（否则会在包名推导阶段失败，测不到事务语义）。
	if err := os.MkdirAll(filepath.Join(root, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "beta", "vm.go"), []byte("package beta\n\ntype VM struct{ Title string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "beta", "row.gxml"), []byte("<Goxml Version=\"1\"><Unclosed>"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", root}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitFailure)
	}
	// alpha 的产物不得存在（虽然 alpha 本身可生成）。
	if _, err := os.Stat(filepath.Join(root, "alpha", "row_gen.go")); !os.IsNotExist(err) {
		t.Error("全或无：批次内任一失败时，已生成的文件也不得落盘")
	}
	if !strings.Contains(errb.String(), "批次全部放弃") {
		t.Errorf("错误信息应说明全或无语义:\n%s", errb.String())
	}
}

// TestCommitBatchRenameFailureLeavesNoTempFile 提交期 rename 失败时，
// **失败那一个**的临时文件也必须清掉。
//
// 注入方式只用真实文件系统语义：把批次第二个目标做成一个已存在的空目录，
// Windows 下 os.Rename 报「Access is denied」且源临时文件仍在——实测见交付
// 目录 raw/。不需要权限、进程或改造系统函数，跑完即可删。
//
// 判别力：修复前 commitBatch 的清理列表**排除**当前失败项，
// 于是用户的包目录里会留下一个 `.goxml-*.tmp`（违反
// TASK-GENERATE-CLI-001 退出条件 5「临时文件在失败后清理干净」）。
func TestCommitBatchRenameFailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a_gen.go")
	// 第二个目标是一个已存在的目录：rename 文件到它上面必然失败。
	blocked := filepath.Join(dir, "b_gen.go")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "keep.txt"), []byte("KEEP"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, err := stageBatch([]string{first, blocked}, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte("NEW"), 0o644)
	})
	if err != nil {
		t.Fatalf("暂存应成功（故障要落在提交期，不是暂存期）: %v", err)
	}
	if len(staged) != 2 {
		t.Fatalf("应有 2 个暂存项，实际 %d", len(staged))
	}

	err = commitBatch(staged)
	if err == nil {
		t.Fatal("目标是已存在目录时 rename 必须失败——故障没有到达提交期")
	}
	if !strings.Contains(err.Error(), "提交") {
		t.Errorf("错误应指明提交期失败: %v", err)
	}

	// 已提交的那一个保留，且内容是新字节（提交不可逆，语义不变）。
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("第一个目标应已提交: %v", err)
	}
	if string(data) != "NEW" {
		t.Errorf("已提交内容 = %q，期望 NEW", data)
	}
	// 失败目标不得被破坏：目录与其中文件仍在。
	keep, err := os.ReadFile(filepath.Join(blocked, "keep.txt"))
	if err != nil {
		t.Fatalf("失败不应动到目标目录: %v", err)
	}
	if string(keep) != "KEEP" {
		t.Errorf("目标目录内容被改动: %q", keep)
	}
	// 核心断言：一个临时文件都不留。
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".goxml-") {
			t.Errorf("提交失败后残留临时文件: %s", e.Name())
		}
	}
}
