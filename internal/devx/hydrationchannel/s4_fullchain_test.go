package hydrationchannel_test

// R29-A S4：全链验收（Windows 开发态，无原生窗口）。用 **S2 exampleapp**（自有 console）+ 真实 watch.New
// 驱动两代应用进程，断言新代 PID 与精确 DTO（取自子进程 APP_REPORT，父端不重读状态文件）。
// 另含：默认关闭（无 hook 行为不变）、构建失败保旧、协议失败（坏状态）拒绝、取消/退出清理、代次错配对照。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/goxml/goxml/internal/devx/hydrationchannel"
	"github.com/goxml/goxml/internal/devx/watch"
)

func buildExampleApp(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skip exampleapp build in -short")
	}
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "exampleapp.exe")
	if runtime.GOOS != "windows" {
		out = filepath.Join(t.TempDir(), "exampleapp")
	}
	buildArgs := []string{"build", "-o", out}
	if ov := os.Getenv("S4_EXAMPLE_OVERLAY"); ov != "" {
		buildArgs = append(buildArgs, "-overlay", ov)
	}
	buildArgs = append(buildArgs, "./internal/devx/hydrationchannel/exampleapp")
	cmd := exec.Command("go", buildArgs...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build exampleapp: %v\n%s", err, b)
	}
	return out
}

// S4 主链：gen1 非默认 DTO -> Watch opt-in 快照 -> 停旧 -> gen2 新 PID 恢复精确值（子自报）。
func TestS4FullChainTwoGenerations(t *testing.T) {
	bin := buildExampleApp(t)
	proj := t.TempDir()
	scratch := t.TempDir()
	valuesFile := filepath.Join(scratch, "old.json")
	reportFile := filepath.Join(scratch, "child.report.json")
	_ = os.WriteFile(valuesFile, []byte(`{"count":7,"items":["a","b","c"],"nonDefault":"OLD-ND"}`), 0o644)

	const token = "s4token"
	cfg := hydrationchannel.SessionConfig{ProjectDir: proj, SessionID: "s4", AppID: "app", Schema: "demo.v1", Token: token, CaptureTimeout: 5 * time.Second}
	sess, err := hydrationchannel.NewSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pm := watch.NewProcessManager(func(int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.Stderr = os.Stderr
	pm.EnvFor = func(gen int) []string {
		env := []string{"APP_PROJECT=" + proj, "APP_SESSION=s4", "APP_APPID=app", "APP_SCHEMA=demo.v1", "APP_TOKEN=" + token,
			"APP_REPORT=" + reportFile}
		if gen == 1 {
			env = append(env, "APP_GEN=1", "APP_VALUES="+valuesFile)
		} else {
			env = append(env, "APP_GEN=2")
		}
		return env
	}
	capture := func(ctx context.Context, oldGen int) ([]byte, error) {
		cl, ok := sess.NewClientFromPort(token)
		if !ok {
			return nil, errNoPort
		}
		return cl.Capture(ctx, oldGen)
	}
	hook := &watch.HydrationHook{Proto: sess.Proto(), Capture: capture}
	builder := &okBuilder{}
	w, err := watch.New(watch.Config{ProjectDir: proj, DebounceMS: 1, Hydration: hook}, builder, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pm.Shutdown() }()
	ctx := context.Background()

	if _, err := w.NotifyChange(ctx, 0, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() != 1 || pm.Current() == nil {
		t.Fatalf("gen1 not serving/published")
	}
	oldPID := pm.Current().PID
	_ = os.Remove(reportFile)

	if _, err := w.NotifyChange(ctx, 200, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 300); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() != 2 || pm.Current() == nil {
		t.Fatalf("gen2 not published: %d", w.LastPublishedGen())
	}
	newPID := pm.Current().PID
	if newPID == oldPID {
		t.Fatalf("PID must change")
	}
	rep := readJSON(t, reportFile)
	if int(rep["pid"].(float64)) != newPID {
		t.Fatalf("child report pid=%v want %d", rep["pid"], newPID)
	}
	if rep["restored"] != true || int(rep["gen"].(float64)) != 2 {
		t.Fatalf("new gen did not restore: %v", rep)
	}
	if rep["nonDefault"] != "OLD-ND" || int(rep["count"].(float64)) != 7 {
		t.Fatalf("restored DTO mismatch: %v", rep)
	}
	items, _ := rep["items"].([]any)
	if len(items) != 3 || items[0] != "a" || items[2] != "c" {
		t.Fatalf("restored items mismatch: %v", rep["items"])
	}
}

var errNoPort = os.ErrNotExist

// S4 对照：默认关闭（无 hook）——首次即发布 gen1，无状态文件、无端点在旧进程。
func TestS4DefaultOffUnchanged(t *testing.T) {
	bin := buildExampleApp(t)
	proj := t.TempDir()
	scratch := t.TempDir()
	reportFile := filepath.Join(scratch, "r.json")
	pm := watch.NewProcessManager(func(int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.Stderr = os.Stderr
	pm.EnvFor = func(gen int) []string {
		return []string{"APP_PROJECT=" + proj, "APP_SESSION=d", "APP_APPID=a", "APP_SCHEMA=demo.v1", "APP_TOKEN=t",
			"APP_REPORT=" + reportFile, "APP_GEN=1"}
	}
	w, err := watch.New(watch.Config{ProjectDir: proj, DebounceMS: 1}, &okBuilder{}, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pm.Shutdown() }()
	ctx := context.Background()
	_, _ = w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	_, _ = w.Tick(ctx, 100)
	if w.LastPublishedGen() != 1 {
		t.Fatalf("default-off should publish gen1")
	}
	// 默认关闭：Watch 未接 hook，不捕获/不恢复；gen1 进程自报 restored=false（示例应用自身可起服务，
	// 但 Watch 无 hook 故不会为换代捕获/恢复）。断言以 Watch 发布结果与子自报为准，不以目录存在性判定。
	rep := readJSON(t, reportFile)
	if rep["restored"] != false {
		t.Fatalf("default-off gen1 must not restore: %v", rep)
	}
}

// S4 对照：构建失败保旧——gen2 构建失败 -> 不切换，gen1 仍服务。
func TestS4BuildFailureKeepsOld(t *testing.T) {
	bin := buildExampleApp(t)
	proj := t.TempDir()
	sess, _ := hydrationchannel.NewSession(hydrationchannel.SessionConfig{ProjectDir: proj, SessionID: "b", AppID: "a", Schema: "demo.v1", Token: "t"})
	pm := watch.NewProcessManager(func(int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.EnvFor = func(int) []string {
		return []string{"APP_PROJECT=" + proj, "APP_SESSION=b", "APP_APPID=a", "APP_SCHEMA=demo.v1", "APP_TOKEN=t"}
	}
	hook := &watch.HydrationHook{Proto: sess.Proto(), Capture: func(context.Context, int) ([]byte, error) { return []byte(`{}`), nil }}
	fb := &failGenBuilder{failGen: 2}
	w, err := watch.New(watch.Config{ProjectDir: proj, DebounceMS: 1, Hydration: hook}, fb, pm, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pm.Shutdown() }()
	ctx := context.Background()
	_, _ = w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	_, _ = w.Tick(ctx, 100)
	pid1 := pm.Current().PID
	_, _ = w.NotifyChange(ctx, 200, []string{"ui/home.gxml"})
	_, _ = w.Tick(ctx, 300)
	if w.LastPublishedGen() != 1 || pm.Current().PID != pid1 {
		t.Fatalf("build failure must keep old gen1 serving")
	}
}

// S4 对照：协议失败（坏状态文件）——Restore 拒绝，不部分应用。
func TestS4ProtocolFailureRejected(t *testing.T) {
	proj := t.TempDir()
	sess, _ := hydrationchannel.NewSession(hydrationchannel.SessionConfig{ProjectDir: proj, SessionID: "p", AppID: "a", Schema: "demo.v1", Token: "t"})
	// 写坏状态文件。
	if _, e := sess.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"x":1}`), nil }, 1); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(sess.Store().Path())
	_ = os.WriteFile(sess.Store().Path(), append(raw, []byte("garbage")...), 0o644)
	called := false
	applied, err := sess.Restore(context.Background(), 0, &syncOwnerS4{}, func(context.Context, []byte) error { called = true; return nil })
	if err == nil || applied || called {
		t.Fatalf("bad state must reject without apply: applied=%v called=%v err=%v", applied, called, err)
	}
}

// S4 对照：取消/退出清理——gen1 进程 Shutdown 后 ActiveAfter=0。
func TestS4ShutdownCleanup(t *testing.T) {
	bin := buildExampleApp(t)
	proj := t.TempDir()
	pm := watch.NewProcessManager(func(int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.EnvFor = func(int) []string {
		return []string{"APP_PROJECT=" + proj, "APP_SESSION=c", "APP_APPID=a", "APP_SCHEMA=demo.v1", "APP_TOKEN=t", "APP_GEN=1"}
	}
	if err := pm.Swap(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := pm.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if pm.ActiveAfter() != 0 {
		t.Fatalf("survivors after shutdown: %d", pm.ActiveAfter())
	}
}

// failGenBuilder 让指定代次构建失败，其它成功。
type failGenBuilder struct{ failGen int }

func (b *failGenBuilder) Build(_ context.Context, gen int, _ []string) watch.GenerationResult {
	if gen == b.failGen {
		return watch.GenerationResult{OK: false, Stage: "build", Detail: "injected build failure", Gen: gen}
	}
	return watch.GenerationResult{OK: true, Gen: gen}
}

type syncOwnerS4 struct{}

func (syncOwnerS4) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
