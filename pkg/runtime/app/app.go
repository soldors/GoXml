// Package app 提供把生成视图接入 Gio 主循环的最小运行入口。
//
// 职责（运行时入口契约 §1）：创建窗口、驱动事件循环、处理退出。
// 不做路由、生命周期、热重载——那些属于 pkg/runtime/ 的其它包。
//
// 主题：本包**不构建**主题。默认主题取自 pkg/runtime/theme.NewTheme()；
// 调用方也可通过 RunWith / NewApp / RunWindow* 显式注入自己的主题
// （两条路径并存是有意的，见 Run 与 RunWith 的文档）。
package app

import (
	"fmt"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/theme"
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

// newThemeWithReport 是默认主题构建入口的注入点（与 mainFunc 同构）。
//
// 存在的唯一理由：默认主题来自 pkg/runtime/theme，而测试需要在**不启动
// 主循环**的前提下证明 Run 确实走了这个入口，而不是绕回
// material.NewTheme()。生产代码从不修改它。
//
// 用 `NewThemeWithReport` 而不是 `NewTheme`：Run 需要区分"主题构建成功"与
// "字体两条链路都失败"——后者必须返回错误（TASK-FONT-001-B / B6），
// 不能拿着一个装了默认 Shaper 的主题继续跑到 Layout 才暴露。
//
// 它只影响 Run（便捷入口）。RunWith / NewApp / RunWindow* 走显式注入，
// 与本变量无关——这是两条路径的行为差异，不可合并。
var newThemeWithReport = theme.NewThemeWithReport

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
// 默认主题由 pkg/runtime/theme 构建（路线 B：字体决策在运行时一侧），
// 传给 View.Layout。需要复用主题、注入受控主题或注入字体策略时，
// 改用 RunWith / NewApp——本函数**不会**把默认主题暴露给调用方。
//
// # 失败语义（TASK-FONT-001-B / B6）
//
// 默认主题构建失败（系统字体与嵌入兜底都不可用）时**返回错误**，不启动窗口：
// 错误包装了 `contracts.ErrFontNotFound`，可用 `errors.Is` 判定；
// 详细原因见 `theme.Diagnostics()`。这是规范 §1.2
// 「字体加载失败必须产生明确诊断，不得静默降级到空白渲染」的落地。
//
// 显式注入主题的入口（RunWith / NewApp / RunWindow*）**不受此影响**：
// 调用方已经提供了主题，运行时不再做字体决策。
//
// 本函数阻塞，且必须调用 Gio 的 app.Main()——该函数要求运行在主系统线程
// （契约 §4）。
func Run(view View, cfg Config) error {
	th, rep := newThemeWithReport()
	if rep == nil {
		// 没有诊断就等于没有证据：宁可报错也不假装成功。
		return fmt.Errorf("app: 默认主题构建未返回诊断报告")
	}
	if rep.Err != nil {
		return fmt.Errorf("app: 默认主题字体不可用: %w", rep.Err)
	}
	return RunWith(view, th, cfg)
}

// App 是生产形态的运行入口：先持有句柄，后阻塞驱动（ADR-0010 实现项）。
//
// 与 Run/RunWith 的关系：那两个是一行式便捷包装（内部 NewApp + Run）；
// 需要在后台 goroutine 触发重绘（ticker、网络回调、事件处理器）时，
// 用 NewApp 在 Run 之前拿到 *App，即可并发安全地调用 Invalidate。
//
// 典型形态：
//
//	a, err := app.NewApp(view, th, cfg)
//	if err != nil { ... }
//	go func() {                       // 后台工作线程
//	    for range time.Tick(time.Second) {
//	        vm.Set(...)               // 状态同步由调用方负责
//	        a.Invalidate()
//	    }
//	}()
//	a.Run()                           // 主线程阻塞
type App struct {
	w    *app.Window
	view View
	th   *material.Theme
}

// NewApp 校验参数并创建运行句柄。
//
// 平台窗口在首次 w.Event() 时才真正创建（S0 源码证据），因此本函数
// 不涉及显示服务器；错误只来自参数校验。
func NewApp(view View, th *material.Theme, cfg Config) (*App, error) {
	if view == nil {
		return nil, fmt.Errorf("app: nil view")
	}
	if th == nil {
		return nil, fmt.Errorf("app: nil theme")
	}
	cfg = cfg.withDefaults()

	w := new(app.Window)
	w.Option(
		app.Title(cfg.Title),
		app.Size(cfg.Width, cfg.Height),
	)
	return &App{w: w, view: view, th: th}, nil
}

// Invalidate 请求窗口产生下一帧（状态更新契约 §2）。
//
// 并发安全。本项目实测（S3）：事件循环启动前调用也安全——
// Gio 的 mayInvalidate 零值为 false，driver 未创建时静默跳过，不 panic。
//
// 职责边界：
//   - 只请求重绘，不修改任何状态、不感知 ViewModel；
//   - 不承诺每次调用对应一个独立帧（Gio 可合并连续失效）；
//   - ⚠️ 不等于 ViewModel 字段读写的并发安全——跨 goroutine 修改
//     状态数据的同步由调用方负责。
func (a *App) Invalidate() {
	a.w.Invalidate()
}

// Run 阻塞驱动主循环，直到窗口关闭。
//
// 必须在主系统线程调用（内部走 app.Main()，契约 §4）。
// 后台 goroutine 应在本调用之前启动，并使用 a.Invalidate() 请求重绘。
func (a *App) Run() error {
	go loop(a.w, a.view, a.th)
	mainFunc()
	return nil
}

// RunWith 与 Run 相同，但允许注入主题。
//
// 注入主题的意义：主题可能带有字体集合等初始化成本，调用方可能希望
// 在多个窗口间复用，或在测试中传入受控主题。
//
// 与 Run 的边界：**本函数不调用默认主题构建入口**（pkg/runtime/theme.NewTheme
// 只由 Run 使用）。注入的 `*material.Theme` 按原样传给 View.Layout，
// 不复制、不二次构建。
//
// 一行式形态：内部 NewApp + Run。需要在后台 goroutine 触发重绘时，
// 请改用 NewApp 拿句柄——RunWith 返回时阻塞已结束，句柄无处可取。
func RunWith(view View, th *material.Theme, cfg Config) error {
	a, err := NewApp(view, th, cfg)
	if err != nil {
		return err
	}
	return a.Run()
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
