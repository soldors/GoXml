# Input 输出契约

- 版本：**v1（冻结）**
- 冻结日期：2026-10-20；同步规则修订：2026-11-02（负责人明确批准用户编辑优先完整方案）
- 状态：**契约 v1 已冻结**——冻结范围见 §10。Schema/Analyzer/Codegen 已实现，
  已有自动化验证；D5-F 原生窗口/真实焦点/IME 人工验收未完成，不得据此标记正式验收通过。
  实现与验收范围以 [TASK-INPUT-001](../04-WBS/任务卡/TASK-INPUT-001.md) 为准。
- 依据：[INPUT-001-B-决策记录.md](../04-WBS/任务卡/INPUT-001-B-决策记录.md)
  （13 项裁决 + v2 修订）、[ADR-0011](adr/ADR-0011-Input受控输入与事件分派.md)
- 关联：[GXML语言规范.md](GXML语言规范.md)、
  [事件处理输出契约.md](事件处理输出契约.md)、
  [状态更新契约.md](状态更新契约.md)、
  [诊断码目录.md](诊断码目录.md)

---

## 1. 范围

本契约冻结 `<Input>` 在 v1 的**语言语义、生成规则、事件分派与状态同步算法**。
本文定义语义与算法；下列实现状态仅作追踪，不代替任务卡或人工验收。

| 项 | 状态 |
|---|---|
| 语义与算法 | **已冻结**（本文） |
| Schema/Analyzer/Codegen 实现 | 已实现，交付链见 TASK-INPUT-001 |
| 运行时行为验收 | 已有自动化/离屏验证；D5-F 原生窗口/真实焦点/IME 人工验收未执行 |

---

## 2. 属性（冻结）

| GXML 属性 | 类型 | 默认值 | 对应 Gio | 语义 |
|---|---|---|---|---|
| `Value` | 绑定或字面量（string） | 空串 | `Editor.SetText`（规则见 §5） | 编辑器的**外部值** |
| `Hint` | 字面量（string） | 空串 | `material.Editor` 的 `hint` | 占位提示 |
| `ReadOnly` | 静态 bool | `false` | `Editor.ReadOnly` | 只读 |
| `SingleLine` | 静态 bool | **`true`** | `Editor.SingleLine` | 单行模式 |

| `MaxLen` | 静态 int | `0`（不限） | `Editor.MaxLen` | 最大字符数（Gio 内置截断） |
| `Filter` | 字面量 | 空串（不过滤） | `Editor.Filter` | 允许字符集（Gio 内置过滤） |
| `Enabled` | 静态 bool | — | — | **v1 明确报错**（沿用 `<Button Enabled>` 裁决） |

> **`Submit` 不是属性**（2026-10-20 修订）：它只作为**事件**存在（§4.3）。
> 编辑器的提交能力由「是否声明 `Submit` 事件」自动决定，不提供独立的
> bool 属性。原因见 §2.3。

### 2.1 不定义的属性

- **`Text`**：与 `Value` 同义冲突，Gio 无此概念——**不定义**。
  出现即按未知属性诊断（ATTR 组）。

### 2.3 `Submit` 为何不设属性（修订记录）

初版契约曾把 `Submit` 同时定义为**静态 bool 属性**（§2）与**事件**（§4）。
Expander 的分类器按固定顺序判定：

```text
1. 全局布局属性 → 2. schema.Prop(name) → 3. schema.Event(name) → 4. 未知
```

**属性检查在事件之前**。同名时 `Submit="OnSubmit"` 会被当成 bool 属性解析
（类型错误），事件声明**永远不可达**——这是机械冲突，不是风格问题。

契约自身也已写明「声明 `Submit` 事件时 `Editor.Submit` 自动为 true」
（§2.2 原表、§4.3 矩阵），即该能力本来就是**事件声明的派生效果**。
因此裁决：**删除属性，只保留事件**。

| 方案 | 结论 |
|---|---|
| 删属性、只留事件 | **采纳**：契约自洽、分类器零改动、用户无需同时写两处 |
| 保留属性、事件改名 | 否决：与 Gio `SubmitEvent` 命名易混，且仍要改契约与规范 |
| 扩分类器支持同名 | 否决：需按值形态判定（方法名 vs `true`/`false`），引入隐式歧义，与「未知名称绝不退化为事件」的既有原则冲突 |

### 2.2 属性与 Gio 字段的对应表（供 C 阶段核对）

| Schema 属性 | Gio 字段/参数 | 备注 |
|---|---|---|
| `Value` | 不入字段，走 §5 同步算法 | 不是 `Editor` 的字段 |
| `Hint` | `material.Editor(th, ed, hint)` 第 3 参 | |
| `ReadOnly` | `Editor.ReadOnly` | |
| `SingleLine` | `Editor.SingleLine` | |

| `MaxLen` | `Editor.MaxLen` | |
| `Filter` | `Editor.Filter` | |

---

## 3. 绑定语义（冻结）

### 3.1 `Value` 是**外部值绑定**（单向）

```xml
<Input Value="{User.Name}"/>
```

`Value="{Path}"` 表示：**把 ViewModel 的当前值作为外部值显示到 Editor**。
方向是 **vm → Editor**，仅此一个方向。

### 3.2 不存在 GXML 层的自动双向绑定

- 回写**只能**经事件方法（`ValueChanged` / `Submit`）显式完成；
- 普通 `{Path}` 的语义**零改动**——本文不扩展表达式系统；
- 若未声明任何回写事件，该 Input 是**只读镜像**（外部值变化才同步）。

> 这与 v1 规范「不支持隐式双向绑定」（规范 §9）一致，不是新例外。

---

## 4. 事件（冻结）

### 4.1 事件声明

| GXML 声明 | 载荷 | 运行时事件源 | 生成的分派 |
|---|---|---|---|
| `ValueChanged="M"` | `value string` | `widget.ChangeEvent` | `v.vm.M(<载荷表达式>)` |
| `Submit="S"` | `text string` | `widget.SubmitEvent` | `v.vm.S(<载荷表达式>)` |

### 4.2 两条路径互不回退

- 只声明 `ValueChanged`：仅消费 `ChangeEvent`；`Editor.Submit` 保持 false。
- 只声明 `Submit`：仅消费 `SubmitEvent`；**不**回写值；`Editor.Submit = true`。
- 两者都声明：各自独立消费，各自调用各自的方法。
- 都不声明：不消费任何编辑事件（只读镜像形态）。

### 4.3 回车行为矩阵

`Submit` **只有事件形式，没有属性**（见 §2.3）。编辑器提交能力的开关即
「是否声明该事件」。

| `Submit` 事件声明 | `SingleLine` | 回车行为 |
|---|---|---|
| 是 | 任意 | 产生 `SubmitEvent`，调用 `S`；生成器置 `Editor.Submit=true` |
| 否 | `true`（默认） | **不产生 GXML 事件**；Editor 不插入换行（单行模式） |
| 否 | `false` | **不产生 GXML 事件**；Editor 插入换行 |

### 4.4 方法签名要求（由 Analyzer 校验，既有机制）

- `value`/`text` 载荷占**第 1 个**形参；
- 方法可以只有该 1 个形参，也可以在其后再接**调用点实参**
  （载荷在前、实参在后，契约 §8.4 R1 既有规则）；
- 方法**不得有返回值**（EVENT-007）；
- 载荷类别不兼容 → EVENT-006；数量不符 → EVENT-004。

---

## 5. 状态同步算法（冻结，实现不得简化）

本节是**唯一权威算法**。任何实现若偏离此处的更新路径与顺序，视为缺陷。

### 5.1 三个值

| 符号 | 名称 | 归属 | 定义 |
|---|---|---|---|
| `E` | Editor 当前文本 | `widget.Editor` | 用户实际看到的文本（含未回写的编辑） |
| `V` | 绑定值 | ViewModel 侧 | `Value="{Path}"` 在本帧的求值结果 |
| `A` | `lastApplied` | View（每个 Input 一个字段） | 同步镜像：外部应用后为原始V，处理器后为事件后Editor文本 |

### 5.2 两条 A 更新路径（2026-11-02 负责人批准）

路径1：事件循环结束后求值 V；若 V != A，则 SetText(V) 成功后 A=V。
路径2：真实 Change/Submit 处理器执行后，A=Editor.Text()（事件后的实际编辑器文本）。
不可使用用户企图输入的原始实参替代编辑器实际文本；MaxLen/Filter 可能已投影。
处理器若把模型归一化或不回写 Value，事件处理末尾的路径1让显示追随模型。
模型不可表示时 A 保存原始 V，Editor 显示 Gio 的可表示投影，quiet 帧不反复追赶。

### 5.3 逐帧执行顺序（固定）

```text
渲染到该 Input 时：
  ① 循环 Editor.Update(gtx) 至无事件
  ② ChangeEvent 且声明 ValueChanged → M(Editor.Text())；A=Editor.Text()
     SubmitEvent 且声明 Submit → S(SubmitEvent.Text)；A=Editor.Text()
  ③ 求值 V；若 V != A：Editor.SetText(V)；成功后 A=V
  ④ material.Editor(...).Layout(gtx)
```

Gio v0.10.3 的 Editor.Layout 内部排空③产生的程序化回显；初始/外部应用
**不触发 ValueChanged**，ReadOnly/Disabled 也保持静默。升级 Gio 须复核该行为。
不以文本相等推断事件来源，不使用跨帧门控，不丢弃整帧真实事件。

### 5.4 同帧冲突、提交时点与多控件收敛

同帧外部值变化和真实用户编辑冲突时，**用户编辑优先**。例如已有 OLD，
外部改 NEW 并队列插入 X，恒等处理器得到 XOLD；外部 NEW 可被覆盖。
处理器归一化后，本次 Layout 事件消费之后、Gio 布局之前显示追随模型；
SetText 可能复位光标。原生 IME/物理焦点仍须人工验收。

Submit 载荷为确认事件产生时的编辑器原文，不保证等于帧末归一化模型：
同帧 Edit abc + UpperText + Return 时 payload=abc，而帧末 E/V=ABC；
下一帧再 Return 则可提交 ABC。

共享字段的 Input 按布局顺序处理：后渲染控件改变模型时，先渲染控件在
下一帧收敛；不保证每帧末所有 Editor==V。稳定模型在下一帧逐控件收敛。
无 ValueChanged 处理器时，用户未回写的 draft 保留；它不属于投影例外。

### 5.5 光标/选区/IME 保留（显式规则）

`V == A` 时**不调用 `SetText`** ⇒ 光标、选区、IME 组合输入状态**全部保留**。

这不是副作用，而是契约保证的唯一手段——Gio 没有"只更新文本不动光标"的 API
（`SetText` 内部 `replace + SetCaret(0,0)`，见 A 阶段审计）。

### 5.6 非受控形态

未声明 `Value` 时：`V` 视为不存在，§5.3 的③跳过；Editor 自持文本。
此时 `A` 不参与；回写事件仍可声明（值由 `Editor.Text()` 取得）。

---

## 6. 控件自持事件（对事件契约的补充条款）

### 6.1 两类事件的区分（冻结）

| 类别 | 消费位置 | 判断标准 | 当前成员 |
|---|---|---|---|
| **顶部轮询事件** | `Layout` 顶部，所有绘制之前 | 消费只需输入区域命中，不依赖布局上下文 | `<Button>` 的 `Click` |
| **控件自持事件** | 控件自身的 `Update`/`Layout` 调用内 | 消费需要控件自身的布局上下文（焦点 tag、输入区域、IME 状态） | `<Input>` 的 `ChangeEvent`/`SubmitEvent` |

### 6.2 为什么 Editor 必须自持

【外部事实】`widget.Editor.Update(gtx)` 依赖本帧 `Layout` 注册的焦点 tag 与
`gtx.Source`；把消费提到 `Layout` 顶部会导致：焦点 tag 尚未注册 → 事件无法
路由；IME 组合状态与布局上下文错位。

### 6.3 对《事件处理输出契约》§2 的修订

> **§2 的"轮询置于 Layout 顶部"规则不适用于控件自持事件类别。**
> 需要控件自身布局上下文的事件，一律在控件位置消费。
> 该类别在 C 阶段正式写入事件契约。

---

## 7. For 内 Input：编译期拒绝（冻结）

### 7.1 规则

`<For>` 子树内出现 `<Input>` → **Parser 报错**，不论 `Key` 是否存在。

### 7.2 诊断

| 项 | 值 |
|---|---|
| 码 | `GXML-SCOPE-005` |
| 归属阶段 | Parser |
| 触发 | `containsInputNode(n.For.Children)`（含间接子树与嵌套 For） |
| 消息 | `<For> contains <Input>, which is not supported in v1; input controls must be static (outside <For>) until per-row editor lifecycle is specified` |

### 7.3 与 `GXML-SCOPE-003` 的关系（不重叠）

| 输入 | 命中 |
|---|---|
| For + 无 Key + Input | **SCOPE-003**（既有规则优先，不叠加 005） |
| For + 有 Key + Input | SCOPE-005 |

【项目实测】有 Key + Input 当前**零诊断**（v1 放行），这正是本规则要补的空隙。

### 7.4 登记状态

`GXML-SCOPE-005` 取号已裁决（诊断码目录 §4 空置 001、005–010；
§5 取最小空置号）。**C 阶段按 §5 维护流程正式登记后，本码才生效**；
登记前实现不得引用为"已登记码"。

### 7.5 Codegen 兜底

Codegen 对 `<Input>` 的既有拒绝（default 分支）**保留为防御层**，
正常管线不可达（Parser 已拦截）。

---

## 8. 与既有契约的关系

| 契约 | 关系 |
|---|---|
| `事件处理输出契约` §2 | §6.3 对其补充（控件自持类别） |
| `事件处理输出契约` §5.4 | `FromPayload` 是其通用能力的第一个消费者 |
| `状态更新契约` §2 | 回写后的重绘复用既有双轨（`InvalidateCmd`） |
| `For列表输出契约` | §7 是对其作用域限制的增补 |
| `代码生成输出契约` §16 | Input 是动态节点；`Value` 不得进 `bindText` 槽位 |

---

## 9. 验收矩阵（契约条款 → 测试）

| # | 场景 | 期望 | 层级 |
|---|---|---|---|
| 1 | 初始外部值 `"x"` | `SetText("x")`，`A="x"` | 状态机 |
| 2 | 用户输入 `"xy"`（声明 ValueChanged） | `M("xy")` 调用；事件后 `A=E`，布局末尾按需追随模型 | 状态机 |
| 3 | 回写后的下一帧（`V` 已等于 `E`） | **不 SetText**（光标保留） | 状态机 |
| 4 | 外部改值 `V="z"` | `SetText("z")`，`A="z"` | 状态机 |
| 5 | 外部改回 `V="xy"` | `SetText("xy")`，`A="xy"` | 状态机 |
| 6 | 同帧外部改值 + 用户输入 | 用户编辑优先；显示/模型按处理器结果收敛 | 状态机 |
| 7 | MaxLen 截断（`MaxLen=3`，输入第 4 字符） | 无 ChangeEvent；`A` 不变 | 状态机 |
| 8 | Filter 过滤（非法字符） | 无 ChangeEvent；`A` 不变 | 状态机 |
| 9 | 只声明 `Submit`，回车 | `S(text)` 调用；无 ChangeEvent 回写 | 状态机 |
| 10 | 不声明 `Submit`，回车（`SingleLine=true`） | 无 GXML 事件 | 结构+状态机 |
| 11 | `<For>` 内 `<Input>`（有 Key） | Parser 报 SCOPE-005 | Parser 单测 |
| 12 | `<For>` 内 `<Input>`（无 Key） | Parser 报 SCOPE-003（不叠加） | Parser 单测 |
| 13 | 静态 `<Input>`（不在 For 内） | 零 SCOPE-005 | Parser 单测 |
| 14 | 含 Input 的页面 | 不得整页固化 | 结构断言 |
| 15 | 焦点/选区/IME | 输入过程不被打断 | **真实窗口人工验收** |

**说明**：场景 15（IME 组合输入）**无法可靠自动化**（Gio 无合成 IME API），
列为人工验收门槛，不得标记为自动化已覆盖。

---

## 10. 冻结范围与未决项

### 10.1 本次冻结

| 项 | 状态 |
|---|---|
| I-01 属性 | 冻结 |
| I-02 单向绑定 | 冻结 |
| I-03 载荷与分派 | 冻结 |
| I-04 回写时机（并存） | 冻结 |
| I-05 lastApplied 算法（两条路径） | 冻结 |
| I-06 循环与光标防护 | 冻结（由 §5 覆盖） |
| I-07 焦点/选区/IME 归属 + 控件自持 | 冻结 |
| I-09 **仅** For 内拒绝规则 | 冻结 |
| I-10 缓存边界 | 冻结 |
| I-11 Submit 事件 | 冻结 |
| I-12 空值/超长/过滤 | 冻结（依赖 Gio 内置） |
| I-13 单向与回写并存 | 冻结 |

### 10.2 未决（不因本冻结而消解）

| # | 项 | 状态 |
|---|---|---|
| 1 | `<For>` 内 Editor 生命周期（键语义/重排归属/删除清理/重放归属） | **独立门槛，未决** |
| 2 | `Enabled`（`gtx.Disabled` 语义） | v1 报错 |
| 3 | 多行编辑完整形态（布局/换行/滚动） | 未决 |
| 4 | 失焦事件 | v1 无（Gio 无原生） |
| 5 | 数字输入类型 | 用 `Filter` 实现 |

### 10.3 不构成验收

本契约的冻结**不表示** Input 已实现。实现交付前：

- Schema/Analyzer/Codegen 的 Input 支持**尚未存在**；
- 本文 §9 的验收矩阵**尚未执行**；
- 不得在需求清单/发布单中标记 Input 为已完成。
