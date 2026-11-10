package widgets

// DataGrid（TASK-WIDGET-001-3 基础范围）：只读、固定行高、垂直虚拟滚动的
// 表格组件。不是 GXML 布局 Grid，不新增 GXML 标签或 Compiler 路径。
//
// # 契约要点（Windows 基础范围已批准，限制见 README.datagrid.md）
//
//   - 数据由调用方一次性提供（SetRows）：显示字符串必须由调用方预生成，
//     组件不在 Layout 内做任何字符串化（性能预算 §3.4 AP-3）；
//   - RowID 是调用方业务身份：选择与点击事件按 RowID 提交，滚动/重排后
//     不串给同索引的新行；空 ID 与重复 ID 在显式更新阶段整体拒绝
//     （fail-closed，报告明细），不会静默误选；
//   - 每帧只访问可见行 + 有界 overscan（dgOverscanRows），并把这批行
//     预热进绘制宏缓存；绘制只画可见行。访问/绘制集合经 LastFrame()
//     白盒披露（底层数组复用，稳定帧零分配）；
//   - O(N) 的 RowID 校验只发生在 SetRows 显式更新阶段并报告成本；
//     Rows() 是 O(1)，Layout 不做任何 O(N) 扫描；
//   - 事件模型：整个网格一个事件区（无逐行 Clickable，不重建）；
//     点击按「按压时单元格身份 == 释放时单元格身份」判定（跨帧
//     Press/Release 可用，Windows Release Buttons=0 可用），命中由本帧
//     几何解析（行=(y-表头+滚动偏移)/行高，列=显式列宽前缀和），
//     视口外/超出总列宽不命中；
//   - 尺寸/clip/命中区全部来自本帧约束并作用域配对，父容器偏移不泄漏；
//   - 键盘：首版不支持键盘选择/滚动（未注册任何按键过滤器，见
//     scope-matrix C-DG6）。
//
// # 性能（INV-PERF-2/3 对齐）
//
//   - 稳定帧 Layout 热路径 0 allocs/op：文本绘制宏按（RowID, 列）LRU
//     缓存（dgTextCacheMax 有界），稳定帧只回放 CallOp；
//   - 滚动帧只对新增可见行做一次宏录制（有界），10000/100000 行的
//     单帧行为只与可见区相关（INV-PERF-3）。
//
// 本组件持有单一 UI 线程所有者语义：全部状态由调用方在 UI 线程经
// Layout/显式更新方法推进，无全局状态、无跨实例缓存。

import (
	"container/list"
	"fmt"
	"image"
	"image/color"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// dgOverscanRows 是可见区外每侧预取的行数（有界 overscan）。
const dgOverscanRows = 2

// dgTextCacheMax 是绘制宏缓存的最大条目数（有界，LRU 淘汰）。
const dgTextCacheMax = 4096

// dgReportDuplicates 是更新报告中列出的重复 ID 上限（有界）。
const dgReportDuplicates = 8

// DataGridColumn 定义一列：显式列宽与表头标题。WidthDp 必须为正。
type DataGridColumn struct {
	Title   string
	WidthDp unit.Dp
}

// DataGridRow 是一行已准备好的显示数据。ID 必须非空且在数据集内唯一
// （稳定 RowID）；Cells 与列数等长，由调用方预生成显示字符串。
type DataGridRow struct {
	ID    string
	Cells []string
}

// DataGridUpdateReport 是 SetRows 显式更新阶段的结果与成本报告。
type DataGridUpdateReport struct {
	// OK 为 false 时更新整体被拒绝，数据保持原状（fail-closed）。
	OK bool
	// Rows 是更新后（或拒绝前扫描到的）行数。
	Rows int
	// ScannedIDs 是本次校验扫描的 RowID 数（O(N) 成本披露）。
	ScannedIDs int
	// Duplicates 列出至多 dgReportDuplicates 个重复 RowID。
	Duplicates []string
	// SelectionCleared 表示因选中行不在新数据中而清除了选择。
	SelectionCleared bool
	// KeyboardIndexBuilt/KeyboardIndexRows：键盘导航开启时本阶段在同一次
	// O(N) 遍历中重建 RowID→索引的披露（候选；关闭时恒 false/0）。
	KeyboardIndexBuilt bool
	KeyboardIndexRows  int
	// Elapsed 是校验阶段耗时（诊断用）。
	Elapsed time.Duration
}

// DataGridFrameStats 是最近一帧的访问/绘制白盒披露（测试与验收用）。
// Accessed/Drawn 的底层数组在帧间复用，调用方只读、不得持有。
type DataGridFrameStats struct {
	// First/Last 是本帧访问窗口的行索引（空数据为 -1）。
	First, Last int
	// Accessed 是本帧读取过身份的 RowID（可见+overscan，有界）。
	Accessed []string
	// Drawn 是本帧实际绘制的 RowID（仅可见行，有界）。
	Drawn []string
	// ScrollOffset 是帧末滚动偏移（px）。
	ScrollOffset int
}

// dgTextKey 是绘制宏缓存的键：稳定 RowID + 列号 + 像素尺寸
// （列宽/行高/DPI 变化后得到新键，旧条目经 LRU 淘汰；表头用空 RowID）。
type dgTextKey struct {
	rowID string
	col   int
	w, h  int
}

// dgTextEntry 是一条缓存的绘制宏与其 LRU 节点。
//
// call 指向本条目**私有持久 Ops**（ops 字段）中的录制区间——这是
// ADR-0009 实测确定的跨帧回放形态：Frame 级 ops 每帧新建，录制其中的
// CallOp 下一帧即失效；私有 Ops 随条目存活，回放由 CallOp.Add 跨缓冲
// 引用（对齐 codegen 绑定缓存槽位的实现）。
type dgTextEntry struct {
	ops  op.Ops
	call op.CallOp
	elem *list.Element
}

// DataGrid 是只读固定行高虚拟滚动表格。零值不可用，用 NewDataGrid 构造。
type DataGrid struct {
	tag overlayTag

	cols    []DataGridColumn
	rows    []DataGridRow
	rowH    unit.Dp
	headerH unit.Dp

	scrollOff int

	// 按压状态（跨帧；与 scrimState 同款约束）
	pressed    bool
	pid        pointer.ID
	pressRowID string
	pressCol   int
	pressValid bool

	selID string
	selOK bool

	clickedID  string
	clickedCol int
	clickedOK  bool

	// 绘制宏缓存（LRU 有界）
	texts map[dgTextKey]*dgTextEntry
	lru   *list.List

	// 可选自定义单元格绘制回调（逐可见单元格调用；设置后默认文本缓存
	// 不参与绘制，成本由回调自担）。
	Cell func(gtx layout.Context, row DataGridRow, col int, rect image.Rectangle) layout.Dimensions

	// 帧诊断（底层数组复用）
	colWidthsBuf []int
	stats        DataGridFrameStats

	// 键盘单选/纵向导航（R28-B 候选，默认关闭；实现见 datagrid_keyboard.go）
	kbEnabled  bool
	kbPageRows int
	kbFocused  bool
	idToIdx    map[string]int
}

// NewDataGrid 以固定行高构造表格（表头高度默认等于行高）。
func NewDataGrid(rowHeightDp unit.Dp) *DataGrid {
	g := &DataGrid{
		rowH:    rowHeightDp,
		headerH: rowHeightDp,
		texts:   make(map[dgTextKey]*dgTextEntry),
		lru:     list.New(),
	}
	g.tag.owner = g
	return g
}

// SetColumns 显式替换列配置。任一列 WidthDp<=0 时整体拒绝并保持旧配置
// （列配置错误有明确行为，不静默）。允许空列集合：渲染出有界的空表体。
func (g *DataGrid) SetColumns(cols []DataGridColumn) error {
	for i := range cols {
		if cols[i].WidthDp <= 0 {
			return fmt.Errorf("datagrid: 列 %d 的 WidthDp 必须为正，得到 %v", i, cols[i].WidthDp)
		}
	}
	g.cols = append([]DataGridColumn(nil), cols...)
	g.clearTextCache()
	return nil
}

// SetRows 显式替换数据集：本阶段做一次 O(N) RowID 校验（非空 + 唯一）。
// 校验失败时整体拒绝（数据与选择保持原状）并在报告中列明重复项；
// 成功时替换数据，若原选中行不在新数据中则清除选择并在报告中声明。
// 调用方在 SetRows 返回后不得再修改 rows 的底层数据（组件持有引用）。
func (g *DataGrid) SetRows(rows []DataGridRow) DataGridUpdateReport {
	start := time.Now()
	rep := DataGridUpdateReport{Rows: len(rows)}
	seen := make(map[string]struct{}, len(rows))
	var kbIdx map[string]int // 仅启用键盘导航时构建（同一次 O(N) 遍历）
	if g.kbEnabled {
		kbIdx = make(map[string]int, len(rows))
	}
	dup := 0
	for i := range rows {
		rep.ScannedIDs++
		id := rows[i].ID
		if id == "" {
			rep.Duplicates = append(rep.Duplicates, "")
			dup++
			if dup >= dgReportDuplicates {
				break
			}
			continue
		}
		if _, exists := seen[id]; exists {
			dup++
			if len(rep.Duplicates) < dgReportDuplicates {
				rep.Duplicates = append(rep.Duplicates, id)
			}
			if dup >= dgReportDuplicates {
				break
			}
			continue
		}
		seen[id] = struct{}{}
		if kbIdx != nil {
			kbIdx[id] = i
		}
	}
	rep.Elapsed = time.Since(start)
	if dup > 0 {
		rep.OK = false
		return rep
	}
	rep.OK = true
	g.rows = rows
	g.idToIdx = kbIdx
	rep.KeyboardIndexBuilt = kbIdx != nil
	rep.KeyboardIndexRows = len(kbIdx)
	g.clearTextCache()
	// 选中行不在新数据中：显式清除（不静默误选）。
	if g.selOK {
		if _, exists := seen[g.selID]; !exists {
			g.selOK = false
			g.selID = ""
			rep.SelectionCleared = true
		}
	}
	// 滚动偏移按新数据量重钳制（帧内也会钳）。
	return rep
}

// Rows 返回当前数据行数（O(1)，不做任何扫描）。
func (g *DataGrid) Rows() int { return len(g.rows) }

// SelectedRowID 返回当前选中行（按稳定 RowID）。
func (g *DataGrid) SelectedRowID() (string, bool) { return g.selID, g.selOK }

// Clicked 单次读取自上次读取以来的行点击结果（稳定 RowID + 列号）。
func (g *DataGrid) Clicked() (rowID string, col int, ok bool) {
	rowID, col, ok = g.clickedID, g.clickedCol, g.clickedOK
	g.clickedID, g.clickedCol, g.clickedOK = "", 0, false
	return rowID, col, ok
}

// ScrollOffset 返回当前滚动偏移（px，诊断用）。
func (g *DataGrid) ScrollOffset() int { return g.scrollOff }

// LastFrame 返回最近一帧访问/绘制白盒披露。返回值只读到下一次 Layout。
func (g *DataGrid) LastFrame() DataGridFrameStats { return g.stats }

// clearTextCache 清空绘制宏缓存（显式更新阶段调用；成本 = 重建可见区）。
func (g *DataGrid) clearTextCache() {
	g.texts = make(map[dgTextKey]*dgTextEntry)
	g.lru.Init()
}

// Layout 布局表格：事件区（点击/滚轮/取消）→ 表头（固定）→ 可见行绘制
// → 访问窗口（可见+overscan）预热与披露。关闭语义不存在——表格始终
// 渲染当前数据（空数据 = 仅表头的有界空表体）。
func (g *DataGrid) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// 自切片重置：保留已增长的底层数组容量（稳定帧零分配的关键——
	// 若从独立缓冲重置，容量每帧重新增长，恰产生 5+4=9 allocs/帧）。
	g.stats.Accessed = g.stats.Accessed[:0]
	g.stats.Drawn = g.stats.Drawn[:0]
	g.stats.First, g.stats.Last = -1, -1

	rowH := gtx.Dp(g.rowH)
	headerH := gtx.Dp(g.headerH)
	viewport := gtx.Constraints.Max
	if viewport.X <= 0 || viewport.Y <= 0 || rowH <= 0 {
		return layout.Dimensions{Size: image.Point{}}
	}

	bodyH := viewport.Y - headerH
	if bodyH < 0 {
		bodyH = 0
	}
	colWidths := g.columnWidthsPx(gtx)
	contentH := len(g.rows) * rowH
	maxOff := contentH - bodyH
	if maxOff < 0 || bodyH <= 0 {
		// 无可见体时无可滚动内容（R1：偏移钳到 0，不产生隐形滚动余量）。
		maxOff = 0
	}
	if g.scrollOff > maxOff {
		g.scrollOff = maxOff
	}
	if g.scrollOff < 0 {
		g.scrollOff = 0
	}
	g.stats.ScrollOffset = g.scrollOff

	// 1. 事件区与事件消费（先消费后绘制，Gio 惯用法）。
	g.handleEvents(gtx, viewport, rowH, headerH, bodyH, maxOff, colWidths)
	// 1b. 键盘导航（候选，默认关闭=直接返回）：在本帧可见窗口计算之前
	// 消费按键，使键盘引起的滚动当帧生效。
	g.handleKeyboard(gtx, viewport, rowH, headerH, bodyH, maxOff)

	// 2. 访问窗口：可见行 + 有界 overscan；绘制窗口：仅可见行。
	// bodyH<=0（无可见体：视口被表头占满/小于表头/零视口）时窗口必须为
	// 空——零访问零绘制，不靠最终 clip 裁剪像素（R1 修复 F-B1）。
	vis0, vis1 := 0, 0
	if bodyH > 0 && len(g.rows) > 0 {
		vis0 = g.scrollOff / rowH
		if vis0 > len(g.rows) {
			vis0 = len(g.rows)
		}
		vis1 = len(g.rows)
		if end := (g.scrollOff + bodyH + rowH - 1) / rowH; end < vis1 {
			vis1 = end
		}
	}
	// overscan 只在存在可见体时外扩；bodyH<=0 时访问窗口保持为空
	//（不加 2×overscan——那是 F-B1 的残留路径）。
	acc0, acc1 := vis0, vis1
	if bodyH > 0 {
		acc0 = vis0 - dgOverscanRows
		if acc0 < 0 {
			acc0 = 0
		}
		acc1 = vis1 + dgOverscanRows
		if acc1 > len(g.rows) {
			acc1 = len(g.rows)
		}
	}

	gridClip := clip.Rect(image.Rectangle{Max: viewport}).Push(gtx.Ops)
	defer gridClip.Pop()

	// 3. 表头（固定带，随点击几何一致：表头行不参与行命中）。
	g.drawHeader(gtx, th, headerH, colWidths, viewport.X)

	// 4. 访问窗口（读取身份、预热缓存）——只访问 [acc0, acc1)。
	for i := acc0; i < acc1; i++ {
		g.stats.First = acc0
		g.stats.Last = acc1 - 1
		g.stats.Accessed = append(g.stats.Accessed, g.rows[i].ID)
		for c := range g.cols {
			if g.Cell == nil {
				g.ensureCellMacro(gtx, th, g.rows[i], c, colWidths, rowH)
			}
		}
	}

	// 5. 绘制可见行（仅 [vis0, vis1)；自定义回调按单元格调用）。
	for i := vis0; i < vis1; i++ {
		top := headerH + i*rowH - g.scrollOff
		rowRect := image.Rectangle{
			Min: image.Pt(0, top),
			Max: image.Pt(min(colTotalWidth(colWidths), viewport.X), top+rowH),
		}
		g.stats.Drawn = append(g.stats.Drawn, g.rows[i].ID)
		if g.Cell != nil {
			x := 0
			for c := range g.cols {
				cellRect := image.Rectangle{
					Min: image.Pt(x, top),
					Max: image.Pt(min(x+colWidths[c], rowRect.Max.X), top+rowH),
				}
				if cellRect.Min.X >= rowRect.Max.X {
					break
				}
				g.Cell(gtx, g.rows[i], c, cellRect)
				x += colWidths[c]
			}
			continue
		}
		g.drawRow(gtx, th, g.rows[i], rowRect, colWidths, rowH)
	}
	return layout.Dimensions{Size: viewport}
}

// columnWidthsPx 换算列宽（O(列数)，与行数无关；缓冲复用）。
func (g *DataGrid) columnWidthsPx(gtx layout.Context) []int {
	if cap(g.colWidthsBuf) < len(g.cols) {
		g.colWidthsBuf = make([]int, len(g.cols))
	}
	w := g.colWidthsBuf[:len(g.cols)]
	for i := range g.cols {
		w[i] = gtx.Dp(g.cols[i].WidthDp)
	}
	return w
}

func colTotalWidth(widths []int) int {
	total := 0
	for _, w := range widths {
		total += w
	}
	return total
}

// drawHeader 绘制固定表头带（不随滚动；点击几何中表头不产生行事件）。
func (g *DataGrid) drawHeader(gtx layout.Context, th *material.Theme, headerH int, colWidths []int, viewportW int) {
	hdrClip := clip.Rect(image.Rectangle{Max: image.Pt(min(colTotalWidth(colWidths), viewportW), headerH)}).Push(gtx.Ops)
	defer hdrClip.Pop()
	paint.FillShape(gtx.Ops, dgColHeaderBg, clip.Rect(image.Rectangle{Max: image.Pt(min(colTotalWidth(colWidths), viewportW), headerH)}).Op())
	x := 0
	for c := range g.cols {
		cellRect := image.Rectangle{Min: image.Pt(x, 0), Max: image.Pt(x+colWidths[c], headerH)}
		e := g.textEntry(gtx, th, dgTextKey{rowID: "", col: c, w: colWidths[c], h: headerH}, g.cols[c].Title, colWidths[c], headerH)
		trans := op.Offset(image.Pt(cellRect.Min.X, 0)).Push(gtx.Ops)
		e.call.Add(gtx.Ops)
		trans.Pop()
		x += colWidths[c]
	}
}

// drawRow 绘制一行：缓存宏按列回放（稳定帧零录制）。
func (g *DataGrid) drawRow(gtx layout.Context, th *material.Theme, row DataGridRow, rowRect image.Rectangle, colWidths []int, rowH int) {
	rowClip := clip.Rect(rowRect).Push(gtx.Ops)
	defer rowClip.Pop()
	bg := dgColRowBg
	if g.selOK && row.ID == g.selID {
		bg = dgColRowSelectedBg
	}
	paint.FillShape(gtx.Ops, bg, clip.Rect(rowRect).Op())
	x := 0
	for c := range g.cols {
		if x >= rowRect.Max.X {
			break
		}
		e := g.textEntry(gtx, th, dgTextKey{rowID: row.ID, col: c, w: colWidths[c], h: rowH}, dgCellText(row, c), colWidths[c], rowH)
		trans := op.Offset(image.Pt(x, rowRect.Min.Y)).Push(gtx.Ops)
		e.call.Add(gtx.Ops)
		trans.Pop()
		x += colWidths[c]
	}
}

// dgCellText 返回单元格预生成显示文本（越界列为空串——列数与数据由
// 调用方保证一致，越界有界退路）。
func dgCellText(row DataGridRow, col int) string {
	if col < 0 || col >= len(row.Cells) {
		return ""
	}
	return row.Cells[col]
}

// ensureCellMacro 为访问窗口内的行预热绘制宏缓存（不绘制）。
func (g *DataGrid) ensureCellMacro(gtx layout.Context, th *material.Theme, row DataGridRow, col int, colWidths []int, rowH int) {
	g.textEntry(gtx, th, dgTextKey{rowID: row.ID, col: col, w: colWidths[col], h: rowH}, dgCellText(row, col), colWidths[col], rowH)
}

// textEntry 取（必要时录制）单元格绘制宏；LRU 有界淘汰。
func (g *DataGrid) textEntry(gtx layout.Context, th *material.Theme, key dgTextKey, text string, w, h int) *dgTextEntry {
	if e, ok := g.texts[key]; ok {
		g.lru.MoveToBack(e.elem)
		return e
	}
	e := &dgTextEntry{elem: g.lru.PushBack(key)}
	macro := op.Record(&e.ops)
	rec := gtx
	rec.Ops = &e.ops
	g.recordCell(rec, th, text, w, h)
	e.call = macro.Stop()
	g.texts[key] = e
	if g.lru.Len() > dgTextCacheMax {
		front := g.lru.Front()
		if front != nil {
			old := front.Value.(dgTextKey)
			delete(g.texts, old)
			g.lru.Remove(front)
		}
	}
	return g.texts[key]
}

// recordCell 在当前变换原点录制一个 (w,h) 单元格的绘制（背景+文本）。
func (g *DataGrid) recordCell(gtx layout.Context, th *material.Theme, text string, w, h int) {
	cellClip := clip.Rect(image.Rectangle{Max: image.Pt(w, h)}).Push(gtx.Ops)
	defer cellClip.Pop()
	col := op.Record(gtx.Ops)
	paint.ColorOp{Color: dgColText}.Add(gtx.Ops)
	textCall := col.Stop()
	inset := layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}
	l := widget.Label{}
	inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return l.Layout(gtx, th.Shaper, font.Font{}, unit.Sp(13), text, textCall)
	})
}

// handleEvents 消费整格事件区：按压/释放（跨帧、身份判定）、取消、滚轮。
func (g *DataGrid) handleEvents(gtx layout.Context, viewport image.Point, rowH, headerH, bodyH, maxOff int, colWidths []int) {
	defer clip.Rect(image.Rectangle{Max: viewport}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &g.tag)
	for {
		ev, ok := gtx.Source.Event(pointer.Filter{
			Target:  &g.tag,
			Kinds:   pointer.Press | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -maxOff, Max: maxOff},
		})
		if !ok {
			break
		}
		e, isPtr := ev.(pointer.Event)
		if !isPtr {
			continue
		}
		switch e.Kind {
		case pointer.Press:
			if e.Buttons&pointer.ButtonPrimary != 0 {
				g.pressed = true
				g.pid = e.PointerID
				g.pressRowID, g.pressCol, g.pressValid = g.cellAt(e.Position, viewport, rowH, headerH, colWidths)
				if g.kbEnabled {
					// 点击聚焦（候选）：主键按压在网格内任意位置（含表头/
					// 空白带）都请求键盘焦点——使「初始无选择」可达。
					gtx.Execute(key.FocusCmd{Tag: &g.tag})
				}
			}
		case pointer.Release:
			if g.pressed && g.pid == e.PointerID {
				rowID, col, ok := g.cellAt(e.Position, viewport, rowH, headerH, colWidths)
				// 身份判定：释放时单元格身份必须与按压时一致（滚动/重排
				// 后同位置是另一行——不串）。
				if ok && g.pressValid && rowID == g.pressRowID && col == g.pressCol {
					g.clickedID, g.clickedCol, g.clickedOK = rowID, col, true
					g.selID, g.selOK = rowID, true
				}
			}
			g.pressed = false
		case pointer.Cancel:
			g.pressed = false
		case pointer.Scroll:
			g.scrollOff += int(e.Scroll.Y) // 原生约定：正向下（见 R26 native 回归）
			if g.scrollOff > maxOff {
				g.scrollOff = maxOff
			}
			if g.scrollOff < 0 {
				g.scrollOff = 0
			}
		}
	}
}

// cellAt 把本组件局部坐标解析为（RowID, 列）：命中由本帧几何计算。
//
// 命中先判浮点边界再转整数（R1 修复 F-B2）：Release 事件会路由到按压
// 时进入的处理者、其位置可落在本帧视口之外——先按本帧 viewport/body
// 浮点比较拒绝越界（含 -0.1 这类截断后会折进界内的小数），再整数换算。
// 表头带、视口外（越出数据范围）、超出总列宽都不命中。
func (g *DataGrid) cellAt(pos f32.Point, viewport image.Point, rowH, headerH int, colWidths []int) (rowID string, col int, ok bool) {
	if pos.X < 0 || pos.Y < 0 || pos.X >= float32(viewport.X) || pos.Y >= float32(viewport.Y) {
		return "", 0, false
	}
	if pos.Y < float32(headerH) {
		return "", 0, false
	}
	y := int(pos.Y) - headerH
	row := (y + g.scrollOff) / rowH
	if row < 0 || row >= len(g.rows) {
		return "", 0, false
	}
	x := int(pos.X)
	if x < 0 || x >= colTotalWidth(colWidths) {
		return "", 0, false
	}
	for c := range colWidths {
		if x < colWidths[c] {
			return g.rows[row].ID, c, true
		}
		x -= colWidths[c]
	}
	return "", 0, false
}

// 单元格/表头固定配色（候选：不做主题化，ThemeRef 不在授权范围）。
var (
	dgColHeaderBg      = color.NRGBA{R: 0xE8, G: 0xE8, B: 0xE8, A: 0xFF}
	dgColRowBg         = color.NRGBA{R: 0xFA, G: 0xFA, B: 0xFA, A: 0xFF}
	dgColRowSelectedBg = color.NRGBA{R: 0xD6, G: 0xE4, B: 0xFF, A: 0xFF}
	dgColText          = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xFF}
)
