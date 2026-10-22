package cli

import (
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// TestPipelineHooksFiredOncePerFile：三个注入点都**仍然生效**，且每个文件恰好一次。
//
// 存在理由（C5 独立复审 P1-2）：`Run` 拆成 `Prepare` + `RunPrepared` 后，
// 预检（阶段零 C）会在写盘前跑一次前端、阶段一复用同一个 `Prepared`。
// 复审问的是「拆分后 Hooks 是否还生效」，而当时**只有 `Generate` 有断言**
// （cache_wire_test.go 的 b3CountCodegen 与 pipeline 包的错误注入），
// `Load` / `Analyze` 在 cli 层从未被断言过——等于没验。
//
// 计数本身就是判据：
//   - 三处计数都不为 0 → 三个钩子确实被调用（钩子没生效时计数为 0）；
//   - 每处都等于文件数 → 前端没有被跑第二次（复用失效时 Load/Analyze 翻倍，
//     而 Generate 仍是每文件一次——这正是「重复前端」故障的指纹，
//     单看 Generate 次数抓不到）。
//
// 变红方式（已实测，见 dev-log §7.6）：把阶段一改成重新 `pipeline.Prepare`
// 而不是取 `preparedFE[p]`，则 load/analyze 均为 4 而 gen 仍为 2。
func TestPipelineHooksFiredOncePerFile(t *testing.T) {
	root := tempProjectInRepo(t)
	copyFixture(t, root)                              // home.gxml（绑定页）+ vm.go
	writeGxml(t, root, "detail.gxml", staticPageGxml) // 第二个源 → 一次运行处理两个文件

	const wantFiles = 2

	var load, analyze, gen int
	orig := pipelineHooks
	pipelineHooks = pipeline.Hooks{
		// 钩子内部调用**真实实现**，否则计数有意义但生成路径没被覆盖。
		Load: func(cfg pipeline.Config) (*analyzer.TypeEnv, error) {
			load++
			return analyzer.NewTypeEnv(analyzer.Config{
				ModuleRoot: cfg.ModuleRoot,
				SourceDir:  cfg.SourceDir,
			})
		},
		Analyze: func(file string, doc *contracts.DocumentIR, schemas expander.SchemaRegistry, env *analyzer.TypeEnv) *analyzer.Result {
			analyze++
			return analyzer.Analyze(file, doc, schemas, env)
		},
		Generate: func(doc *analyzer.VerifiedDoc, opts codegen.Options) ([]byte, error) {
			gen++
			return codegen.Generate(doc, opts)
		},
	}
	t.Cleanup(func() { pipelineHooks = orig })

	// --no-cache：命中会跳过生成，计数就不再是「每文件一次」。
	if code, out, errs := b3Run(t, root, "--no-cache"); code != ExitOK {
		t.Fatalf("两文件应能生成（退出码 %d）\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	if load != wantFiles {
		t.Errorf("Hooks.Load 调用 %d 次，期望 %d（0 = 钩子未生效；>文件数 = 前端被跑了两次）",
			load, wantFiles)
	}
	if analyze != wantFiles {
		t.Errorf("Hooks.Analyze 调用 %d 次，期望 %d（>文件数说明预检与生成各跑了一遍前端）",
			analyze, wantFiles)
	}
	if gen != wantFiles {
		t.Errorf("Hooks.Generate 调用 %d 次，期望 %d", gen, wantFiles)
	}
}
