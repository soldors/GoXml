package app

import (
	"fmt"
	"gioui.org/io/event"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/pkg/runtime/theme"
	"os"
)

// MainConflictPolicy 决定「进程级 Gio Main 已被多窗宿主占用」时，
// 显式单窗进程入口如何收口。这是 R29-C R3 的**候选**新增字段，
// 零值保持 R28 已批准契约不变（见下）。
type MainConflictPolicy uint8

const (
	// MainConflictExitProcess 是零值，也就是 R28 已批准的显式进程入口契约：
	// Observer 收到 ExitResult（Code=1, Err=ErrGioMainAlreadyOwned）后
	// 由本入口 os.Exit(1)。进程里另有宿主在服务时，这条路径会终止整个
	// 进程——这是混用两个进程级入口的编程错误，不是可恢复状态。
	MainConflictExitProcess MainConflictPolicy = iota
	// MainConflictReturn 只在调用方**显式**选择时生效：Observer 收到同样
	// 可判别的结果后直接返回，不退出进程、不关闭宿主窗口、不进入 Main。
	// 供嵌入多窗宿主的调用方在误用旧入口时保留进程与已开窗口。
	MainConflictReturn
)

// ProcessOptions configures an explicitly process-owning single-window entry.
// Theme and Lazy are mutually exclusive; both nil use the default provider.
// Event, failure, and exit injection stay private to package-local tests.
type ProcessOptions struct {
	// Theme supplies a fully constructed synchronous theme.
	Theme *material.Theme
	// Lazy supplies the temporary theme and asynchronous font-loading path.
	Lazy *theme.Lazy
	// Observer runs once after callback/cleanup and before process exit.
	// It must return; it does not control the exit code or process ownership.
	Observer func(ExitResult)
	// OnMainConflict 选择「Main 已被宿主占用」时的收口方式；零值保持
	// R28 契约（Observer 后退出进程）。候选字段，待负责人裁决。
	OnMainConflict MainConflictPolicy
	next           func() event.Event
	themeErr       error
	fontFailure    func() error
	exit           func(int)
}

// RunSingleWindowProcess gives this entry ownership of the entire process.
// Call it explicitly from main, for a single window with no other independent
// windows. It runs Gio Main on the calling thread, then exits the process after
// the window loop invokes its destruction callback, completes cleanup, and
// notifies Observer. Normal close exits 0; validation/window/strict-font errors
// exit 1. Existing Run/RunWindow entry points never call this function or exit
// the caller's process. Windows Gio Main still does not return.
func RunSingleWindowProcess(view View, cfg Config, opts ProcessOptions) {
	steps := &exitSteps{}
	steps.add("validate")
	if view == nil {
		finishProcess(processResultErr(fmt.Errorf("app: nil view"), "validate"), opts, steps)
		return
	}
	if opts.Lazy != nil && opts.Theme != nil {
		finishProcess(processResultErr(fmt.Errorf("app: ProcessOptions.Theme and Lazy are mutually exclusive"), "validate"), opts, steps)
		return
	}
	var a *App
	var err error
	if opts.Lazy != nil {
		if opts.themeErr != nil {
			steps.add("theme")
			finishProcess(processResultErr(opts.themeErr, "validate", "theme"), opts, steps)
			return
		}
		a, err = NewLazyApp(view, opts.Lazy, cfg)
		if err != nil {
			steps.add("construct")
			finishProcess(processResultErr(err, "validate", "construct"), opts, steps)
			return
		}
	} else {
		th, themeErr := resolveProcessTheme(opts)
		if themeErr != nil {
			steps.add("theme")
			finishProcess(processResultErr(themeErr, "validate", "theme"), opts, steps)
			return
		}
		a, err = NewApp(view, th, cfg)
		if err != nil {
			steps.add("construct")
			finishProcess(processResultErr(err, "validate", "theme", "construct"), opts, steps)
			return
		}
	}

	next := opts.next
	if next == nil {
		// 生产路径：真实 Gio 窗口事件泵，已由原生限定验收覆盖。
		next = a.w.Event
	}
	fontFailure := opts.fontFailure
	if fontFailure == nil {
		fontFailure = a.fontFailure
	}

	// R2：在启动窗口事件循环之前认领进程级 Main。Host 已认领时混用
	// 明确失败：Observer 可见错误、不启动窗口、不进入 Main。默认仍按
	// R28 契约退出码 1；MainConflictReturn 只在调用方显式选择时改为
	// 返回而不退出进程（候选，见 MainConflictPolicy）。
	if err := claimGioMain(gioMainLegacyOwner); err != nil {
		if opts.OnMainConflict == MainConflictReturn {
			deliverProcessResult(processResultErr(err, "validate"), opts, steps, "return")
			return
		}
		finishProcess(processResultErr(err, "validate"), opts, steps)
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		res, loopSteps := a.runSingleWindowEvents(next, fontFailure)
		for _, s := range loopSteps.snapshot() {
			steps.add(s)
		}
		finishProcess(res, opts, steps)
	}()

	// 主线程执行 Gio Main（契约 §4 要求主系统线程）。Windows 上永不返回，
	// 进程退出由上面的事件循环 goroutine 显式完成。
	mainFunc()

	// 只有注入 Main 的受控自检会走到这里：等事件循环收口，避免
	// 「主线程先返回、observer 还没写证据」的假绿。
	<-done
}

// finishProcess 把结果交给观察器，再执行进程级退出。
//
// Steps 的最后两步 observer / exit 在调用 Exit 之前写入，因此 console
// 子进程能观察到「清理与显式回调先于进程级退出」的完整顺序。
func finishProcess(res ExitResult, opts ProcessOptions, steps *exitSteps) {
	deliverProcessResult(res, opts, steps, "exit")
	if opts.exit != nil {
		opts.exit(res.Code)
		return
	}
	os.Exit(res.Code)
}

// deliverProcessResult 记录收口阶段并调用 Observer（不退出进程）。
// tailStep 如实标注此后的收口方式："exit"（进程级退出）或 "return"
// （MainConflictReturn 候选路径：调用方显式要求不退出）。
func deliverProcessResult(res ExitResult, opts ProcessOptions, steps *exitSteps, tailStep string) {
	steps.add("observer")
	steps.add(tailStep)
	res.steps = steps.snapshot()
	if opts.Observer != nil {
		opts.Observer(res)
	}
}
