package preview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer 是可从 cmd.Stdout 写入、测试 goroutine 并发读的缓冲
// （exec 的 copy goroutine 与测试轮询并发访问，bytes.Buffer 非并发安全）。
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// snapshotDir 递归收集 dir 下全部文件的（相对路径 → sha256）。
// 「用户模块只读」的判据：预览前后快照逐字节一致。
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sha(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func requireSameSnapshot(t *testing.T, before, after map[string]string, dir string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("用户模块文件数变化（前 %d 后 %d）——预览必须只读：\n前 %v\n后 %v",
			len(before), len(after), keysOf(before), keysOf(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("用户模块文件 %s 被改动（前 %s 后 %s）——预览必须只读", k, v, after[k])
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestRunBuildsRealHost 正例：真实生成 + 隔离宿主 + 真实 go build。
//
// 判据（C2 核心）：
//   - PhaseBuilt，宿主 exe 存在且非空（**只构建，不运行原生窗口**）；
//   - 宿主目录里有所有权标记；
//   - 生成 View 落位字节与内存产物逐字一致；
//   - 资产（PNG/JPEG）有效字节与源逐字一致；
//   - 用户模块（.gxml、资产、无 .goxml 缓存目录）前后快照完全一致。
func TestRunBuildsRealHost(t *testing.T) {
	proj := copyProject(t)
	before := snapshotDir(t, proj)

	res, err := Run(Options{File: filepath.Join(proj, "static_basic.gxml"), WorkDir: proj})
	if err != nil {
		t.Fatalf("Run 操作故障: %v", err)
	}
	if !res.Built() {
		t.Fatalf("应到 PhaseBuilt，实到 %s（诊断 %v）", res.Phase, res.Diags)
	}
	defer func() {
		if err := res.Cleanup(); err != nil {
			t.Fatalf("清理宿主失败: %v", err)
		}
	}()

	// 宿主 exe：只构建不运行。
	st, err := os.Stat(res.HostExe)
	if err != nil || st.Size() == 0 {
		t.Fatalf("宿主 exe 缺失: stat=%v", err)
	}
	t.Logf("OBS 宿主 %d 字节 sha256=%s（只构建，未运行）", st.Size(), sha(readFile(t, res.HostExe)))

	// 所有权标记。
	tokenData, err := os.ReadFile(filepath.Join(res.HostDir, ownershipMarker))
	if err != nil || strings.TrimSpace(string(tokenData)) != res.hostToken {
		t.Fatalf("所有权标记缺失或不匹配: %v", err)
	}

	// 生成 View 落位字节一致。
	viewPath := filepath.Join(res.HostDir, "view", res.OutputName)
	if !bytes.Equal(readFile(t, viewPath), res.Source) {
		t.Fatal("宿主里的生成 View 与内存产物字节不一致")
	}

	// 资产有效字节与源一致（PNG 与 JPEG 两种批准格式）。
	for _, asset := range []string{"assets/logo.png", "assets/photo.jpg"} {
		src := readFile(t, filepath.Join(proj, filepath.FromSlash(asset)))
		got := readFile(t, filepath.Join(res.HostDir, "view", filepath.FromSlash(asset)))
		if !bytes.Equal(src, got) {
			t.Fatalf("资产 %s 宿主嵌入字节与有效源不一致（src sha=%s host sha=%s）", asset, sha(src), sha(got))
		}
		t.Logf("OBS 资产 %s 字节一致（%d 字节，sha256=%s）", asset, len(src), sha(src))
	}

	// 宿主 go.mod：module 行 + replace 指向 goxml 源码根。
	mod := readFile(t, filepath.Join(res.HostDir, "go.mod"))
	for _, want := range []string{
		"module " + hostModuleName,
		"require github.com/goxml/goxml v0.0.0",
		"replace github.com/goxml/goxml => ",
	} {
		if !strings.Contains(string(mod), want) {
			t.Fatalf("宿主 go.mod 缺少 %q:\n%s", want, mod)
		}
	}
	// 宿主 main：同源 View/字体策略 + Gio Main 与独立窗口关闭语义。
	mainSrc := string(readFile(t, filepath.Join(res.HostDir, "main.go")))
	for _, want := range []string{
		"github.com/goxml/goxml/pkg/runtime/app",
		hostModuleName + "/view",
		"gioapp.Main()",
		"runPreviewWindow(v, th, w.Event)",
		"return e.Err",
		"NewGoxmlStatic_BasicView()",
	} {
		if !strings.Contains(mainSrc, want) {
			t.Fatalf("宿主 main.go 缺少 %q:\n%s", want, mainSrc)
		}
	}

	// 用户模块只读：前后快照逐字节一致（含「不得新建 .goxml 缓存目录」）。
	requireSameSnapshot(t, before, snapshotDir(t, proj), proj)
}

// TestHostArrangementOverlayBytes 资产 overlay：用户 -overlay 把资产替换为
// 另一张合法 PNG 时，门禁校验的是 **overlay 有效字节**，宿主嵌入的也是
// 同一份有效字节（与现行管线口径一致，校验字节 ≠ 嵌入字节不可能发生）。
func TestHostArrangementOverlayBytes(t *testing.T) {
	proj := copyProject(t)
	// overlay 目标：另一张合法 PNG（不同内容，字节可区分）。
	alt := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			alt.Set(x, y, color.RGBA{R: 255, G: 80, B: 80, A: 255})
		}
	}
	var abuf bytes.Buffer
	if err := png.Encode(&abuf, alt); err != nil {
		t.Fatal(err)
	}
	altPath := filepath.Join(t.TempDir(), "alt.png")
	if err := os.WriteFile(altPath, abuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := map[string]string{
		filepath.Join(proj, "assets", "logo.png"): altPath,
	}

	res, err := Run(Options{
		File:         filepath.Join(proj, "static_basic.gxml"),
		WorkDir:      proj,
		AssetOverlay: overlay,
		SkipHost:     false,
	})
	if err != nil {
		t.Fatalf("Run 操作故障: %v", err)
	}
	if !res.Built() {
		t.Fatalf("应到 PhaseBuilt，实到 %s（诊断 %v）", res.Phase, res.Diags)
	}
	defer res.Cleanup()

	got := readFile(t, filepath.Join(res.HostDir, "view", "assets", "logo.png"))
	if !bytes.Equal(got, abuf.Bytes()) {
		t.Fatalf("overlay 下宿主应嵌入 overlay 有效字节（got sha=%s want sha=%s）", sha(got), sha(abuf.Bytes()))
	}
	// 磁盘原资产未被改动（overlay 只影响有效字节，不写用户文件）。
	disk := readFile(t, filepath.Join(proj, "assets", "logo.png"))
	if bytes.Equal(disk, abuf.Bytes()) {
		t.Fatal("用户磁盘资产不应被 overlay 改写")
	}
	// 未受 overlay 影响的 JPEG 仍按磁盘字节嵌入。
	jpg := readFile(t, filepath.Join(proj, "assets", "photo.jpg"))
	gotJpg := readFile(t, filepath.Join(res.HostDir, "view", "assets", "photo.jpg"))
	if !bytes.Equal(jpg, gotJpg) {
		t.Fatal("未受 overlay 影响的资产应按磁盘字节嵌入")
	}
}

// TestHostOwnershipGuards 所有权守卫：非自有目录拒绝清理。
//
// 覆盖：标记缺失 / token 不匹配 / 卷根 / 合法路径（可清理）。
// 「旧目录」形态 = 有标记但 token 是别人的（陈旧运行残留）。
func TestHostOwnershipGuards(t *testing.T) {
	// 无标记目录。
	bare := t.TempDir()
	if err := cleanupHost(bare, "some-token"); err == nil {
		t.Fatal("无所有权标记的目录不应被清理")
	}
	// 标记存在但 token 不匹配（旧目录/他人目录）。
	old := t.TempDir()
	if err := os.WriteFile(filepath.Join(old, ownershipMarker), []byte("stale-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cleanupHost(old, "my-token"); err == nil {
		t.Fatal("token 不匹配（旧目录）不应被清理")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("被拒绝清理的目录必须原样保留: %v", err)
	}
	// 合法自有目录：可清理。
	mine := t.TempDir()
	if err := os.WriteFile(filepath.Join(mine, ownershipMarker), []byte("my-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cleanupHost(mine, "my-token"); err != nil {
		t.Fatalf("自有目录应可清理: %v", err)
	}
	if _, err := os.Stat(mine); !os.IsNotExist(err) {
		t.Fatal("清理后目录应不存在")
	}
	// 空路径 / 卷根：结构性拒绝。
	if ownsHostDir("", "t") || ownsHostDir(`C:\`, "t") || ownsHostDir("/", "t") {
		t.Fatal("空路径/卷根不应被认领")
	}
}

// TestBuildFailureKeepsEvidence 构建失败：诊断带编译器输出、宿主目录
// 保留为证据（磁盘快照：go.mod/main.go/生成 View/资产都在）。
//
// 故障注入方式是受控的：往宿主目录写入一个故意编译失败的额外 .go 文件，
// 模拟「宿主编译失败」路径——验证的是预览的失败处理（有界、留证据），
// 不是 codegen 缺陷。
func TestBuildFailureKeepsEvidence(t *testing.T) {
	proj := copyProject(t)
	res, err := Run(Options{File: filepath.Join(proj, "static_basic.gxml"), WorkDir: proj, SkipHost: true})
	if err != nil || !res.OK() {
		t.Fatalf("生成阶段应成功: err=%v phase=%s diags=%v", err, res.Phase, res.Diags)
	}
	toolRoot, err := resolveToolRoot("")
	if err != nil {
		t.Fatal(err)
	}
	host, err := arrangeHost(res, toolRoot, Options{AssetOverlay: nil})
	if err != nil {
		t.Fatalf("宿主布置应成功: %v", err)
	}
	// 受控故障注入：坏 Go 文件。
	if err := os.WriteFile(filepath.Join(host.Dir, "broken_extra.go"),
		[]byte("package main\n\nfunc definitelyBroken() { this is not go }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, buildOut, buildErr := buildHost(host, Options{})
	if buildErr == nil {
		t.Fatal("注入坏文件后构建应失败")
	}
	if !strings.Contains(buildOut, "broken_extra.go") && !strings.Contains(buildOut, "syntax") {
		t.Fatalf("编译器输出应指向注入的坏文件:\n%s", buildOut)
	}
	// 证据保留：宿主目录与关键文件都在。
	for _, rel := range []string{"go.mod", "go.sum", "main.go", ownershipMarker,
		filepath.Join("view", res.OutputName), filepath.Join("view", "assets", "logo.png")} {
		if _, err := os.Stat(filepath.Join(host.Dir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("构建失败后证据 %s 缺失: %v", rel, err)
		}
	}
	t.Logf("OBS 构建失败证据保留在 %s（%d 字节编译器输出）", host.Dir, len(buildOut))
	// 失败目录仍可按所有权清理。
	if err := cleanupHost(host.Dir, host.Token); err != nil {
		t.Fatalf("失败后的证据目录应可按 token 清理: %v", err)
	}
}

// TestRunBuildFailureReturnsEvidencePath Run 级构建失败：Phase=build、
// HostDir 保留、错误信息带证据路径与编译器输出。
func TestRunBuildFailureReturnsEvidencePath(t *testing.T) {
	proj := copyProject(t)
	// 先正常布置一次拿到宿主目录……但 Run 内部布置，注入需在 build 前。
	// 这里用 ToolRoot 指向坏 go.mod 的方式制造真实构建失败：
	// 复制 goxml 源码根的 go.mod 但把 go 指令改成不存在的版本？
	// 不——更诚实的真实失败：ToolRoot 合法但 go.sum 被截断。
	toolRoot, err := resolveToolRoot("")
	if err != nil {
		t.Fatal(err)
	}
	broken := t.TempDir()
	// 复制 toolRoot 的 go.mod，但 go.sum 置为空 → 离线 readonly 构建失败。
	modData, err := os.ReadFile(filepath.Join(toolRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "go.mod"), modData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "go.sum"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// broken 不是合法 goxml 模块根（module 行相同但目录内容不同）——
	// resolveToolRoot 只认 module 行，可通过；构建时 go.sum 校验失败。

	res, runErr := Run(Options{
		File:     filepath.Join(proj, "static_basic.gxml"),
		WorkDir:  proj,
		ToolRoot: broken,
	})
	if runErr == nil {
		t.Fatalf("坏 ToolRoot 应导致构建失败（phase=%s）", res.Phase)
	}
	if res.Phase != PhaseBuild {
		t.Fatalf("Phase 应为 build，实得 %s", res.Phase)
	}
	if res.HostDir == "" || !res.KeptForEvidence {
		t.Fatalf("构建失败应保留宿主目录证据: HostDir=%q Kept=%v", res.HostDir, res.KeptForEvidence)
	}
	if !strings.Contains(runErr.Error(), res.HostDir) {
		t.Fatalf("错误应带证据路径 %q: %v", res.HostDir, runErr)
	}
	if st, err := os.Stat(res.HostDir); err != nil || !st.IsDir() {
		t.Fatalf("证据目录应保留: %v", err)
	}
	// 清理失败证据目录（测试收尾）。
	if err := res.Cleanup(); err != nil {
		t.Fatalf("证据目录应可按 token 清理: %v", err)
	}
}

// TestSupervisorLifecycle 执行控制：用自有 console helper 验证
// 启动/等待/超时/进程树收口/幂等停止。**不运行任何原生窗口宿主**。
func TestSupervisorLifecycle(t *testing.T) {
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("需要 PATH 上的 Go:", err)
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(t.TempDir(), "consolehelper.exe")
	build := exec.Command(goExe, "build", "-o", helper, "./internal/devx/preview/testdata/consolehelper")
	build.Dir = repo
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off", "GOTOOLCHAIN=local")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建 console helper 失败: %v\n%s", err, out)
	}

	t.Run("正常退出", func(t *testing.T) {
		r, err := StartHost(helper, "", "ready")
		if err != nil {
			t.Fatal(err)
		}
		exitErr, werr := r.Wait(10 * time.Second)
		if werr != nil || exitErr != nil {
			t.Fatalf("ready 应正常退出: exitErr=%v waitErr=%v", exitErr, werr)
		}
		// 幂等 Stop：已退出进程不报错。
		if err := r.Stop(); err != nil {
			t.Fatalf("对已退出进程 Stop 应幂等: %v", err)
		}
	})

	t.Run("超时收口", func(t *testing.T) {
		r, err := StartHost(helper, "", "sleep", "30")
		if err != nil {
			t.Fatal(err)
		}
		exitErr, werr := r.Wait(2 * time.Second)
		if werr == nil || exitErr != nil {
			t.Fatalf("sleep 30 应超时: exitErr=%v waitErr=%v", exitErr, werr)
		}
		if werr != ErrHostTimeout {
			t.Fatalf("超时应返回 ErrHostTimeout，实得 %v", werr)
		}
	})

	t.Run("进程树收口", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "child-marker.txt")
		// 就绪门闩：consolehelper tree 分支在 spawn 子进程之后才打印
		// READY；读到 READY 才开始计时，消除「宿主尚未 spawn 子进程就
		// 进入超时窗口」的时序前提（R28 全树阻断 2-A 判别结论）。
		out := &lockedBuffer{}
		r, err := StartHostWithOutput(helper, "", out, nil, "tree", "30", "--child-out", marker)
		if err != nil {
			t.Fatal(err)
		}
		readyDeadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(out.String(), "READY") {
			if time.Now().After(readyDeadline) {
				t.Fatalf("宿主未在就绪窗口内打印 READY: %q", out.String())
			}
			time.Sleep(50 * time.Millisecond)
		}
		exitErr, werr := r.Wait(2 * time.Second)
		if werr != ErrHostTimeout || exitErr != nil {
			t.Fatalf("tree 30 应超时: exitErr=%v waitErr=%v", exitErr, werr)
		}
		// 终止命令成功判据：超时类别下终止失败会并入错误链（werr 不再
		// 等于 ErrHostTimeout），这里再显式断言 StopError 为空，防止
		// 未来把终止失败静默化。
		if serr := r.StopError(); serr != nil {
			t.Fatalf("树终止应成功，终止失败被观测: %v", serr)
		}
		// 退出证明：树终止后宿主在有界窗口内回收，无泄漏。
		if !r.waitBriefly(5 * time.Second) {
			t.Fatal("树终止后宿主应在有界窗口内回收（真实退出证明）")
		}
		// 子进程 10 秒后写 marker；树收口成功则 marker 永不出现。
		// 等 11.5 秒（覆盖子进程的写入窗口 + 判别余量）再判：写入窗口
		// 必须 > 负载下整条终止路径耗时（实测可达 4.5s+，见
		// consolehelper 文件头注释），否则子进程在被终止前自然写出
		// marker，把「未及时终止」误判成泄漏。
		time.Sleep(11500 * time.Millisecond)
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("进程树应收口（子进程不应存活到写 marker）：stat=%v", err)
		}
	})
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
