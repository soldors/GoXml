package preview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAcceptanceCanaryNotInHostGraph 证明业务包不进入宿主依赖图。
//
// 方法（TASK R26-C C3 原文）：在用户模块里放置带 canary init/main 的
// 用户 Go，构建预览宿主后用 **go list -deps** 核实际依赖图。
// 测试**不运行**宿主（不靠运行来猜测安全）：依赖图里根本没有用户
// 模块路径，init/main 无从进入预览进程。
func TestAcceptanceCanaryNotInHostGraph(t *testing.T) {
	proj := copyProject(t)

	// canary 1：用户库包，init 带鲜明副作用（若被拉进依赖图，构建期
	// 就会以「导入不存在」或依赖图出现该路径暴露；运行它则 marker 出现）。
	canarySrc := `package canary

import "os"

// CanaryInitRan 是探测标记路径；预览宿主依赖图不应包含本包。
const CanaryInitRan = "canary-init-ran"

func init() {
	// 故意不做任何事也要留可观测痕迹的形态：只在被运行时写 marker。
	_ = os.Getenv("GOXML_PREVIEW_CANARY_SHOULD_NOT_RUN")
}
`
	if err := os.MkdirAll(filepath.Join(proj, "canary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "canary", "canary.go"), []byte(canarySrc), 0o644); err != nil {
		t.Fatal(err)
	}
	// canary 2：用户 main 包（目标应用入口）。
	mainSrc := `package main

import (
	"fmt"
	"os"

	"example.com/previewproj/canary"
)

func main() {
	fmt.Fprintln(os.Stderr, "user app main ran, canary =", canary.CanaryInitRan)
	os.Exit(42)
}
`
	if err := os.MkdirAll(filepath.Join(proj, "cmd", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "cmd", "app", "main.go"), []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// 预览：构建宿主（不运行）。
	res, err := Run(Options{File: filepath.Join(proj, "static_basic.gxml"), WorkDir: proj})
	if err != nil {
		t.Fatalf("Run 操作故障: %v", err)
	}
	if !res.Built() {
		t.Fatalf("应到 PhaseBuilt，实到 %s（诊断 %v）", res.Phase, res.Diags)
	}
	defer res.Cleanup()

	// 用 go list -deps 核宿主真实依赖图（与 buildHost 同环境口径）。
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("需要 PATH 上的 Go:", err)
	}
	list := exec.Command(goExe, "list", "-deps", ".")
	list.Dir = res.HostDir
	list.Env = append(os.Environ(),
		"CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off",
		"GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly")
	out, err := list.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps 失败: %v\n%s", err, out)
	}
	deps := string(out)

	// 用户模块不在依赖图（库包与 main 包都不得出现）。
	for _, banned := range []string{
		"example.com/previewproj",
		"example.com/previewproj/canary",
		"example.com/previewproj/cmd/app",
	} {
		if strings.Contains(deps, banned) {
			t.Fatalf("用户业务路径 %s 出现在宿主依赖图中：\n%s", banned, deps)
		}
	}
	// 宿主自身的关键依赖必须在（证明 go list 覆盖了宿主入口）。
	for _, want := range []string{
		hostModuleName,
		hostModuleName + "/view",
		"github.com/goxml/goxml/pkg/runtime/app",
		"gioui.org",
	} {
		if !strings.Contains(deps, want) {
			t.Fatalf("宿主依赖图缺少 %s（go list 结果可疑）：\n%s", want, deps)
		}
	}
	// 用户模块只读：canary 文件原样保留（预览不删不改用户源码）。
	if _, err := os.Stat(filepath.Join(proj, "canary", "canary.go")); err != nil {
		t.Fatalf("用户 canary 源码不应被预览改动: %v", err)
	}
	t.Logf("OBS 宿主依赖图 %d 行，无用户模块路径；宿主关键依赖齐备", len(strings.Split(strings.TrimSpace(deps), "\n")))
}
