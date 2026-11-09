package app

import (
	"fmt"
	"gioui.org/io/event"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/pkg/runtime/theme"
	"os"
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
	Observer    func(ExitResult)
	next        func() event.Event
	themeErr    error
	fontFailure func() error
	exit        func(int)
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
		// 生产路径：真实 Gio 窗口事件泵。本轮只构建、不执行。
		next = a.w.Event
	}
	fontFailure := opts.fontFailure
	if fontFailure == nil {
		fontFailure = a.fontFailure
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
	steps.add("observer")
	steps.add("exit")
	res.steps = steps.snapshot()
	if opts.Observer != nil {
		opts.Observer(res)
	}
	if opts.exit != nil {
		opts.exit(res.Code)
		return
	}
	os.Exit(res.Code)
}
