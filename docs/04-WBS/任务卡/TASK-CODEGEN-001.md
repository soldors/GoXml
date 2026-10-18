# TASK-CODEGEN-001：实现 Gio 布局与控件代码生成器

## 元信息
- 状态：Todo | 优先级：P0 | 预计：6h
- 依赖：TASK-ANALYZER-001（已完成并整体验收）
- 输出契约：`docs/03-架构/代码生成输出契约.md`（§2.1–§2.3 入口与门禁，§13 验收项）

## 1. 目标

把 Expander 输出的 `DocumentIR` 翻译为**可直接编译的 Go 源码**，调用 Gio 的
`layout`/`op`/`widget` 完成渲染。生成的代码不得包含反射、不得在 `Layout` 内分配。

## 1.1 布局标签映射

| GXML | Gio |
|---|---|
| `<Column>` | `layout.Flex{Axis: Vertical}` |
| `<Row>` | `layout.Flex{Axis: Horizontal}` |
| `<Grid>` | 外层纵向 `Flex` + 每行横向 `Flex`（见 §1.3） |
| `<Spacer>` | `layout.Dimensions` 占位，或 `Flexed` 空布局 |
| `<Expand>`（如启用） | 单个子节点铺满 |

布局标签的通用属性（`Padding`/`Background`/尺寸/对齐）映射见 §1.3。

## 1.2 基础控件映射

| GXML | Gio |
|---|---|
| `<Text>` | `material.Label` / `widget.Label` |
| `<Button>` | `material.Button` |
| `<Input>` | `material.Editor` |
| `<Image>` | `widget.Image` |

控件状态按 Shadow Struct 约定生成，见 `AOT状态Diff语义.md`。

## 1.3 布局契约（ADR-0008 + GXML 规范 §3.1）

实现必须遵守 `docs/03-架构/adr/ADR-0008-布局属性模型.md` 与 `GXML语言规范.md` §3.1：

- **`Align`**：`Start/Center/End/Baseline` → `layout.Start/Middle/End/Baseline`。**注意 `Center` 映射到 `Middle`**。
- **`Baseline` 适用面**：只允许 `<Row Align="Baseline">`。`<Column>` 与 `<Grid>` 上用 `Baseline` 是编译错误 `GXML-ATTR-003`，Codegen 不应收到这种输入。
- **v1 无 `AlignSelf`**：规范 §3.1.2 已移除子项级对齐，Codegen 不生成任何子项覆盖逻辑。
- **轴向感知默认值**：`<Row>` 未写 `Align` 时生成 `layout.Middle`；`<Column>` 生成 `layout.Start`。这是 Codegen 的责任，IR 层保持 `AlignUnset`。
- **`Justify`**：按 ADR-0008 §2.4 映射表转换，**方向相反**（`Start→SpaceEnd`、`End→SpaceStart`），不可直接同名赋值。
- **`Gap`**：使用原生 `layout.Flex{Gap: n}`，**禁止**逐项生成 `layout.Inset`。
- **`Width`/`Height` 的 `Fill`**：**不能一律翻译为 `Flexed(1)`**。`Fill` 是语言层尺寸语义，必须按父容器主轴方向区分（规范 §3.1.5）：

  | 父容器 | 方向 | 角色 | 实现 |
  |---|---|---|---|
  | `Row` | `Width` | 主轴 | 主轴弹性分配 |
  | `Row` | `Height` | 交叉轴 | 填充交叉轴可用高度 |
  | `Column` | `Height` | 主轴 | 主轴弹性分配 |
  | `Column` | `Width` | 交叉轴 | 填充交叉轴可用宽度 |

  主轴的 `Fill` 参与与其他 `Fill` 兄弟的按比例均分；交叉轴的 `Fill` 直接采用父容器在该方向的可用约束。
- **固定尺寸**：`<dp>` → `Constraints.Exact`；**超出父容器可用空间时不裁剪、不报错**，允许溢出（规范 §3.1.5）。
- **无界约束下的 `Fill`**：退化为内容自然尺寸，不报错。
- **`Baseline` 禁止零值兜底**：**不得**生成 `if d.Baseline == 0 { d.Baseline = d.Size.Y }`。规范 §3.1.3 已明确 `Baseline == 0` 是合法值（基线在底边），隐式兜底会把「未提供」误当「底部基线」，是更隐蔽的错误。基线正确性由控件实现方负责。

## 1.4 Grid 生成契约（规范 §3.1.6 / ADR-0008 §2.7）

`<Grid>` 必须生成「外层纵向 `Flex` + 每行一个横向 `Flex`」：

```text
Column{ Gap: rowGap }
└── Row{ Gap: colGap, Alignment: align }
    ├── Rigid(Exact(固定列宽)) 或 Flexed(权重)
```

硬性要求：

- **补位**：最后一行按列定义补齐剩余列（固定列补等宽 `Rigid` 占位，弹性列补同权重 `Flexed` 占位）。补位是 Codegen 内部的布局项，**不得写回 IR**、不得创建控件、不得参与状态 Diff。
- **补位必须计入 `Gap` 计数**：补位行的子项数量必须等于列数，否则 `Flex` 预留的间距数少一个，弹性列会变宽。
- **子项 `Width` 不进入 Codegen**：Parser 已报 `GXML-ATTR-003`；若在 Codegen 收到带 `Width` 的 Grid 子项，属于上游缺陷，应断言失败。
- **不自行计算像素宽度**：列宽全部交给 `Rigid`/`Flexed`，由 Gio 分配。取整规则见规范 §3.1.6；其中「可能留下未分配像素」是**非保证项**，**不得补偿**（补偿会让生成代码与规范描述不一致）。

## 1.5 尺寸解析（规范 §3.1.7 / ADR-0008 §2.8）

`Width`/`Height` 的决策**必须调用 `internal/compiler/sizelayout`**，不得在 Codegen 内重新实现判断：

```go
main, cross := sizelayout.ResolveMainCross(width, height, mainIsHorizontal, availW, availH)
```

| 指令 | 生成 |
|---|---|
| `DirectiveNatural` | 不施加额外约束 |
| `DirectiveFlex` | `layout.Flexed(1, ...)` |
| `DirectiveFillCross` | 把交叉轴 `Min` 抬到 `Max` 后布局 |
| `DirectiveExact` | `layout.Constraints.Exact`（允许溢出父容器） |

- `<Page>` 按**垂直 Flex** 生成（等价于没有 `Gap`/`Padding` 的 `<Column>`，ADR-0008 §2.8.1）。
- 交叉轴无界时 `Fill` 已由 `sizelayout` 退化为 `Natural`，Codegen **不得**再自行判断无界。
- `<Grid>` 单元格内的 `Height="Fill"` 同样退化为自然高度（规范 §3.1.7）。

## 1.6 性能约束

依据 `代码生成输出契约.md` §15 与 `性能预算与优化策略.md`：

- 静态子树必须整体固化为 `op.CallOp` 并缓存，不得每帧重建闭包。
- 稳定帧 `allocs/op == 0`（`INV-PERF-2`）。
- 禁止在 `Layout` 内做字符串拼接或 `fmt.Sprintf`（文本需预计算或使用 `text.Shaper` 缓存）。
- 列表非虚拟化时不得逐项新建闭包；虚拟化路径见 ADR-0006。

## 1.7 入口与门禁（契约 §2.1–§2.3）

Codegen 的公开入口**只接受已验证的语义 IR**：

```go
func Generate(v *analyzer.VerifiedDoc, opts Options) ([]byte, error)
```

不接收 `*contracts.DocumentIR`、`*analyzer.Result` 或任何未经验证的结构。

门禁由两层共同承担，缺一不可：

| 层 | 职责 | 本轮能否验证 |
|---|---|---|
| 类型入口 | `VerifiedDoc` 只能由 `StatusOK` 且无诊断的 `Result` 构造，编译期挡住未验证 IR | ✅ 可验证 |
| 编排层 | 失败时**不调用 Codegen、不写任何文件** | ⚠️ 需先有编排层 |

**不能只凭 `VerifiedDoc` 这个类型就宣称编排层门禁已验证。** 两者是不同的事实：
前者是「传不进去」，后者是「失败时什么都没产生」。

### 失败语义

| 情况 | 行为 |
|---|---|
| 未取得 `VerifiedDoc` | 不调用 Codegen，不写文件 |
| Codegen 内部出错 | 返回 error，**不返回部分代码** |
| 格式化或包一致性校验失败 | 丢弃本次结果，旧文件不变 |

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-CODEGEN-001-1 | 将 `<Column>`/`<Row>`/`<Grid>`/`<Spacer>` 映射为 Gio Flex 代码 | go test -v -run TestGenerateLayout ./internal/compiler/codegen |
| TASK-CODEGEN-001-2 | 生成基础控件与 Shadow Struct 状态定义 | go test -v -run TestGenerateWidgets ./internal/compiler/codegen |
| TASK-CODEGEN-001-3 | 跑通 Golden 编译测试集 | go test -v -run TestGoldenCodegen ./internal/compiler/codegen |
| TASK-CODEGEN-001-4 | 入口只接受 `*analyzer.VerifiedDoc`，并拒绝未经校验的 IR | go test -v -run TestGenerateRequiresVerified ./internal/compiler/codegen |
| TASK-CODEGEN-001-5 | 生成结果能被 Go 工具链编译（临时模块 `go build`） | go test -v -run TestGeneratedCompiles ./internal/compiler/codegen |
| TASK-CODEGEN-001-6 | 编排层门禁：`Partial`/`Blocked` 不调用 Codegen、不产生任何文件 | go test -v -run TestPipelineGate ./internal/compiler |

## 3. 验收要求（必须全部满足）

| # | 要求 | 验证方式 |
|---|---|---|
| 1 | `StatusOK` 且验证通过，才允许调用 Codegen | 类型入口 + 测试 |
| 2 | `StatusPartial`、`StatusBlocked` 均不得调用 Codegen | 子任务 4 |
| 3 | 失败流程不生成文件、不留部分产物 | 子任务 6 |
| 4 | 测试验证**实际编排行为**，而非只测 `Verified()` 的拒绝逻辑 | 子任务 6 必须经编排层，不能直接调 `Verified()` |
| 5 | 临时 Go 模块 `go build` 通过，证明最小切片生成了可编译代码 | 子任务 5 |
| 6 | 满足 `代码生成输出契约.md` §13 的全部 20 条 | Golden + E2E |

> 第 6 条中的第 20 条（编排层门禁）在编排层落地前无法执行。
> 在此之前，任务状态只能是「设计已定义、实现待完成」，**不得标记为通过**。

## 4. 建议的首个纵向切片

不要一次实现全部控件。先做能端到端跑通的最小单元：

1. 只生成一个静态页面（`<Page>` + 一个 `<Text>`），不含绑定、事件、`<For>`
2. 验证包名、import、保护头、`gofmt` 通过、`go build` 通过
3. 再接布局 → 属性绑定 → 事件 → `<For>`

理由：IR 或编排接口若不适用，越早发现越好；等全部控件写完再验证，
返工代价高得多。
| TASK-CODEGEN-001-4 | 静态子树识别与 `CallOp` 固化（生成源码含缓存字段） | go test -v -run TestStaticSubtreeMacro ./internal/compiler/codegen |
| TASK-CODEGEN-001-5 | `<For Virtualize>` 语法解析与虚拟化代码生成 | go test -v -run TestGenerateVirtualizedFor ./internal/compiler/codegen |
| TASK-CODEGEN-001-6 | 稳定帧零分配断言（静态页面 allocs/op == 0） | go test -bench=BenchStaticStable -benchmem ./internal/compiler/codegen |
| TASK-CODEGEN-001-7 | 生成代码禁止在 Layout 内 `fmt.Sprintf`（AST 断言） | go test -v -run TestNoSprintfInLayout ./internal/compiler/codegen |
| TASK-CODEGEN-001-8 | `Align` 映射含轴向感知默认值 | go test -v -run TestGenerateAlign ./internal/compiler/codegen |
| TASK-CODEGEN-001-9 | `Justify` → `Flex.Spacing` 方向相反的映射表 | go test -v -run TestJustifyMapping ./internal/compiler/codegen |
| TASK-CODEGEN-001-10 | `Width`/`Height` 三取值映射，**`Fill` 按主轴/交叉轴分别处理** | go test -v -run TestGenerateSize ./internal/compiler/codegen |
| TASK-CODEGEN-001-11 | `Gap` 使用原生 `Flex.Gap`（AST 断言无逐项 `Inset`） | go test -v -run TestGapUsesNative ./internal/compiler/codegen |
| TASK-CODEGEN-001-12 | `Baseline` **不生成零值兜底**（AST 断言无 `d.Baseline = ...` 赋值） | go test -v -run TestBaselineNoFallback ./internal/compiler/codegen |

### Grid 专项（规范 §3.1.6 / ADR-0008 §2.7）

| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-CODEGEN-001-13 | 生成外层纵向 Flex + 每行横向 Flex；列宽映射为 `Rigid(Exact)` / `Flexed(权重)` | go test -v -run TestGenerateGrid ./internal/compiler/codegen |
| TASK-CODEGEN-001-14 | 最后一行补位（固定列 `Rigid` 占位、弹性列 `Flexed` 占位），且**不写回 IR** | go test -v -run TestGridLastRowPadding ./internal/compiler/codegen |
| TASK-CODEGEN-001-15 | 补位计入 `Gap` 计数（断言补位行与完整行的 `Flex.Gap` 相同） | go test -v -run TestGridPaddingGapCount ./internal/compiler/codegen |
| TASK-CODEGEN-001-16 | **真实布局约束**：用 gio 渲染 3 行 × 3 列（末行不满），断言**每一列在所有行中像素宽度完全一致** | go test -v -run TestGridColumnAlignment ./internal/compiler/codegen |
| TASK-CODEGEN-001-17 | 权重比例断言：`Columns="2* * 3*"` 在 600px 下实测 `200/100/300` | go test -v -run TestGridWeights ./internal/compiler/codegen |
| TASK-CODEGEN-001-18 | 非保证项：116px 五等分实测合计 115，断言**不自行补偿** | go test -v -run TestGridNoPixelCompensation ./internal/compiler/codegen |
| TASK-CODEGEN-001-19 | 约束：`Gap` 同时作用于行列两个轴向 | go test -v -run TestGridGapBothAxes ./internal/compiler/codegen |
| TASK-CODEGEN-001-20 | Grid 性能回归：多行 Grid 的静态子树仍可 `CallOp` 固化，稳定帧 `allocs/op == 0` | go test -bench . -run xxx ./benchmarks/... |

### 尺寸解析专项（规范 §3.1.7 / ADR-0008 §2.8）

| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-CODEGEN-001-21 | `<Page>` 按垂直 Flex 生成（等价无 `Gap`/`Padding` 的 `<Column>`） | go test -v -run TestPageVerticalStack ./internal/compiler/codegen |
| TASK-CODEGEN-001-22 | 尺寸决策统一走 `sizelayout.ResolveMainCross`（AST 断言无自行判断轴向的代码） | go test -v -run TestSizeUsesResolver ./internal/compiler/codegen |
| TASK-CODEGEN-001-23 | 交叉轴 `Fill` 生成「抬 `Min` 到 `Max`」，且无界时不生成（由 resolver 已退化） | go test -v -run TestCrossFillGeneration ./internal/compiler/codegen |
| TASK-CODEGEN-001-24 | 嵌套约束：`<Column><Row Height="Fill"><Text Width="Fill"/>` 的两级 `Fill` 各走主轴 | go test -v -run TestNestedFill ./internal/compiler/codegen |
| TASK-CODEGEN-001-25 | 固定尺寸溢出：父 `Max=150`、子 `Width="200"` 生成 `Exact(200)`，不裁剪 | go test -v -run TestFixedOverflow ./internal/compiler/codegen |
