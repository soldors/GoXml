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
