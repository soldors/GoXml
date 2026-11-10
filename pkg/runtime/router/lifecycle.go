package router

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gioui.org/layout"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/lifecycle"
)

// ============================================================================
// R28-C / TASK-LIFE-001-1、001-2：真实 Router 的**显式 opt-in** 生命周期接缝。
//
// 设计要点：
//   - 未 EnableLifecycle 的 Router 行为、PageFactory、Shutdown/计数/错误合同
//     完全不变（router.go 里所有新逻辑都在 life != nil 分支，零分配开销）。
//   - 唯一入口是显式包装 LifecyclePage；直接复用 contracts.LifecycleMount /
//     LifecycleUnmount，不反射扫描 VM、不新增第二套回调契约。
//   - 工厂构造 != 挂载：applyNavigation 成功发布后只记录 pending，
//     在 renderCurrent 的**首次业务 Layout 之前**才提交 Mount。
//   - UI owner 负责全部回调；后台 Shutdown 只 CancelOnly（绝不跑 OnUnmount）。
//   - lifecycle 核心不 import 本包；本文件是单向消费方。
// ============================================================================

var (
	// ErrLifecycleAlreadyEnabled 表示重复 opt-in。
	ErrLifecycleAlreadyEnabled = errors.New("router: lifecycle already enabled")
	// ErrLifecycleAfterNavigation 表示导航后才 opt-in（已发布页没有生命期记录）。
	ErrLifecycleAfterNavigation = errors.New("router: lifecycle must be enabled before the first navigation")
	// ErrLifecycleClosed 表示 Router 已关闭。
	ErrLifecycleClosed = errors.New("router: closed")
	// ErrLifecycleNotEnabled 表示未 opt-in 却调用生命周期专属接口。
	ErrLifecycleNotEnabled = errors.New("router: lifecycle is not enabled")
)

// LifecyclePage 是**唯一**显式包装入口：把普通 View 与生命周期目标绑在一起。
// 生成 View 不需要转发任何 VM 接口；Mount/Unmount 可为 nil（无回调页）。
// 它同时实现 router.View、contracts.LifecycleMount 与 LifecycleUnmount。
type LifecyclePage struct {
	View    View
	Mount   func(context.Context)
	Unmount func()
}

func (p *LifecyclePage) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return p.View.Layout(gtx, th)
}

func (p *LifecyclePage) OnMount(ctx context.Context) {
	if p.Mount != nil {
		p.Mount(ctx)
	}
}

func (p *LifecyclePage) OnUnmount() {
	if p.Unmount != nil {
		p.Unmount()
	}
}

var (
	_ contracts.LifecycleMount   = (*LifecyclePage)(nil)
	_ contracts.LifecycleUnmount = (*LifecyclePage)(nil)
	_ View                       = (*LifecyclePage)(nil)
)

type lifecyclePending struct {
	handle *lifecycle.Handle
	ctx    context.Context
}

// lifecycleBridge 把 Router 的成功提交点接到中立 lifecycle.Owner。
// mu 只保护 pending（后台 Shutdown 与 UI owner 可能并发访问）。
type lifecycleBridge struct {
	owner   *lifecycle.Owner
	mu      sync.Mutex
	pending *lifecyclePending
	// finalized 保证销毁回调的 owner 最终化只执行一次（Owner 也已幂等）。
	finalized bool
}

func newLifecycleBridge(root context.Context) *lifecycleBridge {
	return &lifecycleBridge{owner: lifecycle.NewOwner(root)}
}

// onSwitch 在 **UI owner**（applyNavigation 锁外）执行：
// 旧页 Context 先取消 -> 旧生命期取消并 OnUnmount 一次 -> 仅记录新页 pending。
// 失败工厂 / 被合并导航 / 关闭先赢锁的在途结果都不会走到这里。
func (b *lifecycleBridge) onSwitch(oldCancel context.CancelFunc, page View, pageCtx context.Context) {
	if oldCancel != nil {
		oldCancel()
	}
	b.owner.UnmountCurrent()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.owner.Closed() || b.finalized {
		b.pending = nil
		return
	}
	h, err := b.owner.NewHandle(page)
	if err != nil {
		b.pending = nil
		return
	}
	b.pending = &lifecyclePending{handle: h, ctx: pageCtx}
}

// mountPending 在 renderCurrent 的首次业务 Layout 之前由 UI owner 调用。
// 关闭后不再挂载；未发布的页不会被记录，因此不会走到这里。
func (b *lifecycleBridge) mountPending() {
	b.mu.Lock()
	p := b.pending
	b.pending = nil
	b.mu.Unlock()
	if p == nil || b.owner.Closed() {
		return
	}
	_ = b.owner.MountHandle(p.handle, p.ctx)
}

// clearPending 丢弃尚未挂载的在途页（后台 Shutdown / UI 销毁回调共用）。
func (b *lifecycleBridge) clearPending() {
	b.mu.Lock()
	b.pending = nil
	b.mu.Unlock()
}

// cancelOnly 由后台 Shutdown 调用：只取消/拒绝，绝不执行 UI OnUnmount。
func (b *lifecycleBridge) cancelOnly() {
	b.owner.CancelOnly()
	b.clearPending()
}

// finalize 由 UI owner 的销毁回调调用：一次完成取消 + OnUnmount，
// 不依赖关闭后再 Dispatch 或下一帧 Layout。
func (b *lifecycleBridge) finalize() {
	b.mu.Lock()
	if b.finalized {
		b.mu.Unlock()
		return
	}
	b.finalized = true
	b.pending = nil
	b.mu.Unlock()
	b.owner.Finalize()
}

func (b *lifecycleBridge) runPosted() { b.owner.RunPosted() }

func (b *lifecycleBridge) post(h *lifecycle.Handle, fn func()) {
	b.owner.Post(h, fn)
}

// EnableLifecycle 在首次导航前显式开启生命周期。未开启时 Router 的全部
// 既有行为保持原样；重复开启返回错误。
func (r *Router) EnableLifecycle() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.life != nil {
		return ErrLifecycleAlreadyEnabled
	}
	if err := r.rootCtx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrLifecycleClosed, err)
	}
	if r.current != "" || r.pendingNav != "" {
		return ErrLifecycleAfterNavigation
	}
	r.life = newLifecycleBridge(r.rootCtx)
	return nil
}

// LifecycleOwner 返回该 Router 的中立生命周期 owner（诊断与高级用法）。
func (r *Router) LifecycleOwner() (*lifecycle.Owner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.life == nil {
		return nil, ErrLifecycleNotEnabled
	}
	return r.life.owner, nil
}

// LifecyclePost 从任意 goroutine 把更新投递给 UI owner。
// 关键：入队时不执行；owner 在帧首 RunPosted 的**实际执行时**复查当前
// 代次/挂载/Context，取消或换页后的迟到结果被丢弃并计数，业务无需手写检查。
// 未 opt-in 时为 no-op，不改变普通 Router 语义。
func (r *Router) LifecyclePost(fn func()) {
	if fn == nil {
		return
	}
	r.mu.Lock()
	life := r.life
	r.mu.Unlock()
	if life == nil {
		return
	}
	life.post(life.owner.Current(), fn)
	r.Invalidate()
}

// LifecycleFinalize 由 **UI owner 的销毁回调**调用一次（例如
// app.WithOnDestroy(r.View(), r.LifecycleFinalize)）。
// 重复调用幂等；后台 Shutdown 绝不调用本方法。
func (r *Router) LifecycleFinalize() {
	r.mu.Lock()
	life := r.life
	r.mu.Unlock()
	if life != nil {
		life.finalize()
	}
}
