# AOT 状态 Diff 语义规范

- 状态：冻结
- 冻结日期：2026-10-18
- 关联需求：REQ-003
- 关联 ADR：ADR-0003
- 适用范围：Analyzer、Codegen、生成代码、Runtime、Router、Widgets

## 1. 目标与不变量

Goxml 的状态更新采用编译期生成的浅层依赖 Diff 和局部 MacroOp 重绘。v1 必须满足：

1. 生成代码中不得引入 `reflect`。
2. 不复制完整 ViewModel，只保存 GXML 实际引用到的字段。
3. 相同绑定路径的变化必须产生确定的 Dirty 传播结果。
4. `<For>` 中的有状态控件必须按稳定 Key 保留身份。
5. 交互控件的输入处理不能被纯绘制 Macro 缓存绕过。
6. 首次渲染、卸载、重新挂载和路由切换的状态边界必须确定。
7. 渲染上下文变化必须使受影响的 Macro 失效，即使 ViewModel 没有变化。

## 2. 核心模型

### 2.1 ShadowState

生成代码不保存完整的 `oldVM`，而是根据绑定路径生成最小化的 `ShadowState`：

```go
type shadowState struct {
    initialized bool
    renderKey   renderContextKey

    userPresent bool
    userName    string
    loggedIn    bool

    items []itemShadow
}

type itemShadow struct {
    key  string
    name string
    id   string
}
```

ShadowState 只包含以下内容：

- 绑定路径实际访问的叶子字段。
- `<For>` 所需的 Key 和元素绑定字段。
- 交互控件对应的局部状态。
- 当前渲染上下文指纹。
- 是否完成过首次提交。

ShadowState 必须由 Codegen 生成，字段顺序和命名必须确定，不能通过运行时反射动态构造。

### 2.2 Diff 时机

每个页面实例按以下顺序运行：

1. 读取当前 ViewModel 和渲染上下文。
2. 与已提交的 ShadowState 比较。
3. 生成节点级 Dirty Mask。
4. 执行循环状态协调和生命周期变更。
5. 处理交互事件并记录必要的绘制 Macro。
6. 完成布局后提交新的 ShadowState。

只有成功完成当前帧的布局和必要缓存记录后，新的 ShadowState 才能替换旧状态。

## 3. 类型比较规则

| 类型 | v1 规则 |
|---|---|
| `bool`、整数、字符串 | 直接比较 |
| `float32`、`float64` | 分别使用 `math.Float32bits`、`math.Float64bits` 比较，避免 NaN 造成持续失效 |
| 指针 | 先比较 `nil` 状态，再比较绑定到的字段 |
| 嵌套 struct | 只比较实际绑定到的叶子字段 |
| Slice | 只允许作为 `<For In>` 数据源，生成元素快照 |
| Map | 不允许作为 `<For In>` 数据源或自动 Diff 的直接绑定值 |
| 函数、通道 | 不允许绑定 |
| 接口 | 必须在 Analyzer 阶段解析为明确的可比较类型，否则拒绝 |

### 3.1 指针与嵌套结构

对 `{User.Name}` 这类路径，生成代码必须先判断中间指针：

```go
newPresent := vm.User != nil
changed := old.userPresent != newPresent
if !changed && newPresent {
    changed = old.userName != vm.User.Name
}
```

`nil` 指针和零值对象不等价。整个未知 struct 不能被隐式深比较；如果组件需要传递复杂对象，必须拆成明确的属性或提供专用组件契约。

### 3.2 Slice 与 Map

`nil` Slice 和空 Slice 在 `<For>` 渲染语义中等价，都表示没有元素；作为未来的透明组件属性时不得自动套用该规则。

Map 在 v1 中不参与自动状态 Diff，原因是遍历顺序、变更边界和 Key 语义无法由通用编译器稳定推断。需要展示 Map 时，ViewModel 必须提供排序后的 Slice 或明确的 DTO 列表。

## 4. Dirty Mask 与依赖传播

Codegen 为每个节点生成静态依赖关系，并使用位掩码描述失效类型：

```go
type Dirty uint8

const DirtyNone Dirty = 0

const (
    DirtyPaint Dirty = 1 << iota
    DirtyLayout
    DirtyChildren
)
```

推荐的默认传播规则：

| 变化 | 失效类型 |
|---|---|
| `Text.Value` | `DirtyPaint`；文本尺寸可能变化时增加 `DirtyLayout` |
| `Text.Color` | `DirtyPaint` |
| `Padding`、`Gap` | `DirtyLayout` |
| `<If Condition>` | `DirtyChildren | DirtyLayout` |
| `<For In>` 或 Key 集合变化 | `DirtyChildren | DirtyLayout` |
| `Button.Enabled` | `DirtyPaint` |
| `Input.Value` | `DirtyPaint`；编辑器尺寸变化时增加 `DirtyLayout` |
| 主题颜色 | 相关节点 `DirtyPaint`；尺寸相关主题字段增加 `DirtyLayout` |
| `Align`、`Justify` | `DirtyLayout`（位置变化，尺寸可能不变） |
| `Width`、`Height`、`Columns` | `DirtyLayout | DirtyChildren`（可用空间变化） |

绑定路径按前缀传播。例如 `User` 指针变化时，所有 `User.Name`、`User.Enabled` 等后代绑定都视为可能变化。

如果 Codegen 无法证明变化只影响绘制，必须保守地向最近的布局祖先传播 `DirtyLayout`。

## 5. `<For>` 状态协调

### 5.1 Key 规则

包含 `Button`、`Input` 或其他有状态自定义组件的 `<For>` 必须提供 `Key`：

```xml
<For Each="item" In="{Items}" Key="{item.ID}">
    <Button Text="{item.Name}" Click="Select(item.ID)"/>
</For>
```

Key 必须是：

- 稳定的绑定路径。
- 编译期可证明为可比较类型。
- 在同一循环实例中唯一。

没有 Key 的循环只允许包含无状态节点，并按索引比较元素。

### 5.1.1 虚拟化与可见性窗口

当 `<For>` 启用 `Virtualize="true"` 时（见 [ADR-0006](adr/ADR-0006-列表虚拟化与静态子树宏固化.md)），行状态的创建与释放时机由**可见性窗口**决定，Key 语义与协调算法不变：

- 元素进入可见窗口（含预取边距）时创建行状态并执行 `OnMount`。
- 元素离开可见窗口时，按滚动窗口策略保留或释放；释放时执行 `OnUnmount`。
- 行状态必须与 Key 绑定，**滚动不得导致交互控件状态串位**。
- 未启用虚拟化时，行为与本文件既有条款完全一致。

虚拟化只改变「何时创建/释放」，不改变「如何比较」。任何试图借虚拟化绕过 Diff 或复用失效宏的实现都属于 P0 缺陷。

### 5.2 行状态

每个循环元素拥有独立的行状态：

```go
type rowState struct {
    key        ItemKey
    clickable  widget.Clickable
    input      widget.Editor
    paintMacro op.CallOp
    shadow     itemShadow
}
```

### 5.3 协调算法

当集合发生变化时，生成代码按以下步骤协调：

1. 从旧状态构建 `Key -> rowState` 索引。
2. 按新列表顺序读取 Key。
3. 相同 Key 复用原行状态。
4. 新 Key 创建行状态并执行 `OnMount`。
5. 不再出现的 Key 执行 `OnUnmount` 并释放行状态。
6. 只改变顺序时移动行状态，不重新创建控件。
7. 发现重复 Key 时返回确定性的 `duplicate key` 错误。

索引可以使用 Codegen 生成的具体 Key 类型 Map；不得使用反射型通用容器。

## 6. MacroOp 缓存边界

### 6.1 可缓存内容

以下内容可以缓存完整绘制 Macro：

- 静态文本和静态布局。
- 已完成状态比较且尺寸不变的纯绘制节点。
- 不包含输入处理的组件子树。

**静态子树固化是强制要求，不是可选项。** 判定条件：子树不含绑定、事件、条件与循环，且布局只依赖已纳入 `renderContextKey` 的上下文。实测依据（见 [性能预算与优化策略.md](性能预算与优化策略.md) §2.3）：

| 方案 | ns/op | allocs/op |
|---|---:|---:|
| 每帧新建闭包 | 28,000 | 51 |
| 编译期展开 + 固化 Macro | **2,550** | **0** |

注意：仅「预分配并复用切片」不足以解决问题（实测只省约 4%），必须消除闭包逃逸本身。

### 6.2 不可整体缓存的内容

`Button`、`Input` 和其他交互控件不能把事件处理和绘制整体缓存为一个永久回放 Macro。它们必须：

1. 每帧运行输入事件处理。
2. 复用独立的控件状态。
3. 在绘制内容未变化时只回放绘制 Macro。
4. 绘制相关绑定变化时重新记录绘制 Macro。

否则只调用 `op.CallOp` 可能导致 Gio 无法消费新的点击或输入事件。

## 7. 事件、线程与刷新

### 7.1 UI 线程事件

事件回调可以在 Gio UI 线程修改 ViewModel，但不得在回调中直接执行新的布局。流程固定为：

```text
事件发生
  -> 修改 ViewModel
  -> 标记状态待更新
  -> window.Invalidate()
  -> 下一帧执行 ShadowState Diff
  -> 按 Dirty Mask 重录必要 Macro
```

### 7.2 后台更新

后台 Goroutine 不得直接修改正在渲染的 ViewModel。必须通过运行时更新队列派发到 UI 线程：

```text
后台任务完成
  -> 投递状态更新函数
  -> window.Invalidate()
  -> UI 线程批量应用更新
  -> 下一帧执行 Diff
```

同一帧内的多个更新可以合并，只触发一次布局和绘制。

## 8. 生命周期与路由边界

- 首次渲染：所有节点视为 dirty，完整生成布局和绘制缓存。
- 条件分支从不可见变为可见：创建状态，执行 `OnMount`，然后布局。
- 条件分支被移除：取消子 Context，执行 `OnUnmount`，丢弃节点状态。
- 循环新增元素：创建行状态并执行 `OnMount`。
- 循环删除元素：执行 `OnUnmount` 后释放行状态。
- 路由切换：旧页面默认卸载，新页面重新挂载。
- 转场动画保留的只是旧页面的视觉快照，不保留旧页面的交互状态。
- v1 不提供隐式 KeepAlive；页面状态复用必须作为未来显式功能设计。

## 9. 渲染上下文指纹

ShadowState 必须保存 `renderContextKey`。以下任一项变化都会使受影响节点失效：

- Gio 约束尺寸。
- DPI 或字体度量。
- 当前主题版本。
- 语言或文本方向。
- 页面路由实例。
- 图片、字体等资源的加载状态。

ViewModel 不变不代表 Macro 一定有效；Macro 只能在绑定数据和渲染上下文都一致时复用。

## 10. 生成代码形态

Codegen 应生成类似以下接口，不得生成反射或通用深比较器：

```go
func (v *View) checkStateDiff(vm *ViewModel, rc renderContextKey) Dirty {
    var dirty Dirty

    if !v.shadow.initialized || v.shadow.renderKey != rc {
        dirty = DirtyPaint | DirtyLayout | DirtyChildren
    }
    if v.shadow.userName != vm.User.Name {
        dirty |= userNameDirty
    }
    if v.shadow.loggedIn != vm.LoggedIn {
        dirty |= loginBranchDirty
    }
    return dirty
}
```

生成源码必须满足：

- 不包含 `import "reflect"`。
- 不复制完整 ViewModel。
- 不使用运行时字段名、字符串路径或动态表达式求值。
- 对路径、中间指针和循环 Key 生成具体 Go 代码。
- 生成结果经过 `go/format.Source`。

## 11. 验收测试

实现必须覆盖以下场景：

1. 基础字段变化只刷新对应节点。
2. 中间指针从非空变为 nil，再从 nil 恢复。
3. 嵌套 struct 只比较实际绑定字段。
4. `nil` Slice 和空 Slice 不产生循环内容变化。
5. Slice 插入、删除、更新和重排。
6. 相同 Key 的 Button/Input 状态保持不变。
7. 重复 Key 被检测并返回确定错误。
8. 没有 Key 的有状态循环在 Analyzer 阶段失败。
9. Map 绑定在 Analyzer 阶段失败。
10. 主题、尺寸和 DPI 变化会使正确的 Macro 失效。
11. 交互控件每帧仍能接收点击和输入事件。
12. 条件分支和循环元素的 `OnMount`、`OnUnmount` 次数正确。
13. 路由切换不会复用已卸载页面的交互状态。
14. 生成代码 AST 检查确认不存在 `reflect`。
15. `go test -race` 下后台更新不会产生数据竞态。
16. 静态子树在稳定帧不重新记录宏（宏重录计数为 0）。
17. 静态页面稳定帧 `allocs/op == 0`。
18. 启用虚拟化时，单帧工作量只与可见元素数相关，与集合长度无关。
19. 虚拟化滚动过程中，带 Key 的交互控件状态不串位。

## 12. 版本演进边界

以下变更必须进入 v2 或新的 ADR：

- 支持 Map 的增量 Diff。
- 支持任意表达式或运行时计算。
- 支持隐式双向绑定。
- 支持动态组件和命名插槽。
- 支持页面隐式 KeepAlive。
- 改变 Key、nil/空集合或生命周期语义。
- 改变虚拟化的可见性窗口、行状态保留策略或静态子树固化边界。

v1 的 Parser、Analyzer、Codegen 和 Runtime 必须以本文件为 AOT 状态 Diff 的唯一语义依据。
