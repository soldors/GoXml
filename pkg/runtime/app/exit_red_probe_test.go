package app

import (
	"errors"
	"testing"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/widget/material"
)

// R27-C C2 先立反例：冻结实现把 DestroyEvent.Err 丢弃，这里要求它被交出。
// 在修复前这条用例必须**行为红**（不是编译失败、不是未启动）。
// 修复后它作为持久回归保留；首败原始输出在交付 raw/ 中，不重跑覆盖。
func TestExitRedDestroyCauseMustBeReturned(t *testing.T) {
	cause := errors.New("r27c premature destroy cause")

	w := &exitProbeWindow{
		events:  make(chan event.Event, 2),
		actions: make(chan system.Action, 1),
	}
	w.events <- gioapp.DestroyEvent{Err: cause}
	stop := make(chan struct{})
	defer close(stop)

	err := runWindowEvents(w, &countingView{text: "red"}, material.NewTheme(), stop, nil)
	if !errors.Is(err, cause) {
		t.Fatalf("DestroyEvent.Err 未交出：err=%v，期望 errors.Is(err, cause)", err)
	}

	// 正控制：正常关窗（Err=nil）必须返回 nil，不能因为修复就一律报错。
	w2 := &exitProbeWindow{
		events:  make(chan event.Event, 2),
		actions: make(chan system.Action, 1),
	}
	w2.events <- gioapp.DestroyEvent{}
	if err := runWindowEvents(w2, &countingView{text: "red"}, material.NewTheme(), stop, nil); err != nil {
		t.Fatalf("正常关窗应返回 nil，实得 %v", err)
	}
}

type exitProbeWindow struct {
	events  chan event.Event
	actions chan system.Action
}

func (w *exitProbeWindow) Event() event.Event      { return <-w.events }
func (w *exitProbeWindow) Perform(a system.Action) { w.actions <- a }
func (w *exitProbeWindow) Invalidate()             {}
