package router

// R22-B：可合并失效请求的受控帧调度器。
// 失效请求可合并（多次 Invalidate 在两次泵之间只产生一帧）；帧只在有
// 未消费请求时驱动——**不能无条件手动 Layout 后就声称调度活性成立**：
// 余量任务必须由真实失效请求驱动才有后续帧。readyRef 用它锁定基线
// 「一次请求同帧排空、随后空闲」；candidateRef 用它验证有界预算下
// 「确有余量时请求后续帧、排空后停止」。

import (
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

type mergeableInv struct{ pending atomic.Int64 }

func (m *mergeableInv) Invalidate() { m.pending.Add(1) }

// TestR22SingleRequestDrainsAll 基线活性守卫（合并失效调度器）：
// 一次 Invalidate 驱动一帧，基线同帧排空全部 64 个独立回调——调度器随后
// 空闲（无未消费请求、无额外帧）。锁定「同帧排空=一次请求即完成」的现状。
func TestR22SingleRequestDrainsAll(t *testing.T) {
	r := New()
	inv := &mergeableInv{}
	if err := r.SetInvalidator(inv); err != nil {
		t.Fatal(err)
	}
	pa := &recPage{}
	if err := r.Register("/a", factoryOf(pa)); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	gtx := &layout.Context{Ops: new(op.Ops)}
	frame := func() {
		gtx.Reset()
		r.View().Layout(*gtx, material.NewTheme())
	}
	frame() // /a 生效

	const total = 64
	var executed atomic.Int64
	for i := 0; i < total; i++ {
		r.Dispatch(func() { executed.Add(1) })
	}
	// 调度器：只在有未消费失效请求时驱动帧（合并语义），上限 10 帧。
	frames := 0
	for frames < 10 {
		if inv.pending.Swap(0) == 0 {
			break
		}
		frame()
		frames++
		time.Sleep(time.Millisecond) // 让并发 Invalidate 到达（本测试无并发，纯顺序）
	}
	got := executed.Load()
	if frames != 1 {
		t.Fatalf("基线应一帧排空：帧数=%d", frames)
	}
	if got != total {
		t.Fatalf("基线应一次请求排空全部：执行 %d/%d", got, total)
	}
	if inv.pending.Load() != 0 {
		t.Fatalf("排空后不得有未消费失效请求，实际 %d", inv.pending.Load())
	}
	// 最终空闲：再泵一次不产生帧、不执行任务。
	if inv.pending.Swap(0) != 0 {
		t.Fatal("空闲状态下不得有新请求")
	}
	before := got
	frame()
	if executed.Load() != before {
		t.Fatal("空闲帧不得执行任务")
	}
}
