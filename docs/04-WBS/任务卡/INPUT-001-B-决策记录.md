# INPUT-001-B：契约决策记录（草案 → 待评审）

- 日期：2026-10-20
- 性质：B 阶段裁决记录。每项含【选项】【决定】【理由】【影响】【验收】；
  未决项**明确标注**，不由模型补全。
- 证据基线：A 阶段审计（[TASK-INPUT-001.md](TASK-INPUT-001.md) §6）+
  Gio v0.10.3 源码直读 + 仓库现状。
- 状态：**草案**。经独立审查通过后才升级为冻结。

---

## 第一部分：I-01…I-08（基本输入契约）

### I-01 属性定义

【选项】
A. 沿用 Schema 现状（`Value`/`Enabled`）+ 新增；  
B. 按 Gio 能力全量暴露（Text/Value/Placeholder/ReadOnly/MaxLen/Filter/Mask）；  
C. 最小面：只暴露 v1 必需。

【决定】**C（最小面）**，v1 属性集：

| GXML 属性 | 类型 | 对应 Gio | 语义 |
|---|---|---|---|
| `Value` | 绑定或字面量 | `SetText`（规则见 I-05） | 编辑器的**外部值** |
| `Hint` | 字面量 | `material.Editor` 的 `hint` | 占位提示 |
| `ReadOnly` | 静态 bool | `Editor.ReadOnly` | 只读 |
| `SingleLine` | 静态 `bool`，缺省 `true` | `Editor.SingleLine` | 单行模式（多行是另一控件形态，v1 不做） |
| `Submit` | 靪态 bool | `Editor.Submit` | 启用回车提交（I-11） |
| `Enabled` | 靥态 bool | `gtx.Disabled()` 语义 | 禁用（**沿用 Button 的裁决：MVP 报错**） |
| `MaxLen` | 静态 int | `Editor.MaxLen` | 最大长度（Gio 内置过滤） |
| `Filter` | 字面量 | `Editor.Filter` | 允许字符过滤（Gio 内置） |
| `Text` 属性 | — | — | **不定义**（I-01 反面裁决：与 `Value` 冲突，Gio 无此概念） |

【理由】Gio 内置的属性零成本暴露；`Text` 与 `Value` 同义冲突是 I-01 的
主要风险；`Enabled` 沿用 Button 的既有裁决保持一致性。
【影响】`schema.go` 的 Input 定义需补 `Hint/ReadOnly/SingleLine/Submit/MaxLen/Filter`；
`Enabled` 保持现状（Codegen 报错）。
【验收】B 阶段：Schema 定义进契约文档；D 阶段：未定义属性触发 ATTR 诊断。

### I-02 `{Name}` 是单向显示还是双向绑定

【选项】
A. 自动双向；  
B. 分离：`Value="{x}"` 只读显示，回写只经 `ValueChanged` 方法。

【决定】**B**。`Value="{x}"` 是**外部值绑定**（vm → Editor 方向），
回写只能经 `ValueChanged` 方法调用（Editor → vm 方向）。不扩展普通
`{Path}` 的语义——这是 I-02 反面裁决：**不存在 GXML 层的自动双向绑定**。

【理由】v1 表达式系统没有“赋值回写”的概念；自动双向需要 Analyzer 知道
“哪个字段可写”，牵动绑定系统的可写性模型，超出本卡。
【影响】生成代码里 `Value` 绑定只出现在**同步规则**中（I-05），
不出现在事件调用里；普通绑定语义零改动。
【验收】D 阶段回归：普通 `{Path}` 生成代码与 Input 之前完全一致。

### I-03 事件载荷

【选项】
A. `ValueChanged(value string)` 方法调用；  
B. ViewModel 方法回调（无参 + `Editor.Text()` 自取）。

【决定】**A**。事件名 `ValueChanged`（Schema 已有），载荷 `value string`。
**注意（采纳评审）：这是 Input 的接入决策，不是事件系统改造**——
`FromPayload` 的参数类型、诊断与生成规则是**通用 Codegen 能力**
（独立子任务），Input 只消费它。

【理由】三方证据（Schema/fixture/Gio ChangeEvent+SubmitEvent）；
方法自取值会把“编辑器内部状态”泄漏进 ViewModel 调用约定。
【影响】通用能力任务：Codegen 支持 `FromPayload` 渲染（类型经 Analyzer 校验）。
【验收】D 阶段：通用能力先有独立测试（用任一带载荷事件验证），
Input 接入测试其次。

### I-04 回写时机（不二选一）

【选项】
A. 仅 `Submit` 时回写；  
B. `ChangeEvent` 即时回写；  
C. 两者并存。

【决定】**C（并存，各管各的）**，采纳评审：Change 即时回写（实时校验/搜索），
Submit 是**独立事件**（回车确认），不承担值回写职责。

【决定细则】
- `ValueChanged="M"` → `ChangeEvent` 触发 `v.vm.M(<editor text>)`；
- `Submit="S"` → `SubmitEvent` 触发 `v.vm.S(<editor text>)`；
- 两者可同时声明；都未声明时**不回写**（非受控形态，Editor 自持文本）。
- **限制**（采纳评审）：即时回写 ≠ 每帧 `SetText`——
  编辑器当前文本（Editor 自持）与外部绑定值（vm 侧）是两个状态，
  同步规则见 I-05。

【理由】两类事件语义不同（编辑中 vs 已确认提交）；Gio 原生区分二者。
【影响】事件面 +1（Submit 是否进 Schema 待 C 阶段对齐规范）。
【验收】D 阶段：三种形态（仅 Change/仅 Submit/两者）各有运行时证据。

### I-05 外部值同步规则

【选项】
A. 每帧 SetText；  
B. 仅外部值变化时 SetText；  
C. 不同步（非受控）。

【决定】**B**。「外部值变化」的判定：**上一帧已应用的外部值与本次绑定
取值比较**（而不是与 Editor 当前文本比较——那是用户正在编辑的内容）。

【决定细则】
- View 持有 `lastApplied string`（外部值的应用记录）；
- 渲染时：绑定值 == lastApplied → **不调 SetText**（光标/IME 安全）；
- 绑定值 != lastApplied → `SetText(绑定值)` 并更新 lastApplied；
- `lastApplied` 同时记录**是否来自用户回写**：用户回写后
  lastApplied=新值，若 vm 值相同则后续帧不再 SetText（循环阻断）。

【理由】`SetText` 重置光标是实测事实（A 阶段）；与 vm 值比较会被用户
自己的回写触发重置——所以基准必须是“上次应用的值”。
【影响】View 结构体新增 per-Input 状态；For 内形态见 I-09。
【验收】D 颗段：用户输入 → 回写 → 后续帧不再重置（自动化断言）；
外部改值 → 编辑器同步（自动化断言）。

### I-06 反馈循环与光标

【决定】**由 I-05 的 lastApplied 机制统一覆盖**：
- 循环阻断：用户回写后 lastApplied 同步 → 不再 SetText；
- 光标保护：不满足“外部值变化”就不 SetText。

【理由】两个问题同根（SetText 的破坏性），一个机制解决。
【影响】无额外状态。
【验收】D 阶段：连续 N 帧无外部变化 → SetText 调用次数 = 0（可用
测试注入计数验证）。

### I-07 焦点/选区/IME

【决定】**Editor 自持，框架不接管**。

- 焦点、选区、IME 组合输入全部由 `widget.Editor` 内部状态维护；
- 框架只在 I-05 的同步点调用 `SetText`（会重置光标——因此同步必须稀疏）；
- **IME 边界如实声明**：Gio 对组合输入的处理由 Editor 内部完成；
  框架层不感知组合状态；`SetText` 在组合输入进行中调用会破坏输入——
  因此 I-05 的稀疏同步是 IME 正确性的前提，而非优化。
【理由】Gio 已实现完整编辑状态机，自建会重复且劣质。
【影响】框架不暴露选区/光标 API。
【验收】D 阶段：IME 场景列为手动验收项（自动化无法可靠模拟组合输入），
不宣称自动覆盖。

### I-08 测试策略

【决定】分层：

| 层 | 工具 | 场景 |
|---|---|---|
| 结构 | 生成文本断言 | Editor 字段/事件接线/同步块存在 |
| 状态机 | `input.Router` + 真实 `widget.Editor` | 合成按键事件驱动 ChangeEvent |
| 外部同步 | 直接调用生成代码 | lastApplied 规则（不变不重置） |
| 端到端 | 真实窗口 | 手动验收清单（含 IME 人工项） |
| 通用能力 | 独立于 Input 的 FromPayload 测试 | 载荷类型/诊断 |

【理由】IME 组合输入无法可靠自动化（Gio 无合成 IME API）——如实标注。
【影响】验收清单含手动项。
【验收】C 阶段把清单写进验收用例。

---

## 第二部分：I-09…I-13（生命周期与边界）

### I-09 `<For>` 内的 Input 行状态 —— **未决项，标记为门槛**

【选项】
A. `map[K]*widget.Editor`（照搬 Clickable 行状态池）；  
B. v1 冻结为「静态 Input only」，For 内 Input 明确报错。

【决定】**B 为 v1 形态；A 是候选，列为独立门槛**。

【理由】评审裁决：Editor 不只是字符串容器（光标/选区/IME/撤销历史），
生命周期问题（K 从哪来、键重复检测、重排归属、删除清理、缓存重放时
状态归属）每一个都需要独立设计——**不临时发明不完整的键机制**。
`<For>` 已有稳定 Key 语义（SCOPE-003 强制），这是**前提而非答案**：
Editor 的生命周期问题在 Key 之上仍有独立维度。

【影响】v1：For 内出现 `<Input>` → Codegen 明确报错（不是静默忽略）。
【验收】D 阶段：For 内 Input 报错的测试。
【未决（独立门槛）】：Editor 行状态的完整设计——留待独立裁决，
不塞进本卡。

### I-10 Input 与缓存的边界

【决定】**Input 是动态节点；`Value` 绑定不进 bindText 槽位**。

- Editor 实例由 View 持有（静态单 Input）；
- 缓存只覆盖 Input 之外的静态部分；
- 输入变化 → ChangeEvent 处理器改 vm → InvalidateCmd（复用现有双轨）；
- 缓存命中路径**不绕过** Editor 的事件处理（Editor 轮询在 Layout 顶部，
  与缓存无关）。
【理由】遵循既有原则：缓存只固化合适的静态子树，不得冻结交互状态。
【影响】无新缓存机制。
【验收】D 阶段：含 Input 的页面不得整页固化（回归断言）。

### I-11 Submit 事件

【决定】**纳入 v1**，独立于 ValueChanged：

- Schema 增 `Submit` 事件，载荷 `text:string`（Gio `SubmitEvent.Text`）；
- 声明 `Submit="M"` 需要 `Editor.Submit=true`（生成器自动设置）；
- 未声明 Submit 事件时 `Editor.Submit` 保持 false。
【理由】`SubmitEvent.Text` 自带载荷，与 ValueChanged 同构；
声明即启用避免“值监听器被迫收提交事件”。
【影响】Schema +1 事件；帮助文本与规范同步。
【验收】D 阶段：Submit 事件运行时证据（合成回车键）。

### I-12 空值/超长/过滤

【决定】**v1 依赖 Gio 内置**，不自建：

- 空值：Editor 空文本天然合法；Hint 仅在空时显示（material 行为）；
- 超长：`MaxLen` 属性（Gio 截断）；
- 过滤：`Filter` 属性（Gio 字符过滤）；
- 不做数字键盘/输入类型（Gio 无该语义），需要时用 `Filter="0123456789"`。
【理由】Gio 内置即足够，自建是重复。
【影响】Schema 属性集见 I-01。
【验收】D 阶段：MaxLen/Filter 属性透传的生成断言。

### I-13 单向显示绑定与回写并存规则

【决定】**合法且互补**：

- `Value="{x}"`（vm→Editor）与 `ValueChanged="M"`（Editor→vm）可同时声明；
- **循环安全由 I-05 lastApplied 保证**：用户输入 → 回写 M → vm 更新 →
  下一帧绑定值 == lastApplied → 不 SetText（不打断输入）；
- 若用户**跳过**回写（不声明 ValueChanged）：外部值变化才 SetText——
  这是“只读镜像输入”形态，合法。
【理由】两个方向职责不同，并存是常态而非特例。
【影响】I-05 机制覆盖。
【验收】D 阶段：并存形态的循环安全回归。

---

## 第三部分：能力分层（采纳评审：通用 vs 接入）

| 能力 | 层次 | 归属 |
|---|---|---|
| `FromPayload` 参数渲染（类型/诊断/生成） | **通用 Codegen 能力** | 独立子任务，先于 Input 实测 |
| Input 属性 Schema 扩展 | Input 接入 | Input 任务 |
| Editor 持有与同步规则 | Input 接入 | Input 任务 |
| For 内 Input | **独立门槛** | 未决，不塞进本卡 |

**顺序**：通用能力（FromPayload）先有独立测试 → Input 接入 →
For 内 Input 报错实现 → 未决门槛另立裁决。

---

## 第四部分：未决项汇总（明确标注，不补全）

| # | 项 | 状态 |
|---|---|---|
| 1 | For 内 Editor 生命周期（K 语义/键重复检测/重排归属/删除清理/缓存重放归属） | **未决**，独立门槛 |
| 2 | `Enabled`（gtx.Disabled 语义） | 沿用 Button 裁决：报错 |
| 3 | 多行编辑（SingleLine=false 的完整形态） | v1 属性可设，但多行布局细节未审计 |
| 4 | 失焦事件（Gio 无原生 FocusEvent 给 Editor；仅焦点命令） | v1 无失焦事件 |
| 5 | 数字输入类型 | 用 Filter 实现，无专用属性 |

## 第五部分：下一步

1. 本记录经独立审查 → 冻结为《Input 输出契约》+ ADR；
2. 通用能力子任务先行（FromPayload 独立测试）；
3. Input 接入任务随后；
4. For 内 Input 报错实现 + 未决门槛另立。

---

# INPUT-001-B v2：四个阻断项修订（2026-10-20）

审查报告（[INPUT-001-B-审查报告.md](INPUT-001-B-审查报告.md)）的四个阻断项
逐项修订如下。**未修订的裁决维持 v1 原结论**（I-01/I-02/I-06/I-10/I-12/I-13
语义不变）。

---

## R1-1 修订：FromPayload 最小接口形状

### 1. 载荷来源（不依赖事件名或控件类型）

载荷由 **Schema 声明**决定：`EventSchema.Params` 每一项声明
`{Name, Type}`。Analyzer 已把它解析为 `ResolvedEvent.Payload []ResolvedParam`
（含 Name/Type）与 `Mapping []ParamOrigin`（标注每个方法形参来自载荷还是实参）。

因此**载荷来源是 Schema 声明 + Mapping**，不是"事件名是 ValueChanged 所以取
Editor.Text()"——后者是控件特例，不构成通用能力。

### 2. 事件分派（两个不同 EditorEvent → 两个不同方法）

分派规则的**权威来源是事件声明**，不是运行时类型：

| GXML 声明 | 运行时事件源 | 生成的分派 |
|---|---|---|
| `ValueChanged="M"` | `widget.ChangeEvent` | `M(<载荷表达式>)` |
| `Submit="S"` | `widget.SubmitEvent` | `S(<载荷表达式>)` |

两者**互不回退**：未声明 `Submit` 时不生成 SubmitEvent 处理；声明了两者时
各自独立轮询。

### 3. 参数映射（v1 只支持单个载荷参数）

**v1 冻结**：一个事件最多 **1 个载荷参数**（Schema 已如此，`EventParam` 仅 1 项）。
多载荷参数**不在 v1 范围**——Schema 若声明 2 个以上，Analyzer 报 EVENT-004
（数量不匹配路径已存在）。

载荷与实参的对应顺序：**载荷在前、实参在后**（既有契约 §8.4 R1，Analyzer 已实现）。

### 4. 失败边界（三层各司其职）

| 情形 | 归属 | 码 |
|---|---|---|
| 载荷不存在但方法需要参数 | Analyzer | EVENT-004（数量不匹配） |
| 载荷类别与形参不兼容 | Analyzer | EVENT-006（`payloadCompatible` 按类别比较） |
| 方法有返回值 | Analyzer | EVENT-007 |
| 声明了 Schema 未定义的事件名 | Expander | EVENT-002 |

**Codegen 不为载荷报错**：能走到生成的载荷必然已通过上述校验。

### 5. 通用能力的接口形状（可实现、可独立测试）

```text
输入：ResolvedEvent{Payload, Mapping, Method}
输出：方法调用的实参列表，其中 FromPayload 项渲染为「控件当前值表达式」

控件侧的契约义务（Input 的接入点）：
  为每个已声明事件提供一个 Go 表达式，表示该事件的载荷值：
    ChangeEvent  → <Editor 当前文本>   （如 v.input0.Text()）
    SubmitEvent  → <SubmitEvent.Text>  （轮询返回值）
```

**独立测试不依赖 Input**：用 Analyzer 层构造一个带单个 string 载荷的
`ResolvedEvent`，断言 Codegen 生成 `v.vm.M(<expr>)` 且 `expr` 来自注入的
载荷表达式。这样通用能力可先于 Input 交付并验收。

---

## R2-1 修订：lastApplied 同步算法（定稿）

### 1. 三个值必须区分

| 值 | 归属 | 含义 |
|---|---|---|
| `E` = `Editor.Text()` | Editor | 编辑器**当前**文本（含用户未提交的编辑） |
| `V` = 绑定表达式取值 | ViewModel 侧 | 外部值（`Value="{x}"` 求值结果） |
| `A` = `lastApplied` | View（每 Input 一个） | **最近一次经由 SetText 成功应用到 Editor 的外部文本** |

**`A` 的定义**：`A` 只由「SetText 成功」这一个事件更新，
**不由用户回写更新**。（v1 原记录说"回写后 lastApplied=新值"——**修正**：
那会让 `A` 与 `E` 混淆。见 §2 的推导。）

### 2. 为什么 A 不能由用户回写更新

反例（MaxLen 截断）：
- `MaxLen=5`，`E="abcde"`，vm 值为 `"abcde"`，`A="abcde"`；
- 用户输入第 6 个字符 → Editor 因 MaxLen 截断，`E` 仍是 `"abcde"`、
  **不产生 ChangeEvent**（Gio 内 `replace` 先截断再改）；
- 若此帧 vm 侧因别的原因把值改成 `"abcde"`（相同）→ `V == A` → 不 SetText ✓。
- 但如果 `A` 曾被错误地设为「回写实参」`"abcdeX"`（假设有这么个分支），
  则 `V("abcde") != A("abcdeX")` → 触发 SetText → **打断光标**。

结论：`A` 必须只反映「真正写进 Editor 的文本」，即 SetText 的实参。

### 3. 同步算法（逐帧执行，顺序固定）

```text
每帧渲染到该 Input 时：
  1. 求值 V（绑定表达式）
  2. if V != A {
         Editor.SetText(V)     // 应用外部值
         A = V                 // 仅在 SetText 之后更新——失败则 A 不变
     }
  3. 布局 material.Editor(...).Layout(gtx)   // 内部 Update 消费事件
  4. 消费 EditorEvent（见 R3-1 的位置规则）：
       ChangeEvent  → 若有 ValueChanged 声明：调用 v.vm.M(Editor.Text())
       SubmitEvent  → 若有 Submit 声明：调用 v.vm.S(ev.Text)
```

### 4. 同帧优先级（显式规则）

- **步骤 2 在步骤 4 之前**：外部值先应用，编辑事件后消费。
- 若同一帧内「外部改了 V」且「用户刚输入」：以**外部值覆盖**为准
  （步骤 2 先执行）；用户的输入已在 `E` 中，但会被 SetText 覆盖——
  **这是已知且接受的行为**（v1 不做冲突合并）。
- 契约正文必须写明这条「外部覆盖本地编辑」的后果与回归测试。

### 5. 文本相同但光标/选区不同

**显式规则**：`V == A` 时不调用 SetText ⇒ **光标、选区、IME 组合状态全部保留**。
这不是副作用，是契约保证的**唯一手段**——因为 Gio 没有"只更新文本不动光标"的 API。

### 6. `E` 从不被写回 `A`

用户输入只影响 `E` 与（经回写方法）vm 值；`A` 只由步骤 2 更新。
下一帧若 vm 值已被回写更新为 `E`，则 `V == E`；但 `V != A`（A 还是旧外部值）
→ 会 SetText！**这会打断光标**。

**因此必须补一条**（v1 遗漏的关键）：

```text
5. 用户回写后，必须同步 A：
   ChangeEvent 触发 v.vm.M(text) 之后，若 vm 写入是同步的（v1 全部同步），
   则立即 A = <该帧回写后的绑定值>。
   实现形式：回写后重新求值 V 并令 A = V（仅当回写成功）。
```

这条是 v1 的**真实漏洞**：不补的话，"用户输入 → 回写 → 下一帧 SetText 打断
光标"会稳定复现。R2-1 的核心价值就在这里。

### 7. 验收矩阵（R2-1）

| 场景 | A 初始 | E 初始 | 动作 | 期望 |
|---|---|---|---|---|
| 初始外部值 | "" | "" | V="x" | SetText("x"), A="x" |
| 用户输入回写 | "x" | "x" | 用户输入 "xy" | M("xy") 调用；A 更新为回写后 V |
| 回写后下一帧 | "xy" | "xy" | V="xy" | **不 SetText**（光标保留） |
| 外部改值 | "xy" | "xy" | V="z" | SetText("z"), A="z" |
| 外部改回 | "z" | "z" | V="xy" | SetText("xy"), A="xy" |
| MaxLen 截断 | "abcde" | "abcde" | 用户输入第 6 字符 | 无 ChangeEvent、A 不变 |
| Filter 过滤 | "abc" | "abc" | 用户输入被过滤字符 | 无 ChangeEvent、A 不变 |

---

## R3-1 修订：控件自持事件类别（事件契约补充）

### 1. 两类事件的区分

| 类别 | 消费位置 | 适用 | 理由 |
|---|---|---|---|
| **顶部轮询事件** | `Layout` 顶部，所有绘制之前 | Button 的 `Click` | Clickable 只依赖输入区域命中，无布局上下文依赖；顶部消费保证同帧一致 |
| **控件自持事件** | 控件自身 `Layout` 调用内 | Input 的 `ChangeEvent`/`SubmitEvent` | Editor 的事件消费与**焦点、光标、选区、IME 组合状态**耦合——必须在它自己的布局上下文中执行 |

### 2. Editor 为什么必须自持消费

【外部事实】Gio `widget.Editor.Update(gtx)` 必须在 `Editor.Layout` 之前、
且使用**该控件的 gtx**（`editor.go` 内部 `processEvents/processKey/processPointer`
依赖 `gtx.Source` 与 `gtx.Ops` 中的焦点 tag）。把它提到 Layout 顶部会让：
- 焦点 tag 尚未由本帧 Layout 注册 → 事件无法路由到该 Editor；
- IME 组合状态与布局上下文错位。

### 3. 状态归属

Editor 状态（文本缓冲/光标/选区/IME）由 `widget.Editor` 实例持有，
实例由 **View 结构体字段**持有（静态 Input 一个字段）。框架不接管、
不复制、不在每帧重建。

### 4. 覆盖方式

| 层 | 测试 |
|---|---|
| 结构 | 生成代码断言：Editor 的 `Update/Layout` 在控件位置，不在 Layout 顶部（即顶部无 Editor 轮询） |
| 真实交互 | `input.Router` 合成按键 → ChangeEvent 触发；真实窗口手动验收焦点/IME |

### 5. 事件契约的补充条款（C 阶段写入《事件处理输出契约》）

> **§2 的顶部轮询规则不适用于「控件自持事件」类别。**
> 判断标准：事件消费是否需要控件自身的布局上下文（焦点 tag、
> 输入区域、IME 状态）。需要的一律在控件位置消费。
> 当前该类别成员：`<Input>`。

---

## R4-3 修订：For 内 Input 的正式诊断

### 1. 阶段与归属

**Parser**（与 SCOPE-003 同域）。理由：`containsStatefulNode` 已在 Parser
遍历 For 子树；Analyzer 虽也有作用域栈，但 Parser 更早、且规则本身是
**结构限制**而非类型/语义问题。

### 2. 诊断码：`GXML-SCOPE-005`（新增，需先登记）

【项目证据】诊断码目录 §4：`GXML-SCOPE` 已用 002/003/004，空置 001、**005–010**；
§5 取号规则「取最小的空置号」。→ **SCOPE-005** 是合法的下一个号。

> 002 曾被 SYNTAX-002 借用后归还，但 SCOPE-001 是"已删除不复用"，
> 005 从未使用——可用。

### 3. 触发条件（精确）

```text
Parser 遍历到 <For> 节点时：
  若 containsInputNode(n.For.Children) 为真
    → 报 GXML-SCOPE-005
```

- **不论是否有 Key**：有 Key 当前会放行（实测：零诊断），v1 必须拒绝；
- 检测范围：`<For>` 的直接与间接子树（复用 `containsStatefulNode` 的遍历形态，
  但单独实现 `containsInputNode`——语义不同：SCOPE-003 管"缺 Key"，
  SCOPE-005 管"v1 不支持"）；
- 嵌套 `<For>` 内的 Input 同样命中。

### 4. 诊断消息

```text
<For> contains <Input>, which is not supported in v1; input controls must be static (outside <For>) until per-row editor lifecycle is specified
```

中文附注（文档层）：v1 的 Input 只支持静态布局；行内 Input 需要 Editor
生命周期设计（键语义/光标归属/删除清理），属独立门槛。

### 5. 测试位置与正负例

| 位置 | 用例 |
|---|---|
| `internal/compiler/parser/control_flow_test.go` | 正例：For+Key+Input → SCOPE-005；For 无 Key+Input → SCOPE-003（既有规则优先，不叠加两条） |
| 同上 | 负例：静态 Input（不在 For 内）→ 零诊断；For 内只有 Text/Button → 零 SCOPE-005 |
| `internal/compiler/diagdoc/` | 新码登记一致性 |
| `internal/cli/` | 端到端：含 For+Input 的 .gxml 经 CLI 生成 → 退出码 2 + 该码 |

### 6. 与 Codegen 的关系

Codegen 的 default 分支（"`<Input>` is not supported"）**保留为防御层**，
但正常管线不可达——Parser 已拦。这与 `<For>` 缺 Key 的
"Parser 拦 + Codegen 兜底"形态一致（B 阶段既有实践）。

---

## v2 修订汇总

| 阻断项 | 状态 |
|---|---|
| R1-1 | ✅ 已补：载荷来源=Schema+Mapping、分派=声明驱动、v1 单载荷参数、四类失败边界、可独立测试的能力形状 |
| R2-1 | ✅ 已补：三值定义、A 只由 SetText 更新、**回写后必须同步 A（v1 真实漏洞）**、同帧优先级、光标显式规则、7 场景验收矩阵 |
| R3-1 | ✅ 已补：两类事件区分、Editor 自持理由（焦点 tag/IME）、状态归属、覆盖方式、事件契约补充条款 |
| R4-3 | ✅ 已补：Parser 归属、SCOPE-005（经 §4/§5 核对为空置可取）、精确触发条件、消息、正负例位置 |

**冻结建议**：四项阻断已关闭，可进入冻结流程（《Input 输出契约》+ ADR），
随后 C 阶段接口同步。

**仍然未决**（不因本轮修订而消解）：`<For>` 内 Editor 生命周期
（本轮只冻结"v1 编译期拒绝"，生命周期设计仍为独立门槛）。