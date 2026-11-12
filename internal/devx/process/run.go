package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// ExecResult separates the observed command exit from cancellation/cleanup errors.
type ExecResult struct {
	RC        int
	Output    string
	Stdout    string
	Stderr    string
	Cancelled bool
	Err       error
}

const commandReapGrace = 3 * time.Second

// commandOutput remains safe even when an OS cleanup failure prevents joining a copier.
type commandOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *commandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *commandOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }

// RunCommandTree runs an owned tool tree. timeout limits execution (0 inherits
// ctx). Cancellation has a separate bounded cleanup grace; cleanup errors remain
// visible and never imply that all descendants exited.
func RunCommandTree(ctx context.Context, exe string, args []string, dir string, extraEnv []string, timeout time.Duration) ExecResult {
	if ctx == nil {
		return ExecResult{RC: -1, Err: errors.New("nil command context")}
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return ExecResult{RC: -1, Cancelled: true, Err: err}
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	var buf, stdout, stderr commandOutput
	cmd.Stdout = io.MultiWriter(&buf, &stdout)
	cmd.Stderr = io.MultiWriter(&buf, &stderr)
	capture := func(result ExecResult) ExecResult {
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		return result
	}

	// A descendant retaining an inherited pipe must not block root-exit detection.
	cmd.WaitDelay = 250 * time.Millisecond
	job, err := StartOwnedCommand(cmd)
	if err != nil {
		return capture(ExecResult{RC: -1, Err: err})
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case werr := <-waitCh:
		jerr := TerminateAndWaitJob(job, time.Now().Add(commandReapGrace))
		var infra error
		if werr != nil {
			var ee *exec.ExitError
			if !errors.As(werr, &ee) {
				infra = werr
			}
		}
		return capture(ExecResult{RC: exitCodeOf(cmd, werr), Output: buf.String(), Err: errors.Join(infra, jerr)})
	case <-ctx.Done():
		cleanupCtx, cancel := context.WithTimeout(context.Background(), commandReapGrace)
		defer cancel()
		// Use a live cleanup context, not the already-cancelled execution context.
		deadline, _ := cleanupCtx.Deadline()
		// **用已持有的 Job 句柄**终止整棵自有工具树：TerminateAndWaitJob 以 Job 句柄同步杀所有 Job 成员
		//（含后代）并轮询 ActiveProcesses 归零，不等外部 taskkill 的等待期——后代在取消当刻即停，不再写盘
		//（lateWrite）；有界于同一 cleanup deadline；错误可观测（不吞）。这是本轮窄修复：取消路径优先用
		//**已拥有的 Windows Job/进程句柄**退出，而非外部终止工具。Manager 路径的 StopJobTree（含 taskkill）
		// 保持不变。
		jerr := TerminateAndWaitJob(job, deadline)
		_, joined := joinCommand(cleanupCtx, waitCh)
		if !joined {
			kerr := cmd.Process.Kill()
			finalCtx, finalCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			_, joined = joinCommand(finalCtx, waitCh)
			finalCancel()
			if !joined {
				jerr = errors.Join(jerr, kerr, fmt.Errorf("command root not reaped within cleanup grace"))
			}
		}
		return capture(ExecResult{RC: -1, Output: buf.String(), Cancelled: true, Err: errors.Join(ctx.Err(), jerr)})
	}
}

func joinCommand(ctx context.Context, ch <-chan error) (error, bool) {
	select {
	case err := <-ch:
		return err, true
	case <-ctx.Done():
		return ctx.Err(), false
	}
}
func exitCodeOf(cmd *exec.Cmd, werr error) int {
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if werr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) {
		return ee.ExitCode()
	}
	return -1
}
