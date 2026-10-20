# TASK-INPUT-001：Input 控件（契约阶段）

## 元信息

- 状态：**B 阶段决策记录完成（草案，待独立评审）——见 [INPUT-001-B-决策记录.md](INPUT-001-B-决策记录.md)；A 阶段审计已完成（§6）** | 优先级：P1
- 依赖：`TASK-STATE-001`（失效机制）、`TASK-BINDING-IMPLEMENT-001`（绑定）、
  `TASK-GENERATE-CLI-001`（CLI，已关闭）
- 规范依据：[`GXML语言规范.md`](../../03-架构/GXML语言规范.md) §6.1、
  [`事件处理输出契约.md`](../../03-架构/事件处理输出契约.md)、
  [`状态更新契约.md`](../../03-架构/状态更新契约.md)
- 关联需求：REQ-002（组件）、REQ-001（语言）

## 1. 目标

把 `<Input>` 从「Schema 里有定义但 Codegen 明确拒绝」推进到「有完整契约、
可测试、可实现的受控输入控件」。**契约阶段（A/B/C）不写实现代码。**

## 2. 为什么先契约后实现

Input 同时触及语言语义（I-02 双向绑定边界）、事件系统（I-03 载荷）、
状态更新（I-05 外部同步）、显示缓存（I-06 反馈循环）与 IME（I-07）。
先实现后补契约会反复改 Codegen 与状态更新机制——A→B→C→D 的阶段划分
就是把这种返工压到最低。

## 3. 必须裁决的问题清单

### 3.1 用户提出的 8 问（I-01…I-08）

| 编号 | 问题 | 裁决阶段 |
|---|---|---|
| I-01 | `Text`/`Value`/`Placeholder`/`ReadOnly`/`MaxLen` 属性定义 | B |
| I-02 | `{Name}` 对 Input 是单向显示还是双向绑定 | B |
| I-03 | 输入事件载荷：`ValueChanged(value string)` 还是方法回调 | B |
| I-04 | ViewModel 何时接收新值（每次按键/提交/失焦） | B（须先有 A 证据） |
| I-05 | 程序改值后 Editor 如何同步 | B（同上） |
| I-06 | 反馈循环与光标跳动防护 | B |
| I-07 | 焦点/选区/IME 归属 | B |
| I-08 | 自动化测试策略（输入/外部更新/清空/失焦/重聚焦） | C |

### 3.2 审计中新增的裁决点（I-09…I-13）

| 编号 | 问题 | 理由 |
|---|---|---|
| I-09 | `<Input>` 在 `<For>` 内的行状态——Editor 是重量级状态（文本/焦点/选区/IME），与轻量 Clickable 完全不同量级 | 不能照搬行状态池设计 |
| I-10 | Editor 值与显示缓存的关系（I-06 的机制层） | 影响文本布局路径 |
| I-11 | `Submit`/回车提交事件是否进 v1 | 影响事件面 |
| I-12 | 空值、超长、过滤输入（数字等）语义 | 常见需求，需明确范围 |
| I-13 | 单向显示绑定与双向 `Value` 的并存规则 | 防止语义冲突 |

## 4. 阶段

| 阶段 | 内容 | 产出 |
|---|---|---|
| A | 现状审计（Editor API、事件签名规则、写入路径、刷新链、缓存边界） | 审计记录（§6） |
| B | 契约决策 + ADR | 《Input 输出契约》+ ADR |
| C | 接口与验收冻结（规范/事件契约/状态契约/需求矩阵同步） | 文档更新 |
| D | 实现任务拆分 | 子任务卡 |

## 5. 排除

- 契约阶段不写实现代码；
- 不扩展表达式系统（I-02 若引入双向，只限 Input 的 `Value` 属性，
  不动普通 `{Path}` 语义）；
- 不承诺 IME 全场景（组合输入行为依 Gio 能力如实标注）。

## 6. 审计记录（A 阶段产出，2026-10-20）

### 6.1 widget.Editor API 证据（I-01/I-05/I-06/I-07/I-12 的素材）

| API | 签名/字段 | 对契约的含义 |
|---|---|---|
| `SetText(s string)` | 内部 `replace(0, len, s, true)` + **`SetCaret(0,0)`** | 每帧无条件 SetText 会把光标打回开头——I-06 反馈循环防护的直接依据 |
| `Text() string` / `Len() int` | 读当前内容 | 回写 ViewModel 的取值通道 |
| `ChangeEvent{}` | **每次用户改动文本**产生（`text.Changed()`） | I-04「每次按键」的天然钩子 |
| `SubmitEvent{Text string}` | 仅 `Submit=true` 时回车产生，**自带 Text** | I-11 的现成载荷 |
| `SelectEvent{}` | 选区变化 | v1 不需要 |
| `SingleLine`/`ReadOnly`/`Submit`/`Mask rune`/`MaxLen int`/`Filter string` | 导出字段 | I-01/I-12 能力清单：MaxLen 与 Filter **Gio 已内置** |
| `SetCaret(start,end)` / `Selection()` | 光标与选区控制 | I-07 归属 Editor 自身 |
| `material.Editor(th, editor, hint)` | hint 即占位提示 | I-01 的 Placeholder 对应 `Hint` |

【外部事实】以上均为 `widget/editor.go`（Gio v0.10.3）源码直读。

### 6.2 事件签名规则证据（I-03）

- Schema **已有** `Input.ValueChanged` 事件，载荷 `value:string`
  （`expander/schema.go`，注释明确「回写由事件完成；v1 不引入双向绑定」）；
- Analyzer 的 `ResolvedEvent` 已支持 `FromPayload`（`Mapping` 语义），
  **但 Codegen 尚无 FromPayload 的生成路径**（全库 grep 无一处）——
  这是 Input 载荷的**真实实现缺口**，B 阶段必须列为实现任务；
- `SubmitEvent.Text` 与 `ValueChanged(value string)` 的载荷形态天然匹配。

### 6.3 ViewModel 写入路径证据（I-03/I-04）

- 现有生成形态：`v.vm.Method(args)`——写入必须经**方法**，无直接字段写；
- 因此回写签名是 `ValueChanged(value string)` → `v.vm.OnNameChanged(value)`
  这类方法调用（Schema 载荷名 `value` 与 fixture 的 `OnNameChanged(value string)` 一致）。

### 6.4 状态刷新链证据（I-05）

- 状态更新契约 §2：通知入口只请求重绘，不写 ViewModel；
- 帧内失效用 `gtx.Execute(op.InvalidateCmd{})`（事件契约 §3 已冻结）；
- 输入回写后的刷新可完全复用现有双轨：处理器在帧内改 vm →
  InvalidateCmd 请求下一帧。

### 6.5 缓存边界证据（I-06/I-10）

- `bindText` 槽位缓存按「显示串+上下文」命中；Editor 的内部缓冲是
  **Gio 侧状态**，不是 GXML 生成缓存的一部分；
- 结论（B 阶段细化）：Input 的 `Value` 绑定**不得**进 bindText 槽位
  （编辑器自己维护文本），缓存只覆盖 Value 之外的静态部分。

### 6.6 A 阶段结论（供 B 阶段裁决）

1. `ValueChanged(value string)` 有 Schema/fixture/Gio 三方证据，
   是 I-03 的最强候选；
2. **Codegen 的 FromPayload 生成是硬缺口**，Input 是第一个需要它的控件；
3. I-04 的两个候选（每次按键 ChangeEvent / 失焦+Submit）在 Gio 都有
   原生支持，**无需自己发明节流**——决策可基于体验与性能取舍；
4. I-06 的防护义务来自 `SetText` 会重置光标这一实测事实——
   「仅在外部值变化时 SetText」是必须冻结的规则；
5. `MaxLen`/`Filter`/`Mask` Gio 已内置，I-12 的范围可以很小。
