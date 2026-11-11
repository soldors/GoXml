//go:build nativewindow

// judge.go 是 lifecyclenative 的**机器判据**：只对 marker.txt / result.json 的
// 已落盘字段做 fail-closed 判定。它不产生、不模拟、不替代原生 DestroyEvent；
// 受控状态/日志回归（judge_test.go）通过它证明判据本身，也不等于原生验收。
package main

import (
	"fmt"
	"strings"
)

const (
	markerCallback           = "callback"
	markerUnmount            = "unmount:home"
	markerFinalize           = "finalize" // LifecycleFinalize 返回之后写入 = finalize 完成
	markerObserverPrefix     = "observer:"
	markerBackgroundPrefix   = "background-shutdown:"
	markerReady              = "ready-to-close"
	markerReadyRefusedPrefix = "ready-refused:"
	// markerEarlyPrefix 定义在 main.go（写入方），判定器同包引用。
)

const (
	scenarioNatural       = "natural"
	scenarioShutdownFirst = "shutdown-first"
)

// ExpectedMarkerOrder 是手册 MACHINE_MARKER_ORDER 与源码写入顺序必须采用的
// 唯一真值。同步语义：WithOnDestroy 回调 → LifecycleFinalize 内部 unmountCurrent
// 触发 OnUnmount → 回调返回后写 finalize → cleanup 之后 Observer。
var ExpectedMarkerOrder = []string{"callback", "unmount:home", "finalize", "observer"}

// ExpectedStepsOrder 是 result.json 的 steps 子序列真值（layout 可重复多帧）。
var ExpectedStepsOrder = []string{
	"construct:home", "mount:home", "layout:home", "callback", "unmount:home", "finalize",
}

// ReadyState 是写「可关窗 marker」之前的三个独立条件。
type ReadyState struct {
	ShutdownCompleted bool
	Unmounts          int
	CleanupDone       bool
}

// readyToClose 只有三个条件同时满足才返回 true，绝不降级放行。
func readyToClose(s ReadyState) (bool, string) {
	if !s.ShutdownCompleted {
		return false, "background shutdown not completed"
	}
	if s.Unmounts != 0 {
		return false, "OnUnmount already happened"
	}
	if s.CleanupDone {
		return false, "CleanupDone already true"
	}
	return true, ""
}

// Trace 是 result.json / marker.txt 的机器可读投影。
// JSON 字段名即手册 §3 字段表里的名字，保证「手册 ↔ 机器判据」逐字对账。
type Trace struct {
	Scenario                         string   `json:"scenario"`
	Markers                          []string `json:"marker_lines"`
	Steps                            []string `json:"steps"`
	Mounts                           int      `json:"mounts"`
	Unmounts                         int      `json:"unmounts"`
	MountBeforeFirstLayout           bool     `json:"mount_before_first_layout"`
	PageContextCanceledBeforeUnmount bool     `json:"page_context_canceled_before_unmount"`
	PageContextErrAtUnmount          string   `json:"page_context_err_at_unmount"`
	ObserverAfterCleanup             bool     `json:"observer_after_cleanup"`
	ProcessCode                      int      `json:"process_code"`
	ShutdownObserved                 bool     `json:"shutdown_observed"`
	ShutdownUnmounts                 int      `json:"shutdown_unmounts"`
	ShutdownCleanupDone              bool     `json:"shutdown_cleanup_done"`
	ShutdownCalls                    int      `json:"shutdown_calls"`
	ShutdownCallsBeforeFinalize      int      `json:"shutdown_calls_before_finalize"`
	ShutdownCallsAtFinalize          int      `json:"shutdown_calls_at_finalize"`
	FrameRequests                    int      `json:"frame_requests"`
	FramesNeeded                     int      `json:"frames_needed"`
	CloseArrivedEarly                bool     `json:"close_arrived_early"`
}

// Verdict 汇总全部失败原因；Pass 只在 reasons 为空时为真。
type Verdict struct {
	Pass    bool
	Reasons []string
}

func markerKey(line string) string {
	switch {
	case line == markerCallback:
		return "callback"
	case line == markerUnmount:
		return "unmount:home"
	case line == markerFinalize:
		return "finalize"
	case strings.HasPrefix(line, markerObserverPrefix):
		return "observer"
	case strings.HasPrefix(line, markerBackgroundPrefix):
		return "background-shutdown"
	case line == markerReady:
		return "ready-to-close"
	case strings.HasPrefix(line, markerReadyRefusedPrefix):
		return "ready-refused"
	case strings.HasPrefix(line, markerEarlyPrefix):
		return "close-arrived-early"
	default:
		return "other:" + line
	}
}

func firstIndex(lines []string, key string) int {
	for i, line := range lines {
		if markerKey(line) == key {
			return i
		}
	}
	return -1
}

func countKey(lines []string, key string) int {
	n := 0
	for _, line := range lines {
		if markerKey(line) == key {
			n++
		}
	}
	return n
}

// subsequence 检查 want 是否按相对顺序出现在 got 里（允许 got 含额外帧步骤）。
func subsequence(got, want []string) bool {
	j := 0
	for _, g := range got {
		if j < len(want) && g == want[j] {
			j++
		}
	}
	return j == len(want)
}

// Judge 对一份已落盘证据做 fail-closed 判定。任何缺失、错序、早关闭、
// 后台观测缺失或三条件不满足都会产生原因并拒绝通过。
func Judge(t Trace) Verdict {
	// 空切片而非 nil：PASS 时 result.json 的 log_judge.reasons 必须是 []，
	// 与手册 §4/§5 的「reasons=[]」逐字一致，不能序列化成 null。
	reasons := []string{}
	add := func(code, detail string) { reasons = append(reasons, code+": "+detail) }

	// 1) 真实 marker 顺序：callback -> unmount:home -> finalize -> observer
	last := -1
	for _, key := range ExpectedMarkerOrder {
		idx := firstIndex(t.Markers, key)
		if idx < 0 {
			add("missing-marker", key)
			continue
		}
		if countKey(t.Markers, key) != 1 {
			add("duplicate-marker", key)
		}
		if idx <= last {
			add("marker-order", fmt.Sprintf("%s at %d is not after previous at %d", key, idx, last))
		}
		last = idx
	}

	// 2) steps 必须与源码同步语义一致（允许额外的 layout:home 帧）
	if !subsequence(t.Steps, ExpectedStepsOrder) {
		add("steps-order", fmt.Sprintf("steps=%v does not contain %v in order", t.Steps, ExpectedStepsOrder))
	}

	// 3) 三个独立证明：首次 Layout 前 Mount、卸载前 Context 已取消、卸载恰一次
	if !t.MountBeforeFirstLayout {
		add("mount-before-first-layout", "first business Layout saw mounts != 1")
	}
	if !t.PageContextCanceledBeforeUnmount || t.PageContextErrAtUnmount != "context canceled" {
		add("page-context-canceled-before-unmount",
			fmt.Sprintf("canceled=%t err=%q", t.PageContextCanceledBeforeUnmount, t.PageContextErrAtUnmount))
	}
	if t.Mounts != 1 || t.Unmounts != 1 {
		add("unmount-exactly-once", fmt.Sprintf("mounts=%d unmounts=%d", t.Mounts, t.Unmounts))
	}

	// 4) cleanup 后 observer、正常关窗退出码
	if !t.ObserverAfterCleanup {
		add("observer-after-cleanup", "ExitResult.CleanupDone was false")
	}
	if t.ProcessCode != 0 {
		add("process-code", fmt.Sprintf("code=%d", t.ProcessCode))
	}

	// 5) Finalize 不隐式 Shutdown（fixture 侧可观测：finalize 前后调用次数不变）
	if t.ShutdownCallsAtFinalize != t.ShutdownCallsBeforeFinalize {
		add("finalize-implicit-shutdown", fmt.Sprintf("before=%d at=%d",
			t.ShutdownCallsBeforeFinalize, t.ShutdownCallsAtFinalize))
	}

	// 6) 早关闭 / ready 拒绝一律 fail-closed
	if t.CloseArrivedEarly || countKey(t.Markers, "close-arrived-early") > 0 {
		add("close-arrived-early", "destroy callback arrived before readiness evidence")
	}
	if countKey(t.Markers, "ready-refused") > 0 {
		add("ready-refused", "readiness conditions were not all satisfied")
	}

	// 7) 场景专属判据
	switch t.Scenario {
	case scenarioNatural:
		if t.ShutdownCalls != 0 || countKey(t.Markers, "background-shutdown") > 0 {
			add("natural-unexpected-shutdown", "natural scenario must not call Shutdown")
		}
	case scenarioShutdownFirst:
		if countKey(t.Markers, "background-shutdown") != 1 || !t.ShutdownObserved {
			add("missing-background-observation", "background Shutdown completion was not observed")
		}
		if t.ShutdownUnmounts != 0 || t.ShutdownCleanupDone {
			add("shutdown-only-cancel", fmt.Sprintf("unmounts=%d cleanup=%t",
				t.ShutdownUnmounts, t.ShutdownCleanupDone))
		}
		if t.ShutdownCalls != 1 {
			add("shutdown-call-count", fmt.Sprintf("calls=%d want 1", t.ShutdownCalls))
		}
		readyIdx, cbIdx := firstIndex(t.Markers, "ready-to-close"), firstIndex(t.Markers, "callback")
		if readyIdx < 0 {
			add("missing-background-observation", "ready-to-close marker was never written")
		} else if cbIdx >= 0 && readyIdx > cbIdx {
			add("close-arrived-early", "close preceded ready-to-close marker")
		}
		if t.FramesNeeded < 2 || t.FrameRequests < t.FramesNeeded-1 {
			add("frame-requests", fmt.Sprintf("requests=%d needed=%d (no deterministic follow-up FrameEvent)",
				t.FrameRequests, t.FramesNeeded))
		}
	default:
		add("unknown-scenario", t.Scenario)
	}

	return Verdict{Pass: len(reasons) == 0, Reasons: reasons}
}
