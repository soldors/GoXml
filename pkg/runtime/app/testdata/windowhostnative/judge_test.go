// judge_test.go 是**日志回放自检**（默认无窗口集合）。
//
// 合成 Trace 只用于证明判定器能红能绿；它不启动窗口、不产生
// DestroyEvent，也不冒充原生验收结果。真实执行由授权人工操作 +
// runner 原始记录 + Judge(实测字段) 完成。
package main

import (
	"strings"
	"testing"
)

func healthyTrace(scenario string) Trace {
	root := ""
	if scenario == "cancel-first" {
		root = "context canceled"
	}
	markers := []string{"scenario:" + scenario, "open:A", "open:B", "frame:A", "frame:B"}
	if scenario == "cancel-first" {
		markers = append(markers,
			"background-cancel:A unmounts=0 cleanup=false root=context canceled",
			"background-cancel:B unmounts=0 cleanup=false root=context canceled")
	}
	markers = append(markers,
		"close:A", "unmount:A canceled=true count=1", "finalize:A",
		"frame:B-after-A",
		"close:B", "unmount:B canceled=true count=1", "finalize:B",
		"exit:code=0")
	return Trace{
		Scenario: scenario, Markers: markers,
		Unmounts:            map[string]int{"A": 1, "B": 1},
		PageContextCanceled: map[string]bool{"A": true, "B": true},
		CleanupDone:         map[string]bool{"A": true, "B": true},
		RouterRootAfter:     map[string]string{"A": root, "B": root},
		ExitCode:            0,
		RunnerRc:            0,
		DataRace:            false,
		NoResidual:          true,
		HumanBAfterA:        true,
	}
}

func assertPass(t *testing.T, v Verdict) {
	t.Helper()
	if !v.Pass {
		t.Fatalf("期望 PASS，实得 FAIL: %v", v.Reasons)
	}
}

func assertFail(t *testing.T, v Verdict, wantCode string) {
	t.Helper()
	if v.Pass {
		t.Fatal("期望 FAIL，实得 PASS")
	}
	if !strings.Contains(strings.Join(v.Reasons, " | "), wantCode) {
		t.Fatalf("失败原因应含 %q，实得 %v", wantCode, v.Reasons)
	}
}

func TestJudgeHealthyNaturalPassesLogAndFull(t *testing.T) {
	tr := healthyTrace("natural")
	assertPass(t, LogJudge(tr))
	assertPass(t, Judge(tr))
}

func TestJudgeHealthyCancelFirstPassesLogAndFull(t *testing.T) {
	tr := healthyTrace("cancel-first")
	assertPass(t, LogJudge(tr))
	assertPass(t, Judge(tr))
}

func TestJudgeRejectsEarlyExit(t *testing.T) {
	tr := healthyTrace("natural")
	tr.Markers = append(tr.Markers, "early-exit:before-B")
	assertFail(t, LogJudge(tr), "early-exit")
}

func TestJudgeRejectsDuplicateCleanup(t *testing.T) {
	tr := healthyTrace("natural")
	tr.Markers = append(tr.Markers, "duplicate-cleanup:A", "unmount:A canceled=true count=2")
	tr.Unmounts["A"] = 2
	assertFail(t, LogJudge(tr), "duplicate-cleanup")
	assertFail(t, LogJudge(tr), "unmount-exactly-once")
}

func TestJudgeRejectsCrossWindowStop(t *testing.T) {
	tr := healthyTrace("natural")
	tr.Markers = append(tr.Markers, "cross-stop:A->B")
	assertFail(t, LogJudge(tr), "cross-stop")
}

func TestJudgeRejectsMissingBFramesAfterAClose(t *testing.T) {
	tr := healthyTrace("natural")
	var kept []string
	for _, m := range tr.Markers {
		if m != "frame:B-after-A" {
			kept = append(kept, m)
		}
	}
	tr.Markers = kept
	assertFail(t, LogJudge(tr), "missing-marker")
}

func TestJudgeRejectsCancelOnlyViolation(t *testing.T) {
	tr := healthyTrace("cancel-first")
	tr.Markers[6] = "background-cancel:A unmounts=1 cleanup=true root=context canceled"
	assertFail(t, LogJudge(tr), "background-cancel-not-cancel-only")
}

func TestJudgeRejectsNaturalRootCanceled(t *testing.T) {
	tr := healthyTrace("natural")
	tr.RouterRootAfter["A"] = "context canceled"
	assertFail(t, LogJudge(tr), "natural-root-canceled")
}

func TestJudgeRunnerAndHumanFieldsAreIndependent(t *testing.T) {
	tr := healthyTrace("natural")
	// 日志层仍绿：执行者证据缺失不能由日志层代填。
	assertPass(t, LogJudge(tr))

	tr.RunnerRc = -1
	assertFail(t, Judge(tr), "runner-rc")
	tr.RunnerRc = 0

	tr.DataRace = true
	assertFail(t, Judge(tr), "data-race")
	tr.DataRace = false

	tr.NoResidual = false
	assertFail(t, Judge(tr), "residual-process")
	tr.NoResidual = true

	tr.HumanBAfterA = false
	assertFail(t, Judge(tr), "manual-b-serving")
	tr.HumanBAfterA = true
	assertPass(t, Judge(tr))
}

func TestJudgeRejectsNonZeroExitCode(t *testing.T) {
	tr := healthyTrace("natural")
	tr.ExitCode = 1
	assertFail(t, LogJudge(tr), "exit-code")
}

// R1：frame:B-after-A 必须晚于 A 清理完成（finalize:A）、早于 B 收尾
// （close:B）。把该行前移或后移都必须被判定器拒绝。
func TestJudgeRejectsBAfterABeforeFinalizeA(t *testing.T) {
	tr := healthyTrace("natural")
	var kept []string
	moved := "frame:B-after-A"
	for _, m := range tr.Markers {
		if m == moved {
			continue
		}
		kept = append(kept, m)
		if m == "unmount:A canceled=true count=1" {
			kept = append(kept, moved) // 早于 finalize:A
		}
	}
	tr.Markers = kept
	assertFail(t, LogJudge(tr), "marker-order")
}

func TestJudgeRejectsBAfterAAfterBClose(t *testing.T) {
	tr := healthyTrace("natural")
	var kept []string
	moved := "frame:B-after-A"
	for _, m := range tr.Markers {
		if m == moved {
			continue
		}
		kept = append(kept, m)
		if m == "close:B" {
			kept = append(kept, moved) // 晚于 B 收尾起点
		}
	}
	tr.Markers = kept
	assertFail(t, LogJudge(tr), "marker-order")
}

// R29 收口 S1：宿主从 map 启动各窗，原生首帧没有已批准的 A/B 固定顺序。
// frame:B 先于 frame:A 是**合法**交错，两种顺序都必须通过；但两个首帧
// 仍都早于关 A，A 清理完后 B 才出 frame:B-after-A，B 收尾后才 exit。
func frameBFirst(markers []string) []string {
	out := append([]string(nil), markers...)
	for i := range out {
		if out[i] == "frame:A" {
			out[i] = "frame:B"
		} else if out[i] == "frame:B" {
			out[i] = "frame:A"
		}
	}
	return out
}

func TestJudgeAcceptsFrameBFirstNatural(t *testing.T) {
	tr := healthyTrace("natural")
	tr.Markers = frameBFirst(tr.Markers)
	assertPass(t, LogJudge(tr))
	assertPass(t, Judge(tr))
}

func TestJudgeAcceptsFrameBFirstCancelFirst(t *testing.T) {
	tr := healthyTrace("cancel-first")
	tr.Markers = frameBFirst(tr.Markers)
	assertPass(t, LogJudge(tr))
	assertPass(t, Judge(tr))
}

// 两个首帧都必须早于关 A；把 frame:B 挪到 close:A 之后，两种合法顺序
// 都不成立（B 首帧缺失/过晚）。
func TestJudgeRejectsFrameBAfterCloseA(t *testing.T) {
	tr := healthyTrace("natural")
	var moved string
	var kept []string
	for _, m := range tr.Markers {
		if m == "frame:B" {
			moved = m
			continue
		}
		kept = append(kept, m)
		if m == "close:A" {
			kept = append(kept, moved)
		}
	}
	tr.Markers = kept
	assertFail(t, LogJudge(tr), "marker-order")
}

// 启动失败 / 过早关闭 / 缺标记负例：缺 frame:B、close:A 早于两首帧。
func TestJudgeRejectsStartupFailureMissingFrameB(t *testing.T) {
	tr := healthyTrace("natural")
	var kept []string
	for _, m := range tr.Markers {
		if m != "frame:B" {
			kept = append(kept, m)
		}
	}
	tr.Markers = kept
	assertFail(t, LogJudge(tr), "missing-marker")
}

func TestJudgeRejectsCloseABeforeBothFrames(t *testing.T) {
	tr := healthyTrace("natural")
	var frameA, frameB string
	var kept []string
	for _, m := range tr.Markers {
		switch m {
		case "frame:A":
			frameA = m
		case "frame:B":
			frameB = m
		case "close:A":
			kept = append(kept, m) // 关 A 先到
		default:
			kept = append(kept, m)
		}
	}
	tr.Markers = append(kept, frameA, frameB)
	assertFail(t, LogJudge(tr), "marker-order")
}

// cancel-first：A/B 各恰一次、都在关 A 之前、都只取消未卸载。
func cancelLineIndex(markers []string, label string) int {
	prefix := "background-cancel:" + label + " "
	for i, line := range markers {
		if strings.HasPrefix(line, prefix) {
			return i
		}
	}
	return -1
}

func TestJudgeCancelFirstRequiresBothLabelledCancels(t *testing.T) {
	// 只有 A：B 缺失必须拒。
	tr := healthyTrace("cancel-first")
	var kept []string
	for _, m := range tr.Markers {
		if !strings.HasPrefix(m, "background-cancel:B ") {
			kept = append(kept, m)
		}
	}
	tr.Markers = kept
	assertFail(t, LogJudge(tr), "missing-background-cancel")

	// A 重复两次必须拒（不能靠总数=2 蒙混）。
	tr = healthyTrace("cancel-first")
	dup := "background-cancel:A unmounts=0 cleanup=false root=context canceled"
	idx := cancelLineIndex(tr.Markers, "A")
	tr.Markers = append(tr.Markers[:idx+1], append([]string{dup}, tr.Markers[idx+1:]...)...)
	assertFail(t, LogJudge(tr), "duplicate-background-cancel")
}

// 现实现只比较**第一条** background-cancel 与 close:A；把 B 的取消行
// 挪到 close:A 之后必须被拒（否则「第二个后台取消先落盘」前提不成立）。
func TestJudgeCancelFirstRejectsSecondCancelAfterCloseA(t *testing.T) {
	tr := healthyTrace("cancel-first")
	var bLine string
	var kept []string
	for _, m := range tr.Markers {
		if strings.HasPrefix(m, "background-cancel:B ") {
			bLine = m
			continue
		}
		kept = append(kept, m)
		if m == "close:A" {
			kept = append(kept, bLine)
		}
	}
	tr.Markers = kept
	assertFail(t, LogJudge(tr), "cancel-after-close")
}

// 两窗都必须「只取消、未卸载」：B 行若已发生卸载或 cleanup，也必须拒。
func TestJudgeCancelFirstRejectsBNotCancelOnly(t *testing.T) {
	tr := healthyTrace("cancel-first")
	idx := cancelLineIndex(tr.Markers, "B")
	tr.Markers[idx] = "background-cancel:B unmounts=1 cleanup=true root=context canceled"
	assertFail(t, LogJudge(tr), "background-cancel-not-cancel-only")
}
