// Package lifecycle 提供**显式、UI owner 驱动**的页面/root 生命周期核心
// （R28-C / TASK-LIFE-001-1、001-2 的隔离候选）。
//
// 责任分层：
//   - 本包只管理状态机与 owner 队列，**不创建任何 goroutine**，不 import
//     pkg/runtime/app 或 pkg/runtime/router（避免依赖环、避免假冒 UI 线程）；
//   - OnMount / OnUnmount 只复用 contracts.LifecycleMount / LifecycleUnmount，
//     不发明第二套回调契约；两者必须由 UI owner（渲染帧或销毁回调所在线程）
//     显式提交，且**在状态锁之外**调用；
//   - 后台任务只持有 Handle，通过 Post 把更新投递给 owner；RunPosted 在
//     **实际执行时**复查生命期，取消/换页之后才排到的旧结果一律丢弃，
//     业务无需每处手写检查。
//
// 明确不承诺：本包不能强制杀死任意 goroutine；不合作的 worker 回调若阻塞
// 或持有 lock 写 VM，属于调用方责任。外部仅取消而 owner 再不运行时，
// CleanupDone() 保持 false，作为责任边界如实披露。
package lifecycle

import (
	"context"
	"errors"
	"sync"

	"github.com/goxml/goxml/contracts"
)

var (
	// ErrOwnerClosed 表示 owner 已被外部取消或最终化，不能再挂载。
	ErrOwnerClosed = errors.New("lifecycle: owner is closed")
	// ErrHandleMounted 表示同一 Handle 被重复挂载（重挂必须取新 Handle）。
	ErrHandleMounted = errors.New("lifecycle: handle already mounted")
	// ErrHandleStale 表示 Handle 属于已结束的旧代次。
	ErrHandleStale = errors.New("lifecycle: handle belongs to a stale generation")
	// ErrWrongOwner 表示 Handle 来自另一个 Owner。
	ErrWrongOwner = errors.New("lifecycle: handle belongs to another owner")
)

// State 是一次生命期的状态。
type State uint8

const (
	// StateCreated：已构造但未挂载（工厂构造 / 成功发布但尚未首次 Layout）。
	StateCreated State = iota
	// StateMounted：已在 UI owner 提交挂载。
	StateMounted
	// StateUnmounted：已取消并完成一次清理（终态）。
	StateUnmounted
)

func (s State) String() string {
	switch s {
	case StateCreated:
		return "created"
	case StateMounted:
		return "mounted"
	case StateUnmounted:
		return "unmounted"
	}
	return "unknown"
}

// Handle 是**一次**挂载实例，携带独立代次与 Context。
// 重挂必须调用 Owner.NewHandle 取新 Handle；旧 Handle 不能向新实例写入。
type Handle struct {
	owner     *Owner
	gen       uint64
	target    any
	onMount   contracts.LifecycleMount
	onUnmount contracts.LifecycleUnmount

	mu     sync.Mutex
	state  State
	ctx    context.Context
	cancel context.CancelFunc
}

// Generation 返回该生命期的代次号。
func (h *Handle) Generation() uint64 { return h.gen }

// Target 返回构造时传入的生命周期目标（诊断用）。
func (h *Handle) Target() any { return h.target }

// State 返回当前状态。
func (h *Handle) State() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// Context 返回该生命期的 Context；未挂载时为 nil。
func (h *Handle) Context() context.Context {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ctx
}

// Err 返回该生命期 Context 的取消原因；未挂载时返回 nil。
func (h *Handle) Err() error {
	h.mu.Lock()
	ctx := h.ctx
	h.mu.Unlock()
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (h *Handle) setState(s State) {
	h.mu.Lock()
	h.state = s
	h.mu.Unlock()
}

// Owner 是显式 UI owner 的生命周期状态机。零值不可用，用 NewOwner 构造。
type Owner struct {
	mu          sync.Mutex
	root        context.Context
	rootCancel  context.CancelFunc
	current     *Handle
	gen         uint64
	closed      bool
	cleanupDone bool

	posted []posting

	mountCalls   int64
	unmountCalls int64
	applied      int64
	rejected     int64
}

type posting struct {
	h  *Handle
	fn func()
}

// NewOwner 用根 Context 构造 owner。root 为 nil 时用 context.Background()。
// 构造不启动任何 goroutine；所有回调须由 UI owner 显式驱动。
func NewOwner(root context.Context) *Owner {
	if root == nil {
		root = context.Background()
	}
	ctx, cancel := context.WithCancel(root)
	return &Owner{root: ctx, rootCancel: cancel}
}

// Context 返回 owner 根 Context（页面 Context 从它派生）。
func (o *Owner) Context() context.Context { return o.root }

// NewHandle 为一个**尚未挂载**的页面创建独立 Handle。
// 工厂构造或 Router 成功发布但尚未首次 Layout 时调用；不产生 Context、
// 不调用 OnMount，因此失败/丢弃的在途页面不占用资源。
func (o *Owner) NewHandle(target any) (*Handle, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, ErrOwnerClosed
	}
	o.gen++
	h := &Handle{
		owner:  o,
		gen:    o.gen,
		target: target,
		state:  StateCreated,
	}
	h.onMount, _ = target.(contracts.LifecycleMount)
	h.onUnmount, _ = target.(contracts.LifecycleUnmount)
	return h, nil
}

// MountHandle 在 **UI owner** 上提交挂载。
//
// 顺序：若已有挂载实例，先取消它的 Context 并调用一次 OnUnmount（清理先于
// 新页），再为新 Handle 建立独立 Context 并调用 OnMount。两个回调都在状态
// 锁之外执行，允许回调内做 Current 查询、合法导航或关闭请求回入。
// parent 为页面级 Context（Router 页面 ctx）；nil 时用 owner 根 Context。
func (o *Owner) MountHandle(h *Handle, parent context.Context) error {
	if h == nil || h.owner != o {
		return ErrWrongOwner
	}
	o.mu.Lock()
	if st := h.State(); st == StateMounted || st == StateUnmounted {
		o.mu.Unlock()
		return ErrHandleMounted
	}
	if o.closed {
		o.mu.Unlock()
		return ErrOwnerClosed
	}
	o.mu.Unlock()

	// 旧生命期先取消再清理（回调在锁外）。
	o.unmountCurrent()

	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		h.setState(StateUnmounted)
		return ErrOwnerClosed
	}
	base := parent
	if base == nil {
		base = o.root
	}
	ctx, cancel := context.WithCancel(base)
	h.mu.Lock()
	h.ctx = ctx
	h.cancel = cancel
	h.state = StateMounted
	h.mu.Unlock()
	o.current = h
	o.mountCalls++
	o.mu.Unlock()

	// 锁外回调：OnMount 可启动后台任务，但不得同步等待其完成（线程模型 §7）。
	if h.onMount != nil {
		h.onMount.OnMount(ctx)
	}
	return nil
}

// Switch 是「新 Handle + 立即挂载」的便捷入口（UI owner 专用）。
func (o *Owner) Switch(target any, parent context.Context) (*Handle, error) {
	h, err := o.NewHandle(target)
	if err != nil {
		return nil, err
	}
	if err := o.MountHandle(h, parent); err != nil {
		return nil, err
	}
	return h, nil
}

// unmountCurrent 取消当前生命期并调用一次 OnUnmount。
// 取消一定在清理回调之前；重复调用幂等；不持状态锁执行回调。
func (o *Owner) unmountCurrent() {
	o.mu.Lock()
	h := o.current
	if h == nil {
		o.mu.Unlock()
		return
	}
	h.mu.Lock()
	if h.state == StateUnmounted {
		h.mu.Unlock()
		o.current = nil
		o.mu.Unlock()
		return
	}
	cancel := h.cancel
	h.state = StateUnmounted
	h.mu.Unlock()
	o.current = nil
	o.unmountCalls++
	o.mu.Unlock()

	if cancel != nil {
		cancel() // 先取消
	}
	if h.onUnmount != nil {
		h.onUnmount.OnUnmount() // 再一次清理（锁外）
	}
}

// UnmountCurrent 由 UI owner 显式调用：取消当前生命期并完成一次清理。
// Router 在页面成功切换时用它实现「旧 ctx 先取消、OnUnmount 一次，再挂新页」。
// 幂等；没有当前生命期时为 no-op。绝不从后台线程调用。
func (o *Owner) UnmountCurrent() { o.unmountCurrent() }

// Finalize 在 **UI owner 的销毁回调**里调用一次：丢弃排队更新、取消根与
// 页面 Context、完成一次 OnUnmount。不依赖关闭后再 Dispatch 或下一帧 Layout。
// 重复调用幂等。
func (o *Owner) Finalize() {
	o.mu.Lock()
	if o.cleanupDone {
		o.mu.Unlock()
		return
	}
	o.closed = true
	o.rootCancel()
	fns := o.posted
	o.posted = nil
	o.mu.Unlock()
	for range fns {
		o.addRejected()
	}
	o.unmountCurrent()
	o.mu.Lock()
	o.cleanupDone = true
	o.mu.Unlock()
}

// CancelOnly 可由**后台线程**调用（例如 Router.Shutdown）：只**直接**取消
// owner 根 Context、拒绝排队更新，但**绝不执行 OnUnmount**——UI 回调不能在
// 后台线程跑。
//
// 注意：本函数不承诺取消**独立 parent 的页面** Context。MountHandle 传入非 nil
// 且独立于 owner 根的 parent 时，页面 Context 的取消由调用方负责：
// Router.Shutdown 取消 Router 根，或 Finalize 的 unmountCurrent 调用
// Handle.cancel。parent 为 nil 时页面 Context 派生自 owner 根，会随
// rootCancel 一并取消。本注释只澄清所有权，不改变任何行为。
//
// 若此后 owner 不再运行，CleanupDone() 保持 false，必须由调用方披露。
// 重复调用幂等，不会重复计数丢弃项。
func (o *Owner) CancelOnly() {
	o.mu.Lock()
	o.closed = true
	o.rootCancel()
	fns := o.posted
	o.posted = nil
	o.mu.Unlock()
	for range fns {
		o.addRejected()
	}
}

// Post 从任意 goroutine 把「与某生命期关联的更新」投递给 owner。
// 只是入队，**不在后台执行 fn**；是否应用由 RunPosted 在实际执行时复查。
func (o *Owner) Post(h *Handle, fn func()) {
	if fn == nil {
		return
	}
	// h 为 nil（尚未挂载或已切换）时仍允许入队：RunPosted 会在实际执行时
	// 判为不活跃并计入拒绝，避免「未挂载时投递」静默消失。
	if h != nil && h.owner != o {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		o.rejected++
		return
	}
	o.posted = append(o.posted, posting{h: h, fn: fn})
}

// RunPosted 在 UI owner 上执行已排队的更新，逐条**在实际执行时**复查生命期：
// 只有仍是当前代次、已挂载且 Context 未取消的更新才执行；其余丢弃并计数。
// 稳定帧无投递时只做一次 lock/unlock，不分配内存。
func (o *Owner) RunPosted() {
	o.mu.Lock()
	fns := o.posted
	o.posted = nil
	o.mu.Unlock()
	for i := range fns {
		if o.isActive(fns[i].h) {
			fns[i].fn()
			o.addApplied()
		} else {
			o.addRejected()
		}
		fns[i] = posting{}
	}
}

func (o *Owner) isActive(h *Handle) bool {
	o.mu.Lock()
	ok := !o.closed && o.current == h && h != nil
	o.mu.Unlock()
	if !ok {
		return false
	}
	h.mu.Lock()
	st, ctx := h.state, h.ctx
	h.mu.Unlock()
	return st == StateMounted && ctx != nil && ctx.Err() == nil
}

func (o *Owner) addApplied() {
	o.mu.Lock()
	o.applied++
	o.mu.Unlock()
}

func (o *Owner) addRejected() {
	o.mu.Lock()
	o.rejected++
	o.mu.Unlock()
}

// Current 返回当前挂载的 Handle（可能为 nil）。
func (o *Owner) Current() *Handle {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.current
}

// CleanupDone 披露 UI owner 是否已完成一次清理。
// 外部只 CancelOnly 而 owner 不再运行时为 false —— 不能宣称资源回调已完成。
func (o *Owner) CleanupDone() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cleanupDone
}

// Closed 披露 owner 是否已进入关闭态（CancelOnly 或 Finalize）。
func (o *Owner) Closed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closed
}

// Snapshot 是诊断快照。
type Snapshot struct {
	Generation   uint64
	State        State
	Closed       bool
	CleanupDone  bool
	MountCalls   int64
	UnmountCalls int64
	Applied      int64
	Rejected     int64
	Posted       int
}

func (o *Owner) Snapshot() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	s := Snapshot{
		Closed: o.closed, CleanupDone: o.cleanupDone,
		MountCalls: o.mountCalls, UnmountCalls: o.unmountCalls,
		Applied: o.applied, Rejected: o.rejected, Posted: len(o.posted),
	}
	if o.current != nil {
		s.Generation = o.current.gen
		s.State = o.current.State()
	}
	return s
}

// ErrStale 用于显式判断一个 Handle 是否属于旧代次。
func (o *Owner) ErrStale(h *Handle) error {
	if h == nil || h.owner != o {
		return ErrWrongOwner
	}
	o.mu.Lock()
	stale := o.current != h
	o.mu.Unlock()
	if stale {
		return ErrHandleStale
	}
	return nil
}
