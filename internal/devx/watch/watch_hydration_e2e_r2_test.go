package watch

// R27-A 返修 R2（F-A7-R1）：真实 Watch 整链 E2E。
// 以真实 watch.New + NotifyChange/Tick 进入 runGeneration，驱动：首次发布 → 变更 → 捕获(hook.BeforeSwap)
// → stop-old/start-new → 新代恢复。**不**手工调用 hook.BeforeSwap / pm.Swap 冒充编排。
// 新代（console helper）自报 PID/session/gen/nonce/非默认精确 DTO/restored 到 report 文件，父进程验证子报告。
//
// 同一健康守卫在两种**可编译生产退化 overlay**下必须真实 FAIL（见 raw/mut）：
//   1) 关闭 Watch Hydration 分支（不捕获 → 新代无状态可恢复 → 非默认未恢复）；
//   2) 关闭子进程恢复（console 忽略 HYDRATE_STATE → restored=false）。

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
)

// okBuilder 是恒成功的 Builder（真实 Watch 编排用；不跑真实 gio 构建）。
type okBuilder struct{ calls int }

func (b *okBuilder) Build(_ context.Context, gen int, _ []string) GenerationResult {
	b.calls++
	return GenerationResult{OK: true, Gen: gen}
}

func buildHydrationConsole(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skip console build in -short")
	}
	root := repoRootForTest(t)
	out := filepath.Join(t.TempDir(), "hydconsole.exe")
	if runtime.GOOS != "windows" {
		out = filepath.Join(t.TempDir(), "hydconsole")
	}
	buildArgs := []string{"build", "-o", out}
	// 允许外部以 overlay 退化 console 源码（child-restore-disabled 可编译退化）；overlay 需传到嵌套 go build。
	if ov := os.Getenv("HYDRATE_CONSOLE_OVERLAY"); ov != "" {
		buildArgs = append(buildArgs, "-overlay", ov)
	}
	buildArgs = append(buildArgs, "./internal/devx/hydration/testdata/console")
	cmd := exec.Command("go", buildArgs...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build hydration console: %v\n%s", err, b)
	}
	return out
}

func readReportR2(t *testing.T, path string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				return m
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("child report not produced: %s", path)
	return nil
}

// TestTwoProcessHydrationRestoreViaWatch 真实 Watch 驱动整链恢复。
func TestTwoProcessHydrationRestoreViaWatch(t *testing.T) {
	bin := buildHydrationConsole(t)
	proj := mkProj(t) // 有 ui/home.gxml 的合法工程目录
	st, err := devxh.NewStore(proj, "sess-watch")
	if err != nil {
		t.Fatal(err)
	}
	const nonce = "NONCE-WATCH-R2"
	// 注意：快照请求/响应/报告/旧值文件必须放在 **projectDir 之外**，否则捕获期间写入会被 Watch
	// 当作新用户输入（F-A6 复扫判 stale）导致 gen2 不发布——这是测试框架问题，非产品缺陷。
	scratch := t.TempDir()
	reportFile := filepath.Join(scratch, "child.report.json")
	reqFile := filepath.Join(scratch, "snap.req")
	respFile := filepath.Join(scratch, "snap.resp")
	readyFile := filepath.Join(scratch, "snap.ready")
	valsFile := filepath.Join(scratch, "old.json")
	ob, _ := json.Marshal(map[string]any{"count": 7, "items": []string{"a", "b", "c"}, "nonDefault": "OLD-NON-DEFAULT"})
	_ = os.WriteFile(valsFile, ob, 0o644)

	pm := NewProcessManager(func(int) string { return bin })
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
		return nil, context.DeadlineExceeded
	}
	hook := &HydrationHook{Proto: proto, Capture: capture,
		RestoreEnv: func(gen int) []string {
			if gen >= 2 {
				return []string{"HYDRATE_STATE=" + st.Path(), "HYDRATE_SESSION=sess-watch", "HYDRATE_SCHEMA=demo.v1", "HYDRATE_APP=app-1",
					"HYDRATE_REPORT=" + reportFile, "HYDRATE_NONCE=" + nonce}
			}
			return nil
		}}
	pm.EnvFor = func(gen int) []string {
		if gen == 1 {
			return []string{"HYDRATE_VALUES=" + valsFile, "HYDRATE_SESSION=sess-watch", "HYDRATE_SCHEMA=demo.v1", "HYDRATE_APP=app-1",
				"HYDRATE_SNAPSHOT_REQ=" + reqFile, "HYDRATE_SNAPSHOT_RESP=" + respFile, "HYDRATE_SNAPSHOT_READY=" + readyFile}
		}
		return hook.RestoreEnv(gen)
	}

	builder := &okBuilder{}
	w, err := New(Config{ProjectDir: proj, DebounceMS: 1, Hydration: hook}, builder, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := pm.Shutdown(); e != nil {
			t.Errorf("shutdown: %v", e)
		}
	}()
	ctx := context.Background()

	// gen1 首次发布（冷启动：无旧服务，hook 跳过捕获，直接 start gen1）。
	if _, err := w.NotifyChange(ctx, 0, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if pm.Current() == nil || !pm.Current().Running() {
		t.Fatal("gen1 did not start serving")
	}
	oldPID := pm.Current().PID
	if w.LastPublishedGen() != 1 {
		t.Fatalf("gen1 not published: %d", w.LastPublishedGen())
	}
	// 子进程启动即写一次初始 report（restored=false, old values）；gen2 会覆盖为新报告。
	_ = os.Remove(reportFile)

	// gen2：变更 → runGeneration → hook.BeforeSwap(capture 旧 gen1) → stop-old/start-new（恢复）。
	if _, err := w.NotifyChange(ctx, 200, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 300); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() != 2 {
		t.Fatalf("gen2 not published (Watch hook chain failed): %d", w.LastPublishedGen())
	}
	if pm.Current() == nil || !pm.Current().Running() {
		t.Fatal("gen2 not serving")
	}
	newPID := pm.Current().PID
	if newPID == oldPID {
		t.Fatalf("PID did not change (%d)", oldPID)
	}
	// 父进程据**新进程自报**验证恢复（非重读父状态文件）。
	rep := readReportR2(t, reportFile)
	if int(rep["pid"].(float64)) != newPID {
		t.Fatalf("report pid=%v want %d", rep["pid"], newPID)
	}
	if rep["session"] != "sess-watch" || rep["nonce"] != nonce {
		t.Fatalf("report session/nonce mismatch: %v", rep)
	}
	if rep["restored"] != true || int(rep["gen"].(float64)) != 2 {
		t.Fatalf("new process did not restore via Watch chain: %v", rep)
	}
	if rep["nonDefault"] != "OLD-NON-DEFAULT" || int(rep["count"].(float64)) != 7 {
		t.Fatalf("restored value mismatch: %v", rep)
	}
	items, _ := rep["items"].([]any)
	if len(items) != 3 || items[0] != "a" || items[1] != "b" || items[2] != "c" {
		t.Fatalf("restored items mismatch: %v", rep["items"])
	}
	if gen, ok := pm.ServingGeneration(); !ok || gen != 2 {
		t.Fatalf("serving generation should be observable=2 after gen2: gen=%d ok=%v", gen, ok)
	}
}
