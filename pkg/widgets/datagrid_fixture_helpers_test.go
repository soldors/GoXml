package widgets

// DataGrid 原生验收 fixture 的日志生产者与根 View（R28-B2 拆分）。
//
// 本文件仅在测试中编译：native fixture 的留痕与布局逻辑
// 与真实窗口循环分离，使「实际 Go 日志写入路径」可以在无窗口离屏回归
// 中被真实驱动（datagrid_fixturelog_test.go）。nativewindow 标签文件
// （datagrid_keyboard_native_test.go）只保留窗口循环与入口，并复用本
// 文件的 fixtureView/fixtureLog。
//
// 留痕字段与判定口径见 testdata/datagrid-keyboard/native/（judge 与
// RUNBOOK）。**不记录用户输入内容**（Editor 只记长度增量）。
//
// 几何口径（R2 修正）：sel-change/scroll 事件的 bodyHpx/rowHpx/headerHpx
// 一律取**当帧** gtx.Dp 换算值——不引用上一帧缓存，DPI 变化当帧即为
// 新值（无窗口回归 TestKBFixtureLogProducerPositive 守护）。

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// fixtureNavKeys 是观察用的导航键集（与组件消费集一致）。
var fixtureNavKeys = []key.Name{
	key.NameUpArrow, key.NameDownArrow, key.NameHome,
	key.NameEnd, key.NamePageUp, key.NamePageDown,
}

// fixtureGridRows 生成 rows 行可核验 RowID 数据（ROW-%05d，与 R27 原生
// 验收 RowID 形态一致，judge R1 据此校验）。
func fixtureGridRows(rows int) []DataGridRow {
	rs := make([]DataGridRow, rows)
	for i := range rs {
		rs[i] = DataGridRow{
			ID:    fmt.Sprintf("ROW-%05d", i),
			Cells: []string{fmt.Sprintf("ROW-%05d", i), "24 KB", "2026-11-10"},
		}
	}
	return rs
}

// fixtureGridColumns 是 fixture 列配置（gridA/gridB 同构）。
func fixtureGridColumns() []DataGridColumn {
	return []DataGridColumn{
		{Title: "ID", WidthDp: unit.Dp(200)},
		{Title: "大小", WidthDp: unit.Dp(120)},
		{Title: "修改时间", WidthDp: unit.Dp(160)},
	}
}

// fixtureBandBodyH 是 gridA 带高（dp）；gridA 实际 body viewport =
// fixtureBandBodyH − 表头（当帧 Dp 换算）。
const fixtureBandBodyH = 300

type fixtureLog struct {
	f      *os.File
	bw     *bufio.Writer
	seq    int
	frameN int
}

// newFixtureLog 创建独占日志文件。path 为空时返回仅内存计数的
// 日志（emit 仍推进 seq/frameN，供无窗口生产者测试复用真实路径）。
func newFixtureLog(path string) (*fixtureLog, error) {
	l := &fixtureLog{}
	if path == "" {
		return l, nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	l.f = f
	l.bw = bufio.NewWriter(f)
	return l, nil
}

// emit 经真实日志写入路径落一条事件（每条即 flush——强杀不丢帧）。
func (l *fixtureLog) emit(m map[string]interface{}) {
	if l == nil {
		return
	}
	l.seq++
	m["seq"] = l.seq
	m["frame"] = l.frameN
	m["tms"] = time.Now().Sub(time.Unix(0, 0)).Milliseconds()
	if l.bw == nil {
		return
	}
	b, _ := json.Marshal(m)
	l.bw.Write(b)
	l.bw.WriteByte('\n')
	l.bw.Flush()
}

func (l *fixtureLog) recordDestroy() {
	l.emit(map[string]interface{}{"event": "destroy-callback"})
}

func (l *fixtureLog) recordRunResult(runErr error) {
	l.emit(map[string]interface{}{"event": "run-return", "err": fmt.Sprint(runErr)})
	l.emit(map[string]interface{}{"event": "exit"})
}

// flush 落盘（exit 后由调用方 Sync/Close）。
func (l *fixtureLog) flushAndClose() {
	if l == nil || l.f == nil {
		return
	}
	l.bw.Flush()
	l.f.Sync()
	l.f.Close()
}

// fixtureView 是 fixture 的根 View：每帧布局三个焦点/对照带并记录状态。
// 窗口无关——离屏 harness（input.Router）可用真实事件驱动同一 Layout。
type fixtureView struct {
	gridA *DataGrid // 键盘导航 opt-in
	gridB *DataGrid // 禁用对照（永不消费导航键）
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
	lastSelB   string
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
	band(0, fixtureBandBodyH, func(gtx layout.Context) { v.gridA.Layout(gtx, th) })
	band(fixtureBandBodyH, 140, func(gtx layout.Context) { v.gridB.Layout(gtx, th) })
	band(fixtureBandBodyH+140, 60, func(gtx layout.Context) {
		v.ed.Update(gtx)
		material.Editor(th, v.ed, "editor (焦点消费者 2)").Layout(gtx)
	})

	// 几何留痕（R2 修正：全部取**当帧** Dp 换算值，不引用上一帧缓存）。
	bodyHpx := fixtureBandBodyH - gtx.Dp(unit.Dp(24))
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
			"bodyHpx": bodyHpx, "rowHpx": rowHpx, "headerHpx": headerHpx})
		v.lastSel = selStr
	}
	if v.gridA.ScrollOffset() != v.lastOff && v.lastOff >= 0 {
		v.log.emit(map[string]interface{}{"event": "scroll", "offset": v.gridA.ScrollOffset(), "sel": selStr, "selVisible": v.selVisible,
			"bodyHpx": bodyHpx, "rowHpx": rowHpx, "headerHpx": headerHpx})
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
	return layout.Dimensions{Size: gtx.Constraints.Max}
}
