//go:build nativewindow

// r1_repro_test.go 是 R28-C1 的「先立红」探针：把协调者 review-and-repair.md
// 对 C 线的两项 P2 发现变成可执行的静态一致性检查。
//
// 纪律：本文件只做**静态文本 / 受控状态**检查，不开窗、不注入事件、
// 不产生原生 DestroyEvent 证据。首败保留在 r1-delivery-c/raw/cmdlogs/。
package main

import (
	"os"
	"strings"
	"testing"
)

// manualWantMarkerOrder 是手册必须声明、机器判据必须采用的真实顺序。
// 来源：WithOnDestroy 回调同步执行 LifecycleFinalize，后者在回调返回前
// 经 unmountCurrent 调用 OnUnmount；finalize 只能在返回之后写入。
var manualWantMarkerOrder = []string{"callback", "unmount:home", "finalize", "observer"}

// manualWantStepsOrder 是 result.json 的 steps 真实顺序（首帧布局折叠为一次）。
var manualWantStepsOrder = []string{
	"construct:home", "mount:home", "layout:home", "callback", "unmount:home", "finalize",
}

func splitOrder(s string) []string {
	parts := strings.Split(s, ">")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func equalOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func machineDecl(readme, prefix string) string {
	for _, line := range strings.Split(readme, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// TestR1ReproduceReviewFindings 逐条复现协调者的红；修复后该探针转为
// 「手册 ↔ 源码 ↔ 机器判据」三方一致性守卫，永不删除。
func TestR1ReproduceReviewFindings(t *testing.T) {
	readmeBytes, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	mainBytes, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	readme, mainGo := string(readmeBytes), string(mainBytes)

	// F1-a：旧手册把 finalize 写在 unmount 之前，与同步 Finalize 的真实顺序矛盾。
	if strings.Contains(readme, "callback → finalize → unmount:home") {
		t.Errorf("F1-a: 手册仍写反序 callback → finalize → unmount:home")
	}
	// F1-b：手册必须有与机器判据逐字一致的 marker 顺序声明。
	if decl := machineDecl(readme, "MACHINE_MARKER_ORDER:"); decl == "" {
		t.Errorf("F1-b: 手册缺少 MACHINE_MARKER_ORDER 声明")
	} else if got := splitOrder(decl); !equalOrder(got, manualWantMarkerOrder) {
		t.Errorf("F1-b: 手册 marker 顺序 %v != 真实顺序 %v", got, manualWantMarkerOrder)
	}
	// F1-c：手册必须有与源码 steps 一致的顺序声明。
	if decl := machineDecl(readme, "MACHINE_STEPS_ORDER:"); decl == "" {
		t.Errorf("F1-c: 手册缺少 MACHINE_STEPS_ORDER 声明")
	} else if got := splitOrder(decl); !equalOrder(got, manualWantStepsOrder) {
		t.Errorf("F1-c: 手册 steps 顺序 %v != 真实顺序 %v", got, manualWantStepsOrder)
	}

	// F2-a：shutdown-first 必须由 fixture 请求后续真实 FrameEvent，
	// 不得依赖操作员碰巧移动窗口。
	if !strings.Contains(mainGo, "op.InvalidateCmd{}") {
		t.Errorf("F2-a: main.go 未在 Layout 中请求后续真实 FrameEvent（op.InvalidateCmd{}）")
	}
	// F2-b：只有三条件同时满足才写可关窗 marker。
	if !strings.Contains(mainGo, "readyToClose(") || !strings.Contains(mainGo, "ready-to-close") {
		t.Errorf("F2-b: main.go 缺少 readyToClose 三条件判定或 ready-to-close marker")
	}
	// F2-c：关窗早到 / 后台观测缺失必须显式拒绝，不能静默判通过。
	if !strings.Contains(mainGo, "close-arrived-early") {
		t.Errorf("F2-c: main.go 缺少关窗早到的证据不足标记")
	}
	// F2-d：机器判据文件必须与手册采用同一个顺序常量。
	judgeBytes, err := os.ReadFile("judge.go")
	if err != nil {
		t.Errorf("F2-d: 缺少机器判据 judge.go: %v", err)
	} else {
		judgeGo := string(judgeBytes)
		if !strings.Contains(judgeGo, "ExpectedMarkerOrder") {
			t.Errorf("F2-d: judge.go 未定义 ExpectedMarkerOrder")
		}
		if !strings.Contains(judgeGo, `"callback", "unmount:home", "finalize", "observer"`) {
			t.Errorf("F2-d: judge.go 的 ExpectedMarkerOrder 字面量与手册/源码真实顺序不一致")
		}
		if !strings.Contains(judgeGo, "close-arrived-early") {
			t.Errorf("F2-d: judge.go 未把早关闭列为 fail-closed 原因")
		}
	}
}
