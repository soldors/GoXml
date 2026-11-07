// Package watchadapter connects the Watch builder to a verified goxml tool.
// NewRealBuilder captures an explicit executable per instance for production.
// SetToolExe configures only the standalone CLIGenerate default. Without that
// default, CLIGenerate offers a source-development go-run fallback; this is not
// the installed CLI route. Tool processes are owned by watch.RunCommandTree.
package watchadapter

import (
	"context"
	"fmt"
	"github.com/goxml/goxml/internal/devx/watch"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

var (
	toolMu   sync.Mutex
	toolExe  string // 缓存的可执行工具路径
	toolRoot string // 推导出的仓库根（诊断）
)

// SetToolExe 显式设置被验工具可执行文件路径（协调者接线时用；优先于自动解析）。
func SetToolExe(path string) {
	toolMu.Lock()
	defer toolMu.Unlock()
	toolExe = path
}

// ToolIdentity returns the standalone CLIGenerate default, not a builder override.
func ToolIdentity() (exe, root string) {
	toolMu.Lock()
	defer toolMu.Unlock()
	return toolExe, toolRoot
}

// repoRootViaCaller 由本文件源码位置推导仓库根（含 cmd/goxml/main.go 与 go.mod），与 cwd 无关。
func repoRootViaCaller() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	dir := filepath.Dir(file)
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

// resolveToolExe is a private development build helper. It is not called by
// the production builder route, and never reuses an unverified cross-process file.
func resolveToolExe() (string, string, error) {
	toolMu.Lock()
	defer toolMu.Unlock()
	if toolExe != "" {
		return toolExe, toolRoot, nil
	}
	root, ok := repoRootViaCaller()
	if !ok {
		return "", "", os.ErrNotExist
	}
	exeName := "goxml-tool"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	file, createErr := os.CreateTemp("", "goxml-watch-*-"+exeName)
	if createErr != nil {
		return "", root, createErr
	}
	bin := file.Name()
	file.Close()
	result := watch.RunCommandTree(context.Background(), "go", []string{"build", "-o", bin, "./cmd/goxml"}, root, nil, 2*time.Minute)
	if result.Err != nil || result.RC != 0 {
		_ = os.Remove(bin)
		err := result.Err
		if err == nil {
			err = fmt.Errorf("tool build exit %d", result.RC)
		}
		return "", root, &buildError{msg: result.Output, err: err}
	}

	toolExe, toolRoot = bin, root
	return toolExe, root, nil
}

type buildError struct {
	msg string
	err error
}

func (e *buildError) Error() string {
	return "构建被验工具失败: " + e.msg + ": " + e.err.Error()
}

// CLIGenerate 执行默认工具身份。显式 SetToolExe 优先；未设置时只提供
// 源码开发态 fallback。发布命令应把自身 exe 传给 NewRealBuilder，不依赖源码。
func CLIGenerate(ctx context.Context, projectDir string) (int, string) {
	absolute, err := filepath.Abs(projectDir)
	if err != nil {
		return -1, err.Error()
	}
	projectDir = absolute
	exe, _ := ToolIdentity()
	if exe != "" {
		return generateWithTool(ctx, exe, projectDir)
	}
	root, ok := repoRootViaCaller()
	if !ok {
		return -1, "watchadapter: no tool executable supplied (source fallback unavailable)"
	}
	return generationResult(watch.RunCommandTree(ctx, "go", []string{"run", "./cmd/goxml", "generate", projectDir, "--check"}, root, nil, 0))
}

func generateWithTool(ctx context.Context, exe, projectDir string) (int, string) {
	absolute, err := filepath.Abs(projectDir)
	if err != nil {
		return -1, err.Error()
	}
	projectDir = absolute
	return generationResult(watch.RunCommandTree(ctx, exe, []string{"generate", projectDir, "--check"}, projectDir, nil, 0))
}

// 保留执行失败与取消后的清理原因；不能把清理失败压成一个泛化的取消字符串。
func generationResult(res watch.ExecResult) (int, string) {
	if res.Cancelled {
		detail := "canceled/timeout"
		if res.Err != nil {
			detail += ": " + res.Err.Error()
		}
		return -1, detail + "\n" + tail(res.Output)
	}
	if res.Err != nil {
		return -1, "watchadapter: " + res.Err.Error() + "\n" + tail(res.Output)
	}
	return res.RC, tail(res.Output)
}

// toolIdentityOrErr 返回可执行路径（空表示失败）。
func toolIdentityOrErr() (string, error) {
	exe, _, err := resolveToolExe()
	if err != nil {
		return "", err
	}
	return exe, nil
}

func tail(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}

// NewRealBuilder 构造已接线（CLIGenerate）的 RealBuilder。entryPkg 相对 ProjectDir。
// toolExe 非空时显式指定被验工具身份（推荐协调者接线时传入）；空则自动解析（runtime.Caller 推导）。
func NewRealBuilder(projectDir, entryPkg, outBinary, toolExe string) *watch.RealBuilder {
	if absolute, err := filepath.Abs(projectDir); err == nil {
		projectDir = absolute
	}
	generate := CLIGenerate
	if toolExe != "" {
		// Capture this builder's tool, independent of later builders/default changes.
		generate = func(ctx context.Context, project string) (int, string) {
			return generateWithTool(ctx, toolExe, project)
		}
	}
	return &watch.RealBuilder{ProjectDir: projectDir, EntryPkg: entryPkg, OutBinary: outBinary, Generate: generate}
}
