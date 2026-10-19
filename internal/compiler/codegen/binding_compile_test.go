package codegen

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// ============================================================================
// 步骤 1 验收：含绑定的生成代码必须可编译
//
// 为什么单独测：静态页面的编译验证已有 TestGeneratedCompiles，
// 但绑定页面引入了 vm 参数与跨包 import，是新的失败面。
// ============================================================================

// TestBindingGeneratedCompiles 把含绑定的生成代码放进仓库内临时包编译。
//
// **必须**在仓库内建包，不能建独立模块——原因已实测：
// Go 的 internal 规则禁止外部模块 import github.com/goxml/goxml/internal/...，
// 而测试用 ViewModel 恰在 internal/compiler/analyzer/testdata/fixture。
// 本仓库自身 go.mod 即 github.com/goxml/goxml，同模块内引用合法。
func TestBindingGeneratedCompiles(t *testing.T) {
	repoRoot := repoRootOf(t)

	v := verified(t, `<Column><Text Value="Static"/><Text Value="{Title}"/></Column>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	dir := filepath.Join(repoRoot, "internal", "compiler", "codegen",
		"tmpcompile"+randomSuffix())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "gen.go"), out, 0o644); err != nil {
		t.Fatal(err)
	}

	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "./"+filepath.ToSlash(rel)+"/...")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	bout, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("生成代码无法编译:\n%s\n%v", bout, err)
	}
	t.Log("含绑定的生成代码在本仓库模块内编译通过")
}

// TestBindingMustNotFreezeDynamic 是红线 2 的机器可验证形式：
// 含绑定的页面绝不能生成 op.Record / op.CallOp 固化逻辑。
//
// 一旦生成，绑定值变化后画面将永久陈旧（正确性优先于性能，红线 4）。
func TestBindingMustNotFreezeDynamic(t *testing.T) {
	v := verified(t, `<Text Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"op.Record", "op.CallOp", "cacheKey", "renderKey"} {
		if contains(s, bad) {
			t.Errorf("含绑定页面不应生成 %q（会把动态值固化）:\n%s", bad, s)
		}
	}
}

// TestStaticPageStillFreezes 对照：静态页面仍必须固化
// （不能因绑定改动而退化——静态缓存是 A1–A6 的成果）。
func TestStaticPageStillFreezes(t *testing.T) {
	v := verified(t, `<Text Value="Hello"/>`)
	out, err := Generate(v, defaultOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"op.Record", "op.CallOp", "cacheKey", "renderKey"} {
		if !contains(s, want) {
			t.Errorf("静态页面应保留 %q:\n%s", want, s)
		}
	}
}

// TestBindingOutputIsFormatted 确认生成代码已规范化
// （契约 §7：生成输出必须经 format.Source）。
func TestBindingOutputIsFormatted(t *testing.T) {
	v := verified(t, `<Text Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("生成代码不是合法 Go 源码: %v", err)
	}
	if string(formatted) != string(out) {
		t.Error("生成代码未规范化")
	}
}

// contains 判断是否包含子串。
func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// randomSuffix 生成短随机后缀，避免并行测试撞目录名。
func randomSuffix() string {
	return strconv.FormatInt(time.Now().UnixNano()%1000000, 10)
}
