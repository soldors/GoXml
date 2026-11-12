//go:build nativewindow

// judge_test.go 是**受控状态 / 日志回放**回归：不启动窗口、不投递
// gioapp.DestroyEvent、不执行 RunSingleWindowProcess；只把 marker 行与
// result.json 字段喂给 Judge / readyToClose / recorder 状态机。
// 这些用例证明的是「判据会拒绝缺证与错序」，不是原生验收结论。
package main

import (
	"context"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/goxml/goxml/pkg/runtime/app"
)

func fullShutdownFirstTrace() Trace {
	return Trace{
		Scenario: scenarioShutdownFirst,
		Markers: []string{
			"background-shutdown:unmounts=0 cleanup=false",
			"ready-to-close",
			"callback",
			"unmount:home",
			"finalize",
			"observer:callback=true cleanup=true code=0",
		},
		Steps: []string{
			"construct:home", "mount:home", "layout:home",
			"layout:home", "layout:home",
			"callback", "unmount:home", "finalize",
		},
		Mounts:                           1,
		Unmounts:                         1,
		MountBeforeFirstLayout:           true,
		PageContextCanceledBeforeUnmount: true,
		PageContextErrAtUnmount:          "context canceled",
		ObserverAfterCleanup:             true,
		ProcessCode:                      0,
		ShutdownObserved:                 true,
		ShutdownUnmounts:                 0,
		ShutdownCleanupDone:              false,
		ShutdownCalls:                    1,
		ShutdownCallsBeforeFinalize:      1,
		ShutdownCallsAtFinalize:          1,
		ReadyPublished:                   true,
		FrameRequests:                    2,
		FramesNeeded:                     3,
		CloseArrivedEarly:                false,
	}
}

func fullNaturalTrace() Trace {
	t := fullShutdownFirstTrace()
	t.Scenario = scenarioNatural
	t.Markers = []string{
		"callback", "unmount:home", "finalize",
		"observer:callback=true cleanup=true code=0",
	}
	t.Steps = []string{
		"construct:home", "mount:home", "layout:home",
		"callback", "unmount:home", "finalize",
	}
	t.ShutdownObserved = false
	t.ShutdownCalls = 0
	t.ShutdownCallsBeforeFinalize = 0
	t.ShutdownCallsAtFinalize = 0
	t.ReadyPublished = false
	t.FrameRequests = 0
	t.FramesNeeded = 0
	return t
}

func reasonCodes(v Verdict) string { return strings.Join(v.Reasons, " | ") }

func mustPass(t *testing.T, label string, tr Trace) {
	t.Helper()
	v := Judge(tr)
	if !v.Pass {
		t.Fatalf("%s: 期望 PASS，实得 FAIL reasons=%s", label, reasonCodes(v))
	}
}

func mustReject(t *testing.T, label, wantCode string, tr Trace) {
	t.Helper()
	v := Judge(tr)
	if v.Pass {
		t.Fatalf("%s: 期望拒绝，实得 PASS", label)
	}
	if !strings.Contains(reasonCodes(v), wantCode) {
		t.Fatalf("%s: 拒绝原因应含 %q，实得 %s", label, wantCode, reasonCodes(v))
	}
}

// 正向：两个场景的完整顺序都通过同一套判据。
func TestJudgeAcceptsFullNaturalOrder(t *testing.T) {
	mustPass(t, "natural 完整顺序", fullNaturalTrace())
}

func TestJudgeAcceptsFullShutdownFirstOrder(t *testing.T) {
	mustPass(t, "shutdown-first 完整顺序", fullShutdownFirstTrace())
}

// 反向：关窗早到（后台观测/ready 尚未就绪）必须拒判通过。
func TestJudgeRejectsEarlyClose(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.Markers = []string{
		"close-arrived-early:background shutdown observation missing",
		"callback", "unmount:home", "finalize",
		"observer:callback=true cleanup=true code=0",
		"background-shutdown:unmounts=0 cleanup=false",
		"ready-to-close",
	}
	tr.CloseArrivedEarly = true
	mustReject(t, "关窗早到", "close-arrived-early", tr)
}

// 反向：ready-to-close 写在 callback 之后（后台观测晚到）也要拒绝。
func TestJudgeRejectsReadyMarkerAfterClose(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.Markers = []string{
		"background-shutdown:unmounts=0 cleanup=false",
		"callback", "unmount:home", "finalize",
		"observer:callback=true cleanup=true code=0",
		"ready-to-close",
	}
	mustReject(t, "ready 晚于关窗", "close-arrived-early", tr)
}

// 反向：后台观测缺失不得判通过。
func TestJudgeRejectsMissingBackgroundObservation(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.Markers = []string{
		"callback", "unmount:home", "finalize",
		"observer:callback=true cleanup=true code=0",
	}
	tr.ShutdownObserved = false
	mustReject(t, "后台观测缺失", "missing-background-observation", tr)
}

// 反向：三条件不满足而写了 ready-refused 时拒绝。
func TestJudgeRejectsReadyRefused(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.Markers = []string{
		"background-shutdown:unmounts=1 cleanup=true",
		"ready-refused:OnUnmount already happened",
		"callback", "unmount:home", "finalize",
		"observer:callback=true cleanup=true code=0",
	}
	mustReject(t, "ready 条件被拒", "ready-refused", tr)
}

// 反向：把 finalize 写在 unmount 之前（旧手册的反序）必须拒绝。
func TestJudgeRejectsOldReversedMarkerOrder(t *testing.T) {
	tr := fullNaturalTrace()
	tr.Markers = []string{
		"callback", "finalize", "unmount:home",
		"observer:callback=true cleanup=true code=0",
	}
	mustReject(t, "旧反序 callback→finalize→unmount", "marker-order", tr)
}

// 反向：三个独立证明缺一不可。
func TestJudgeRequiresMountBeforeFirstLayout(t *testing.T) {
	tr := fullNaturalTrace()
	tr.MountBeforeFirstLayout = false
	mustReject(t, "首次 Layout 前未 Mount", "mount-before-first-layout", tr)
}

func TestJudgeRequiresContextCanceledBeforeUnmount(t *testing.T) {
	tr := fullNaturalTrace()
	tr.PageContextCanceledBeforeUnmount = false
	tr.PageContextErrAtUnmount = ""
	mustReject(t, "卸载前 Context 未取消", "page-context-canceled-before-unmount", tr)
}

func TestJudgeRequiresExactlyOneUnmount(t *testing.T) {
	tr := fullNaturalTrace()
	tr.Unmounts = 2
	mustReject(t, "OnUnmount 两次", "unmount-exactly-once", tr)
}

// 反向：fixture 侧显式调用计数在 Finalize 前后变化必须拒绝。
// 口径已收窄：这只是 fixture 日志自洽检查，**不证明** Router 内部行为；
// Router 内部行为由 r2_contract_test.go 的真实 Router 组合守卫证明。
func TestJudgeRejectsFixtureShutdownCountChanged(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.ShutdownCallsAtFinalize = 2
	mustReject(t, "fixture 显式计数变化", "fixture-shutdown-call-count-changed", tr)
}

// 反向：没有 fixture 主动请求的后续帧，不得判通过。
func TestJudgeRejectsMissingFrameRequests(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.FrameRequests = 0
	mustReject(t, "缺少确定性帧请求", "frame-requests", tr)
}

// readyToClose 的三条件单独验证：任一不满足即拒。
func TestReadyToCloseRequiresAllThreeConditions(t *testing.T) {
	cases := []struct {
		name string
		in   ReadyState
		want bool
	}{
		{"全部满足", ReadyState{ShutdownCompleted: true, Unmounts: 0, CleanupDone: false}, true},
		{"后台未完成", ReadyState{ShutdownCompleted: false, Unmounts: 0, CleanupDone: false}, false},
		{"已发生 OnUnmount", ReadyState{ShutdownCompleted: true, Unmounts: 1, CleanupDone: false}, false},
		{"CleanupDone 已为真", ReadyState{ShutdownCompleted: true, Unmounts: 0, CleanupDone: true}, false},
	}
	for _, c := range cases {
		got, reason := readyToClose(c.in)
		if got != c.want {
			t.Errorf("%s: readyToClose=%t want %t (reason=%q)", c.name, got, c.want, reason)
		}
		if !got && reason == "" {
			t.Errorf("%s: 拒绝时必须给出原因", c.name)
		}
	}
}

// 手册声明、红探针期望与机器判据三方必须逐字一致。
func TestManualAndMachineOrderAgree(t *testing.T) {
	if !equalOrder(manualWantMarkerOrder, ExpectedMarkerOrder) {
		t.Fatalf("marker 顺序不一致: manual=%v machine=%v", manualWantMarkerOrder, ExpectedMarkerOrder)
	}
	if !equalOrder(manualWantStepsOrder, ExpectedStepsOrder) {
		t.Fatalf("steps 顺序不一致: manual=%v machine=%v", manualWantStepsOrder, ExpectedStepsOrder)
	}
}

// 受控状态机：fixture 请求确定数量的后续帧并在之后停止，
// 不依赖操作员碰巧移动窗口。零值 layout.Context 的 Source 未启用，
// Execute 不会真正入队；这里只证明状态机确定性，不模拟原生帧投递。
func TestControlledFrameRequestsAreDeterministic(t *testing.T) {
	rec := &recorder{
		scenario:     scenarioShutdownFirst,
		framesNeeded: framesNeededForShutdown,
		ready:        make(chan struct{}),
	}
	page := &nativePage{rec: rec}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rec.onMount(ctx)
	for i := 0; i < framesNeededForShutdown; i++ {
		page.Layout(gtx, nil)
	}
	if rec.frameRequests != framesNeededForShutdown-1 {
		t.Fatalf("帧请求数=%d 期望 %d", rec.frameRequests, framesNeededForShutdown-1)
	}
	select {
	case <-rec.ready:
	default:
		t.Fatal("达到所需帧数后 ready 未通知后台")
	}
	page.Layout(gtx, nil) // 额外一帧不应再请求
	if rec.frameRequests != framesNeededForShutdown-1 {
		t.Fatalf("达到帧数后仍在请求: %d", rec.frameRequests)
	}
}

// 受控 recorder：首次 Layout 前 Mount、卸载前 Context 已取消、卸载恰一次，
// 并把这三个真实状态喂给 Judge 验证完整顺序通过。
func TestControlledRecorderProvesLifecycleInvariants(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := &recorder{scenario: scenarioNatural}
	rec.onMount(ctx)
	rec.noteLayout()
	rec.onUnmount()
	tr := rec.trace(app.ExitResult{CleanupDone: true, Code: 0})
	if !tr.MountBeforeFirstLayout {
		t.Fatal("第 1 次业务 Layout 时 mounts != 1")
	}
	if !tr.PageContextCanceledBeforeUnmount || tr.PageContextErrAtUnmount != "context canceled" {
		t.Fatalf("卸载前页面 Context 未取消: canceled=%t err=%q",
			tr.PageContextCanceledBeforeUnmount, tr.PageContextErrAtUnmount)
	}
	if tr.Mounts != 1 || tr.Unmounts != 1 {
		t.Fatalf("mounts=%d unmounts=%d，卸载必须恰一次", tr.Mounts, tr.Unmounts)
	}
	full := fullNaturalTrace()
	full.MountBeforeFirstLayout = tr.MountBeforeFirstLayout
	full.PageContextCanceledBeforeUnmount = tr.PageContextCanceledBeforeUnmount
	full.PageContextErrAtUnmount = tr.PageContextErrAtUnmount
	full.Mounts, full.Unmounts = tr.Mounts, tr.Unmounts
	mustPass(t, "受控 recorder 三证明 + 完整顺序", full)
}
