package app

import (
	"fmt"
	"os"

	"gioui.org/io/event"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ============================================================================
// R27-C：显式进程级单窗入口（**新策略 / 新公共 API，candidate**）
//
// 这是方案 B 的落地。它和通用库的边界是硬性的：
//
//   - `Run` / `RunWith` / `(*App).Run` / `RunWindow*` 一律不调用本入口，
//     也绝不终止调用者进程；它们仍只能交出库函数返回值。
//   - 只有显式调用 `RunSingleWindowProcess` 的 **main** 才让出
//     「终止整个进程」的所有权：主线程执行 Gio Main，事件循环结束后
//     由本入口显式 os.Exit(code)。
//   - 仅适用于单窗且该 main 独占进程的场景。双实例 / 并存窗口没有
//     「任意一窗关闭就全进程退出」的语义，本入口不提供多窗口策略。
//
// 隐藏 os.Exit 伪装正常返回是不允许的：函数名、注释与 ProcessOptions.Exit
// 都把进程边界摆到台面上。生产形态永不正常返回（Windows Main 不返回）。
// ============================================================================

// ProcessOptions 是进程级入口的显式选项。
//
// 所有注入项都只用于受控自检与自有 console helper；零值即真实生产路径。
// 这不是「生产退化全局开关」——每个调用方在自己的调用点显式决定。
type ProcessOptions struct {
	// Next 是受控事件源。nil 表示使用真实 Gio 窗口事件泵（生产路径，
	// 会创建平台窗口；本轮作者阶段不执行该路径）。
	Next func() event.Event
	// Theme / ThemeErr 注入预构建主题或开窗前失败。两者为零时走真实
	// newThemeWithReport（默认主题字体失败在开窗前返回错误）。
	Theme    *material.Theme
	ThemeErr error
	// Lazy 注入懒加载句柄以覆盖严格字体路径；nil 走 NewApp 同步路径。
	Lazy *theme.Lazy
	// FontFailure 注入字体失败源；nil 读 App.fontFailure()。
	FontFailure func() error
	// Observer 在回调、清理、结果生成之后、进程退出之前调用一次。
	// 自有 console helper 用它写 cleanup marker 与结果 JSON。
	Observer func(ExitResult)
	// Exit 注入进程级退出；nil 表示 os.Exit（真实终止整个进程）。
	Exit func(int)
}

// RunSingleWindowProcess 是显式的单窗进程边界（候选）。
//
// 正常生产形态：主线程执行 mainFunc()（即 Gio app.Main()，Windows 上
// 永不返回），事件循环 goroutine 在 DestroyEvent 后完成回调与清理、
// 生成 ExitResult、调用 Observer，然后 os.Exit(Code)。
//
// 受控自检形态（注入 Next / Exit）：Main 也被注入为空函数时，主线程会
// 等待事件循环结束再返回，保证 Observer 与 Exit 已发生；这一形态不创建
// 任何平台窗口、不终止测试进程。
//
// 错误语义：开窗前的参数/主题失败、窗口 DestroyEvent.Err、严格字体失败
// 都会进入 ExitResult.Err（errors.Is 可分别判别），Code=1；正常关窗且
// 无错误为 Code=0。错误绝不只打印后 return nil / exit 0。
func RunSingleWindowProcess(view View, cfg Config, opts ProcessOptions) {
	steps := &exitSteps{}
	steps.add("validate")

	if view == nil {
		finishProcess(processResultErr(fmt.Errorf("app: nil view"), "validate"), opts, steps)
		return
	}
	// R27-C R1 / F-C1：**先按主题来源分流，再构造 App**。
	//
	// 旧实现无条件先调 resolveProcessTheme(opts)（内部走同步默认
	// newThemeWithReport / 默认 Provider）。传入有效 Lazy 时这一步与懒加载
	// 完全无关：默认 Provider 失败会让非 strict 的正常关窗在 NewLazyApp
	// 之前就以 Code=1 退出，临时主题与异步加载根本没机会发生（协调者
	// TestCoordR27LazyProcessDoesNotResolveSyncTheme 的反例）。
	//
	// 分流策略（**候选，待负责人裁决**）：
	//   - opts.Lazy != nil：懒加载路径。主题由 Lazy 在运行时异步交付，首帧
	//     用临时主题；不读取、不等待同步默认 Provider。Lazy 优先于显式
	//     Theme 作为构造入口，显式 Theme 在该路径下不参与构造。
	//   - opts.Lazy == nil：同步路径（显式 Theme 或默认 Provider）。B6 语义
	//     不变——默认主题字体两条链路都失败时**开窗前**返回错误，绝不
	//     静默降级到空白渲染；不把默认错误一概忽略。
	//   - opts.ThemeErr 是调用方显式声明的开窗前失败，两条路径都保留，
	//     它不是同步 Provider，不受分流影响。
	//
	// 保留严格字体语义：StrictFontFailure 由 App.fontFailure() 在事件循环
	// 结束时判定，Lazy 路径仍交出包装了 contracts.ErrFontNotFound 的诊断。
	var a *App
	var err error
	if opts.Lazy != nil {
		if opts.ThemeErr != nil {
			steps.add("theme")
			finishProcess(processResultErr(opts.ThemeErr, "validate", "theme"), opts, steps)
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

	next := opts.Next
	if next == nil {
		// 生产路径：真实 Gio 窗口事件泵。本轮只构建、不执行。
		next = a.w.Event
	}
	fontFailure := opts.FontFailure
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
	res.Steps = steps.snapshot()
	if opts.Observer != nil {
		opts.Observer(res)
	}
	if opts.Exit != nil {
		opts.Exit(res.Code)
		return
	}
	os.Exit(res.Code)
}
