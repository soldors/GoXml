package cli

// R15-A BUILD-INPUT-CACHE-SAFETY 判别回归。
//
// 核心：GOFLAGS 字符串不变，仅改 `-overlay` 指向的 JSON / 替换源文件，或 `-modfile`
// 指向的替代模块文件内容时，缓存必须**安全 miss**（K11）；普通工程仍**真命中**。
// 若移除 K11（BuildInputs），前三个断言变红；若 K11 误伤普通工程，则 plainTrueHit 变红。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func r15Project(t *testing.T, name string) string {
	t.Helper()
	root := standaloneCLIProject(t)
	_ = os.Remove(filepath.Join(root, "home.gxml"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "doc.go"), []byte("package ui\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "ui", "home.gxml"),
		[]byte(`<Goxml Version="1"><Page Route="/"><Text Value="r15"/></Page></Goxml>`))
	return root
}

// TestBuildInputsOverlayJSONChangeSafeMiss 只改 overlay JSON 内容 -> 安全 miss。
func TestBuildInputsOverlayJSONChangeSafeMiss(t *testing.T) {
	root := r15Project(t, "ovjson")
	writeCLIFileMkdir(t, filepath.Join(root, "alt", "a.go"), []byte("package ui\n\nvar A = 1\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "alt", "b.go"), []byte("package ui\n\nvar B = 2\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "ov.json"), []byte(`{"Replace":{"ui/doc.go":"alt/a.go"}}`))
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", `-mod=readonly "-overlay=ov.json"`)

	src := filepath.Join(root, "ui", "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("首次生成应成功: %s", stderr)
	}
	// 仅改 overlay JSON 指向 b.go（内容变化），GOFLAGS 字符串不变。
	writeCLIFileMkdir(t, filepath.Join(root, "ov.json"), []byte(`{"Replace":{"ui/doc.go":"alt/b.go"}}`))
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("二次应成功: %s", stderr)
	}
	if strings.Contains(stdout, "缓存命中") || !strings.Contains(stdout, "缓存未命中") {
		t.Fatalf("overlay JSON 内容变化必须安全 miss，stdout: %s", stdout)
	}
}

// TestBuildInputsOverlaySourceChangeSafeMiss 仅改 overlay 替换源文件内容 -> 安全 miss。
func TestBuildInputsOverlaySourceChangeSafeMiss(t *testing.T) {
	root := r15Project(t, "ovsrc")
	writeCLIFileMkdir(t, filepath.Join(root, "alt", "s.go"), []byte("package ui\n\nvar A = 1\n"))
	writeCLIFileMkdir(t, filepath.Join(root, "ov.json"), []byte(`{"Replace":{"ui/doc.go":"alt/s.go"}}`))
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", `-mod=readonly "-overlay=ov.json"`)

	src := filepath.Join(root, "ui", "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("首次生成应成功: %s", stderr)
	}
	writeCLIFileMkdir(t, filepath.Join(root, "alt", "s.go"), []byte("package ui\n\nvar A = 2\n"))
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("二次应成功: %s", stderr)
	}
	if strings.Contains(stdout, "缓存命中") || !strings.Contains(stdout, "缓存未命中") {
		t.Fatalf("overlay 替换源内容变化必须安全 miss，stdout: %s", stdout)
	}
}

// TestBuildInputsModfileChangeSafeMiss 仅改 -modfile 替代模块文件内容 -> 安全 miss。
func TestBuildInputsModfileChangeSafeMiss(t *testing.T) {
	root := r15Project(t, "modf")
	base := readCLIFile(t, filepath.Join(root, "go.mod"))
	writeCLIFileMkdir(t, filepath.Join(root, "alt.mod"), append(append([]byte{}, base...), []byte("\n// marker A\n")...))
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", `-mod=readonly "-modfile=alt.mod"`)

	src := filepath.Join(root, "ui", "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("首次生成应成功: %s", stderr)
	}
	writeCLIFileMkdir(t, filepath.Join(root, "alt.mod"), append(append([]byte{}, base...), []byte("\n// marker B\n")...))
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("二次应成功: %s", stderr)
	}
	if strings.Contains(stdout, "缓存命中") || !strings.Contains(stdout, "缓存未命中") {
		t.Fatalf("modfile 内容变化必须安全 miss，stdout: %s", stdout)
	}
}

// TestBuildInputsPlainProjectStillTrueHit 普通工程（无 overlay/modfile）仍真命中。
func TestBuildInputsPlainProjectStillTrueHit(t *testing.T) {
	root := r15Project(t, "plain")
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "-mod=readonly")
	src := filepath.Join(root, "ui", "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, src); code != ExitOK {
		t.Fatalf("首次生成应成功: %s", stderr)
	}
	gen := filepath.Join(root, "ui", "home_gen.go")
	before := readCLIFile(t, gen)
	st1, _ := os.Stat(gen)
	manBefore := readCLIFile(t, manifestPath(root))
	code, stdout, stderr := runStandaloneCLI(t, src)
	if code != ExitOK {
		t.Fatalf("二次应成功: %s", stderr)
	}
	if !strings.Contains(stdout, "缓存命中") {
		t.Fatalf("普通工程应真命中，stdout: %s", stdout)
	}
	st2, _ := os.Stat(gen)
	if string(readCLIFile(t, gen)) != string(before) || st1.ModTime() != st2.ModTime() {
		t.Fatal("真命中不得改动产物字节/mtime")
	}
	if string(readCLIFile(t, manifestPath(root))) != string(manBefore) {
		t.Fatal("真命中不得改动 Manifest")
	}
}

func TestBuildInputsGOENVOverlayChangeSafeMiss(t *testing.T) {
	root := r15Project(t, "goenv-overlay")
	writeCLIFileMkdir(t, filepath.Join(root, "alt", "original.go"), []byte("package alt\n"))
	replacement := filepath.Join(root, "replacement.go")
	writeCLIFile(t, replacement, []byte("package alt\nvar Number = 1\n"))
	writeCLIFile(t, filepath.Join(root, "with space.json"), []byte(`{"Replace":{"alt/original.go":"replacement.go"}}`))
	goenv := filepath.Join(t.TempDir(), "goenv")
	writeCLIFile(t, goenv, []byte("GOFLAGS=-mod=readonly \"-overlay=with space.json\"\n"))
	t.Setenv("GOENV", goenv)
	t.Setenv("GOFLAGS", "")
	src := filepath.Join(root, "ui", "home.gxml")
	if rc, _, stderr := runStandaloneCLI(t, src); rc != ExitOK {
		t.Fatalf("warm persisted overlay: rc=%d %s", rc, stderr)
	}
	if rc, stdout, stderr := runStandaloneCLI(t, src); rc != ExitOK || !strings.Contains(stdout, "缓存命中") {
		t.Fatalf("stable persisted overlay: rc=%d stdout=%s stderr=%s", rc, stdout, stderr)
	}
	writeCLIFile(t, replacement, []byte("package alt\nvar Number = 2\n"))
	rc, stdout, stderr := runStandaloneCLI(t, src)
	if rc != ExitOK || !strings.Contains(stdout, "缓存未命中") || strings.Contains(stdout, "缓存命中") {
		t.Fatalf("changed persisted overlay must miss: rc=%d stdout=%s stderr=%s", rc, stdout, stderr)
	}
}

func TestBuildInputsModfileSumFingerprint(t *testing.T) {
	root := t.TempDir()
	writeCLIFile(t, filepath.Join(root, "alternate.mod"), []byte("module example.com/alt\n"))
	before, ok := buildInputsFingerprint("-modfile=alternate.mod", root)
	if !ok {
		t.Fatal("an absent alternate.sum is valid")
	}
	writeCLIFile(t, filepath.Join(root, "alternate.sum"), []byte("\n"))
	after, ok := buildInputsFingerprint("-modfile=alternate.mod", root)
	if !ok || before == after {
		t.Fatal("adding the checksum file must change the fingerprint")
	}
}

// TestBuildInputsFingerprintUnit buildInputsFingerprint 的纯函数行为。
func TestBuildInputsFingerprintUnit(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ov.json"), `{"Replace":{"a.go":"b.go"}}`)
	mustWrite(t, filepath.Join(root, "b.go"), "package p\n")
	// 无引用 -> 空指纹、ok
	if fp, ok := buildInputsFingerprint("-mod=readonly", root); !ok || fp != "" {
		t.Fatalf("无 overlay/modfile 应返回空且 ok: %q %v", fp, ok)
	}
	// 有 overlay -> 非空、ok
	fp1, ok := buildInputsFingerprint(`-mod=readonly "-overlay=ov.json"`, root)
	if !ok || fp1 == "" {
		t.Fatalf("overlay 应产生指纹: %q %v", fp1, ok)
	}
	// 改替换源 -> 指纹变化
	mustWrite(t, filepath.Join(root, "b.go"), "package p\n\nvar X = 2\n")
	fp2, ok := buildInputsFingerprint(`-mod=readonly "-overlay=ov.json"`, root)
	if !ok || fp1 == fp2 {
		t.Fatalf("替换源变化应改变指纹: %q vs %q", fp1, fp2)
	}
	// 缺失替换源 -> ok=false（保守 miss）
	mustWrite(t, filepath.Join(root, "ov.json"), `{"Replace":{"a.go":"missing.go"}}`)
	if _, ok := buildInputsFingerprint(`-mod=readonly "-overlay=ov.json"`, root); ok {
		t.Fatal("缺失替换源应 ok=false（保守 miss）")
	}
	// 损坏 JSON -> ok=false
	mustWrite(t, filepath.Join(root, "ov.json"), `{not json`)
	if _, ok := buildInputsFingerprint(`-mod=readonly "-overlay=ov.json"`, root); ok {
		t.Fatal("损坏 overlay JSON 应 ok=false")
	}
}
