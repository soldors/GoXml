package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// R11-B B3：输出与成本验收。
//
// - 生产输出洁净：生成 View 不含 staticRecs 验收仪表、不含 reflect。
// - 生成确定性：同输入两次 Generate 字节一致。
// - 同包两份生成 View 真实 build（rtProbeVM 编译即真实构建）。
// - 稳定 Layout-only AllocsPerRun=0 守卫（继续持有，防回归）。
// - 绝对 ns/op 对照（cached 回放 vs forced 每帧重录），仅工程对照。
// ============================================================================

// TestLocalStaticProductionOutputClean 生产输出洁净守卫：
// 生成 View 不含验收计数器（staticRecs）、不含 reflect、不含 fmt 调试输出。
func TestLocalStaticProductionOutputClean(t *testing.T) {
	v := verified(t, `<Column>
  <Text Value="{Title}"/>
  <Button Text="提交" Click="Increment"/>
  <Column>
    <Text Value="静态标题A"/>
    <Text Value="静态标题B"/>
  </Column>
</Column>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, banned := range []string{"staticRecs", "reflect.", "probeRecs"} {
		if strings.Contains(s, banned) {
			t.Fatalf("生产输出含禁用标识 %q", banned)
		}
	}
	if !strings.Contains(s, "staticOps0") || !strings.Contains(s, "staticCall0") {
		t.Fatalf("局部静态缓存本体字段缺失")
	}
}

// TestLocalStaticGenerationDeterministic 生成确定性：同输入两次 Generate
// 字节一致（契约 §13 的确定性承诺在局部静态子树形态下保持）。
func TestLocalStaticGenerationDeterministic(t *testing.T) {
	gxml := `<Column>
  <Text Value="{Title}"/>
  <Button Text="提交" Click="Increment"/>
  <Column>
    <Text Value="静态标题A"/>
    <Text Value="静态标题B"/>
  </Column>
</Column>`
	a, err := Generate(verified(t, gxml), bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(verified(t, gxml), bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("同输入两次生成字节不一致（%d vs %d 字节）", len(a), len(b))
	}
}

// TestLocalStaticTwoViewsSamePackageRealBuild 同包两份生成 View 真实 build：
// 两个不同 SourcePath 的生成文件落在同一 Go 包（类型名按源文件唯一，
// goxmlRenderKey<SourceA>/<SourceB> 不冲突），真实 go build 通过。
func TestLocalStaticTwoViewsSamePackageRealBuild(t *testing.T) {
	a := bindingSource("first_gen.go", "first.gxml")
	b := bindingSource("second_gen.go", "second.gxml")
	a.body = `<Column><Column><Text Value="island A"/></Column><Text Value="{Title}"/></Column>`
	b.body = `<Column><Column><Text Value="island B"/></Column><Text Value="{Title}"/></Column>`
	x, y := buildTwoSourcesTogether(t, a, b)
	for _, s := range []string{x, y} {
		if !strings.Contains(s, "staticOps0") || strings.Contains(s, "staticRecs") || strings.Contains(s, "probeRecs") {
			t.Fatal("bad production output")
		}
	}
}
