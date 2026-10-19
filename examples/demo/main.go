// Command demo 演示 Goxml 的端到端链路：
//
//	.gxml → Parser → Expander → Analyzer → Codegen → 真实窗口
//	→ 可点击按钮（帧内失效）→ 后台 ticker（帧外失效）
//
// 页面由内嵌的 .gxml 源码走完整管线生成，再交给 pkg/runtime/app 渲染。
//
// 这是 demo，不是 CLI——没有目录发现、没有写盘、没有 watch。
//
// 交互部分（事件切片）：<Button Click="Increment"/> 由生成代码轮询
// widget.Clickable 并在 Layout 顶部执行处理器，随后请求下一帧。
// demo 的 demoView 与生成代码**同构**（见其注释）。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/internal/compiler/pipeline"
	"github.com/goxml/goxml/pkg/runtime/app"
)

// page 是演示页面：
//   - {Title} 由按钮点击更新（帧内事件 + InvalidateCmd）
//   - {Status} 由后台 ticker 更新（帧外 App.Invalidate）
const page = `<Goxml Version="1" ViewModel="demoVM">
  <Page Route="/">
    <Text Value="{Title}"/>
    <Text Value="{Status}"/>
    <Button Text="点我 +1" Click="Increment"/>
    <Button Text="加 10" Click="Add(10)"/>
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
// 失效通知（InvalidateCmd / App.Invalidate）并发安全 ≠ 字段读写并发安全，
// 因此这里用 mutex 保护；按钮处理器在 UI 线程、ticker 在后台 goroutine，
// 两者都走锁。
type demoVM struct {
	mu sync.Mutex
	// 绑定目标必须是**导出**字段（Analyzer 的 BIND-001 要求）。
	// 读取统一走 snapshot()，避免直接暴露未加锁的字段访问。
	Title  string
	Status string
	count  int
}

// Increment 是零参无返回值的合法事件处理器（EVENT-007 要求无返回值）。
func (m *demoVM) Increment() {
	m.mu.Lock()
	m.count++
	m.Title = fmt.Sprintf("点击次数：%d", m.count)
	m.mu.Unlock()
}

// Add 是**带参**事件处理器（契约 v2）：实参 10 由生成代码传入。
func (m *demoVM) Add(n int) {
	m.mu.Lock()
	m.count += n
	m.Title = fmt.Sprintf("点击次数：%d", m.count)
	m.mu.Unlock()
}

// SetStatus 由后台 ticker 调用。
func (m *demoVM) SetStatus(s string) {
	m.mu.Lock()
	m.Status = s
	m.mu.Unlock()
}

// snapshot 供 Layout 读取（同一次加锁内取两个字段，避免撕裂）。
func (m *demoVM) snapshot() (title, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Title, m.Status
}

// run 走完整管线并把结果接入窗口。
//
// demo 保持单进程、单编译单元：生成代码不写盘再编译（那需要外部 go build），
// 页面结构由真实管线产出并打印，视图实现与生成器按同一契约手写。
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

	// 打印生成结果：证明管线确实产出了源码（含 Button 事件接线）。
	fmt.Printf("--- 生成 %s ---\n%s\n", res.Artifact.OutputName, res.Artifact.Source)

	vm := &demoVM{Title: "点击次数：0", Status: "等待后台更新…"}
	view := newDemoView(vm)

	a, err := app.NewApp(view, material.NewTheme(), app.Config{
		Title:  "Goxml Demo — 可点击",
		Width:  640,
		Height: 360,
	})
	if err != nil {
		return err
	}

	// 后台 ticker：帧外失效路径（App.Invalidate）。
	go func() {
		start := time.Now()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for range tick.C {
			vm.SetStatus(fmt.Sprintf("已运行 %d 秒（后台 goroutine + App.Invalidate）",
				int(time.Since(start).Seconds())))
			a.Invalidate()
		}
	}()

	return a.Run()
}

// demoView 与生成代码同构（事件处理输出契约 §1/§2、ADR-0010）：
//
//   - 构造时持有 vm 引用；
//   - 每个 <Button> 一个 widget.Clickable 字段（跨帧地址不变）；
//   - Layout 两参，事件轮询在**一切绘制之前**；
//   - 处理器后请求下一帧（InvalidateCmd 是保险）。
//
// 不解析生成源码——那等于再写一个解释器，且会掩盖 Codegen 的真实职责。
type demoView struct {
	vm     *demoVM
	click0 widget.Clickable // 对应 Click="Increment"
	click1 widget.Clickable // 对应 Click="Add(10)"
}

// newDemoView 对应生成代码的构造语义：nil vm 明确拒绝（状态更新契约 §4）。
func newDemoView(vm *demoVM) *demoView {
	if vm == nil {
		panic("demo: nil ViewModel") // demo 内直接暴露配置错误
	}
	return &demoView{vm: vm}
}

// Layout 实现 app.View。
func (v *demoView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// 事件轮询置于一切绘制之前：保证同帧内所有节点看到同一份最新状态。
	for v.click0.Clicked(gtx) {
		v.vm.Increment()
		gtx.Execute(op.InvalidateCmd{})
	}
	for v.click1.Clicked(gtx) {
		v.vm.Add(10) // 带参事件：实参来自 .gxml 声明
		gtx.Execute(op.InvalidateCmd{})
	}

	title, status := v.vm.snapshot()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body1(th, title).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body2(th, status).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, &v.click0, "点我 +1").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, &v.click1, "加 10").Layout(gtx)
		}))
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
