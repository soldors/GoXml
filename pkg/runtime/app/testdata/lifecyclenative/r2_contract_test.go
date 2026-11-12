//go:build nativewindow

// r2_contract_test.go 是 R28-C2 的**受控状态 / 真实 Router 组合**回归：
// 不启动窗口、不投递 gioapp.DestroyEvent、不执行 RunSingleWindowProcess。
// A 组证明唯一就绪发布点（未发布/已发布正对照、写入失败、早到后补写不翻案）；
// B 组用公开 router.Context() 证明 LifecycleFinalize 不隐式触发 Shutdown
// 的可观察副作用，并与真实 r.Shutdown() 对照区分。
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/pkg/runtime/router"
)

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

const backgroundMarkerLine = "background-shutdown:unmounts=0 cleanup=false"
const observerMarkerLine = "observer:callback=true cleanup=true code=0"

func newObservedRecorder(t *testing.T) (*recorder, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "marker.txt")
	r := &recorder{scenario: scenarioShutdownFirst}
	if err := r.writeMarker(path, backgroundMarkerLine); err != nil {
		t.Fatalf("write background marker: %v", err)
	}
	r.noteShutdownObservation(0, false)
	return r, path
}

// A1 未发布正对照：后台观测已完成，但没有任何已落盘的 ready marker，
// 关窗检查必须判早到（协调者 C1-01 的精确交错）。
func TestUnpublishedCloseIsEarly(t *testing.T) {
	r, path := newObservedRecorder(t)
	r.setReadyWritten() // 遗留 no-op：修复后不再存在可提前置位的 flag
	early, reason := r.checkCloseArrival()
	if !early {
		t.Fatalf("close accepted before publication: reason=%q", reason)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), markerReady) {
		t.Fatalf("ready marker must not exist yet: %q", content)
	}
}

// A2 已发布正对照：唯一发布点成功后关窗检查才允许通过，
// 且 marker.txt 与 recorder.markers 都含同一行。
func TestPublishedCloseIsAccepted(t *testing.T) {
	r, path := newObservedRecorder(t)
	if err := r.publishReady(); err != nil {
		t.Fatalf("publishReady: %v", err)
	}
	if !r.readyPublished {
		t.Fatal("readyPublished not set after durable write")
	}
	early, reason := r.checkCloseArrival()
	if early {
		t.Fatalf("close rejected after publication: reason=%q", reason)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), markerReady) {
		t.Fatalf("ready marker missing on disk: %q", content)
	}
	if countKey(r.markers, "ready-to-close") != 1 {
		t.Fatalf("ready marker not in recorder snapshot: %v", r.markers)
	}
}

// A3 写入失败：发布点必须返回 error、**不发布**，关窗检查仍判早到，
// 磁盘上不留 ready marker。用真实不存在目录触发 OpenFile 失败。
func TestReadyPublishFailsOnWriteError(t *testing.T) {
	r, goodPath := newObservedRecorder(t)
	badPath := filepath.Join(filepath.Dir(goodPath), "missing-subdir", "marker.txt")
	r.setMarkerPath(badPath)
	if err := r.publishReady(); err == nil {
		t.Fatal("publishReady must fail when marker.txt cannot be written")
	}
	if r.readyPublished {
		t.Fatal("ready published despite write failure")
	}
	if r.readyPublishErr == "" {
		t.Fatal("write failure was not recorded")
	}
	early, reason := r.checkCloseArrival()
	if !early {
		t.Fatalf("close accepted after failed publication: reason=%q", reason)
	}
	content, err := os.ReadFile(goodPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), markerReady) {
		t.Fatalf("ready marker leaked into earlier file: %q", content)
	}
}

// A4 早到后补写仍不通过：先记录早到，再让发布点成功写 ready；
// closeArrivedEarly 不可逆，Judge 仍因 close-arrived-early / ready 晚于
// callback 拒判通过。不得靠随后补写 marker 修复先到的关闭。
func TestLateReadyPublishCannotRescueEarlyClose(t *testing.T) {
	r, path := newObservedRecorder(t)
	early, reason := r.checkCloseArrival()
	if !early {
		t.Fatal("precondition: close must be early before publication")
	}
	for _, line := range []string{
		markerEarlyPrefix + reason,
		markerCallback, markerUnmount, markerFinalize, observerMarkerLine,
	} {
		if err := r.writeMarker(path, line); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}
	if err := r.publishReady(); err != nil {
		t.Fatalf("late publishReady: %v", err)
	}
	if !r.closeArrivedEarly {
		t.Fatal("publishReady cleared the irreversible early-close verdict")
	}
	tr := fullShutdownFirstTrace()
	tr.Markers = append([]string(nil), r.markers...)
	tr.CloseArrivedEarly = r.closeArrivedEarly
	tr.ReadyPublished = true
	v := Judge(tr)
	if v.Pass {
		t.Fatalf("late ready write rescued an early close: %v", tr.Markers)
	}
	if !strings.Contains(strings.Join(v.Reasons, " | "), "close-arrived-early") {
		t.Fatalf("rejection reasons must name close-arrived-early: %v", v.Reasons)
	}
}

// B0 真实 Router 组合：EnableLifecycle + Register + Navigate。
func newRealRouter(t *testing.T) *router.Router {
	t.Helper()
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatalf("EnableLifecycle: %v", err)
	}
	page := &nativePage{rec: &recorder{scenario: scenarioNatural}}
	if err := r.Register("/home", func(ctx context.Context) (router.View, error) {
		return &router.LifecyclePage{
			View:    page,
			Mount:   func(context.Context) {},
			Unmount: func() {},
		}, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Navigate("/home"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	return r
}

// B1 真实 Router 正例：公开 router.Context() 的根在 LifecycleFinalize
// 前后仍活跃。若 Finalize 隐式触发 Shutdown，该根会被取消，此测试必红。
func TestRealRouterFinalizeKeepsRootContextActive(t *testing.T) {
	r := newRealRouter(t)
	if err := r.Context().Err(); err != nil {
		t.Fatalf("root context already canceled before Finalize: %v", err)
	}
	r.LifecycleFinalize()
	if err := r.Context().Err(); err != nil {
		t.Fatalf("LifecycleFinalize implicitly shut down the Router root: %v", err)
	}
}

// B2 真实 Router 反例（可区分对照）：显式 r.Shutdown() 确实取消根 Context。
// 与 B1 一起证明「根是否仍活跃」能区分 Finalize 与 Shutdown。
func TestRealRouterShutdownControlCancelsRootContext(t *testing.T) {
	r := newRealRouter(t)
	r.Shutdown()
	if !errors.Is(r.Context().Err(), context.Canceled) {
		t.Fatalf("Shutdown control did not cancel root: err=%v", r.Context().Err())
	}
}

// B3 把真实 Router 的观察值喂给 Judge：natural 完整顺序仍通过。
func TestRealRouterObservationFeedsJudge(t *testing.T) {
	r := newRealRouter(t)
	before := r.Context().Err()
	r.LifecycleFinalize()
	after := r.Context().Err()
	tr := fullNaturalTrace()
	tr.RouterRootErrBeforeFinalize = errString(before)
	tr.RouterRootErrAfterFinalize = errString(after)
	mustPass(t, "真实 Router 根观察值 + natural 完整顺序", tr)
}

// B4 判据反例：若日志显示 Finalize 后根已取消，Judge 必须拒绝。
func TestJudgeRejectsRouterRootCanceledAfterFinalize(t *testing.T) {
	tr := fullNaturalTrace()
	tr.RouterRootErrAfterFinalize = "context canceled"
	mustReject(t, "Router 根在 Finalize 后被取消", "router-root-canceled-after-finalize", tr)
}

// B5 判据反例：就绪未发布不得判通过。
func TestJudgeRejectsReadyNotPublished(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.ReadyPublished = false
	mustReject(t, "就绪未发布", "ready-not-published", tr)
}

// B6 口径收窄：fixture 显式调用计数只作日志自洽校验，
// 不能冒充 Router 内部行为证据；这里只确认它仍可被 Judge 独立拒绝。
func TestFixtureShutdownCountIsNotRouterBehaviorEvidence(t *testing.T) {
	tr := fullShutdownFirstTrace()
	tr.ShutdownCallsAtFinalize = 2
	v := Judge(tr)
	if v.Pass {
		t.Fatal("fixture 显式计数变化未被拒绝")
	}
	if !strings.Contains(strings.Join(v.Reasons, " | "), "fixture-shutdown-call-count-changed") {
		t.Fatalf("rejection must name the narrowed fixture-only reason: %v", v.Reasons)
	}
	// 真实 Router 行为证据来自 B1/B2 的公开 Context 观察，不是这个计数。
	tr2 := fullNaturalTrace()
	if tr2.ShutdownCalls != 0 {
		t.Fatal("natural trace must not call Shutdown")
	}
}
