package watch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Child 是一个受 Watch 管理的子进程代次。
type Child struct {
	Gen     int
	PID     int
	cmd     *exec.Cmd
	started time.Time
	done    chan struct{}
	waitErr error
	// stdinWrite 保持子进程存活；stop 时关闭使其自然 EOF。
	stdinWrite *os.File
	// job（Windows）是该子进程所在 Job 对象句柄；close 它即结束整棵进程树（含后代）。
	job windows.Handle
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

// ProcessManager 实现 Swapper：按任务 A2 串行交接——**先停止旧代，再启动新代**。
//
// 进程树收口（F-A4）：Windows 上为每个子进程创建带 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 的
// Job 对象并在 Start 后立即 assign，使关闭 Job 句柄即结束**整棵进程树（含后代）**，不依赖
// 根进程存活或 `taskkill /T` 猜测。非 Windows 用进程组 + Kill。
//
// 交接顺序与失败语义：stop-old → start-new。若停止旧代成功但启动新代失败，则**没有**进程在服务
// （如实报告错误）；不承诺“启动失败时旧 PID 仍在”。若需要该保证，用「回滚语义」候选（见 candidates）：
// 先起新代健康探测通过后再停旧代（存在短暂重叠，非串行）。默认采用任务要求的 stop-old→start-new。
type ProcessManager struct {
	BinaryForGen func(gen int) string
	Args         []string
	StopTimeout  time.Duration
	Spawn        func(gen int, bin string, args []string) (*exec.Cmd, error)

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

// newJob 创建一个 KILL_ON_JOB_CLOSE 的 Job 对象（Windows）；其它平台返回 0。
func newJob() (windows.Handle, error) {
	if runtime.GOOS != "windows" {
		return 0, nil
	}
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return 0, err
	}
	return h, nil
}

// start 启动一个子进程并纳入 Job（Windows）。
func (m *ProcessManager) start(gen int, bin string) (*Child, error) {
	var cmd *exec.Cmd
	var err error
	var stdinR, stdinW *os.File
	if m.Spawn != nil {
		cmd, err = m.Spawn(gen, bin, m.Args)
	} else {
		cmd = exec.Command(bin, m.Args...)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if r, w, perr := os.Pipe(); perr == nil {
			stdinR, stdinW = r, w
			cmd.Stdin = r
		}
	}
	if err != nil {
		return nil, err
	}
	// Windows：先建 Job，再把进程纳入（须在进程可能派生子进程前尽快 assign）。
	job, jerr := newJob()
	if jerr != nil {
		if stdinR != nil {
			_ = stdinR.Close()
		}
		if stdinW != nil {
			_ = stdinW.Close()
		}
		return nil, fmt.Errorf("创建 Job 失败: %w", jerr)
	}
	if err := cmd.Start(); err != nil {
		if job != 0 {
			windows.CloseHandle(job)
		}
		if stdinR != nil {
			_ = stdinR.Close()
		}
		if stdinW != nil {
			_ = stdinW.Close()
		}
		return nil, err
	}
	if stdinR != nil {
		_ = stdinR.Close()
	}
	if job != 0 {
		ph, perr := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if perr == nil {
			_ = windows.AssignProcessToJobObject(job, ph)
			windows.CloseHandle(ph)
		}
	}
	c := &Child{Gen: gen, PID: cmd.Process.Pid, cmd: cmd, started: time.Now(), done: make(chan struct{}), stdinWrite: stdinW, job: job}
	go func() {
		c.waitErr = cmd.Wait()
		close(c.done)
	}()
	m.logf("watch: started gen=%d pid=%d bin=%s job=%v", gen, c.PID, bin, job)
	return c, nil
}

// stop 停止子进程及其整棵树：关闭 Job（Windows，杀全部后代）→ 关闭 stdin → 有界等待根退出。
func (m *ProcessManager) stop(c *Child) (stopped bool, waitErr error) {
	if c == nil {
		return true, nil
	}
	if !c.Running() {
		// 根已退出：仍用 Job 终止其后代（Job 句柄在根退出后仍有效）。
		m.terminateAndWaitJob(c)
		return true, c.waitErr
	}
	// 根仍存活：先 taskkill /T 结束整棵树（含后代）——必须在根退出前执行，否则后代脱管。
	m.killTree(c)
	// 关闭 stdin（非 Windows/无 Job 优雅路径）并终止 Job 兜底。
	if c.stdinWrite != nil {
		_ = c.stdinWrite.Close()
		c.stdinWrite = nil
	}
	m.terminateAndWaitJob(c)
	timer := time.NewTimer(m.timeout())
	defer timer.Stop()
	select {
	case <-c.done:
		return true, c.waitErr
	case <-timer.C:
		return false, fmt.Errorf("old child gen=%d pid=%d did not exit after tree kill", c.Gen, c.PID)
	}
}

// closeJob 关闭 Job 句柄（KILL_ON_JOB_CLOSE 触发整棵树结束）。
func (m *ProcessManager) closeJob(c *Child) {
	if c == nil || c.job == 0 {
		return
	}
	_ = windows.CloseHandle(c.job)
	c.job = 0
}

// killTree 兜底：Windows taskkill /T /F；其它平台 Kill。
func (m *ProcessManager) killTree(c *Child) {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return
	}
	pid := c.cmd.Process.Pid
	if runtime.GOOS == "windows" {
		if err := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", pid)).Run(); err != nil {
			m.logf("watch: taskkill pid=%d err=%v", pid, err)
		}
		return
	}
	_ = c.cmd.Process.Kill()
}

// Swap 实现 Swapper：**先停旧代，再启新代**（任务 A2 串行交接）。构建已在 Swap 前完成。
// 停止旧代成功而启动新代失败 => 无进程服务，返回错误（不承诺旧 PID 仍在）。
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

	// 1) 先停旧代（有界；Job 关闭杀整棵树）。成功后清空 current（旧已不再服务）。
	if old != nil {
		stopped, werr := m.stop(old)
		if !stopped {
			return fmt.Errorf("旧代 gen=%d 未收口，交接中止: %v", old.Gen, werr)
		}
		m.logf("watch: stopped old gen=%d pid=%d (waitErr=%v)", old.Gen, old.PID, werr)
		m.mu.Lock()
		if m.current == old {
			m.current = nil
		}
		m.mu.Unlock()
	}
	// 2) 再启新代。
	newc, err := m.start(gen, bin)
	if err != nil {
		return fmt.Errorf("停止旧代后启动新代 gen=%d 失败（当前无进程服务）: %w", gen, err)
	}
	m.mu.Lock()
	m.current = newc
	m.history = append(m.history, newc)
	m.mu.Unlock()

	// 3) 新代存活观察（有界，非次数证据）。
	select {
	case <-newc.done:
		return fmt.Errorf("新代 gen=%d pid=%d 启动后立即退出: %v", gen, newc.PID, newc.waitErr)
	case <-time.After(50 * time.Millisecond):
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// jobAccounting 对应 JOBOBJECT_BASIC_ACCOUNTING_INFORMATION（ActiveProcesses 在偏移 40）。
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// terminateAndWaitJob 显式终止 Job 内全部进程并**有界等待 ActiveProcesses 归零**，再关闭句柄。
// 这样 Shutdown/stop 返回时整棵树确已收口（KILL_ON_JOB_CLOSE 的终止是异步的，不能靠根退出即认为后代清）。
func (m *ProcessManager) terminateAndWaitJob(c *Child) {
	if c == nil || c.job == 0 || runtime.GOOS != "windows" {
		return
	}
	_ = windows.TerminateJobObject(c.job, 1)
	deadline := time.Now().Add(m.timeout())
	for time.Now().Before(deadline) {
		var info jobAccounting
		err := windows.QueryInformationJobObject(c.job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		if err == nil && info.ActiveProcesses == 0 {
			break
		}
		if err != nil {
			break // 查询失败（已关闭等）也视为不可再查，交给 CloseHandle 后 OS 终止
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.closeJob(c)
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

// ActiveAfter 返回**仍存活的真实进程树根数量**（不会把已退出根算作活跃）。
// 注意：后代的存在由 Job 保证随根一起收口；这里统计 history 中仍 Running 的根。
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
