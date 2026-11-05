package cli

// R22-A / R1 可编译退化变异（F4）：用 r1MutationMode 打开修复前的退化路径，证明：
//   - 退化① skip-go-selection（按目录全读，不按 Go 选源）：跨包/依赖字段假接受（BIND 不再报）。
//   - 退化② ignore-selection-filter（读全部映射）：非 Go 未用缺失替换 → 误拒绝。
// 健康对照：同一材料在默认（无变异）下通过。
// 这些是**可编译**的行为退化（不是编译错误）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withMutation(t *testing.T, mode string, fn func()) {
	t.Helper()
	prev := r1MutationMode
	r1MutationMode = mode
	defer func() { r1MutationMode = prev }()
	fn()
}

// TestMutationSkipsGoSelectionCausesFalseAccept 退化①：应使跨包字段假接受（写盘且不报 BIND）。
func TestMutationSkipsGoSelectionCausesFalseAccept(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "model", "vm.go"), "package model\ntype VM struct { Title string }\n")
	r22Write(t, filepath.Join(root, "vm.txt"), "package model\ntype VM struct { Title int }\n")
	r22Write(t, filepath.Join(root, "views", "home", "doc.go"), "package home\n")
	r22Write(t, filepath.Join(root, "views", "home", "home.gxml"),
		`<Goxml Version="1" ViewModel="model.VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"model/vm.go":"vm.txt"}}`)
	r1SetEnv(t)
	var code int
	var se string
	withMutation(t, "skip-go-selection", func() {
		code, _, se = runCLIStrings(t, filepath.Join(root, "views", "home", "home.gxml"))
	})
	// 退化下：不应报 BIND-004（假接受），否则说明变异未生效——明确失败提示。
	if strings.Contains(se, "GXML-BIND-004") {
		t.Fatalf("退化①应复现假接受（不报 BIND），实际仍报 BIND，变异无效：%s", se)
	}
	if code != ExitOK {
		t.Fatalf("退化①应假接受 rc0，实际 rc=%d stderr=%s", code, se)
	}
	if _, err := os.Stat(filepath.Join(root, "views", "home", "home_gen.go")); err != nil {
		t.Fatal("退化①应写出坏产物（假接受）")
	}
}

// TestMutationIgnoresSelectionCausesFalseReject 退化②：非 Go 未用缺失替换 → 误拒绝。
func TestMutationIgnoresSelectionCausesFalseReject(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\ntype VM struct { Title string }\n")
	r22Write(t, filepath.Join(root, "ui", "home.gxml"),
		`<Goxml Version="1" ViewModel="VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"ui/notes.txt":"not-present.txt"}}`)
	r1SetEnv(t)
	var code int
	withMutation(t, "ignore-selection-filter", func() {
		code, _, _ = runCLIStrings(t, filepath.Join(root, "ui", "home.gxml"))
	})
	if code == ExitOK {
		t.Fatalf("退化②应误拒绝（rc!=0），实际 rc0，变异无效")
	}
}

// TestMutationHealthyControlDefaultPasses 健康对照：默认（无变异）同材料通过。
func TestMutationHealthyControlDefaultPasses(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\ntype VM struct { Title string }\n")
	r22Write(t, filepath.Join(root, "ui", "home.gxml"),
		`<Goxml Version="1" ViewModel="VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"ui/notes.txt":"not-present.txt"}}`)
	r1SetEnv(t)
	if r1MutationMode != "" {
		t.Fatal("默认不应处于变异模式")
	}
	if code, _, se := runCLIStrings(t, filepath.Join(root, "ui", "home.gxml")); code != ExitOK {
		t.Fatalf("健康对照应通过，got rc=%d stderr=%s", code, se)
	}
}
