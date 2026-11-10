package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"
)

// lifeTarget 复用 contracts 的 OnMount/OnUnmount（结构化实现，不新增契约）。
type lifeTarget struct {
	name        string
	mountCtx    context.Context
	mountCalls  int
	unmount     int
	unmountErr  error
	onMountFn   func(context.Context)
	onUnmountFn func()
}

func (t *lifeTarget) OnMount(ctx context.Context) {
	t.mountCalls++
	t.mountCtx = ctx
	if t.onMountFn != nil {
		t.onMountFn(ctx)
	}
}

func (t *lifeTarget) OnUnmount() {
	t.unmount++
	t.unmountErr = t.mountCtx.Err()
	if t.onUnmountFn != nil {
		t.onUnmountFn()
	}
}

func mustSwitch(t *testing.T, o *Owner, target any, parent context.Context) *Handle {
	t.Helper()
	h, err := o.Switch(target, parent)
	if err != nil {
		t.Fatalf("Switch 失败: %v", err)
	}
	return h
}

// TestLifecycleMountUnmountOrderAndOnce 先取消再清理，且清理只发生一次。
func TestLifecycleMountUnmountOrderAndOnce(t *testing.T) {
	o := NewOwner(context.Background())
	tgt := &lifeTarget{name: "page"}
	h := mustSwitch(t, o, tgt, nil)
	if tgt.mountCalls != 1 || h.Context() == nil || h.State() != StateMounted {
		t.Fatalf("挂载状态异常: mount=%d ctx=%v state=%v", tgt.mountCalls, h.Context(), h.State())
	}
	o.Finalize()
	o.Finalize() // 幂等
	if tgt.unmount != 1 {
		t.Fatalf("OnUnmount 应恰一次，实际 %d", tgt.unmount)
	}
	if !errors.Is(tgt.unmountErr, context.Canceled) || !errors.Is(h.Err(), context.Canceled) {
		t.Fatalf("清理回调未观察到已取消的 ctx: unmountErr=%v handleErr=%v", tgt.unmountErr, h.Err())
	}
	if o.Snapshot().UnmountCalls != 1 || !o.CleanupDone() {
		t.Fatalf("最终化计数/责任标记异常: %+v", o.Snapshot())
	}
}

// TestLifecycleFactoryConstructIsNotMounted 构造不等于挂载。
func TestLifecycleFactoryConstructIsNotMounted(t *testing.T) {
	o := NewOwner(context.Background())
	tgt := &lifeTarget{name: "pending"}
	h, err := o.NewHandle(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if h.State() != StateCreated || h.Context() != nil || tgt.mountCalls != 0 {
		t.Fatal("NewHandle 不得触发挂载或创建页面 Context")
	}
	if snap := o.Snapshot(); snap.MountCalls != 0 || snap.State != 0 {
		t.Fatalf("未发布页不应进入 owner 当前生命期: %+v", snap)
	}
	// 丢弃的未挂载 handle 不产生任何资源回调。
	o.Finalize()
	if tgt.unmount != 0 || tgt.mountCalls != 0 {
		t.Fatal("丢弃的在途页不应 Mount/Unmount")
	}
}

// TestLifecycleRepeatMountRejectedAndNewGeneration 重复挂载拒绝，重挂用新代次。
func TestLifecycleRepeatMountRejectedAndNewGeneration(t *testing.T) {
	o := NewOwner(context.Background())
	first := &lifeTarget{name: "first"}
	h1 := mustSwitch(t, o, first, nil)
	if err := o.MountHandle(h1, nil); !errors.Is(err, ErrHandleMounted) {
		t.Fatalf("重复 Mount 应被拒绝，实际 %v", err)
	}
	second := &lifeTarget{name: "second"}
	h2 := mustSwitch(t, o, second, nil)
	if h1.Generation() >= h2.Generation() {
		t.Fatal("重挂必须产生更高代次")
	}
	if first.unmount != 1 || !errors.Is(first.unmountErr, context.Canceled) {
		t.Fatal("切换时旧页未先取消再清理一次")
	}
	if o.ErrStale(h1) == nil || o.ErrStale(h2) != nil {
		t.Fatal("旧 handle 必须判为 stale")
	}
	if o.Snapshot().MountCalls != 2 || o.Snapshot().UnmountCalls != 1 {
		t.Fatalf("挂载/卸载计数异常: %+v", o.Snapshot())
	}
}

// TestLifecycleLateUpdateRejectedAtExecutionTime 关键：投递时仍活跃，
// 但在 owner 实际执行前生命期结束 -> 必须丢弃，业务无需手写检查。
func TestLifecycleLateUpdateRejectedAtExecutionTime(t *testing.T) {
	o := NewOwner(context.Background())
	tgt := &lifeTarget{name: "late"}
	h := mustSwitch(t, o, tgt, nil)
	applied := 0
	o.Post(h, func() { applied++ })
	if snap := o.Snapshot(); snap.Posted != 1 {
		t.Fatalf("更新未入队: %+v", snap)
	}
	o.Finalize()
	o.RunPosted()
	if applied != 0 {
		t.Fatal("取消后才执行的迟到更新被应用")
	}
	if snap := o.Snapshot(); snap.Rejected != 1 || snap.Applied != 0 {
		t.Fatalf("迟到拒绝计数异常: %+v", snap)
	}
}

// TestLifecycleStaleGenerationCannotWriteNewInstance 旧代次 Post 不写入新实例。
func TestLifecycleStaleGenerationCannotWriteNewInstance(t *testing.T) {
	o := NewOwner(context.Background())
	h1 := mustSwitch(t, o, &lifeTarget{name: "old"}, nil)
	h2 := mustSwitch(t, o, &lifeTarget{name: "new"}, nil)
	applied := 0
	o.Post(h1, func() { applied += 100 })
	o.Post(h2, func() { applied++ })
	o.RunPosted()
	if applied != 1 {
		t.Fatalf("旧代次更新污染了新生命期: applied=%d", applied)
	}
	if snap := o.Snapshot(); snap.Rejected != 1 || snap.Applied != 1 {
		t.Fatalf("拒绝/应用计数异常: %+v", snap)
	}
}

// TestLifecycleExternalCancelOnlyDoesNotRunUIUnmount 后台取消绝不执行 UI 清理，
// 且 owner 不再运行时 CleanupDone 保持 false（责任边界披露）。
func TestLifecycleExternalCancelOnlyDoesNotRunUIUnmount(t *testing.T) {
	o := NewOwner(context.Background())
	tgt := &lifeTarget{name: "bg"}
	h := mustSwitch(t, o, tgt, nil)
	o.Post(h, func() { t.Error("取消后的排队更新被应用") })
	o.CancelOnly()
	o.CancelOnly() // 幂等，不重复计数
	if tgt.unmount != 0 || o.Snapshot().UnmountCalls != 0 {
		t.Fatal("后台 CancelOnly 执行了 UI OnUnmount")
	}
	if !errors.Is(h.Err(), context.Canceled) {
		t.Fatal("后台取消未传播到页面 Context")
	}
	if o.CleanupDone() {
		t.Fatal("owner 未运行却宣称清理完成")
	}
	o.Finalize() // UI owner 真正运行一次
	if tgt.unmount != 1 || !o.CleanupDone() {
		t.Fatalf("owner 最终化未完成一次清理: unmount=%d done=%v", tgt.unmount, o.CleanupDone())
	}
	o.RunPosted()
	if snap := o.Snapshot(); snap.Applied != 0 {
		t.Fatalf("取消后排队的更新被应用: %+v", snap)
	}
}

// TestLifecycleParentContextCancellation 父子 Context 取消传播。
func TestLifecycleParentContextCancellation(t *testing.T) {
	root, rootCancel := context.WithCancel(context.Background())
	o := NewOwner(root)
	parent, parentCancel := context.WithCancel(root)
	tgt := &lifeTarget{name: "child"}
	h := mustSwitch(t, o, tgt, parent)
	applied := 0
	o.Post(h, func() { applied++ })
	parentCancel()
	o.RunPosted()
	if applied != 0 || !errors.Is(h.Err(), context.Canceled) {
		t.Fatal("父 Context 取消未传播或未拒绝更新")
	}
	o.Finalize()
	rootCancel()
	if tgt.unmount != 1 || !errors.Is(tgt.unmountErr, context.Canceled) {
		t.Fatal("最终化清理顺序异常")
	}
}

// TestLifecycleCallbacksRunOutsideStateLock 回调内合法回查不得死锁。
func TestLifecycleCallbacksRunOutsideStateLock(t *testing.T) {
	o := NewOwner(context.Background())
	tgt := &lifeTarget{name: "reenter"}
	tgt.onMountFn = func(context.Context) {
		_ = o.Snapshot()
		_ = o.Current()
		_ = o.Context().Err()
	}
	tgt.onUnmountFn = func() {
		_ = o.Snapshot()
		_ = o.CleanupDone()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		mustSwitch(t, o, tgt, nil)
		o.Finalize()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("生命周期回调持有了状态锁（死锁）")
	}
	if tgt.mountCalls != 1 || tgt.unmount != 1 {
		t.Fatalf("回入后计数异常: mount=%d unmount=%d", tgt.mountCalls, tgt.unmount)
	}
}

// TestLifecycleOnMountMayRequestSwitch OnMount 中合法切换（回入）不死锁，
// 且最终只保留新生命期。
func TestLifecycleOnMountMayRequestSwitch(t *testing.T) {
	o := NewOwner(context.Background())
	first := &lifeTarget{name: "first"}
	second := &lifeTarget{name: "second"}
	first.onMountFn = func(context.Context) {
		if _, err := o.Switch(second, nil); err != nil {
			t.Errorf("OnMount 内合法切换失败: %v", err)
		}
	}
	if _, err := o.Switch(first, nil); err != nil {
		t.Fatal(err)
	}
	if first.mountCalls != 1 || first.unmount != 1 || second.mountCalls != 1 {
		t.Fatalf("回入切换顺序异常: first=%+v second=%+v", first, second)
	}
	cur := o.Current()
	if cur == nil || cur.Target() != second {
		t.Fatal("回入后当前生命期不是新页")
	}
	o.Finalize()
	if second.unmount != 1 || first.unmount != 1 {
		t.Fatal("最终化重复清理")
	}
}

// TestLifecycleTwoOwnersIsolated 两个 owner 互不干扰。
func TestLifecycleTwoOwnersIsolated(t *testing.T) {
	a, b := NewOwner(context.Background()), NewOwner(context.Background())
	ta, tb := &lifeTarget{name: "a"}, &lifeTarget{name: "b"}
	ha := mustSwitch(t, a, ta, nil)
	hb := mustSwitch(t, b, tb, nil)
	if ha.owner != a || hb.owner != b {
		t.Fatal("handle 归属错误")
	}
	if err := a.MountHandle(hb, nil); !errors.Is(err, ErrWrongOwner) {
		t.Fatalf("跨 owner 挂载未拒绝: %v", err)
	}
	a.Finalize()
	if ta.unmount != 1 || tb.unmount != 0 || b.CleanupDone() {
		t.Fatal("一个 owner 的最终化影响了另一个")
	}
}

// TestLifecycleClosedOwnerRejectsNewHandle 关闭后不再产生新生命期。
func TestLifecycleClosedOwnerRejectsNewHandle(t *testing.T) {
	o := NewOwner(context.Background())
	o.Finalize()
	if _, err := o.NewHandle(&lifeTarget{name: "x"}); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("关闭后 NewHandle 未拒绝: %v", err)
	}
	if _, err := o.Switch(&lifeTarget{name: "y"}, nil); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("关闭后 Switch 未拒绝: %v", err)
	}
	if err := o.MountHandle(nil, nil); !errors.Is(err, ErrWrongOwner) {
		t.Fatalf("nil handle 未拒绝: %v", err)
	}
	o.CancelOnly()
	if !o.Closed() {
		t.Fatal("关闭态未标记")
	}
}

// TestLifecycleStateStringsAndDiagnostics 状态字符串与诊断入口。
func TestLifecycleStateStringsAndDiagnostics(t *testing.T) {
	cases := map[State]string{
		StateCreated: "created", StateMounted: "mounted",
		StateUnmounted: "unmounted", State(99): "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Fatalf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
	o := NewOwner(nil) // nil root 走 Background
	tgt := &lifeTarget{name: "diag"}
	h, err := o.NewHandle(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if h.Err() != nil || h.Context() != nil || h.Target() != tgt || h.Generation() == 0 {
		t.Fatalf("未挂载 handle 诊断异常: err=%v ctx=%v gen=%d", h.Err(), h.Context(), h.Generation())
	}
	if h.State() != StateCreated || o.Context() == nil {
		t.Fatal("初始状态或 owner Context 异常")
	}
	if err := o.ErrStale(nil); !errors.Is(err, ErrWrongOwner) {
		t.Fatalf("nil handle 未判为 ErrWrongOwner: %v", err)
	}
	other := NewOwner(context.Background())
	oh, err := other.NewHandle(&lifeTarget{name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.ErrStale(oh); !errors.Is(err, ErrWrongOwner) {
		t.Fatalf("跨 owner handle 未拒绝: %v", err)
	}
	cur := mustSwitch(t, o, tgt, nil)
	if err := o.ErrStale(cur); err != nil {
		t.Fatalf("当前 handle 不应判为 stale: %v", err)
	}
	if snap := o.Snapshot(); snap.Generation != cur.Generation() {
		t.Fatalf("快照代次不符: %+v", snap)
	}
}

// TestLifecycleUnmountCurrentExported 导出方法供 Router 接缝使用；幂等。
func TestLifecycleUnmountCurrentExported(t *testing.T) {
	o := NewOwner(context.Background())
	tgt := &lifeTarget{name: "exported"}
	h := mustSwitch(t, o, tgt, nil)
	o.UnmountCurrent()
	o.UnmountCurrent()
	if tgt.unmount != 1 || !errors.Is(h.Err(), context.Canceled) {
		t.Fatalf("UnmountCurrent 未恰好清理一次: %d %v", tgt.unmount, h.Err())
	}
	if o.Current() != nil {
		t.Fatal("清理后仍有当前生命期")
	}
}

// TestLifecyclePostGuards 投递守卫：nil 更新、跨 owner、关闭后投递。
func TestLifecyclePostGuards(t *testing.T) {
	o := NewOwner(context.Background())
	tgt := &lifeTarget{name: "post"}
	h := mustSwitch(t, o, tgt, nil)
	o.Post(h, nil) // nil 更新忽略
	// nil handle 表示「尚未挂载/已切换」的迟到结果：仍入队，由 RunPosted
	// 在实际执行时判为不活跃并拒绝，避免静默消失。
	o.Post(nil, func() { t.Error("nil handle 更新被应用") })
	other := NewOwner(context.Background())
	oh, err := other.NewHandle(&lifeTarget{name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	o.Post(oh, func() { t.Error("跨 owner 更新被应用") })
	if snap := o.Snapshot(); snap.Posted != 1 {
		t.Fatalf("仅 nil handle 的迟到结果应入队: %+v", snap)
	}
	o.RunPosted()
	if snap := o.Snapshot(); snap.Rejected != 1 || snap.Applied != 0 {
		t.Fatalf("迟到结果未在执行时被拒绝: %+v", snap)
	}
	o.Finalize()
	o.Post(h, func() { t.Error("关闭后更新被入队") })
	if snap := o.Snapshot(); snap.Rejected != 2 {
		t.Fatalf("关闭后投递未计入拒绝: %+v", snap)
	}
}
