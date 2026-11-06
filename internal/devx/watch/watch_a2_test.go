package watch

// R26-A A2 判别回归：真实进程交接（console helper）与 generate/build 分类/超时/次数。
// 进程测试只启动本线自有 console helper（无窗口），有界收口，断言无幸存子进程。
// RealBuilder 的 generate/build 用注入 fake 证明分类/argv/次数/超时（不跑完整 gio 构建）。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// buildConsoleApp 编译 testdata/consoleapp 到临时目录，返回二进制路径。仅在进程测试用。
func buildConsoleApp(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skip console helper build in -short")
	}
	root := repoRootForTest(t)
	out := filepath.Join(t.TempDir(), "consoleapp.exe")
	if runtime.GOOS != "windows" {
		out = filepath.Join(t.TempDir(), "consoleapp")
	}
	cmd := exec.Command("go", "build", "-o", out, "./internal/devx/watch/testdata/consoleapp")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if out2, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build consoleapp: %v\n%s", err, out2)
	}
	return out
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	// 从当前工作目录向上找 go.mod
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到 go.mod")
		}
		dir = parent
	}
}

// 1) 真实进程交接：启动 gen1 -> 启动 gen2 停 gen1 -> 收口；PID 不同、旧代已退出、无幸存。
func TestProcessManagerRealHandoff(t *testing.T) {
	bin := buildConsoleApp(t)
	pm := NewProcessManager(func(gen int) string { return bin })
	pm.Args = nil
	ctx := context.Background()

	if err := pm.Swap(ctx, 1); err != nil {
		t.Fatalf("swap gen1: %v", err)
	}
	c1 := pm.Current()
	if c1 == nil || !c1.Running() {
		t.Fatal("gen1 应运行")
	}
	pid1 := c1.PID

	if err := pm.Swap(ctx, 2); err != nil {
		t.Fatalf("swap gen2: %v", err)
	}
	c2 := pm.Current()
	if c2 == nil || !c2.Running() {
		t.Fatal("gen2 应运行")
	}
	if c2.PID == pid1 {
		t.Fatalf("gen2 PID 必须与 gen1 不同（%d）", pid1)
	}
	// 旧代 gen1 必须已退出
	if c1.Running() {
		t.Fatal("交接后旧代 gen1 必须已退出")
	}
	// 历史 2 代，当前存活仅 1
	if h := pm.History(); len(h) != 2 {
		t.Fatalf("history 应 2 代，实际 %d", len(h))
	}
	if n := pm.ActiveAfter(); n != 1 {
		t.Fatalf("应有且仅有 1 个存活子进程，实际 %d", n)
	}
	// 收口
	if err := pm.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if n := pm.ActiveAfter(); n != 0 {
		t.Fatalf("收口后不得有幸存子进程，实际 %d", n)
	}
}

// 2) 新代启动失败：旧代继续服务（不停止旧代），返回错误。
func TestProcessManagerSpawnFailKeepsOld(t *testing.T) {
	bin := buildConsoleApp(t)
	pm := NewProcessManager(func(gen int) string { return bin })
	ctx := context.Background()
	if err := pm.Swap(ctx, 1); err != nil {
		t.Fatalf("swap gen1: %v", err)
	}
	old := pm.Current()
	// 让 gen2 指向不存在的二进制
	pm.BinaryForGen = func(gen int) string {
		if gen == 2 {
			return filepath.Join(t.TempDir(), "does-not-exist")
		}
		return bin
	}
	if err := pm.Swap(ctx, 2); err == nil {
		t.Fatal("gen2 启动失败应返回错误")
	}
	if pm.Current() != old || !old.Running() {
		t.Fatal("新代启动失败时旧代必须继续服务")
	}
	_ = pm.Shutdown()
}

//  3. RealBuilder：generate 失败 -> Stage=generate，且不触发 build；build 失败 -> Stage=build；
//     成功 -> OK，且 go build argv 含 -o 与入口；统计 generate/build 各调用次数。
func TestRealBuilderClassifyAndCount(t *testing.T) {
	var genCalls, buildCalls int32
	var gotBuildArgs []string
	rb := &RealBuilder{
		ProjectDir: t.TempDir(),
		EntryPkg:   "./app",
		OutBinary:  filepath.Join(t.TempDir(), "bin"),
		Generate: func(ctx context.Context, dir string) (int, string) {
			atomic.AddInt32(&genCalls, 1)
			return 0, ""
		},
		BuildFn: func(ctx context.Context, args []string) (int, string) {
			atomic.AddInt32(&buildCalls, 1)
			gotBuildArgs = args
			return 0, ""
		},
	}
	res := rb.Build(context.Background(), 1, nil)
	if !res.OK || res.Gen != 1 {
		t.Fatalf("成功构建应 OK，实际 %+v", res)
	}
	if genCalls != 1 || buildCalls != 1 {
		t.Fatalf("generate/build 各应 1 次，实际 gen=%d build=%d", genCalls, buildCalls)
	}
	if !strings.Contains(strings.Join(gotBuildArgs, " "), "-o") || !strings.Contains(strings.Join(gotBuildArgs, " "), "./app") {
		t.Fatalf("go build argv 应含 -o 与入口: %v", gotBuildArgs)
	}

	// generate 失败：不进入 build
	genCalls, buildCalls = 0, 0
	rb.Generate = func(ctx context.Context, dir string) (int, string) {
		atomic.AddInt32(&genCalls, 1)
		return 2, "bad gxml"
	}
	res = rb.Build(context.Background(), 2, nil)
	if res.OK || res.Stage != "generate" {
		t.Fatalf("generate 失败应 Stage=generate，实际 %+v", res)
	}
	if buildCalls != 0 {
		t.Fatal("generate 失败不得触发 build")
	}

	// build 失败
	rb.Generate = func(ctx context.Context, dir string) (int, string) { return 0, "" }
	rb.BuildFn = func(ctx context.Context, args []string) (int, string) { return 1, "compile error" }
	res = rb.Build(context.Background(), 3, nil)
	if res.OK || res.Stage != "build" {
		t.Fatalf("build 失败应 Stage=build，实际 %+v", res)
	}
}

// 4) RealBuilder 超时：ctx 取消时返回失败（不 hang）。
func TestRealBuilderTimeout(t *testing.T) {
	rb := &RealBuilder{
		ProjectDir: t.TempDir(),
		Generate: func(ctx context.Context, dir string) (int, string) {
			<-ctx.Done()
			return -1, "timeout"
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := rb.Build(ctx, 1, nil)
	if time.Since(start) > 2*time.Second {
		t.Fatal("超时不得长时间 hang")
	}
	if res.OK {
		t.Fatal("超时应失败")
	}
}
