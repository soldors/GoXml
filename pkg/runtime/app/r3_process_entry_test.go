//go:build !nativewindow

// R29-C R3：旧单窗进程入口在「Main 已被宿主占用」时的两套收口对照。
// 默认保持 R28 已批准契约（Observer 后退出进程）；MainConflictReturn 是
// 候选字段，只有调用方显式选择时才改为返回而不退出进程。
package app

import (
	"errors"
	"testing"

	"gioui.org/widget/material"
)

// 旧单窗进程入口的默认收口：Main 已被宿主占用时仍按 R28 契约退出进程。
// 用注入的 exit 观察真实调用序（真实进程里这一步会终止整个进程）。
func TestR3LegacyProcessEntryConflictExitsByDefault(t *testing.T) {
	host := NewWindowHost()
	w := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"held": w})
	if _, err := host.Open("held", &s1View{}, material.NewTheme(), Config{Title: "held"}); err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)

	// 宿主自己的事件泵会读事件；这里对比调用前后，证明被拒的旧入口没再触后端。
	// 先等宿主自己的泵完成首次 Event 进入，再对比被拒旧入口的增量。
	r1SpinUntil(t, "host pump entered its window Event", func() bool { return w.enters.Load() >= 1 })
	before, beforeCloses := w.enters.Load(), w.closes.Load()
	var obs ExitResult
	exits, code := 0, -1
	RunSingleWindowProcess(&s1View{}, Config{}, ProcessOptions{
		Theme:    material.NewTheme(),
		next:     s1Events(1),
		Observer: func(res ExitResult) { obs = res },
		exit:     func(c int) { exits++; code = c },
	})
	if exits != 1 || code != 1 {
		t.Fatalf("默认策略应走进程级退出一次（code=1），实得 exits=%d code=%d", exits, code)
	}
	if !errors.Is(obs.Err, ErrGioMainAlreadyOwned) {
		t.Fatalf("Observer 应看到可判别的所有权错误，实得 %v", obs.Err)
	}
	steps := obs.stringSteps()
	if r3StepIndex(steps, "observer") < 0 || r3StepIndex(steps, "exit") < 0 {
		t.Fatalf("默认收口应记录 observer/exit 阶段: %v", steps)
	}
	if w.enters.Load() != before || w.closes.Load() != beforeCloses {
		t.Fatalf("被拒的旧入口不得触后端: enters %d→%d closes %d→%d",
			before, w.enters.Load(), beforeCloses, w.closes.Load())
	}
	// 证据意义：默认策略下真实进程会在宿主仍持窗时退出；此处宿主仍存活。
	if got := host.Active(); got != 1 {
		t.Fatalf("宿主窗口应仍在服务，实得 %d", got)
	}
}

// 候选收口：MainConflictReturn 明确不退出进程，宿主与窗口保持可用。
func TestR3LegacyProcessEntryConflictCanReturnWithoutKillingHost(t *testing.T) {
	host := NewWindowHost()
	w := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"held": w})
	handle, err := host.Open("held", &s1View{}, material.NewTheme(), Config{Title: "held"})
	if err != nil {
		t.Fatal(err)
	}
	r3StartHost(t, host)

	// 先等宿主自己的泵完成首次 Event 进入，再对比被拒旧入口的增量。
	r1SpinUntil(t, "host pump entered its window Event", func() bool { return w.enters.Load() >= 1 })
	before, beforeCloses := w.enters.Load(), w.closes.Load()
	var obs ExitResult
	exits, code := 0, -1
	RunSingleWindowProcess(&s1View{}, Config{}, ProcessOptions{
		Theme:          material.NewTheme(),
		next:           s1Events(1),
		OnMainConflict: MainConflictReturn,
		Observer:       func(res ExitResult) { obs = res },
		exit:           func(c int) { exits++; code = c },
	})
	if exits != 0 {
		t.Fatalf("显式返回策略不得退出进程，实得 exits=%d code=%d", exits, code)
	}
	if !errors.Is(obs.Err, ErrGioMainAlreadyOwned) || obs.Code != 1 {
		t.Fatalf("Observer 仍应看到同一失败结果: err=%v code=%d", obs.Err, obs.Code)
	}
	steps := obs.stringSteps()
	if r3StepIndex(steps, "observer") < 0 || r3StepIndex(steps, "return") < 0 {
		t.Fatalf("返回策略应记录 observer/return 阶段: %v", steps)
	}
	if r3StepIndex(steps, "exit") >= 0 {
		t.Fatalf("返回策略不得记录 exit 阶段: %v", steps)
	}
	if w.enters.Load() != before || w.closes.Load() != beforeCloses {
		t.Fatalf("被拒的旧入口不得触后端: enters %d→%d closes %d→%d",
			before, w.enters.Load(), beforeCloses, w.closes.Load())
	}
	// 宿主仍可服务：给一帧、关窗、完整收口。
	w.frame()
	s2WaitFrames(t, handle, 1)
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	s2WaitDone(t, handle)
	if res := host.Wait(); !res.Drained || res.Closed != 1 {
		t.Fatalf("宿主应继续正常收口: %+v", res)
	}
}

// 健康对照：无宿主占用时旧单窗进程入口逐字保持 R28 契约（正常退出 0）。
func TestR3LegacyProcessEntryHealthyControl(t *testing.T) {
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	mainFunc = func() {}
	t.Cleanup(func() { mainFunc = orig })

	var obs ExitResult
	exits, code := 0, -1
	RunSingleWindowProcess(&s1View{}, Config{}, ProcessOptions{
		Theme:    material.NewTheme(),
		next:     s1Events(1),
		Observer: func(res ExitResult) { obs = res },
		exit:     func(c int) { exits++; code = c },
	})
	if exits != 1 || code != 0 || obs.Err != nil {
		t.Fatalf("健康路径应正常退出 0: exits=%d code=%d err=%v", exits, code, obs.Err)
	}
	if steps := obs.stringSteps(); r3StepIndex(steps, "exit") < 0 {
		t.Fatalf("健康路径应记录 exit 阶段: %v", steps)
	}
}
