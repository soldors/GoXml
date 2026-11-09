package hydrationchannel_test

// R28-A S2：真实 Watch → 旧应用(端点) → 新应用(恢复) 整链。
// 用真实 watch.New + NotifyChange/Tick 驱动 runGeneration；旧子进程用 hydrationchannel.Endpoint 暴露快照；
// 父端据旧代 portfile 构造 Client，经 NewWatchHook 接 HydrationHook.Capture；新子进程经 Applier 恢复并
// 自报 pid/session/gen/nonce/精确 DTO/restored。父端核验子报告（不重读状态文件）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
	"github.com/goxml/goxml/internal/devx/hydrationchannel"
	"github.com/goxml/goxml/internal/devx/watch"
)

type dto struct {
	Count      int      `json:"count"`
	Items      []string `json:"items"`
	NonDefault string   `json:"nonDefault"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		p := filepath.Dir(dir)
		if p == dir {
			t.Fatal("go.mod not found")
		}
		dir = p
	}
}

func buildConsole(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skip console build in -short")
	}
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "hconsole.exe")
	if runtime.GOOS != "windows" {
		out = filepath.Join(t.TempDir(), "hconsole")
	}
	cmd := exec.Command("go", "build", "-o", out, "./internal/devx/hydrationchannel/testdata/consoleapp")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build console: %v\n%s", err, b)
	}
	return out
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				return m
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("file not produced: %s", path)
	return nil
}

// okBuilder 恒成功 Builder。
type okBuilder struct{}

func (okBuilder) Build(_ context.Context, gen int, _ []string) watch.GenerationResult {
	return watch.GenerationResult{OK: true, Gen: gen}
}

// TestWatchChannelTwoProcessRecovery 真实 Watch 整链。
func TestWatchChannelTwoProcessRecovery(t *testing.T) {
	bin := buildConsole(t)
	proj := t.TempDir()
	scratch := t.TempDir() // 端口/报告/旧值文件在 projectDir 之外，避免被 Watch 当输入
	st, err := devxh.NewStore(proj, "sess-chain")
	if err != nil {
		t.Fatal(err)
	}
	const nonce = "NONCE-R28-S2"
	reportFile := filepath.Join(scratch, "child.report.json")
	portFile := filepath.Join(scratch, "old.port.json")
	valsFile := filepath.Join(scratch, "old.values.json")
	ob, _ := json.Marshal(dto{Count: 7, Items: []string{"a", "b", "c"}, NonDefault: "OLD-NON-DEFAULT"})
	_ = os.WriteFile(valsFile, ob, 0o644)

	const token1 = "tok-gen1-" + nonce
	const token2 = "tok-gen2-" + nonce

	pm := watch.NewProcessManager(func(int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.Stderr = os.Stderr

	common := func(gen int) []string {
		return []string{"HYD_PROJECT=" + proj, "HYD_SESSION=sess-chain", "HYD_APP=app-1", "HYD_SCHEMA=demo.v1",
			"HYD_PORTFILE=" + portFile, fmt.Sprintf("HYD_GEN=%d", gen)}
	}
	pm.EnvFor = func(gen int) []string {
		env := common(gen)
		switch gen {
		case 1:
			env = append(env, "HYD_TOKEN="+token1, "HYD_VALUES="+valsFile)
		case 2:
			env = append(env, "HYD_TOKEN="+token2, "HYD_STATE="+st.Path(),
				"HYD_REPORT="+reportFile, "HYD_NONCE="+nonce)
		}
		return env
	}

	proto := &devxh.Protocol{Store: st, Schema: "demo.v1", AppID: "app-1", CaptureTimeout: 5 * time.Second}
	// 父端维护“当前在服务代次的 portfile 与 token”。
	type portInfo struct {
		addr  string
		token string
	}
	var serving *portInfo
	// ClientFor(oldGen)：把旧代 portfile 读出来构造 Client（token 父端按 gen 提供）。
	cfg := hydrationchannel.HookConfig{
		Proto: proto,
		ClientFor: func(oldGen int) (*hydrationchannel.Client, bool) {
			if oldGen != 1 || serving == nil {
				return nil, false
			}
			cl, e := hydrationchannel.NewClient(serving.addr, "sess-chain", "app-1", "demo.v1", serving.token)
			if e != nil {
				return nil, false
			}
			return cl, true
		},
		RestoreEnv: func(gen int) []string {
			// 新代由 pm.EnvFor 注入 state/report；这里补充会话维度（幂等）。
			return []string{"HYD_SESSION=sess-chain", "HYD_APP=app-1", "HYD_SCHEMA=demo.v1"}
		},
	}
	hook, err := hydrationchannel.NewWatchHook(cfg)
	if err != nil {
		t.Fatal(err)
	}

	w, err := watch.New(watch.Config{ProjectDir: proj, DebounceMS: 1, Hydration: hook}, okBuilder{}, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := pm.Shutdown(); e != nil {
			t.Errorf("shutdown: %v", e)
		}
	}()
	ctx := t.Context()

	// gen1：首次发布（冷启动，无旧服务 → hook 跳过捕获 → start gen1）。
	if _, err := w.NotifyChange(ctx, 0, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() != 1 || pm.Current() == nil || !pm.Current().Running() {
		t.Fatalf("gen1 not serving/published")
	}
	oldPID := pm.Current().PID
	// 等 gen1 端点就绪（portfile 出现）。
	pi := readJSON(t, portFile)
	if int(pi["gen"].(float64)) != 1 {
		t.Fatalf("portfile gen=%v want 1", pi["gen"])
	}
	serving = &portInfo{addr: pi["addr"].(string), token: token1}
	_ = os.Remove(reportFile)

	// gen2：变更 → runGeneration → hook.BeforeSwap(Client.Capture 旧 gen1 端点) → stop-old/start-new(恢复)。
	if _, err := w.NotifyChange(ctx, 200, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 300); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() != 2 {
		t.Fatalf("gen2 not published (channel chain failed): %d", w.LastPublishedGen())
	}
	if pm.Current() == nil || !pm.Current().Running() {
		t.Fatal("gen2 not serving")
	}
	newPID := pm.Current().PID
	if newPID == oldPID {
		t.Fatalf("PID did not change (%d)", oldPID)
	}
	// 父端据**新进程自报**核验。
	rep := readJSON(t, reportFile)
	if int(rep["pid"].(float64)) != newPID {
		t.Fatalf("report pid=%v want %d", rep["pid"], newPID)
	}
	if rep["session"] != "sess-chain" || rep["nonce"] != nonce {
		t.Fatalf("report session/nonce mismatch: %v", rep)
	}
	if rep["restored"] != true {
		t.Fatalf("new process did not restore via channel: %v", rep)
	}
	if rep["nonDefault"] != "OLD-NON-DEFAULT" || int(rep["count"].(float64)) != 7 {
		t.Fatalf("restored value mismatch: %v", rep)
	}
	items, _ := rep["items"].([]any)
	if len(items) != 3 || items[0] != "a" || items[1] != "b" || items[2] != "c" {
		t.Fatalf("restored items mismatch: %v", rep["items"])
	}
}
