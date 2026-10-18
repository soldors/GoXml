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
// 生成代码为每个 .gxml 产生一个实现该接口的类型，签名与
// 代码生成输出契约 §4.3 一致。
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

// RunWindow 与 RunWith 相同，但**不调用 app.Main()**，只驱动事件循环。
//
// 存在的唯一理由：让测试进程能够可控退出（契约 §3）。
// 收到 stop 信号后返回。**生产代码不得使用**——它不调用 app.Main()，
// 平台窗口可能不显示。
func RunWindow(view View, th *material.Theme, cfg Config, stop <-chan struct{}) error {
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

	// stop 到达时通知窗口关闭，从而让阻塞中的 w.Event() 返回。
	//
	// 不能只在循环里 select stop——w.Event() 会阻塞到下一个事件到来，
	// 无事件时循环永远回不到 select。
	go func() {
		<-stop
		w.Perform(system.ActionClose)
		// Invalidate 确保窗口产生一次帧事件，让 Event 循环有机会处理关闭。
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
