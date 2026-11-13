// judge.go 是 R29-C S3 双窗原生 fixture 的机器判据（无 build tag，便于
// 默认无窗口集合做日志回放自检）。
//
// 边界：LogJudge 只判已落盘 marker/状态顺序；Judge 再叠加执行者字段
// （runner rc、race、无残留、人工观察）。两者都**不能**单独证明真实
// Gio DestroyEvent 或窗口确实显示——那需要人工操作与 runner 原始记录。
package main

import (
	"fmt"
	"strings"
)

// Trace 是标记/状态/执行者证据的机器可读投影。
type Trace struct {
	Scenario            string            `json:"scenario"`
	Markers             []string          `json:"marker_lines"`
	Unmounts            map[string]int    `json:"unmounts"`
	PageContextCanceled map[string]bool   `json:"page_context_canceled_before_unmount"`
	CleanupDone         map[string]bool   `json:"cleanup_done"`
	RouterRootAfter     map[string]string `json:"router_root_after_finalize"`
	ExitCode            int               `json:"exit_code"`
	RunnerRc            int               `json:"runner_observed_rc"`
	DataRace            bool              `json:"data_race"`
	NoResidual          bool              `json:"no_residual_process"`
	HumanBAfterA        bool              `json:"manual_b_serving_after_a_close"`
}

// Verdict 汇总所有失败原因；Pass 只在 reasons 为空时为真。
type Verdict struct {
	Pass    bool
	Reasons []string
	Scope   string
}

const logJudgeScope = "日志/状态判据，不是原生 DestroyEvent 或 native race 验收结论"

// ExpectedOrder 是健康的双窗轨迹（cancel-first 的后台取消行插在就绪后）。
var ExpectedOrder = []string{
	"open:A", "open:B", "frame:A", "frame:B",
	"close:A", "unmount:A", "finalize:A", "frame:B-after-A",
	"close:B", "unmount:B", "finalize:B", "exit",
}

func markerKey(line string) string {
	switch {
	case line == "open:A" || line == "open:B" || line == "frame:A" || line == "frame:B" ||
		line == "close:A" || line == "close:B" || line == "finalize:A" || line == "finalize:B" ||
		line == "frame:B-after-A":
		return line
	case strings.HasPrefix(line, "unmount:A"):
		return "unmount:A"
	case strings.HasPrefix(line, "unmount:B"):
		return "unmount:B"
	case strings.HasPrefix(line, "exit:code="):
		return "exit"
	case strings.HasPrefix(line, "background-cancel:"):
		return "background-cancel"
	case strings.HasPrefix(line, "early-exit"):
		return "early-exit"
	case strings.HasPrefix(line, "cross-stop:"):
		return "cross-stop"
	case strings.HasPrefix(line, "duplicate-cleanup:"):
		return "duplicate-cleanup"
	case strings.HasPrefix(line, "scenario:"):
		return "scenario"
	case strings.HasPrefix(line, "observer:"):
		return "observer"
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

// LogJudge 只判日志/状态层，不含任何 runner / 人工字段。
func LogJudge(t Trace) Verdict {
	reasons := []string{}
	add := func(code, detail string) { reasons = append(reasons, code+": "+detail) }

	// 1) 必要轨迹：顺序递增、各关键行恰一次。
	last := -1
	for _, key := range ExpectedOrder {
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

	// 2) 异常红证：早退、串写/串停、重复清理一律不可逆拒绝。
	for _, key := range []string{"early-exit", "cross-stop", "duplicate-cleanup"} {
		if countKey(t.Markers, key) > 0 {
			add(key, "failure marker present in trace")
		}
	}

	// 3) 每窗恰一次卸载、页面 Context 先取消、cleanup 完成。
	for _, label := range []string{"A", "B"} {
		if t.Unmounts[label] != 1 {
			add("unmount-exactly-once", fmt.Sprintf("%s=%d", label, t.Unmounts[label]))
		}
		if !t.PageContextCanceled[label] {
			add("page-context-canceled-before-unmount", label)
		}
		if !t.CleanupDone[label] {
			add("cleanup-not-done", label)
		}
	}

	// 4) Router 根：natural 不隐式 Shutdown；cancel-first 已显式取消为预期。
	for _, label := range []string{"A", "B"} {
		root := t.RouterRootAfter[label]
		switch t.Scenario {
		case "cancel-first":
			if root != "context canceled" {
				add("cancel-first-root", fmt.Sprintf("%s root=%q", label, root))
			}
		default: // natural
			if root != "" {
				add("natural-root-canceled", fmt.Sprintf("%s root=%q", label, root))
			}
		}
	}

	// 5) 场景专属：后台取消只取消，且必须先于关窗。
	cancels := countKey(t.Markers, "background-cancel")
	switch t.Scenario {
	case "cancel-first":
		if cancels != 2 {
			add("missing-background-cancel", fmt.Sprintf("count=%d want 2", cancels))
		}
		for _, line := range t.Markers {
			if markerKey(line) == "background-cancel" &&
				(!strings.Contains(line, "unmounts=0") || !strings.Contains(line, "cleanup=false")) {
				add("background-cancel-not-cancel-only", line)
			}
		}
		closeA := firstIndex(t.Markers, "close:A")
		if idx := firstIndex(t.Markers, "background-cancel"); idx >= 0 && closeA >= 0 && idx > closeA {
			add("cancel-after-close", "background cancel must precede title-bar close")
		}
	default:
		if cancels != 0 {
			add("unexpected-background-cancel", "natural scenario must not call Shutdown")
		}
	}

	if t.ExitCode != 0 {
		add("exit-code", fmt.Sprintf("code=%d", t.ExitCode))
	}
	return Verdict{Pass: len(reasons) == 0, Reasons: reasons, Scope: logJudgeScope}
}

// Judge 是完整判据：日志层 + 执行者证据层（执行者字段必须实测填写）。
func Judge(t Trace) Verdict {
	v := LogJudge(t)
	reasons := append([]string(nil), v.Reasons...)
	add := func(code, detail string) { reasons = append(reasons, code+": "+detail) }
	if t.RunnerRc != 0 {
		add("runner-rc", fmt.Sprintf("rc=%d (未执行=-1)", t.RunnerRc))
	}
	if t.DataRace {
		add("data-race", "stdout/stderr 出现 DATA RACE")
	}
	if !t.NoResidual {
		add("residual-process", "无残留未由执行者确认")
	}
	if !t.HumanBAfterA {
		add("manual-b-serving", "关 A 后 B 仍布局/响应的人工观察缺失")
	}
	return Verdict{Pass: len(reasons) == 0, Reasons: reasons, Scope: "含执行者 runner/人工证据的完整判据"}
}
