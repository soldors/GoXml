package app

// R29-C R3：显式多窗口宿主**候选**（未批准合同，等待协调者验收）。
//
// 设计红线（ADR-0018 与 R28 单窗进程入口保持不变）：
//   - Gio 的 app.Main() 是**进程级一次性**所有权：包内全局只认领一次。
//     注册（Open）与启动（Run）分离：Open 只登记窗口句柄，**绝不进入
//     Event 源**；Gio 的首次 Event() 才会创建平台窗口。Run 先做所有权
//     判定，成功后才启动每窗事件泵，最后调用 Main。被拒绝的宿主
//     无 Event、无后端调用、无遗留窗口/等待者，句柄与宿主结果可观察。
//   - R27 首个 Event 驱动就绪边界、R2 严格帧握手、R1 关闭双门闩保持：
//     Close 只排队、首事件前不交付、销毁优先、每窗独立、关 A 保 B。
//   - 宿主不持有、不调用、不推断任何窗口的 Router/Owner；不主动
//     os.Exit。最后一窗自然关闭 = drained；调用方显式 ExitAll = explicit。
//
// 平台线程规则：Main 必须在主系统线程调用一次且生产不返回；
// Host.Run() 因此在生产形态下不返回，Host.Wait() 必须由调用方在
// 另一 goroutine 使用。Linux/macOS 需要 CGO=1（交付平台矩阵）。

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

var (
	errWindowCanceled      = errors.New("app: window canceled before host Run")
	errWindowNotCancelable = errors.New("app: window is not cancelable after host Run")
	errHostNotAcceptingRun = errors.New("app: WindowHost is not accepting Run in this state")
)

// gioMainLegacyOwner 标记旧单窗入口（Run/RunWith/RunSingleWindowProcess）
// 已经取得进程级 Main 所有权。
const gioMainLegacyOwner = "legacy"

// gioMain 是**包级**进程所有权状态（不是每个 Host 一份 Once）。
// calls 只统计**被允许进入 Main** 的次数；同宿主重复 Run 由宿主状态机
// 幂等拦截，不进入本结构，因此不会把 Run 次数误记成 Main 次数。
// 诊断接口 gioMainState 保持包内，不扩大公开 API。
var gioMain = struct {
	mu           sync.Mutex
	owner        string
	calls        int
	blockedCalls int
	once         sync.Once
}{}

var nextHostID atomic.Uint64

// claimGioMain 是进程级 Main 的唯一认领点。并发入口由 mutex 串行化。
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

// gioMainState 是包内只读诊断快照（R3：不对外公开）。
func gioMainState() (owner string, calls, blocked int) {
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
	hostAccepting hostState = iota // 可 Open / Cancel / Run / ExitAll(Run 前)
	hostStarting                   // Run 正在做所有权判定
	hostRunning                    // 所有权已认领，窗口事件泵已启动
	hostExiting                    // ExitAll 已排队关闭
	hostDrained                    // 全部窗口收口
	hostRejected                   // Run 被明确拒绝（Main 所有权属于他者）
)

// hostWindowFactory 是包内窗口句柄装配点（与 mainFunc 同构，生产从不改写）。
var hostWindowFactory = func(cfg Config) windowEvents { return newWindow(cfg) }

// HostResult 是宿主收口结果（候选字段，待裁决）。
type HostResult struct {
	Code     int
	Explicit bool
	Drained  bool
	Rejected bool  // Run 因 Main 所有权被拒
	Canceled int   // Run 前被取消/放弃的窗口数
	Closed   int   // 真正经事件泵收口的窗口数
	Err      error // 拒绝原因（errors.Is 可判 ErrGioMainAlreadyOwned）
	Errors   []error
}

// WindowHost 显式管理一组独立窗口实例（候选 API）。
type WindowHost struct {
	mu          sync.Mutex
	id          string
	windows     map[*WindowHandle]struct{}
	labels      map[string]struct{}
	state       hostState
	result      HostResult
	finished    chan struct{}
	finishOnce  sync.Once
	runVerdict  chan struct{} // R4：启动结果（成功/拒绝）发布点，恰关闭一次
	verdictOnce sync.Once
}

// NewWindowHost 构造空宿主；不调用 Main、不创建平台窗口、不进入 Event 源。
func NewWindowHost() *WindowHost {
	return &WindowHost{
		id:         strconv.FormatUint(nextHostID.Add(1), 10),
		windows:    make(map[*WindowHandle]struct{}),
		labels:     make(map[string]struct{}),
		finished:   make(chan struct{}),
		runVerdict: make(chan struct{}),
	}
}

// Open **只注册**窗口句柄（R3）。label 非空唯一、view/th 非空；校验失败
// 返回 error 且不记账。事件泵在 Run 成功取得 Main 所有权后才启动，
// Gio 首次 Event() 创建平台窗口这一步因此不会发生在被拒宿主上。
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
	defer h.mu.Unlock()
	if h.state != hostAccepting {
		return nil, fmt.Errorf("app: WindowHost is not accepting windows (state=%d)", h.state)
	}
	if _, exists := h.labels[label]; exists {
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
	return wh, nil
}

// Run 的顺序（R3 收敛）：宿主状态检查 → 进程级 Main 所有权判定 →
// 启动已注册窗口的事件泵 → 调用一次 Main。
//
// 生产：Main 永不返回，本函数也不返回；Wait() 由另一 goroutine 观察窗口。
// 同宿主重复 Run 幂等返回 nil，不重复启动、不重复调用 Main、不增加
// gioMain.calls。所有权被拒时 reject() 取消所有已注册窗口（Done 关闭、
// Err 可判别、无 Event/后端调用），Wait() 立即返回 Rejected=true。
// publishRunVerdict 发布启动结果，恰关闭一次。**发布点不等待 Gio Main
// 返回**：胜者在完成所有权判定并启动事件泵后立即发布，再调用 Main
// （生产 Main 永不返回，等待者不能依赖它的返回）。
func (h *WindowHost) publishRunVerdict() {
	h.verdictOnce.Do(func() { close(h.runVerdict) })
}

// Run 的顺序（R3 收敛、R4 收敛并发结果）：宿主状态检查 → 进程级 Main
// 所有权判定 → 启动已注册窗口的事件泵 → 发布启动结果 → 调用一次 Main。
//
// 同一宿主并发 Run 的线性化语义（R4）：
//   - 第一个 Run 是「胜者」：它做所有权判定，并在判定完成后发布结果。
//   - 其它并发 Run 是「等待者」：在状态 hostStarting 时**等待结果发布**，
//     不再提前返回 nil；发布后按同一结果返回（成功 nil / 被拒同一 error）。
//   - hostRunning（成功已发布）→ 幂等 nil；hostRejected → 同一拒绝错误；
//     hostDrained/hostExiting（取消/收口）→ errHostNotAcceptingRun。
//
// 生产：Main 永不返回，本函数也不返回；Wait() 由另一 goroutine 观察窗口。
// 同宿主重复 Run 幂等返回 nil，不重复启动、不重复调用 Main、不增加
// gioMain.calls。
func (h *WindowHost) Run() error {
	h.mu.Lock()
	switch h.state {
	case hostRunning:
		h.mu.Unlock()
		return nil
	case hostStarting:
		verdict := h.runVerdict
		h.mu.Unlock()
		<-verdict // 等待胜者的结果发布；发布点先于 Gio Main 返回
		h.mu.Lock()
		defer h.mu.Unlock()
		switch h.state {
		case hostRunning:
			return nil
		case hostRejected:
			return h.result.Err
		default: // hostDrained / hostExiting：启动后又被取消或收口
			return errHostNotAcceptingRun
		}
	case hostRejected:
		err := h.result.Err
		h.mu.Unlock()
		return err
	case hostDrained, hostExiting:
		h.mu.Unlock()
		return errHostNotAcceptingRun
	}
	h.state = hostStarting
	h.mu.Unlock()

	if err := claimGioMain(h.id); err != nil {
		h.reject(err)
		return err
	}

	h.mu.Lock()
	if h.state != hostStarting { // 并发下状态被改变：不启动事件泵
		h.mu.Unlock()
		h.publishRunVerdict()
		return errHostNotAcceptingRun
	}
	h.state = hostRunning
	pending := make([]*WindowHandle, 0, len(h.windows))
	for wh := range h.windows {
		pending = append(pending, wh)
	}
	h.mu.Unlock()

	for _, wh := range pending {
		wh.started.Store(true)
		go h.loopWindow(wh)
	}
	if len(pending) == 0 { // 零窗：立即收口，仍只认领一次 Main；状态保持
		// hostRunning 以便同宿主重复 Run 幂等返回 nil（R1 已验语义）。
		h.signalFinished(true)
	}
	h.publishRunVerdict() // 成功的线性化点：先发布，再进入 Main
	gioMain.once.Do(mainFunc)
	return nil
}

// reject 把被拒宿主的窗口全部标记为 rejected 并收口；不启动任何
// goroutine、不触碰窗口后端。句柄 Done/Err 与宿主 Wait 均可观察。
func (h *WindowHost) reject(err error) {
	h.mu.Lock()
	if h.state == hostRejected {
		h.mu.Unlock()
		return
	}
	h.state = hostRejected
	h.result.Rejected = true
	h.result.Drained = true
	h.result.Err = err
	pending := make([]*WindowHandle, 0, len(h.windows))
	for wh := range h.windows {
		pending = append(pending, wh)
	}
	h.windows = make(map[*WindowHandle]struct{})
	h.labels = make(map[string]struct{})
	h.mu.Unlock()

	for _, wh := range pending {
		wh.rejected.Store(true)
		wh.setErr(fmt.Errorf("app: window %q rejected before start: %w", wh.label, err))
		wh.finish()
	}
	h.signalFinished(true)
	h.publishRunVerdict() // 拒绝的线性化点：等待者立即看到同一拒绝结果
}

// Active 返回当前仍登记（未收口/未取消/未被拒）的窗口数。
func (h *WindowHost) Active() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.windows)
}

// Wait 阻塞到宿主收口（窗口全部自然关闭、显式退出、Run 被拒或零窗）。
func (h *WindowHost) Wait() HostResult {
	<-h.finished
	h.mu.Lock()
	defer h.mu.Unlock()
	res := h.result
	res.Errors = append([]error(nil), h.result.Errors...)
	return res
}

// ExitAll：**Run 前**取消所有已注册窗口（不启动、不触后端）；
// **Run 后**幂等向每个未关闭窗口排队一次关闭请求，由各窗自行清理。
// 幂等，不代做 Finalize、不杀 goroutine、不 os.Exit。
func (h *WindowHost) ExitAll(code int) {
	h.mu.Lock()
	switch h.state {
	case hostAccepting:
		h.result.Explicit = true
		h.result.Code = code
		h.state = hostDrained
		h.result.Drained = true
		pending := make([]*WindowHandle, 0, len(h.windows))
		for wh := range h.windows {
			pending = append(pending, wh)
		}
		h.windows = make(map[*WindowHandle]struct{})
		h.labels = make(map[string]struct{})
		h.result.Canceled += len(pending)
		h.mu.Unlock()
		for _, wh := range pending {
			wh.canceled.Store(true)
			wh.setErr(errWindowCanceled)
			wh.finish()
		}
		h.signalFinished(true)
		return
	case hostStarting:
		// Run 正在做所有权判定、事件泵尚未启动：只把每个已注册窗口的
		// 关闭请求排队（Close 不触后端），由 Run 启动的 loop 在首个事件
		// 就绪后交付；不改状态，避免判定成功后无法完成启动。
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
		return
	case hostRunning:
		h.result.Explicit = true
		h.result.Code = code
		h.state = hostExiting
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
		return
	default:
		h.mu.Unlock()
		return
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

	wh.finish()
	if remaining == 0 {
		h.signalFinished(true)
	}
}

// loopWindow 是单个窗口的事件循环（R2 严格帧握手，R3 起只由 Run 启动）。
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
				wh.destroySeen.Store(true)
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
	if !requestNext() {
		return
	}
	for {
		select {
		case <-wh.closeNotify:
			wh.flushClose()
		case e := <-events:
			wh.driverReady.Store(true)
			switch e := e.(type) {
			case gioapp.DestroyEvent:
				wh.destroyed.Store(true)
				wh.destroyOnce.Do(func() {
					wh.setErr(windowExitError(e.Err))
					notifyViewDestroyed(wh.view)
				})
				return
			case gioapp.FrameEvent:
				gtx := gioapp.NewContext(&ops, e)
				wh.view.Layout(gtx, wh.th)
				e.Frame(gtx.Ops)
				wh.frames.Add(1)
			}
			wh.flushClose()
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
	finishOnce     sync.Once
	frames         atomic.Int64
	done           chan struct{}
	closeNotify    chan struct{}
	driverReady    atomic.Bool
	destroySeen    atomic.Bool
	destroyed      atomic.Bool
	closePending   atomic.Bool
	closeDelivered atomic.Bool
	started        atomic.Bool
	canceled       atomic.Bool
	rejected       atomic.Bool
}

func (wh *WindowHandle) Label() string         { return wh.label }
func (wh *WindowHandle) Done() <-chan struct{} { return wh.done }
func (wh *WindowHandle) Frames() int64         { return wh.frames.Load() }
func (wh *WindowHandle) DriverReady() bool     { return wh.driverReady.Load() }
func (wh *WindowHandle) CloseDelivered() bool  { return wh.closeDelivered.Load() }
func (wh *WindowHandle) Started() bool         { return wh.started.Load() }
func (wh *WindowHandle) Canceled() bool        { return wh.canceled.Load() }
func (wh *WindowHandle) Rejected() bool        { return wh.rejected.Load() }

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

func (wh *WindowHandle) finish() {
	wh.finishOnce.Do(func() { close(wh.done) })
}

// Cancel 是**Run 前**取消已注册窗口的明确入口（R3）：不启动事件源、
// 不触后端；句柄 Done 关闭、Err 可判别，宿主 Wait 记 Canceled。
func (wh *WindowHandle) Cancel() error {
	h := wh.host
	h.mu.Lock()
	if h.state != hostAccepting || wh.started.Load() {
		h.mu.Unlock()
		return errWindowNotCancelable
	}
	if !wh.canceled.CompareAndSwap(false, true) {
		h.mu.Unlock()
		return nil
	}
	delete(h.windows, wh)
	delete(h.labels, wh.label)
	h.result.Canceled++
	h.mu.Unlock()
	wh.setErr(errWindowCanceled)
	wh.finish()
	return nil
}

// Close 只**排队**一次关闭请求，绝不直接调用后端。
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
