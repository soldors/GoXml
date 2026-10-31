package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// R3-FOLLOWUP 切片 1（N4）：pipeline 级 createdDirs 措辞的真实反例。
//
// 评审 N4（P3）：createdDirs 的诚实措辞此前只有 reportCommitFailure 的手搓
// txnError 单测，缺一个**真实 generate 流程**里「新目录已创建、随后提交失败」
// 的反例。任务书要求：新目录部分创建后发生失败，不能谎称无磁盘改动；且故障
// 必须**确实到达暂存/提交阶段**，不能用 readOutput/前端即早停的 fixture 冒充。
//
// 真实文件系统触发（零注入、零 ACL、不改任何生产事务代码）：
//   - 源工程有一个名为 a_gen.go 的**子目录**，里面放 b.gxml；根目录放 a.gxml。
//   - --out-dir=gen（新目录）。两输入映射为：
//       a.gxml          → gen/a_gen.go        （目标**文件**，targetDir=gen）
//       a_gen.go/b.gxml → gen/a_gen.go/b_gen.go（targetDir=gen/a_gen.go）
//   - 阶段二开头集中 MkdirAll：gen 与 gen/a_gen.go 都被新建 → createdDirs 收两者。
//   - 提交按路径排序先处理 gen/a_gen.go：它此刻是**目录**（为另一输入建的
//     targetDir），rename(临时文件→目录) 真实失败 → 进入 commitBatchOps 的回滚
//     与 reportCommitFailure。这不是 readOutput 早停（缓存判定时 gen 尚不存在，
//     readOutput 走 IsNotExist→未命中），而是确实到达提交阶段的 rename 失败。
//
// 锁住的事实：错误如实点名「新建输出目录 … 保留」，绝不声称「磁盘恢复原状/
// 无任何变更」；两个新目录确实仍在磁盘上（可证明的残留范围）；产物未落盘、
// Manifest/缓存未登记、临时文件清干净。
// ============================================================================

func TestGenerateCommitFailureAfterNewDirsNamesCreatedDirs(t *testing.T) {
	root := tempProjectInRepo(t)
	// 根目录一页：a.gxml → gen/a_gen.go（目标文件）。
	writeSemanticPage(t, root, "a", "Title")
	// 名为 a_gen.go 的子目录里一页：b.gxml → gen/a_gen.go/b_gen.go，
	// 其 targetDir=gen/a_gen.go 恰好占据上一页的目标文件路径。
	writeSemanticPage(t, filepath.Join(root, "a_gen.go"), "b", "Title")

	outRoot := filepath.Join(root, "gen")
	var out, errb bytes.Buffer
	code := Run([]string{"generate", root, "--out-dir", outRoot}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d；stdout:\n%s\nstderr:\n%s", code, ExitFailure, out.String(), errb.String())
	}
	msg := errb.String()
	t.Logf("提交失败实际措辞（确实到达提交阶段、点名新建目录）:\n%s", msg)
	// 确实到达提交阶段（而非 readOutput/前端早停）：措辞来自 reportCommitFailure。
	if !strings.Contains(msg, "提交失败") {
		t.Fatalf("应是提交阶段失败，实际:\n%s", msg)
	}
	if strings.Contains(msg, "读取产物") {
		t.Errorf("不得用 readOutput 早停冒充提交阶段失败:\n%s", msg)
	}
	// 新目录已创建 → 如实点名，绝不声称无磁盘改动。
	createdSub := filepath.Join(outRoot, "a_gen.go")
	if !strings.Contains(msg, "新建输出目录") || !strings.Contains(msg, outRoot) || !strings.Contains(msg, createdSub) {
		t.Errorf("应点名新建输出目录 %s 与 %s:\n%s", outRoot, createdSub, msg)
	}
	if strings.Contains(msg, "磁盘恢复原状") || strings.Contains(msg, "磁盘无任何变更") {
		t.Errorf("新目录已创建后不得声称磁盘无改动/恢复原状:\n%s", msg)
	}

	// 可证明的残留范围：两个新目录确实仍在（不编造完整父目录枚举）。
	for _, d := range []string{outRoot, createdSub} {
		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() {
			t.Errorf("新建目录应仍保留 %s: err=%v", d, err)
		}
	}
	// 产物未落盘：gen/a_gen.go 是目录而非产物文件，b_gen.go 未创建。
	if fi, err := os.Stat(createdSub); err != nil || !fi.IsDir() {
		t.Errorf("gen/a_gen.go 应是目录（占位），不是落盘产物: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(createdSub, "b_gen.go")); !os.IsNotExist(err) {
		t.Errorf("提交失败不得落盘 b_gen.go: %v", err)
	}
	// Manifest 与缓存不被误登记（失败在登记之前）。
	if _, err := os.Stat(manifestPathOverride); !os.IsNotExist(err) {
		t.Errorf("提交失败不得写 Manifest: %v", err)
	}
	if _, err := os.Stat(cacheDir(root)); !os.IsNotExist(err) {
		t.Errorf("提交失败不得创建缓存目录: %v", err)
	}
	// 临时文件清干净（真实 remove 成功），只剩诚实点名的空目录。
	if residue := countTempResidue(t, outRoot); len(residue) > 0 {
		t.Errorf("提交失败后残留临时文件: %v", residue)
	}
}
