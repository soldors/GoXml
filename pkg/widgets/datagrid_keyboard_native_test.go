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
// 布局（自上而下）：gridA 300px、gridB 140px、editor 60px，窗口 960×640dp。
//
// 留痕：JSONL（-fixture-log，默认 native-grid-fixture-log.jsonl），每帧
// flush。记录：焦点（gridA/gridB/editor/none）、选择（ROW-id）与当帧可见
// 性、滚动偏移、视口与 pxPerDp（resize）、Editor 编辑长度增量（**不记录
// 用户输入内容**）、未被消费的导航键（泄漏观察）、close/exit。
// 行为判定：testdata/datagrid-keyboard/native/judge_native_log.py 依不变式
// 自动判定；按键→结果的人工对应见交付 native-acceptance 计划。
//
// 退出：标题栏关闭 → DestroyEvent → 记录并结束（rc=0）。超时由运行方
// 容器强杀（每帧 flush，日志不丢；收口办法见交付 runbook）。
package widgets

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	goxmlapp "github.com/goxml/goxml/pkg/runtime/app"
)

var fixtureLogPath = flag.String("fixture-log", "native-grid-fixture-log.jsonl", "原生 fixture JSONL 日志输出路径")
var fixtureRows = flag.Int("fixture-rows", 10000, "gridA/gridB 数据行数")

// fixtureNavKeys 是观察用的导航键集（与组件消费集一致）。
var fixtureNavKeys = []key.Name{
	key.NameUpArrow, key.NameDownArrow, key.NameHome,
	key.NameEnd, key.NamePageUp, key.NamePageDown,
}

type fixtureLog struct {
	f      *os.File
	bw     *bufio.Writer
	seq    int
	frameN int
}

func (l *fixtureLog) emit(m map[string]interface{}) {
	if l == nil || l.bw == nil {
		return
	}
	l.seq++
	m["seq"] = l.seq
	m["frame"] = l.frameN
	m["tms"] = time.Now().Sub(time.Unix(0, 0)).Milliseconds()
	b, _ := json.Marshal(m)
	l.bw.Write(b)
	l.bw.WriteByte('\n')
	l.bw.Flush()
}

// TestDataGridKeyboardNativeFixture 打开真实窗口并运行键盘验收场景。
// 判定不在本测试内（fixture 只产日志），由 judge_native_log.py 依不变式
// 判定；本测试自身仅要求：窗口循环退出且日志已写出。
func TestDataGridKeyboardNativeFixture(t *testing.T) {
	lf, err := os.Create(*fixtureLogPath)
	if err != nil {
		t.Fatalf("创建 fixture 日志失败: %v", err)
	}
	log := &fixtureLog{f: lf, bw: bufio.NewWriter(lf)}
	defer func() {
		log.emit(map[string]interface{}{"event": "exit"})
		log.bw.Flush()
		lf.Sync()
		lf.Close()
	}()

	rows := make([]DataGridRow, *fixtureRows)
	for i := range rows {
		rows[i] = DataGridRow{
			ID:    fmt.Sprintf("ROW-%05d", i),
			Cells: []string{fmt.Sprintf("ROW-%05d", i), "24 KB", "2026-11-10"},
		}
	}
	cols := []DataGridColumn{
		{Title: "ID", WidthDp: unit.Dp(200)},
		{Title: "大小", WidthDp: unit.Dp(120)},
		{Title: "修改时间", WidthDp: unit.Dp(160)},
	}

	gridA := NewDataGrid(unit.Dp(24))
	if err := gridA.SetColumns(cols); err != nil {
		t.Fatal(err)
	}
	if rep := gridA.SetRows(rows); !rep.OK {
		t.Fatal("gridA 数据失败")
	}
	gridA.EnableKeyboardNav(DataGridKeyboardConfig{}) // opt-in

	gridB := NewDataGrid(unit.Dp(24))
	if err := gridB.SetColumns(cols); err != nil {
		t.Fatal(err)
	}
	if rep := gridB.SetRows(rows); !rep.OK {
		t.Fatal("gridB 数据失败")
	}
	// gridB 不开启键盘：禁用对照（按键不被消费）。

	var ed widget.Editor
	th := material.NewTheme()

	// 真实公开 API：RunWindow 驱动真实窗口（R27 原生验收同款入口）。
	// R1 修复：传入**自有且不提前关闭**的 stop channel——nil 会在创建
	// 窗口前被拒绝（"app: nil stop channel"）；窗口经标题栏关闭自行返回，
	// stop 永不关闭即不会提前终止测试宿主。
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
	wv, err := goxmlapp.WithOnDestroy(view, func() {
		log.emit(map[string]interface{}{"event": "destroy-callback"})
	})
	if err != nil {
		t.Fatalf("WithOnDestroy: %v", err)
	}
	runErr := goxmlapp.RunWindow(wv, th, goxmlapp.Config{
		Title:  "R28-B DataGrid keyboard fixture",
		Width:  unit.Dp(960),
		Height: unit.Dp(640),
	}, stop)
	// 成功返回 / 错误 / 运行方观察到的 rc 三者区分记录（不互相冒充）。
	log.emit(map[string]interface{}{"event": "run-return", "err": fmt.Sprint(runErr)})
	if runErr != nil {
		t.Logf("RunWindow 返回错误（原样记录，交运行方判定）: %v", runErr)
	}
	if log.frameN == 0 {
		t.Fatal("RunWindow 未执行任何帧")
	}
}

// fixtureView 是 fixture 的根 View：每帧布局三个焦点/对照带并记录状态。
type fixtureView struct {
	gridA *DataGrid
	gridB *DataGrid
	ed    *widget.Editor
	th    *material.Theme
	log   *fixtureLog

	lastFocus  string
	lastSel    string
	selVisible bool
	lastOff    int
	lastW      int
	lastH      int
	lastDpi    float32
	lastEdLen  int

	lastSelB      string
	lastBodyHpx   int
	lastRowHpx    int
	lastHeaderHpx int
}

func (v *fixtureView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.log.frameN++
	w := gtx.Constraints.Max.X

	// 布局：三段带状（gridA 先声明：持焦时优先消费导航键）。
	band := func(y, height int, body func(gtx layout.Context)) {
		tr := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		btx := gtx
		btx.Constraints = layout.Exact(image.Pt(w, height))
		body(btx)
		tr.Pop()
	}
	const bandBodyH = 300
	band(0, bandBodyH, func(gtx layout.Context) { v.gridA.Layout(gtx, th) })
	band(300, 140, func(gtx layout.Context) { v.gridB.Layout(gtx, th) })
	band(440, 60, func(gtx layout.Context) {
		v.ed.Update(gtx)
		material.Editor(th, v.ed, "editor (焦点消费者 2)").Layout(gtx)
	})

	// 几何留痕：gridA 实际 body viewport 与本帧 Dp 换算的行高/表头
	// （judge 依此判定滚动边界，不用整窗高度和固定 24px）。
	bodyHpx := bandBodyH - gtx.Dp(unit.Dp(24)) // band 300 − 表头 24dp
	rowHpx := gtx.Dp(unit.Dp(24))
	headerHpx := gtx.Dp(unit.Dp(24))

	// 状态观察（Layout 之后：本帧 sel/滚动/焦点已更新）。
	focus := "none"
	if gtx.Source.Focused(&v.gridA.tag) {
		focus = "gridA"
	} else if gtx.Source.Focused(v.ed) {
		focus = "editor"
	} else if gtx.Source.Focused(&v.gridB.tag) {
		focus = "gridB"
	}
	if focus != v.lastFocus {
		v.log.emit(map[string]interface{}{"event": "focus-change", "from": v.lastFocus, "to": focus})
		v.lastFocus = focus
	}

	sel, selOK := v.gridA.SelectedRowID()
	v.selVisible = false
	if selOK {
		for _, d := range v.gridA.LastFrame().Drawn {
			if d == sel {
				v.selVisible = true
				break
			}
		}
	}
	selStr := ""
	if selOK {
		selStr = sel
	}
	if selStr != v.lastSel {
		v.log.emit(map[string]interface{}{"event": "sel-change", "from": v.lastSel, "to": selStr, "visible": v.selVisible, "focus": focus,
			"bodyHpx": v.lastBodyHpx, "rowHpx": v.lastRowHpx, "headerHpx": v.lastHeaderHpx})
		v.lastSel = selStr
	}
	if v.gridA.ScrollOffset() != v.lastOff && v.lastOff >= 0 {
		v.log.emit(map[string]interface{}{"event": "scroll", "offset": v.gridA.ScrollOffset(), "sel": selStr, "selVisible": v.selVisible,
			"bodyHpx": v.lastBodyHpx, "rowHpx": v.lastRowHpx, "headerHpx": v.lastHeaderHpx})
	}
	v.lastOff = v.gridA.ScrollOffset()

	if gtx.Constraints.Max.X != v.lastW || gtx.Constraints.Max.Y != v.lastH || gtx.Metric.PxPerDp != v.lastDpi {
		v.log.emit(map[string]interface{}{"event": "resize", "oldW": v.lastW, "oldH": v.lastH,
			"newW": gtx.Constraints.Max.X, "newH": gtx.Constraints.Max.Y, "pxPerDp": gtx.Metric.PxPerDp})
		v.lastW, v.lastH, v.lastDpi = gtx.Constraints.Max.X, gtx.Constraints.Max.Y, gtx.Metric.PxPerDp
	}
	edLen := len([]rune(v.ed.Text()))
	if v.lastEdLen >= 0 && edLen != v.lastEdLen {
		v.log.emit(map[string]interface{}{"event": "edit-delta", "chars": edLen - v.lastEdLen, "focus": focus})
	}
	v.lastEdLen = edLen

	// 泄漏观察：未被消费的导航键（gridA 持焦时其命中键先被消费；无焦点
	// 或 Editor 持焦时按键按焦点归属，漏网键在此现形）。
	for _, n := range fixtureNavKeys {
		for {
			ev, ok := gtx.Source.Event(key.Filter{Name: n})
			if !ok {
				break
			}
			if ke, isKey := ev.(key.Event); isKey && ke.State == key.Press {
				v.log.emit(map[string]interface{}{"event": "unconsumed-key", "name": string(ke.Name), "focus": focus})
			}
		}
	}

	selB, selBOK := v.gridB.SelectedRowID()
	selBStr := ""
	if selBOK {
		selBStr = selB
	}
	if selBStr != v.lastSelB {
		// 禁用对照：gridB 仅鼠标选择可用（键盘导航未开启）。
		v.log.emit(map[string]interface{}{"event": "selB-change", "from": v.lastSelB, "to": selBStr, "focus": focus})
		v.lastSelB = selBStr
	}

	if v.log.frameN%60 == 0 {
		v.log.emit(map[string]interface{}{"event": "heartbeat", "focus": focus, "sel": selStr, "selVisible": v.selVisible, "offset": v.gridA.ScrollOffset()})
	}
	v.lastBodyHpx, v.lastRowHpx, v.lastHeaderHpx = bodyHpx, rowHpx, headerHpx
	return layout.Dimensions{Size: gtx.Constraints.Max}
}
