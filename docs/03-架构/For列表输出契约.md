# `<For>` 输出契约

- 状态：**草案（待冻结）**
- 日期：2026-10-19
- 范围：**Codegen 侧的列表生成**；Parser/Expander/Analyzer 的既有校验不重复定义
- 关联：`GXML语言规范.md` §6.2/§9、`ADR-0006`、`事件处理输出契约.md`、
  `代码生成输出契约.md`、`诊断码目录.md`

---

## 0. 边界与前置事实

### 0.1 分层现状

| 层 | 负责 | 状态 |
|---|---|---|
| Parser | `For` 结构、`Each`/`In` 必填、**SCOPE-003**（含状态控件但缺 Key）、SCOPE-002 | ✅ 已实现 |
| Expander | 属性分类 | ✅ |
| Analyzer | 可迭代性（**SCOPE-004**）、`ForVar` 作用域与类型、`Key` 路径解析（`ResolvedFor.Key`） | ✅ |
| **Codegen** | **列表渲染、行状态池、循环内事件、键生命周期** | ❌ **本契约** |

### 0.2 已核实的关键事实

1. **`Key` 已由 Parser 强制**：`containsStatefulNode` 的名单是 `"Button", "Input"`
   （`parser/control_flow.go:217`），缺 Key 直接报 SCOPE-003
   （`control_flow.go:201`）。因此"循环体含交互控件但没有 Key"这一状态
   **在 Codegen 阶段不可能出现**。
2. **Map 已被拒绝**：`iterableElemType` 现只接受 slice/array，报文
   `expects a slice or array, got X`；规范 §9 明文禁止 Map 作为 `<For In>`。
   （本契约起草前完成的前置修复。）
3. **`ResolvedFor` 已含 Key**：`Key *ResolvedPath`（可为 nil）。
4. **`pathExpr` 已支持 `RootForVar`**：绑定取值可生成 `item.Name`
   （`codegen/binding.go`）；但**事件实参**目前显式拒绝 ForVar，本契约要求解除。

### 0.3 规范留下的真空（本契约必须填补）

规范 §6.2 原文：

> 循环状态协调、`nil`/空 Slice 语义、**行状态生命周期**和 Macro 缓存边界在
> AOT Diff 尚未引入前**不构成当前运行时承诺**。

`TASK-STATE-001` S5 已裁决**不引入 AOT Diff**。因此这条"承诺留白"成为真空：
本契约必须自行定义 nil/空切片、行状态生命周期、键回收——这是 §4 的内容。

---

## 1. 数据源（冻结）

| 项 | 规则 |
|---|---|
| 允许类型 | slice、array（元素类型任意，只要绑定可解析） |
| 拒绝类型 | Map（规范 §9）、string、其他 —— 由 SCOPE-004 在 Analyzer 拦下 |
| `nil` slice | **渲染为空**，不 panic、不报错（Go 的 range 语义） |
| 空 slice | 同上 |
| 每次求值次数 | **每帧求值一次**，结果存入局部变量后供轮询与渲染**共用**（§5） |

`In` 表达式生成规则沿用 `pathExpr`（与属性绑定同源）。

---

## 2. 结构语义：内联展开而非嵌套容器（冻结）

规范 §3 明确 `<For>` **不产生新轴**，透传父容器的主轴/交叉轴。

因此 Codegen **必须内联展开**：`<For>` 的子项直接成为父容器的 `FlexChild`，
而不是包一层自己的 `Flex`。

### 2.1 为什么不能用嵌套 Flex 近似

嵌套 `Flex{Axis: 父轴, Gap: 父Gap, Alignment: 父对齐}` 在纯竖直列表上视觉等价，
但存在两处漂移：

- 作为父容器的**单个 `Rigid` 子项**，其宽度是**内容宽度**而非父宽，
  交叉轴对齐会偏移；
- `Padding`/`Alignment` 的继承语义会被多套一层打破。

内联展开没有这些问题。

### 2.2 代价（如实声明）

父容器一旦包含 `<For>`，其子节点数在编译期不可知，必须**运行时组装**：

```go
children := make([]layout.FlexChild, 0, len(items))
```

这是**布局路径的每帧分配**，不是文本缓存路径的分配。它不属于 `INV-PERF-4` 的适用域：该不变量
约束的是**文本节点**——绑定值未变化时不得重建字符串、不得重新 shaping，并以文本路径
`allocs/op == 0` 为断言。`[]layout.FlexChild` 的分配属于**布局路径**，在性能分类上与
`INV-PERF-2`（热路径零分配）讨论的是同一类问题；但 `INV-PERF-2` 现行的零分配要求**只明确
覆盖静态页面**，因此**不能据此直接判定**含 `<For>` 的页面违反该不变量。

换言之：**"属于某不变量讨论的领域"不等于"已经违反该不变量的现行要求"。** 含 `<For>` 的页面
属于**未纳入现有零分配承诺**的范围——既不能据此判定它违反 `INV-PERF-4`，也不能在无对应承诺的
情况下宣称它违反 `INV-PERF-2`。**"未纳入承诺范围"不等于"允许无条件分配"，更不等于"已证明性能
合格"**：本契约对该场景不作零分配承诺，只如实记录其分配来源。
本契约**不宣称**含 `<For>` 的页面享有零分配保证（见 §7）。

---

## 3. 行状态池（冻结）

### 3.1 何时需要

| 循环体内容 | `Key` | 行状态 | 生成字段 |
|---|---|---|---|
| 无交互控件（仅 Text 等） | 可选 | **没有** | 无 |
| 含 `Button` / `Input` | **强制**（SCOPE-003） | 每行一份 `widget.Clickable` | `map[K]*widget.Clickable` |

**不存在"按索引 Slice"的方案**：需要行状态时 Key 必然存在（Parser 保证），
用索引等于主动放弃已有的稳定身份，且会在增删重排时串位。

### 3.2 类型

```go
rowClicks map[K]*widget.Clickable   // K = Key 表达式的 Go 类型
```

`K` 由 `ResolvedFor.Key.Type` 决定。**必须是可比较类型**（Go map 键要求）；
不可比较（slice/map/func 等）时 Analyzer 应已有诊断；若走到 Codegen
则**显式报错**，不得生成无法编译的代码。

> `map[K]widget.Clickable` 不可用：**Go 的 map 元素不可寻址**，
> `&m[k]` 编译失败。必须用指针。

### 3.3 惰性创建

```go
func (v *X) rowClick(key K) *widget.Clickable {
	if v.rowClicks == nil {
		v.rowClicks = make(map[K]*widget.Clickable)
	}
	c := v.rowClicks[key]
	if c == nil {
		c = new(widget.Clickable)
		v.rowClicks[key] = c
	}
	return c
}
```

首帧为每行分配一次；稳定期查找零分配（map 命中）。

---

## 4. 键生命周期（冻结——填补 §0.3 的真空）

| 事件 | 行为 |
|---|---|
| 行新增 | 首次渲染时惰性创建 |
| 行删除 | **当帧清理**：map 中不在当前 keys 集合内的条目被 `delete` |
| 列表清空 | 全部清理；map 仍保留（容量复用），不作为泄漏 |
| Key 值变化（同一行换了身份） | 视为「删旧 + 增新」——旧状态丢弃，符合 Key 的语义 |
| 页面销毁 | 随 View 回收；不引入显式释放 API |

### 4.1 清理时机与实现约束

清理**必须**在「轮询之后、渲染之前」完成，理由：

- 轮询要能处理**本帧仍存在**的行的点击（先轮询，后清理，行还在）；
- 渲染要用最新池（清理后再取）。

实现不得每帧重建 map（那会丢失状态并产生分配）。做法：

```go
// 轮询之后：按当前 keys 标记，删除不在集合内的条目
if len(v.rowClicks) > len(items) {   // 快速跳过：池不比列表大就不可能有失效项
	keep := make(map[K]struct{}, len(items))   // 仅在此分支分配
	for _, item := range items { keep[item.ID] = struct{}{} }
	for k := range v.rowClicks {
		if _, ok := keep[k]; !ok { delete(v.rowClicks, k) }
	}
}
```

快速跳过条件保证**未发生删除的稳定帧零分配**。

---

## 5. 循环内的顶部轮询（冻结，同源同序是硬要求）

事件系统要求轮询在 `Layout` 顶部、绘制之前（`事件处理输出契约` §2）。
列表场景下轮询必须**数据驱动**——因为带参实参需要 `item`：

```go
// 轮询（Layout 顶部）：数据源各求值一次，但表达式与渲染**相同**
{
	rowItems := v.vm.Items
	for _, rowItem := range rowItems {
		rowItem := rowItem
		if c := v.rowClicks0[rowItem.ID]; c != nil {
			for c.Clicked(gtx) {
				v.vm.Select(rowItem.ID)     // 实参在轮询变量上求值
				gtx.Execute(op.InvalidateCmd{})
			}
		}
	}
}
```

**同源同序的真实含义**（实现期修订）：轮询在 Layout 顶部作用域、渲染在
容器闭包作用域——两个作用域**无法共享同一个局部变量**，因此「同一局部
变量」做不到字面意义。实际冻结为：

- **同源**：两处使用**同一数据源表达式**（都由 `ResolvedFor.In` 渲染）；
- **同序**：都是对切片的正序 range；
- **同根**：轮询与实参都在轮询变量（`rowItem`）上求值，渲染在
  `Each` 变量（`item`）上求值——两者来自同一份切片的同一元素。

串位风险的真正来源是「两次取数之间列表被并发修改」。而轮询与渲染之间
**没有 yield 点**（同一帧内顺序执行，无 goroutine 切换介入），因此该风险
在单帧内不存在。若未来引入跨 goroutine 的取数分离，必须重新审视本节。

`Index` 变量同理：轮询与渲染用同一个 `for i, item := range items`。

---

## 6. 循环作用域内的生成（冻结）

### 6.1 变量名

```go
for _, item := range items { ... }   // Each="item"
for i, item := range items { ... }   // 有 Index="i" 且循环体**引用**了 i
```

生成代码使用 `Each`/`Index` 的**原始名称**，与 `pathExpr` 的
`RootForVar` 分支一致（`binding.go` 用 `path.Root` 作基名）。

**`Index` 按引用生成**（步骤 1 的实测结论）：`Index` 是可选属性，声明了却
不引用是合法写法；而 Go 对 `for i, item := range` 中未使用的 `i` 会报
`declared and not used`（编译失败）。因此 Codegen 必须扫描循环体是否引用该
变量，未引用时生成 `for _, item := range`。

> 该问题由编译探测发现：`TestForIndexCompiles` 最初直接失败，报
> `gen.go:46:8: declared and not used: i`。该测试保留为回归防线。

若 `Each`/`Index` 与生成代码的其它标识符冲突（`v`、`th`、`gtx`、`items`、
`children`、`rec`、`k`、`ops`），Codegen **必须**报错——不得静默覆盖。
MVP 采用**显式报错**。

### 6.2 绑定取值

`{item.Name}` → `item.Name`（既有 `pathExpr` 已支持）。
`<For>` 内的绑定**不**引入新的显示缓存槽位语义：`bindText` 槽位按
`<For>` 节点（而非每行）分配——每行复用同一槽位会互相覆盖，因此
**`<For>` 内的绑定 Text 不使用槽位缓存**，直接布局。

> 这是与 §7 相关的第二处性能诚实边界：循环体内的文本每帧重新布局。

### 6.3 事件实参含 ForVar

`事件处理输出契约` §5.4.3 当前对 `RootForVar` 实参显式报错。
本契约要求**解除**：`Select(item.ID)` 生成 `v.vm.Select(item.ID)`，
因为 `item` 在轮询闭包作用域内可见。

---

## 7. 性能边界（如实声明，不夸大）

| 场景 | 保证 |
|---|---|
| 静态页（无绑定/无事件/无 For） | 整页固化，29 ns/op（既有测量，**仅指静态页**；对应 `INV-PERF-2` 的静态页面口径） |
| 绑定页稳定帧 | 0 allocs/op（既有测量，**仅指绑定页稳定帧的文本路径**；对应 `INV-PERF-4` 的文本路径口径，不含 `[]FlexChild`） |
| **含 `<For>` 的页面** | **不享有零分配**：`[]FlexChild` 每帧分配 + 循环体文本每帧布局（属**未纳入**现有零分配承诺，非"违反 INV-PERF-4"） |
| 含 `<For>` + 行交互 | 另加 map 查找（命中不分配） |

**不得**把含 `<For>` 的页面计入 `INV-PERF-4` 的零分配证据范围（该不变量只覆盖文本路径）；在
`INV-PERF-2` 的零分配要求扩展到动态页面之前，同样**不得**将其计入 `INV-PERF-2` 的证据范围。
上述排除是**口径边界**，不等于允许无条件分配，也不代表已证明性能合格。
后续若需优化，方向是列表指纹缓存或虚拟化（ADR-0006，v1.1），不在本切片。

---

## 8. 范围外（本轮不做）

- 虚拟化（`Virtualize` / `ItemExtent`）—— v1.1，ADR-0006
- Map 数据源（规范 §9 禁止）
- 嵌套 `<For>`（先按单层设计；内层需要 Key 与作用域栈）
- `<For>` 内的 `<If>` 组合（`<If>` 本身尚未实现）
- 行级显示缓存（§6.2/§7 已说明）
- 列表增删动画

---

## 9. 验收标准

| # | 要求 | 证据形式 |
|---|---|---|
| 1 | 生成代码可编译 | 仓库内临时包 `go build` |
| 2 | 内联展开（不引入嵌套 Flex） | 生成文本 + 父容器子项数断言 |
| 3 | 空/nil 切片渲染不 panic | 离屏渲染 + 尺寸断言 |
| 4 | `{item.X}` 绑定取到正确值 | 离屏渲染像素/文本断言 |
| 5 | 行按钮点击命中**正确行** | `input.Router` 合成点击 + 处理器记录 |
| 6 | 行删除后状态被清理 | 直接检查池长度（白盒，测试在 codegen 包内） |
| 7 | 稳定帧分配不得**优于**声明（即不得偷偷宣称零分配） | 基准测试如实记录数值 |
| 8 | 循环变量名冲突显式报错 | 错误信息断言 |

第 5 项是核心：**点第 3 行必须只触发第 3 行的处理器**——这是列表正确性的分水岭。

---

## 10. 未决项

| # | 项 | 归属 |
|---|---|---|
| 1 | `<For>` 与父容器 `Columns`/`Grid` 的组合语义 | 待 Grid 实现后 |
| 2 | 行内 `<Input>` 的受控语义 | 待 Input 事件 |
| 3 | 列表指纹缓存（恢复零分配） | 性能专项 |
| 4 | 嵌套 For 的作用域栈 | 后续 |
