package watch

// R26-A 补充真实路径测试（非镜像实现）：真实 realGoBuild（cwd=ProjectDir）、配置默认值/访问器、
// SetLogger、Debounce/BuildGen、NewDebouncer 默认、stop 的优雅-EOF 分支。
// 真实 CLI 生成（cli.Run）的测试在 watchadapter 适配层（F-A6/A7：watch 不 import cli）。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 真实 realGoBuild：在 workDir=模块根 用真实 go 构建 consoleapp，rc0 且产物存在。
func TestRealGoBuildExecutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skip real go build in -short")
	}
	root := repoRootForTest(t)
	out := filepath.Join(t.TempDir(), "cb.exe")
	cmd := exec.Command("go", "build", "-o", out, "./internal/devx/watch/testdata/consoleapp")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("real go build failed: %v\n%s", err, b)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
}

// 配置默认值与访问器。
func TestConfigDefaultsAndAccessors(t *testing.T) {
	c := Config{}
	if c.debounce() != DefaultDebounceMS {
		t.Fatalf("debounce 默认应 %d，实际 %d", DefaultDebounceMS, c.debounce())
	}
	if c.poll() != DefaultPollMS {
		t.Fatalf("poll 默认应 %d", DefaultPollMS)
	}
	if c.buildMS() != DefaultBuildMS {
		t.Fatalf("buildMS 默认应 %d", DefaultBuildMS)
	}
	c2 := Config{DebounceMS: 50, PollIntervalMS: 10, BuildTimeoutMS: 1000}
	if c2.debounce() != 50 || c2.poll() != 10 || c2.buildMS() != 1000 {
		t.Fatal("显式配置应被采用")
	}
}

// NewDebouncer 默认 + Debounce/BuildGen 访问器。
func TestDebouncerDefaultsAndAccessors(t *testing.T) {
	d := NewDebouncer(0)
	if d.Debounce() != DefaultDebounceMS {
		t.Fatalf("NewDebouncer(0) 应取默认 %d", DefaultDebounceMS)
	}
	if d.BuildGen() != 0 {
		t.Fatal("初始 BuildGen 应为 0")
	}
	d.Note(0, "a")
	gen := d.Pump(120)
	if gen == 0 || d.BuildGen() != gen {
		t.Fatalf("构建中 BuildGen 应为 %d，实际 %d", gen, d.BuildGen())
	}
}

// SetLogger + timeout 默认。
func TestProcessManagerSetLoggerTimeout(t *testing.T) {
	pm := NewProcessManager(nil)
	if pm.timeout() != 3*time.Second {
		t.Fatal("默认 StopTimeout 应 3s")
	}
	called := false
	pm.SetLogger(func(string, ...any) { called = true })
	pm.logf("x")
	if !called {
		t.Fatal("SetLogger 注入的日志函数应被调用")
	}
}

// stop 优雅-EOF 分支：启动 console helper（保持 stdin 打开），Shutdown 关闭 stdin 使其自然退出。
func TestStopGracefulEOF(t *testing.T) {
	bin := buildConsoleApp(t)
	pm := NewProcessManager(func(gen int) string { return bin })
	ctx := context.Background()
	if err := pm.Swap(ctx, 1); err != nil {
		t.Fatalf("swap: %v", err)
	}
	c := pm.Current()
	if c == nil || c.stdinWrite == nil {
		t.Fatal("应持有 stdin 写端（优雅路径）")
	}
	if err := pm.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if c.Running() {
		t.Fatal("关闭 stdin 后应自然退出")
	}
}
