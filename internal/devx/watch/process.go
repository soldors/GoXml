package watch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Child 是一个受 Watch 管理的子进程代次。
type Child struct {
	Gen     int
	PID     int
	cmd     *exec.Cmd
	started time.Time
	done    chan struct{} // 关闭表示进程已退出
	waitErr error
	// stdinWrite 是为保持子进程存活而持有的 stdin 写端；stop 时关闭使其自然 EOF。
	stdinWrite *os.File
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

// ProcessManager 实现 Swapper：串行交接——先启动新代子进程，成功后再停止旧代并等待其退出。
//
// 语义（A2）：
//   - 新构建成功才启动新代；新代启动失败 => 不停止旧代（旧继续服务），返回错误。
//   - 新代启动成功后，停止旧代（有界等待）；旧代拒绝退出 => 有界后强制结束进程树。
//   - 只管理自己启动的树（记录每个 Child 的 cmd/pid）。
//   - 报告 PID/代次/真实退出码；上层（Watch）不谎报已更新。
type ProcessManager struct {
	// BinaryForGen 返回某代次的二进制路径（由 RealBuilder 产出）。
	BinaryForGen func(gen int) string
	// Args 是子进程启动参数（通常空）。
	Args []string
	// StopTimeout 是等待旧代优雅退出的上限；超时强杀进程树。
	StopTimeout time.Duration
	// Spawn 是可注入的子进程启动器（测试用真实 console helper）；nil 用默认 exec.
	Spawn func(gen int, bin string, args []string) (*exec.Cmd, error)

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

// History 返回已启动过的子进程（含已退出），用于断言代次序列。
func (m *ProcessManager) History() []*Child {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Child, len(m.history))
	copy(out, m.history)
	return out
}

// start 启动一个子进程并登记，返回 Child（含 done 通道，异步 Wait）。
func (m *ProcessManager) start(gen int, bin string) (*Child, error) {
	var cmd *exec.Cmd
	var err error
	var stdinR, stdinW *os.File
	if m.Spawn != nil {
		cmd, err = m.Spawn(gen, bin, m.Args)
	} else {
		cmd = exec.Command(bin, m.Args...)
		cmd.Stdout = os.Stderr // 子输出并入 stderr，避免与 watch 争 stdout（无窗口 console helper）
		cmd.Stderr = os.Stderr
		// 保持子进程 stdin 打开（console helper 阻塞读 stdin），使其存活直到被停止；
		// 关闭写端 => helper 读到 EOF => 自然退出（优雅收口路径）。
		if r, w, perr := os.Pipe(); perr == nil {
			stdinR, stdinW = r, w
			cmd.Stdin = r
		}
	}
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if stdinR != nil {
			_ = stdinR.Close()
		}
		if stdinW != nil {
			_ = stdinW.Close()
		}
		return nil, err
	}
	if stdinR != nil {
		_ = stdinR.Close() // 父进程不再持有读端；子进程持有
	}
	c := &Child{Gen: gen, PID: cmd.Process.Pid, cmd: cmd, started: time.Now(), done: make(chan struct{}), stdinWrite: stdinW}
	go func() {
		c.waitErr = cmd.Wait()
		close(c.done)
	}()
	m.logf("watch: started gen=%d pid=%d bin=%s", gen, c.PID, bin)
	return c, nil
}

// stop 有界停止子进程：先尝试优雅 Kill（进程本身），超时后强杀进程树。
// 返回真实是否已退出（true=已收口）与 wait error（如有）。
func (m *ProcessManager) stop(c *Child) (stopped bool, waitErr error) {
	if c == nil {
		return true, nil
	}
	select {
	case <-c.done:
		return true, c.waitErr
	default:
	}
	// 优雅路径：关闭 stdin 写端，helper 读到 EOF 自然退出（有界等待）。
	if c.stdinWrite != nil {
		_ = c.stdinWrite.Close()
		c.stdinWrite = nil
		grace := time.NewTimer(m.timeout() / 2)
		select {
		case <-c.done:
			grace.Stop()
			m.killTree(c) // 清理可能残留的后代
			return true, c.waitErr
		case <-grace.C:
		}
	}
	if c.cmd != nil && c.cmd.Process != nil {
		// 请求结束（Windows 无 SIGTERM 语义，直接 Kill 进程；随后进程树清理）。
		_ = c.cmd.Process.Kill()
	}
	// 有界等待退出
	timer := time.NewTimer(m.timeout())
	defer timer.Stop()
	select {
	case <-c.done:
		m.killTree(c) // 清理可能残留的后代
		return true, c.waitErr
	case <-timer.C:
		// 旧进程拒绝退出：强杀进程树。
		m.killTree(c)
		select {
		case <-c.done:
			return true, c.waitErr
		case <-time.After(2 * time.Second):
			return false, fmt.Errorf("old child gen=%d pid=%d did not exit after tree kill", c.Gen, c.PID)
		}
	}
}

// killTree 尽力结束该子进程及其后代（仅限本管理器启动的树）。
// Windows 用 taskkill /T /F；其它平台杀进程组或进程本身。失败仅记录，不 panic。
func (m *ProcessManager) killTree(c *Child) {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return
	}
	pid := c.cmd.Process.Pid
	if runtime.GOOS == "windows" {
		// /T 结束整棵进程树，/F 强制；只针对本管理器记录的 PID。
		if err := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", pid)).Run(); err != nil {
			m.logf("watch: taskkill pid=%d err=%v", pid, err)
		}
		return
	}
	_ = c.cmd.Process.Kill()
}

// Swap 实现 Swapper：串行交接。先启动新代；成功后才停止旧代并等待；返回错误时旧代仍在（守护旧服务）。
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

	// 1) 启动新代
	newc, err := m.start(gen, bin)
	if err != nil {
		return fmt.Errorf("启动新代 gen=%d 失败（旧代继续服务）: %w", gen, err)
	}
	// 2) 停止旧代（有界）
	if old != nil {
		stopped, werr := m.stop(old)
		if !stopped {
			// 旧代未收口：为安全起见结束新代并返回错误（不谎报已更新）。
			_, _ = m.stop(newc)
			return fmt.Errorf("旧代 gen=%d 未退出，交接中止: %v", old.Gen, werr)
		}
		m.logf("watch: stopped old gen=%d pid=%d (waitErr=%v)", old.Gen, old.PID, werr)
	}
	m.mu.Lock()
	m.current = newc
	m.history = append(m.history, newc)
	m.mu.Unlock()

	// 3) 确认新代仍在运行（启动后立即崩溃 => 报告失败）
	select {
	case <-newc.done:
		return fmt.Errorf("新代 gen=%d pid=%d 启动后立即退出: %v", gen, newc.PID, newc.waitErr)
	case <-time.After(50 * time.Millisecond):
		// 存活（有界观察，非次数证据）
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

// Shutdown 有界停止当前子进程（Watch 停止时调用）。
func (m *ProcessManager) Shutdown() error {
	m.mu.Lock()
	c := m.current
	m.mu.Unlock()
	stopped, werr := m.stop(c)
	if !stopped {
		return fmt.Errorf("shutdown: child gen=%d 未收口: %v", c.Gen, werr)
	}
	return nil
}

// ActiveAfter 返回仍存活（未退出）的子进程数量——用于断言无幸存子进程。
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
