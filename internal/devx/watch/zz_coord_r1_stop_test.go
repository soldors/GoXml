package watch

// R28 Manager R2：旧 TestCoordR1ManagerStopTimeoutIncludesTreeCommand 已改测 —— Manager 的自有 Windows Job
// 走已持有 Job 句柄（TerminateAndWaitJob），**不再**调用 PID 型外部命令。本测试断言：
//   (a) Shutdown 在有界时间内收口（不依赖外部 taskkill）；
//   (b) PATH 中的 taskkill 替身**不得收到调用**（未被启动）；
//   (c) 保留真实自有根及后代的退出证明（两者 WAIT_OBJECT_0）。
// 旧「必须调用 taskkill」的实现限制已由协调者决定替代；慢替身红证仍存档（历史）。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestCoordR1OwnedConsoleHelper 是父根：启动一个真实 leaf 后代（写 pid），然后自身长睡，等待被终止。
func TestCoordR1OwnedConsoleHelper(t *testing.T) {
	if os.Getenv("COORD_STOP_HELPER") != "1" {
		return
	}
	leafPidFile := os.Getenv("COORD_STOP_LEAF_PID")
	self, e := os.Executable()
	if e != nil {
		os.Exit(2)
	}
	leaf := exec.Command(self, "-test.run=^TestCoordR1OwnedConsoleLeaf$")
	leaf.Env = append(os.Environ(), "COORD_STOP_LEAF=1", "COORD_STOP_LEAF_PID="+leafPidFile)
	if err := leaf.Start(); err != nil {
		os.Exit(3)
	}
	time.Sleep(30 * time.Second)
}

func TestCoordR1OwnedConsoleLeaf(t *testing.T) {
	if os.Getenv("COORD_STOP_LEAF") != "1" {
		return
	}
	if p := os.Getenv("COORD_STOP_LEAF_PID"); p != "" {
		_ = os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0600)
	}
	time.Sleep(30 * time.Second)
}

func TestCoordR1ManagerStopTimeoutIncludesTreeCommand(t *testing.T) {
	realGo, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	// taskkill 替身：一旦被启动即写 marker（用于断言“从未被调用”）。
	bin := filepath.Join(root, "taskkill.exe")
	src := filepath.Join(root, "shim.go")
	code := `package main
import("os";"time")
func main(){os.WriteFile(os.Getenv("COORD_STOP_ENTERED"),[]byte("entered"),0600);time.Sleep(2*time.Second)}
`
	if err := os.WriteFile(src, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	if out, e := exec.Command(realGo, "build", "-o", bin, src).CombinedOutput(); e != nil {
		t.Fatalf("shim build: %v %s", e, out)
	}
	entered := filepath.Join(root, "entered")
	leafPidFile := filepath.Join(root, "leaf.pid")
	t.Setenv("COORD_STOP_ENTERED", entered)
	t.Setenv("COORD_STOP_HELPER", "1")
	t.Setenv("COORD_STOP_LEAF_PID", leafPidFile)

	self, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	pm := NewProcessManager(func(int) string { return self })
	pm.Args = []string{"-test.run=^TestCoordR1OwnedConsoleHelper$"}
	pm.StopTimeout = 2 * time.Second
	if e = pm.Swap(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer pm.Shutdown()
	rootPID := pm.Current().PID

	// 就绪握手：等真实 leaf 后代写出自身 PID。
	deadline := time.Now().Add(5 * time.Second)
	leafPID := 0
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(leafPidFile); e == nil {
			if v, _ := strconv.Atoi(strings.TrimSpace(string(b))); v > 0 {
				leafPID = v
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if leafPID == 0 {
		t.Fatal("真实 leaf 后代未就绪")
	}
	// 停前持有根与叶两个同步句柄。
	rootH, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(rootPID))
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(rootH)
	leafH, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(leafPID))
	if e != nil {
		t.Fatal(e)
	}
	defer windows.CloseHandle(leafH)

	// PATH 指向 taskkill 替身所在：Job-only 路径**不得**启动它。
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- pm.Shutdown() }()
	var stopErr error
	select {
	case stopErr = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopTimeout=2s 未在有界时间内收口（不应依赖外部 taskkill）")
	}
	elapsed := time.Since(start)
	if stopErr != nil {
		t.Fatalf("shutdown 应成功收口: %v", stopErr)
	}
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("Shutdown 超出 StopTimeout 界: %v", elapsed)
	}
	// (b) 外部 taskkill 替身不得收到调用。
	if _, e := os.Stat(entered); e == nil {
		t.Fatal("Manager 停止路径调用了 PID 型外部任务终止命令（应仅用 Job 句柄）")
	}
	// (c) 真实根与后代退出证明：WAIT_OBJECT_0。
	if state, werr := windows.WaitForSingleObject(rootH, 1000); werr != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("根未退出: state=%d err=%v", state, werr)
	}
	if state, werr := windows.WaitForSingleObject(leafH, 1000); werr != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("后代未退出: state=%d err=%v", state, werr)
	}
}
