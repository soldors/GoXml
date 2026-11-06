package cli

// R25-A IMAGE-OVERLAY-CACHE-SAFETY 判别回归（跨阶段一致性组合）。
//
// 目的：把已批准 ADR-0015 的资产门禁、CLI 缓存身份（K12：资产内容/拼写进键）、Go overlay
// 与批次提交放在**同一链路**上做组合断言，钉住"现行保证是否跨阶段一致"。基线未发现生产
// 违例（故不硬造生产修复）；本文件是比既有单点用例更有组合判别力的回归，若移除 K12
// （assetsFingerprint 不进键）或批次原子性退化，下列断言变红。
//
// 覆盖：
//   1) 冷生成 -> 真命中（产物字节/mtime/Manifest 皆不动）
//   2) 同路径内容变化 -> 安全 miss 并重新生成（新内容进新键）
//   3) 删除/损坏 -> 安全 miss -> 资产门禁拒绝，旧产物/Manifest 原样保留
//   4) 恢复原资产 -> 命中（与原内容同键）
//   5) --no-cache 冷批 + 单页坏资产 -> 整批拒绝且**不落任何产物**（a/b/Manifest 均不存在）
//   6) 暖批单页坏资产 -> 整批退出非0；兄弟页与坏页旧产物、Manifest 全部保持不变（保留成功者）
//   7) overlay 指向某资产路径但磁盘该资产被改坏 -> 门禁读磁盘真相 -> 拒绝（overlay 不能替换资产字节）
//   8) 跨包 --out-dir：资产以生成包目录为根，缓存身份沿用新目录（同一源改变 out-dir 仍需正确校验）

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

func r25PNG(t *testing.T, w, h int) []byte {
	t.Helper()
	return r16PNG(t, w, h)
}

func r25TwoPageAssetProject(t *testing.T) string {
	t.Helper()
	root := standaloneCLIProject(t)
	_ = os.Remove(filepath.Join(root, "home.gxml"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "doc.go"), []byte("package ui\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "a.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/a"><Image Src="assets/a.png"/><Text Value="a"/></Page></Goxml>`))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "b.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/b"><Image Src="assets/b.png"/><Text Value="b"/></Page></Goxml>`))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "assets", "a.png"), r25PNG(t, 4, 4))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "assets", "b.png"), r25PNG(t, 4, 4))
	return root
}

func r25Env(t *testing.T) {
	t.Helper()
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
}

// 1) 冷 -> 命中：产物字节/mtime/Manifest 不动。
func TestR25AssetColdThenHitStable(t *testing.T) {
	root := r25TwoPageAssetProject(t)
	r25Env(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "a.gxml")
	gen := filepath.Join(ui, "a_gen.go")
	if code, _, se := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("cold a: %s", se)
	}
	before := readCLIFile(t, gen)
	st1, _ := os.Stat(gen)
	manBefore := readCLIFile(t, manifestPath(root))
	code, stdout, se := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("hit a: %s", se)
	}
	if !strings.Contains(stdout, "缓存命中") {
		t.Fatalf("second run must be cache hit: %s", stdout)
	}
	st2, _ := os.Stat(gen)
	if string(readCLIFile(t, gen)) != string(before) || !st1.ModTime().Equal(st2.ModTime()) {
		t.Fatal("hit must not change artifact bytes/mtime")
	}
	if string(readCLIFile(t, manifestPath(root))) != string(manBefore) {
		t.Fatal("hit must not change Manifest")
	}
}

// 2) 同路径内容变化 -> 安全 miss 且可重新生成（新键）。
func TestR25AssetContentChangeRegenerates(t *testing.T) {
	root := r25TwoPageAssetProject(t)
	r25Env(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "a.gxml")
	if code, _, se := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("cold: %s", se)
	}
	writeCLIFileMkdir(t, filepath.Join(ui, "assets", "a.png"), r25PNG(t, 6, 6))
	code, stdout, se := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("content-change should regenerate: %s", se)
	}
	if strings.Contains(stdout, "缓存命中") {
		t.Fatalf("content change must be safe miss, stdout=%s", stdout)
	}
}

// 3) 删除/损坏 -> 安全 miss -> 拒绝且保旧产物/Manifest；4) 恢复 -> 命中。
func TestR25AssetDeleteCorruptRestore(t *testing.T) {
	root := r25TwoPageAssetProject(t)
	r25Env(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "a.gxml")
	gen := filepath.Join(ui, "a_gen.go")
	asset := filepath.Join(ui, "assets", "a.png")
	origAsset := r25PNG(t, 4, 4)

	if code, _, se := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("cold: %s", se)
	}
	before := readCLIFile(t, gen)
	manBefore := readCLIFile(t, manifestPath(root))

	for _, tc := range []struct {
		name string
		mut  func()
	}{
		{"delete", func() { _ = os.Remove(asset) }},
		{"corrupt", func() { writeCLIFileMkdir(t, asset, []byte("not-a-png")) }},
	} {
		tc.mut()
		code, stdout, _ := runStandaloneCLI(t, src)
		if code == ExitOK {
			t.Fatalf("%s: broken asset must be rejected", tc.name)
		}
		if strings.Contains(stdout, "缓存命中") {
			t.Fatalf("%s: broken asset must be safe miss (not cache hit): %s", tc.name, stdout)
		}
		if string(readCLIFile(t, gen)) != string(before) {
			t.Fatalf("%s: old artifact bytes must be preserved", tc.name)
		}
		if string(readCLIFile(t, manifestPath(root))) != string(manBefore) {
			t.Fatalf("%s: Manifest must be preserved on failure", tc.name)
		}
	}
	// restore
	writeCLIFileMkdir(t, asset, origAsset)
	code, stdout, se := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("restore should succeed: %s", se)
	}
	if !strings.Contains(stdout, "缓存命中") {
		t.Fatalf("restore to original bytes must hit cache (same key): %s", stdout)
	}
}

// 5) --no-cache 冷批 + 单页坏资产 -> 整批拒绝且不落任何产物。
func TestR25AssetBatchNoCacheBrokenAtomic(t *testing.T) {
	root := r25TwoPageAssetProject(t)
	r25Env(t)
	ui := filepath.Join(root, "ui")
	writeCLIFileMkdir(t, filepath.Join(ui, "assets", "b.png"), []byte("not-a-png"))
	code, stdout, _ := runStandaloneCLI(t, ui, "--no-cache")
	if code == ExitOK {
		t.Fatalf("cold broken batch must reject, stdout=%s", stdout)
	}
	for _, f := range []string{"a_gen.go", "b_gen.go"} {
		if _, err := os.Stat(filepath.Join(ui, f)); !os.IsNotExist(err) {
			t.Fatalf("broken cold batch must not write %s", f)
		}
	}
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Fatalf("broken cold batch must not write Manifest")
	}
}

// 6) 暖批单页坏资产 -> 退出非0；兄弟/坏页旧产物与 Manifest 全部保持。
func TestR25AssetBatchWarmMixedPreserves(t *testing.T) {
	root := r25TwoPageAssetProject(t)
	r25Env(t)
	ui := filepath.Join(root, "ui")
	if code, _, se := runStandaloneCLI(t, ui); code != ExitOK {
		t.Fatalf("warm batch: %s", se)
	}
	ga := readCLIFile(t, filepath.Join(ui, "a_gen.go"))
	gb := readCLIFile(t, filepath.Join(ui, "b_gen.go"))
	man := readCLIFile(t, manifestPath(root))
	writeCLIFileMkdir(t, filepath.Join(ui, "assets", "b.png"), []byte("not-a-png"))
	code, _, _ := runStandaloneCLI(t, ui)
	if code == ExitOK {
		t.Fatal("warm batch with broken asset must be non-zero")
	}
	if string(readCLIFile(t, filepath.Join(ui, "a_gen.go"))) != string(ga) {
		t.Fatal("sibling page a_gen.go must be preserved")
	}
	if string(readCLIFile(t, filepath.Join(ui, "b_gen.go"))) != string(gb) {
		t.Fatal("broken page b_gen.go old artifact must be preserved")
	}
	if string(readCLIFile(t, manifestPath(root))) != string(man) {
		t.Fatal("Manifest must be preserved on mixed-batch rejection")
	}
}

// ---- overlay helpers (R25-A repair F-A2) ----
//
// 用 encoding/json 生成 overlay JSON（避免反斜杠转义错误），并把整个 `-overlay=` flag
// 用双引号包起来（goFlagsSplit 只认"整 token 引号"，路径含空格时必需）。

func writeOverlayJSON(t *testing.T, root string, replace map[string]string) string {
	t.Helper()
	type ov struct {
		Replace map[string]string
	}
	b, err := json.Marshal(ov{Replace: replace})
	if err != nil {
		t.Fatalf("marshal overlay: %v", err)
	}
	p := filepath.Join(root, "overlay.json")
	writeCLIFile(t, p, b)
	return p
}

func goFlagsWithOverlay(t *testing.T, ovFile string) string {
	t.Helper()
	return "-mod=readonly \"-overlay=" + ovFile + "\""
}

// diskPNGAssetProject 造单页 home.gxml 引用 assets/a.png（磁盘为合法 PNG）。
func diskPNGAssetProject(t *testing.T) string {
	t.Helper()
	root := standaloneCLIProject(t)
	_ = os.Remove(filepath.Join(root, "home.gxml"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "doc.go"), []byte("package ui\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "home.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/"><Image Src="assets/a.png"/><Text Value="x"/></Page></Goxml>`))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "assets", "a.png"), r25PNG(t, 4, 4))
	return root
}

// 7a) 磁盘合法 PNG + overlay 把被引用的 a.png 映射到“坏字节” replacement：
// 修复后资产门禁读取**有效（overlay）字节** → CODEGEN-ASSET-002 拒绝，rc2 且不写产物。
// 且用真实 go build 的 Go 构造测试确认 Go 实际嵌入的是 overlay 字节（bad bytes → decode panic），
// 正面证明"校验字节==嵌入字节"这条判别成立。
func TestR25OverlayBadBytesRejected(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	diskAbs := filepath.Join(ui, "assets", "a.png")
	badRepl := filepath.Join(root, "bad.bin")
	writeCLIFile(t, badRepl, []byte("not-a-png"))
	ov := writeOverlayJSON(t, root, map[string]string{diskAbs: badRepl})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))

	code, stdout, stderr := runStandaloneCLI(t, src, "--check")
	if code == ExitOK {
		t.Fatalf("bad overlay effective bytes must be rejected (rc0 now), stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "CODEGEN-ASSET-002") {
		t.Fatalf("must fail with CODEGEN-ASSET-002 (effective bytes invalid), stderr=%s", stderr)
	}
	// --check 不得写产物
	if _, err := os.Stat(filepath.Join(ui, "home_gen.go")); !os.IsNotExist(err) {
		t.Fatalf("rejected --check must not write artifact")
	}
	// 真实 Go 嵌入有效性对照：生成“不受门禁保护”的同一 //go:embed 夹具，go build 后构造即 panic。
	// 这一步证明 Go 的确把 overlay 坏字节嵌入，故门禁必须按有效字节判。
	if panicked, msg := embedBadOverlayPanics(t, root, diskAbs, badRepl); !panicked || !strings.Contains(msg, "unknown format") {
		t.Fatalf("Go overlay should embed bad bytes causing decode panic; panicked=%v msg=%q", panicked, msg)
	}
}

// 7b) 正控制：overlay 把被引用 a.png 映射到另一个**合法 PNG** → 有效字节合法 → 接受（rc0）。
// 说明修复不是"一律拒绝 overlay"，而是按有效字节判。
func TestR25OverlayValidReplacementAccepted(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	diskAbs := filepath.Join(ui, "assets", "a.png")
	goodRepl := filepath.Join(root, "good-replacement.png")
	writeCLIFile(t, goodRepl, r25PNG(t, 8, 8))
	ov := writeOverlayJSON(t, root, map[string]string{diskAbs: goodRepl})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))
	code, stdout, stderr := runStandaloneCLI(t, src, "--check")
	if code != ExitOK {
		t.Fatalf("valid overlay replacement must be accepted, rc=%d stderr=%s", code, stderr)
	}
	_ = stdout
	// 真实 Go 构造测试：embed 的是合法 PNG → 构造成功不 panic。
	if panicked, msg := embedBadOverlayPanics(t, root, diskAbs, goodRepl); panicked {
		t.Fatalf("valid overlay bytes should construct fine, but panicked=%q", msg)
	}
}

// 7c) overlay 把被引用资产映射为“删除”（""）：有效字节缺失 → 拒绝（fail-closed）。
func TestR25OverlayDeleteRejected(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	diskAbs := filepath.Join(ui, "assets", "a.png")
	ov := writeOverlayJSON(t, root, map[string]string{diskAbs: ""})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))
	code, stdout, stderr := runStandaloneCLI(t, src, "--check")
	if code == ExitOK {
		t.Fatalf("overlay-deleted asset must reject (effective bytes absent), stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "CODEGEN-ASSET") {
		t.Fatalf("expected CODEGEN-ASSET diagnostic, stderr=%s", stderr)
	}
}

// 7d) overlay 有效字节变化 → 缓存键（K12 有效字节指纹）变化 → 安全 miss（不假命中）。
func TestR25OverlayEffectiveBytesEnterCacheKey(t *testing.T) {
	root := diskPNGAssetProject(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "home.gxml")
	diskAbs := filepath.Join(ui, "assets", "a.png")
	repl := filepath.Join(root, "repl.png")
	writeCLIFile(t, repl, r25PNG(t, 4, 4))
	ov := writeOverlayJSON(t, root, map[string]string{diskAbs: repl})
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", goFlagsWithOverlay(t, ov))

	// 直接对指纹函数断言：有效（overlay）字节进键。overlay 目标内容变化必须改变指纹。
	srcBytes := readCLIFile(t, src)
	overlay := map[string]string{filepath.Clean(diskAbs): repl}
	fp1, ok1 := assetsFingerprint("home.gxml", srcBytes, ui, overlay)
	if !ok1 || fp1 == "" {
		t.Fatalf("fingerprint with overlay must be present: ok=%v", ok1)
	}
	writeCLIFile(t, repl, r25PNG(t, 9, 9)) // 有效字节变
	fp2, ok2 := assetsFingerprint("home.gxml", srcBytes, ui, overlay)
	if !ok2 {
		t.Fatal("fingerprint second read failed")
	}
	if fp1 == fp2 {
		t.Fatalf("effective overlay bytes MUST enter the cache fingerprint; got equal fp (fix missing): %q", fp1)
	}
}

// 8) 跨包 --out-dir：资产根为生成包目录；指纹读取同一目标目录。改变目标目录下资产应触发 miss。
func TestR25AssetOutDirUsesTargetPackageDir(t *testing.T) {
	root := standaloneCLIProject(t)
	_ = os.Remove(filepath.Join(root, "home.gxml"))
	outDir := filepath.Join(root, "out", "ui")
	writeCLIFileMkdir(t, filepath.Join(outDir, "doc.go"), []byte("package ui\n"))
	src := filepath.Join(root, "src")
	writeCLIFileMkdir(t, filepath.Join(src, "home.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/"><Image Src="assets/logo.png"/></Page></Goxml>`))
	writeCLIFileMkdir(t, filepath.Join(outDir, "assets", "logo.png"), r25PNG(t, 4, 4))
	r25Env(t)
	srcFile := filepath.Join(src, "home.gxml")
	if code, _, se := runStandaloneCLI(t, srcFile, "--out-dir", outDir); code != ExitOK {
		t.Fatalf("cold out-dir: %s", se)
	}
	gen := filepath.Join(outDir, "home_gen.go")
	if _, err := os.Stat(gen); err != nil {
		t.Fatalf("out-dir generation missing: %v", err)
	}
	// 改变目标包目录下的资产内容 -> 指纹变 -> 安全 miss
	writeCLIFileMkdir(t, filepath.Join(outDir, "assets", "logo.png"), r25PNG(t, 6, 6))
	code, stdout, se := runStandaloneCLI(t, srcFile, "--out-dir", outDir)
	if code != ExitOK {
		t.Fatalf("out-dir content-change: %s", se)
	}
	if strings.Contains(stdout, "缓存命中") {
		t.Fatalf("asset change under target package dir must miss, stdout=%s", stdout)
	}
	_ = codegen.MaxAssetBytes
}
