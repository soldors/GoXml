package cli

// R25-A repair：真实 Go embed 有效性对照助手。构建一个临时 Go module，副本 //go:embed
// assets/a.png，使用调用方给的 overlay；go test 里构造并 decode 嵌入字节，捕获 panic。
// 用于证明"Go overlay 确实替换 //go:embed 字节"，从而资产门禁必须按有效字节判。
//
// 只用本机 go 工具链（无窗口、无用户应用、无网络：GOFLAGS=-mod=mod + 仅 stdlib）。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func goExeForTest() string {
	if p := os.Getenv("GXML_TEST_GO"); p != "" {
		return p
	}
	return "go"
}

// embedBadOverlayPanics 在临时 module 中 //go:embed 该资产，overlay 把磁盘资产映射到 repl，
// go test 构造 decode；返回 (是否 panic, 输出)。repl 为合法 PNG 时不应 panic。
func embedBadOverlayPanics(t *testing.T, root, diskAssetAbs, replAbs string) (bool, string) {
	t.Helper()
	mod := filepath.Join(t.TempDir(), "embedmod")
	if err := os.MkdirAll(filepath.Join(mod, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 磁盘原样放一份合法 PNG（diskAssetAbs 内容）到 module 内 assets/a.png
	diskBytes, err := os.ReadFile(diskAssetAbs)
	if err != nil {
		t.Fatalf("read disk asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mod, "assets", "a.png"), diskBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	writeGoFile := func(name, content string) {
		p := filepath.Join(mod, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeGoFile("go.mod", "module embedmod\n\ngo 1.26\n")
	writeGoFile("embed.go", "package embedmod\n\nimport \"embed\"\n\n//go:embed assets/a.png\nvar FS embed.FS\n")
	writeGoFile("embed_test.go", `package embedmod

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"testing"
)

func TestEffectiveDecode(t *testing.T) {
	data, err := FS.ReadFile("assets/a.png")
	if err != nil {
		t.Fatalf("read embed: %v", err)
	}
	if _, _, derr := image.Decode(bytes.NewReader(data)); derr != nil {
		panic("EFFECTIVE-ASSET-PANIC: " + derr.Error())
	}
}
`)
	// overlay JSON mapping module asset abs path -> replAbs
	ov := filepath.Join(mod, "ov.json")
	modAssetAbs := filepath.Join(mod, "assets", "a.png")
	ovBody := `{"Replace":{"` + filepath.ToSlash(modAssetAbs) + `":"` + filepath.ToSlash(replAbs) + `"}}`
	if err := os.WriteFile(ov, []byte(ovBody), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goExeForTest(), "test", "-count=1", "-run", "TestEffectiveDecode", "-overlay="+ov, "./...")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GO111MODULE=on", "GOWORK=off", "GOPROXY=off", "GOENV=off")
	out, rerr := cmd.CombinedOutput()
	s := string(out)
	panicked := strings.Contains(s, "EFFECTIVE-ASSET-PANIC") || strings.Contains(s, "unknown format")
	if rerr == nil && !panicked {
		return false, s
	}
	return panicked, s
}
