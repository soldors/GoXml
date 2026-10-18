// Command demo 演示 Goxml 的最小端到端链路：
//
//	.gxml → Parser → Expander → Analyzer → Codegen → 真实窗口
//
// 页面由内嵌的 .gxml 源码走完整管线生成，再交给 pkg/runtime/app 渲染。
//
// 这是 demo，不是 CLI——没有目录发现、没有写盘、没有 watch。
//
// 当前边界（运行时入口契约 §5）：
//   - Codegen 只支持 <Page> + 单个静态 <Text>；
//   - 窗口是否真的显示需人工确认，自动化只能验证「启动不崩溃」。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gioui.org/layout"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/internal/compiler/pipeline"
	"github.com/goxml/goxml/pkg/runtime/app"
)

// page 是演示页面。刻意保持最小：静态文本，无绑定、无事件。
const page = `<Goxml Version="1">
  <Page Route="/">
    <Text Value="Hello, Goxml"/>
  </Page>
</Goxml>`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

// run 走完整管线并把结果接到窗口。
func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	res := pipeline.Run("demo.gxml", []byte(page), pipeline.Config{
		ModuleRoot:  root,
		SourceDir:   root,
		PackageName: "main",
		SourcePath:  "demo.gxml",
	}, pipeline.Hooks{})
	if !res.OK() {
		return fmt.Errorf("编译失败:%s", diagSummary(res))
	}

	// 打印生成结果：证明管线确实产出了源码，而非 demo 里硬编码界面。
	fmt.Printf("--- 生成 %s ---\n%s\n", res.Artifact.OutputName, res.Artifact.Source)

	return app.RunWith(&demoView{text: "Hello, Goxml"}, material.NewTheme(), app.Config{
		Title:  "Goxml Demo",
		Width:  640,
		Height: 360,
	})
}

// demoView 与 Codegen 生成的视图等价。
//
// 真实链路中生成代码会被写盘并一起编译；demo 保持单进程，因此这里用
// **与生成器同一契约**的实现：生成的 Layout 方法体正是
// `return material.Body1(th, "Hello, Goxml").Layout(gtx)`。
//
// 不解析生成源码——那等于再写一个解释器，且会掩盖 Codegen 的真实职责。
type demoView struct{ text string }

// Layout 实现 app.View。
func (v *demoView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return material.Body1(th, v.text).Layout(gtx)
}

// moduleRoot 返回仓库根目录。
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod")
		}
		dir = parent
	}
}

// diagSummary 汇总诊断，便于失败时定位。
func diagSummary(r *pipeline.Result) string {
	var out string
	for _, d := range r.Diags {
		out += "\n  " + d.Code + ": " + d.Message
	}
	if out == "" {
		out = fmt.Sprintf(" status=%v", r.Status)
	}
	return out
}
