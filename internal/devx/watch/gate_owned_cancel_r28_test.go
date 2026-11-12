package watch

// R28 组合门禁：已拥有工具树在**显式取消**后立即收口、且取消后再放行也不迟到写盘。
// 门闩控制的自有根/子进程（无窗口）：根进程起一个 leaf 子进程；取消 RunCommandTree 后，leaf 已随 Job
// 终止（退出证明），随后即使写入 release 文件，leaf 也不会写 late 文件。用于断言“取消后用已持有 Job 句柄
// 退出”的窄修复，同时给出正常退出正控制与无后代遗留。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestGateOwnedTreeCancelStopsDescendantsNoLateWrite：显式 cancel -> 后代已退出 -> 取消后放行无 late 写。
func TestGateOwnedTreeCancelStopsDescendantsNoLateWrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	exe := filepath.Join(dir, "owned-gate-wrapper.exe")
	pidfile := filepath.Join(dir, "pid")
	release := filepath.Join(dir, "release")
	late := filepath.Join(dir, "late")
	code := `package main
import("os";"os/exec";"strconv";"time")
func main(){if os.Args[1]=="leaf"{os.WriteFile(os.Args[2],[]byte(strconv.Itoa(os.Getpid())),0600);end:=time.Now().Add(10*time.Second);for{if _,e:=os.Stat(os.Args[3]);e==nil{break};if time.Now().After(end){return};time.Sleep(time.Millisecond)};os.WriteFile(os.Args[4],[]byte("late"),0600);return};self,_:=os.Executable();c:=exec.Command(self,append([]string{"leaf"},os.Args[2:]...)...);c.Stdout=os.Stdout;c.Stderr=os.Stderr;c.Run()}`
	if err := os.WriteFile(src, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, e := exec.Command("go", "build", "-o", exe, src).CombinedOutput(); e != nil {
		t.Fatalf("build wrapper: %v %s", e, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var finished atomic.Bool
	done := make(chan ExecResult, 1)
	go func() {
		r := RunCommandTree(ctx, exe, []string{"parent", pidfile, release, late}, "", nil, 0)
		finished.Store(true)
		done <- r
	}()
	// 等 leaf 子进程起来（写 pid 文件）——真实后代就绪握手。
	deadline := time.After(4 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	pid := 0
	for {
		if b, e := os.ReadFile(pidfile); e == nil {
			if v, _ := strconv.Atoi(strings.TrimSpace(string(b))); v > 0 {
				pid = v
				break
			}
		}
		select {
		case <-deadline:
			t.Fatal("no owned descendant started")
		case <-tick.C:
		}
	}
	_ = pid
	// 显式取消（非短 deadline）：RunCommandTree 必须在有界时间内返回 Cancelled。
	cancel()
	select {
	case r := <-done:
		if !r.Cancelled {
			t.Fatalf("cancel must be observed as Cancelled, got %+v", r)
		}
	case <-time.After(2 * time.Second): // 墙钟仅作上限
		t.Fatal("RunCommandTree did not return within bound after explicit cancel")
	}
	// 取消返回后再放行 leaf 的 release：若后代仍活着会写 late 文件——必须不出现（无迟到写）。
	_ = os.WriteFile(release, []byte("release"), 0o600)
	time.Sleep(300 * time.Millisecond)
	if _, e := os.Stat(late); e == nil {
		t.Fatalf("late write after cancel: descendant still alive and wrote %s", late)
	}
	// 再确认 pid 进程确实不再运行（退出证明）。
	if pid > 0 && processStillAlive(pid) {
		t.Fatalf("owned descendant pid=%d still alive after cancel", pid)
	}
}

// TestGateOwnedTreeNormalExitPositiveControl：正常退出（不取消）返回 rc=0 且 leaf 不写 late（被 release 提前放行）。
func TestGateOwnedTreeNormalExitPositiveControl(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	exe := filepath.Join(dir, "owned-gate-ok.exe")
	late := filepath.Join(dir, "late")
	code := `package main
import("os";"time")
func main(){time.Sleep(20*time.Millisecond);os.WriteFile(os.Args[1],[]byte("late"),0600);os.Exit(0)}`
	if err := os.WriteFile(src, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, e := exec.Command("go", "build", "-o", exe, src).CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	r := RunCommandTree(context.Background(), exe, []string{late}, "", nil, 5*time.Second)
	if r.Cancelled || r.RC != 0 {
		t.Fatalf("normal exit positive control: want rc=0 not cancelled, got %+v", r)
	}
	if _, e := os.Stat(late); e != nil {
		t.Fatalf("normal exit should have written late file: %v", e)
	}
}

// processStillAlive 用真实 Windows 句柄判定 PID 是否仍在运行（能否 OpenProcess + 未 signal）。
func processStillAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false // 打不开（不存在/已退出）=> 不存活
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return false
	}
	return state == uint32(windows.WAIT_TIMEOUT) // 仍运行
}
