//go:build nativewindow

// DataGrid 键盘导航原生验收 fixture（R28-B release-grid）。
//
// 默认 `go test` **不编译不执行**本文件：仅在协调者显式 `-tags nativewindow`
// 下编译（普通/race 测试二进制），由授权的原生阶段运行。作者阶段不开窗、
// 不注入桌面按键、不执行本测试。
//
// 场景（真实公开 API，无内部注入）：
//   - gridA：10k 行（ROW-%05d 可核验 RowID），键盘导航 opt-in；
//   - gridB：同数据、键盘禁用对照（永不消费导航键）；
//   - editor：真实 widget.Editor（第二个可区分焦点消费者）。
//
// 布局与留痕在 fixtureView（datagrid_fixture_helpers_test.go，留痕路径
// 与无窗口生产者回归共用同一实现）。本文件只保留窗口循环与入口：
// RunWindow（自有且永不关闭的 stop channel）+ WithOnDestroy 真实 close
// 记录 + run-return/exit 区分（成功/错误不互相冒充；RunWindow 非 nil
// 错误使本测试 FAIL——R2 修正）。
//
// 退出：标题栏关闭 → DestroyEvent → 记录并结束（rc=0）。超时由运行方
// 容器强杀（每帧 flush，日志不丢；收口办法见 RUNBOOK）。
package widgets

import (
	"flag"
	"testing"

	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	goxmlapp "github.com/goxml/goxml/pkg/runtime/app"
)

var fixtureLogPath = flag.String("fixture-log", "native-grid-fixture-log.jsonl", "原生 fixture JSONL 日志输出路径")
var fixtureRows = flag.Int("fixture-rows", 10000, "gridA/gridB 数据行数")

// TestDataGridKeyboardNativeFixture 打开真实窗口并运行键盘验收场景。
// 判定不在本测试内（fixture 只产日志），由 judge_native_log.py 依不变式
// 判定；RunWindow 非 nil 错误使本测试 FAIL（R2：真实失败不得被吞）。
func TestDataGridKeyboardNativeFixture(t *testing.T) {
	log, err := newFixtureLog(*fixtureLogPath)
	if err != nil {
		t.Fatalf("创建 fixture 日志失败: %v", err)
	}
	defer log.flushAndClose()

	gridA := NewDataGrid(unit.Dp(24))
	if err := gridA.SetColumns(fixtureGridColumns()); err != nil {
		t.Fatal(err)
	}
	if rep := gridA.SetRows(fixtureGridRows(*fixtureRows)); !rep.OK {
		t.Fatal("gridA 数据失败")
	}
	gridA.EnableKeyboardNav(DataGridKeyboardConfig{}) // opt-in

	gridB := NewDataGrid(unit.Dp(24))
	if err := gridB.SetColumns(fixtureGridColumns()); err != nil {
		t.Fatal(err)
	}
	if rep := gridB.SetRows(fixtureGridRows(*fixtureRows)); !rep.OK {
		t.Fatal("gridB 数据失败")
	}
	// gridB 不开启键盘：禁用对照（按键不被消费）。

	var ed widget.Editor
	th := material.NewTheme()

	// 自有且永不关闭的 stop channel：窗口经标题栏关闭自行返回。
	stop := make(chan struct{})
	view := &fixtureView{
		gridA: gridA,
		gridB: gridB,
		ed:    &ed,
		th:    th,
		log:   log,
	}
	// 真实 close 经公开 WithOnDestroy 观察记录（事件循环收到
	// DestroyEvent 时同步回调）。
	wv, err := goxmlapp.WithOnDestroy(view, log.recordDestroy)
	if err != nil {
		t.Fatalf("WithOnDestroy: %v", err)
	}
	runErr := goxmlapp.RunWindow(wv, th, goxmlapp.Config{
		Title:  "R28-B DataGrid keyboard fixture",
		Width:  unit.Dp(960),
		Height: unit.Dp(640),
	}, stop)
	// 成功返回 / 错误 / 运行方观察到的 rc 三者区分记录（不互相冒充）。
	log.recordRunResult(runErr)
	if log.frameN == 0 {
		t.Fatal("RunWindow 未执行任何帧")
	}
	// R2 修正：RunWindow 非 nil 错误必须使 fixture 失败（不得被吞）。
	if runErr != nil {
		t.Errorf("RunWindow 返回非 nil 错误（原样记录）: %v", runErr)
	}
}
