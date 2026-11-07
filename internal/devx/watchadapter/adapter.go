// Package watchadapter 是 internal/devx/watch 核心与 internal/cli 之间的**独立适配层**。
//
// 依赖方向（避免 cli↔watch 环，F-A6）：watch 不 import cli；cli 不 import watch；本包 import 两者。
// 主 CLI 分发由协调者保留（可在 cmd/ 同级调度层接线，或在 cli 外组合本 adapter）。
//
// R26-A repair-r2：
//   - **取消（F-A3a-R1）**：CLIGenerate 不再把 cli.Run 放 goroutine 里“join”。cli.Run 无外部取消入口，
//     故改为**可取消工具子进程**：把 `go run ./cmd/goxml generate <dir> --check` 作为自有子进程运行，
//     ctx 取消/超时即终止该子进程（有界），杜绝 join 假取消与迟到写盘。
//   - **每代时限（F-A3b-R1）**：ctx 由 Watch 传入（已含 BuildTimeoutMS 与外部较早 deadline 的较短者）；
//     本处将其作为子进程 ctx，generate+build 全链路共享同一取消。
package watchadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/goxml/goxml/internal/devx/watch"
)

// findRepoRoot 从 start 向上查找含 `cmd/goxml/main.go` 的模块根（goxml 工具源）。
func findRepoRoot(start string) (string, bool) {
	dir := start
	if dir == "" {
		dir, _ = os.Getwd()
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "cmd", "goxml", "main.go")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// CLIGenerate 以**可取消子进程**执行 `goxml generate <projectDir> --check`。
// 返回 observed rc 与 stderr 摘要。ctx 取消/超时 => 立即终止子进程并非阻塞返回（rc=-1）。
//
// 复用工具自身 CLI（`go run ./cmd/goxml …`），不重造生成器；子进程受 ctx 控制（CommandContext +
// WaitDelay 兜底强杀），因此取消有界、返回后无该子进程迟到写盘。
func CLIGenerate(ctx context.Context, projectDir string) (int, string) {
	repo, ok := findRepoRoot("")
	if !ok {
		return -1, "watchadapter: 找不到 goxml 工具源根（cmd/goxml）"
	}
	goBin := "go"
	args := []string{"run", "./cmd/goxml", "generate", projectDir, "--check"}
	cmd := exec.CommandContext(ctx, goBin, args...)
	cmd.Dir = repo
	// WaitDelay：ctx 取消后若子进程仍未退出，再等一小段后强杀，确保有界返回。
	cmd.WaitDelay = 2 * time.Second
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		return -1, "canceled/timeout: " + ctx.Err().Error() + "\n" + tail(out.String())
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), tail(out.String())
		}
		return -1, tail(out.String()) + "\n" + err.Error()
	}
	return 0, tail(out.String())
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	const max = 2000
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}

// NewRealBuilder 构造已接线（CLIGenerate）的 RealBuilder。entryPkg 相对 ProjectDir。
func NewRealBuilder(projectDir, entryPkg, outBinary string) *watch.RealBuilder {
	return &watch.RealBuilder{
		ProjectDir: projectDir,
		EntryPkg:   entryPkg,
		OutBinary:  outBinary,
		Generate:   CLIGenerate,
	}
}
