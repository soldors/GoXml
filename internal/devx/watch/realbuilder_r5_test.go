package watch

// R29-A R5：构建产物所有权（并行运行隔离二进制 + 默认路径兼容）。无窗口。

import (
	"path/filepath"
	"runtime"
	"testing"
)

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// 默认（无 OutDir）：R28 兼容路径 <ProjectDir>/.goxml/watch-bin-genN.exe。
func TestR5BinaryPathDefaultSharedCompat(t *testing.T) {
	b := &RealBuilder{ProjectDir: filepath.Join("C:", "proj")}
	got := b.BinaryPath(2)
	want := filepath.Join("C:", "proj", ".goxml", "watch-bin-gen2"+exeSuffix())
	if got != want {
		t.Fatalf("default binary path must stay R28-compatible: got %q want %q", got, want)
	}
}

// OutDir（本运行专属）：<OutDir>/watch-bin-genN.exe；不同运行 OutDir 不同 => 二进制互不覆盖。
func TestR5BinaryPathPerRunIsolated(t *testing.T) {
	base := filepath.Join("C:", "proj")
	a := &RealBuilder{ProjectDir: base, OutDir: filepath.Join(base, ".goxml", "run", "runAAA")}
	c := &RealBuilder{ProjectDir: base, OutDir: filepath.Join(base, ".goxml", "run", "runBBB")}
	ga1, gb1 := a.BinaryPath(1), c.BinaryPath(1)
	if ga1 == gb1 {
		t.Fatalf("distinct runs must use distinct gen-1 binaries: %q", ga1)
	}
	if filepath.Base(ga1) != "watch-bin-gen1"+exeSuffix() {
		t.Fatalf("per-run binary name wrong: %q", ga1)
	}
	// 同一运行内不同代次仍是不同文件（不覆盖）。
	if a.BinaryPath(1) == a.BinaryPath(2) {
		t.Fatal("gen1 and gen2 must differ within a run")
	}
}
