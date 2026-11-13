//go:build !nativewindow

// R29-C R5：Wait 不可变终态与启动结果脱钩的受控回归（默认无窗口集合）。
//
// 纯内存 windowEvents 只驱动生产 loopWindow 分支；合成帧不冒充原生
// FrameEvent / DestroyEvent。所有窗口关闭都在测试内显式投递受控事件。
package app

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/widget/material"
)

func r5Reset(t *testing.T) {
	t.Helper()
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	mainFunc = func() {}
	t.Cleanup(func() { mainFunc = orig })
}

// r5OpenHost 构造零窗或有窗宿主；有窗用 r3ProbeWindow 统计事件源进入次数。
func r5OpenHost(t *testing.T, withWindow bool) (*WindowHost, *r3ProbeWindow, *WindowHandle) {
	t.Helper()
	h := NewWindowHost()
	if !withWindow {
		return h, nil, nil
	}
	w := &r3ProbeWindow{s2Window: newS2Window()}
	r3UseWindows(t, map[string]windowEvents{"r5": w})
	wh, err := h.Open("r5", &s1View{}, material.NewTheme(), Config{Title: "r5"})
	if err != nil {
		t.Fatal(err)
	}
	return h, w, wh
}

func r5SameResult(a, b HostResult) bool {
	if a.Code != b.Code || a.Explicit != b.Explicit || a.Drained != b.Drained ||
		a.Rejected != b.Rejected || a.Canceled != b.Canceled || a.Closed != b.Closed ||
		len(a.Errors) != len(b.Errors) || a.Err != b.Err {
		return false
	}
	for i := range a.Errors {
		if !errors.Is(a.Errors[i], b.Errors[i]) {
			return false
		}
	}
	return true
}

// r5AssertFrozen 在 finished 已关闭后改写 h.result，再验证 Wait 仍返回
// 第一次的冻结终态：终态与窗口生命周期的后续写入完全隔离。
func r5AssertFrozen(t *testing.T, h *WindowHost, first HostResult) {
	t.Helper()
	h.mu.Lock()
	h.result.Rejected = !first.Rejected
	h.result.Code = 999
	h.result.Closed += 7
	h.mu.Unlock()
	again := h.Wait()
	if !r5SameResult(first, again) {
		t.Fatalf("Wait 终态被后续写入改变: first=%+v again=%+v", first, again)
	}
}

// TestR5WaitTerminalImmutable 覆盖 claim 成功/拒绝 × 零窗/有窗四个路径：
// 两次 Wait 一致、冻结后不可变、取消状态、事件源所有权与无残留守卫。
func TestR5WaitTerminalImmutable(t *testing.T) {
	cases := []struct {
		name       string
		rejected   bool
		withWindow bool
	}{
		{"claim-ok-zero", false, false},
		{"claim-ok-window", false, true},
		{"claim-rejected-zero", true, false},
		{"claim-rejected-window", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r5Reset(t)
			if tc.rejected {
				owner := NewWindowHost()
				if err := owner.Run(); err != nil {
					t.Fatalf("Main 持有者启动失败: %v", err)
				}
			}
			h, w, wh := r5OpenHost(t, tc.withWindow)
			runErr := h.Run()
			if tc.rejected {
				if !errors.Is(runErr, ErrGioMainAlreadyOwned) {
					t.Fatalf("应被明确拒绝，实得 %v", runErr)
				}
			} else if runErr != nil {
				t.Fatalf("claim 成功路径 Run 应返回 nil，实得 %v", runErr)
			}

			if !tc.rejected && tc.withWindow {
				// 有窗成功路径：合成一帧使 flushClose 就绪，再由受控
				// DestroyEvent 完成真实生产 loop 的自然关闭路径。
				w.frame()
				s2WaitFrames(t, wh, 1)
				w.events <- gioapp.DestroyEvent{}
				s2WaitDone(t, wh)
			}

			first, second := h.Wait(), h.Wait()
			if !r5SameResult(first, second) {
				t.Fatalf("两次 Wait 不一致: first=%+v second=%+v", first, second)
			}
			if first.Rejected != tc.rejected || !first.Drained {
				t.Fatalf("终态类别错误: %+v (rejected=%v)", first, tc.rejected)
			}
			if tc.rejected && !errors.Is(first.Err, ErrGioMainAlreadyOwned) {
				t.Fatalf("拒绝原因应保留: %+v", first)
			}
			if !tc.rejected && tc.withWindow && first.Closed != 1 {
				t.Fatalf("有窗收口应记 Closed=1，实得 %+v", first)
			}
			if tc.rejected && first.Closed != 0 {
				t.Fatalf("被拒宿主不得记 Closed，实得 %+v", first)
			}

			// 所有权诊断：被拒宿主计入 blockedCalls，成功宿主计入 calls。
			_, calls, blocked := gioMainState()
			if tc.rejected && blocked < 1 {
				t.Fatalf("被拒宿主应记 blockedCalls>=1: calls=%d blocked=%d", calls, blocked)
			}
			if !tc.rejected && calls < 1 {
				t.Fatalf("成功宿主应记 calls>=1: calls=%d blocked=%d", calls, blocked)
			}

			if tc.withWindow {
				if tc.rejected {
					if w.enters.Load() != 0 || w.closes.Load() != 0 || w.invalidates.Load() != 0 {
						t.Fatalf("被拒宿主不得进入事件源/触后端: enters=%d closes=%d invalidates=%d",
							w.enters.Load(), w.closes.Load(), w.invalidates.Load())
					}
					if !wh.Rejected() || wh.Started() || wh.Canceled() {
						t.Fatalf("被拒句柄状态异常: rejected=%t started=%t canceled=%t",
							wh.Rejected(), wh.Started(), wh.Canceled())
					}
					if !errors.Is(wh.Err(), ErrGioMainAlreadyOwned) {
						t.Fatalf("被拒句柄原因异常: %v", wh.Err())
					}
				} else if w.enters.Load() < 1 || !wh.Started() {
					t.Fatalf("成功宿主应启动事件泵: enters=%d started=%t", w.enters.Load(), wh.Started())
				}
				select {
				case <-wh.Done():
				default:
					t.Fatal("窗口句柄必须收口（无残留等待者）")
				}
			}
			if got := h.Active(); got != 0 {
				t.Fatalf("收口后不得有登记窗口（无残留）: %d", got)
			}

			r5AssertFrozen(t, h, first)
		})
	}
}

// TestR5CancelStatePreserved 验证 Run 前取消的窗口状态在冻结终态中保留。
func TestR5CancelStatePreserved(t *testing.T) {
	r5Reset(t)
	h, w, wh := r5OpenHost(t, true)
	if err := wh.Cancel(); err != nil {
		t.Fatal(err)
	}
	h.ExitAll(4)
	first, second := h.Wait(), h.Wait()
	if !r5SameResult(first, second) {
		t.Fatalf("两次 Wait 不一致: %+v / %+v", first, second)
	}
	if first.Canceled != 1 || !first.Explicit || first.Code != 4 || !first.Drained || first.Rejected {
		t.Fatalf("取消/显式退出状态未保留: %+v", first)
	}
	if w.enters.Load() != 0 || w.closes.Load() != 0 || w.invalidates.Load() != 0 {
		t.Fatalf("取消窗口不得触事件源/后端: %d/%d/%d", w.enters.Load(), w.closes.Load(), w.invalidates.Load())
	}
	if !wh.Canceled() || wh.Started() || wh.Rejected() {
		t.Fatalf("取消句柄状态异常: canceled=%t started=%t rejected=%t", wh.Canceled(), wh.Started(), wh.Rejected())
	}
	if !errors.Is(wh.Err(), errWindowCanceled) {
		t.Fatalf("取消原因应可判别: %v", wh.Err())
	}
	select {
	case <-wh.Done():
	default:
		t.Fatal("取消窗口必须立即收口")
	}
	r5AssertFrozen(t, h, first)
}

// r5Receive 带超时收集一个启动结果，避免等待者挂住测试。
func r5Receive(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatalf("%s 未在 3s 内返回", what)
		return nil
	}
}

// TestR5StartupWaitersDetachedFromLifecycle 证明：同一启动尝试的等待者在
// 快速销毁或 ExitAll 改变窗口生命周期状态之后，仍收到与胜者**相同**的
// 启动结果（不再按当前 h.state 推断）。概率性时序判据重复 8 次。
func TestR5StartupWaitersDetachedFromLifecycle(t *testing.T) {
	for _, mode := range []string{"exit-all", "fast-destroy"} {
		for attempt := 1; attempt <= 8; attempt++ {
			t.Run(fmt.Sprintf("%s-%02d", mode, attempt), func(t *testing.T) {
				r5Reset(t)
				previous := runtime.GOMAXPROCS(1)
				defer runtime.GOMAXPROCS(previous)
				h, w, wh := r5OpenHost(t, true)

				// 测试持有 gioMain.mu 阻塞 claimGioMain，制造确定性的
				// hostStarting 窗口：胜者与全部等待者都在 verdict 上等待。
				gioMain.mu.Lock()
				winner := make(chan error, 1)
				go func() { winner <- h.Run() }()
				r1SpinUntil(t, "hostStarting", func() bool {
					h.mu.Lock()
					defer h.mu.Unlock()
					return h.state == hostStarting
				})
				const waiters = 4
				results := make([]chan error, waiters)
				for i := range results {
					results[i] = make(chan error, 1)
					ch := results[i]
					go func() { ch <- h.Run() }()
				}
				// GOMAXPROCS(1) 下让出，使等待者进入 <-runVerdict。
				time.Sleep(10 * time.Millisecond)
				gioMain.mu.Unlock()

				win := r5Receive(t, winner, "胜者 Run")
				if win != nil {
					t.Fatalf("胜者应成功，实得 %v", win)
				}
				<-h.runVerdict // 发布点已过；此后改变窗口生命周期状态。

				switch mode {
				case "exit-all":
					h.ExitAll(9)
				case "fast-destroy":
					w.frame() // 使 driverReady 后受控销毁
					s2WaitFrames(t, wh, 1)
					w.events <- gioapp.DestroyEvent{}
					s2WaitDone(t, wh)
				}

				for i, ch := range results {
					got := r5Receive(t, ch, fmt.Sprintf("等待者 %d", i))
					if got != win {
						t.Fatalf("等待者 %d 收到与胜者不同的启动结果: got=%v winner=%v "+
							"（按当前宿主状态推断即会落到 errHostNotAcceptingRun）", i, got, win)
					}
				}

				// 无残留：exit-all 的 Close 需要一帧使 driverReady 后交付；
				// fast-destroy 路径已等过 Done。
				if mode == "exit-all" {
					w.frame()
					s2WaitFrames(t, wh, 1)
					s2WaitDone(t, wh)
				}
				_ = h.Wait()
				if got := h.Active(); got != 0 {
					t.Fatalf("收口后不得有登记窗口: %d", got)
				}
			})
		}
	}
}

// 编译期确认受控事件满足生产事件接口（防止 fake 与 Gio 事件脱节）。
var _ event.Event = gioapp.DestroyEvent{}
