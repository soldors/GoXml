package watchadapter

// R26-A repair-r3：adapter 工具身份 API 的真实（非镜像）测试——SetToolExe/ToolIdentity 生效、
// resolveToolExe 从 runtime.Caller 推导仓库根并构建工具一次（真实 go build），buildError 渲染。
// 不启动 GUI、不跑用户 main；仅构建自有被验工具。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestToolIdentityExplicitOverride(t *testing.T) {
	old, _ := ToolIdentity()
	defer SetToolExe(old) // 还原（可能为空，下次自动解析）
	exe := filepath.Join(t.TempDir(), "fake-tool.exe")
	SetToolExe(exe)
	got, _ := ToolIdentity()
	if got != exe {
		t.Fatalf("SetToolExe not honored: got %q want %q", got, exe)
	}
}

func TestResolveToolExeBuildsFromCallerRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("skip real tool build in -short")
	}
	// 清空显式覆盖，走 runtime.Caller 推导 + 真实构建。
	SetToolExe("")
	exe, root, err := resolveToolExe()
	if err != nil {
		t.Fatalf("resolveToolExe: %v", err)
	}
	t.Cleanup(func() { SetToolExe(""); _ = os.Remove(exe) })
	if root == "" || exe == "" {
		t.Fatalf("root/exe empty: root=%q exe=%q", root, exe)
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "goxml", "main.go")); err != nil {
		t.Fatalf("root does not contain cmd/goxml: %v", err)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("built tool exe missing: %v", err)
	}
}

func TestCLIGenerateReportsToolIdentityError(t *testing.T) {
	// 指向不存在的显式工具：CLIGenerate 应以启动/收口错误返回非零（不静默成功）。
	old, _ := ToolIdentity()
	defer SetToolExe(old)
	SetToolExe(filepath.Join(t.TempDir(), "no-such-tool"))
	rc, detail := CLIGenerate(context.Background(), t.TempDir())
	if rc == 0 {
		t.Fatalf("missing tool must not return rc0; detail=%s", detail)
	}
}

func TestBuildErrorRenders(t *testing.T) {
	e := &buildError{msg: "boom", err: os.ErrNotExist}
	if e.Error() == "" {
		t.Fatal("buildError.Error empty")
	}
}
