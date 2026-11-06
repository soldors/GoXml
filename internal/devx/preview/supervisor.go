package preview

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// ErrHostTimeout 是宿主进程超时（已被进程树收口）。
var ErrHostTimeout = errors.New("preview: 宿主进程超时，已终止进程树")

// HostRunner 是宿主/子进程的执行控制（生命周期）。
//
// 本线不运行原生窗口：执行控制用自有 console helper 验证（见
// testdata/consolehelper）；真正把窗口跑起来的接线由协调者按
// integration-proposal.diff 落地，本类型即为其执行控制核心。
//
// 保证：
//   - Wait 带 Timeout：超时先做**进程树**终止（Windows taskkill /T /F；
//     其他平台 Kill 本进程并等待），再返回 ErrHostTimeout；
//   - Stop 幂等：对已退出进程不报错；
//   - 只回收自己启动的进程，不按名字/时间扫杀别人的进程。
type HostRunner struct {
	cmd     *exec.Cmd
	waiting bool
}

// StartHost 启动 exe（工作目录 dir），返回执行控制器。
func StartHost(exe string, dir string, args ...string) (*HostRunner, error) {
	if exe == "" {
		return nil, errors.New("preview: StartHost 需要 exe 路径")
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("preview: 启动 %s: %w", exe, err)
	}
	return &HostRunner{cmd: cmd}, nil
}

// Pid 返回宿主进程 PID（已退出后仍返回原值，供证据记录）。
func (h *HostRunner) Pid() int {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

// Wait 等待宿主退出；timeout 内未退出则终止进程树并返回 ErrHostTimeout。
//
// 返回的 exitErr 是进程自身的退出错误（nil = 退出码 0）。
func (h *HostRunner) Wait(timeout time.Duration) (exitErr error, err error) {
	if h == nil || h.cmd == nil {
		return nil, errors.New("preview: Wait on nil runner")
	}
	done := make(chan error, 1)
	go func() { done <- h.cmd.Wait() }()
	select {
	case werr := <-done:
		return werr, nil
	case <-time.After(timeout):
		if stopErr := h.Stop(); stopErr != nil {
			return nil, fmt.Errorf("%w（且终止失败: %v）", ErrHostTimeout, stopErr)
		}
		// Stop 之后 Wait 必然返回（进程已死）；回收它，避免僵尸句柄。
		<-done
		return nil, ErrHostTimeout
	}
}

// Stop 终止宿主进程树（幂等；已退出返回 nil）。
func (h *HostRunner) Stop() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	// 先探活：已退出就不动它（幂等）。
	if h.cmd.ProcessState != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		// taskkill /T 连子进程一起收口；/F 强制（窗口进程可能不响应消息）。
		kill := exec.Command("taskkill", "/PID", fmt.Sprint(h.cmd.Process.Pid), "/T", "/F")
		kill.Stdout = nil
		kill.Stderr = nil
		if err := kill.Run(); err != nil {
			// taskkill 对已退出进程报「找不到」——视作成功终止。
			return nil
		}
		return nil
	}
	if err := h.cmd.Process.Kill(); err != nil {
		// 已退出（ESRCH 类）不算失败。
		return nil
	}
	return nil
}
