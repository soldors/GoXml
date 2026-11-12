package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrHostTimeout 是宿主进程等待超时（已请求终止进程树；终止是否成功
	// 由返回错误链单独说明，ErrHostTimeout 本身不代表「成功终止」）。
	ErrHostTimeout = errors.New("preview: 宿主进程等待超时")
	// ErrHostCanceled 是宿主等待被显式取消（同样已请求终止）。
	ErrHostCanceled = errors.New("preview: 宿主等待被取消")
)

// 有界回收的时限。所有等待都有上限：终止命令失败不允许 Wait 无限挂住。
//
// killCommandTimeout 是**外部终止命令**（绝对路径 taskkill /PID /T /F）的
// 执行预算。R28 Preview 共享门禁诊断（preview-gate-delivery）在本机用自建
// console helper 实测：该命令需 685–711ms；原 500ms 会在命令返回前触发
// CommandContext 的 deadline，把 taskkill 自身杀掉，表现为 exit status 1
// 且 CombinedOutput 为空，于是把真实可用的终止命令误报为失败，自有进程树
// 也未被及时收口（leaf 仍写出 marker）。
//
// 5s 仍是**有界**预算，不是等待/凑绿：终止工具不可用时启动失败立即上报，
// 绝不吞错；超时后仍保留精确自有 PID 的兜底 Kill 与 reapBound /
// finalReapBound 有界回收。不得按错误文本放宽「终止命令不可用」守卫。
const (
	killCommandTimeout = 5 * time.Second
	reapBound          = 250 * time.Millisecond
	finalReapBound     = 250 * time.Millisecond
)

// killProcessTree 终止 pid 的**自有进程树**（Windows taskkill /T /F）。
//
// 可执行文件按 SystemRoot/WINDIR/SystemDrive 的绝对路径解析，不依赖
// 可被测试或用户改动的 PATH；解析不到时才回退 PATH 查找——那样启动
// 失败会被当作真实错误上报，绝不吞掉。命令本身也带时限。
var killProcessTree = func(pid int) error {
	ctx, cancel := context.WithTimeout(context.Background(), killCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, taskkillExe(), "/PID", strconv.Itoa(pid), "/T", "/F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("preview: taskkill /PID %d /T /F 失败: %w (%s)",
			pid, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// taskkillExe 返回可靠的 taskkill 绝对路径；所有候选都不可用时回退
// 到 PATH 查找（失败由调用方作为错误上报）。
func taskkillExe() string {
	candidates := []string{
		os.Getenv("SystemRoot"),
		os.Getenv("WINDIR"),
	}
	if drive := os.Getenv("SystemDrive"); drive != "" {
		candidates = append(candidates, filepath.Join(drive, "Windows"))
	}
	for _, root := range candidates {
		if root == "" {
			continue
		}
		exe := filepath.Join(root, "System32", "taskkill.exe")
		if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
			return exe
		}
	}
	return "taskkill"
}

// HostRunner 是宿主/子进程的执行控制（生命周期）。
//
// 本线不运行原生窗口：执行控制用自有 console helper 验证（见
// testdata/consolehelper）；真正把窗口跑起来的接线由协调者按
// integration-proposal.diff 落地，本类型即为其执行控制核心。
//
// 语义（R26-C R1 修复 F-C3）：
//   - StartHost 只启动一个 Wait goroutine，done 在其返回后关闭；
//     Wait/WaitContext/Stop 可重复调用，不再各起一个 cmd.Wait；
//   - 超时/取消先做进程树终止（Windows taskkill /T /F，绝对路径），
//     非 Windows 用精确自有 PID 的 Process.Kill；
//   - **终止命令失败必须被观测**：Stop 返回错误并保留在 StopError()，
//     Wait 把它并入错误链，不把失败压成成功；
//   - 所有回收等待都有上限，并对精确自有 PID 兜底 Kill；从不按名字/
//     时间扫杀别人的进程。
type HostRunner struct {
	cmd  *exec.Cmd
	done chan struct{} // cmd.Wait 返回后关闭（单一 waiter）

	mu       sync.Mutex
	waitErr  error // cmd.Wait 观察到的进程退出错误（nil = 退出码 0）
	stopErr  error // 最近一次真实终止失败（可观测，进程退出后仍保留）
	stopOnce sync.Once
	stopRes  error
}

// StartHost 启动 exe（工作目录 dir），返回执行控制器。
func StartHost(exe string, dir string, args ...string) (*HostRunner, error) {
	return StartHostWithOutput(exe, dir, nil, nil, args...)
}

// StartHostWithOutput preserves host diagnostics for a public CLI caller.
// The original StartHost behavior remains available for existing callers.
func StartHostWithOutput(exe, dir string, stdout, stderr io.Writer, args ...string) (*HostRunner, error) {
	if exe == "" {
		return nil, errors.New("preview: StartHost 需要 exe 路径")
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("preview: 启动 %s: %w", exe, err)
	}
	h := &HostRunner{cmd: cmd, done: make(chan struct{})}
	// 唯一的 Wait：结果存入 waitErr 后关闭 done，供 Wait/Stop 共享。
	go func() {
		werr := cmd.Wait()
		h.mu.Lock()
		h.waitErr = werr
		h.mu.Unlock()
		close(h.done)
	}()
	return h, nil
}

// Pid 返回宿主进程 PID（已退出后仍返回原值，供证据记录）。
func (h *HostRunner) Pid() int {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// ExitError 返回进程自身的退出错误（nil = 退出码 0）。
// 超时/取消路径的 Wait 首返回值保持 nil（既有契约），需要进程退出
// 错误时读这里。
func (h *HostRunner) ExitError() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.waitErr
}

// StopError 返回最近一次真实终止失败；即使进程随后已退出也保留，
// 用于「taskkill 失败需被观测」的证据路径。
func (h *HostRunner) StopError() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopErr
}

// Wait 等待宿主退出；timeout 内未退出则终止进程树并返回 ErrHostTimeout。
//
// 返回契约（不因故障路径改变）：
//   - 正常退出：(exitErr, nil)，exitErr 为进程退出错误（nil = 退出码 0）；
//   - 超时：(nil, 错误)，errors.Is(err, ErrHostTimeout) 为真；
//     若终止命令失败，错误链同时包含该失败（不视为成功）。
//
// 重复调用：进程已退出时返回该进程的退出结果，不重放超时语义。
func (h *HostRunner) Wait(timeout time.Duration) (exitErr error, err error) {
	if h == nil || h.cmd == nil {
		return nil, errors.New("preview: Wait on nil runner")
	}
	if timeout <= 0 {
		return nil, errors.New("preview: Wait 需要正的超时（不隐式启动或停止进程）")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return h.WaitContext(ctx)
}

// WaitContext 与 Wait 同语义，但由调用方 ctx 决定取消/超时。
// ctx 为 nil 时返回错误，不启动任何新等待。
func (h *HostRunner) WaitContext(ctx context.Context) (exitErr error, err error) {
	if h == nil || h.cmd == nil {
		return nil, errors.New("preview: WaitContext on nil runner")
	}
	if ctx == nil {
		return nil, errors.New("preview: WaitContext 需要非空 ctx")
	}
	var cause error
	select {
	case <-h.done:
		return h.ExitError(), nil
	case <-ctx.Done():
		cause = ctx.Err()
	}

	// 超时/取消：先请求终止（错误必须被观测），再做有界回收。
	stopErr := h.Stop()
	if !h.waitBriefly(finalReapBound) {
		// 仍未回收：对**精确自有 PID**兜底 Kill，再等一个有界窗口；
		// 即使仍不成功也返回错误，绝不无限等 <-done。
		if kerr := h.cmd.Process.Kill(); kerr != nil && !h.waitBriefly(finalReapBound) {
			stopErr = errors.Join(stopErr,
				fmt.Errorf("preview: 兜底终止/有界回收失败: %w", kerr))
		}
	}
	base := ErrHostTimeout
	if !errors.Is(cause, context.DeadlineExceeded) {
		base = ErrHostCanceled
	}
	if stopErr != nil {
		// 两个 %w：既保留超时/取消类别，也保留终止失败原因。
		return nil, fmt.Errorf("%w；终止命令失败（已观测，不视为成功）: %w", base, stopErr)
	}
	return nil, base
}

// Stop 终止宿主进程树（幂等；已退出返回 nil；终止命令失败返回并保留错误）。
//
// 重复调用返回第一次的结果；不会把「taskkill 找不到/启动失败」当作
// 成功终止。非 Windows 平台没有 taskkill，用精确自有 PID 的 Kill。
func (h *HostRunner) Stop() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	h.stopOnce.Do(func() { h.stopRes = h.stopProcess() })
	return h.stopRes
}

func (h *HostRunner) stopProcess() error {
	// 已退出：幂等成功（不动别人的进程）。
	select {
	case <-h.done:
		return nil
	default:
	}

	var treeErr error
	if runtime.GOOS == "windows" {
		treeErr = killProcessTree(h.cmd.Process.Pid)
	} else if err := h.cmd.Process.Kill(); err != nil {
		treeErr = fmt.Errorf("preview: 终止自有 PID 失败: %w", err)
	}

	// 有界观察：树终止成功时 done 应很快关闭；否则兜底 Kill 精确自有 PID。
	if !h.waitBriefly(reapBound) {
		if kerr := h.cmd.Process.Kill(); kerr != nil && !h.waitBriefly(finalReapBound) {
			treeErr = errors.Join(treeErr,
				fmt.Errorf("preview: 兜底 Kill 失败或有界回收超时: %w", kerr))
		}
	}
	if treeErr != nil {
		h.mu.Lock()
		h.stopErr = treeErr
		h.mu.Unlock()
		return treeErr
	}
	return nil
}

// waitBriefly 在 d 内等待 cmd.Wait 回收；返回是否已回收。
func (h *HostRunner) waitBriefly(d time.Duration) bool {
	select {
	case <-h.done:
		return true
	case <-time.After(d):
		return false
	}
}
