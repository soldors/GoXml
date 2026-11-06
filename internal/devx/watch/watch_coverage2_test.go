package watch

// R26-A 进一步真实路径：直接行使 realGoBuild（绝对 .go 文件构建，CWD 无关）、summarize 截断、
// Cancel 的“窗口等待中”分支、Running(nil)、PendingPaths 空/非空。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realGoBuild 真实执行：构建一个绝对路径的 .go 文件（consoleapp main.go），验证 rc0 与产物。
func TestRealGoBuildDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("skip real go build in -short")
	}
	root := repoRootForTest(t)
	src := filepath.Join(root, "internal", "devx", "watch", "testdata", "consoleapp", "main.go")
	if _, err := os.Stat(src); err != nil {
		t.Skip("consoleapp main.go missing")
	}
	out := filepath.Join(t.TempDir(), "cb2.exe")
	rc, outp := realGoBuild(context.Background(), []string{"go", "build", "-o", out, src})
	if rc != 0 {
		t.Fatalf("realGoBuild 构建单文件应 rc0，实际 rc=%d outp=%s", rc, outp)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("产物应存在: %v", err)
	}
}

// realGoBuild 失败路径：不存在的文件 -> 真实非零 rc（证明真实 rc 分类，而非哨兵）。
func TestRealGoBuildFails(t *testing.T) {
	rc, _ := realGoBuild(context.Background(), []string{"go", "build", filepath.Join(t.TempDir(), "nope.go")})
	if rc == 0 {
		t.Fatal("构建不存在文件应非零 rc")
	}
}

// summarize 截断：超长保留尾部（...+后 max 字节）。
func TestSummarizeTruncates(t *testing.T) {
	long := strings.Repeat("A", 5000) + "TAIL"
	got := summarize(long)
	if len(got) > 2005 {
		t.Fatalf("summarize 应截断到 ~2000，实际 %d", len(got))
	}
	if !strings.HasSuffix(got, "TAIL") {
		t.Fatal("截断应保留尾部")
	}
	if summarize("  short  ") != "short" {
		t.Fatal("summarize 应 trim 短文本")
	}
}

// Cancel 的“窗口等待中（未开始构建）”分支：取消清空待办，随后 Pump 不触发。
func TestCancelWhileWaitingWindow(t *testing.T) {
	d := NewDebouncer(100)
	d.Note(0, "a")
	d.Cancel() // 尚未 Pump 进入构建
	if got := d.Pump(200); got != 0 {
		t.Fatal("窗口等待中取消后不得再触发构建")
	}
}

// PendingPaths 空/非空 + Running(nil)。
func TestPendingAndRunningNil(t *testing.T) {
	d := NewDebouncer(100)
	if len(d.PendingPaths()) != 0 {
		t.Fatal("初始 PendingPaths 应空")
	}
	d.Note(0, "b")
	ps := d.PendingPaths()
	if len(ps) != 1 || ps[0] != "b" {
		t.Fatalf("PendingPaths 应含 b，实际 %v", ps)
	}
	var c *Child
	if c.Running() {
		t.Fatal("nil Child Running 应为 false")
	}
}
