package watch

// R26-A repair-r3：导出「自有进程树」的可取消子进程执行器，供 adapter 复用（避免 cli 依赖环）。
//
// 语义：
//   - 启动 exe（自有工具身份），纳入平台进程树所有权（Windows Job KILL_ON_JOB_CLOSE）。
//   - ctx 取消/超时 => **终止整棵树**（先在有界 ctx 内 taskkill /T，再 TerminateJobObject + 有界等待
//     ActiveProcesses 归零），返回前有界确认全部自有后代已退出。**不是** 只杀直接子进程。
//   - 正常退出 => rc 为真实退出码；取消 => rc=-1 且 Cancelled=true。
//   - 任何失败（启动/Job 纳入/终止/等待）如实反映，不伪装成功。
// 非 Windows：进程树所有权 unsupported，返回明确错误（不声称已支持）。

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ExecResult 是 RunCommandTree 的结果。
type ExecResult struct {
	RC        int
	Output    string
	Cancelled bool
	Err       error // 启动/收口失败（非普通非零退出）
}

// RunCommandTree 在自有进程树下运行 exe，可取消并回收整棵树。
func RunCommandTree(ctx context.Context, exe string, args []string, dir string, extraEnv []string, timeout time.Duration) ExecResult {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	job, jerr := newJobOwnership()
	if jerr != nil {
		return ExecResult{RC: -1, Err: jerr}
	}
	if err := cmd.Start(); err != nil {
		closeJobOwnership(job)
		return ExecResult{RC: -1, Err: err}
	}
	pid := cmd.Process.Pid
	if aerr := assignJobOwnership(job, pid); aerr != nil {
		closeJobOwnership(job)
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return ExecResult{RC: -1, Err: aerr}
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	select {
	case werr := <-waitCh:
		// 正常结束：仍终止/清理 Job（子孙可能仍在），有界等待，再返回真实 rc。
		if jerr := terminateAndWaitJob(job, time.Now().Add(3*time.Second)); jerr != nil {
			return ExecResult{RC: exitCodeOf(cmd, werr), Output: buf.String(), Err: jerr}
		}
		if werr != nil {
			return ExecResult{RC: exitCodeOf(cmd, werr), Output: buf.String()}
		}
		return ExecResult{RC: 0, Output: buf.String()}
	case <-ctx.Done():
		return cancelTree(ctx, job, pid, cmd, waitCh, &buf, deadline)
	}
}

func cancelTree(ctx context.Context, job jobHandle, pid int, cmd *exec.Cmd, waitCh chan error, buf *strings.Builder, deadline time.Time) ExecResult {
	if deadline.IsZero() {
		deadline = time.Now().Add(5 * time.Second)
	}
	killProcessTree(ctx, pid) // 有界 ctx（调用方 deadline 或默认）
	if jerr := terminateAndWaitJob(job, deadline); jerr != nil {
		return ExecResult{RC: -1, Output: buf.String(), Cancelled: true, Err: jerr}
	}
	// 有界等根退出。
	select {
	case <-waitCh:
	case <-time.After(time.Until(deadline)):
		return ExecResult{RC: -1, Output: buf.String(), Cancelled: true, Err: errors.New("tree reap wait timeout")}
	}
	return ExecResult{RC: -1, Output: buf.String(), Cancelled: true, Err: ctx.Err()}
}

func exitCodeOf(cmd *exec.Cmd, werr error) int {
	if werr == nil {
		return 0
	}
	if ee, ok := werr.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
