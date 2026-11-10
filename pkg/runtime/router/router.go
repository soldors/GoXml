// Package router 实现单窗口路由核心（TASK-ROUTER-001 阶段 B1）。
//
// 职责边界（运行时入口契约 §1：路由属本包，app 不做路由）：
//   - 显式注册「精确路径 → 页面构造函数」，不支持通配/参数/规范化；
//   - 页面导航状态管理——Router 状态由 UI 线程拥有（线程模型 §1）；
//   - 页面 Context 的创建与切换取消（线程模型 §5/§7）；
//   - Dispatcher 语义：Dispatch / Invalidate / Context（线程模型 §4）。
//
// B1 明确不做（见授权 §2）：转场快照与动画（B2）、OSWindow（B3）、
// codegen 路由表、多窗口、OnMount/OnUnmount 回调（TASK-LIFE-001）。
//
// # 线程约定
//
// UI 线程 = 处理 FrameEvent 的 goroutine，即根 View.Layout 的调用方
// （宿主用 app.Run/RunWith/NewApp 驱动时即 Gio 事件循环 goroutine）。
//
//   - 可从任意 goroutine 调用：Navigate / Dispatch / Invalidate
//     （线程模型 §7：路由切换请求可以来自后台线程，但必须通过
//     Dispatcher 排队）；后台调用其余方法违反线程模型 §9（P0）。
//   - 内部状态全部互斥保护：误用不会产生数据竞争，但语义不受支持。
//
// # 与 pkg/runtime/app 的关系
//
// 本包不导入 pkg/runtime/app。View / Invalidator 按入口契约 §2 的冻结
// 签名在本包重新声明——Go 接口为结构化类型，*app.App 与 *app.Window
// 可直接作为 Invalidator 传入，app.Run/RunWith/NewApp 可直接接收
// Router.View() 的返回值。
package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"gioui.org/layout"
	"gioui.org/widget/material"
)

var (
	// ErrPathRegistered 表示重复注册（不覆盖既有注册）。
	ErrPathRegistered = errors.New("router: path already registered")
	// ErrPathNotRegistered 表示未注册路径（精确匹配失败）。
	ErrPathNotRegistered = errors.New("router: path not registered")
)

// View 与 pkg/runtime/app 的 View（运行时入口契约 §2 冻结签名）结构化等价。
type View interface {
	Layout(gtx layout.Context, th *material.Theme) layout.Dimensions
}

// Invalidator 与 app.Invalidator（状态更新契约 §2）结构化等价：
// 只请求重新布局，不修改状态；不承诺每次调用对应一个独立帧。
type Invalidator interface {
	Invalidate()
}

// PageFactory 在 UI 线程上构造页面 View。
//
// ctx 是页面 Context：由 Router 从自身 Context 派生，页面切换时取消
// （线程模型 §5/§7）。页面可把 ctx 交给自己的后台任务；任务必须遵守
// 「Context 取消后停止或忽略结果」的约定。
//
// 返回 error 表示页面创建失败：本次导航整体放弃——旧页面保持运行
// （Context 不取消），Router 记录 NavError。不存在半提交状态。
//
// 调用约束：工厂在帧内的 UI 线程执行，**不得阻塞**（线程模型 §1.5；
// R17-B candidateRef：F-07 注释补齐）。
type PageFactory func(ctx context.Context) (View, error)

// Router 是单窗口路由核心。零值不可用，用 New 构造。
type Router struct {
	mu sync.Mutex

	registry map[string]PageFactory

	// pendingNav 是尚未在 UI 线程应用的最新导航请求；同帧多次导航
	// 合并为最后一个（线程模型 §4「多个更新可以在同一帧合并」），
	// 被合并的中间页面不会被构造（避免重复生命周期）。
	// 「是否有待定导航」由 pendingNav=="" 承载（空路径不可注册，router.go
	// Register 已拒绝 ""——R17-B candidateRef：删除 F-01 死字段）。
	pendingNav string

	// current / page 仅在 UI 线程（drain 中）写入；读取经 mu 保护，
	// 供诊断与测试使用。current 的变更只发生在 applyNavigation 成功后。
	current    string
	page       View
	pageCancel context.CancelFunc

	// navErr 是最近一次异步导航失败原因；成功导航会清空它。
	navErr error

	// queue 是 Dispatcher 队列；帧首排空（线程模型 §3）。
	queue []func()

	// inv 由 SetInvalidator 恰好设置一次；nil 表示尚未接线。
	inv Invalidator

	// dropped 统计 Router Context 取消后被丢弃的请求（线程模型 §4：
	// 「Dispatcher 关闭后，新任务必须返回取消错误或被丢弃并记录诊断」）。
	dropped atomic.Int64

	// rootCtx 是 Router 级 Context，页面 Context 由它派生。
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// life 为 nil 表示未 opt-in 生命周期（R28-C）。所有新增行为都只在
	// 该分支发生；未开启时既有行为、锁序与计数合同逐字不变。
	life *lifecycleBridge
}

// New 创建路由核心。
func New() *Router {
	ctx, cancel := context.WithCancel(context.Background())
	return &Router{
		registry:   make(map[string]PageFactory),
		rootCtx:    ctx,
		rootCancel: cancel,
	}
}

// Register 注册 path → factory。UI 线程专用（后台误用在语义上不受支持）。
//
//   - path 为精确匹配键：区分大小写、不忽略尾部斜杠、不做任何规范化
//     （授权 §1.2）；
//   - 重复注册返回 ErrPathRegistered，不覆盖；
//   - 空路径与 nil factory 是配置错误，直接拒绝。
func (r *Router) Register(path string, f PageFactory) error {
	if path == "" {
		return fmt.Errorf("router: 空路径不可注册")
	}
	if f == nil {
		return fmt.Errorf("router: path %q 的页面构造函数为 nil", path)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.registry[path]; dup {
		return fmt.Errorf("%w: %q", ErrPathRegistered, path)
	}
	r.registry[path] = f
	return nil
}

// Navigate 请求导航到 path。**可从任意 goroutine 调用**——这是后台线程
// 修改路由状态的唯一合法入口（线程模型 §7）。
//
// 同步语义：
//   - path 未注册 → ErrPathNotRegistered（注册表互斥保护；v1 无反注册，
//     故不存在校验后失效的竞态）；
//   - path == 当前路径 → no-op（授权 §4.7 的确定语义），同时撤销任何
//     未应用的待定导航（「最后一次请求胜出」的一致推论）；
//   - 其余 → 记为待定导航（同帧合并）并 Invalidate。
//
// 异步语义：待定导航在下一帧由 UI 线程应用；页面创建失败时本次导航
// 整体放弃并记录 NavError（见 PageFactory）。
func (r *Router) Navigate(path string) error {
	r.mu.Lock()
	if err := r.rootCtx.Err(); err != nil {
		r.mu.Unlock()
		r.dropped.Add(1)
		return fmt.Errorf("router: 已关闭: %w", err)
	}
	_, ok := r.registry[path]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrPathNotRegistered, path)
	}
	if path == r.current {
		r.pendingNav = ""
		r.mu.Unlock()
		return nil
	}
	r.pendingNav = path
	inv := r.inv
	r.mu.Unlock()
	if inv != nil {
		inv.Invalidate()
	}
	return nil
}

// Current 返回当前路径（初始为 ""；导航请求入队不改变它，帧内应用后才变）。
// 语义上属于 UI 线程查询；实现经 mu 保护，误读不会产生数据竞争。
func (r *Router) Current() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

// NavError 返回最近一次异步导航失败原因；nil 表示无（成功导航会清空）。
// 诊断用途；Navigate 的同步校验错误直接由 Navigate 返回，不经此通道。
func (r *Router) NavError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.navErr
}

// DroppedCount 返回 Router Context 取消后被丢弃的请求数（诊断）。
func (r *Router) DroppedCount() int64 { return r.dropped.Load() }

// Dispatch 把 fn 排入 Dispatcher 队列，在下一帧的 UI 线程执行。
// 可从任意 goroutine 调用；多个 fn 按 FIFO 全部执行（与导航请求的
// 「合并到最后一个」不同——任意函数不能安全合并）。
// Router Context 取消后：丢弃并入 dropped 计数（线程模型 §4）。
func (r *Router) Dispatch(fn func()) {
	if fn == nil {
		return
	}
	r.mu.Lock()
	if r.rootCtx.Err() != nil {
		r.mu.Unlock()
		r.dropped.Add(1)
		return
	}
	r.queue = append(r.queue, fn)
	inv := r.inv
	r.mu.Unlock()
	if inv != nil {
		inv.Invalidate()
	}
}

// Invalidate 请求窗口产生下一帧；并发安全，可从任意 goroutine 调用。
// 未接线（SetInvalidator 之前）时为 no-op——调用方随后驱动帧即可。
func (r *Router) Invalidate() {
	r.mu.Lock()
	inv := r.inv
	r.mu.Unlock()
	if inv != nil {
		inv.Invalidate()
	}
}

// SetInvalidator 接线窗口失效入口（如 *app.App 或 *app.Window）。
// UI 线程专用；成功后不可更换（防运行中换窗）。
func (r *Router) SetInvalidator(inv Invalidator) error {
	if inv == nil {
		return fmt.Errorf("router: invalidator 为 nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inv != nil {
		return fmt.Errorf("router: invalidator 已设置，不可更换")
	}
	r.inv = inv
	return nil
}

// Shutdown cancels the Router and page contexts and discards pending work
// without waiting for another frame. It is safe to call more than once or
// concurrently. If shutdown wins the state lock before a navigation result
// can commit, that result is dropped without changing the page or NavError.
func (r *Router) Shutdown() {
	r.mu.Lock()
	r.rootCancel()
	r.dropPendingLocked()
	life := r.life
	r.mu.Unlock()
	// R28-C：Shutdown 允许后台线程调用；这里只取消/拒绝排队更新，
	// **绝不**在调用它的后台线程执行 UI OnUnmount。owner 最终化由
	// UI owner 的销毁回调 LifecycleFinalize 负责。
	if life != nil {
		life.cancelOnly()
	}
}

func (r *Router) dropPendingLocked() {
	n := len(r.queue)
	r.queue = nil
	if r.pendingNav != "" {
		n++
		r.pendingNav = ""
	}
	r.dropped.Add(int64(n))
}

// Context 返回 Router 级 Context。页面 Context 由它派生。
func (r *Router) Context() context.Context { return r.rootCtx }

// View 返回根 View——把它交给 app.Run / RunWith / NewApp 即完成接入，
// app 包零改动（结构化接口，见包注释）。
func (r *Router) View() View { return routerView{r} }

// routerView 是根 View 的实现；转场快照（B2）将只包裹 renderCurrent。
type routerView struct{ r *Router }

func (v routerView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.r.drain()
	return v.r.renderCurrent(gtx, th)
}

// drain 在 UI 线程执行帧首工作，顺序对应线程模型 §3：
// 排空 dispatcher 队列 → 应用路由变更 → （渲染由调用方继续）。
func (r *Router) drain() {
	if r.rootCtx.Err() != nil {
		r.mu.Lock()
		r.dropPendingLocked()
		r.mu.Unlock()
		return
	}
	for {
		r.mu.Lock()
		fns := r.queue
		r.queue = nil
		r.mu.Unlock()
		if len(fns) == 0 {
			break
		}
		for i, fn := range fns {
			if r.rootCtx.Err() != nil {
				r.dropped.Add(int64(len(fns) - i))
				return
			}
			// R17-B candidateRef：F-08 边界声明——fn 执行循环无界：
			// fn 内再 Dispatch 会在同帧继续执行（线程模型 §4 未禁止）。
			fn()
		}
	}
	// R28-C：UI owner 在**实际执行时**复查生命周期后才应用路由变更；
	// 迟到/旧代次更新在这里被丢弃。未 opt-in 时仅一次 nil 判断，
	// 稳定帧不分配内存。
	r.mu.Lock()
	life := r.life
	r.mu.Unlock()
	if life != nil {
		life.runPosted()
	}
	r.applyNavigation()
}

// applyNavigation 应用待定导航。必须在 UI 线程调用。
//
// 原子性（授权 §4.6：无半提交）的关键顺序：先构造新页面，成功后才
// 取消旧页 Context 并切换状态——构造失败时旧页原样保留。取消与切换
// 同属一个 UI 线程步骤（线程模型 §7），外部观察者看不到中间状态。
func (r *Router) applyNavigation() {
	r.mu.Lock()
	req := r.pendingNav
	r.pendingNav = ""
	factory := r.registry[req]
	r.mu.Unlock()
	if req == "" {
		return
	}
	if factory == nil {
		// Navigate 已同步校验且 v1 无反注册——此分支理论不可达，保留防御。
		r.setNavErr(fmt.Errorf("%w: %q", ErrPathNotRegistered, req))
		return
	}
	ctx, cancel := context.WithCancel(r.rootCtx)
	// 不持锁调用工厂：页面构造函数允许回查 Current / NavError。
	page, err := factory(ctx)
	// 复查和发布共享同一临界区。显式关闭在先时，成功和失败结果都
	// 作为一次丢弃，保持当前页和最近一次导航错误。
	r.mu.Lock()
	if r.rootCtx.Err() != nil {
		r.mu.Unlock()
		cancel()
		r.dropped.Add(1)
		return
	}
	if err != nil {
		r.navErr = fmt.Errorf("router: 导航到 %q 失败: %w", req, err)
		r.mu.Unlock()
		cancel()
		return
	}
	oldCancel := r.pageCancel
	r.current = req
	r.page = page
	r.pageCancel = cancel
	r.navErr = nil
	life := r.life
	r.mu.Unlock()
	if life != nil {
		// R28-C：仅在**成功发布之后**（UI owner、锁外）才动生命周期。
		// 旧页 Context 先取消 + OnUnmount 一次；新页只记录 pending，
		// 真正的 Mount 在 renderCurrent 的首次业务 Layout 之前。
		life.onSwitch(oldCancel, page, ctx)
	} else if oldCancel != nil {
		oldCancel()
	}
}

func (r *Router) setNavErr(err error) {
	r.mu.Lock()
	r.navErr = err
	r.mu.Unlock()
}

// renderCurrent 渲染当前页面；无当前页时返回零 Dimensions。
//
// 【B2 接口条件】这是当前页面的**唯一**渲染入口：转场快照只需在本方法
// 外层包 op.Record（录制已提交帧到 Router 自有 op.Ops），切换时回放
// 旧快照——转场帧不会走到旧页面的任何业务 Layout。
func (r *Router) renderCurrent(gtx layout.Context, th *material.Theme) layout.Dimensions {
	r.mu.Lock()
	page := r.page
	life := r.life
	r.mu.Unlock()
	if page == nil {
		return layout.Dimensions{}
	}
	// R28-C：首次业务 Layout 之前，由 UI owner 在状态锁之外提交挂载。
	// 未发布 / 失败 / 被丢弃的在途页没有 pending，不会走到 Mount。
	if life != nil {
		life.mountPending()
	}
	return page.Layout(gtx, th)
}
