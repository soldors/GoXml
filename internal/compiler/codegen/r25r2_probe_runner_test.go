package codegen

// R25-R2 探针运行器：全源码形态（支持 Defs 在 Goxml 层）+ 可合并失效调度
// 之外的独立链路。与 rtProbeVM 同链路（Parser→Expander→Analyzer→Codegen→
// 写盘→go test），仅差：full source 直接给定、header 带 system 导入。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

func r25r2ProbeFull(t *testing.T, vmSrc, fullSrc, probeBody string) string {
	t.Helper()
	repoRoot := repoRootOf(t)

	base := "tmprt" + randomSuffix()
	dir := filepath.Join(repoRoot, "internal", "compiler", "codegen", "testdata", base)
	if err := os.MkdirAll(filepath.Join(dir, "vm"), 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "vm", "vm.go"), []byte(vmSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	rel := "internal/compiler/codegen/testdata/" + base
	fullSrc = strings.Replace(fullSrc, "__VMREF__", rel+"/vm.VM", 1)

	p := runFrontend(t, fullSrc)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		msgs := make([]string, 0, len(r.Diags))
		for _, d := range r.Diags {
			msgs = append(msgs, d.Error())
		}
		t.Fatalf("Analyzer 未通过: %s", strings.Join(msgs, " | "))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("取凭证失败: %v", err)
	}
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"),
		[]byte(fmt.Sprintf(mixedProbeHeaderTmpl, base)+probeBody), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-v", "-run", "TestProbe", "./"+filepath.ToSlash(rel))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	bout, err := cmd.CombinedOutput()
	return string(bout) + "\n[err=" + rtErr(err) + "]"
}
