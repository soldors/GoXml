// Command demo 演示 Goxml 的端到端链路（旗舰样板）：
//
//	.gxml → Parser → Expander → Analyzer → Codegen → 真实窗口
//	→ 数据绑定 → 行内带参事件 → 帧内失效 → 后台 ticker（帧外失效）
//
// 页面由内嵌的 .gxml 源码走完整管线生成，再交给 pkg/runtime/app 渲染。
//
// 这是 demo，不是 CLI——没有目录发现、没有写盘、没有 watch。
//
// 交互能力（本切片）：
//   - 全局按钮：<Button Click="AddItem"/>（无参）
//   - 列表：<For Each="item" In="{Items}" Key="{item.ID}">
//   - 行内按钮：<Button Click="Select(item.ID)"/> / <Button Click="Delete(item.ID)"/>
//     ——带参 + 行状态池（每行独立的 widget.Clickable）
//
// demoView 与生成代码**同构**（见其注释），因为生成产物在本进程内只被打印、
// 不参与编译：解析生成源码再解释执行等于重写一个解释器，会掩盖 Codegen 的职责。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/internal/compiler/pipeline"
	"github.com/goxml/goxml/pkg/runtime/app"
)

// page 是演示页面：全局统计 + 增加按钮 + 行内选中/删除。
const page = `<Goxml Version="1" ViewModel="demoVM">
  <Page Route="/">
    <Text Value="{Title}"/>
    <Text Value="{Status}"/>
    <Button Text="增加一行" Click="AddItem"/>
    <Button Text="清空列表" Click="ClearAll"/>
    <If Condition="{HasItems}">
      <Then>
        <For Each="item" In="{Items}" Key="{item.ID}">
          <Text Value="{item.Name}"/>
          <Button Text="选中" Click="Select(item.ID)"/>
          <Button Text="删除" Click="Delete(item.ID)"/>
        </For>
      </Then>
      <Else>
        <Text Value="暂无数据——点「增加一行」开始"/>
      </Else>
    </If>

    <Text Value="--- Input 交互样板 ---"/>
    <Text Value="{EchoLabel}"/>
    <Input Value="{EchoText}" Hint="输入数字…" MaxLen="12" Filter="0123456789" ValueChanged="OnEchoChanged" Submit="OnEchoSubmit"/>
    <Text Value="{SubmitLabel}"/>
    <Button Text="程序设置输入框" Click="SetEchoFromProgram"/>
    <Input Value="{ReadOnlyText}" ReadOnly="true" Hint="只读示例"/>
  </Page>
</Goxml>`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

// demoItem 是列表元素。ID 是稳定身份（For 的 Key）。
type demoItem struct {
	ID   int
	Name string
}

// demoVM 是演示 ViewModel。
//
// ⚠️ 状态同步由调用方负责（状态更新契约 §2.3）：失效通知并发安全 ≠
// 字段读写并发安全。按钮处理器在 UI 线程、ticker 在后台 goroutine，
// 因此统一走 mutex。
type demoVM struct {
	mu   sync.Mutex
	next int
	// 绑定目标必须是**导出**字段（Analyzer 的 BIND-001 要求）。
	Items []demoItem
	Title string

	Status string
	// selected 是当前选中行的 ID（0 表示未选中）。
	// 不参与 .gxml 绑定，仅由 Title 反映。
	selected int
	// HasItems 是 <If Condition> 的绑定目标：列表非空。
	// 由增/删/清空统一维护，与 Items 严格同步。
	HasItems bool

	// ---- Input 样板（D5 手动验收入口）----
	// EchoText 是受控输入的外部值；EchoLabel 显示回写结果。
	EchoText    string
	EchoLabel   string
	SubmitLabel string
	// ReadOnlyText 是只读输入的外部值。
	ReadOnlyText string
	echoCount    int
}

func (m *demoVM) init() {
	m.next = 3
	m.Items = []demoItem{
		{ID: 1, Name: "第一行"},
		{ID: 2, Name: "第二行"},
		{ID: 3, Name: "第三行"},
	}
	m.HasItems = true
	m.EchoText = "12345"
	m.EchoLabel = "回写值：" + "12345" + "（长度 5）"
	m.SubmitLabel = "尚未提交"
	m.ReadOnlyText = "只读内容（不可编辑）"
	m.Title = m.summaryLocked()
	m.Status = "提示：点「增加一行」或行内按钮试试"
}

// summaryLocked 生成标题文本（调用方须持锁）。
func (m *demoVM) summaryLocked() string {
	return fmt.Sprintf("共 %d 行（选中 ID：%d）", len(m.Items), m.selectedLocked())
}

// selectedLocked 返回当前选中 ID（0 表示未选中）。
func (m *demoVM) selectedLocked() int { return m.selected }

// AddItem 是零参事件处理器（EVENT-007 要求无返回值）。
func (m *demoVM) AddItem() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	m.Items = append(m.Items, demoItem{ID: m.next, Name: fmt.Sprintf("第 %d 行", m.next)})
	m.Status = fmt.Sprintf("已新增 ID=%d", m.next)
	m.Title = m.summaryLocked()
}

// ClearAll 清空列表（零参事件）。清空后 <If Condition="{HasItems}"> 切到
// 空状态分支；行状态池会随下一帧的键清理收缩。
func (m *demoVM) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Items = nil
	m.HasItems = false
	m.selected = 0
	m.Status = "列表已清空（<If> 已切换到空状态分支）"
	m.Title = m.summaryLocked()
}

// Select 接收行 ID（带参事件）。
func (m *demoVM) Select(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selected = id
	m.Status = fmt.Sprintf("已选中 ID=%d", id)
	m.Title = m.summaryLocked()
}

// Delete 接收行 ID；删除后 Key 对应的行状态应被清理。
func (m *demoVM) Delete(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.Items[:0]
	for _, it := range m.Items {
		if it.ID != id {
			out = append(out, it)
		}
	}
	m.Items = out
	m.HasItems = len(m.Items) > 0
	if m.selected == id {
		m.selected = 0
	}
	m.Status = fmt.Sprintf("已删除 ID=%d，剩余 %d 行", id, len(m.Items))
	m.Title = m.summaryLocked()
}

// OnEchoChanged 是 ValueChanged 处理器：回写外部值。
//
// 注意：这里把 vm 值设成编辑器文本——这是"受控输入"的标准回写形态；
// 生成代码会在处理器后重求值 V 并更新 lastApplied（契约 §5.2 路径 2），
// demo 的手写同构 View 也必须照做。
func (m *demoVM) OnEchoChanged(v string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EchoText = v
	m.EchoLabel = fmt.Sprintf("回写值：%q（长度 %d）", v, len([]rune(v)))
}

// OnEchoSubmit 是 Submit 处理器：回车提交。
func (m *demoVM) OnEchoSubmit(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.echoCount++
	m.SubmitLabel = fmt.Sprintf("已提交 %d 次，最后提交：%q", m.echoCount, text)
}

// SetEchoFromProgram 演示「程序修改 Value → 编辑器同步」。
func (m *demoVM) SetEchoFromProgram() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EchoText = fmt.Sprintf("程序设置-%d", m.next)
	m.EchoLabel = fmt.Sprintf("回写值：%q（程序写入）", m.EchoText)
}

// echoText / echoLabelText / submitLabelText / readOnlyText 是加锁读取器
// （状态更新契约 §2.3：同步由调用方负责，这里统一加锁）。
func (m *demoVM) echoText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.EchoText
}

func (m *demoVM) echoLabelText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.EchoLabel
}

func (m *demoVM) submitLabelText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.SubmitLabel
}

func (m *demoVM) readOnlyText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ReadOnlyText
}

// SetStatus 由后台 ticker 调用（帧外失效路径）。
func (m *demoVM) SetStatus(s string) {
	m.mu.Lock()
	m.Status = s
	m.mu.Unlock()
}

// snapshot 供 Layout 读取：一次加锁取全部可见状态，避免同帧内撕裂。
func (m *demoVM) snapshot() (title string, items []demoItem, selected int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Title, append([]demoItem(nil), m.Items...), m.selected
}

// run 走完整管线并把结果接入窗口。
func run() error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	// SourceDir 指向 demo 目录：同包 ViewModel（demoVM）在这里解析。
	res := pipeline.Run("demo.gxml", []byte(page), pipeline.Config{
		ModuleRoot:  root,
		SourceDir:   filepath.Join(root, "examples", "demo"),
		PackageName: "main",
		SourcePath:  "demo.gxml",
	}, pipeline.Hooks{})
	if !res.OK() {
		return fmt.Errorf("编译失败:%s", diagSummary(res))
	}

	// 打印生成结果：证明管线确实产出了列表 + 行内事件接线。
	fmt.Printf("--- 生成 %s ---\n%s\n", res.Artifact.OutputName, res.Artifact.Source)

	vm := &demoVM{}
	vm.init()
	view := newDemoView(vm)

	a, err := app.NewApp(view, material.NewTheme(), app.Config{
		Title:  "Goxml Demo — 可交互动态列表",
		Width:  720,
		Height: 520,
	})
	if err != nil {
		return err
	}

	// 后台 ticker：帧外失效路径（App.Invalidate）。
	go func() {
		start := time.Now()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for range tick.C {
			vm.SetStatus(fmt.Sprintf("已运行 %d 秒（后台 goroutine + App.Invalidate）",
				int(time.Since(start).Seconds())))
			a.Invalidate()
		}
	}()

	return a.Run()
}

// demoView 与生成代码同构（<For> 列表契约 §3/§5、事件处理输出契约 §1/§2）。
//
// 逐项对应：
//   - 全局按钮 → 每按钮一个 Clickable 字段；
//   - <For> 内每按钮 → 一个 map[K]*widget.Clickable 行状态池（每按钮一池，
//     同一行的「选中」「删除」各自独立）；
//   - Layout 顶部数据驱动轮询 → 再绘制。
type demoView struct {
	vm *demoVM

	// 全局按钮（Layout 顶部直接轮询）
	clickAdd   widget.Clickable
	clickClear widget.Clickable

	// 行状态池：每按钮一池（Key = item.ID）
	rowSelect map[int]*widget.Clickable
	rowDelete map[int]*widget.Clickable

	// ---- Input 样板（与生成代码同构，D4-A/D4-B 形态）----
	clickSetEcho widget.Clickable
	input0       widget.Editor // Value="{EchoText}" + ValueChanged + Submit
	applied0     string        // lastApplied（路径 1/2）
	input1       widget.Editor // ReadOnly 示例
	applied1     string
}

func newDemoView(vm *demoVM) *demoView {
	if vm == nil {
		panic("demo: nil ViewModel")
	}
	return &demoView{vm: vm}
}

// rowClick 惰性创建行状态（与生成的 rowClick<N> 辅助同构）。
func rowClick(pool map[int]*widget.Clickable, key int) *widget.Clickable {
	c := pool[key]
	if c == nil {
		c = new(widget.Clickable)
		pool[key] = c
	}
	return c
}

// Layout 实现 app.View。
func (v *demoView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// ① 事件轮询：一切绘制之前（同帧强一致）
	for v.clickAdd.Clicked(gtx) {
		v.vm.AddItem()
		gtx.Execute(op.InvalidateCmd{})
	}
	for v.clickClear.Clicked(gtx) {
		v.vm.ClearAll()
		gtx.Execute(op.InvalidateCmd{})
	}
	for v.clickSetEcho.Clicked(gtx) {
		v.vm.SetEchoFromProgram()
		gtx.Execute(op.InvalidateCmd{})
	}

	title, items, selected := v.vm.snapshot()
	echoLabel, submitLabel := v.vm.echoLabelText(), v.vm.submitLabelText()

	// <If Condition="{HasItems}"> 的分支守卫：与渲染同源（同一字段读取，
	// 同一帧内无 yield 点，语义等价——If 输出契约 §2.3）。
	hasItems := v.vm.hasItems()

	// 行状态池惰性初始化（首帧）。
	if v.rowSelect == nil {
		v.rowSelect = make(map[int]*widget.Clickable)
		v.rowDelete = make(map[int]*widget.Clickable)
	}

	// ② 行内轮询：数据驱动，与渲染同源同序；整个 <For> 在 Then 分支内，
	// 因此轮询被条件守卫（If 输出契约 §4.2）。
	for _, item := range items {
		if !hasItems {
			break
		}
		if c := v.rowSelect[item.ID]; c != nil {
			for c.Clicked(gtx) {
				v.vm.Select(item.ID)
				gtx.Execute(op.InvalidateCmd{})
			}
		}
		if c := v.rowDelete[item.ID]; c != nil {
			for c.Clicked(gtx) {
				v.vm.Delete(item.ID)
				gtx.Execute(op.InvalidateCmd{})
			}
		}
	}

	// ③ 键清理：轮询之后、渲染之前（防无界增长）
	if len(v.rowSelect) > len(items) {
		keep := make(map[int]struct{}, len(items))
		for _, item := range items {
			keep[item.ID] = struct{}{}
		}
		for k := range v.rowSelect {
			if _, ok := keep[k]; !ok {
				delete(v.rowSelect, k)
			}
		}
	}
	if len(v.rowDelete) > len(items) {
		keep := make(map[int]struct{}, len(items))
		for _, item := range items {
			keep[item.ID] = struct{}{}
		}
		for k := range v.rowDelete {
			if _, ok := keep[k]; !ok {
				delete(v.rowDelete, k)
			}
		}
	}

	// ④ 渲染：内联展开子项（不嵌套 Flex）
	children := make([]layout.FlexChild, 0, 3+3*len(items))
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body1(th, title).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body2(th, v.vm.statusText()).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, &v.clickAdd, "增加一行").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, &v.clickClear, "清空列表").Layout(gtx)
		}),
	)

	// <If Condition="{HasItems}"> 内联展开：有数据 → For；无数据 → 空状态。
	if hasItems {
		for _, item := range items {
			item := item
			// 行标题：选中行加标记（不用颜色 API，保持最小依赖）
			label := item.Name
			if item.ID == selected {
				label = "▶ " + item.Name
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, label).Layout(gtx)
			}))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Button(th, rowClick(v.rowSelect, item.ID), "选中").Layout(gtx)
			}))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Button(th, rowClick(v.rowDelete, item.ID), "删除").Layout(gtx)
			}))
		}
	} else {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body1(th, v.vm.emptyText()).Layout(gtx)
		}))
	}

	// ---- Input 样板（D4-A/D4-B 同构形态）----
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body2(th, "--- Input 交互样板 ---").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body2(th, echoLabel).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.inputRow(gtx, th)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body2(th, submitLabel).Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, &v.clickSetEcho, "程序设置输入框").Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.readonlyRow(gtx, th)
		}),
	)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// inputRow 是第一个 Input：MaxLen/Filter + ValueChanged + Submit。
//
// 与生成代码同构（D4-A/D4-B）：
//   - 控件位置消费（不进顶部轮询）
//   - 路径 1：V != A 才 SetText
//   - 路径 2：处理器后重求值 V 并更新 A
func (v *demoView) inputRow(gtx layout.Context, th *material.Theme) layout.Dimensions {
	ed := &v.input0
	ed.SingleLine = true
	ed.MaxLen = 12
	ed.Filter = "0123456789"
	ed.Submit = true

	external := v.vm.echoText()
	if cur := external; cur != v.applied0 {
		ed.SetText(cur)
		v.applied0 = cur
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		switch e := ev.(type) {
		case widget.ChangeEvent:
			v.vm.OnEchoChanged(ed.Text())
			v.applied0 = v.vm.echoText()
		case widget.SubmitEvent:
			v.vm.OnEchoSubmit(e.Text)
			v.applied0 = v.vm.echoText()
		}
	}
	return material.Editor(th, ed, "输入数字…").Layout(gtx)
}

// readonlyRow 是只读示例。
func (v *demoView) readonlyRow(gtx layout.Context, th *material.Theme) layout.Dimensions {
	ed := &v.input1
	ed.SingleLine = true
	ed.ReadOnly = true
	if cur := v.vm.readOnlyText(); cur != v.applied1 {
		ed.SetText(cur)
		v.applied1 = cur
	}
	return material.Editor(th, ed, "只读示例").Layout(gtx)
}

// hasItems 读 <If Condition> 绑定目标。
func (m *demoVM) hasItems() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.HasItems
}

// emptyText 是空状态分支的文案。
func (m *demoVM) emptyText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return "暂无数据——点「增加一行」开始"
}

// statusText 读状态文本（单独加锁，避免与 snapshot 的锁交织）。
func (m *demoVM) statusText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Status
}

// moduleRoot 返回仓库根目录。
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod")
		}
		dir = parent
	}
}

// diagSummary 汇总诊断，便于失败时定位。
func diagSummary(r *pipeline.Result) string {
	var out string
	for _, d := range r.Diags {
		out += "\n  " + d.Code + ": " + d.Message
	}
	if out == "" {
		out = fmt.Sprintf(" status=%v", r.Status)
	}
	return out
}
