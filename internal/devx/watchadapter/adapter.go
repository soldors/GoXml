// Package watchadapter 是 internal/devx/watch 与 internal/cli 之间的独立适配层（无 cli↔watch 环）。
//
// R26-A repair-r3：
//   - **F-A9 工具身份**：CLIGenerate 不再依赖调用者 cwd 里存在 goxml 源码、也不默认每代 `go run`
//     工具源。改为**解析被验工具可执行文件身份**一次（缓存复用），再从任意用户 cwd 运行它。
//     工具身份解析顺序：显式注入的 ToolExe（SetToolExe / NewRealBuilder 的 tool 参数）> 包缓存（首次
//     由 `runtime.Caller` 定位仓库并 `go build ./cmd/goxml` 一次）> 失败即如实报错。**不硬编码作者
//     工作树/主库路径**：仓库根由 runtime.Caller 从本文件位置推导，不依赖 cwd。
//   - **F-A3-R2 取消收整棵树**：用 watch.RunCommandTree（自有 Job/进程树所有权）运行工具，取消/超时
//     终止**整棵**生成树并有界等待收口后返回；不再用 CommandContext/WaitDelay 声称无迟到写盘。
package watchadapter

import (
	"context"
	"github.com/goxml/goxml/internal/devx/watch"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
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

// ToolIdentity 返回当前解析到的工具身份（路径）与仓库根（诊断用）。
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

// resolveToolExe 返回被验工具可执行文件路径（缓存）。首次从源码构建一次（不依赖 cwd）。
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
	bin := filepath.Join(os.TempDir(), "goxml-tool-"+buildStamp()+exeName)
	if _, err := os.Stat(bin); err != nil {
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/goxml")
		cmd.Dir = root
		cmd.Env = os.Environ()
		if out, berr := cmd.CombinedOutput(); berr != nil {
			return "", root, &buildError{msg: string(out), err: berr}
		}
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

// buildStamp 提供一个进程级稳定后缀（同一进程复用缓存二进制；跨进程可重建）。
func buildStamp() string { return "r3" }

// CLIGenerate 以**自有进程树**运行被验工具 `generate <projectDir> --check`，可取消并回收整棵树。
// 工具以 `go run ./cmd/goxml`（cwd=仓库根，runtime.Caller 推导，**不依赖调用者 cwd**）形式运行，
// 保留 toolexec→go build→generator→go run 完整真实生成父链；整棵树由 RunCommandTree 的 Job 拥有，
// 取消即终止全链并有界收口（F-A3-R2）。工具源码根由 runtime.Caller 推导，非作者工作树/主库硬编码。
func CLIGenerate(ctx context.Context, projectDir string) (int, string) {
	root, ok := repoRootViaCaller()
	if !ok {
		return -1, "watchadapter: 无法从源码位置推导被验工具仓库根"
	}
	goBin := "go"
	res := watch.RunCommandTree(ctx, goBin, []string{"run", "./cmd/goxml", "generate", projectDir, "--check"}, root, nil, 0)
	if res.Err != nil && !res.Cancelled {
		// 启动/收口失败（非业务非零退出）如实上报。
		return -1, "watchadapter: " + res.Err.Error() + "\n" + tail(res.Output)
	}
	if res.Cancelled {
		return -1, "canceled/timeout: " + tail(res.Output)
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
	if toolExe != "" {
		SetToolExe(toolExe)
	}
	return &watch.RealBuilder{
		ProjectDir: projectDir,
		EntryPkg:   entryPkg,
		OutBinary:  outBinary,
		Generate:   CLIGenerate,
	}
}
