package watchadapter_test

// R26-A A7/E2E（真实整链路）：真实 cli.Run generate --check + 真实 go build 相对入口 + 真实 console
// 子进程代次切换。用一个临时 module，通过 replace 指向本仓库，使 --check 的 go build 能解析依赖。
// 不启动 GUI；console 入口是自有最小 main（非用户业务）。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goxml/goxml/internal/devx/watch"
	"github.com/goxml/goxml/internal/devx/watchadapter"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeRealProject 建一个可 --check 通过的临时 gxml 工程：**复制本仓库完整 go.mod**（含全部
// require，使生成代码的 gio 等依赖可解析），改 module 名并 replace 自身到本仓库；复制 go.sum。
func makeRealProject(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	baseMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read repo go.mod: %v", err)
	}
	mod := strings.Replace(string(baseMod), "module github.com/goxml/goxml", "module example.com/watchdemo", 1)
	mod = strings.TrimRight(mod, "\n") + "\n\nreplace github.com/goxml/goxml => " + filepath.ToSlash(root) + "\n"
	writeFile(t, filepath.Join(dir, "go.mod"), mod)
	if b, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		writeFile(t, filepath.Join(dir, "go.sum"), string(b))
	}
	writeFile(t, filepath.Join(dir, "ui", "doc.go"), "package ui\n")
	writeFile(t, filepath.Join(dir, "ui", "home.gxml"),
		`<Goxml Version="1"><Page Route="/"><Text Value="hello"/></Page></Goxml>`)
	// 控制台入口：blocking console helper（引用 ui 包以触发编译、又不打开窗口；阻塞读 stdin 以存活
	// 到被 Watch 停止，用于观察真实进程代次切换）。
	writeFile(t, filepath.Join(dir, "cmd", "app", "main.go"),
		"package main\n\nimport (\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n\n\t_ \"example.com/watchdemo/ui\"\n)\n\nfunc main() {\n\tfmt.Printf(\"CONSOLEAPP pid=%d\\n\", os.Getpid())\n\t_, _ = io.Copy(io.Discard, os.Stdin)\n}\n")
	return dir
}

func goBin(t *testing.T) string { return "go" }

// buildConsole 真实 go build 生成 console 入口（用于进程切换观察）。
func buildConsole(t *testing.T, proj string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "app.exe")
	cmd := exec.Command(goBin(t), "build", "-o", out, "./cmd/app")
	cmd.Dir = proj
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build console: %v\n%s", err, b)
	}
	return out
}

// TestRealEndToEndGenerateBuildSwap 真实整链路：
// 真实 cli --check（合法页）-> 真实 go build -> 真实子进程启动；随后坏 GXML 时真实 --check 非0
// 不重启；恢复后成功。断言真实 generate/build 调用与真实子进程 PID 代次。
func TestRealEndToEndGenerateBuildSwap(t *testing.T) {
	if testing.Short() {
		t.Skip("skip real e2e in -short")
	}
	proj := makeRealProject(t)
	consoleBin := buildConsole(t, proj)

	// 真实 Generate：cli.Run generate --check（对 ProjectDir）。
	rb := watchadapter.NewRealBuilder(proj, "./cmd/app", "", "")
	// 覆盖 OutBinary 到真实 console（真实 go build 会重建，但为观察进程用真实构建产物）。
	rb.OutBinary = consoleBin
	pm := watch.NewProcessManager(func(gen int) string { return consoleBin })
	pm.StopTimeout = 3 * time.Second

	obsCount := 0
	w, err := watch.New(watch.Config{ProjectDir: proj}, rb, pm, obsFunc(func(gen int, res watch.GenerationResult, pub bool) {
		obsCount++
		t.Logf("OBS gen=%d ok=%v stage=%q pub=%v detail=%s", gen, res.OK, res.Stage, pub, res.Detail)
	}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 1) 初始真实 --check + build + 启动 console
	if _, err := w.NotifyChange(ctx, 0, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 120); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() == 0 {
		t.Fatalf("真实合法页应发布一代（--check/build 成功）")
	}
	if pm.Current() == nil || !pm.Current().Running() {
		t.Fatal("应启动真实 console 子进程")
	}
	gen1 := w.LastPublishedGen()
	pid1 := pm.Current().PID

	// 2) 坏 GXML：真实 --check 应失败，不重启（旧继续服务、代次不变）
	writeFile(t, filepath.Join(proj, "ui", "home.gxml"), `<Goxml Version="1"><Page Route="/"><Bogus/></Page></Goxml>`)
	if _, err := w.NotifyChange(ctx, 200, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 320); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() != gen1 {
		t.Fatalf("坏 GXML 不得发布新代，gen=%d want %d", w.LastPublishedGen(), gen1)
	}
	if pm.Current() == nil || pm.Current().PID != pid1 {
		t.Fatal("坏 GXML 时旧 console 必须继续服务（同 PID）")
	}

	// 3) 恢复：真实 --check 成功 -> 新代发布，PID 变化
	writeFile(t, filepath.Join(proj, "ui", "home.gxml"), `<Goxml Version="1"><Page Route="/"><Text Value="ok"/></Page></Goxml>`)
	if _, err := w.NotifyChange(ctx, 400, []string{"ui/home.gxml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Tick(ctx, 520); err != nil {
		t.Fatal(err)
	}
	if w.LastPublishedGen() == gen1 {
		t.Fatal("恢复后应发布新代")
	}
	if pm.Current().PID == pid1 {
		t.Fatal("恢复后 console PID 应变化（真实替换）")
	}
	// 收口
	if err := pm.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if pm.ActiveAfter() != 0 {
		t.Fatalf("收口后不得有幸存进程，实际 %d", pm.ActiveAfter())
	}
	_ = strings.TrimSpace("")
}

// obsFunc 适配 Observer。
type obsFunc func(gen int, res watch.GenerationResult, published bool)

func (f obsFunc) OnGeneration(gen int, res watch.GenerationResult, published bool) {
	f(gen, res, published)
}
