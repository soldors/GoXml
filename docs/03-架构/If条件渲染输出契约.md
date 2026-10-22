# `<If>` 输出契约

- 状态：**草案（待冻结）**
- 日期：2026-10-20
- 范围：**Codegen 侧的条���生成**；Parser/Expander/Analyzer 的既有校验不重复定义
- 关联：`GXML语言规范.md` §6.1、`For列表输出契约.md`、`事件处理输出契约.md`、
  `代码生成输出契约.md`

---

## 0. 边界与前置事实

### 0.1 分层现状（已核实的既有能力）

| 层 | 负责 | 状态 |
|---|---|---|
| Parser | `<If>` 结构：必须含且仅含一个 `<Then>`、最多一个 `<Else>`、不允许直接渲染子节点（SCOPE-002） | ✅ 已实现 |
| Analyzer | `Condition` 必须为布尔绑定（BIND-004）；解析为 `ResolvedAttr{Target: PropBool}`；递归遍历 `Then`/`Else` 子树 | ✅ 已实现 |
| **Codegen** | **分支渲染、分支内事件轮询守卫、动态判定** | ❌ **本契约** |

关键事实：`Condition` **已经**是 `ResolvedAttr`（与普通属性同构），
Codegen 用既有的 `lookupAttr(n, "Condition", facts)` 即可取到路径，
**无需任何事实扩展**——这是与 `<For>`（需要补 `ResolvedFor.In`）不同的地方。

### 0.2 已核实的数据结构

```go
type IfClause struct {
	Condition BindingVal   // 必须是布尔绑定
	Then      []*NodeIR    // <Then> 的内容
	Else      []*NodeIR    // <Else> 的内容，可为 nil
}
```

`Kind == KindIf`；`n.Children` 为空（内容在 Then/Else 里）。

---

## 1. 结构语义：同级内联展开（冻结）

与 `<For>` 同一原则（`For列表输出契约` §2）：`<If>` 的子项直接成为
**父容器的 `FlexChild`**，不包裹自己的 Flex。

```go
if cond {
	children = append(children, layout.Rigid(...))   // Then 的内容
} else {
	children = append(children, layout.Rigid(...))   // Else 的内容（可无）
}
```

**理由**（同 For §2.1）：嵌套 Flex 作为父容器的单个 `Rigid` 子项时，
宽度是内容宽而非父宽，交叉轴对齐会偏移；多套一层也打破 `Padding`/`Alignment`
的继承语义。

`<Then>`/`<Else>` 本身**不产生节点**——它们是分组标记，只贡献内容。

### 1.1 无 `<Else>` 时

条件为假 → **不追加任何子项**（不是追加零尺寸占位）。父容器的 `Gap`
因此也不会在空分支处产生额外间距。

---

## 2. 条件求值与"同源"规则（冻结）

### 2.1 渲染期

```go
cond := <condition 表达式>
if cond { ... }
```

条件**每帧求值一次**（在同一容器内），不重复求值。

### 2.2 轮询期（关键）

分支内的 `<Button>` 在 `Layout` 顶部轮询时**必须**带上同一个条件守卫：

```go
if <condition 表达式> {
	for v.click0.Clicked(gtx) { v.vm.Do(); gtx.Execute(op.InvalidateCmd{}) }
}
```

**为什么必须守卫**（不是可选优化）：

1. 条件为假时该分支**未渲染**，其 Clickable 未注册输入区域；
   不加守卫去轮询一个不存在的控件，语义上是"处理幻影事件"。
2. 条件在帧内翻转（处理器改了条件）时，若轮询与渲染对条件的判断不同源，
   会出现"渲染了但没轮询"或"轮询了但没渲染"的错配。

### 2.3 "同源"的可实现形态

`<If>` 比 `<For>` 简单：条件的路径根只有两种，二者规则不同但都无需提升变量。

| 条件根 | 轮询期表达式 | 渲染期表达式 | 处理 |
|---|---|---|---|
| `RootViewModel`（如 `{User.LoggedIn}`） | 原样 | 原样 | 两处**同一表达式**，可直接共享 |
| `RootForVar`（如 `{item.Done}`，If 在 For 内） | 根改写为轮询变量 `rowItem.Done` | `item.Done` | 复用 `<For>` 的 `rewriteRoot` |

> 两处各求值一次，但**同一表达式、同一帧、无 yield 点**，语义等价。
> 这与 `For列表输出契约` §5 修订后的口径一致。

---

## 3. 动态判定（冻结）

`<If>` 是动态节点：条件依赖运行时数据，两个分支的内容都可能变化。

```text
动态 ⇔ 交互控件 ∨ For ∨ **If** ∨ 事件 ∨ 属性绑定 ∨ 子节点动态
```

**必修**：`hasDynamic` 必须覆盖 `Kind == KindIf`。漏判会让整个分支被
冻进整页宏——条件切换时界面不动（与 `<For>` 漏判同类后果）。

---

## 4. 与 `<For>` 组合（冻结）

两种组合都必须支持，因为它们是真实界面的基本形态：

### 4.1 `<For>` 内的 `<If>`（行内条件渲染）

```xml
<For Each="item" In="{Items}" Key="{item.ID}">
  <Text Value="{item.Name}"/>
  <If Condition="{item.Done}">
    <Then><Text Value="已完成"/></Then>
  </If>
</For>
```

- 条件在循环作用域内解析（Analyzer 已支持：`RootForVar`）；
- 渲染期条件用 `item.Done`；
- 若 `<Then>`/`<Else>` 含按钮 → 行状态池照常（每按钮一池），
  但**轮询块内**的条件守卫必须改写成 `rowItem.Done`。

### 4.2 `<If>` 内的 `<For>`（空状态 / 列表开关）

```xml
<If Condition="{HasItems}">
  <Then><For Each="item" In="{Items}" Key="{item.ID}">...</For></Then>
  <Else><Text Value="暂无数据"/></Else>
</If>
```

- `<For>` 在分支内正常展开（内联、行状态池、键清理都不变）；
- 条件为假时该 `<For>` 的**轮询与清理也应跳过**（同 §2.2 理由）。

### 4.3 嵌套层级

MVP 支持任意层的 `If` 嵌套与 `If`/`For` 交替组合；多条件守卫按
**由外到内**顺序以 `&&` 连接。

---

## 5. 生成形态（冻结，示例）

```go
func (v *X) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// ① 轮询（顶部）：带条件守卫
	if v.vm.User.LoggedIn {
		for v.click0.Clicked(gtx) {
			v.vm.Logout()
			gtx.Execute(op.InvalidateCmd{})
		}
	}

	// ② 渲染：内联展开
	return func() layout.Dimensions {
		children := make([]layout.FlexChild, 0, 1)
		if v.vm.User.LoggedIn {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Button(th, &v.click0, "退出").Layout(gtx)
			}))
		} else {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, "未登录").Layout(gtx)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}()
}
```

---

## 6. 性能边界（如实声明）

| 项 | 说明 |
|---|---|
| 条件求值 | 每帧一次（渲染）+ 每帧一次（轮询守卫），均为字段读取，无分配 |
| 子项组装 | 与 `<For>` 相同：`[]FlexChild` 每帧分配，属**布局路径**分配，不属 `INV-PERF-4` 适用域；**不享有零分配保证**（`INV-PERF-2` 现行要求只覆盖静态页面） |
| 未选中的分支 | 不产生节点、不布局、不轮询——这是 `<If>` 相对「两个分支都渲染再隐藏」的实质收益 |

含 `<If>` 的页面**不得**计入 `INV-PERF-4` 的零分配证据范围（该不变量只覆盖文本路径）；在
`INV-PERF-2` 的零分配要求扩展到动态页面之前，同样**不得**将其计入 `INV-PERF-2` 的证据范围。
`[]FlexChild` 的分配属布局路径，不能据此判定违反 `INV-PERF-4`，也不能在无对应承诺时宣称违反
`INV-PERF-2`。上述排除是**口径边界**，不等于允许无条件分配，也不代表已证明性能合格。

---

## 7. 范围外（本轮不做）

- `<Else If>` 语法糖（用嵌套 `<If>` 代替）
- 条件表达式运算（`&&`/`!`/比较）——`Condition` 仍是单一路径绑定；
  `!` 前缀已由既有的 `ResolvedPath.Negated` 支持，不在本轮扩展
- 分支切换动画/转场
- 分支内的行级显示缓存

---

## 8. 验收标准

| # | 要求 | 证据形式 |
|---|---|---|
| 1 | 生成代码可编译 | 仓库内临时包 `go build` |
| 2 | 内联展开（分支内容直接进父 children，无嵌套 Flex） | 生成文本断言 |
| 3 | 条件为真渲染 Then、为假渲染 Else | 布局尺寸差异（不同文本 → 宽度不同） |
| 4 | 无 `<Else>` 时假分支不产生节点 | 高度为 0 |
| 5 | **分支内按钮仅在条件为真时可点击** | 真实指针点击 + 处理器计数 |
| 6 | `If` 在 `For` 内按行判定 | 真实点击：只有满足条件的行响应 |
| 7 | 含 `<If>` 的页面不得整页固化 | 断言无 `cacheCall` 回放 |

第 5 项是核心：它同时验证了 §2.2 的轮询守卫与渲染一致。

---

## 9. 未决项

| # | 项 | 归属 |
|---|---|---|
| 1 | `<Else If>` 链 | 后续语法糖 |
| 2 | 条件表达式运算（比较、布尔组合） | 语言扩展，需 Parser/Expander 支持 |
| 3 | 分支切换的焦点/输入状态清理 | 待 Input 控件 |
