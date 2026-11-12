package watch

// R28 Manager 服务进程树收口 —— 确定性时间线复现 + 判别回归（Windows-only，无窗口，自有根/后代）。
// 用 PATH 覆盖的 taskkill 垫片（shim）**确定性地**让外部 taskkill 消耗 StopTimeout，复现
//   “job 未在 StopTimeout 内清空（ActiveProcesses 未归零）” 与 “process exit proof deadline expired”。
// 该垫片在 StopTimeout 里 sleep 后成功退出，模拟真实 taskkill 在重负载/竞争下耗时；不依赖真实 CPU 竞争。
// 修复后：Manager 先用已持有 Job 句柄终止整棵自有树（不受 taskkill 耗时挤占），taskkill 仅作有界补充，
// 仍在单一 StopTimeout 内，退出证明用句柄 signaled。

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// buildTaskkillShim 编译一个 taskkill 垫片：进入时写 entered 文件，sleep delay 后写 exited 文件并退出 0。
func buildTaskkillShim(t *testing.T, delay time.Duration, entered, exited string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "shim.go")
	bin := filepath.Join(dir, "taskkill.exe")
	code := fmt.Sprintf(`package main
import("os";"time")
func main(){os.WriteFile(%q,[]byte("entered"),0600);time.Sleep(%d);os.WriteFile(%q,[]byte("exited"),0600)}
`, entered, int64(delay), exited)
	if err := os.WriteFile(src, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, e := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); e != nil {
		t.Fatalf("build taskkill shim: %v %s", e, out)
	}
	return dir
}

// buildFamily 编译根/后代 helper：parent 起 leaf 并阻塞在 stdin 读；leaf 写 pid 后长睡。
func buildFamily(t *testing.T, dir string) (string, string) {
	t.Helper()
	src := filepath.Join(dir, "main.go")
	bin := filepath.Join(dir, "family.exe")
	pidfile := filepath.Join(dir, "leaf.pid")
	const program = `package main
import("os";"os/exec";"strconv";"time")
func main(){if os.Args[1]=="leaf"{os.WriteFile(os.Args[2],[]byte(strconv.Itoa(os.Getpid())),0600);time.Sleep(60*time.Second);return};self,_:=os.Executable();c:=exec.Command(self,"leaf",os.Args[2]);if e:=c.Start();e!=nil{os.Exit(2)};_=c;time.Sleep(60*time.Second)}`
	if err := os.WriteFile(src, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, e := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); e != nil {
		t.Fatalf("build family: %v %s", e, out)
	}
	return bin, pidfile
}

func waitPIDFile(t *testing.T, pidfile string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(pidfile); e == nil {
			if v, _ := strconv.Atoi(strings.TrimSpace(string(b))); v > 0 {
				return v
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("leaf pid not reported")
	return 0
}

// TestManagerShutdownReapsDespiteSlowTaskkillExceedingStopTimeout：外部 taskkill 垫片 sleep(>StopTimeout=1s)
// 时，修复后 Shutdown 仍须在单一 StopTimeout 内用已持有 Job 句柄收口成功、leaf 退出。这是判别回归；
// 变异（回退 proc_windows.stopJob 到 taskkill-first）会真实翻红（见 raw/mut）。
func TestManagerShutdownReapsDespiteSlowTaskkillExceedingStopTimeout(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only manager job tree proof")
	}
	dir := t.TempDir()
	bin, pidfile := buildFamily(t, dir)
	entered := filepath.Join(dir, "tk.entered")
	exited := filepath.Join(dir, "tk.exited")
	shimDir := buildTaskkillShim(t, 1200*time.Millisecond, entered, exited)

	pm := NewProcessManager(func(int) string { return bin })
	pm.Args = []string{"parent", pidfile}
	pm.StopTimeout = time.Second // single StopTimeout; shim 0.9s 挤占其中大部分
	if e := pm.Swap(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pm.Shutdown() })
	leaf := waitPIDFile(t, pidfile)
	handle, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(leaf))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { windows.CloseHandle(handle) })

	// 记录根是否仍在服务（诊断）。
	rootAlive := pm.Current() != nil && pm.Current().Running()
	t.Logf("pre-shutdown rootAlive=%v currentPID=%v", rootAlive, func() any {
		if pm.Current() != nil {
			return pm.Current().PID
		}
		return nil
	}())
	// 记录时间线。
	t0 := time.Now()
	tel := &timeline{}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := pm.Shutdown()
	tel.add("shutdown-return", time.Since(t0), err)
	if _, e := os.Stat(entered); e == nil {
		tel.add("taskkill-entered(observed)", time.Since(t0), nil)
	}
	if _, e := os.Stat(exited); e == nil {
		tel.add("taskkill-exited", time.Since(t0), nil)
	} else {
		tel.add("taskkill-NOT-exited", time.Since(t0), nil)
	}
	t.Logf("hasShimDir=%q PATHhead=%q", shimDir, func() string {
		p := os.Getenv("PATH")
		if len(p) > 60 {
			return p[:60]
		}
		return p
	}())
	state, _ := windows.WaitForSingleObject(handle, 0)
	leafAlive := state == uint32(windows.WAIT_TIMEOUT)
	t.Logf("timeline=%v leafAliveAfterShutdown=%v err=%v", tel.entries, leafAlive, err)

	// 修复后判据（既有 taskkill 慢于单 StopTimeout 也不能饿死收口）：Shutdown 成功、leaf 已退出、
	// 在单一 StopTimeout 有界完成（不靠放大 StopTimeout）。变异（回退 proc_windows.stopJob）会翻红。
	if err != nil {
		t.Fatalf("shutdown must reap despite slow taskkill (>StopTimeout): %v; timeline=%v", err, tel.entries)
	}
	if leafAlive {
		t.Fatalf("leaf still alive after shutdown; timeline=%v", tel.entries)
	}
	if el := time.Since(t0); el > 3*time.Second {
		t.Fatalf("shutdown not bounded: %v", el)
	}
	t.Logf("GREEN: reaped despite taskkill>StopTimeout; timeline=%v", tel.entries)
}

// TestManagerShutdownReapsAfterTaskkillDelay 是修复后的判别回归：即使外部 taskkill 慢（垫片 0.9s），
// Shutdown 仍须在单一 StopTimeout 内收口成功（用已持有 Job 句柄终止），且 leaf 句柄 signaled。
func TestManagerShutdownReapsAfterTaskkillDelay(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	dir := t.TempDir()
	bin, pidfile := buildFamily(t, dir)
	entered := filepath.Join(dir, "tk.entered")
	exited := filepath.Join(dir, "tk.exited")
	shimDir := buildTaskkillShim(t, 400*time.Millisecond, entered, exited)

	pm := NewProcessManager(func(int) string { return bin })
	pm.Args = []string{"parent", pidfile}
	pm.StopTimeout = 3 * time.Second // 含 0.4s taskkill 仍有充足预算
	if e := pm.Swap(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pm.Shutdown() })
	leaf := waitPIDFile(t, pidfile)
	handle, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(leaf))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { windows.CloseHandle(handle) })
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if e := pm.Shutdown(); e != nil {
		t.Fatalf("shutdown must reap despite taskkill delay: %v", e)
	}
	state, _ := windows.WaitForSingleObject(handle, 0)
	if state == uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("leaf still alive after shutdown")
	}
}

// timeline 收集 (label, elapsed, err) 事件。
type timeline struct {
	mu      sync.Mutex
	entries []string
}

func (tl *timeline) add(label string, d time.Duration, err error) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if err != nil {
		tl.entries = append(tl.entries, fmt.Sprintf("%s@%dms(err=%v)", label, d.Milliseconds(), err))
	} else {
		tl.entries = append(tl.entries, fmt.Sprintf("%s@%dms", label, d.Milliseconds()))
	}
}
