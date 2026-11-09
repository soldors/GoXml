package app

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ============================================================================
// R27-C C2：可运行候选的受控回归
//
// 全部使用受控事件源 / 注入的退出函数；不调用真实 app.Main()、不执行
// os.Exit、不创建平台窗口。真实进程退出码由 exit_helper_test.go 的
// 自有 console 子进程验证。
// ============================================================================

func exitControlled(evts ...event.Event) (func() event.Event, chan event.Event) {
	ch := make(chan event.Event, len(evts))
	for _, e := range evts {
		ch <- e
	}
	return func() event.Event { return <-ch }, ch
}

func exitStubMain(t *testing.T) {
	t.Helper()
	orig := mainFunc
	t.Cleanup(func() { mainFunc = orig })
	mainFunc = func() {}
}

// TestExitContractWindowCauseDoubleDiscriminable 双判别：
// errors.Is 同时命中 ErrWindowDestroy 与 Gio 原始 cause，且正常关窗为 nil。
func TestExitContractWindowCauseDoubleDiscriminable(t *testing.T) {
	cause := errors.New("native premature close")
	err := windowExitError(cause)
	if !errors.Is(err, ErrWindowDestroy) || !errors.Is(err, cause) {
		t.Fatalf("窗口错误不可双判别: %v", err)
	}
	var typed *WindowExitError
	if !errors.As(err, &typed) || typed.Cause != cause {
		t.Fatalf("窗口错误类型/原因丢失: %v", err)
	}
	if windowExitError(nil) != nil {
		t.Fatal("正常关窗不应产生错误")
	}
}

// TestExitContractJoinKeepsFontDiagnostic 并存错误不被吞：
// destroy + strict font 两条都能 errors.Is 独立判定，文本都保留。
func TestExitContractJoinKeepsFontDiagnostic(t *testing.T) {
	cause := errors.New("join destroy cause")
	fontErr := fmt.Errorf("lazy font failed: %w", contracts.ErrFontNotFound)
	joined := combineExitErrors(windowExitError(cause), fontErr)
	if !errors.Is(joined, cause) || !errors.Is(joined, ErrWindowDestroy) {
		t.Fatalf("合并后窗口原因丢失: %v", joined)
	}
	if !errors.Is(joined, contracts.ErrFontNotFound) {
		t.Fatalf("合并后字体诊断被吞: %v", joined)
	}
	text := joined.Error()
	if !strings.Contains(text, cause.Error()) || !strings.Contains(text, "font failed") {
		t.Fatalf("合并后文本缺失任一原因: %s", text)
	}
	if combineExitErrors(nil, nil) != nil {
		t.Fatal("两条都为空应返回 nil")
	}
}

// TestExitContractSingleWindowOrderAndOnce 顺序与一次语义：
// 帧 → destroy → callback → cleanup → font → result；重复关闭不二次回调；
// late worker 在结果生成前停止；严格字体失败与销毁错误并存时 Code=1。
func TestExitContractSingleWindowOrderAndOnce(t *testing.T) {
	destroyCause := errors.New("single window destroy")
	fontErr := fmt.Errorf("lazy font failed: %w", contracts.ErrFontNotFound)

	var mu sync.Mutex
	callbacks := 0
	view, err := WithOnDestroy(&countingView{text: "single"}, func() {
		mu.Lock()
		callbacks++
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{view: view, th: material.NewTheme()}
	a.lateStop = make(chan struct{})
	a.lateWake = nil // 合成帧不进入懒加载分支

	next, remaining := exitControlled(
		synthFrameEvent(&frameRecorder{}),
		gioapp.DestroyEvent{Err: destroyCause},
		gioapp.DestroyEvent{Err: destroyCause}, // 重复关闭
	)
	res, steps := a.driveSingleWindow(next, func() error { return fontErr })

	want := []string{"destroy", "callback", "cleanup", "font", "result"}
	got := steps.snapshot()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("执行顺序不符: got %v want %v", got, want)
	}
	if !res.CallbackDone || !res.CleanupDone {
		t.Fatalf("回调/清理标记未置位: %+v", res)
	}
	if a.lateStop != nil {
		t.Fatal("late worker 未停止")
	}
	mu.Lock()
	n := callbacks
	mu.Unlock()
	if n != 1 {
		t.Fatalf("WithOnDestroy 回调应恰好一次，实得 %d", n)
	}
	if len(remaining) != 1 {
		t.Fatalf("重复 DestroyEvent 被继续消费，剩余=%d", len(remaining))
	}
	if res.Code != 1 || !errors.Is(res.Err, destroyCause) ||
		!errors.Is(res.Err, ErrWindowDestroy) || !errors.Is(res.Err, contracts.ErrFontNotFound) {
		t.Fatalf("并存错误/退出码不符: %+v", res)
	}
	if strings.Join(res.Steps, ",") != strings.Join(want, ",") {
		t.Fatalf("ExitResult.Steps 未记录顺序: %v", res.Steps)
	}
}

// TestExitContractNormalCloseAndNonStrictFont 正控制：
// 正常关窗 + 非严格字体路径 → Code=0、Err=nil、两条错误字段皆 nil。
func TestExitContractNormalCloseAndNonStrictFont(t *testing.T) {
	a := &App{view: &countingView{text: "normal"}, th: material.NewTheme()}
	next, _ := exitControlled(gioapp.DestroyEvent{})
	res, _ := a.driveSingleWindow(next, func() error { return nil })
	if res.Code != 0 || res.Err != nil || res.DestroyErr != nil || res.FontErr != nil {
		t.Fatalf("正常关窗不应产生错误: %+v", res)
	}
	// 非严格路径：字体失败只在诊断里可读，不进入退出结果。
	res2, _ := a.driveSingleWindow(func() event.Event { return gioapp.DestroyEvent{} }, nil)
	if res2.Code != 0 || res2.Err != nil {
		t.Fatalf("非严格路径不应把字体诊断变成退出错误: %+v", res2)
	}
}

// TestExitContractRunWindowControlKeepsLibraryBoundary 控制形态核对：
// RunWindow* 不调用 Main、不调用进程退出、正常关窗返回 nil。
func TestExitContractRunWindowControlKeepsLibraryBoundary(t *testing.T) {
	orig := mainFunc
	t.Cleanup(func() { mainFunc = orig })
	mainCalled := make(chan struct{}, 1)
	mainFunc = func() { mainCalled <- struct{}{} }

	w := &exitProbeWindow{
		events:  make(chan event.Event, 1),
		actions: make(chan system.Action, 1),
	}
	w.events <- gioapp.DestroyEvent{}
	stop := make(chan struct{})
	defer close(stop)
	if err := runWindowEvents(w, &countingView{text: "control"}, material.NewTheme(), stop, nil); err != nil {
		t.Fatalf("正常关窗应返回 nil: %v", err)
	}
	select {
	case <-mainCalled:
		t.Fatal("RunWindow 控制形态调用了 Main")
	default:
	}
	// 测试进程仍然存活即证明库函数没有终止调用者进程。
	t.Log("OBS RunWindow 控制形态：Main 未调用、进程未被终止、正常关窗 nil")
}

// TestExitContractProcessBoundaryObserverBeforeExit 显式进程边界：
// Observer 在 Exit 之前、清理与回调先于结果、退出码由错误决定，
// 且注入的 Exit 不终止测试进程。
func TestExitContractProcessBoundaryObserverBeforeExit(t *testing.T) {
	exitStubMain(t)
	destroyCause := errors.New("process boundary destroy")

	var order []string
	var observed ExitResult
	opts := ProcessOptions{
		Theme: material.NewTheme(),
		Next:  func() event.Event { return gioapp.DestroyEvent{Err: destroyCause} },
		FontFailure: func() error {
			return fmt.Errorf("strict font: %w", contracts.ErrFontNotFound)
		},
		Observer: func(res ExitResult) {
			order = append(order, "observer")
			observed = res
		},
		Exit: func(code int) { order = append(order, fmt.Sprintf("exit:%d", code)) },
	}
	RunSingleWindowProcess(&countingView{text: "process"}, Config{}, opts)

	if len(order) != 2 || order[0] != "observer" || order[1] != "exit:1" {
		t.Fatalf("observer 必须先于进程退出: %v", order)
	}
	wantTail := []string{"observer", "exit"}
	got := observed.Steps
	if len(got) < len(wantTail)+5 {
		t.Fatalf("步骤不完整: %v", got)
	}
	if strings.Join(got[len(got)-2:], ",") != strings.Join(wantTail, ",") {
		t.Fatalf("observer/exit 顺序不符: %v", got)
	}
	if !observed.CleanupDone || !observed.CallbackDone {
		t.Fatalf("清理/回调未先于结果: %+v", observed)
	}
	if !errors.Is(observed.Err, destroyCause) || !errors.Is(observed.Err, contracts.ErrFontNotFound) {
		t.Fatalf("并存错误不可判别: %v", observed.Err)
	}
}

// TestExitContractThemeFailureBeforeAnyWindow 开窗前默认主题失败：
// 不建窗、不启动事件循环、不调用 Main；错误进入 ExitResult，Code=1。
func TestExitContractThemeFailureBeforeAnyWindow(t *testing.T) {
	orig := mainFunc
	t.Cleanup(func() { mainFunc = orig })
	mainCalled := make(chan struct{}, 1)
	mainFunc = func() { mainCalled <- struct{}{} }

	themeErr := fmt.Errorf("no font: %w", contracts.ErrFontNotFound)
	nextCalls := 0
	var observed ExitResult
	exited := make(chan int, 1)
	RunSingleWindowProcess(&countingView{text: "theme"}, Config{}, ProcessOptions{
		ThemeErr: themeErr,
		Next: func() event.Event {
			nextCalls++
			return gioapp.DestroyEvent{}
		},
		Observer: func(res ExitResult) { observed = res },
		Exit:     func(code int) { exited <- code },
	})
	select {
	case code := <-exited:
		if code != 1 {
			t.Fatalf("主题失败应退出 1，实得 %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("进程边界未收口")
	}
	if nextCalls != 0 || mainFuncCalled(mainCalled) {
		t.Fatalf("开窗前失败不应启动事件循环或 Main: next=%d main=%v", nextCalls, mainFuncCalled(mainCalled))
	}
	if !errors.Is(observed.Err, contracts.ErrFontNotFound) || observed.Code != 1 {
		t.Fatalf("主题失败未进入结果: %+v", observed)
	}
	if strings.Contains(strings.Join(observed.Steps, ","), "destroy") {
		t.Fatalf("开窗前失败不应产生事件循环步骤: %v", observed.Steps)
	}
}

func mainFuncCalled(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// TestExitContractStrictFontFailureThroughRealLazy 真实懒加载严格路径：
// 冻结文件 font_lazy_strict_test.go 不改，这里复用它的 failing lazy 夹具，
// 证明退出结果确实携带 contracts.ErrFontNotFound，而不是只靠注入字符串。
func TestExitContractStrictFontFailureThroughRealLazy(t *testing.T) {
	lz := newFailingLazy(t, nil)
	a, err := NewLazyApp(&countingView{text: "strict"}, lz, Config{StrictFontFailure: true})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)
	if lz.State() != theme.LoadFailed || !errors.Is(lz.Report().Err, contracts.ErrFontNotFound) {
		t.Fatalf("failing lazy 未进入带 ErrFontNotFound 的失败终态: state=%s err=%v", lz.State(), lz.Report().Err)
	}
	if got := a.fontFailure(); !errors.Is(got, contracts.ErrFontNotFound) {
		t.Fatalf("真实严格字体错误不可判别: %v", got)
	}
	next, _ := exitControlled(gioapp.DestroyEvent{})
	res, _ := a.driveSingleWindow(next, a.fontFailure)
	if res.Code != 1 || !errors.Is(res.Err, contracts.ErrFontNotFound) {
		t.Fatalf("真实严格字体错误未进入退出结果: %+v", res)
	}
}
