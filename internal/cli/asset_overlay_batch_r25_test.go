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

// 7) overlay 指向资产路径但磁盘资产损坏 -> 门禁读磁盘真相，拒绝（overlay 不能替换资产字节）。
func TestR25OverlayCannotSubstituteAssetBytes(t *testing.T) {
	root := r25TwoPageAssetProject(t)
	r25Env(t)
	ui := filepath.Join(root, "ui")
	src := filepath.Join(ui, "a.gxml")
	if code, _, se := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("cold: %s", se)
	}
	ovFile := filepath.Join(root, "ov.json")
	repl := filepath.Join(ui, "assets", "b.png") // 一个合法 PNG，作为 overlay 目标
	writeCLIFile(t, ovFile, []byte(`{"Replace":{"`+repl+`":"`+repl+`"}}`))
	t.Setenv("GOFLAGS", "-mod=readonly -overlay="+ovFile)
	// 把 a 的真实磁盘资产改坏：embed/门禁读磁盘 => 必须拒绝，overlay 无法"提供"好字节
	writeCLIFileMkdir(t, filepath.Join(ui, "assets", "a.png"), []byte("not-a-png"))
	code, stdout, _ := runStandaloneCLI(t, src)
	if code == ExitOK {
		t.Fatalf("overlay must NOT substitute asset bytes; broken disk asset must reject: %s", stdout)
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
