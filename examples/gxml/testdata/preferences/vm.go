// Package preferences 是「设置编辑 → 校验 → 审阅 → 提交 / 撤销」完整工程的手写一半。
//
// 它与 testdata/forms（孤立输入框）与 testdata/workbench（列表 + 草稿）的区别：
//   - 两份**互相独立**的草稿字段：名称（Unicode + MaxLen）与端口（数字 Filter + MaxLen），
//     各自一份 <Input>、各自一个 ValueChanged 处理器，互不串写；
//   - 一次提交要**先过校验、再进审阅、最后确认**，撤销（Reset）把草稿恢复成已保存值；
//   - 只读 <Input> 只做「确认值」展示：它接了 ValueChanged，用来证明「值没变」
//     是 ReadOnly 生效的结果，而不是处理器没接线；
//   - 所有显示串（确认值 / 审阅信息 / 提示串 / 脏标记文案）都在**事件处理器**里
//     算好（refresh），不在 Layout 热路径逐帧拼接。
//
// 回写策略是**原样回写**：处理器把载荷直接赋给草稿，不做 Trim / 转大写之类的规范化。
// 规范化本身是幂等的，但当前同步算法下会让「编辑器显示」与「模型值」产生分歧
// （见 docs/05-开发/2026-11-02-D3-输入同步有界实验观察.md）；本例不把未经验证的
// 规范化写法藏进示例。
//
// 后台写入需要调用方自行同步：Invalidate 的并发安全不等于字段读写安全。
package preferences

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// 已保存值的初始状态。Reset 把草稿恢复到这两个值。
const (
	DefaultName = "默认名称"
	DefaultPort = "8080"
)

// 两个字段各自的静态上限，与 preferences.gxml 里的 MaxLen 逐字一致。
//
// Gio 的 Editor.MaxLen 按 **rune** 计数（不是字节、也不是 UTF-16 码元），
// 所以中文名称与 ASCII 的计数口径一致；断言请用 utf8.RuneCountInString。
const (
	NameMaxRunes = 12
	PortMaxRunes = 5
)

// 端口字段的静态字符集与取值范围。Filter 只允许数字，范围校验留给处理器。
const (
	PortDigits = "0123456789"
	PortMin    = 1
	PortMax    = 65535
)

// 脏标记的**两态文案**（逐字常量）。
//
// 自动测试逐字断言这两个值，绝不可以用 strings.Contains(文案, "未保存") 判断——
// "未保存" 三个字在两态里都出现，那样的判据永远不会红。
const (
	DirtyTextClean = "（无未保存修改）"
	DirtyTextDirty = "（草稿有未保存的修改）"
)

// ViewModel 由 UI 线程的事件处理器更新。
type ViewModel struct {
	// Name / Port 是两份**独立草稿**：各自绑定一个 <Input> 的 Value，
	// 只能被各自的 ValueChanged 回写。
	//
	// Name 走 MaxLen=12（rune 计数，可含中文）；Port 走 Filter=数字 + MaxLen=5。
	Name string
	Port string

	// SavedName / SavedPort 是**已保存值**：只有 Commit 才把草稿写进来，
	// Reset 把草稿恢复回去。它们是「有没有未保存改动」的唯一参照。
	SavedName string
	SavedPort string

	// ConfirmText 是只读 <Input> 的外部值（确认值）。
	// ReviewText 是审阅分支里的显示串。二者都由 refresh 计算。
	ConfirmText string
	ReviewText  string

	// Status / HasStatus 驱动结果提示的 If/Then/Else。
	Status    string
	HasStatus bool

	// InReview 驱动「编辑 / 审阅」分支：false 时只有「提交审阅」，
	// true 时才有「确认提交」与「取消」。
	InReview bool

	// Dirty 是**派生量**：草稿与已保存值不一致即有未保存改动。
	//
	// 写成派生而不是独立变量，是因为独立变量在 Commit / Reset / Cancel 每条
	// 路径上都得记得清空——漏一处就是静默失真。DirtyText 是它的显示串。
	Dirty     bool
	DirtyText string

	// Reviews / Commits / Cancels / Resets / StatusClears 是不可被后续事件覆盖的计数。
	//
	// 不能用 LastAction 统计次数：点按钮后紧随的外部值应用会再触发一次
	// ChangeEvent，把 LastAction 覆盖成 "…-change"。
	Reviews      int
	Commits      int
	Cancels      int
	Resets       int
	StatusClears int

	// Submits / LastSubmitPayload 记录名称框回车（Submit）的次数与载荷。
	//
	// 回车**不回写草稿**：Submit 的载荷只是"提交瞬间编辑器里有什么"，
	// 草稿仍是上一次 ValueChanged 写进去的值（契约 §4.2）。
	Submits           int
	LastSubmitPayload string

	// NameChanges / PortChanges / ConfirmChanges 记录三个 ValueChanged 的
	// 真实调用次数，LastXxxPayload 记录当次载荷。
	//
	// 注意：这些是「事件链路调用次数」，**不等于用户敲了几个字**——
	// 外部值应用调用 SetText 后，Gio 缓冲区的 Changed 标记会让紧随的 Update
	// 再产出一次 ChangeEvent；而 Filter 过滤掉全部字符时反而一次都不产出。
	// 自动测试一律取**增量**断言。
	NameChanges     int
	LastNamePayload string
	PortChanges     int
	LastPortPayload string
	ConfirmChanges  int

	// LastAction 记录最近一次事件，仅用于扫描时判断「点到了哪个按钮」；
	// 它会被后续事件覆盖，不要用它统计次数。
	LastAction string
}

// NewViewModel 构造初始状态：草稿与已保存值一致，因此初始不脏。
func NewViewModel() *ViewModel {
	m := &ViewModel{SavedName: DefaultName, SavedPort: DefaultPort}
	m.Name = DefaultName
	m.Port = DefaultPort
	m.refresh()
	return m
}

// refresh 在**事件处理器**里准备所有显示串，不在 Layout 中每帧格式化。
func (m *ViewModel) refresh() {
	// 脏标记派生：两份草稿只要有一份与已保存值不同就算脏。
	m.Dirty = m.Name != m.SavedName || m.Port != m.SavedPort
	if m.Dirty {
		m.DirtyText = DirtyTextDirty
	} else {
		m.DirtyText = DirtyTextClean
	}
	m.ConfirmText = "确认：" + m.Name + " / " + m.Port
	m.ReviewText = "审阅：名称=" + m.Name + "（" + strconv.Itoa(utf8.RuneCountInString(m.Name)) +
		" 字符），端口=" + m.Port
}

// validate 返回第一处校验失败的原因；合法返回空串。
//
// 校验**只读**草稿、不写任何状态：失败时调用方自己决定提示文案。
func (m *ViewModel) validate() string {
	if strings.TrimSpace(m.Name) == "" {
		return "名称不能为空"
	}
	if utf8.RuneCountInString(m.Name) > NameMaxRunes {
		return "名称超过 " + strconv.Itoa(NameMaxRunes) + " 个字符"
	}
	if strings.TrimSpace(m.Port) == "" {
		return "端口不能为空"
	}
	if strings.Trim(m.Port, PortDigits) != "" {
		return "端口只能包含数字"
	}
	n, err := strconv.Atoi(m.Port)
	if err != nil {
		return "端口不是有效数字"
	}
	if n < PortMin || n > PortMax {
		return "端口需在 " + strconv.Itoa(PortMin) + "-" + strconv.Itoa(PortMax) + " 之间"
	}
	return ""
}

// OnNameChanged 是名称 <Input> 的 ValueChanged：原样回写，不改端口。
func (m *ViewModel) OnNameChanged(value string) {
	m.Name = value
	m.NameChanges++
	m.LastNamePayload = value
	m.LastAction = "name-change"
	m.refresh()
}

// OnPortChanged 是端口 <Input> 的 ValueChanged：原样回写，不改名称。
//
// 载荷已经是 Filter + MaxLen 之后的编辑器文本：非法字符根本不会进到这里。
func (m *ViewModel) OnPortChanged(value string) {
	m.Port = value
	m.PortChanges++
	m.LastPortPayload = value
	m.LastAction = "port-change"
	m.refresh()
}

// OnConfirmChanged 是只读 <Input> 的 ValueChanged。
//
// 它**故意只计数、不改任何状态**：只读框的值由 refresh 派生，处理器再写回去
// 就会与派生结果打架。保留这条接线的意义是——只读框对真实 EditEvent 不变值，
// 只能归因于 ReadOnly，不能归因于「压根没接处理器」。
func (m *ViewModel) OnConfirmChanged(value string) {
	m.ConfirmChanges++
	m.LastAction = "confirm-change"
}

// OnNameSubmit 是名称框的 Submit（回车）处理器。
//
// 它必须与 BeginReview 分成两个方法：`Submit` 事件带一个 text 载荷，
// 而 `Click` 不带载荷——同一个方法无法同时满足两种签名（GXML-EVENT-004）。
// 载荷只记录、不回写（契约 §4.2：声明 Submit 不会自动写草稿）。
func (m *ViewModel) OnNameSubmit(text string) {
	m.Submits++
	m.LastSubmitPayload = text
	m.LastAction = "name-submit"
	m.beginReview()
}

// BeginReview 是「提交审阅」按钮：先校验，通过才进审阅分支。
func (m *ViewModel) BeginReview() {
	m.LastAction = "review-button"
	m.beginReview()
}

// beginReview 是「进入审阅」的唯一实现：校验不通过就停在编辑态。
func (m *ViewModel) beginReview() {
	if reason := m.validate(); reason != "" {
		m.Status = "校验未通过，未进入审阅：" + reason
		m.HasStatus = true
		m.LastAction = "review-reject"
		return
	}
	m.InReview = true
	m.Reviews++
	m.Status = "已进入审阅，确认后才会提交"
	m.HasStatus = true
	m.LastAction = "review"
}

// Commit 是「确认提交」：把草稿写入已保存值并退出审阅。
//
// 审阅分支里才看得到这个按钮，但校验仍然要重做一次——分支可见性不是校验。
func (m *ViewModel) Commit() {
	if reason := m.validate(); reason != "" {
		m.Status = "校验未通过，未提交：" + reason
		m.HasStatus = true
		m.LastAction = "commit-reject"
		return
	}
	m.SavedName = m.Name
	m.SavedPort = m.Port
	m.Commits++
	m.InReview = false
	m.Status = "已提交：名称=" + m.Name + "，端口=" + m.Port
	m.HasStatus = true
	m.LastAction = "commit"
	m.refresh()
}

// CancelReview 是「取消」：退出审阅，**保留草稿**（不回滚、不提交）。
func (m *ViewModel) CancelReview() {
	m.InReview = false
	m.Cancels++
	m.Status = "已取消审阅，草稿保留"
	m.HasStatus = true
	m.LastAction = "cancel"
}

// Reset 把两份草稿恢复成已保存值，并退出审阅。
func (m *ViewModel) Reset() {
	m.Name = m.SavedName
	m.Port = m.SavedPort
	m.InReview = false
	m.Resets++
	m.Status = "已重置为已保存值"
	m.HasStatus = true
	m.LastAction = "reset"
	m.refresh()
}

// ClearStatus 切回提示区的 Else 分支。
func (m *ViewModel) ClearStatus() {
	m.HasStatus = false
	m.StatusClears++
	m.LastAction = "clear-status"
}
