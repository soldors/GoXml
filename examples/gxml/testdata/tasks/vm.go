// Package tasks 是「列表 + 条件显示 + 行事件」入门工程的手写一半。
//
// 与 testdata/starter 的区别：
//   - 本例的列表带**行事件**，实参分别来自行内绑定（{task.ID}）与
//     <For Index="i">（行索引）；
//   - 显示字符串在事件处理器里准备（refresh），不在 Layout 热路径拼接；
//   - 条件显示分支由 ViewModel 的计算字段驱动（If/Then/Else）。
//
// 后台写入需要调用方自行同步：Invalidate 的并发安全不等于字段读写安全。
package tasks

import "strconv"

// Task 是列表中的一行。
//
// ID 必须**稳定且可比较**：<For Key="{task.ID}"> 用它作为行状态身份，
// 增删与重排时行状态跟随数据而不是跟随位置。
type Task struct {
	ID    int
	Title string
	Done  bool
}

// ViewModel 由 UI 线程的事件处理器更新。
type ViewModel struct {
	// Items 是 <For In="{Items}"> 的数据源。nil/空切片渲染为空。
	Items []Task

	// Summary 与 HasTasks 是**计算字段**：显示串在事件里准备好，
	// 避免在 Layout 中每帧格式化（性能预算 INV-PERF-2、PIT-006）。
	Summary  string
	HasTasks bool

	// ShowDone 控制统计区与"已完成"行的条件显示分支。
	ShowDone bool

	// LastAction 记录最近一次行事件，供自动测试断言"点到了哪一行"。
	LastAction string

	nextID int
}

// NewViewModel 构造初始数据并计算首个显示状态。
func NewViewModel() *ViewModel {
	m := &ViewModel{nextID: 4, ShowDone: true}
	m.Items = []Task{
		{ID: 1, Title: "读 GXML 语言规范", Done: true},
		{ID: 2, Title: "写列表示例"},
		{ID: 3, Title: "跑一次真实生成"},
	}
	m.refresh()
	return m
}

// refresh 在**事件处理器**里准备显示字符串，不在 Layout 中每帧格式化。
func (m *ViewModel) refresh() {
	m.HasTasks = len(m.Items) > 0
	done := 0
	for _, it := range m.Items {
		if it.Done {
			done++
		}
	}
	m.Summary = strconv.Itoa(done) + "/" + strconv.Itoa(len(m.Items)) + " 已完成"
}

// Toggle 是行事件：实参来自行内绑定 {task.ID}。
//
// 点第 N 行必须只切换第 N 行——这是列表正确性的分水岭
// （For 列表输出契约 §9 第 5 项）。
func (m *ViewModel) Toggle(id int) {
	for i := range m.Items {
		if m.Items[i].ID == id {
			m.Items[i].Done = !m.Items[i].Done
			m.LastAction = "toggle:" + strconv.Itoa(id)
			m.refresh()
			return
		}
	}
	m.LastAction = "toggle-miss:" + strconv.Itoa(id)
}

// RemoveAt 演示 <For Index="i">：实参是**行索引**而不是行内字段。
func (m *ViewModel) RemoveAt(index int) {
	if index < 0 || index >= len(m.Items) {
		m.LastAction = "remove-out-of-range"
		return
	}
	removed := m.Items[index]
	m.Items = append(m.Items[:index], m.Items[index+1:]...)
	m.LastAction = "remove:" + strconv.Itoa(removed.ID)
	m.refresh()
}

// ToggleFilter 切换条件显示分支（If/Then/Else）。
func (m *ViewModel) ToggleFilter() {
	m.ShowDone = !m.ShowDone
	m.LastAction = "filter:" + strconv.FormatBool(m.ShowDone)
}

// Add 追加一行：ID 由 ViewModel 自增，保证 Key 稳定唯一。
func (m *ViewModel) Add() {
	m.Items = append(m.Items, Task{ID: m.nextID, Title: "新任务 " + strconv.Itoa(m.nextID)})
	m.nextID++
	m.LastAction = "add"
	m.refresh()
}
