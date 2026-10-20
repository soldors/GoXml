# ADR-0012：主题/字体边界（路线 B）与 `<Spacer>` 主轴默认值

- 状态：**已接受（2026-10-20）**
- 决策依据：[字体与文本渲染策略.md](../字体与文本渲染策略.md)（v1 冻结）、
  [代码生成输出契约.md](../代码生成输出契约.md)（v1 冻结 §4.3.1 / §4.3.2）、
  [GXML语言规范.md](../GXML语言规范.md)（v1 冻结 §3.1）、
  [STYLE-FONT依赖审计.md](../../05-开发/STYLE-FONT依赖审计.md)
- 关联：ADR-0008（布局属性模型）、ADR-0009（静态子树固化）、ADR-0010（统一 View 接口）
- 决策人：项目所有者（2026-10-20 裁决）；执行：维护会话

---

## 背景

两处「冻结文档互相矛盾 / 规范缺一句话」的问题，都已经在代码里有了事实上的答案，
但文档没跟上，长期靠实现单方面维持：

1. **字体边界**：字体规范 §6.2 要求生成代码「只通过主题引用字体 `th.Fonts.Family`」，
   而输出契约 §4.3.1/§4.3.2 把 `Layout` 的主题参数固定为 **Gio 的 `*material.Theme`**
   ——该类型没有 `Fonts` 字段。§4.3.1 当年只登记了冲突（"th 不在作用域内，无法成立"），
   没有裁决。
2. **`<Spacer>` 缺省尺寸**：§3.1 只写「占用剩余空间的弹性空白」，没写「未给尺寸时主轴
   取什么」。实现（`internal/compiler/codegen/layout.go` 的 `sizePlanFor`）按
   **主轴 `Flexed(1)`** 处理，并有真实布局证据，但规范里没有对应条款。

---

## 决策 1：字体边界取**路线 B——运行时解析**

### 决策

- 保留生成代码签名 `Layout(gtx, th *material.Theme)`（输出契约 §4.3.1）。
- 字体探测、fallback 链、懒加载与 `text.Shaper` 配置全部由 **运行时**
  （`pkg/runtime/theme`）在**主题构建过程**中完成；结果体现为一个已配好字体的
  `*material.Theme`，由 `pkg/runtime/app` 交给视图。
- 生成代码**不引用任何字体字段**（既不写 `th.Fonts.Family`，也不出现字体路径、
  字号常量或 `text.Shaper` 构建）。
- 跨层通道只有一条：`contracts.FontProvider{Resolve(family string) (string, error)}`。

### 选项对比

| | 路线 A（规范字面） | **路线 B（本决策）** |
|---|---|---|
| 生成代码 | 写 `th.Fonts.Family` | 不引用字体 |
| 契约 §4.3.1/§4.3.2 | 必须修订（主题参数换成项目 `Theme`） | 不动 |
| Codegen / 已生成代码 / 相关测试 | 全部迁移签名 | 一行不改 |
| 字体平台细节的位置 | 泄漏到每一页生成代码 | 留在运行时 |
| 代价 | 一次全量契约改动 | 修订字体规范 §6.2（本次已改） |

### 理由（不是"少改几行代码"）

1. **当前生成代码并不依赖任何自定义 Theme 字段**——`grep 'Theme\.'` 在 codegen
   生产码中零命中。字体探测不应迫使所有页面的签名迁移。
2. §4.3.2 的既定理由是「项目主题会演进，不应反向改变已生成代码的签名」。
   路线 A 恰好违反它：字体需求驱动一次全局签名变更。
3. §6.2 原本的**禁令**在 B 下同样成立且更强：生成代码连 `Fonts.Family` 都不出现，
   §13 第 19 条（不得含字体路径/字号常量/`text.Shaper` 构建）自动满足。
4. 输出契约 §4.3.2 第 2 条已经写明：「`material.Theme` 携带字体集合，满足 §13 第 19 条」
   ——B 与既有论证一致，A 与它冲突。

### 一致性门禁

文档会漂移，所以裁决不只写在 ADR 里：

- `internal/docgate/theme_seam_test.go` 的
  `TestThemeRouteBDocumentationConsistent`：
  - 字体规范**不得**再出现 `th.Fonts.Family` / `th.Fonts.`；
  - 字体规范**必须**说明字体由 `pkg/runtime/theme` 在运行时完成，并登记 `FontProvider`；
  - 输出契约**必须**仍是 `th *material.Theme`。
- `TestGeneratedLayoutSignatureMatchesRouteB`：Codegen 实际生成的签名必须是
  `Layout(gtx layout.Context, th *material.Theme)`，且不得出现 `th *theme.Theme`。

任何一侧回退到路线 A，这两条测试会先红。

---

## 决策 2：`<Spacer>` 无尺寸时主轴默认弹性

### 决策

未显式指定主轴尺寸时，`<Spacer>` 在父容器主轴上作为一个弹性布局项，**权重为 1**；
显式 `Width` / `Height` **优先**；交叉轴行为遵循通用尺寸规则（§3.1.4 / §3.1.7）。

已写入规范 §3.1.1.1（并说明它与 `<Expanded>` 的关系，§3.1.1.2）。

### 证据

- **项目实测**（headless 离屏，400×1200 帧缓冲，PxPerDp=1）：
  `<Row><Text Value="A"/><Spacer/><Text Value="B"/></Row>` 在 300 宽约束下
  `A=[0,10]`、`B=[290,299]`——空白吃掉 279px。
  去掉该缺省后同一用例 `B` 落在 `x=12`（退化成 0 宽空块），测试立即失败。
- **对照测量**：`<Spacer Width="0"/>` 与 `<Spacer Width="8"/>` 两行的文本间隙差
  **恰好 8px**（差分抵消字形留白）。
- **外部事实**：Gio `layout.Flex` 对 `Flexed` 子项传入 `Min=Max=flexSize` 的约束
  （gioui.org@v0.10.3/layout/flex.go:153），`layout.Spacer` 用
  `Constraints.Constrain` 落成实际占位（layout.go:242）——因此「弹性空白」可以由
  这两个原语直接实现，不需要框架自己算尺寸。

### 为什么不是"自然尺寸"缺省

若照搬其它节点的「内容自然尺寸」缺省，`<Spacer/>` 会变成 0 尺寸的空块：
名字说「占用剩余空间」，行为却什么都不做。这与 §3.1 的原定义直接冲突。

---

## 契约与包边界（THEME-SEAM 的范围）

| 层 | 职责 |
|---|---|
| `contracts/runtime_api.go` | `FontProvider`（本次冻结，未接线）、`ThemeProvider`（保留，标注未接线与替换规则）、`ErrFontNotFound` |
| `pkg/runtime/theme` | 字体解析、fallback 链、主题构建（`NewTheme()`）。**尚不存在** |
| `pkg/runtime/app` | 启动、窗口生命周期；不再硬编码 `material.NewTheme()`，改为调用 `pkg/runtime/theme` |
| 生成代码 | 只接受 `*material.Theme`，不引用字体 |

### `FontProvider` 的冻结内容

- 入参 `family` 是**逻辑字体族名**（如 `"Body"` / `"CJK"`），不是文件路径；
- 返回 `pkg/runtime/theme` 在探测阶段注册的**字体集合标识**；
- 无法解析时返回 `ErrFontNotFound`（`errors.Is` 可判定），**不返回空串 + nil**；
- 调用时机：主题构建期或首次 shaping 时（懒加载）；**不得每帧调用**，实现自带超时，
  不得阻塞首帧。

### 现有 `ThemeProvider` 的处置：**保留**（不是删除、也不是替换）

理由：语言层已能表达主题色（`$Theme.Field` → `NRGBAColor{IsThemeRef, ThemeField}`，
由 Parser/Expander 产出），它需要一个契约层落点；删掉会让 IR 字段没有出口。
它与 `FontProvider` 分管**颜色**与**字体**，不是同一抽象的两种实现，因此不算
"并存两个没有调用方的抽象"。替换规则写进 `contracts/runtime_api.go` 注释：
若 STYLE-001-1 决定改用强类型/编译期字段而非 string key，应**替换**本接口，
不得再新增第二种颜色获取方式。

---

## 影响与后续

| 项 | 状态 |
|---|---|
| 生成代码签名 | 不变 |
| 契约 §4.3.1 / §4.3.2 | 不变 |
| 字体规范 §6.2 / §6.3 / §8 / §9 | **已修订**（含 §9.1 变更记录） |
| GXML 规范 §3.1.1.1 / §3.1.1.2 | **已新增** |
| `contracts/runtime_api.go` | 新增 `FontProvider`、`ErrFontNotFound`；`ThemeProvider` 补状态说明 |
| 字体功能实现 | **本 ADR 不做**——仅冻结接缝 |

后续任务顺序：`TASK-THEME-SEAM-001`（本 ADR 的落地与验收方案）→
`TASK-FONT-001-A`（平台探测 + Provider 单元测试）→ B 嵌入子集 → C fallback 链 →
D 懒加载；IME 回归 `TASK-WIDGET-001` / `INPUT-001`。

## 已知风险

1. `Resolve` 返回的「字体集合标识」是 `string`。若 `TASK-FONT-001-A` 的实测显示
   它需要携带更多信息（例如按平台的字体文件路径或权重），改签名属于契约变更，
   必须走 ADR + `make contract-lint`，不得就地放宽。
2. 路线 B 把字体决策推迟到运行时，因此**同一份程序在不同环境下可能选到不同字体**。
   这不能靠"跑一次就算通过"验收，必须有跨环境 smoke test（§8 已列）。
3. `pkg/runtime/theme` 目前不存在，本 ADR 冻结的是边界而不是实现；
   在 `TASK-THEME-SEAM-001` 完成前，字体相关验收命令（指向该包）仍是空转。
