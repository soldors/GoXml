// Package workbench 是「可编辑列表 + 独立表单」工作台工程的手写一半。
//
// 它把 testdata/tasks（列表与行事件）与 testdata/forms（受控输入）组合起来：
//   - 列表用**稳定 ID** 作 <For Key>，行事件带的是 ID 而不是位置；
//   - 编辑器固定在 <For> 之外（v1 的行内 Editor 生命周期未定义，
//     Input 输出契约 §7），它只写**草稿**，不直接改列表；
//   - 「保存 / 回车提交」才把草稿写回被选中的那一项；空草稿拒绝保存；
//   - 删除**当前选中项**要清空选择与草稿；删除其它项不影响选择
//     （因为选择记的是 ID，删行不会让 ID 串行）；
//   - 显示串（选择信息 / 提示串）在事件处理器里准备，不在 Layout 拼接。
//
// 后台写入需要调用方自行同步：Invalidate 的并发安全不等于字段读写安全。
package workbench

import (
	"strconv"
	"strings"
)

// Item 是列表中的一行。
//
// ID 必须**稳定且可比较**：<For Key="{item.ID}"> 用它作为行状态身份，
// 增删与重排时行状态跟随数据而不是跟随位置。
type Item struct {
	ID    int
	Title string
}

// ViewModel 由 UI 线程的事件处理器更新。
type ViewModel struct {
	// Items 是 <For In="{Items}"> 的数据源。nil/空切片走空态分支。
	Items []Item

	// SelectedID 是当前选中项的 ID，**0 表示未选择**。
	//
	// 记 ID 而不是下标：删除前面的行时下标会串，ID 不会。
	SelectedID int

	// Draft 是 <Input Value="{Draft}"> 的外部值（草稿）。
	//
	// ValueChanged 只写它；写回 Items 由 Save / SaveSubmit 完成。
	Draft string

	// Selection / Status 是**计算字段**：显示串在事件里准备好。
	Selection string
	Status    string

	// CountText 是列表规模的计算字段（"共 N 项"）。
	//
	// 它是派生量、不是计数器：随时由 len(Items) 重算，不随事件累加，
	// 因此不存在"忘了在某条路径里更新"的问题。
	CountText string

	// Dirty 报告**草稿与选中行标题不一致**（有未保存的修改），
	// DirtyText 是它对应的显示串。
	//
	// 二者都在 refresh 里派生：Draft 只由 ValueChanged 写、只有保存才写回
	// Items，所以"两者是否相等"就是"有没有未保存改动"的**唯一真相**，
	// 不需要额外的脏标记变量去维护（多一个变量就多一处忘记清空的路径）。
	//
	// 页面只绑定 DirtyText（显示串不在 Layout 里现拼）；Dirty 是给断言用的
	// 机器可读形式——断言布尔值比断言一句文案健壮得多。
	Dirty     bool
	DirtyText string

	// HasSelection / HasItems / HasStatus 驱动 If/Then/Else 分支。
	HasSelection bool
	HasItems     bool
	HasStatus    bool

	// Saves / Adds / Deletes 记录各类操作的次数，供自动测试断言
	// 「保存真的发生了 / 没有发生」。
	Saves   int
	Adds    int
	Deletes int

	// Selects / Removes / StatusClears 与对应的 LastXxxID 是**不被后续事件覆盖**
	// 的计数：点击「选择」会改草稿，紧随的外部值同步会再触发一次 ChangeEvent，
	// 把 LastAction 覆盖成 "change"（R4 实测行为）。扫描要定位"点到了哪个按钮"
	// 只能靠这些计数器。
	Selects      int
	LastSelectID int
	Removes      int
	LastRemoveID int
	LastAddID    int
	StatusClears int

	// Changes / LastPayload 记录真实输入事件的回写次数与载荷。
	//
	// 注意：**外部值应用也会带来一次 ChangeEvent**（R4 实测：Gio 的
	// SetText 会置位缓冲区 Changed 标记，紧随的 Update 因此产出该事件），
	// 所以 Changes 是「事件链路调用次数」，不等于「用户敲了几个字」。
	Changes     int
	LastPayload string

	// LastAction 记录最近一次事件，供自动测试断言点到了哪一行/哪个按钮。
	LastAction string

	// nextID 单调递增：新增**永不复用**已存活或已删除的 ID。
	nextID int
}

// NewViewModel 构造初始数据并计算首个显示状态。
func NewViewModel() *ViewModel {
	m := &ViewModel{nextID: 4}
	m.Items = []Item{
		{ID: 1, Title: "读 GXML 语言规范"},
		{ID: 2, Title: "写列表示例"},
		{ID: 3, Title: "跑一次真实生成"},
	}
	m.refresh()
	return m
}

// refresh 在**事件处理器**里准备显示字符串，不在 Layout 中每帧格式化。
func (m *ViewModel) refresh() {
	m.HasItems = len(m.Items) > 0
	m.HasSelection = m.SelectedID != 0
	// CountText 是派生量：每次重算，不随事件累加。
	m.CountText = "共 " + strconv.Itoa(len(m.Items)) + " 项"
	// 脏标记同样派生：草稿 ≠ 选中行标题 ⇔ 有未保存改动。
	// 未选择时不存在"草稿属于哪一行"的参照，一律判为不脏。
	m.Dirty = m.HasSelection && m.Draft != m.titleOf(m.SelectedID)
	if m.Dirty {
		m.DirtyText = "（草稿有未保存的修改）"
	} else {
		m.DirtyText = "（无未保存修改）"
	}
	if m.HasSelection {
		m.Selection = "已选择 #" + strconv.Itoa(m.SelectedID) + "：" + m.titleOf(m.SelectedID)
		return
	}
	m.Selection = "（未选择，点一行的「选择」）"
}

// titleOf 返回某 ID 的标题；不存在返回空串。
func (m *ViewModel) titleOf(id int) string {
	for _, it := range m.Items {
		if it.ID == id {
			return it.Title
		}
	}
	return ""
}

// Select 是行事件：把该行填入草稿并显示选择信息。
//
// 它**只改草稿与选择**，不动 Items——这点与 Save 的区别是本工程的核心。
func (m *ViewModel) Select(id int) {
	m.Selects++
	m.LastSelectID = id
	if !m.hasID(id) {
		m.SelectedID = 0
		m.Draft = ""
		m.LastAction = "select-miss:" + strconv.Itoa(id)
		m.refresh()
		return
	}
	m.SelectedID = id
	m.Draft = m.titleOf(id)
	m.LastAction = "select:" + strconv.Itoa(id)
	m.refresh()
}

// hasID 报告某 ID 是否还在列表中。
func (m *ViewModel) hasID(id int) bool {
	for _, it := range m.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

// OnDraftChanged 是 ValueChanged 的处理器：载荷 = 编辑器当前文本。
//
// 这里**只写草稿**。写回 Items 必须走 Save / SaveSubmit。
func (m *ViewModel) OnDraftChanged(value string) {
	m.Draft = value
	m.Changes++
	m.LastPayload = value
	m.LastAction = "change"
	// 必须重算派生显示：草稿一变，脏标记就可能翻转。
	// 这也说明为什么 Dirty 要是**派生量**而不是独立变量——
	// 每条改 Draft 的路径都得记得维护它，漏一处就是静默失真。
	m.refresh()
}

// SaveSubmit 是 Submit 的处理器：载荷 = SubmitEvent.Text。
//
// 保存一律用**重求值后的 Draft**，不用事件实参——与契约 §5.2 路径 2 同源
// （实参是"用户想输入的"，Draft 才是当前外部值）。
func (m *ViewModel) SaveSubmit(text string) {
	m.LastPayload = text
	m.LastAction = "submit"
	m.save()
}

// Save 是「保存」按钮的处理器，与回车提交共用同一段保存逻辑。
func (m *ViewModel) Save() {
	m.LastAction = "save"
	m.save()
}

// save 是保存的唯一实现：空草稿 / 未选择一律拒绝，并给出条件提示。
func (m *ViewModel) save() {
	if m.SelectedID == 0 {
		m.Status = "未选择任何行，未保存"
		m.HasStatus = true
		m.LastAction = "save-no-selection"
		return
	}
	if strings.TrimSpace(m.Draft) == "" {
		m.Status = "草稿为空，未保存 #" + strconv.Itoa(m.SelectedID)
		m.HasStatus = true
		m.LastAction = "save-empty"
		return
	}
	for i := range m.Items {
		if m.Items[i].ID == m.SelectedID {
			m.Items[i].Title = m.Draft
			m.Saves++
			m.Status = "已保存 #" + strconv.Itoa(m.SelectedID) + "：" + m.Draft
			m.HasStatus = true
			m.refresh()
			return
		}
	}
	m.Status = "选中项已不存在，未保存"
	m.HasStatus = true
	m.LastAction = "save-missing"
}

// Add 追加一行：ID 由 ViewModel 自增，**永不复用**已存活过的 ID。
func (m *ViewModel) Add() {
	id := m.nextID
	m.nextID++
	m.Items = append(m.Items, Item{ID: id, Title: "新项目 " + strconv.Itoa(id)})
	m.LastAddID = id
	m.Adds++
	m.LastAction = "add:" + strconv.Itoa(id)
	m.refresh()
}

// Remove 是行事件：按 ID 删除。
//
// 删的是**当前选中项**就清空选择与草稿；删其它项不影响选择——
// 因为选择记的是 ID，不是下标。
func (m *ViewModel) Remove(id int) {
	m.Removes++
	m.LastRemoveID = id
	for i := range m.Items {
		if m.Items[i].ID == id {
			m.Items = append(m.Items[:i], m.Items[i+1:]...)
			m.Deletes++
			m.LastAction = "remove:" + strconv.Itoa(id)
			if m.SelectedID == id {
				m.SelectedID = 0
				m.Draft = ""
				m.Status = "已删除 #" + strconv.Itoa(id) + "，选择已清空"
				m.HasStatus = true
			} else {
				m.Status = "已删除 #" + strconv.Itoa(id)
				m.HasStatus = true
			}
			m.refresh()
			return
		}
	}
	m.LastAction = "remove-miss:" + strconv.Itoa(id)
	m.Status = "要删除的行已不存在"
	m.HasStatus = true
}

// ClearStatus 切回提示区的 Else 分支。
func (m *ViewModel) ClearStatus() {
	m.HasStatus = false
	m.StatusClears++
	m.LastAction = "clear-status"
}
