// Package app 提供把生成视图接入 Gio 主循环的最小运行入口。
//
// 职责（运行时入口契约 §1）：创建窗口、驱动事件循环、处理退出。
// 不做路由、生命周期、主题构建、热重载——那些属于 pkg/runtime/ 的其它包。
package app

import (
	"fmt"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// View 是可被渲染的页面。
//
// 生成代码为每个 .gxml 产生一个实现该接口的类型。静态页面与绑定页面
// 统一使用本接口（状态更新契约 §1、ADR-0010）：绑定页面在构造时持有
// ViewModel 引用，Layout 签名不因绑定而变化。
type View interface {
	Layout(gtx layout.Context, th *material.Theme) layout.Dimensions
}

// Config 是窗口配置。
type Config struct {
	// Title 是窗口标题。
	Title string
	// Width / Height 是初始逻辑尺寸（dp）。
	Width, Height unit.Dp
}

// 默认窗口参数。
const (
	defaultTitle  = "Goxml"
	defaultWidth  = unit.Dp(800)
	defaultHeight = unit.Dp(600)
)

// mainFunc 是 app.Main 的注入点。
//
// 存在的唯一理由：app.Main() 会接管主线程且无法返回，测试无法调用它。
// 测试替换为一个立即返回的空函数，从而覆盖 RunWith 的其余逻辑。
// 生产代码从不修改它。
var mainFunc = app.Main

// Invalidator 是状态变化后请求重新布局的入口（状态更新契约 §2）。
//
// 职责**只有**一个：请求窗口产生下一帧。它不修改任何状态、不感知
// ViewModel、不承诺每次调用对应一个独立帧（Gio 可合并连续失效）。
//
// 并发安全：实现必须可从任意 goroutine 调用（S0 源码证据：
// Gio 的 w.Invalidate() 标注 safe for concurrent use）。
//
// ⚠️ 失效通知的并发安全**不等于** ViewModel 字段读写的并发安全。
// 跨 goroutine 修改状态数据的同步由调用方负责。
type Invalidator interface {
	Invalidate()
}

// withDefaults 补齐零值。
func (c Config) withDefaults() Config {
	if c.Title == "" {
		c.Title = defaultTitle
	}
	if c.Width <= 0 {
		c.Width = defaultWidth
	}
	if c.Height <= 0 {
		c.Height = defaultHeight
	}
	return c
}

// Run 启动主循环，直到窗口关闭。
//
// 内部创建主题并传给 View.Layout。本函数阻塞，且必须调用 Gio 的
// app.Main()——该函数要求运行在主系统线程（契约 §4）。
func Run(view View, cfg Config) error {
	return RunWith(view, material.NewTheme(), cfg)
}

// RunWith 与 Run 相同，但允许注入主题。
//
// 注入主题的意义：主题可能带有字体集合等初始化成本，调用方可能希望
// 在多个窗口间复用，或在测试中传入受控主题。
func RunWith(view View, th *material.Theme, cfg Config) error {
	if view == nil {
		return fmt.Errorf("app: nil view")
	}
	if th == nil {
		return fmt.Errorf("app: nil theme")
	}
	cfg = cfg.withDefaults()

	w := new(app.Window)
	w.Option(
		app.Title(cfg.Title),
		app.Size(cfg.Width, cfg.Height),
	)

	// 事件循环放在 goroutine 中，主线程交给 app.Main()。
	go loop(w, view, th)

	// mainFunc 默认是 app.Main()：接管主线程，窗口关闭后返回。
	mainFunc()
	return nil
}

// loop 驱动事件循环直到窗口关闭。
func loop(w *app.Window, view View, th *material.Theme) {
	var ops op.Ops
	for {
		e := w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			// 窗口被销毁：循环结束。错误由调用方通过其它机制感知，
			// 此处不打印，避免在库代码里直接写 stderr。
			_ = e
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			view.Layout(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// RunWindowWithInvalidator 与 RunWindow 相同，另外把窗口的失效入口
// 交给调用方（状态更新契约 §2）。
//
// 存在的理由：状态变化后，持有 ViewModel 的 goroutine 需要一个
// 并发安全的「请求重新布局」入口。Runtime 不修改状态、不感知 vm——
// 它只把 Gio 的 Invalidate 交给持有状态的人。
//
// inv 为 nil 时等同 RunWindow（不交付失效入口）。
//
// 仍为**测试与集成**形态：不调用 app.Main()。生产代码用 Run/RunWith
// （S3 将在真实主循环形态中提供同一失效入口）。
func RunWindowWithInvalidator(view View, th *material.Theme, cfg Config, stop <-chan struct{}, inv func(Invalidator)) error {
	if view == nil {
		return fmt.Errorf("app: nil view")
	}
	if th == nil {
		return fmt.Errorf("app: nil theme")
	}
	if stop == nil {
		return fmt.Errorf("app: nil stop channel")
	}
	cfg = cfg.withDefaults()

	w := new(app.Window)
	w.Option(
		app.Title(cfg.Title),
		app.Size(cfg.Width, cfg.Height),
	)

	// 交付失效入口：在事件循环启动前回调，调用方持有它即可请求重布局。
	//
	// 注意：必须在 goroutine 之外同步回调，保证 inv 返回后窗口已就绪。
	if inv != nil {
		inv(w)
	}

	// stop 到达时通知窗口关闭。
	go func() {
		<-stop
		w.Perform(system.ActionClose)
		w.Invalidate()
	}()

	var ops op.Ops
	for {
		e := w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			return nil
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			view.Layout(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// RunWindow 与 RunWith 相同，但**不调用 app.Main()**，只驱动事件循环。
//
// 存在的唯一理由：让测试进程能够可控退出（契约 §3）。
// 收到 stop 信号后返回。**生产代码不得使用**——它不调用 app.Main()，
// 平台窗口可能不显示。
func RunWindow(view View, th *material.Theme, cfg Config, stop <-chan struct{}) error {
	return RunWindowWithInvalidator(view, th, cfg, stop, nil)
}
