package expander_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// PRODUCT-R4-B 切片 2 全链：嵌套 Def、If 两分支与 Slot 组合经
// Parser→Expander→Analyzer→Codegen→真实 go build；重复生成字节确定。
// 外部测试包（expander_test）：pipeline 导入 expander，包内测试会成环。

const nestedComboSrc = `<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel">
  <Defs>
    <Def Name="OuterCard" Props="Greeting:string">
      <Column>
        <If Condition="{Dirty}">
          <Then><InnerRow Label="{Greeting}"><Text Value="slot-then"/></InnerRow></Then>
          <Else><InnerRow Label="fallback"><Text Value="slot-else"/></InnerRow></Else>
        </If>
      </Column>
    </Def>
    <Def Name="InnerRow" Props="Label:string">
      <For Each="item" In="{Items}" Key="{item.ID}">
        <Column>
          <Text Value="{Label}"/>
          <Text Value="{item.Name}"/>
          <Slot/>
        </Column>
      </For>
    </Def>
  </Defs>
  <Page Route="/"><OuterCard Greeting="hello"/></Page>
</Goxml>`

func TestNestedDefSlotFullChainBuild(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "internal", "compiler", "expander", "testdata")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "nestedbuild")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	cfg := pipeline.Config{
		ModuleRoot:  root,
		SourceDir:   filepath.Join(root, "internal", "compiler", "expander"),
		PackageName: "demo",
		SourcePath:  "nested.gxml",
	}
	res := pipeline.Run("nested.gxml", []byte(nestedComboSrc), cfg, pipeline.Hooks{})
	if len(res.Diags) != 0 {
		t.Fatalf("pipeline 诊断: %v", res.Diags)
	}
	if res.Artifact == nil {
		t.Fatal("合法组合失败未产出 Artifact（全或无被破坏）")
	}
	if err := os.WriteFile(filepath.Join(dir, "nested_gen.go"), res.Artifact.Source, 0o644); err != nil {
		t.Fatal(err)
	}
	res2 := pipeline.Run("nested.gxml", []byte(nestedComboSrc), cfg, pipeline.Hooks{})
	if len(res2.Diags) != 0 || res2.Artifact == nil || !bytes.Equal(res.Artifact.Source, res2.Artifact.Source) {
		t.Fatal("重复生成字节必须一致")
	}
	cmd := exec.Command("go", "build", "-p", "2", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成的嵌套组合未通过真实构建: %v\n%s", err, out)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("未找到 go.mod（仓库根）")
		}
		dir = parent
	}
}
