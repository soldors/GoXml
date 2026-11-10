package lifecycle_test

// R28-C combination 后续收口：对 **公开** lifecycle.Owner / Handle API 的独立合同探针。
//
// 与作者/HY4 守卫的区别：本文件从「公开 API 的状态转换、幂等、线程所有权、
// Context 取消归属」角度重新验证，不复用其步骤 trace 助手；凡涉及后台线程
// 的调用都用真实 goroutine + WaitGroup/通道汇合，不用 sleep 猜顺序。
//
// 本文件还显式记录一个**文档/实现边界事实**（见
// TestCloseoutCancelOnlyContextOwnershipBoundary）：CancelOnly 只取消 owner
// 根 Context；当 MountHandle 传入非 nil 且独立于 owner 根的 parent 时，
// 页面 Handle.Context 的取消不发生在 CancelOnly，而在 Finalize 的
// unmountCurrent()。该事实连同 Router 侧的 r.rootCancel() 补偿写入 proposed
// ADR 的「需负责人裁决项」，本线不擅自改生产语义。

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/goxml/goxml/pkg/runtime/lifecycle"
)

type closeoutTarget struct {
	mu         sync.Mutex
	mounts     int
	unmounts   int
	mountCtx   context.Context
	unmountErr error
}

func (t *closeoutTarget) OnMount(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mounts++
	t.mountCtx = ctx
}

func (t *closeoutTarget) OnUnmount() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.unmounts++
	if t.mountCtx != nil {
		t.unmountErr = t.mountCtx.Err()
	}
}

func (t *closeoutTarget) counts() (int, int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mounts, t.unmounts, t.unmountErr
}

// TestCloseoutOwnerStateMachineAndCancelBeforeCleanup 验证 Created → Mounted →
// Unmounted 状态转换、一次清理，以及「先取消、再清理」（OnUnmount 观察到已取消 ctx）。
func TestCloseoutOwnerStateMachineAndCancelBeforeCleanup(t *testing.T) {
	o := lifecycle.NewOwner(nil)
	if o.Context().Err() != nil || o.Closed() || o.CleanupDone() {
		t.Fatal("新建 owner 必须是活的、未关闭、未清理")
	}
	tgt := &closeoutTarget{}
	h, err := o.NewHandle(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if h.State() != lifecycle.StateCreated || h.Context() != nil || h.Err() != nil {
		t.Fatalf("NewHandle 只构造不挂载: state=%v ctx=%v err=%v", h.State(), h.Context(), h.Err())
	}
	if s := o.Snapshot(); s.MountCalls != 0 || s.UnmountCalls != 0 || s.Posted != 0 {
		t.Fatalf("构造后快照应为零: %+v", s)
	}

	if err := o.MountHandle(h, nil); err != nil {
		t.Fatal(err)
	}
	if h.State() != lifecycle.StateMounted || h.Context() == nil || o.Current() != h {
		t.Fatalf("挂载后状态不符: state=%v ctxNil=%v current=%v", h.State(), h.Context() == nil, o.Current() == h)
	}
	if m, u, _ := tgt.counts(); m != 1 || u != 0 {
		t.Fatalf("挂载应只调用一次 OnMount: mount=%d unmount=%d", m, u)
	}
	if s := o.Snapshot(); s.MountCalls != 1 || s.UnmountCalls != 0 {
		t.Fatalf("挂载后计数: %+v", s)
	}

	o.UnmountCurrent()
	if h.State() != lifecycle.StateUnmounted || o.Current() != nil {
		t.Fatalf("卸载后应为终态且 current=nil: state=%v current=%v", h.State(), o.Current())
	}
	if !errors.Is(h.Err(), context.Canceled) {
		t.Fatalf("卸载必须取消页面 Context, got %v", h.Err())
	}
	m, u, unmountErr := tgt.counts()
	if m != 1 || u != 1 {
		t.Fatalf("卸载应只调用一次 OnUnmount: mount=%d unmount=%d", m, u)
	}
	if !errors.Is(unmountErr, context.Canceled) {
		t.Fatalf("OnUnmount 必须观察到已取消的 Context（先取消再清理）, got %v", unmountErr)
	}
	o.UnmountCurrent() // 幂等
	if _, u2, _ := tgt.counts(); u2 != 1 {
		t.Fatalf("UnmountCurrent 重复调用不得重复清理: %d", u2)
	}
	if s := o.Snapshot(); s.UnmountCalls != 1 {
		t.Fatalf("卸载计数应恰好一次: %+v", s)
	}
}

// TestCloseoutCancelOnlyNeverRunsUIUnmount 验证后台 CancelOnly 只取消/拒绝，
// 绝不执行 UI OnUnmount；随后 UI owner 的 Finalize 恰好完成一次清理。
func TestCloseoutCancelOnlyNeverRunsUIUnmount(t *testing.T) {
	o := lifecycle.NewOwner(nil)
	tgt := &closeoutTarget{}
	h, err := o.NewHandle(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.MountHandle(h, nil); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		o.CancelOnly() // 后台线程
	}()
	wg.Wait()

	if !o.Closed() || o.CleanupDone() {
		t.Fatalf("CancelOnly 应置 Closed=true / CleanupDone=false: closed=%v cleanup=%v",
			o.Closed(), o.CleanupDone())
	}
	if !errors.Is(o.Context().Err(), context.Canceled) {
		t.Fatalf("CancelOnly 必须取消 owner 根 Context, got %v", o.Context().Err())
	}
	if _, u, _ := tgt.counts(); u != 0 {
		t.Fatalf("CancelOnly 绝不能执行 UI OnUnmount: %d", u)
	}
	if s := o.Snapshot(); s.UnmountCalls != 0 || s.CleanupDone {
		t.Fatalf("CancelOnly 后快照: %+v", s)
	}

	o.RunPosted() // 关闭后不得执行任何排队更新
	if _, u, _ := tgt.counts(); u != 0 {
		t.Fatal("RunPosted 不得间接执行 UI 回调")
	}

	o.Finalize() // UI owner 销毁回调
	if !o.CleanupDone() {
		t.Fatal("Finalize 后 CleanupDone 必须为 true")
	}
	if _, u, _ := tgt.counts(); u != 1 {
		t.Fatalf("Finalize 应恰好一次清理: %d", u)
	}
	o.Finalize()
	if _, u2, _ := tgt.counts(); u2 != 1 || o.Snapshot().UnmountCalls != 1 {
		t.Fatalf("Finalize 必须幂等: unmounts=%d snap=%+v", u2, o.Snapshot())
	}
}

// TestCloseoutCancelOnlyContextOwnershipBoundary 精确记录 CancelOnly 的 Context
// 取消归属（proposed ADR 的待裁决事实，不是失败判据）：
//   - parent == nil 时 Handle.Context 派生自 owner 根，CancelOnly 连带取消；
//   - parent 为独立 Context 时 CancelOnly **不**取消 Handle.Context，
//     页面 Context 的取消发生在 Finalize 的 unmountCurrent()（h.cancel()）。
func TestCloseoutCancelOnlyContextOwnershipBoundary(t *testing.T) {
	t.Run("parent-nil", func(t *testing.T) {
		o := lifecycle.NewOwner(nil)
		h, err := o.NewHandle(&closeoutTarget{})
		if err != nil {
			t.Fatal(err)
		}
		if err := o.MountHandle(h, nil); err != nil {
			t.Fatal(err)
		}
		o.CancelOnly()
		if !errors.Is(h.Err(), context.Canceled) {
			t.Fatalf("parent=nil 时页面 ctx 应随 owner 根取消, got %v", h.Err())
		}
	})

	t.Run("independent-parent", func(t *testing.T) {
		parent, parentCancel := context.WithCancel(context.Background())
		defer parentCancel()
		o := lifecycle.NewOwner(context.Background())
		h, err := o.NewHandle(&closeoutTarget{})
		if err != nil {
			t.Fatal(err)
		}
		if err := o.MountHandle(h, parent); err != nil {
			t.Fatal(err)
		}
		o.CancelOnly()
		if !errors.Is(o.Context().Err(), context.Canceled) {
			t.Fatal("owner 根必须被 CancelOnly 取消")
		}
		if parent.Err() != nil || h.Err() != nil {
			t.Fatalf("独立 parent 的页面 ctx 不由 CancelOnly 取消（当前实现事实）: parent=%v handle=%v",
				parent.Err(), h.Err())
		}
		o.Finalize()
		if !errors.Is(h.Err(), context.Canceled) {
			t.Fatalf("Finalize 的 unmountCurrent 必须取消页面 ctx, got %v", h.Err())
		}
		if parent.Err() != nil {
			t.Fatal("Finalize 不应取消调用方自己的 parent Context")
		}
	})
}

// TestCloseoutFinalizeIsExactlyOnceUnderConcurrentCalls 验证「重复调用幂等」
// 在并发调用下仍只执行一次 UI OnUnmount（unmountCurrent 在锁内先清空 current）。
func TestCloseoutFinalizeIsExactlyOnceUnderConcurrentCalls(t *testing.T) {
	o := lifecycle.NewOwner(nil)
	tgt := &closeoutTarget{}
	h, err := o.NewHandle(tgt)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.MountHandle(h, nil); err != nil {
		t.Fatal(err)
	}
	const n = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			o.Finalize()
		}()
	}
	close(start)
	wg.Wait()
	if _, u, _ := tgt.counts(); u != 1 {
		t.Fatalf("并发 Finalize 必须只清理一次: %d", u)
	}
	if s := o.Snapshot(); s.UnmountCalls != 1 || !s.CleanupDone || !s.Closed {
		t.Fatalf("并发 Finalize 后快照: %+v", s)
	}
}

// TestCloseoutPostRejectedAfterCancelOnly 验证排队更新在「实际执行时」复查，
// 关闭后既丢弃已排队项，也不接受新投递。
func TestCloseoutPostRejectedAfterCancelOnly(t *testing.T) {
	o := lifecycle.NewOwner(nil)
	h, err := o.NewHandle(&closeoutTarget{})
	if err != nil {
		t.Fatal(err)
	}
	if err := o.MountHandle(h, nil); err != nil {
		t.Fatal(err)
	}
	applied := 0
	o.Post(h, func() { applied++ })
	o.RunPosted()
	if applied != 1 || o.Snapshot().Applied != 1 {
		t.Fatalf("当前代次的更新应被应用: applied=%d snap=%+v", applied, o.Snapshot())
	}

	o.Post(h, func() { applied++ }) // 投递时活跃，执行前被取消
	o.CancelOnly()
	o.RunPosted()
	if applied != 1 {
		t.Fatalf("取消后排队的迟到更新不得应用: %d", applied)
	}
	o.Post(h, func() { applied++ }) // 关闭后投递即刻拒绝
	if s := o.Snapshot(); s.Posted != 0 {
		t.Fatalf("关闭后不得入队: %+v", s)
	}
	o.RunPosted()
	if applied != 1 {
		t.Fatalf("关闭后不得执行任何投递: %d", applied)
	}
	if s := o.Snapshot(); s.Rejected < 2 {
		t.Fatalf("迟到/关闭投递应计入拒绝: %+v", s)
	}
}

// TestCloseoutClosedOwnerRejectsNewHandlesAndMounts 验证关闭态的公开 API 失败语义。
func TestCloseoutClosedOwnerRejectsNewHandlesAndMounts(t *testing.T) {
	o := lifecycle.NewOwner(nil)
	h, err := o.NewHandle(&closeoutTarget{})
	if err != nil {
		t.Fatal(err)
	}
	o.CancelOnly()
	if _, err := o.NewHandle(&closeoutTarget{}); !errors.Is(err, lifecycle.ErrOwnerClosed) {
		t.Fatalf("关闭后 NewHandle = %v", err)
	}
	if _, err := o.Switch(&closeoutTarget{}, nil); !errors.Is(err, lifecycle.ErrOwnerClosed) {
		t.Fatalf("关闭后 Switch = %v", err)
	}
	if err := o.MountHandle(h, nil); !errors.Is(err, lifecycle.ErrOwnerClosed) {
		t.Fatalf("关闭后 MountHandle = %v", err)
	}
	if h.State() != lifecycle.StateCreated {
		t.Fatalf("被拒绝的 Handle 不应进入挂载态: %v", h.State())
	}
	if err := o.MountHandle(nil, nil); !errors.Is(err, lifecycle.ErrWrongOwner) {
		t.Fatalf("nil Handle = %v", err)
	}
}

// TestCloseoutSwitchAndErrStale 验证换页代次、旧 Handle 不可重挂、ErrStale。
func TestCloseoutSwitchAndErrStale(t *testing.T) {
	o := lifecycle.NewOwner(nil)
	t1, t2 := &closeoutTarget{}, &closeoutTarget{}
	h1, err := o.Switch(t1, nil)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := o.Switch(t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h1.Generation() == h2.Generation() {
		t.Fatal("换页必须产生新的独立代次")
	}
	if h1.State() != lifecycle.StateUnmounted || h2.State() != lifecycle.StateMounted {
		t.Fatalf("旧页终态/新页挂载态不符: old=%v new=%v", h1.State(), h2.State())
	}
	if _, u, _ := t1.counts(); u != 1 {
		t.Fatalf("换页时旧页 OnUnmount 恰好一次: %d", u)
	}
	if err := o.ErrStale(h1); !errors.Is(err, lifecycle.ErrHandleStale) {
		t.Fatalf("旧代次应返回 ErrHandleStale, got %v", err)
	}
	if err := o.ErrStale(h2); err != nil {
		t.Fatalf("当前代次不应 stale: %v", err)
	}
	if err := o.MountHandle(h1, nil); !errors.Is(err, lifecycle.ErrHandleMounted) {
		t.Fatalf("旧 Handle 不可重挂 = %v", err)
	}
	if err := o.MountHandle(h2, nil); !errors.Is(err, lifecycle.ErrHandleMounted) {
		t.Fatalf("已挂载 Handle 不可重挂 = %v", err)
	}
	if err := o.ErrStale(nil); !errors.Is(err, lifecycle.ErrWrongOwner) {
		t.Fatalf("nil Handle 的 ErrStale = %v", err)
	}
}
