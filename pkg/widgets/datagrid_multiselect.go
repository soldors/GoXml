// DataGrid 多选（R29-B S3，显式 opt-in）。
//
// 多选模式在指针点击与键盘导航基础上扩展：Ctrl+点击切换选中、Shift+点击
// 范围选中、键盘 Shift+方向键从锚点扩展。选择集以稳定 RowID 表示，
// 重排（SortRows）按 ID 天然保留；缩短/清空（SetRows）后自动剔除已
// 不存在的 RowID。默认单选模式行为不受影响。
//
// 可视高亮在多选模式下由选择集驱动（selID 只作为 Shift 扩展锚点，不再
// 单独高亮）；单选模式仍由 selID 驱动——原行为不变。

package widgets

import "gioui.org/io/key"

// EnableMultiSelect 显式开启多选模式（默认关闭）。
// 开启时把现有单选（若有）迁移进选择集。可与键盘导航组合：同时开启
// EnableKeyboardNav 后，Shift+方向键从锚点扩展选择。
func (g *DataGrid) EnableMultiSelect() {
	g.multiSelEnabled = true
	if g.selSet == nil {
		g.selSet = make(map[string]bool)
	}
	// 迁移现有单选到多选集，锚点跟随。
	if g.selOK && g.selID != "" {
		g.selSet[g.selID] = true
		g.selAnchor = g.selID
	}
}

// DisableMultiSelect 关闭多选并清除选择集与锚点（单选状态保持不变）。
func (g *DataGrid) DisableMultiSelect() {
	g.multiSelEnabled = false
	g.selSet = nil
	g.selAnchor = ""
}

// MultiSelectEnabled 报告多选是否开启。
func (g *DataGrid) MultiSelectEnabled() bool { return g.multiSelEnabled }

// SelectedRowIDs 返回所有已选行的 RowID（按当前数据顺序排列）。
// 多选关闭时返回单选结果（0 或 1 个元素）。
func (g *DataGrid) SelectedRowIDs() []string {
	if !g.multiSelEnabled {
		if g.selOK {
			return []string{g.selID}
		}
		return nil
	}
	var out []string
	for _, row := range g.rows {
		if g.selSet[row.ID] {
			out = append(out, row.ID)
		}
	}
	return out
}

// commitPointerSelection 提交指针点击后的选择（仅多选模式被调用；
// Release 身份判定已通过）。单选模式行为与原实现一致——只改 selID/selOK。
//
//   - Ctrl+点击：切换该行选中（锚点移到该行）；
//   - Shift+点击：从锚点到该行的范围全部选中（锚点不动）；
//   - 普通点击：收拢为仅该行（锚点移到该行）。
func (g *DataGrid) commitPointerSelection(rowID string, mods key.Modifiers) {
	switch {
	case mods&key.ModCtrl != 0:
		g.multiSelectToggle(rowID)
		g.selID, g.selOK = rowID, true
		g.selAnchor = rowID
	case mods&key.ModShift != 0:
		g.multiSelectRange(rowID)
		g.selID, g.selOK = rowID, true // 锚点保持不变
	default:
		g.selSet = map[string]bool{rowID: true}
		g.selID, g.selOK = rowID, true
		g.selAnchor = rowID
	}
}

// commitKeyboardSelection 提交键盘导航一步后的选择（handleKeyboard 调用）。
// 单选模式行为与原实现完全一致；多选模式下 Shift 按住时从锚点扩展范围，
// 否则收拢为该行并移动锚点。
func (g *DataGrid) commitKeyboardSelection(newIdx int, extend bool) {
	id := g.rows[newIdx].ID
	g.selID, g.selOK = id, true
	if !g.multiSelEnabled {
		return
	}
	if !extend {
		g.selSet = map[string]bool{id: true}
		g.selAnchor = id
		return
	}
	// Shift 扩展：区间按（锚点→当前行）整体重算（替换式）——向下扩展
	// 增行、向上回撤减行，与列表控件通用语义一致。锚点失效时退化为单行。
	g.selSet = make(map[string]bool)
	lo, hi := newIdx, newIdx
	if ai, ok := g.rowIndexByID(g.selAnchor); ok {
		lo, hi = ai, newIdx
		if lo > hi {
			lo, hi = hi, lo
		}
	} else {
		g.selAnchor = id
	}
	for i := lo; i <= hi; i++ {
		g.selSet[g.rows[i].ID] = true
	}
}

// multiSelectToggle 切换指定行的选中状态（Ctrl+点击）。
func (g *DataGrid) multiSelectToggle(rowID string) {
	if g.selSet == nil {
		g.selSet = make(map[string]bool)
	}
	if g.selSet[rowID] {
		delete(g.selSet, rowID)
	} else {
		g.selSet[rowID] = true
	}
}

// multiSelectRange 把选择收拢为锚点到 rowID 的范围（按当前数据顺序，
// 含两端；替换式——与资源管理器语义一致）。锚点失效时退化为单行。
func (g *DataGrid) multiSelectRange(rowID string) {
	set := make(map[string]bool)
	spanned := false
	if ai, ok1 := g.rowIndexByID(g.selAnchor); ok1 {
		if ci, ok2 := g.rowIndexByID(rowID); ok2 {
			lo, hi := ai, ci
			if lo > hi {
				lo, hi = hi, lo
			}
			for i := lo; i <= hi; i++ {
				set[g.rows[i].ID] = true
			}
			spanned = true
		}
	}
	if !spanned {
		set[rowID] = true
	}
	g.selSet = set
}

// rowIndexByID 把 RowID 解析为当前数据索引：优先 idToIdx（键盘索引），
// 未构建时线性扫描兜底（点击是低频事件，一次性 O(N) 可接受）。
func (g *DataGrid) rowIndexByID(id string) (int, bool) {
	if g.idToIdx != nil {
		i, ok := g.idToIdx[id]
		return i, ok
	}
	for i := range g.rows {
		if g.rows[i].ID == id {
			return i, true
		}
	}
	return 0, false
}

// multiSelectInvalidate 剔除选择集中已不存在的 RowID（SetRows 显式更新
// 阶段调用；一次 O(N)）。
func (g *DataGrid) multiSelectInvalidate() {
	if !g.multiSelEnabled || g.selSet == nil {
		return
	}
	valid := make(map[string]bool, len(g.rows))
	for _, row := range g.rows {
		valid[row.ID] = true
	}
	for id := range g.selSet {
		if !valid[id] {
			delete(g.selSet, id)
		}
	}
}
