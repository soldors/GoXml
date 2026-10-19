package app

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/widget/material"
)

// ============================================================================
// S3：App 句柄形态（NewApp / Run / Invalidate）的生产入口验证
//
// 设计依据：ADR-0010 实现项 + S3 切片决策（先持有句柄后阻塞）。
//
// 首帧前 Invalidate 的语义（本项目实测 + Gio 源码双重证据）：
//   - Gio 源码：mayInvalidate 零值 false，driver 未创建时 Invalidate 静默跳过；
//   - 本项目实测：真实 app.Window 上 Event() 之前连续两次 Invalidate 未 panic。
// 因此 App.Invalidate 不加就绪门；早期调用是「安全丢弃」语义，
// 已写入方法注释与运行时入口契约。
// ============================================================================

// TestNewAppValidatesParams 参数校验。
func TestNewAppValidatesParams(t *testing.T) {
	th := material.NewTheme()
	view := &countingView{text: "x"}

	if _, err := NewApp(nil, th, Config{}); err == nil {
		t.Error("nil view 应被拒绝")
	}
	if _, err := NewApp(view, nil, Config{}); err == nil {
		t.Error("nil theme 应被拒绝")
	}
	a, err := NewApp(view, th, Config{})
	if err != nil {
		t.Fatalf("合法参数创建失败: %v", err)
	}
	if a == nil {
		t.Fatal("返回 nil App")
	}
}

// TestAppInvalidateBeforeRunIsSafe 实测固化：Run 之前调用 Invalidate 安全。
//
// 这是「不能凭经验拍板」问题的实测证据，结论：
// 不 panic、不阻塞；mayInvalidate 零值 false → 静默跳过。
func TestAppInvalidateBeforeRunIsSafe(t *testing.T) {
	th := material.NewTheme()
	a, err := NewApp(&countingView{text: "x"}, th, Config{Title: "pre-inv"})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.Invalidate()
		a.Invalidate()
	}()

	select {
	case <-done:
		t.Log("Run 之前 Invalidate：安全返回（静默跳过语义）")
	case <-time.After(3 * time.Second):
		t.Fatal("Run 之前 Invalidate 阻塞")
	}
}

// TestAppRunDrivesAndInvalidateRefreshes 句柄形态的完整链路：
//
//	NewApp → 后台 goroutine 改状态 + Invalidate → Run 阻塞驱动
//	→ Layout 读到新值。
//
// mainFunc 注入为空函数，让 Run 在测试中可退出（stop 关闭窗口）。
func TestAppRunDrivesAndInvalidateRefreshes(t *testing.T) {
	orig := mainFunc
	defer func() { mainFunc = orig }()

	vm := &stateVM{Title: "AAAA"}
	view, err := newBindingView(vm)
	if err != nil {
		t.Fatal(err)
	}

	th := material.NewTheme()
	a, err := NewApp(view, th, Config{Title: "s3-handle", Width: 200, Height: 60})
	if err != nil {
		t.Fatal(err)
	}

	// Run 的 mainFunc 置为等待 stop——模拟「主线程被占住」的生产形态。
	stop := make(chan struct{})
	mainFunc = func() { <-stop }

	// 后台工作线程：改状态 + 失效（生产典型形态，先于 Run 启动）。
	go func() {
		// 等首帧出现（说明事件循环已在跑）。
		deadline := time.After(3 * time.Second)
		for {
			select {
			case got := <-view.observed:
				if got == "AAAA" {
					goto ready
				}
			case <-deadline:
				close(stop)
				return
			}
		}
	ready:
		vm.Set("WWWWWWWW")
		a.Invalidate()
	}()

	// Run 在 goroutine 中驱动（mainFunc 已注入，不会占住主线程）。
	done := make(chan error, 1)
	go func() { done <- a.Run() }()

	// 等新值出现。
	deadline := time.After(3 * time.Second)
	for {
		select {
		case got := <-view.observed:
			if got == "WWWWWWWW" {
				close(stop)
				<-done
				t.Logf("句柄形态链路生效：AAAA → WWWWWWWW（帧数=%d）", view.frames.Load())
				return
			}
		case <-deadline:
			close(stop)
			<-done
			t.Fatalf("3 秒内未观察到新值；帧数=%d", view.frames.Load())
		}
	}
}

// TestAppConcurrentInvalidate 并发失效不 panic（多 goroutine 同时请求）。
func TestAppConcurrentInvalidate(t *testing.T) {
	th := material.NewTheme()
	a, err := NewApp(&countingView{text: "x"}, th, Config{Title: "s3-conc"})
	if err != nil {
		t.Fatal(err)
	}

	var panicCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()
			for j := 0; j < 100; j++ {
				a.Invalidate()
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		if panicCount.Load() != 0 {
			t.Fatalf("并发 Invalidate 出现 %d 次 panic", panicCount.Load())
		}
		t.Log("8 goroutine × 100 次并发失效：无 panic")
	case <-time.After(5 * time.Second):
		t.Fatal("并发 Invalidate 阻塞")
	}
}

// layout.Context 引用保护（避免 import 未用）。
var _ = layout.Context{}
