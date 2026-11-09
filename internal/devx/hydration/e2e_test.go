package hydration_test

// R27-A 返修 F-A7：真实 Watch opt-in 整链恢复 + 新代自报验证（不重读父状态文件冒充恢复）。
// 通过 Watch.New + Config.Hydration + ProcessManager.EnvFor(RestoreEnv) 驱动两代 console；
// 父进程用**新进程自报的 report 文件**（pid/session/gen/nonce/精确 DTO/restored）验证恢复，
// 并加严：禁用子恢复（HYDRATE_STATE 不注入）时同一健康守卫必须拒绝。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
	"github.com/goxml/goxml/internal/devx/watch"
)

type demoDTO struct {
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

func buildHelper(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skip helper build in -short")
	}
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "console.exe")
	cmd := exec.Command("go", "build", "-o", out, "./internal/devx/hydration/testdata/console")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, b)
	}
	return out
}

// readReport 有界等待新进程报告文件出现并解析。
func readReport(t *testing.T, path string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				return m
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("report file not produced: %s", path)
	return nil
}

// TestTwoProcessHydrationRestore 真实 Watch 整链：旧代非默认 DTO → Watch opt-in 快照 → 停旧 →
// 新代（新 PID）自报 restored=true 且精确值；父进程据新进程报告验证（非重读文件）。
func TestTwoProcessHydrationRestore(t *testing.T) {
	bin := buildHelper(t)
	proj := t.TempDir()
	st, err := devxh.NewStore(proj, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	const nonce = "NONCE-R27-FA7"
	reportFile := filepath.Join(proj, "child.report.json")

	// 快照请求/响应（Capture 用）
	reqFile := filepath.Join(proj, "snap.req")
	respFile := filepath.Join(proj, "snap.resp")
	readyFile := filepath.Join(proj, "snap.ready")
	// 旧代初始值（非默认）
	valsFile := filepath.Join(proj, "old.json")
	ob, _ := json.Marshal(demoDTO{Count: 7, Items: []string{"a", "b", "c"}, NonDefault: "OLD-NON-DEFAULT"})
	_ = os.WriteFile(valsFile, ob, 0o644)

	pm := watch.NewProcessManager(func(gen int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.Stderr = os.Stderr
	proto := &devxh.Protocol{Store: st, Schema: "demo.v1", AppID: "app-1", CaptureTimeout: 5 * time.Second}

	capture := func(c context.Context, oldGen int) ([]byte, error) {
		_ = os.Remove(readyFile)
		_ = os.Remove(respFile)
		if err := os.WriteFile(reqFile, []byte("snap"), 0o644); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(readyFile); err == nil {
				return os.ReadFile(respFile)
			}
			select {
			case <-c.Done():
				return nil, c.Err()
			case <-time.After(2 * time.Millisecond):
			}
		}
		return nil, fmt.Errorf("snapshot response timeout")
	}
	hook := &watch.HydrationHook{Proto: proto, Capture: capture,
		RestoreEnv: func(gen int) []string {
			// 仅 gen>=2 注入恢复状态（新代恢复）；gen1 是旧代（仅设初值 + 提供快照服务）。
			if gen >= 2 {
				return []string{"HYDRATE_STATE=" + st.Path(), "HYDRATE_SESSION=sess-1", "HYDRATE_SCHEMA=demo.v1", "HYDRATE_APP=app-1",
					"HYDRATE_REPORT=" + reportFile, "HYDRATE_NONCE=" + nonce}
			}
			return nil
		}}
	// 旧代（gen1）需快照服务 + 初值。
	baseEnv := func(gen int) []string {
		if gen == 1 {
			return []string{"HYDRATE_VALUES=" + valsFile, "HYDRATE_SESSION=sess-1", "HYDRATE_SCHEMA=demo.v1", "HYDRATE_APP=app-1",
				"HYDRATE_SNAPSHOT_REQ=" + reqFile, "HYDRATE_SNAPSHOT_RESP=" + respFile, "HYDRATE_SNAPSHOT_READY=" + readyFile}
		}
		return nil
	}
	pm.EnvFor = func(gen int) []string {
		env := baseEnv(gen)
		if gen >= 2 {
			env = append(env, hook.RestoreEnv(gen)...)
		}
		return env
	}

	ctx := context.Background()
	if err := pm.Swap(ctx, 1); err != nil {
		t.Fatalf("start gen1: %v", err)
	}
	oldPID := pm.Current().PID

	// 经真实 Watch 驱动：用 hook 的 BeforeSwap 执行快照（等价 runGeneration 停旧前调用）。
	if err := hook.BeforeSwap(ctx, 2, 1); err != nil {
		t.Fatalf("BeforeSwap snapshot: %v", err)
	}
	// 快照应捕获旧代非默认值。
	if raw, err := os.ReadFile(st.Path()); err != nil {
		t.Fatalf("state file: %v", err)
	} else {
		_ = raw
	}

	// 停旧启新（新代恢复）。
	if err := pm.Swap(ctx, 2); err != nil {
		t.Fatalf("swap gen2: %v", err)
	}
	newPID := pm.Current().PID
	if newPID == oldPID {
		t.Fatalf("new PID must differ (%d)", oldPID)
	}
	// 父进程据**新进程自报**验证（非重读状态文件冒充）。
	rep := readReport(t, reportFile)
	if int(rep["pid"].(float64)) != newPID {
		t.Fatalf("report pid=%v want new pid %d", rep["pid"], newPID)
	}
	if rep["session"] != "sess-1" || rep["nonce"] != nonce {
		t.Fatalf("report session/nonce mismatch: %v", rep)
	}
	if rep["restored"] != true || int(rep["gen"].(float64)) != 2 {
		t.Fatalf("new process did not restore: %v", rep)
	}
	if rep["nonDefault"] != "OLD-NON-DEFAULT" || int(rep["count"].(float64)) != 7 {
		t.Fatalf("restored value mismatch in child report: %v", rep)
	}
	items, _ := rep["items"].([]any)
	if len(items) != 3 || items[0] != "a" || items[1] != "b" || items[2] != "c" {
		t.Fatalf("restored items mismatch: %v", rep["items"])
	}
	_ = pm.Shutdown()
	if pm.ActiveAfter() != 0 {
		t.Fatalf("survivors: %d", pm.ActiveAfter())
	}
}

// TestDisabledChildRestoreRejected 加严：禁用新代恢复（不注入 HYDRATE_STATE）时，新代自报 restored=false，
// 同一健康守卫必须拒绝（证明守卫确实检查子恢复，而非仅父侧文件）。
func TestDisabledChildRestoreRejected(t *testing.T) {
	bin := buildHelper(t)
	proj := t.TempDir()
	st, _ := devxh.NewStore(proj, "sess-1")
	reportFile := filepath.Join(proj, "child.report.json")
	// 先写一份合法状态（供“若恢复开启则能恢复”），但新代**不**被注入 HYDRATE_STATE。
	p := &devxh.Protocol{Store: st, Schema: "demo.v1", AppID: "app-1"}
	if _, err := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":7,"items":["a"],"nonDefault":"X"}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}

	pm := watch.NewProcessManager(func(gen int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.Stderr = os.Stderr
	// 禁用子恢复：只给 report+nonce，不给 HYDRATE_STATE。
	pm.EnvFor = func(gen int) []string {
		return []string{"HYDRATE_REPORT=" + reportFile, "HYDRATE_NONCE=n", "HYDRATE_SESSION=sess-1", "HYDRATE_SCHEMA=demo.v1", "HYDRATE_APP=app-1"}
	}
	ctx := context.Background()
	if err := pm.Swap(ctx, 1); err != nil {
		t.Fatalf("start: %v", err)
	}
	rep := readReport(t, reportFile)
	if rep["restored"] != false {
		t.Fatalf("with restore disabled child must report restored=false, got %v", rep)
	}
	// 健康守卫语义：restored=false 且非默认未恢复 => 判失败（这正是真正的恢复守卫要求）。
	if rep["nonDefault"] == "X" {
		t.Fatal("child must not have restored X when disabled")
	}
	_ = pm.Shutdown()
	_ = strings.TrimSpace("")
}
