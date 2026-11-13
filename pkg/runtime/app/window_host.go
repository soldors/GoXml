package app

// R29-C R1：显式多窗口宿主**候选**（未批准合同，等待协调者验收）。
//
// 设计红线（ADR-0018 与 R28 单窗进程入口保持不变）：
//   - Gio 的 app.Main() 是**进程级一次性**所有权：包内全局只认领一次，
//     第二个 WindowHost.Run() 明确拒绝；旧单窗入口与 Host 混用不受支持。
//   - 每个窗口在自己独立的事件泵里驱动 Event()。窗口 loop goroutine
//     就是该窗口的 UI owner 串行点：Layout、WithOnDestroy 回调、
//     Router.LifecycleFinalize 都在这里发生。
//   - R27 首个 Event 驱动就绪边界：**首次 Event() 返回之后**才允许向
//     后端 Perform(ActionClose)；提前的 Close/ExitAll 只排队，就绪后
//     由该窗 loop 至多一次交付。若首次或随后已进入 DestroyEvent，
//     禁止任何后端调用。
//   - 宿主不持有、不调用、不推断任何窗口的 Router/Owner；不主动
//     os.Exit。最后一窗自然关闭 = drained；调用方显式 ExitAll = explicit。
//   - 旧 Run / RunWith / RunWindow* / RunSingleWindowProcess 的公开 API
//     与行为逐字不变（本文件不改它们的定义）。
//
// 平台线程规则：Main 必须在主系统线程调用一次且生产不返回；
// Host.Run() 因此在生产形态下不返回，Host.Wait() 必须由调用方在
// 另一 goroutine 使用。Linux/macOS 需要 CGO=1（交付 S4 平台矩阵）。

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// ErrGioMainAlreadyOwned 表示 Gio Main 的进程级所有权已被其它宿主或
// 旧单窗入口占用；混用双方都明确拒绝，绝不第二次进入 Main。
var ErrGioMainAlreadyOwned = errors.New("app: Gio Main is already owned")

// gioMainLegacyOwner 标记旧单窗入口（Run/RunWith/RunSingleWindowProcess）
// 已经取得进程级 Main 所有权。
const gioMainLegacyOwner = "legacy"

// gioMain 是**包级**进程所有权状态（不是每个 Host 一份 Once）。
// R2：不再只靠 mainFunc 包装计数；所有入口在调用 Main 前必须 claimGioMain，
// owner 一旦被 Host 认领，旧入口再次进入 Main 会被阻止并计数（可验证边界）。
var gioMain = struct {
	mu           sync.Mutex
	owner        string
	calls        int
	blockedCalls int
	once         sync.Once
}{}

var nextHostID atomic.Uint64

// claimGioMain 是进程级 Main 的唯一认领点。并发入口由 mutex 串行化：
// 第一个认领者成功；同一入口重复调用（受控形态）允许；不同入口混用
// 明确返回 ErrGioMainAlreadyOwned，且**不调用 mainFunc**。
func claimGioMain(who string) error {
	gioMain.mu.Lock()
	defer gioMain.mu.Unlock()
	switch {
	case gioMain.owner == "":
		gioMain.owner = who
		gioMain.calls++
		return nil
	case gioMain.owner == who:
		gioMain.calls++
		return nil
	default:
		gioMain.blockedCalls++
		return fmt.Errorf("%w: owner=%q requestedBy=%q mainCalls=%d",
			ErrGioMainAlreadyOwned, gioMain.owner, who, gioMain.calls)
	}
}

// GioMainState 返回只读快照，供协调者验证进程级所有权边界。
func GioMainState() (owner string, calls, blocked int) {
	gioMain.mu.Lock()
	defer gioMain.mu.Unlock()
	return gioMain.owner, gioMain.calls, gioMain.blockedCalls
}

// resetGioMainForTest 只供包内受控测试隔离全局进程所有权状态。
func resetGioMainForTest() {
	gioMain.mu.Lock()
	defer gioMain.mu.Unlock()
	gioMain.owner = ""
	gioMain.calls = 0
	gioMain.blockedCalls = 0
	gioMain.once = sync.Once{}
}

type hostState uint8

const (
	hostRunning hostState = iota
	hostExiting
	hostDrained
)

// hostWindowFactory 是包内窗口句柄装配点（与 mainFunc 同构，生产从不改写）。
var hostWindowFactory = func(cfg Config) windowEvents { return newWindow(cfg) }

// HostResult 是宿主收口结果。Explicit 区分「调用方显式退出」与
// 「最后一窗自然关闭」；Code 只在显式退出时由调用方给定（默认 0）。
type HostResult struct {
	Code     int
	Explicit bool
	Drained  bool
	Closed   int
	Errors   []error
}

// WindowHost 显式管理一组独立窗口实例（候选 API）。
type WindowHost struct {
	mu         sync.Mutex
	id         string
	windows    map[*WindowHandle]struct{}
	labels     map[string]struct{}
	state      hostState
	result     HostResult
	finished   chan struct{}
	finishOnce sync.Once
}

// NewWindowHost 构造空宿主；不调用 Main、不创建平台窗口。
func NewWindowHost() *WindowHost {
	return &WindowHost{
		id:       strconv.FormatUint(nextHostID.Add(1), 10),
		windows:  make(map[*WindowHandle]struct{}),
		labels:   make(map[string]struct{}),
		finished: make(chan struct{}),
	}
}

// Open 注册并启动一个独立窗口实例。label 必须非空且唯一；view / th 必须
// 非空。校验失败返回 error，宿主不记账，已打开的窗口不受影响。
func (h *WindowHost) Open(label string, view View, th *material.Theme, cfg Config) (*WindowHandle, error) {
	if label == "" {
		return nil, errors.New("app: WindowHost.Open requires a non-empty label")
	}
	if view == nil {
		return nil, errors.New("app: WindowHost.Open requires a view")
	}
	if th == nil {
		return nil, errors.New("app: WindowHost.Open requires a theme")
	}

	h.mu.Lock()
	if h.state != hostRunning {
		h.mu.Unlock()
		return nil, fmt.Errorf("app: WindowHost is not accepting windows (state=%d)", h.state)
	}
	if _, exists := h.labels[label]; exists {
		h.mu.Unlock()
		return nil, fmt.Errorf("app: duplicate window label %q", label)
	}
	w := hostWindowFactory(cfg.withDefaults())
	wh := &WindowHandle{
		host:        h,
		label:       label,
		w:           w,
		view:        view,
		th:          th,
		done:        make(chan struct{}),
		closeNotify: make(chan struct{}, 1),
	}
	h.windows[wh] = struct{}{}
	h.labels[label] = struct{}{}
	h.mu.Unlock()

	go h.loopWindow(wh)
	return wh, nil
}

// Run 尝试取得**进程级** Gio Main 所有权并调用一次 Main。
//
// 生产：Main 永不返回，本函数也不返回；调用方必须在另一 goroutine 用
// Wait() 取得窗口收口结果。受控注入 Main 返回时本函数返回 nil。
// 若已有其它 Host 认领，或旧单窗入口已经调用过 Main，返回
// ErrGioMainAlreadyOwned，绝不重复调用 Main（不扩大退出政策）。
func (h *WindowHost) Run() error {
	if err := claimGioMain(h.id); err != nil {
		return err
	}
	gioMain.once.Do(mainFunc)
	return nil
}

// Active 返回当前仍登记（未收口）的窗口数。
func (h *WindowHost) Active() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.windows)
}

// Wait 阻塞到所有窗口收口（最后一窗自然关闭或显式 ExitAll 完成）。
// 与 Run 相互独立：生产 Main 不返回时，Wait 在后台 goroutine 观察窗口，
// 不参与进程级退出决定。
func (h *WindowHost) Wait() HostResult {
	<-h.finished
	h.mu.Lock()
	defer h.mu.Unlock()
	res := h.result
	res.Errors = append([]error(nil), h.result.Errors...)
	return res
}

// ExitAll 是**调用方显式退出**：幂等向所有未关闭窗口排队一次关闭请求，
// 等待各窗口自行完成清理；不代做 Finalize、不杀 goroutine、不 os.Exit。
func (h *WindowHost) ExitAll(code int) {
	h.mu.Lock()
	if h.state != hostRunning {
		h.mu.Unlock()
		return
	}
	h.state = hostExiting
	h.result.Explicit = true
	h.result.Code = code
	pending := make([]*WindowHandle, 0, len(h.windows))
	for wh := range h.windows {
		pending = append(pending, wh)
	}
	h.mu.Unlock()

	for _, wh := range pending {
		_ = wh.Close()
	}
	if len(pending) == 0 {
		h.signalFinished(true)
	}
}

func (h *WindowHost) signalFinished(drained bool) {
	h.finishOnce.Do(func() {
		h.mu.Lock()
		if drained {
			h.result.Drained = true
		}
		h.mu.Unlock()
		close(h.finished)
	})
}

func (h *WindowHost) onWindowClosed(wh *WindowHandle) {
	h.mu.Lock()
	delete(h.windows, wh)
	delete(h.labels, wh.label)
	h.result.Closed++
	if err := wh.Err(); err != nil {
		h.result.Errors = append(h.result.Errors, fmt.Errorf("app: window %q: %w", wh.label, err))
	}
	remaining := len(h.windows)
	if remaining == 0 && h.state == hostRunning {
		h.state = hostDrained
	}
	h.mu.Unlock()

	close(wh.done)
	if remaining == 0 {
		h.signalFinished(true)
	}
}

// loopWindow 是单个窗口的事件循环（R2 严格帧握手）。
//
// Gio v0.10.3 的 os.go eventLoop.deliverEvent 在投递 FrameEvent 后，要等
// client 调用 FrameEvent.Frame（eventLoop.Frame 阻塞在 frameAck）并用
// theFlushEvent 确认「前一事件已处理」，之后才投递下一个事件。若独立
// pump 在前一帧 Layout / Frame 回调完成前就调用下一次 Window.Event()，
// 会与后端的帧交换/ack 顺序冲突（受控探针证明的是这个生产事件顺序风险，
// 不等于已在真实原生窗口挂起）。
//
// 因此 pump **只在 loop 明确请求时**读取一次 Event()；loop 在处理完该事件
// （Layout + Frame 回调返回 + 关闭请求结算）之后才发出下一次请求。
// 每窗拥有独立的 requests/events/pump，互不串扰。
func (h *WindowHost) loopWindow(wh *WindowHandle) {
	defer h.onWindowClosed(wh)
	events := make(chan event.Event) // 无缓冲：pump 必须等 loop 接收
	requests := make(chan struct{})  // loop -> pump：允许拉取下一次 Event()
	go func() {
		for {
			select {
			case <-requests:
			case <-wh.done:
				return
			}
			e := wh.w.Event()
			destroy := false
			if _, ok := e.(gioapp.DestroyEvent); ok {
				wh.destroySeen.Store(true) // 后端已交出销毁事件：禁止后续调用
				destroy = true
			}
			select {
			case events <- e:
			case <-wh.done:
				return
			}
			if destroy {
				return
			}
		}
	}()

	requestNext := func() bool {
		select {
		case requests <- struct{}{}:
			return true
		case <-wh.done:
			return false
		}
	}

	var ops op.Ops
	if !requestNext() { // 首个 Event() 由 loop 显式放行
		return
	}
	for {
		select {
		case <-wh.closeNotify:
			// Close 非阻塞：排队信号在这里结算；驱动未就绪时只保留排队。
			wh.flushClose()
		case e := <-events:
			// R27 边界：首个 Event() 返回（pump 已读到事件）才标记驱动就绪。
			wh.driverReady.Store(true)
			switch e := e.(type) {
			case gioapp.DestroyEvent:
				// 销毁优先：不再请求下一个事件，也不再触后端。
				wh.destroyed.Store(true)
				wh.destroyOnce.Do(func() {
					wh.setErr(windowExitError(e.Err))
					notifyViewDestroyed(wh.view)
				})
				return
			case gioapp.FrameEvent:
				gtx := gioapp.NewContext(&ops, e)
				wh.view.Layout(gtx, wh.th)
				e.Frame(gtx.Ops) // 真实后端在此完成帧交换并 ack
				wh.frames.Add(1)
			}
			// 首事件就绪后结算排队的关闭请求（至多一次）。
			wh.flushClose()
			// 前一事件（含 Frame 回调）处理完成后，才允许拉取下一事件。
			if !requestNext() {
				return
			}
		}
	}
}

// WindowHandle 是一个独立窗口实例的句柄。
type WindowHandle struct {
	host           *WindowHost
	label          string
	w              windowEvents
	view           View
	th             *material.Theme
	mu             sync.Mutex
	err            error
	destroyOnce    sync.Once
	frames         atomic.Int64
	done           chan struct{}
	closeNotify    chan struct{}
	driverReady    atomic.Bool
	destroySeen    atomic.Bool
	destroyed      atomic.Bool
	closePending   atomic.Bool
	closeDelivered atomic.Bool
}

func (wh *WindowHandle) Label() string         { return wh.label }
func (wh *WindowHandle) Done() <-chan struct{} { return wh.done }
func (wh *WindowHandle) Frames() int64         { return wh.frames.Load() }
func (wh *WindowHandle) DriverReady() bool     { return wh.driverReady.Load() }
func (wh *WindowHandle) CloseDelivered() bool  { return wh.closeDelivered.Load() }

func (wh *WindowHandle) Err() error {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	return wh.err
}

func (wh *WindowHandle) setErr(err error) {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	wh.err = err
}

// Close 只**排队**一次关闭请求，绝不直接调用后端。
// 若已进入 DestroyEvent（首次/随后），直接返回，后端调用次数为 0。
// 真正交付由该窗 loop 在驱动就绪后完成，至多一次。
func (wh *WindowHandle) Close() error {
	if wh.destroyed.Load() || wh.destroySeen.Load() {
		return nil
	}
	if wh.closePending.CompareAndSwap(false, true) {
		select {
		case wh.closeNotify <- struct{}{}:
		default:
		}
	}
	return nil
}

// flushClose 只能由该窗 loop goroutine 调用：R27 就绪门闩 + 销毁门闩 +
// 至多一次交付。Perform 与 DestroyEvent 在同一串行点，无跨 goroutine 竞争。
func (wh *WindowHandle) flushClose() {
	if !wh.driverReady.Load() || !wh.closePending.Load() {
		return
	}
	if wh.destroyed.Load() || wh.destroySeen.Load() {
		return
	}
	if !wh.closeDelivered.CompareAndSwap(false, true) {
		return
	}
	wh.w.Perform(system.ActionClose)
	wh.w.Invalidate()
}

var _ windowEvents = (*gioapp.Window)(nil)
