package app

// R29-C S2：显式多窗口宿主**候选**（未批准合同，等待协调者验收）。
//
// 设计红线（ADR-0018 与 R28 单窗进程入口保持不变）：
//   - Gio 的 app.Main() 只由宿主调用一次；每个窗口在自己独立的 goroutine
//     里驱动 Event()。窗口事件循环 goroutine 就是该窗口的 UI owner 串行点：
//     Layout、WithOnDestroy 回调、Router.LifecycleFinalize 都在这里发生。
//   - 宿主**不**持有、不调用、不推断任何窗口的 Router/Owner；每个窗口由
//     调用方用 WithOnDestroy(view, finalize) 自行绑定自己的 Router。
//   - 关闭一个窗口只清该窗口；宿主不主动 os.Exit。最后一窗自然关闭 =
//     drained；调用方显式 ExitAll = explicit；两者在 HostResult 里可区分。
//   - 旧 Run / RunWith / RunWindow* / RunSingleWindowProcess 一字不改：
//     本文件只新增 API，不修改既有全局变量与入口。
//
// 平台线程规则：Gio v0.10.3 的 Main 必须在主系统线程调用且生产不返回；
// Open 可以在 Main 之前注册窗口，窗口 goroutine 在首次 Event() 时等驱动。
// Linux/macOS 需要 CGO=1（平台矩阵见交付 S4）。

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	gioapp "gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// hostState 是宿主的三种可观察状态。
type hostState uint8

const (
	hostRunning hostState = iota // 至少还可以打开窗口
	hostExiting                  // 调用方已显式 ExitAll，正在收口
	hostDrained                  // 所有窗口已关闭（自然或显式收口完成）
)

// hostWindowFactory 是包内窗口句柄装配点（与 mainFunc 同构，生产从不改写）。
// 默认构造真实 *gioapp.Window；默认无窗口测试集合把它换成纯内存受控窗口。
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
	windows    map[*WindowHandle]struct{}
	labels     map[string]struct{}
	state      hostState
	result     HostResult
	finished   chan struct{}
	finishOnce sync.Once
	mainOnce   sync.Once
}

// NewWindowHost 构造空宿主；不调用 Main、不创建平台窗口。
func NewWindowHost() *WindowHost {
	return &WindowHost{
		windows:  make(map[*WindowHandle]struct{}),
		labels:   make(map[string]struct{}),
		finished: make(chan struct{}),
	}
}

// Open 注册并启动一个独立窗口实例。label 必须非空且唯一；view / th 必须
// 非空。校验失败返回 error，宿主不记账，已打开的窗口不受影响。
//
// 线程：可从任意 goroutine 调用，但生产形态一般在主线程 Run() 之前完成。
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
		host:  h,
		label: label,
		w:     w,
		view:  view,
		th:    th,
		done:  make(chan struct{}),
	}
	h.windows[wh] = struct{}{}
	h.labels[label] = struct{}{}
	h.mu.Unlock()

	// 每个窗口一条独立事件泵；回调与 Layout 都在该 goroutine（UI owner）。
	go h.loopWindow(wh)
	return wh, nil
}

// Run 在主系统线程调用一次 Gio Main（生产永不返回）。
//
// 与单窗入口的区别：Main 只调用一次，且**不**由窗口关闭触发进程退出；
// 调用方在自己的 goroutine 用 Wait() 取得收口结果，再决定是否做进程级动作。
func (h *WindowHost) Run() {
	h.mainOnce.Do(mainFunc)
}

// Active 返回当前仍登记（未收口）的窗口数。
func (h *WindowHost) Active() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.windows)
}

// Wait 阻塞到所有窗口收口（最后一窗自然关闭或显式 ExitAll 完成），
// 返回可判别的 HostResult。生产形态可在后台 goroutine 调用。
func (h *WindowHost) Wait() HostResult {
	<-h.finished
	h.mu.Lock()
	defer h.mu.Unlock()
	res := h.result
	res.Errors = append([]error(nil), h.result.Errors...)
	return res
}

// ExitAll 是**调用方显式退出**：幂等地向所有未关闭窗口发出一次
// ActionClose，等待各窗口自行完成清理（清理由各窗口的 UI owner 回调负责，
// 宿主不代做 Finalize、不杀 goroutine、不 os.Exit）。
// 已 drained 或已显式退出时为空操作，不会改写已有结果。
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
		wh.Close()
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

// onWindowClosed 由窗口事件泵收尾调用：记账、汇错、判断是否最后一窗。
func (h *WindowHost) onWindowClosed(wh *WindowHandle) {
	h.mu.Lock()
	delete(h.windows, wh)
	delete(h.labels, wh.label)
	h.result.Closed++
	if err := wh.Err(); err != nil {
		h.result.Errors = append(h.result.Errors, fmt.Errorf("app: window %q: %w", wh.label, err))
	}
	remaining := len(h.windows)
	state := h.state
	if remaining == 0 && state == hostRunning {
		h.state = hostDrained
	}
	h.mu.Unlock()

	close(wh.done)
	if remaining == 0 {
		// 自然最后一窗（state==hostDrained）或显式 ExitAll 收口完成都算 drained。
		h.signalFinished(true)
	}
}

func (h *WindowHost) loopWindow(wh *WindowHandle) {
	defer h.onWindowClosed(wh)
	var ops op.Ops
	for {
		e := wh.w.Event()
		switch e := e.(type) {
		case gioapp.DestroyEvent:
			// 只回调一次：重复关窗 / 重复 Destroy 不重复卸载。
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
	}
}

// WindowHandle 是一个独立窗口实例的句柄。
type WindowHandle struct {
	host        *WindowHost
	label       string
	w           windowEvents
	view        View
	th          *material.Theme
	mu          sync.Mutex
	err         error
	closeOnce   sync.Once
	destroyOnce sync.Once
	frames      atomic.Int64
	done        chan struct{}
}

// Label 返回窗口标签。
func (wh *WindowHandle) Label() string { return wh.label }

// Done 在该窗口事件泵收口（DestroyEvent 回调完成）后关闭。
func (wh *WindowHandle) Done() <-chan struct{} { return wh.done }

// Frames 返回该窗口已经处理过的 FrameEvent 数（健康对照用）。
func (wh *WindowHandle) Frames() int64 { return wh.frames.Load() }

// Err 返回该窗口 DestroyEvent 交出的错误（正常关闭为 nil）。
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

// Close 幂等请求关闭**本窗口**：一次 ActionClose + Invalidate。
// 真正清理由该窗口事件泵里的 DestroyEvent 回调完成，不阻塞、不跨窗口。
func (wh *WindowHandle) Close() error {
	wh.closeOnce.Do(func() {
		wh.w.Perform(system.ActionClose)
		wh.w.Invalidate()
	})
	return nil
}

// 编译期确认生产窗口句柄满足宿主使用的窗口接口。
var _ windowEvents = (*gioapp.Window)(nil)
