package watch

// R26-A stop() 超时强杀进程树分支（有界）+ Watch Stop/Stopped。
// 用 Spawn 注入一个**不读 stdin、短期内不退出**的真实进程（本机 shell 睡眠），
// 验证 stop 会在 StopTimeout 内强杀并收口；不启动 GUI、不用用户 main。

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// stubbornSpawn 返回一个忽略 stdin、循环睡眠的真实子进程（用于命中有界强杀路径）。
func stubbornSpawn(gen int, bin string, args []string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" {
		// ping 自身地址到不可达超时作为阻塞（无需外部工具安装；ping 是系统自带）。
		return exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 >NUL"), nil
	}
	return exec.Command("sh", "-c", "sleep 30"), nil
}

// 命中有界强杀：不读 stdin 的子进程，stop 的优雅等待超时后 Kill+tree 收口。
func TestStopTimeoutTreeKill(t *testing.T) {
	pm := NewProcessManager(func(gen int) string { return "unused" })
	pm.Spawn = stubbornSpawn
	pm.StopTimeout = 400 * time.Millisecond // 有界
	ctx := context.Background()
	if err := pm.Swap(ctx, 1); err != nil {
		// Swap 的新代存活观察 50ms 内应存活；若确实无法启动则跳过（环境差异），不误判。
		t.Skipf("stubborn spawn unavailable: %v", err)
	}
	c := pm.Current()
	if c == nil || !c.Running() {
		t.Fatal("stubborn 子进程应运行")
	}
	start := time.Now()
	stopped, _ := pm.stop(c)
	elapsed := time.Since(start)
	if !stopped {
		t.Fatal("有界强杀后应已收口")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stop 不得无界等待，实际 %v", elapsed)
	}
	if c.Running() {
		t.Fatal("强杀后不得仍存活")
	}
}

// Watch Stop/Stopped。
func TestWatchStopStopped(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir}, fb, fs, nil)
	if w.Stopped() {
		t.Fatal("初始应为未停止")
	}
	w.Stop()
	if !w.Stopped() {
		t.Fatal("Stop 后 Stopped 应为 true")
	}
}
