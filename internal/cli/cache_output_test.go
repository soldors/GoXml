package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// TASK-GENERATE-CACHE-001 切片 B4：可观测性输出与汇总
//
// 冻结词表（任务卡 §5）：
//   - 命中：     跳过 <out>（缓存命中）
//   - 未命中：   缓存未命中 <out>：<reason>
//   - 生成：     已生成 <out>
//   - 汇总：     命中 N，生成 M（仅 N>0 时输出）
//   - 缓存写失败：stderr 警告，退出码仍 0（D-4 裁决）
//
// 变红方式（已实测）：
//   - 汇总行改成无条件输出 → TestCacheOutputNoSummaryOnAllMiss 红；
//   - 缓存写失败改成返回错误 → TestCacheOutputCacheWriteFailureOnlyWarns 红。
// ============================================================================

// TestCacheOutputHitAndSummary：命中轮的输出形态与汇总。
func TestCacheOutputHitAndSummary(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("命中轮失败: %s", e)
	}
	lines := nonEmptyLines(out)

	if got := countPrefix(lines, "跳过 "); got != 1 {
		t.Fatalf("应有 1 行「跳过 …」，实际 %d:\n%s", got, out)
	}
	if !strings.Contains(out, "（缓存命中）") {
		t.Fatalf("命中行必须带「（缓存命中）」:\n%s", out)
	}
	if strings.Contains(out, "已生成") {
		t.Fatalf("命中轮不应出现「已生成」:\n%s", out)
	}
	if strings.Contains(out, "缓存未命中") {
		t.Fatalf("命中轮不应出现「缓存未命中」:\n%s", out)
	}
	if !hasLine(lines, "命中 1，生成 0") {
		t.Fatalf("缺少汇总行「命中 1，生成 0」:\n%s", out)
	}
}

// TestCacheOutputNoSummaryOnAllMiss：全未命中不输出汇总行（避免改变既有输出基线）。
func TestCacheOutputNoSummaryOnAllMiss(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("生成失败: %s", e)
	}
	// 注意：不能用 Contains("命中 ")——「缓存未命中 …」也含该子串。
	// 汇总行的形态是「命中 N，生成 M」，按行首 + 中文逗号判定。
	lines := nonEmptyLines(out)
	for _, line := range lines {
		if strings.HasPrefix(line, "命中 ") && strings.Contains(line, "，生成 ") {
			t.Fatalf("全未命中不应有命中汇总行: %q", line)
		}
	}
	if !strings.Contains(out, "已生成") {
		t.Fatalf("应有「已生成」:\n%s", out)
	}
}

// TestCacheOutputMissReasonBeforeGenerated：未命中原因出现在「已生成」之前。
//
// 顺序有意义：用户先看到「为什么没命中」，再看到「生成了什么」。
func TestCacheOutputMissReasonBeforeGenerated(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	if code, _, e := b3Run(t, src); code != ExitOK {
		t.Fatalf("首次生成失败: %s", e)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, append(data, []byte("\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, e := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("重新生成失败: %s", e)
	}
	iReason := strings.Index(out, "缓存未命中")
	iGenerated := strings.Index(out, "已生成")
	if iReason < 0 || iGenerated < 0 {
		t.Fatalf("缺少未命中行或已生成行:\n%s", out)
	}
	if iReason > iGenerated {
		t.Fatalf("未命中原因应出现在「已生成」之前:\n%s", out)
	}
	if !strings.Contains(out, "input-changed") {
		t.Fatalf("原因应为 input-changed:\n%s", out)
	}
}

// TestCacheOutputCacheWriteFailureOnlyWarns：缓存写失败只警告，退出码仍 0（D-4）。
//
// 构造：把缓存目录那一级占成普通文件，MkdirAll 必然失败。
func TestCacheOutputCacheWriteFailureOnlyWarns(t *testing.T) {
	root := b3Project(t)
	src := filepath.Join(root, "home.gxml")

	blocker := b3CacheDir(t) // 期望是目录，这里故意建成文件
	if err := os.WriteFile(blocker, []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errs := b3Run(t, src)
	if code != ExitOK {
		t.Fatalf("缓存写失败不得让生成失败：code=%d stderr=%s", code, errs)
	}
	if !strings.Contains(out, "已生成") {
		t.Fatalf("产物应正常生成:\n%s", out)
	}
	if !strings.Contains(errs, "缓存写入失败") {
		t.Fatalf("stderr 应警告缓存写入失败:\n%s", errs)
	}
	if stillFile, err := os.Stat(blocker); err != nil || stillFile.IsDir() {
		t.Fatalf("占位文件应保持不变（缓存写失败是正常分支）: %v", err)
	}
}

// ---- 文本辅助 ----

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func countPrefix(lines []string, prefix string) int {
	n := 0
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

func hasLine(lines []string, want string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
