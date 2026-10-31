package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCommitBatchRestoresOldTargetOnFailure 锁定输出契约 §9 的核心承诺：
// 「磁盘写入失败…旧生成文件必须保持不变」。
//
// 三目标：a 已有、b 新建、c 是一个已存在目录（rename 文件到目录必失败，真实
// 文件系统语义，跨平台）。第三个提交失败时，第一个已被新产物覆盖的旧目标
// **必须恢复原样**，而不是留着新字节。
//
// 判别力：本用例只用稳定签名 commitBatch/stageBatch，不依赖任何新增结构字段，
// 因此能在「无回滚」的旧实现上编译并行为变红——旧实现把 a 覆盖成 NEW 后就不管了，
// 这里读到 NEW 即红；实现进程内回滚后 a 恢复 OLD-A 即绿。
func TestCommitBatchRestoresOldTargetOnFailure(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a_gen.go") // 已有
	b := filepath.Join(dir, "b_gen.go") // 新建
	c := filepath.Join(dir, "c_gen.go") // 目录 → rename 失败
	if err := os.WriteFile(a, []byte("OLD-A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(c, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c, "keep.txt"), []byte("KEEP"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, err := stageBatch([]string{a, b, c}, func(tmp string, i int) error {
		return os.WriteFile(tmp, []byte("NEW"), 0o644)
	})
	if err != nil {
		t.Fatalf("暂存应成功（故障要落在提交期）: %v", err)
	}
	if err := commitBatch(staged); err == nil {
		t.Fatal("第三个目标是目录，rename 必须失败——故障没有到达提交期")
	}

	// 核心断言（修前红）：已有目标 a 必须保持 OLD-A。
	data, err := os.ReadFile(a)
	if err != nil {
		t.Fatalf("提交失败后 a 应恢复存在: %v", err)
	}
	if string(data) != "OLD-A" {
		t.Errorf("提交失败后已有目标必须保持不变（契约 §9），实际 %q", data)
	}
	// 新建 b 撤回；目录 c 原样（绝不备份移走再删）。
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Errorf("新建目标 b 应被撤回: %v", err)
	}
	keep, err := os.ReadFile(filepath.Join(c, "keep.txt"))
	if err != nil || string(keep) != "KEEP" {
		t.Errorf("目录目标 c 应保持原样: %q err=%v", keep, err)
	}
}
