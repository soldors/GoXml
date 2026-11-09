package app

import (
	"errors"
	"fmt"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ============================================================================
// R27-C R1 / S1：懒加载与同步入口分流的**持久回归**
//
// 全部走真实公开入口 RunSingleWindowProcess（不直接调 driveSingleWindow，
// 也不总传 Theme 绕开分支）。协调者逐字探针副本 coord_r27_review_test.go
// 单独保留，不弱化、不改 Skip。
//
// 分流策略（候选，待裁决）：Lazy != nil → NewLazyApp，绝不读取同步默认
// Provider；Lazy == nil → 显式 Theme 或默认 Provider（B6 开窗前失败保留）；
// 显式 ThemeErr 两条路径都代表调用方声明的开窗前失败。
// ============================================================================

// r1StubSyncTheme 把同步默认主题入口换成可计数的失败 Provider。
func r1StubSyncTheme(t *testing.T, cause error) *int {
	t.Helper()
	orig := newThemeWithReport
	calls := new(int)
	t.Cleanup(func() { newThemeWithReport = orig })
	newThemeWithReport = func() (*material.Theme, *theme.ProbeReport) {
		(*calls)++
		return nil, &theme.ProbeReport{Err: cause}
	}
	return calls
}

// r1RunProcess 用注入的 Exit / 受控 main 调用**真实入口**并交出结果。
func r1RunProcess(t *testing.T, cfg Config, opts ProcessOptions) ExitResult {
	t.Helper()
	exitStubMain(t)
	var res ExitResult
	opts.Observer = func(r ExitResult) { res = r }
	opts.exit = func(int) {}
	RunSingleWindowProcess(&countingView{text: "r1"}, cfg, opts)
	return res
}

func r1HasStep(steps []string, name string) bool {
	for _, s := range steps {
		if s == name {
			return true
		}
	}
	return false
}

func r1DestroyNext() func() event.Event {
	ch := make(chan event.Event, 1)
	ch <- gioapp.DestroyEvent{}
	return func() event.Event { return <-ch }
}

// TestR1LazyProcessNeverConsultsSyncThemeProvider 与协调者同材料、判据更严：
// 不仅要求同步 Provider 调用 0 次与 Code=0，还要求真的进了事件循环
// （destroy/callback/cleanup/font/result 齐全）、回调与清理均已发生。
func TestR1LazyProcessNeverConsultsSyncThemeProvider(t *testing.T) {
	calls := r1StubSyncTheme(t, errors.New("unused synchronous provider failed"))
	lz := newFailingLazy(t, material.NewTheme())
	res := r1RunProcess(t, Config{StrictFontFailure: false}, ProcessOptions{
		Lazy: lz,
		next: r1DestroyNext(),
	})
	if *calls != 0 {
		t.Fatalf("Lazy 路径仍读取同步默认 Provider: calls=%d", *calls)
	}
	if res.Code != 0 || res.Err != nil {
		t.Fatalf("非 strict 正常关窗应 Code=0 且无错误: %+v", res)
	}
	for _, s := range []string{"destroy", "callback", "cleanup", "font", "result", "observer", "exit"} {
		if !r1HasStep(res.steps, s) {
			t.Fatalf("未真正进入事件循环（缺 %q）: %v", s, res.steps)
		}
	}
	if !res.CallbackDone || !res.CleanupDone {
		t.Fatalf("显式回调/late 清理未先于结果: %+v", res)
	}
	if lz.State() == theme.LoadUnloaded {
		t.Fatal("NewLazyApp 未启动异步加载（Lazy 仍为 Unloaded）")
	}
}

// TestR1LazyStrictProcessKeepsFontDiagnosticThroughRealLazy 走真实入口 +
// 真实 failing Lazy + StrictFontFailure，不注入 FontFailure；
// Next 用 Lazy.Done() 确定性等终态（不是 sleep 猜时序），再交出 DestroyEvent。
func TestR1LazyStrictProcessKeepsFontDiagnosticThroughRealLazy(t *testing.T) {
	calls := r1StubSyncTheme(t, errors.New("unused synchronous provider failed"))
	lz := newFailingLazy(t, material.NewTheme())
	next := func() event.Event {
		select {
		case <-lz.Done():
		case <-time.After(5 * time.Second):
		}
		return gioapp.DestroyEvent{}
	}
	res := r1RunProcess(t, Config{StrictFontFailure: true}, ProcessOptions{Lazy: lz, next: next})
	if *calls != 0 {
		t.Fatalf("strict Lazy 路径仍读取同步默认 Provider: calls=%d", *calls)
	}
	if lz.State() != theme.LoadFailed || !errors.Is(lz.Report().Err, contracts.ErrFontNotFound) {
		t.Fatalf("failing lazy 未进入 ErrFontNotFound 终态: state=%s err=%v", lz.State(), lz.Report().Err)
	}
	if res.Code != 1 || !errors.Is(res.Err, contracts.ErrFontNotFound) {
		t.Fatalf("严格字体诊断未从真实入口交出: %+v", res)
	}
	if res.FontErr == nil || !errors.Is(res.FontErr, contracts.ErrFontNotFound) {
		t.Fatalf("FontErr 丢失严格字体原因: %+v", res)
	}
}

// TestR1SyncDefaultProviderFailureFailsBeforeWindow B6 不变：
// 无 Lazy 的默认同步路径仍在**开窗前**因字体诊断失败返回 Code=1，
// 不把默认错误一概忽略、不静默降级。
func TestR1SyncDefaultProviderFailureFailsBeforeWindow(t *testing.T) {
	calls := r1StubSyncTheme(t, fmt.Errorf("default provider: %w", contracts.ErrFontNotFound))
	res := r1RunProcess(t, Config{}, ProcessOptions{next: r1DestroyNext()})
	if *calls != 1 {
		t.Fatalf("同步路径应恰好构建一次默认主题: calls=%d", *calls)
	}
	if res.Code != 1 || !errors.Is(res.Err, contracts.ErrFontNotFound) {
		t.Fatalf("默认主题失败未交出可判别错误: %+v", res)
	}
	if r1HasStep(res.steps, "destroy") || r1HasStep(res.steps, "callback") {
		t.Fatalf("开窗前失败不应启动事件循环: %v", res.steps)
	}
	if !r1HasStep(res.steps, "theme") {
		t.Fatalf("开窗前失败应记录 theme 阶段: %v", res.steps)
	}
}

// TestR1ExplicitThemeProcessSkipsSyncProvider 显式注入主题路径不读默认 Provider。
func TestR1ExplicitThemeProcessSkipsSyncProvider(t *testing.T) {
	calls := r1StubSyncTheme(t, errors.New("unused synchronous provider failed"))
	res := r1RunProcess(t, Config{}, ProcessOptions{
		Theme: material.NewTheme(),
		next:  r1DestroyNext(),
	})
	if *calls != 0 {
		t.Fatalf("显式 Theme 路径仍读取同步默认 Provider: calls=%d", *calls)
	}
	if res.Code != 0 || !r1HasStep(res.steps, "destroy") {
		t.Fatalf("显式 Theme 正常关窗应 Code=0 且进入事件循环: %+v", res)
	}
}

// TestR1LazyThemeErrStillFailsBeforeWindow 显式 ThemeErr 是调用方声明的
// 开窗前失败，与同步 Provider 无关，Lazy 路径也保留。
func TestR1LazyThemeErrStillFailsBeforeWindow(t *testing.T) {
	calls := r1StubSyncTheme(t, errors.New("unused synchronous provider failed"))
	lz := newFailingLazy(t, material.NewTheme())
	res := r1RunProcess(t, Config{}, ProcessOptions{
		Lazy:     lz,
		themeErr: fmt.Errorf("caller declared theme failure: %w", contracts.ErrFontNotFound),
		next:     r1DestroyNext(),
	})
	if *calls != 0 {
		t.Fatalf("ThemeErr 路径不应读取同步默认 Provider: calls=%d", *calls)
	}
	if res.Code != 1 || !errors.Is(res.Err, contracts.ErrFontNotFound) {
		t.Fatalf("显式 ThemeErr 未被交出: %+v", res)
	}
	if r1HasStep(res.steps, "destroy") {
		t.Fatalf("开窗前失败不应启动事件循环: %v", res.steps)
	}
}

// TestR1LazyProcessPreservesTemporaryThemeAndAsyncLoad 正常关窗的非 strict
// Lazy 路径：临时主题/异步加载行为保留，字体失败不误报为进程错误。
func TestR1LazyProcessPreservesTemporaryThemeAndAsyncLoad(t *testing.T) {
	calls := r1StubSyncTheme(t, errors.New("unused synchronous provider failed"))
	lz := newFailingLazy(t, material.NewTheme())
	res := r1RunProcess(t, Config{StrictFontFailure: false}, ProcessOptions{
		Lazy: lz,
		next: r1DestroyNext(),
	})
	requireTerminal(t, lz)
	if lz.State() != theme.LoadFailed {
		t.Fatalf("异步加载未走到失败终态: %s", lz.State())
	}
	if lz.Theme() == nil {
		t.Fatal("临时主题丢失（Lazy.Theme() 为 nil）")
	}
	if *calls != 0 || res.Code != 0 || res.FontErr != nil {
		t.Fatalf("非 strict 不应把懒加载失败升级为进程错误: calls=%d res=%+v", *calls, res)
	}
}
