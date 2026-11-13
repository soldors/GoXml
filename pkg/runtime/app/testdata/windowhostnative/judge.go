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

// ExpectedOrder 是健康的双窗轨迹之一：A 首帧先于 B 首帧。
// 宿主从 map 启动各窗，原生首帧**没有**已批准的 A/B 固定顺序，因此
// frame:B 先于 frame:A 是同样合法的交错（见 LegalMarkerOrders）。
// 取消优先场景的 background-cancel 行插在两首帧之后、关 A 之前。
var ExpectedOrder = []string{
	"open:A", "open:B", "frame:A", "frame:B",
	"close:A", "unmount:A", "finalize:A", "frame:B-after-A",
	"close:B", "unmount:B", "finalize:B", "exit",
}

// expectedOrderBFirst 是另一种合法首帧顺序。两窗首帧的先后由真实
// FrameEvent 决定，不得由判定器强加顺序；但两个首帧都必须早于关 A。
var expectedOrderBFirst = []string{
	"open:A", "open:B", "frame:B", "frame:A",
	"close:A", "unmount:A", "finalize:A", "frame:B-after-A",
	"close:B", "unmount:B", "finalize:B", "exit",
}

// LegalMarkerOrders 是全部已批准的首帧交错（仅此两种）。
var LegalMarkerOrders = [][]string{ExpectedOrder, expectedOrderBFirst}

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

// backgroundCancelLabel 解析 "background-cancel:<label> ..." 的窗口标签。
// 格式由 fixture main.go 后台取消 goroutine 写盘，判定器必须与落盘行一致。
func backgroundCancelLabel(line string) string {
	const prefix = "background-cancel:"
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	rest := line[len(prefix):]
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// orderReasons 对**一种**合法序列检查递增顺序与各关键行恰一次。
func orderReasons(lines []string, order []string) []string {
	reasons := []string{}
	add := func(code, detail string) { reasons = append(reasons, code+": "+detail) }
	last := -1
	for _, key := range order {
		idx := firstIndex(lines, key)
		if idx < 0 {
			add("missing-marker", key)
			continue
		}
		if countKey(lines, key) != 1 {
			add("duplicate-marker", key)
		}
		if idx <= last {
			add("marker-order", fmt.Sprintf("%s at %d is not after previous at %d", key, idx, last))
		}
		last = idx
	}
	return reasons
}

// LogJudge 只判日志/状态层，不含任何 runner / 人工字段。
func LogJudge(t Trace) Verdict {
	reasons := []string{}
	add := func(code, detail string) { reasons = append(reasons, code+": "+detail) }

	// 1) 必要轨迹：两种合法首帧顺序任一成立即可（顺序递增、各关键行
	//    恰一次）。两种都不成立时报告原因最少的那一种。
	best := orderReasons(t.Markers, LegalMarkerOrders[0])
	for _, order := range LegalMarkerOrders[1:] {
		if r := orderReasons(t.Markers, order); len(r) < len(best) {
			best = r
		}
	}
	reasons = append(reasons, best...)

	// 1b) 与具体序列无关的独立守卫：两个首帧都早于关 A；A 清理完成
	//     （finalize:A）之后 B 才出 frame:B-after-A，且早于 B 收尾
	//     （close:B）；B 收尾（finalize:B）之后才允许 exit。
	closeA := firstIndex(t.Markers, "close:A")
	for _, key := range []string{"frame:A", "frame:B"} {
		idx := firstIndex(t.Markers, key)
		if idx >= 0 && closeA >= 0 && idx > closeA {
			add("frame-after-close-A", fmt.Sprintf("%s at %d after close:A at %d", key, idx, closeA))
		}
	}
	finalizeA := firstIndex(t.Markers, "finalize:A")
	afterA := firstIndex(t.Markers, "frame:B-after-A")
	closeB := firstIndex(t.Markers, "close:B")
	finalizeB := firstIndex(t.Markers, "finalize:B")
	exitIdx := firstIndex(t.Markers, "exit")
	if afterA >= 0 && finalizeA >= 0 && afterA < finalizeA {
		add("b-frame-before-a-cleanup", "frame:B-after-A recorded before finalize:A")
	}
	if afterA >= 0 && closeB >= 0 && afterA > closeB {
		add("b-frame-after-b-close", "frame:B-after-A recorded after close:B")
	}
	if exitIdx >= 0 && finalizeB >= 0 && exitIdx < finalizeB {
		add("exit-before-b-finalize", "exit recorded before finalize:B")
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

	// 5) 场景专属：cancel-first 的后台取消按**窗口标签**逐条核验。
	//    A/B 各恰一次、都在关 A 之前、都只取消未卸载（unmounts=0 /
	//    cleanup=false / root=context canceled）；不得只比较第一条。
	cancels := countKey(t.Markers, "background-cancel")
	switch t.Scenario {
	case "cancel-first":
		byLabel := map[string][]int{}
		for i, line := range t.Markers {
			if markerKey(line) != "background-cancel" {
				continue
			}
			label := backgroundCancelLabel(line)
			byLabel[label] = append(byLabel[label], i)
			if !strings.Contains(line, "unmounts=0") || !strings.Contains(line, "cleanup=false") {
				add("background-cancel-not-cancel-only", line)
			}
			if !strings.Contains(line, "root=context canceled") {
				add("background-cancel-root", line)
			}
		}
		if cancels == 0 {
			add("missing-background-cancel", "no background cancel markers")
		}
		for _, label := range []string{"A", "B"} {
			idxs := byLabel[label]
			if len(idxs) == 0 {
				add("missing-background-cancel", label)
			} else if len(idxs) > 1 {
				add("duplicate-background-cancel", label)
			}
		}
		for label, idxs := range byLabel {
			if label != "A" && label != "B" {
				add("unexpected-background-cancel-label", label)
			}
			for _, idx := range idxs {
				if closeA >= 0 && idx > closeA {
					add("cancel-after-close", fmt.Sprintf("%s at %d after close:A at %d", label, idx, closeA))
				}
			}
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
