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
// ============================================================================

// stateVM 是最小 ViewModel，自带锁（测试内的同步手段）。
type stateVM struct {
	mu    sync.Mutex
	Title string
}

func (m *stateVM) Set(t string) {
	m.mu.Lock()
	m.Title = t
	m.mu.Unlock()
}

func (m *stateVM) Get() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Title
}

// bindingView 与生成代码同构：构造时持有 vm 引用（ADR-0010），
// Layout(gtx, th) 两参（统一 View 接口）。
type bindingView struct {
	vm *stateVM
	// observed 记录每次 Layout 实际读到的显示值（证据核心）。
	observed chan string
	// frames 帧计数。
	frames atomic.Int32
}

var errNilVM = errorString("goxml: ViewModel is required; a binding page cannot render without state")

type errorString string

func (e errorString) Error() string { return string(e) }

func newBindingView(vm *stateVM) (*bindingView, error) {
	if vm == nil {
		return nil, errNilVM
	}
	return &bindingView{vm: vm, observed: make(chan string, 64)}, nil
}

func (v *bindingView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.frames.Add(1)
	t := v.vm.Get()
	select {
	case v.observed <- t:
	default:
	}
	return material.Body1(th, t).Layout(gtx)
}

// waitValue 从 ch 等一个值，超时返回空串。
func waitValue(t *testing.T, ch <-chan string, d time.Duration) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(d):
		return ""
	}
}
