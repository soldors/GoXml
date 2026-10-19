package app

import (
	"testing"
	"time"

	"gioui.org/widget/material"
)

// ============================================================================
// S2 端到端：真实 app.Window 上的状态更新 → 失效 → 新帧 → 新值
//
// 与离屏测试（codegen 包）的分工：
//   - 离屏：绑定缓存命中/失效、渲染正确性（像素比对）
//   - 本文件：**真实事件循环**中「失效请求 → 后续 FrameEvent → Layout 读到新值」
//
// 诚实边界：
//   - 真实窗口受显示服务器/CI 影响：无图形会话时帧可能永不出现，
//     此时 Skip 并记录，**不得**用离屏测试冒充本链路证据；
//   - View 构造时持有 vm 引用，与生成代码同构（ADR-0010）；
//   - 另一 goroutine 修改 vm 用 mutex 同步——失效通知的并发安全
//     **不等于** vm 字段读写并发安全（状态更新契约 §2.3）。
//
// stateVM / bindingView / newBindingView 定义见 state_e2e_test.go。
// ============================================================================

// TestRealWindowStateRefresh 验证完整链路：
//
//	改 vm（带锁）→ inv.Invalidate() → 真实窗口后续 FrameEvent
//	→ Layout 读到新值。
//
// 证据设计（区分两件事）：
//   - frames 计数证明「确实出现了后续新帧」（不是只调用了 Invalidate）；
//   - observed 记录证明「新帧读到了新值」（不是布局跑了但值没变）。
func TestRealWindowStateRefresh(t *testing.T) {
	th := material.NewTheme()
	vm := &stateVM{Title: "AAAA"}

	v, err := newBindingView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var inv Invalidator
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- RunWindowWithInvalidator(v, th, Config{
			Title:  "s2-state",
			Width:  200,
			Height: 60,
		}, stop, func(i Invalidator) { inv = i })
	}()

	// 首帧：显示初始值。
	first := waitValue(t, v.observed, 3*time.Second)
	if first == "" {
		close(stop)
		<-done
		if v.frames.Load() == 0 {
			t.Skip("环境未产生任何帧（可能无图形会话）；真实窗口链路未经自动验证")
		}
		t.Fatal("首帧未观察到值")
	}
	if first != "AAAA" {
		t.Fatalf("首帧显示 %q, 期望 AAAA", first)
	}
	t.Logf("首帧显示初始值 AAAA（帧数=%d）", v.frames.Load())

	if inv == nil {
		close(stop)
		<-done
		t.Fatal("失效入口未交付")
	}

	// 改状态（锁同步）→ 请求失效。
	framesBefore := v.frames.Load()
	vm.Set("WWWWWWWW")
	inv.Invalidate()
	t.Log("已修改 vm 并请求失效")

	// 等后续帧读到新值。
	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-v.observed:
			if got == "WWWWWWWW" {
				t.Logf("后续帧显示新值（帧数 %d → %d）", framesBefore, v.frames.Load())
				if v.frames.Load() <= framesBefore {
					close(stop)
					<-done
					t.Fatal("观察到新值但帧数未增加——新值不是来自后续新帧")
				}
				close(stop)
				<-done
				return
			}
			// 排队中的旧值帧：继续等
		case <-deadline:
			close(stop)
			<-done
			t.Fatalf("3 秒内未观察到新值；总帧数=%d——失效→新帧→新值链路未生效", v.frames.Load())
		}
	}
}

// TestRealWindowNilVMRejected 验证 nil vm 明确拒绝（状态更新契约 §4）。
func TestRealWindowNilVMRejected(t *testing.T) {
	if _, err := newBindingView(nil); err == nil {
		t.Fatal("nil ViewModel 应被明确拒绝")
	}
}

// TestRealWindowConsecutiveUpdates 验证连续更新不残留旧文本。
func TestRealWindowConsecutiveUpdates(t *testing.T) {
	th := material.NewTheme()
	vm := &stateVM{Title: "AAAA"}
	v, err := newBindingView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var inv Invalidator
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- RunWindowWithInvalidator(v, th, Config{Title: "s2-multi"}, stop, func(i Invalidator) { inv = i })
	}()

	first := waitValue(t, v.observed, 3*time.Second)
	if first == "" {
		close(stop)
		<-done
		if v.frames.Load() == 0 {
			t.Skip("环境未产生任何帧；真实窗口链路未经自动验证")
		}
		t.Fatal("首帧未观察到值")
	}

	// 连续更新，每次失效后等新值按序出现。
	want := []string{"BBBB", "CCCCCC", "DD"}
	for _, next := range want {
		vm.Set(next)
		inv.Invalidate()

		deadline := time.After(3 * time.Second)
		ok := false
		for !ok {
			select {
			case got := <-v.observed:
				if got == next {
					ok = true
				}
			case <-deadline:
				close(stop)
				<-done
				t.Fatalf("未观察到 %q", next)
			}
		}
	}
	t.Logf("连续更新 %v 全部按序显示（最终帧数=%d）", want, v.frames.Load())
	close(stop)
	<-done
}
