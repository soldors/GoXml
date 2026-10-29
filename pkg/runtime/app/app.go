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
	"sync/atomic"
	"time"

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

	// StrictFontFailure 只在**懒加载路径**（`NewLazyApp`）生效
	// （TASK-FONT-001-D-IMPL-3 §1）。
	//
	//	false（默认）：字体加载失败**不终止窗口**，UI 停留在临时字体，
	//	              失败原因从 `App.Lazy().Report().Err` 读取（含 ErrFontNotFound）。
	//	true：窗口照常运行（**不得**闪退、不得 panic），但 `Run()` 在窗口正常
	//	      关闭后返回包装了 `contracts.ErrFontNotFound` 的错误，
	//	      供 CI / 上层据此判定失败。
	//
	// 对同步路径（`NewApp` / `Run` / `RunWith` / `RunWindow*`）**无影响**：
	// 那两条链路的严格语义由 `Run` 的默认主题构建保证（B6），与本开关无关。
	// 两套语义各自独立，不得让一个开关同时改两条链路（卡 §1 禁止事项 3）。
	StrictFontFailure bool
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
//
// # 懒加载形态
//
// 由 `NewLazyApp` 构造的 App 额外持有 `*theme.Lazy`（字段 `lz`）。事件循环
// 与上面完全一致，只多一步"帧内取走待应用主题"（见 `frame` 的顺序注释）。
// `lz == nil` 时（NewApp / RunWith / RunWindow*），懒加载的每一步都被跳过，
// 因此既有入口的行为**逐字不变**（TASK-FONT-001-D-IMPL-2 §7-3）。
type App struct {
	w    *app.Window
	view View
	th   *material.Theme

	// lz 是懒加载句柄；nil 表示不走懒加载。只有 NewLazyApp 会填它。
	lz *theme.Lazy

	// Late binding needs a completion wake after the window has begun framing.
	// These fields belong to the event-loop goroutine; the waiter only captures
	// the callback and stop channel, and never touches rendering state.
	lateWake func()
	lateDone <-chan struct{}
	lateStop chan struct{}

	// inFrame 标记"当前是否处于 FrameEvent 的处理区间内"。
	//
	// 存在的理由（规则 R2）：主题替换**只能**发生在帧内、渲染线程。
	// 跨 goroutine 共用 shaper 的后果是 Go 运行时 fatal error
	// （recover 无效，D 卡 §4.3.2 实测 6/6 必现），靠"这次没崩"证明不了
	// 正确性。把它做成字段后，`swapPending` 在帧外**直接返回 false**——
	// 帧外替换从"靠测试抓的 bug"变成"类型层面不可达的分支"。
	//
	// 只由事件循环 goroutine 写入，但 notify 回调可能在另一 goroutine
	// 间接触发 `swapPending` 的读取，因此用 atomic 而非裸 bool。
	inFrame atomic.Bool

	// firstLayoutDone：切换后首次排版耗时是否已记录。
	//
	// 与 `Lazy.MarkFirstLayout` 内部的幂等互为双保险——方法保证"写不进去"，
	// 本字段保证"根本不调用"。只由事件循环 goroutine 读写，无需同步。
	firstLayoutDone bool

	// strict 是构造时快照的 `Config.StrictFontFailure`。
	//
	// 快照而不是每次读 cfg：`Config` 是值传递的入参，构造之后调用方改自己的
	// 副本不应影响运行中的窗口。
	strict bool

	// createdAt 是运行句柄构造的**入口**时刻（M1 起点，见 FirstFrameReport）。
	createdAt time.Time

	// firstFrameAt 是首帧开始处理的时刻（UnixNano，0 = 尚未有过帧）。
	//
	// 只由事件循环 goroutine 写入（唯一写入者），但可能在任意 goroutine 被读
	// （`FirstFrame()`），因此用 atomic 而非裸 time.Time。
	firstFrameAt atomic.Int64
}

// newWindow 按配置创建窗口句柄（不显示）。
//
// 平台窗口在首次 w.Event() 时才真正创建（S0 源码证据），因此本函数
// 不涉及显示服务器。抽出来的唯一理由：NewApp 与 NewLazyApp 必须用同一份
// 窗口装配口径，否则两条入口的窗口选项会各自漂移。
func newWindow(cfg Config) *app.Window {
	cfg = cfg.withDefaults()
	w := new(app.Window)
	w.Option(
		app.Title(cfg.Title),
		app.Size(cfg.Width, cfg.Height),
	)
	return w
}

// NewApp 校验参数并创建运行句柄。
//
// 平台窗口在首次 w.Event() 时才真正创建（S0 源码证据），因此本函数
// 不涉及显示服务器；错误只来自参数校验。
//
// M1 起点按 IMPL-3 §4.4 取**构造入口**：先记录 M1，再装配窗口。
// 不能写进 `&App{...}` 字面量——复合字面量字段按词法从左到右求值，
// `w: newWindow(cfg)` 会先于 `createdAt: time.Now()`，使起点晚于窗口装配。
func NewApp(view View, th *material.Theme, cfg Config) (*App, error) {
	// 入口快照：整条构造路径只取这一次时间。
	started := time.Now()

	if view == nil {
		return nil, fmt.Errorf("app: nil view")
	}
	if th == nil {
		return nil, fmt.Errorf("app: nil theme")
	}
	// StrictFontFailure 也会快照，但对同步路径不产生任何影响（见 fontFailure）。
	return &App{
		w:         newWindow(cfg),
		view:      view,
		th:        th,
		strict:    cfg.StrictFontFailure,
		createdAt: started,
	}, nil
}

// NewLazyApp 用**异步加载**的主题构造运行句柄（TASK-FONT-001-D-IMPL-2 §1.1）。
//
// 与 NewApp 的关系：NewApp 拿现成的 `*material.Theme`；本函数拿 `*theme.Lazy`
// （TASK-FONT-001-D-IMPL-1 的产出）。两条路径的不变量：
//
//   - **不接管** `theme.NewTheme()` 的默认构建路径（路线 B 的边界不变）——
//     `Run` 仍然走同步默认主题，本函数只服务于显式选择懒加载的调用方；
//   - 内部**先 `lz.SetNotify(...)` 再 `lz.Request()`**：顺序反了就没有
//     "通知哪个窗口"的答案（IMPL-1 §2），且 SetNotify 只接受 Request 之前
//     的首次绑定；
//   - **不等待**加载完成：首帧用临时主题渲染，真主题在后续帧内换上。
//
// # SetNotify 失败的处理（第 3 版：写入责任已收回 IMPL-1）
//
// `SetNotify` 返回 false（重复绑定 / 晚绑定 / nil）时，保留一次构造期
// `a.Invalidate()`；首帧开始后另观察当前加载尝试的完成信号，再请求重绘。
// 这覆盖窗口驱动尚未创建时失效被跳过的情形。`Report().NotifyLateBound`
// 由 `Lazy.SetNotify` 内部置位——本包对该字段**零命中**（`grep NotifyLateBound
// pkg/runtime/app/` 必须无结果），这是 IMPL-1 §9-4 的验收项之一。
func NewLazyApp(view View, lz *theme.Lazy, cfg Config) (*App, error) {
	// 入口快照：必须早于 newWindow 与 SetNotify / Request（后者会同步构造临时主题，
	// 按 §4.4 属构造路径的有界同步成本，应计入 M1）。
	started := time.Now()

	if view == nil {
		return nil, fmt.Errorf("app: nil view")
	}
	if lz == nil {
		return nil, fmt.Errorf("app: nil lazy")
	}

	// 先建壳、后填主题：通知回调需要 `*App` 才能 Invalidate，而回调可能在
	// `Request()` 之后的任意时刻（后台 goroutine）触发。若等到 `th` 就绪才
	// 构造 App，回调闭包就会读到尚未赋值的变量——那是数据竞争，不是时序问题。
	a := &App{w: newWindow(cfg), view: view, lz: lz, strict: cfg.StrictFontFailure, createdAt: started}

	// 回调里**只允许请求重绘**（规则 R2）：绝不在这里替换 shaper。
	bound := lz.SetNotify(func() { a.Invalidate() })

	// Request 返回后临时主题一定可用（IMPL-1 §1.1：首次调用内同步构造）。
	lz.Request()
	a.th = lz.Theme()
	if a.th == nil {
		return nil, fmt.Errorf("app: 懒加载未提供可用主题（Lazy.Theme() 为 nil）")
	}

	if !bound {
		a.lateWake = a.Invalidate
		a.lateStop = make(chan struct{})
		// Preserve the constructor compensation; completion is observed only once
		// a frame exists, so an App that never runs creates no waiting goroutine.
		a.Invalidate()
	}
	return a, nil
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
//
// # 返回值（TASK-FONT-001-D-IMPL-3 §1）
//
//   - 同步路径（`NewApp`，`lz == nil`）：**恒返回 nil**——字体语义由
//     `Run` 的默认主题构建保证，`Config.StrictFontFailure` 对它无影响；
//   - 懒加载路径（`NewLazyApp`）：`StrictFontFailure=true` 且字体加载进入
//     失败终态时，窗口**照常运行**（不闪退、不 panic），关闭后本函数返回
//     包装了 `contracts.ErrFontNotFound` 的错误；否则返回 nil。
//
// 读取时机：在 `mainFunc()`（即 `app.Main()`）返回之后——那时窗口已经关闭，
// 后台加载的终态也已确定。不得为了"早点知道结果"而在构造或首帧时同步等待
// 加载（卡 §1 禁止事项 2：那等于取消懒加载）。
func (a *App) Run() error {
	go a.loop()
	mainFunc()
	return a.fontFailure()
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
func (a *App) loop() {
	defer a.stopLateCompletion()
	var ops op.Ops
	for {
		e := a.w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			// 窗口被销毁：循环结束。错误由调用方通过其它机制感知，
			// 此处不打印，避免在库代码里直接写 stderr。
			_ = e
			return
		case app.FrameEvent:
			a.frame(&ops, e)
		}
	}
}

// watchLateCompletion runs on the event-loop goroutine. Each attempt creates at
// most one waiter; ordinary frames allocate nothing. The waiter only requests a
// frame, leaving Take and Shaper replacement on the event-loop goroutine.
func (a *App) watchLateCompletion() {
	if a.lateWake == nil {
		return
	}
	done := a.lz.Done()
	if done == a.lateDone {
		return
	}
	a.lateDone = done
	wake, stop := a.lateWake, a.lateStop
	go func() {
		select {
		case <-stop:
			return
		case <-done:
			select {
			case <-stop:
				return
			default:
				wake()
			}
		}
	}()
}

// Called by the event loop on exit; blocked loaders may outlive the window,
// but the App's completion waiter must not do so.
func (a *App) stopLateCompletion() {
	if a.lateStop != nil {
		close(a.lateStop)
		a.lateStop = nil
	}
}

// frame 处理一个 FrameEvent：懒加载替换 → 排版 → 提交。
//
// # 顺序冻结（TASK-FONT-001-D-IMPL-2 §1.2），不得调整
//
//	① 帧内标记置真
//	② Take()（只在帧内）→ 只换 Shaper
//	③ 排版（计时区间）
//	④ 记首次排版耗时（只记一次）
//	⑤ 提交
//	⑥ 记首次提交时刻（提交返回**之后**）
//
// 四条不许动的理由：
//
//  1. ② 必须在 FrameEvent 内：`th.Shaper` 非并发安全，替换与渲染必须在同一
//     goroutine；跨 goroutine 共用 shaper 是 Go 运行时 fatal error
//     （D 卡 §4.3.2）。
//  2. ② **只换 Shaper**：同时改 Palette / 字号会让"布局差"混入无关变量，
//     D6-6 的度量差就不可归因了。
//  3. ⑥ 必须在 ⑤ 之后：`e.Frame` 返回前只是"生成了绘制指令"；返回后才算
//     这一帧交给了 Gio 的呈现路径（规则 R1：加载完成 ≠ 已提交）。
//  4. ④ / ⑥ 只记首次：否则字段会随每帧漂移，变成一个没有意义的数字。
//
// 抽成方法的唯一理由：替换点必须唯一且可测。测试用合成 FrameEvent 直接
// 调用本方法，从而在不依赖图形会话的前提下验证上面六步。
func (a *App) frame(ops *op.Ops, e app.FrameEvent) {
	// M1 止点：第一个 FrameEvent **开始处理**的瞬间（`view.Layout` 之前）。
	// 只记第一次——首帧预算的语义是"到首帧处理开始为止"，后续帧不参与。
	// 唯一写入者是本 goroutine，因此先判 0 再写不会丢更新。
	if a.firstFrameAt.Load() == 0 {
		a.firstFrameAt.Store(time.Now().UnixNano())
	}
	a.watchLateCompletion()

	a.inFrame.Store(true)
	defer a.inFrame.Store(false)

	swapped, layoutStart := a.swapPending()

	gtx := app.NewContext(ops, e)
	a.view.Layout(gtx, a.th)

	if swapped && !a.firstLayoutDone {
		a.lz.MarkFirstLayout(time.Since(layoutStart))
		a.firstLayoutDone = true
	}

	e.Frame(gtx.Ops)

	if swapped {
		// 语义上界止于「Frame 已返回」：这是**提交**时刻的代理值，
		// 不是屏幕呈现时刻，任何报告都不得表述为"用户已看到新字体"。
		a.lz.MarkFrameSubmitted(time.Now())
	}
}

// swapPending 取走待应用的真主题并只替换 Shaper——**懒加载切换的唯一实现**
// （完成判据 §7-2：切换路径的赋值点必须唯一，且位于帧内分支）。
//
// 返回值：`swapped` 说明本帧是否发生了替换；`layoutStart` 是替换完成、
// 排版即将开始的时刻（未替换时为零值，调用方不得用它计时）。
//
// 帧外调用返回 `(false, 零值)`，且**不消耗** pending：把"帧外替换"从
// "靠测试抓的 bug"变成不可达的分支（见 `App.inFrame` 的注释）。
func (a *App) swapPending() (swapped bool, layoutStart time.Time) {
	if a.lz == nil || !a.inFrame.Load() {
		return false, time.Time{}
	}
	next := a.lz.Take()
	if next == nil {
		return false, time.Time{}
	}
	a.th.Shaper = next.Shaper
	return true, time.Now()
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
