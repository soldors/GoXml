//go:build nativewindow

// DataGrid R29 交互扩展原生验收 fixture（N1–N6，协调者 R1 实装）。
//
// 默认 `go test` **不编译不执行**本文件：仅在协调者显式 `-tags nativewindow`
// 下编译，由授权的原生阶段运行。作者阶段不开窗、不注入桌面输入、不执行
// 本测试。
//
// 场景（真实公开 API，无内部注入）：
//   - gridA：10k 行（ROW-%05d），5 列 ×300dp（1500dp 总宽 > 1200dp 视口），
//     EnableHorizontalScroll + EnableKeyboardNav + EnableMultiSelect；
//   - gridB：同数据、无 opt-in 禁用对照；
//   - editor：真实 widget.Editor；
//   - 显式排序入口：P 键（视图级，Editor 持焦时不触发）→ SortRows 反序。
//
// 操作脚本、判定不变式与运行形态见 RUNBOOK-R29.md 与
// judge_r29_native_log.py（STRUCTURAL 结构判定 + SCENARIO 人工确认）。
// 日志不记录真实用户输入：无按键名/无文本内容；unconsumed-key 仅导航键。
//
// 退出：标题栏关闭 → DestroyEvent → 记录并结束（rc=0）。超时由运行方
// 容器强杀（每帧 flush，日志不丢）。
package widgets

import (
	"flag"
	"testing"

	"gioui.org/unit"

	goxmlapp "github.com/goxml/goxml/pkg/runtime/app"
)

var r29FixtureLogPath = flag.String("r29-fixture-log", "r29-native-fixture-log.jsonl", "R29 原生 fixture JSONL 日志输出路径（须不存在，独占）")
var r29FixtureRows = flag.Int("r29-rows", 10000, "gridA/gridB 数据行数")

// TestDataGridR29NativeFixture 打开真实窗口运行 N1–N6 场景。判定不在本
// 测试内（fixture 只产日志），由 judge_r29_native_log.py 依模型判定；
// RunWindow 非 nil 错误使本测试 FAIL。
func TestDataGridR29NativeFixture(t *testing.T) {
	log, err := newFixtureLog(*r29FixtureLogPath)
	if err != nil {
		t.Fatalf("创建 fixture 日志失败: %v", err)
	}
	defer log.flushAndClose()

	view := newFixtureR29View(log, *r29FixtureRows)

	// 自有且永不关闭的 stop channel：窗口经标题栏关闭自行返回。
	stop := make(chan struct{})
	wv, err := goxmlapp.WithOnDestroy(view, log.recordDestroy)
	if err != nil {
		t.Fatalf("WithOnDestroy: %v", err)
	}
	runErr := goxmlapp.RunWindow(wv, view.th, goxmlapp.Config{
		Title:  "R29-B DataGrid interaction fixture (N1-N6)",
		Width:  unit.Dp(r29WinW),
		Height: unit.Dp(r29WinH),
	}, stop)
	log.recordRunResult(runErr)
	if log.frameN == 0 {
		t.Fatal("RunWindow 未执行任何帧")
	}
	if runErr != nil {
		t.Errorf("RunWindow 返回非 nil 错误（原样记录）: %v", runErr)
	}
}

// 编译期守卫：fixture 视图必须实现应用 View 合同（与 R28 同款形态）。
var _ goxmlapp.View = (*fixtureR29View)(nil)
