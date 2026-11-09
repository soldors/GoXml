package app

import (
	"errors"
	"fmt"
	"sync"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// ============================================================================
// R27-C：退出契约（BUG-028 的获批单窗基础路径）
//
// 三层分离，不许互相冒充：
//   1. 库函数返回 —— Run / RunWindow* 的 error（本文件让它交出 DestroyEvent.Err）；
//   2. 事件循环结束 —— loopEvents / runWindowEvents / driveSingleWindow 的 return；
//   3. 整个进程退出 —— 只有显式进程级入口 RunSingleWindowProcess 会做，
//      且必须由调用它的 main 明确让出进程所有权。
//
// 本文件不调用 app.Main()、不调用 os.Exit()、不创建平台窗口。
// 基础范围已批准进入隔离验收；原生门禁未完成，不得写成已发布。
// ============================================================================

// ErrWindowDestroy 标记「窗口带着错误提前销毁」。DestroyEvent.Err 的
// 原始 cause 通过 WindowExitError.Unwrap 保留，因此同一条错误既能
// errors.Is(err, ErrWindowDestroy)，也能 errors.Is(err, cause)。
var ErrWindowDestroy = errors.New("app: window destroyed with error")

// WindowExitError 是 DestroyEvent.Err 的明确类型包装（不打印、不吞）。
type WindowExitError struct {
	Cause error
}

func (e *WindowExitError) Error() string {
	if e == nil {
		return "app: window destroyed with error"
	}
	if e.Cause == nil {
		return "app: window destroyed without a cause"
	}
	return fmt.Sprintf("app: window destroyed with error: %v", e.Cause)
}

// Unwrap 保留 Gio 的原始销毁原因，供 errors.Is / errors.As 判别。
func (e *WindowExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is 让 errors.Is(err, ErrWindowDestroy) 成立，同时不遮蔽 cause 的判别。
func (e *WindowExitError) Is(target error) bool { return target == ErrWindowDestroy }

// windowExitError 把 Gio 的 DestroyEvent.Err 转成可判别错误。
// 正常关窗（Err == nil）返回 nil —— 不把正常关闭伪装成失败。
func windowExitError(cause error) error {
	if cause == nil {
		return nil
	}
	return &WindowExitError{Cause: cause}
}

// combineExitErrors 是获批的并存错误合并策略：
//
//   - 只有一条错误时原样返回，保留 errors.Is 判别；
//   - 两条并存时用 errors.Join，窗口原因在前、字体诊断在后，
//     两条都可用 errors.Is 独立判定，字体诊断不被吞掉；
//   - 都为空返回 nil（正常关窗）。
//
// 备选策略（未采用）：只返回优先错误并把另一条塞进自定义字段。
// 选 Join 的理由是调用方无需知道优先级就能分别判定，且不丢诊断文本。
func combineExitErrors(destroyErr, fontErr error) error {
	switch {
	case destroyErr == nil && fontErr == nil:
		return nil
	case destroyErr == nil:
		return fontErr
	case fontErr == nil:
		return destroyErr
	default:
		return errors.Join(destroyErr, fontErr)
	}
}

// ExitResult 是事件循环结束之后、进程级退出之前生成的完整结果。
//
// Internal step diagnostics preserve the real execution order in package tests:
//
//	destroy → callback → cleanup → font → result
//
// 进程级入口还会追加 observer / exit 两步（见 process_entry.go）。
type ExitResult struct {
	// Code 是建议进程退出码：0 = 正常关窗且无错误；1 = 存在错误。
	Code int
	// Err 是合并后的错误（见 combineExitErrors）。
	Err error
	// DestroyErr 是窗口销毁原因（正常关窗为 nil）。
	DestroyErr error
	// FontErr 是严格字体失败（非严格/无懒加载为 nil）。
	FontErr error
	// CallbackDone / CleanupDone 说明显式回调与清理确实先于结果生成。
	CallbackDone bool
	CleanupDone  bool
	// steps is a package-private diagnostic, not a public string contract.
	steps []string
}

func (r ExitResult) stringSteps() []string { return append([]string(nil), r.steps...) }

// exitSteps 是只在事件循环 goroutine 内写入的顺序记录；用互斥锁是为了
// 让 console observer 在另一个 goroutine 读快照时也不会有数据竞争。
type exitSteps struct {
	mu    sync.Mutex
	steps []string
}

func (s *exitSteps) add(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

func (s *exitSteps) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// driveSingleWindow 是**纯受控单窗事件循环**：不调用 Main、不 os.Exit、
// 不创建平台窗口、不启动无限 goroutine。next 由调用方提供。
//
// 它复用现有 App.frame（懒字体/首帧顺序不变）、notifyViewDestroyed
// （WithOnDestroy 回调）与 stopLateCompletion（late worker 清理）。
// 重复 DestroyEvent 只处理一次（第一次即返回）。
func (a *App) driveSingleWindow(next func() event.Event, fontFailure func() error) (ExitResult, *exitSteps) {
	steps := &exitSteps{}
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			a.stopLateCompletion()
			steps.add("cleanup")
		})
	}
	defer cleanup()

	var ops op.Ops
	for {
		e := next()
		switch e := e.(type) {
		case gioapp.DestroyEvent:
			steps.add("destroy")
			// 显式回调一次；重复关闭不会走到第二次（循环已返回）。
			notifyViewDestroyed(a.view)
			steps.add("callback")
			cleanup()
			destroyErr := windowExitError(e.Err)
			var fontErr error
			if fontFailure != nil {
				fontErr = fontFailure()
			}
			steps.add("font")
			err := combineExitErrors(destroyErr, fontErr)
			steps.add("result")
			code := 0
			if err != nil {
				code = 1
			}
			return ExitResult{
				Code:         code,
				Err:          err,
				DestroyErr:   destroyErr,
				FontErr:      fontErr,
				CallbackDone: true,
				CleanupDone:  true,
				steps:        steps.snapshot(),
			}, steps
		case gioapp.FrameEvent:
			a.frame(&ops, e)
		}
	}
}

// runSingleWindowEvents 是给显式进程入口使用的循环包装：返回 ExitResult
// 与顺序记录，供 observer 在进程退出前写出证据。
func (a *App) runSingleWindowEvents(next func() event.Event, fontFailure func() error) (ExitResult, *exitSteps) {
	return a.driveSingleWindow(next, fontFailure)
}

// resolveProcessTheme 决定默认主题（开窗前）。
//
// 注入 Theme / ThemeErr 只用于受控自检；生产两者为零时走真实
// newThemeWithReport，与 Run 的 B6 语义同源：报告缺失或字体两链路都
// 失败时必须返回错误，不静默降级到空白渲染。
func resolveProcessTheme(opts ProcessOptions) (*material.Theme, error) {
	if opts.themeErr != nil {
		return nil, opts.themeErr
	}
	if opts.Theme != nil {
		return opts.Theme, nil
	}
	th, rep := newThemeWithReport()
	if rep == nil {
		return nil, fmt.Errorf("app: 默认主题构建未返回诊断报告")
	}
	if rep.Err != nil {
		return nil, fmt.Errorf("app: 默认主题字体不可用: %w", rep.Err)
	}
	return th, nil
}

// processResultErr 把「开窗前」的失败也变成 ExitResult，保证错误不会
// 只打印后 return nil / exit 0。
func processResultErr(err error, steps ...string) ExitResult {
	return ExitResult{
		Code:  1,
		Err:   err,
		steps: append([]string(nil), steps...),
	}
}
