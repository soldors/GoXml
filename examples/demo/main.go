// Command demo 演示 Goxml 的最小端到端链路：
//
//	.gxml → Parser → Expander → Analyzer → Codegen → 真实窗口
//	→ 后台状态更新 → Invalidate → 自动刷新
//
// 页面由内嵌的 .gxml 源码走完整管线生成，再交给 pkg/runtime/app 渲染。
//
// 这是 demo，不是 CLI——没有目录发现、没有写盘、没有 watch。
//
// 动态部分（S3）：后台 ticker 每秒更新计数并请求失效，
// 演示「状态变化 → 失效 → 下一帧显示新值」的生产形态闭环。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/internal/compiler/pipeline"
	"github.com/goxml/goxml/pkg/runtime/app"
)

// page 是演示页面。绑定 {Title}：值由后台 goroutine 每秒更新。
const page = `<Goxml Version="1" ViewModel="demoVM">
  <Page Route="/">
    <Text Value="{Title}"/>
  </Page>
</Goxml>`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

// demoVM 是演示 ViewModel。
//
// ⚠️ 状态同步由调用方负责（状态更新契约 §2.3）：
// 失效通知（Invalidate）并发安全 ≠ 字段读写并发安全，
// 因此这里用 mutex 保护，UI 线程读取也走锁。
type demoVM struct {
	mu    sync.Mutex
	Title string
}

func (m *demoVM) Set(t string) {
	m.mu.Lock()
	m.Title = t
	m.mu.Unlock()
}

func (m *demoVM) Get() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Title
}

// run 走完整管线、编译生成代码语义等价的视图，并接入窗口。
//
// 说明：demo 保持**单进程、单编译单元**——生成代码不会被写盘再编译
// （那需要外部 go build）。页面结构由真实管线产出并打印，
// 视图实现与生成器按同一契约手写（见 demoView 注释）。
func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	// SourceDir 指向 demo 目录：同包 ViewModel（demoVM）在这里解析。
	res := pipeline.Run("demo.gxml", []byte(page), pipeline.Config{
		ModuleRoot:  root,
		SourceDir:   filepath.Join(root, "examples", "demo"),
		PackageName: "main",
		SourcePath:  "demo.gxml",
	}, pipeline.Hooks{})
	if !res.OK() {
		return fmt.Errorf("编译失败:%s", diagSummary(res))
	}

	// 打印生成结果：证明管线确实产出了源码，而非 demo 里硬编码界面。
	fmt.Printf("--- 生成 %s ---\n%s\n", res.Artifact.OutputName, res.Artifact.Source)

	vm := &demoVM{Title: "启动中…"}
	view := newDemoView(vm)

	a, err := app.NewApp(view, material.NewTheme(), app.Config{
		Title:  "Goxml Demo",
		Width:  640,
		Height: 360,
	})
	if err != nil {
		return err
	}

	// 后台 ticker：每秒更新状态并请求失效（生产典型形态）。
	go func() {
		n := 0
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for range tick.C {
			n++
			vm.Set(fmt.Sprintf("已运行 %d 秒（后台 goroutine 更新）", n))
			a.Invalidate()
		}
	}()

	return a.Run()
}

// demoView 与生成代码的运行形态同构（ADR-0010）：
//
//   - 构造时持有 vm 引用；
//   - Layout(gtx, th) 两参（统一 View 接口）；
//   - 布局体内从 vm 读取显示值。
//
// 不解析生成源码——那等于再写一个解释器，且会掩盖 Codegen 的真实职责。
type demoView struct{ vm *demoVM }

// newDemoView 对应生成代码的构造语义：nil vm 明确拒绝（状态更新契约 §4）。
func newDemoView(vm *demoVM) *demoView {
	if vm == nil {
		panic("demo: nil ViewModel") // demo 内直接暴露配置错误
	}
	return &demoView{vm: vm}
}

// Layout 实现 app.View。
func (v *demoView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return material.Body1(th, v.vm.Get()).Layout(gtx)
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
