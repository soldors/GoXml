//go:build goxml_router_native_window

// This test opens a native window and is only for explicitly authorized runs.

package router_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/router"
)

// 本文件是**外部测试包**（router_test）：仅用于验证 router 根 View 与
// 项目既有 Gio 入口（app.RunWindowWithInvalidator）的真实事件循环兼容性。
// pkg/runtime/router 的包内依赖只有 gioui.org 与标准库——该安排已在
// B1 报告「与字体 Agent 的冲突检查」一节中说明。

type countPage struct{ calls atomic.Int64 }

func (p *countPage) Layout(_ layout.Context, _ *material.Theme) layout.Dimensions {
	p.calls.Add(1)
	return layout.Dimensions{}
}

type ctxRec struct {
	mu   sync.Mutex
	ctxs []context.Context
}

func (rec *ctxRec) factory(page router.View, err error) router.PageFactory {
	return func(ctx context.Context) (router.View, error) {
		rec.mu.Lock()
		rec.ctxs = append(rec.ctxs, ctx)
		rec.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return page, nil
	}
}

func (rec *ctxRec) lastCtx() context.Context {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.ctxs) == 0 {
		return nil
	}
	return rec.ctxs[len(rec.ctxs)-1]
}

func TestRouterInRealEventLoop(t *testing.T) {
	r := router.New()
	pageA, pageB := &countPage{}, &countPage{}
	recA, recB := &ctxRec{}, &ctxRec{}
	if err := r.Register("/first", recA.factory(pageA, nil)); err != nil {
		t.Fatalf("注册 /first: %v", err)
	}
	if err := r.Register("/second", recB.factory(pageB, nil)); err != nil {
		t.Fatalf("注册 /second: %v", err)
	}

	stop := make(chan struct{})
	done := make(chan error, 1)
	invReady := make(chan struct{})
	go func() {
		done <- app.RunWindowWithInvalidator(r.View(), material.NewTheme(),
			app.Config{Title: "router-b1-integration"}, stop,
			func(inv app.Invalidator) {
				// 运行时入口契约 §2：该回调在事件循环启动前同步执行。
				_ = r.SetInvalidator(inv)
				close(invReady)
			})
	}()

	select {
	case <-invReady:
	case <-time.After(5 * time.Second):
		close(stop)
		<-done
		t.Fatal("5 秒内未拿到 Invalidator（事件循环未启动）")
	}

	// 首次导航：Invalidator 已接线，Invalidate 可唤醒事件循环。
	if err := r.Navigate("/first"); err != nil {
		t.Fatalf("导航 /first: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return pageA.calls.Load() > 0 },
		"真实事件循环中 /first 未被渲染")
	if got := r.Current(); got != "/first" {
		t.Fatalf("Current 应为 /first，得到 %q", got)
	}

	// 真实循环中的页面切换。
	if err := r.Navigate("/second"); err != nil {
		t.Fatalf("导航 /second: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return pageB.calls.Load() > 0 },
		"真实事件循环中 /second 未被渲染")
	if got := r.Current(); got != "/second" {
		t.Fatalf("Current 应为 /second，得到 %q", got)
	}
	if err := recA.lastCtx().Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("切换后旧页 Context 应取消，得到: %v", err)
	}

	// 干净退出（运行时入口契约 §3 的测试形态）。
	close(stop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWindowWithInvalidator 应无错误返回: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop 后事件循环未退出")
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.After(d)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("%s（超时 %v）", msg, d)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
