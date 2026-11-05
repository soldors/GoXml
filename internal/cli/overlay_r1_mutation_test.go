package cli

// R22-A / R2（F6）：可编译退化用 **Go `-overlay` 对独立源码副本**实施，而非在生产代码里放测试开关。
// 本测试：
//   1) 读取真实 generated_overlay.go，复制到临时文件并施加退化文本变异；
//   2) 用 `go test -overlay=<json>` 把变异副本映射到 generated_overlay.go，跑**健康行为回归**
//      （TestBehaviorCrossFieldRejected / TestBehaviorUnusedMissingNotBlocking）；
//   3) 断言：退化下目标健康测试实际 FAIL（rc!=0 且含目标 FAIL 行）；
//   4) 恢复：同一测试在不加 overlay 时 PASS。
// 变异文本使 healthy 测试翻红，而产物仍可编译。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func r2RepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(wd)) // internal/cli -> repo root
}

func runGoTestWithOverlay(t *testing.T, overlayJSON, runRe string) (int, string) {
	t.Helper()
	args := []string{"test", "./internal/cli", "-count=1", "-run", runRe}
	if overlayJSON != "" {
		args = append(args, "-overlay="+overlayJSON)
	}
	cmd := exec.Command(r21Go(t), args...)
	cmd.Dir = r2RepoRoot(t)
	cmd.Env = append(os.Environ(), "GOENV=off", "GOTOOLCHAIN=local")
	out, _ := cmd.CombinedOutput()
	rc := 0
	if cmd.ProcessState != nil {
		rc = cmd.ProcessState.ExitCode()
	}
	return rc, string(out)
}

func writeOverlayJSONForTest(t *testing.T, from, to string) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "ov.json")
	payload := `{"Replace":{` + jsonQuote(from) + `:` + jsonQuote(to) + `}}`
	if err := os.WriteFile(tmp, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func jsonQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func mutantCopy(t *testing.T, replacements [][2]string) (string, bool) {
	t.Helper()
	src := filepath.Join(r2RepoRoot(t), "internal", "cli", "generated_overlay.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, rp := range replacements {
		if !strings.Contains(text, rp[0]) {
			return "", false
		}
		text = strings.Replace(text, rp[0], rp[1], 1)
	}
	dst := filepath.Join(t.TempDir(), "generated_overlay_mutant.go")
	if err := os.WriteFile(dst, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst, true
}

// TestMutationGoOverlayCrossField 退化①：Analyzer 无用户 overlay（看磁盘）→ 跨包字段健康测试 FAIL。
func TestMutationGoOverlayCrossField(t *testing.T) {
	anchor := "if len(user) == 0 {\n\t\treturn nil, nil\n\t}"
	mut := "if true {\n\t\treturn nil, nil\n\t}\n\t_ = vmImportPaths\n\t_ = moduleRoot\n\t_ = goExe\n\tif len(user) == 0 {\n\t\treturn nil, nil\n\t}"
	mutant, ok := mutantCopy(t, [][2]string{{anchor, mut}})
	if !ok {
		t.Skip("generated_overlay.go 结构变化，跳过文本变异")
	}
	src := filepath.Join(r2RepoRoot(t), "internal", "cli", "generated_overlay.go")
	ov := writeOverlayJSONForTest(t, src, mutant)
	rc, out := runGoTestWithOverlay(t, ov, "^TestBehaviorCrossFieldRejected$")
	if rc == 0 || !strings.Contains(out, "--- FAIL") {
		t.Fatalf("退化①应使跨包字段健康测试 FAIL（rc=%d）；输出:\n%s", rc, out)
	}
	rc2, out2 := runGoTestWithOverlay(t, "", "^TestBehaviorCrossFieldRejected$")
	if rc2 != 0 {
		t.Fatalf("恢复后应 PASS（rc=%d）；输出:\n%s", rc2, out2)
	}
}

// TestMutationGoOverlayPreReadAll 退化②：读全部映射（预读未用缺失）→ 健康测试 FAIL。
func TestMutationGoOverlayPreReadAll(t *testing.T) {
	anchor := "if !selected[full] {\n\t\t\tcontinue // 未被 Go 选中：不读取（F3：平台排除/无关/未用缺失）\n\t\t}"
	mut := "// mutation: 不按选中过滤，直接读全部替换源\n\t\t_ = selected"
	mutant, ok := mutantCopy(t, [][2]string{{anchor, mut}})
	if !ok {
		t.Skip("generated_overlay.go 结构变化，跳过文本变异")
	}
	src := filepath.Join(r2RepoRoot(t), "internal", "cli", "generated_overlay.go")
	ov := writeOverlayJSONForTest(t, src, mutant)
	rc, out := runGoTestWithOverlay(t, ov, "^TestBehaviorUnusedMissingNotBlocking$")
	if rc == 0 || !strings.Contains(out, "--- FAIL") {
		t.Fatalf("退化②应使未用缺失健康测试 FAIL（rc=%d）；输出:\n%s", rc, out)
	}
	rc2, out2 := runGoTestWithOverlay(t, "", "^TestBehaviorUnusedMissingNotBlocking$")
	if rc2 != 0 {
		t.Fatalf("恢复后应 PASS（rc=%d）；输出:\n%s", rc2, out2)
	}
}
