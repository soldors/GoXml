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
