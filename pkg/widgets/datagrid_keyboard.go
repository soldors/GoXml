// DataGrid 键盘单选/纵向导航（TASK-WIDGET-001-3 键盘扩展，R28-B 候选）。
//
// # 候选语义（默认关闭；显式 EnableKeyboardNav 开启，未批准不入规范）
//
//   - opt-in：仅 EnableKeyboardNav 后注册按键过滤器；默认关闭路径不新增
//     键盘监听、不新增每帧分配、原「键盘不支持且不消费按键」行为不变；
//   - 焦点：过滤器全部限定 Focus=&g.tag——只有当前持有焦点的网格消费
//     按键（不用全局 catch-all、不逐帧夺焦、不吞其他控件/Modal 的键）。
//     获得焦点的候选路径 = 指针主键按压网格（点击聚焦）；焦点转给
//     Editor 等可聚焦消费者后网格自动停止消费；焦点状态经 Focused()
//     披露（诊断）；
//   - 按键集：Up/Down/Home/End/PageUp/PageDown。仅 key.Press 生效
//     （Release 消费后忽略；平台连发表现为连续 Press，每 Press 一步）；
//   - 导航按稳定 RowID 单选：当前选中行索引经 idToIdx（显式 SetRows /
//     EnableKeyboardNav 阶段一次性 O(N) 构建，计入成本）解析，不退化为
//     旧索引；重排/缩短后按当前数据邻接移动；
//   - 初始无选择：Up/Down/PageUp/PageDown 选中当前可见首行；Home=第 0
//     行、End=最后一行；首尾钳制（边界处按键=无操作且不重滚）；
//   - scroll-into-view：选择变化时把目标行滚入可见区（本帧生效——
//     键盘处理先于本帧可见窗口计算）；Home 滚到顶、End 滚到底；
//     PageUp/PageDown 按 page 行移动并滚入；
//   - 空 body（bodyH<=0）：选择仍按 RowID 更新，但不滚动、不产生任何
//     全表访问（访问窗口保持为空）——明示语义；
//   - 键盘选中不伪造鼠标点击：Clicked() 仍只反映指针点击；键盘只改选择。
//
// # 成本口径
//
//   - 启用后空闲帧：6 个 Focus 限定过滤器轮询 + 焦点状态读取，稳定帧
//     0 allocs/op（有门禁断言）；
//   - 索引 map：启用时随 SetRows / EnableKeyboardNav 一次性 O(N) 构建，
//     内存 ≈ 每 RowID 一个 map 条目；禁用时立即释放。
package widgets

import (
	"image"

	"gioui.org/io/key"
	"gioui.org/layout"
)

// DataGridKeyboardConfig 是键盘导航候选配置。零值=按可见行数翻页。
type DataGridKeyboardConfig struct {
	// PageRows 是 PageUp/PageDown 的移动行数；<=0 表示按当前视口可见
	// 行数（bodyH/rowH，至少 1）。
	PageRows int
}

// dgNavKeys 是导航键集（顺序即轮询顺序，与语义无关）。
var dgNavKeys = []key.Name{
	key.NameUpArrow,
	key.NameDownArrow,
	key.NameHome,
	key.NameEnd,
	key.NamePageUp,
	key.NamePageDown,
}

// EnableKeyboardNav 显式开启键盘单选/纵向导航（候选；默认关闭）。
// 开启时若已有数据则立即构建一次 O(N) RowID→索引映射（计入本轮显式
// 更新成本）；此后每次 SetRows 成功都会在同一次 O(N) 校验遍历中重建。
func (g *DataGrid) EnableKeyboardNav(cfg DataGridKeyboardConfig) {
	g.kbEnabled = true
	g.kbPageRows = cfg.PageRows
	g.buildRowIndex()
}

// DisableKeyboardNav 显式关闭键盘导航：停止注册按键过滤器并释放
// RowID→索引映射；选择状态保持不变。
func (g *DataGrid) DisableKeyboardNav() {
	g.kbEnabled = false
	g.idToIdx = nil
}

// KeyboardNavEnabled 报告键盘导航是否处于开启状态（候选）。
func (g *DataGrid) KeyboardNavEnabled() bool { return g.kbEnabled }

// Focused 报告最近一帧网格是否持有键盘焦点（仅开启时有意义；诊断用）。
func (g *DataGrid) Focused() bool { return g.kbFocused }

// buildRowIndex 一次 O(N) 构建 RowID→索引（显式更新阶段；计入成本）。
func (g *DataGrid) buildRowIndex() {
	if len(g.rows) == 0 {
		g.idToIdx = nil
		return
	}
	m := make(map[string]int, len(g.rows))
	for i := range g.rows {
		m[g.rows[i].ID] = i
	}
	g.idToIdx = m
}

// handleKeyboard 消费本帧发往本网格（须有焦点）的导航键。在可见窗口
// 计算之前调用：按键引起的滚动当帧生效（滚到目标不只是改 SelectedID）。
func (g *DataGrid) handleKeyboard(gtx layout.Context, viewport image.Point, rowH, headerH, bodyH, maxOff int) {
	if !g.kbEnabled {
		return
	}
	// focusable 资格登记 + 焦点状态披露（焦点本体由点击聚焦/外部转移）。
	gtx.Source.Event(key.FocusFilter{Target: &g.tag})
	g.kbFocused = gtx.Source.Focused(&g.tag)

	page := g.kbPageRows
	if page <= 0 {
		page = bodyH / rowH
		if page < 1 {
			page = 1
		}
	}
	firstVis := 0
	if bodyH > 0 {
		firstVis = g.scrollOff / rowH
	}

	// 按键过滤器轮询只在持焦时进行：未持焦时 Focus 限定过滤器本来就不
	// 匹配，注册只会让 Gio 全局 key.filter 切片每帧增长（实测 2.6KB/帧
	// 的隐性分配）。语义不变——未持焦时按键本就不属于本网格。
	if !g.kbFocused {
		return
	}

	for _, name := range dgNavKeys {
		for {
			ev, ok := gtx.Source.Event(key.Filter{Focus: &g.tag, Name: name})
			if !ok {
				break
			}
			e, isKey := ev.(key.Event)
			if !isKey || e.State != key.Press {
				continue // Release/非键事件消费后忽略
			}
			n := len(g.rows)
			if n == 0 {
				continue // 空表：明确无操作
			}
			sel := -1
			if g.selOK {
				if i, ok := g.idToIdx[g.selID]; ok {
					sel = i
				}
			}
			newIdx := sel
			switch e.Name {
			case key.NameDownArrow:
				if sel < 0 {
					newIdx = firstVis
				} else {
					newIdx = sel + 1
				}
			case key.NameUpArrow:
				if sel < 0 {
					newIdx = firstVis
				} else {
					newIdx = sel - 1
				}
			case key.NameHome:
				newIdx = 0
			case key.NameEnd:
				newIdx = n - 1
			case key.NamePageDown:
				base := sel
				if base < 0 {
					base = firstVis
				}
				newIdx = base + page
			case key.NamePageUp:
				base := sel
				if base < 0 {
					base = firstVis
				}
				newIdx = base - page
			default:
				continue
			}
			// 首尾钳制：越界移动在本行原地保持（不重滚、不换行）。
			if newIdx < 0 {
				newIdx = 0
			}
			if newIdx > n-1 {
				newIdx = n - 1
			}
			if newIdx == sel {
				continue
			}
			// 单选按稳定 RowID 提交（不伪造鼠标点击事件）。
			g.selID, g.selOK = g.rows[newIdx].ID, true
			// scroll-into-view（本帧生效；空 body 明确不滚动）。
			if bodyH > 0 {
				top := newIdx * rowH
				if top < g.scrollOff {
					g.scrollOff = top
				}
				if top+rowH > g.scrollOff+bodyH {
					g.scrollOff = top + rowH - bodyH
				}
				if g.scrollOff > maxOff {
					g.scrollOff = maxOff
				}
				if g.scrollOff < 0 {
					g.scrollOff = 0
				}
			}
		}
	}
}
