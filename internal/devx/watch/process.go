package watch

// ProcessManager 的平台无关部分。Job Object 实现经 proc.go 委托给
// internal/devx/process；其它平台明确返回 unsupported（可编译不代表可运行）。
//
// 交接顺序（任务 A2）：stop-old → start-new（构建已完成，构建期间旧继续服务）。
// 停止链有界（F-A4-R1）：整个 stop（tree command + job 终止 + 等待）由一个 StopTimeout deadline 覆盖，
// taskkill/tree 命令亦受该 deadline 约束（不无限卡）。ActiveAfter 仅统计仍存活的**根**，不作为“整棵树
// 已收口”的证明；树收口以 job terminate+wait 的真实返回值/错误为据。

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Child 是一个受 Watch 管理的子进程代次。
type Child struct {
	Gen        int
	PID        int
	cmd        *exec.Cmd
	started    time.Time
	done       chan struct{}
	waitErr    error
	stdinWrite *os.File

	// 停止状态同步保护：job 句柄的唯一消费点。stopMu 保护 job/stopDone/proofErr。
	stopMu   sync.Mutex
	job      jobHandle // 平台句柄（Windows Job；其它平台 0）。消费后清零。
	stopDone bool      // 该 Child 的停止尝试是否已消费其 Job 句柄（只消费一次）
	proofErr error     // 记录的该代真实退出证明结果（nil=证明成功）
}

// Running 报告子进程是否仍在运行（未退出）。
func (c *Child) Running() bool {
	if c == nil {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// ProcessManager 实现 Swapper：stop-old → start-new 串行交接；停止链整体有界。
type ProcessManager struct {
	BinaryForGen   func(gen int) string
	Args           []string
	WorkDir        string
	Stdout, Stderr io.Writer
	StopTimeout    time.Duration
	Spawn          func(gen int, bin string, args []string) (*exec.Cmd, error)
	// EnvFor 可选：为新代 gen 追加环境变量（R27-A hydration 恢复注入状态文件/会话）。nil=不追加。
	EnvFor func(gen int) []string

	mu      sync.Mutex
	current *Child
	history []*Child
	logf    func(format string, a ...any)
}

// NewProcessManager 构造。
func NewProcessManager(binaryForGen func(gen int) string) *ProcessManager {
	return &ProcessManager{BinaryForGen: binaryForGen, StopTimeout: 3 * time.Second, logf: func(string, ...any) {}}
}

// SetLogger 注入日志函数（可选）。
func (m *ProcessManager) SetLogger(f func(format string, a ...any)) { m.logf = f }

func (m *ProcessManager) timeout() time.Duration {
	if m.StopTimeout > 0 {
		return m.StopTimeout
	}
	return 3 * time.Second
}

// Current 返回当前在服务的子进程（可能 nil）。
func (m *ProcessManager) Current() *Child {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// History 返回已启动过的子进程（含已退出）。
func (m *ProcessManager) History() []*Child {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Child, len(m.history))
	copy(out, m.history)
	return out
}

// start 启动一个子进程并纳入平台树所有权。
func (m *ProcessManager) start(gen int, bin string) (*Child, error) {
	var cmd *exec.Cmd
	var err error
	var stdinR, stdinW *os.File
	if m.Spawn != nil {
		cmd, err = m.Spawn(gen, bin, m.Args)
	} else {
		cmd = exec.Command(bin, m.Args...)
		cmd.Dir = m.WorkDir
		if m.EnvFor != nil {
			cmd.Env = append(os.Environ(), m.EnvFor(gen)...)
		}
		cmd.Stdout = m.Stdout
		if cmd.Stdout == nil {
			cmd.Stdout = os.Stderr
		}
		cmd.Stderr = m.Stderr
		if cmd.Stderr == nil {
			cmd.Stderr = os.Stderr
		}
		if r, w, perr := os.Pipe(); perr == nil {
			stdinR, stdinW = r, w
			cmd.Stdin = r
		}
	}
	if err != nil {
		return nil, err
	}
	job, jerr := startOwnedCommand(cmd)
	if jerr != nil {
		if stdinR != nil {
			_ = stdinR.Close()
		}
		if stdinW != nil {
			_ = stdinW.Close()
		}
		return nil, fmt.Errorf("创建进程树所有权失败: %w", jerr)
	}
	if stdinR != nil {
		_ = stdinR.Close()
	}
	c := &Child{Gen: gen, PID: cmd.Process.Pid, cmd: cmd, started: time.Now(), done: make(chan struct{}), stdinWrite: stdinW, job: job}
	go func() {
		c.waitErr = cmd.Wait()
		close(c.done)
	}()
	m.logf("watch: started gen=%d pid=%d bin=%s", gen, c.PID, bin)
	return c, nil
}

// stop 有界停止子进程及其整棵树。**整个停止链**受一个 StopTimeout deadline 约束：
// tree 命令（taskkill）、job 终止、等待根退出都以此 deadline 为界（F-A4-R1）。
func (m *ProcessManager) stop(c *Child) (stopped bool, waitErr error) {
	if c == nil {
		return true, nil
	}
	// 唯一消费点：在 stopMu 下判定并取走 Job 句柄；消费后清零，再次停止不操作旧句柄。
	c.stopMu.Lock()
	if c.stopDone {
		prev := c.proofErr
		werr := c.waitErr
		c.stopMu.Unlock()
		if prev != nil {
			// 既有失败不得被“空句柄哨兵”转成成功（fail-closed）。
			return false, prev
		}
		return true, werr
	}
	job := c.job
	c.job = 0
	c.stopDone = true
	c.stopMu.Unlock()

	deadline := time.Now().Add(m.timeout())
	record := func(err error) (bool, error) {
		c.stopMu.Lock()
		c.proofErr = err
		c.stopMu.Unlock()
		if err != nil {
			return false, err
		}
		return true, c.waitErr
	}
	if job == 0 {
		// 无可证明的自有 Job 句柄（不应发生：正常 start 必带 Job）：fail-closed，不谎报成功。
		return record(fmt.Errorf("gen=%d 无自有 Job 句柄可证明退出", c.Gen))
	}
	// 关闭 stdin（根若在读则自然退出；不影响 Job 证明）。
	if c.stdinWrite != nil {
		_ = c.stdinWrite.Close()
		c.stdinWrite = nil
	}
	// Manager 的 Windows 自有 Job **统一用已持有的 Job 句柄收口**（TerminateAndWaitJob）：无 PID 型外部命令。
	if jerr := terminateAndWaitJob(job, deadline); jerr != nil {
		return record(fmt.Errorf("gen=%d job 终止/证明失败: %w", c.Gen, jerr))
	}
	// 记录真实证明结果（TerminateAndWaitJob 成功即 ActiveProcesses==0 且已捕获句柄 signaled）。
	// 额外等待根 cmd.Wait 归口，bounded 于同一 deadline（不延长 StopTimeout）。
	select {
	case <-c.done:
		return record(nil)
	case <-time.After(time.Until(deadline)):
		return record(fmt.Errorf("old child gen=%d pid=%d 未在 StopTimeout 内收口", c.Gen, c.PID))
	}
}

// Swap 实现 Swapper：先停旧代（有界），成功后再启新代（任务 A2）。
func (m *ProcessManager) Swap(ctx context.Context, gen int) error {
	bin := ""
	if m.BinaryForGen != nil {
		bin = m.BinaryForGen(gen)
	}
	if bin == "" {
		return fmt.Errorf("gen=%d 无二进制路径", gen)
	}
	m.mu.Lock()
	old := m.current
	m.mu.Unlock()

	if old != nil {
		stopped, werr := m.stop(old)
		if !stopped {
			return fmt.Errorf("旧代 gen=%d 未收口，交接中止: %v", old.Gen, werr)
		}
		m.mu.Lock()
		if m.current == old {
			m.current = nil
		}
		m.mu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	newc, err := m.start(gen, bin)
	if err != nil {
		return fmt.Errorf("停止旧代后启动新代 gen=%d 失败（当前无进程服务）: %w", gen, err)
	}
	m.mu.Lock()
	m.current = newc
	m.history = append(m.history, newc)
	m.mu.Unlock()

	select {
	case <-newc.done:
		return fmt.Errorf("新代 gen=%d pid=%d 启动后立即退出: %v", gen, newc.PID, newc.waitErr)
	case <-time.After(50 * time.Millisecond):
	}
	return ctx.Err()
}

// Shutdown 有界停止当前子进程及其整棵树。
func (m *ProcessManager) Shutdown() error {
	m.mu.Lock()
	c := m.current
	m.mu.Unlock()
	if c == nil {
		return nil
	}
	stopped, werr := m.stop(c)
	if !stopped {
		return fmt.Errorf("shutdown: child gen=%d 未收口: %v", c.Gen, werr)
	}
	m.mu.Lock()
	m.current = nil
	m.mu.Unlock()
	return nil
}

// ActiveAfter 返回仍存活的**根**数量（不作为整树收口证明；树收口以 terminateAndWaitJob 结果为准）。
func (m *ProcessManager) ActiveAfter() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.history {
		if c.Running() {
			n++
		}
	}
	return n
}
