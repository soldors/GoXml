package widgets

// R29-B R1 N1–N6 fixture：视图 + 离线回放（窗口无关，默认编译）。
//
// fixtureR29View 与 R28 fixtureView 平行（不动 R28 已验生产者）：
//   - gridA：10k 行（ROW-%05d），5 列 ×300dp（总宽 1500dp > 视口 → 横向
//     溢出），EnableHorizontalScroll + EnableKeyboardNav + EnableMultiSelect；
//   - gridB：同数据、无 opt-in 禁用对照；
//   - editor：真实 widget.Editor（第二焦点消费者）；
//   - 显式排序入口：视图级 P 键（Editor 持焦时由 Editor 消费、不触发），
//     调 SortRows 反序（ROW 降序），emit sort 事件携带排序前后行序样本。
//
// 日志不记录真实用户输入：无按键名/无文本内容；unconsumed-key 仅导航键
// （R28 同款，judge R4 消费）；edit-delta 仅字符计数。
//
// TestR29FixtureReplayN1N6 用同一事件脚本经离屏 input.Router 驱动同一
// Layout，产出与原生夹具相同生产者格式的日志（judge_r29_native_log.py
// 可回放判定）；-r29fix-out 指定输出目录（空 = t.TempDir）。
// 生命周期三事件（destroy-callback/run-return/exit）在离线回放中由同一
// 生产者函数合成优雅关闭序列——它证明 judge 链路与日志形态，不代表
// 原生窗口已执行（原生验收属协调者阶段）。

import (
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var r29FixOut = flag.String("r29fix-out", "", "R29 回放日志输出目录（空 = t.TempDir）")

// r29 夹具几何（dp；离线回放 PxPerDp=1 → px 同值）。
const (
	r29BandA    = 300 // gridA 带高
	r29BandB    = 140
	r29BandEd   = 60
	r29ColWidth = 300 // 单列宽；5 列总宽 1500dp
	r29ColCount = 5
	r29WinW     = 1200 // 视口宽（hMaxOff = 300dp）
	r29WinH     = 800
	r29SortKey  = "P" // 显式排序入口键
)

func r29Cols() []DataGridColumn {
	cols := make([]DataGridColumn, r29ColCount)
	for i := range cols {
		cols[i] = DataGridColumn{Title: fmt.Sprintf("列%d", i+1), WidthDp: unit.Dp(r29ColWidth)}
	}
	return cols
}

// fixtureR29View 是 N1–N6 夹具根 View：每帧布局三带并留痕。
type fixtureR29View struct {
	gridA *DataGrid // 横滚+键盘+多选 opt-in
	gridB *DataGrid // 禁用对照
	ed    *widget.Editor
	th    *material.Theme
	log   *fixtureLog

	lastFocus    string
	lastSel      string
	selVisible   bool
	lastOffX     int // -1 哨兵：首帧强制 emit
	lastOffY     int
	lastSelMulti []string
	lastEdLen    int
	lastW        int
	lastH        int
	lastDpi      float32
}

func newFixtureR29View(log *fixtureLog, rows int) *fixtureR29View {
	gridA := NewDataGrid(unit.Dp(24))
	if err := gridA.SetColumns(r29Cols()); err != nil {
		panic(err)
	}
	if rep := gridA.SetRows(fixtureGridRows(rows)); !rep.OK {
		panic("gridA 数据失败")
	}
	gridA.EnableHorizontalScroll()
	gridA.EnableKeyboardNav(DataGridKeyboardConfig{})
	gridA.EnableMultiSelect()

	gridB := NewDataGrid(unit.Dp(24))
	if err := gridB.SetColumns(r29Cols()); err != nil {
		panic(err)
	}
	if rep := gridB.SetRows(fixtureGridRows(rows)); !rep.OK {
		panic("gridB 数据失败")
	}

	return &fixtureR29View{
		gridA:     gridA,
		gridB:     gridB,
		ed:        &widget.Editor{},
		th:        material.NewTheme(),
		log:       log,
		lastOffX:  -1,
		lastOffY:  -1,
		lastEdLen: -1,
	}
}

// r29OrderSample 取当前数据序的头/尾各 n 个 RowID（排序后底层数组已被
// SortRows 原地重排——组件持有引用，视图读同一数组）。
func r29OrderSample(rows []DataGridRow, n int) []string {
	out := make([]string, 0, 2*n)
	for i := 0; i < n && i < len(rows); i++ {
		out = append(out, rows[i].ID)
	}
	for i := len(rows) - n; i < len(rows); i++ {
		if i >= 0 {
			out = append(out, rows[i].ID)
		}
	}
	return out
}

func r29EmitClick(l *fixtureLog, grid string, g *DataGrid, offX, offXMax, offY, offYMax, colWpx, viewWpx int, focus string) {
	if id, col, ok := g.Clicked(); ok {
		l.emit(map[string]interface{}{
			"event": "click", "grid": grid, "rowID": id, "col": col,
			"offX": offX, "offXMax": offXMax, "offY": offY, "offYMax": offYMax,
			"colWpx": colWpx, "viewWpx": viewWpx, "focus": focus,
		})
	}
}

func (v *fixtureR29View) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.log.frameN++
	w := gtx.Constraints.Max.X

	band := func(y, height int, body func(gtx layout.Context)) {
		tr := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		btx := gtx
		btx.Constraints = layout.Exact(image.Pt(w, height))
		body(btx)
		tr.Pop()
	}
	band(0, r29BandA, func(gtx layout.Context) { v.gridA.Layout(gtx, th) })
	band(r29BandA, r29BandB, func(gtx layout.Context) { v.gridB.Layout(gtx, th) })
	band(r29BandA+r29BandB, r29BandEd, func(gtx layout.Context) {
		v.ed.Update(gtx)
		material.Editor(th, v.ed, "editor (焦点消费者 2)").Layout(gtx)
	})

	// 焦点观察（bands 之后：当帧处理器已注册）。
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

	// 显式排序入口：P 键（Editor 持焦时 P 已被 Editor 消费；此处只在
	// 非编辑器持焦时触发）。排序前后行序样本入日志（judge R15）。
	for {
		ev, ok := gtx.Source.Event(key.Filter{Name: key.Name(r29SortKey)})
		if !ok {
			break
		}
		if ke, isKey := ev.(key.Event); isKey && ke.State == key.Press && focus != "editor" {
			before := r29OrderSample(v.gridA.rows, 5)
			v.gridA.SortRows(func(a, b DataGridRow) bool { return a.ID > b.ID })
			after := r29OrderSample(v.gridA.rows, 5)
			v.log.emit(map[string]interface{}{
				"event": "sort", "n": len(v.gridA.rows),
				"before-head": before[:5], "before-tail": before[5:],
				"after-head": after[:5], "after-tail": after[5:],
				"focus": focus,
			})
		}
	}

	// 几何留痕（当帧 Dp 换算）。
	bodyHpx := r29BandA - gtx.Dp(unit.Dp(24))
	rowHpx := gtx.Dp(unit.Dp(24))
	headerHpx := gtx.Dp(unit.Dp(24))
	colWpx := gtx.Dp(unit.Dp(r29ColWidth))
	offX := v.gridA.HScrollOffset()
	offXMax := r29ColCount*colWpx - w
	if offXMax < 0 {
		offXMax = 0
	}
	offY := v.gridA.ScrollOffset()
	offYMax := len(v.gridA.rows)*rowHpx - bodyHpx
	if offYMax < 0 {
		offYMax = 0
	}

	// 点击消费（一次性读取）→ click 事件（judge R14：列/RowID/偏移一致）。
	r29EmitClick(v.log, "A", v.gridA, offX, offXMax, offY, offYMax, colWpx, w, focus)
	r29EmitClick(v.log, "B", v.gridB, 0, 0, v.gridB.ScrollOffset(), len(v.gridB.rows)*rowHpx-(r29BandB-gtx.Dp(unit.Dp(24))), colWpx, w, focus)

	// 多选集观察（judge R16：集合逐段精确）。
	ids := v.gridA.SelectedRowIDs()
	if ids == nil {
		ids = []string{}
	}
	if !r29StringSliceEqual(ids, v.lastSelMulti) {
		lead := ""
		if sel, ok := v.gridA.SelectedRowID(); ok {
			lead = sel
		}
		v.log.emit(map[string]interface{}{
			"event": "sel-multi", "ids": ids, "count": len(ids), "lead": lead, "focus": focus,
			"bodyHpx": bodyHpx, "rowHpx": rowHpx, "headerHpx": headerHpx,
		})
		v.lastSelMulti = ids
	}

	// lead 选择变化（R28 sel-change 同款形态，judge R2 复用）。
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

	// 双轴偏移（judge R13：每事件单轴变化 + 钳制域）。
	if offX != v.lastOffX || offY != v.lastOffY {
		if v.lastOffX >= 0 || v.lastOffY >= 0 {
			v.log.emit(map[string]interface{}{
				"event": "axis", "offX": offX, "offY": offY, "offXMax": offXMax, "offYMax": offYMax,
				"prevX": v.lastOffX, "prevY": v.lastOffY, "focus": focus,
				"colWpx": colWpx, "viewWpx": w,
			})
		}
		v.lastOffX, v.lastOffY = offX, offY
	}

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

	// 泄漏观察：未被消费的导航键（R28 同款；不含字母键——排序键 P 由
	// 入口或 Editor 消费，不落日志）。
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

	if v.log.frameN%60 == 0 {
		v.log.emit(map[string]interface{}{"event": "heartbeat", "frameN": v.log.frameN})
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func r29StringSliceEqual(a, b []string) bool {
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

// r29Replay 用离屏 Router 驱动 fixtureR29View 执行 N1–N6 事件脚本。
// 事件几何与原生阶段同构（PxPerDp=1）。
type r29Replay struct {
	r      input.Router
	ops    op.Ops
	view   *fixtureR29View
	w, h   int
	frames int
}

func (p *r29Replay) frame(events ...event.Event) {
	p.frames++
	p.r.Queue(events...)
	p.ops.Reset()
	gtx := layout.Context{
		Ops:         &p.ops,
		Source:      p.r.Source(),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(p.w, p.h)),
	}
	p.view.Layout(gtx, p.view.th)
	p.r.Frame(&p.ops)
}

func (p *r29Replay) wheel(x, y float32, dx, dy int) {
	p.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(x, y), Scroll: f32.Pt(float32(dx), float32(dy))})
}

func (p *r29Replay) clickMods(x, y int, mods key.Modifiers) {
	p.frame(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Modifiers: mods, Position: f32.Pt(float32(x), float32(y))},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Modifiers: mods, Position: f32.Pt(float32(x), float32(y))},
	)
}

func (p *r29Replay) key(name key.Name, mods key.Modifiers) {
	p.frame(key.Event{Name: name, State: key.Press, Modifiers: mods})
}

func TestR29FixtureReplayN1N6(t *testing.T) {
	out := *r29FixOut
	if out == "" {
		out = t.TempDir()
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(out, "r29-replay.jsonl")
	log, err := newFixtureLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.flushAndClose()

	const rows = 10000
	view := newFixtureR29View(log, rows)
	p := &r29Replay{view: view, w: r29WinW, h: r29WinH}
	p.frame() // 初始帧（resize/axis 基线）

	// 行几何：rowH=24、header=24；gridA body 276px → 可见 11 行。
	// N1 双轴独立：Y 两步、X 两步（每事件单轴），再 Y 反向回顶
	//（后续点击与视觉行号对齐；反向步进同时验证轴内双向）。
	p.wheel(600, 150, 0, 200)
	p.wheel(600, 150, 0, 120)
	p.wheel(600, 150, 100, 0)
	p.wheel(600, 150, 0, 0) // 零步进帧（不产生 axis 事件）
	p.wheel(600, 150, 50, 0)

	// N1.5 焦点与精确回顶：表头点击持焦（不产生选择），Home 键盘回顶
	//（scroll-into-view → offY 精确为 0，DPI 无关；初始无选择时 Home
	// 选中第 0 行——R28 语义）。
	p.clickMods(600, 12, 0)
	p.frame()
	p.key(key.NameHome, 0)
	p.frame()

	// N2 滚后命中：offX=100、offY=0 时点 x=450（可见第 3 列中部）、
	// 视觉行 1（未排序：ROW-00001）。
	p.clickMods(450, 24+1*24+12, 0)

	// N3 排序命中：P 反序后点首行（ROW-09999）。
	p.key(key.Name(r29SortKey), 0)
	p.frame() // 排序生效帧
	p.clickMods(600, 24+12, 0)

	// N4 Ctrl 多选：普通点击视觉行 2 归位（收拢为 ROW-09997），Ctrl 行 4
	//（并集 {09997,09995}），Ctrl 行 2（切换撤销 → {09995}）。
	p.clickMods(600, 24+2*24+12, 0)
	p.clickMods(600, 24+4*24+12, key.ModCtrl)
	p.clickMods(600, 24+2*24+12, key.ModCtrl)

	// N5 Shift 范围：普通点行 1（锚点 ROW-09998）→ Shift 行 3 → 反向
	// Shift 行 0 → 普通点行 2 收拢。
	p.clickMods(600, 24+1*24+12, 0)
	p.clickMods(600, 24+3*24+12, key.ModShift)
	p.clickMods(600, 24+0*24+12, key.ModShift)
	p.clickMods(600, 24+2*24+12, 0)

	// N6 双实例/Editor：gridB 点击（对照单选）→ Editor 输入 → gridA
	// 点击收拢（焦点回网格、语义恢复）。
	p.clickMods(600, r29BandA+24+12, 0)
	p.r.Source().Execute(key.FocusCmd{Tag: view.ed}) // Editor 持焦
	p.frame()
	p.frame(key.EditEvent{Range: key.Range{}, Text: "ab"})
	p.frame()
	p.clickMods(600, 24+5*24+12, 0)

	// 合成优雅关闭（同一生产者函数；证明 judge 链路，不代表原生已执行）。
	log.recordDestroy()
	log.recordRunResult(nil)

	if log.frameN == 0 {
		t.Fatal("回放未产生帧")
	}
	t.Logf("回放完成：%d 帧 → %s", p.frames, logPath)
}
