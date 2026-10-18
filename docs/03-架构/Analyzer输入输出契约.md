# Analyzer 输入输出契约

- 状态：**部分冻结**（§1–§6、§8.2–§8.3 已定稿；§7 待 Q7 确认）
- 适用阶段：`TASK-ANALYZER-001`
- 前置任务：`EXPANDER` 输出 Schema（见 §9 变更清单）
- 相关：ADR-0001、ADR-0002、ADR-0003、`GXML语言规范.md`、`诊断码目录.md`

## 1. 目的与范围

本文定义 Analyzer 与上下游之间的边界：

```text
Parser ──► Expander ──► Analyzer ──► Codegen
                          │
                          └──► Go 类型环境（go/packages）
```

**在范围内**：Analyzer 接收什么、依赖什么、必须输出什么、诊断如何传播。

**不在范围内**：Analyzer 的算法实现、`go/packages` 的具体加载参数、Codegen 的代码模板。

### 1.1 贯穿原则

> 规范负责定义语义，组件负责提供实现所需的信息，诊断目录负责固定错误契约。

因此：

- 上游已经解析并语义化的内容，下游**不得重新解析**。
- 每个模块只做自己那一层的判断，不越界补做别人的工作。
- 为了让某模块方便而重复解析上游结果，等同于制造两套理解，最终会不一致。

## 2. 现状核实（对照代码）

本文基于 2026-10-18 的仓库状态。以下是**实际代码**，不是设想：

| 结构 | 实际定义 | 是否满足 Analyzer |
|---|---|---|
| `expander.Result` | `{Doc *contracts.DocumentIR; Diags []contracts.CompilerError}` | ❌ 缺 Schema |
| `expander.ComponentSchema` | `{Tag string; Properties []PropertySchema; Events []contracts.EventSchema}` | ✅ 内容足够，但**未导出** |
| `expander.PropertySchema` | `{Name string; Type contracts.PropType; Required bool}` | ✅ |
| `contracts.EventSchema` | `{Name string; Params []EventParam; Span SourceSpan}` | ✅ |
| `contracts.EventParam` | `{Name string; Type PropType}` | ⚠️ 无独立 Span，沿用 `EventSchema.Span` |
| `contracts.DocumentIR` | 含 `ViewModel string` | ❌ **纯字符串，无源码位置** |
| `contracts.NodeIR.Attrs` | `AttrList`，每项含 `Span` | ✅ 已分类且带位置 |
| `contracts.NodeIR.Events` | `[]EventBinding`，含 `Name`/`Method`/`Args`/`Span` | ✅ 已分类且带位置 |
| `contracts.PropType` | `Unknown/String/Bool/Int/Float/Color/Insets` | ✅ 6 种具体类型 |

**结论**：开始实现前必须先完成 §9 的三项契约变更。

## 3. 输入 A：Expander 输出

### 3.1 形态

```go
// 变更后（见 §9），位于 internal/compiler/expander
type Result struct {
    Doc     *contracts.DocumentIR
    Schemas SchemaRegistry          // 新增
    Diags   []contracts.CompilerError
}

// SchemaRegistry 只读查询接口
type SchemaRegistry interface {
    // Lookup 按标签名返回组件契约；布局标签（<Row>/<Column>/<Grid>/<Page>）返回 nil。
    Lookup(tag string) (*ComponentSchema, bool)
}
```

### 3.2 内容约束

| 约束 | 说明 |
|---|---|
| **`Schemas` 是已分类的结果，不是原始 `<Defs>` 文本** | Analyzer 不解析 `<Defs>`，只查询属性/事件契约 |
| **必须合并内置控件** | `Text`/`Button`/`Input`/`Image` 的契约在 `builtinSchemas` 中，注册表必须包含它们，否则 `Input.ValueChanged(value:string)` 这类内置事件签名无法校验 |
| **组件与文档同一编译上下文** | 注册表与 `Doc` 必须来自同一次 `Expand` 调用，禁止跨文件或跨轮次混用 |
| **只暴露稳定结果** | 不导出 Expander 的内部缓存、`defs` 索引、`inExpansion` 标记等实现细节 |
| **布局属性不进注册表** | `<Row>` 等布局标签返回 `nil`；布局属性（`Padding`/`Gap`/`Align`…）已由 Parser 完整校验并写入 `NodeIR` 专用字段，Analyzer 无需再判断 |

### 3.3 诊断传播

| 情况 | 规则 |
|---|---|
| `Result.Diags` 非空 | **Analyzer 不得启动**。上游已有阻断性错误，`Doc` 不可用于后续阶段（该约束已写在 `Result.Diags` 的注释中） |
| 调用方忽略该约束 | 属于调用方缺陷；Analyzer 应对 `Doc == nil` 或 `Diags` 非空时返回明确错误，而不是产出误导性的二次诊断 |

## 4. 输入 B：Go 类型环境

### 4.1 需要的外部信息

| 信息 | 来源 | 说明 |
|---|---|---|
| 模块根目录 | 从 `.gxml` 所在目录向上查找 `go.mod` | 用于把 `ViewModel` 的相对路径解析为真实包路径 |
| 目标包路径 | `DocumentIR.Package`（来自 CLI/目录） | 生成代码所在包，**不从 GXML 推导**（规范 §10 已有此约束） |
| 类型加载 | `go/packages` | 加载模式至少需要 `NeedTypes | NeedTypesInfo | NeedSyntax` |
| 加载失败 | 编译错误 | 按 `GXML-ROOT-002` 报告，附包路径 |

### 4.2 `ViewModel` 的合法写法（已定稿）

规范原本只写「Go 类型路径」，从未定义精确语法。以下为**已定稿**的文法：

```text
ViewModel := TypeExpr
TypeExpr  := Ident                          // 同包类型
           | PkgDir "." Ident               // 模块内跨包类型

PkgDir    := Ident ("/" Ident)*             // 模块根相对目录，仅 "/" 分隔
Ident     := Go 标识符
```

**关键点：类型名与包路径之间用 `.` 分隔，包路径内部用 `/` 分隔。** 即**最后一个 `.`** 是分界点，其左侧是包目录、右侧是类型名。

**合法示例：**

| 写法 | 解析结果 |
|---|---|
| `ViewModel="AppState"` | 与 `.gxml` 同目录的包内类型 `AppState` |
| `ViewModel="demo.ViewModel"` | 模块根下 `demo/` 包中的 `ViewModel` |
| `ViewModel="internal/models.User"` | 模块根下 `internal/models/` 包中的 `User` |

**必须拒绝的形式：**

| 形式 | 示例 | 拒绝理由 |
|---|---|---|
| 绝对路径 | `/demo.ViewModel` | 包定位必须相对模块根 |
| 空路径段 | `demo//models.User` | 非法目录 |
| `.` 或 `..` 路径段 | `./demo.User`、`../x.User` | **路径穿越**，会逃出模块根 |
| 反斜杠 | `demo\models.User` | 非 Go 包路径语法；同时避免 Windows/Linux 歧义 |
| 完整导入路径 | `example.com/app/demo.ViewModel` | 见下 |
| 前导/尾随 `.` | `.User`、`demo.` | 类型名或包路径为空 |
| 包路径内含 `.` | `a.b.c` | `PkgDir` 只允许 `/` 分隔的标识符；`a.b` 不合法 |
| 非标识符段 | `demo/1bad.User`、`demo-x.User` | 非 Go 标识符 |
| 指针 / 泛型 | `*demo.ViewModel`、`demo.Box[T]` | 声明的是类型本身；指针由 Codegen 取地址。v1 不支持泛型 |
| 第三方模块 | `other.com/pkg.Type` | 见下 |

注意：完整导入路径会被文法**自然拒绝**——`example.com/app/demo.ViewModel` 中 `example.com` 含 `.`，不满足 `PkgDir := Ident ("/" Ident)*`。

**已定稿的两项边界决策**（原 Q1、Q2）：

| 决策 | 结论 | 理由 |
|---|---|---|
| 第三方模块类型 | ❌ 不支持 | 限定 `go/packages` 加载边界，避免版本解析、依赖下载与网络访问成为编译器的隐式行为 |
| 完整导入路径 | ❌ 不支持（v1） | 模块根已能定位全部内部包。未来若确有需求，再设计导入表或显式包映射，而不是现在放宽文法 |

**代码生成侧**：生成签名固定为 `vm *demo.ViewModel`（见 `代码生成输出契约.md` §4.3）。指针由 Codegen 添加，不在声明中表达。

### 4.3 实现要点（已落地）

`load.go` 的实现遵守以下约束：

| 约束 | 落地方式 |
|---|---|
| `ModuleRoot` 显式提供 | `Config.Validate` 要求绝对路径且目录下有 `go.mod`；**不依赖进程工作目录** |
| 两阶段严格分开 | `ParseTypeExpr` 纯文法，不接触 Go 工具链；`ResolveViewModel` 才加载包 |
| 只在目标包作用域查找 | `pkg.Types.Scope().Lookup(name)`，**不做全局类型名搜索**（否则同名类型会混淆） |
| 包缓存 | `TypeEnv.loaded` 按导入路径缓存，同一 `TypeEnv` 内不重复加载 |
| 加载成功判据 | **检查 `pkg.Errors`**，不看 `err`，也不看 `Types != nil`（见下） |

#### go/packages 的实际行为（实测，x/tools v0.52.0）

| 情形 | `err` | `len(pkgs)` | `pkg.Errors` | `Types` |
|---|---|---|---|---|
| 正常包 | nil | 1 | 0 | 非 nil |
| 包不存在 | nil | 1 | 1 | — |
| 包存在但类型错误 | nil | 1 | 2 | **非 nil** |
| 模式匹配不到 | nil | 1 | 1 | — |

两条必须记住的事实：

1. **`packages.Load` 的 `err` 几乎总是 nil**，真正的错误在 `pkg.Errors`。只看返回值会把「包不存在」当成加载成功。
2. **损坏的包 `Types` 仍可能非 nil**（部分类型信息可用）。因此「能取到类型」绝不能作为加载成功的判据。

这正是「包加载与类型解析是两个阶段」的具体体现。上述行为已由 `load_behavior_test.go` 固化为断言——若 x/tools 升级改变行为，测试会先失败，而不是加载器悄悄失效。

#### 四类失败统一映射到 `ROOT-002`

文法非法、包不存在、类型不存在、符号不是命名类型——四种原因**共用一个码**，通过消息区分。

理由与 `GXML-ATTR-003` 归集码一致：它们的**修复动作相同**——修正 `ViewModel` 声明。拆成四个码只会增加记忆负担而不增加可操作性。

### 4.4 类型解析的唯一入口

```go
// 位于 internal/compiler/analyzer，不导出到 contracts
type TypeRef struct {
    PackagePath string // 模块内相对路径；同包为空
    Name        string
}
```

约束：

- **只有一处解析 `ViewModel`**：单个入口完成字符串切分、包定位、类型查找。
- **结果在本次分析内缓存**：同一个 `ViewModel` 不重复加载。
- **不得散落在各绑定检查器里**：每个检查器都自己切分字符串，会产生不一致的边界判断。

## 5. Analyzer 行为

### 5.1 校验顺序

顺序固定，因为后一步依赖前一步的信息：

| 顺序 | 步骤 | 依赖 | 失败时 |
|---|---|---|---|
| 1 | 解析 `ViewModel` 声明 | 模块根、`go/packages` | **阻断**全部绑定校验（`GXML-ROOT-002`） |
| 2 | 收集 `<For>` 作用域变量 | 遍历 `Doc.Page` | 局部 |
| 3 | 校验属性绑定路径 | 步骤 1 + 2 | 局部，逐属性 |
| 4 | 校验事件处理方法 | 步骤 1 | 局部，逐事件 |
| 5 | 校验事件实参 | 步骤 4 | 仅在方法存在时进行 |

步骤 2 必须在 3 之前：`<For Each="item" In="{Items}">` 内的 `{item.Name}` 首段是**循环变量**，不是 ViewModel 字段。

### 5.2 错误恢复策略

**诊断失败不等于立即终止整个文档分析。** 但也不得在缺少必要信息时伪造成功结果。

| 错误类别 | 可恢复 | 理由 |
|---|---|---|
| `ViewModel` 无法解析 | ❌ **阻断** | 所有绑定的根类型未知，继续检查只会产生大量派生错误 |
| 单条绑定路径无法解析 | ✅ | 各属性互相独立，继续检查其余属性 |
| 事件方法不存在 | ✅ | 各事件互相独立 |
| `go/packages` 加载失败 | ❌ 阻断 | 无类型信息，无法做任何语义判断 |
| 组件 Schema 缺失 | ✅ | 仅该节点无法校验，其余继续 |

**禁止的伪恢复**：

- 绑定路径解析失败后，不得假设其类型为 `string` 继续检查后续属性。
- `ViewModel` 阻断时，不得对绑定路径「跳过检查并当作通过」，必须让下游知道该文档未被完整校验。

### 5.3 诊断去重

| 规则 | 说明 |
|---|---|
| 同一 `SourceSpan` + 同一码 | 只报一次 |
| 同一绑定路径在多处使用 | 每处**分别报**（不同位置都要标） |
| 同一 `<Def>` 被多次展开 | 报在 **Def 定义处**，不按展开次数重复 |
| 上游已报的错误 | 不重复报（例如 Expander 已报 `GXML-ATTR-004` 未知名称，Analyzer 不再报该属性） |

最后一条是关键：Analyzer 只检查**已分类成功**的 `Attrs` 与 `Events`，未知名称在 Expander 阶段已被剔除并报错。

### 5.4 部分分析

`Analyzer.Result` 必须能让下游区分三种状态：

```go
type Status uint8

const (
    StatusOK      Status = iota // 全部校验通过
    StatusPartial               // 有可恢复错误，Doc 部分有效但不可用于 Codegen
    StatusBlocked               // 阻断性错误，未做有效分析
)
```

三态的语义边界：

| 状态 | 含义 | `Doc` 是否可用于 Codegen |
|---|---|---|
| `StatusOK` | 全部校验通过 | ✅ 可以 |
| `StatusPartial` | 存在可恢复的局部诊断；部分节点未校验 | ❌ **不可** |
| `StatusBlocked` | 关键输入不可用（`ViewModel` 或 `go/packages`），未产出有效分析结果 | ❌ 完全不可用 |

`StatusPartial` 的 `Doc` 只允许用于两类用途：

1. **IDE 提示**——让用户一次看到尽可能多的错误。
2. **连续诊断**——避免修一个错、重编译、再看到下一个。

除此之外不得消费。


## 6. Analyzer 输出（语义 IR）

### 6.1 三个层次的职责

| 层次 | 产出 | 是否可变 | 消费者 |
|---|---|---|---|
| **结构 IR** | `contracts.DocumentIR`：节点树、已分类属性与事件、源码位置、宏展开后的结构 | Parser/Expander 产出后**不再改动** | Analyzer、IDE、诊断 |
| **语义 IR** | 本文定义的 `Facts` 与 `Resolved*`：已解析的绑定路径、已解析的类型、已验证的事件映射 | Analyzer 产出 | Codegen、IDE |
| **代码** | Go 源码 | Codegen 产出 | 编译器 |

**关键约束：Analyzer 不修改结构 IR。**

分析结果以**旁表**形式保存（`Facts` 按节点指针索引），而不是往 `NodeIR` 里塞字段。理由：

- 结构 IR 是 Parser/Expander 的产物，被诊断与 IDE 消费；混入语义字段会让「谁负责填这个字段」变得含混。
- 旁表让增量分析与后续扩展有干净的边界——新增一类语义事实只需扩表，不动节点结构。
- 失败时结构 IR 仍完整可用，便于 IDE 显示未校验的原始结构。

### 6.2 Codegen 需要的语义事实

| 事实 | 说明 |
|---|---|
| 绑定路径的字段链 | 每一段的字段名与其所属类型，全部来自真实类型检查 |
| 绑定值的类型 | 含底层类别（string/int/bool/…）与是否实现 `fmt.Stringer` |
| 绑定 → 目标 `PropType` 的匹配结论 | **只产出通过的**：失败的会进诊断，不会出现在 `Facts` 里 |
| `<For>` 循环变量的类型 | 元素类型，供循环体内的绑定解析 |
| 事件形参与两类参数的对应关系 | 逐项标注「来自载荷」还是「来自调用点实参」 |
| 事件方法签名 | 真实形参类型、是否有返回值 |

### 6.3 边界：Analyzer 不做的事

| 不做 | 归属 |
|---|---|
| 生成任何 Go 语句或表达式文本 | Codegen |
| 决定属性如何渲染（`Color` → `color.NRGBA`、`int` → 字符串格式） | Codegen |
| 判断静态子树能否固化、如何缓存 `op.CallOp` | Codegen |
| 重判属性名是否合法、事件是否已声明 | Expander（已完成） |
| 重新解析 `<Defs>` | Expander（已完成，经 `Schemas` 提供） |
| 解析 `.gxml` 语法、位置、属性值形态 | Parser（已完成） |

### 6.4 分析结果

```go
// 位于 internal/compiler/analyzer
type Status uint8

const (
    StatusOK      Status = iota // 全部通过
    StatusPartial               // 有可恢复错误
    StatusBlocked               // 关键输入不可用
)

type Result struct {
    // Doc 是**原样**的结构 IR，未做任何修改。
    Doc *contracts.DocumentIR
    // Facts 是语义 IR；仅当 Status == StatusOK 时完整可用。
    Facts *Facts
    Status Status
    // Diags 用项目既有的诊断类型，不另造一套。
    Diags []contracts.CompilerError
}
```

`Doc` 保持原样这一点是刻意的：`StatusPartial` 时 IDE 仍能基于完整结构 IR 展示界面并标注部分错误。

### 6.5 语义事实容器

```go
// Facts 是按节点索引的语义事实旁表。只读。
type Facts struct {
    nodes map[*contracts.NodeIR]*NodeFacts
}

// Lookup 返回某个节点的语义事实。未校验的节点返回 (nil, false)。
func (f *Facts) Lookup(n *contracts.NodeIR) (*NodeFacts, bool)

type NodeFacts struct {
    // Attrs 是已解析的属性绑定（不含静态字面量属性）。
    Attrs []ResolvedAttr
    // Events 是已验证的事件绑定。
    Events []ResolvedEvent
    // For 仅在节点是 KindFor 时非 nil。
    For *ResolvedFor
}
```

**只记录已通过校验的事实。** 失败的属性不在 `Attrs` 里，而是对应一条诊断——避免出现「有事实但已被判定失败」的矛盾状态。

### 6.6 绑定路径

```go
// RootKind 标识路径的起点。
type RootKind uint8

const (
    // RootViewModel 起点是 ViewModel 根对象。
    RootViewModel RootKind = iota + 1
    // RootForVar 起点是 <For> 引入的循环变量。
    RootForVar
)

type ResolvedPath struct {
    // Root 是起点名称：ViewModel 时为空；循环变量时为变量名（如 "item"）。
    Root     string
    RootKind RootKind
    // Steps 按访问顺序记录字段。
    Steps []PathStep
    // Type 是整条路径解析后的类型。
    Type TypeInfo
    // Negated 对应 {!Path}。仅当 Type.Kind 为 ValueBool 时允许存在。
    Negated bool
}

type PathStep struct {
    Name string
    // Type 是访问该字段后的类型。
    Type TypeInfo
}
```

**不设计的部分**：索引访问（`Items[0]`）、Map 取值、方法调用、任意表达式。这些都不在已冻结的点分字段路径语法内，等语法扩展时再设计对应 IR。

### 6.7 类型表示

`contracts` 不得依赖 `go/types`，但 Codegen 也不能拿到一个字符串再自己解析。因此语义 IR 提供**结构化的类型事实**：

```go
// ValueKind 是类型的底层类别（已剥离命名类型的外壳）。
type ValueKind uint8

const (
    ValueUnknown ValueKind = iota
    ValueString
    ValueBool
    ValueInt      // 所有整数类型
    ValueFloat    // float32 / float64
    ValueStruct
    ValueSlice
    ValueMap
    ValuePointer
    ValueInterface
    ValueOther
)

// TypeInfo 描述一个已通过类型检查的类型。
type TypeInfo struct {
    // Display 是可打印形式（如 "demo.User"）。**仅用于诊断与日志**，
    // 消费者不得解析它来推断语义。
    Display string
    // Kind 是底层类别。对 `type Status string`，Kind 为 ValueString。
    Kind ValueKind
    // Named 表示是否为命名类型（有包限定）。
    Named bool
    // Stringer 表示是否实现 fmt.Stringer。
    Stringer bool
}
```

**为什么 `Display` 不能作为 Codegen 的输入**：字符串是给人和诊断用的，解析它等于让 Codegen 重做类型检查。Codegen 需要判断时看 `Kind` / `Named` / `Stringer`。

这也回答了「`Type string` 是否足够稳定」：不够，所以 `Display` 只是附带信息，**语义由结构化字段承载**。

### 6.8 事件绑定

必须保留三类独立信息，不能只存拼接后的参数列表——否则无法区分某个参数来自事件载荷还是 XML 实参，也无法生成准确的诊断。

```go
type ParamOrigin uint8

const (
    // FromPayload 参数来自组件 Schema 的事件声明。
    FromPayload ParamOrigin = iota + 1
    // FromCallArg 参数来自 XML 中显式写出的调用点实参。
    FromCallArg
)

type ResolvedParam struct {
    Name string
    Type TypeInfo
    Span contracts.SourceSpan
}

type ResolvedMethod struct {
    Name   string
    Params []ResolvedParam
    // HasReturn 记录方法是否有返回值。
    // Q4 规定不允许返回值，此事实用于产出 EVENT 类诊断。
    HasReturn bool
}

type ResolvedEvent struct {
    // Name 是事件名，如 "ValueChanged"。
    Name string
    // Payload 来自 ComponentSchema.Events[i].Params。
    Payload []ResolvedParam
    // CallArgs 来自 XML 的 EventBinding.Args。
    CallArgs []ResolvedParam
    // Method 来自 go/types 的真实签名。
    Method ResolvedMethod
    // Mapping 与 Method.Params 一一对应，标注每个形参的来源。
    Mapping []ParamOrigin
}
```

**`Mapping` 的作用**：Codegen 按它生成调用，无需推断「这个参数从哪来」。

**`ResolvedEvent` 与契约 §8.4 的对应**：

| 字段 | 对应 §8.4 |
|---|---|
| `Payload` | R1 的「事件声明参数」 |
| `CallArgs` | R1 的「调用点实参」 |
| `Method.Params` | 目标 Go 方法的真实签名 |
| `Mapping` | 拼接后的对应关系；逐项标注 R1 的顺序 |

`Mapping` 的长度必然等于 `Method.Params` 的长度（R2 已校验）。若不等，说明 Analyzer 有实现缺陷，不是用户错误。

### 6.9 `<For>` 作用域

```go
type ResolvedFor struct {
    // Each 是循环变量名（如 "item"）。
    Each string
    // Elem 是元素类型（可迭代体已被校验）。
    Elem TypeInfo
    // Index 是索引变量名，可为空。
    Index string
    // IndexType 仅在 Index 非空时有效。
    IndexType TypeInfo
    // Key 是已解析的 Key 路径，可为 nil（无状态循环可不提供）。
    Key *ResolvedPath
}
```

循环体内的绑定以 `RootKind == RootForVar`、`Root == Each` 指向它。

### 6.10 准入门禁：用类型系统而非约定

§6.5 的「编排层检查状态」仍依赖调用方自觉。更可靠的做法是让**绕过门禁在类型上不可能**：

```go
// VerifiedDoc 是「已通过全部校验」的凭证。
//
// 字段全部未导出，且只能由 Result.Verified 构造，
// 因此包外无法伪造。
type VerifiedDoc struct {
    doc   *contracts.DocumentIR
    facts *Facts
}

// Verified 仅在 Status == StatusOK 时返回非 nil。
func (r *Result) Verified() (*VerifiedDoc, error)

func (v *VerifiedDoc) Document() *contracts.DocumentIR
func (v *VerifiedDoc) Facts() *Facts
```

Codegen 的入口签名接收 `*analyzer.VerifiedDoc`：

```go
// 位于 internal/compiler/codegen
func Generate(v *analyzer.VerifiedDoc, opts Options) ([]byte, error)
```

**这样 `StatusPartial` / `StatusBlocked` 的 IR 在编译期就无法传进 Codegen**——不需要运行时检查，也不依赖任何人「记得检查」。

门禁测试因此可以断言「无法构造出 VerifiedDoc」这一类型层面的事实，而不只是「某个状态存在」。

### 6.11 为什么保留三个全局状态

| 理由 | 说明 |
|---|---|
| 当前只需要一个判定 | 编译器只关心「能否进入 Codegen」，这是文档级的二值问题 |
| 节点级状态服务于 IDE | 精确到节点的可诊断性、增量重分析属于 IDE 场景，当前没有这个消费者 |
| 过早引入会固化错误的粒度 | 一旦 `NodeIR` 携带状态，所有下游都要处理它，而实际用途尚未明确 |

节点级状态留待将来由 IDE 集成或增量分析的真实需求驱动。

### 6.12 最小契约测试夹具

夹具的目标是**固定边界行为**，不是搭一个大而全的集成框架。

**目录结构**（每个夹具一个子目录，含 `.gxml` 与可被 `go/packages` 加载的 Go 类型）：

```text
internal/compiler/analyzer/testdata/
├── fixture/            # 共享的 ViewModel 类型包
│   └── vm.go
└── cases/
    ├── valid-binding/
    │   ├── case.gxml
    │   └── want.txt    # 期望状态 + 诊断码
    └── ...
```

| 夹具 | 输入 | 期望 |
|---|---|---|
| `valid-binding` | `{User.Name}`，字段存在且可访问 | `StatusOK`；`Steps` 与 `Type` 正确；`Mapping` 无关 |
| `invalid-root` | 绑定首段不存在 | `StatusPartial`；`GXML-BIND-001`；该属性**不在** `Facts` 中 |
| `invalid-nested-field` | 中间字段不存在 | `GXML-BIND-002`；不伪造成功路径 |
| `unexported-field` | 字段存在但未导出 | `GXML-BIND-003` |
| `type-mismatch` | 绑定类型与属性目标不符 | `GXML-BIND-004` |
| `negate-non-bool` | `{!Count}`，`Count` 为 `int` | `GXML-BIND-004`（Q6） |
| `valid-event-payload` | `ValueChanged="OnNameChanged"` | `StatusOK`；`Payload` 1 项；`Mapping = [FromPayload]` |
| `valid-event-args` | `Click="Delete(item.ID)"` | `StatusOK`；`CallArgs` 1 项；`Mapping = [FromCallArg]` |
| `valid-event-combined` | 自定义事件 `Open(id:string)` + `item.ID` | `Mapping = [FromPayload, FromCallArg]` |
| `invalid-event-arity` | 形参数量与拼接结果不符 | `GXML-EVENT-004` |
| `invalid-event-type` | 形参类型不兼容 | `GXML-EVENT-006` |
| `invalid-method-missing` | 方法不存在 | `GXML-EVENT-003` |
| `for-not-iterable` | `<For In>` 非可迭代类型 | `GXML-SCOPE-004` |
| `blocked-viewmodel` | `ViewModel` 无法解析 | `StatusBlocked`；`GXML-ROOT-002`；只报一条（不产生派生错误） |
| `codegen-gate` | 取 `StatusPartial` / `StatusBlocked` 的 `Result` | **`Verified()` 返回 error，无法取得 `VerifiedDoc`** |

`codegen-gate` 是门禁的实证：它验证的不是「某个状态存在」，而是「**拿不到进入 Codegen 的凭证**」。

### 6.13 已实现与待验证

| 项目 | 状态 |
|---|---|
| `ir.go` 类型定义（§6.4–§6.9） | ✅ 已实现 |
| `Verified()` 凭证发放与拒绝 | ✅ 已实现并测试 |
| `VerifiedDoc` 防伪（字段未导出） | ✅ 已实现，反射测试锁定 |
| `Facts` 冻结（发放凭证后写入 panic） | ✅ 已实现并测试 |
| `NodeFacts` 访问器返回副本 | ✅ 已实现，通过「修改返回值不影响内部」验证 |
| 16 个夹具目录 + 期望表 | ✅ 已建，且已验证全部通过 Parser/Expander |
| **Go 类型加载与 ViewModel 解析** | ✅ 已实现（`load.go`）：`TypeExpr` 文法、包定位、类型查找、缓存 |
| 绑定路径解析（`BIND-001`~`003`） | ✅ 已实现 |
| 类型兼容（`BIND-004`，契约 §11） | ✅ 已实现 |
| 事件签名校验（`EVENT-003`/`004`/`006`/`007`） | ✅ 已实现 |
| `<For>` 可迭代性与作用域（`SCOPE-004`） | ✅ 已实现 |
| **编排层** | ⬜ 不存在 |
| **编排层门禁的实际路径** | ⬜ **待验证** |

最后一项需要特别说明：当前只能验证「`Verified()` 在非 OK 时拒绝发放凭证」，**不能**验证「编排层确实阻止了 Codegen 执行」。

完整验证需要两个前提：

1. Codegen 存在，且其入口签名为 `Generate(*analyzer.VerifiedDoc, ...)`；
2. 存在编排层（编译管线驱动），负责串联 Parser → Expander → Analyzer → Codegen。

届时必须补一条测试：**用 `StatusPartial` / `StatusBlocked` 的输入跑完整编排流程，断言没有产生任何生成产物**。这不能用手动调用 `Verified()` 代替——它验证的是管线而不是 API。

在此之前，`TestAnalyzeBehavior` 以 `t.Skip` 显式标记该状态，避免被误认为已覆盖。


## 7. 诊断码增补方案

### 7.1 原则

- **按「根本语义是否不同」划分**，不按错误文案划分。
- **不为每种错误机械分配新码。**
- 新增码必须同步更新 `诊断码目录.md`（全表、空置清单、测试）。

### 7.2 关于 `GXML-BIND-001` 的既有承诺

`GXML-BIND-001` 当前是**预留码**（`诊断码目录.md` §2.7），目录中记录的规划含义是：

> 绑定路径不存在或不可访问（如 `User.Nmae` 拼写错误）

这条记录**同时包含两种语义**（不存在 / 不可访问）。若按 §7.1 原则拆分，需要**重写**这条承诺，而不是把编号自动分配给第一个实现的功能。

### 7.3 提议新增的码

| 码 | 常量建议 | 语义 | 触发条件 |
|---|---|---|---|
| `GXML-ROOT-002` | `CodeViewModelType` | `ViewModel` 声明无法解析 | 包不存在、类型不存在、非结构体类型 |
| `GXML-BIND-001` | `CodePathRoot` | 绑定路径**首段**无法解析 | 首段既不是 ViewModel 字段，也不是作用域变量 |
| `GXML-BIND-002` | `CodePathField` | 路径**中间段**字段不存在 | 前段类型上无该字段/方法 |
| `GXML-BIND-003` | `CodePathUnexported` | 字段存在但不可访问 | 字段未导出 |
| `GXML-BIND-004` | `CodeBindTypeMismatch` | 绑定值类型与属性目标类型不匹配 | 如 `{User.Age}` 给 `string` 属性 |
| `GXML-BIND-005` | `CodeBindUnsupported` | 不支持的绑定表达式形式 | 如对非结构体取字段、对方法取结果以外的形式 |
| `GXML-EVENT-003` | `CodeMethodNotFound` | 事件处理方法不存在 | 接收者上无此方法 |
| `GXML-EVENT-004` | `CodeMethodArity` | 事件方法参数数量不匹配 | **载荷数 + 调用点实参数** ≠ 方法形参数量（见 §8.4） |
| `GXML-EVENT-006` | `CodeMethodArgType` | 事件方法参数类型不匹配 | 按「载荷在前、实参在后」的顺序，某一项类型不兼容（见 §8.4） |
| `GXML-SCOPE-004` | `CodeNotIterable` | `<For In>` 的值不可迭代 | 不是切片/数组/Map |

共 **10 个新码**，总数从 23 → 33。

### 7.4 为什么这样拆

| 拆分的码 | 分开的理由 |
|---|---|
| `BIND-001` vs `BIND-002` | **修复动作不同**：首段错误去查 ViewModel，中间段错误去查嵌套类型定义 |
| `BIND-002` vs `BIND-003` | 「不存在」的修复是改字段名，「未导出」的修复是导出字段——后者更常被误判为前者，合并会浪费最有价值的提示 |
| `BIND-004` vs `BIND-001` | 类型不匹配是「找到了但用错」，修复动作是转换或换字段，与「找不到」完全不同 |
| `BIND-005` vs `BIND-001` | **规范明确要求**：「不支持的绑定表达式形式不能误报成路径不存在」（见 §7.5） |
| `EVENT-003` vs `EVENT-004` | 方法不存在要去看 ViewModel；参数数量不对要去看方法签名 |
| `EVENT-004` vs `EVENT-006` | 同上，改法不同 |

### 7.5 必须避免的误报

规范 §4 禁止混合绑定与表达式。这类输入在 Parser 阶段已被拒绝（`GXML-ATTR-003`），**Analyzer 不应再遇到**。

但若某些形态逃过 Parser（例如对非结构体取字段），必须报 `BIND-005` 而非 `BIND-001`——否则用户会去检查拼写，而真正的问题是表达式形式不被支持。

## 8. 评审结论

### 8.1 已定稿（原 Q1–Q6）

| 编号 | 决策 | 归类 | 落实位置 |
|---|---|---|---|
| **Q1** | 只允许模块内类型；第三方模块类型不支持 | 语言与构建边界 | §4.2 |
| **Q2** | v1 不支持完整导入路径 | 语言与构建边界 | §4.2 |
| **Q3** | 保留 `BIND-002` 与 `BIND-003` 两个独立码 | 诊断语义 | §7.3 |
| **Q4** | 事件方法**不允许返回值** | 语言与构建边界 | §8.2 |
| **Q5** | 保留三个全局 `Status`，不引入节点级状态 | 当前阶段范围控制 | §6.6 |
| **Q6** | `{!Path}` 只允许作用于布尔类型 | 类型系统规则 | §8.3 |

### 8.2 事件方法签名的形态（Q4 的展开）

**事件处理方法不得有返回值。** 合法形态：

```go
func (vm *ViewModel) OnClick()
func (vm *ViewModel) OnDelete(id int)
```

**不合法**：

```go
func (vm *ViewModel) OnClick() error        // ❌ 返回值
func (vm *ViewModel) OnClick() bool         // ❌ 返回值
func (vm *ViewModel) OnClick() (int, error) // ❌ 多返回值
```

理由：UI 事件处理器应具有明确的调用契约。一旦允许返回值，就必须定义「返回值怎么处理」——忽略？记录？传播为运行时错误？这些都是额外语义，v1 不引入。

**接收者**必须是 `*ViewModel` 或其方法集包含该方法的类型。接收者类型由 `ViewModel` 声明决定，Analyzer 校验方法是否在该类型的方法集上。

### 8.3 `{!Path}` 的类型要求（Q6 的展开）

取反是**逻辑操作**，因此被取反的路径最终类型必须是 `bool`。

| 表达式 | 目标类型 | 结论 |
|---|---|---|
| `{!ReadOnly}` | `bool` | ✅ |
| `{!Count}` | `int` | ❌ `GXML-BIND-004` |
| `{!Name}` | `string` | ❌ `GXML-BIND-004` |

**不得隐式转换**：其他类型不会被自动「按零值/空值」解释为布尔。理由与「未知名称绝不退化为事件」一致——隐式转换会把类型错误变成行为差异，而后者更难定位。

### 8.4 事件载荷与调用点实参（Q7，已定稿）

**问题**：规范 §5.5 规定「实参必须与处理方法签名匹配」，但从未说明事件携带的数据如何到达方法。这导致最常见的场景无法成立：

```xml
<Input Value="{User.Name}" ValueChanged="OnNameChanged"/>
```

`ValueChanged` 自带 `value:string`。若「方法形参 = 调用点实参」，则 `OnNameChanged` 必须无参——**输入框的新值无处可达**，`ValueChanged` 失去意义。

**决策：方案 A —— 载荷与实参严格分开，按序拼接。**

#### 8.4.1 方法形参的组成

```text
MethodParams = EventParams ++ CallArgs
```

`++` 表示按顺序拼接（不是 Go 运算符）：

1. **事件载荷参数**：由组件 Schema 的事件声明决定。
2. **调用点实参参数**：由 XML 中事件绑定表达式显式写出的实参决定。

| XML | 事件载荷 | 调用点实参 | 处理方法签名 |
|---|---|---|---|
| `Click="Delete(item.ID)"` | 无 | `item.ID` | `Delete(id int)` |
| `ValueChanged="OnNameChanged"` | `value string` | 无 | `OnNameChanged(value string)` |
| `Open="OnOpen(item.ID)"` | `id string` | `item.ID` | `OnOpen(id string, cardID int)` |

最后一行假定 `item.ID` 的 Go 类型为 `int`。

#### 8.4.2 四条强制规则

| 编号 | 规则 | 违反后果 |
|---|---|---|
| **R1 顺序固定** | 事件载荷永远在前，调用点实参永远在后。**不允许根据方法签名反向猜测参数归属** | 归属于 Analyzer 实现缺陷；无法诊断 |
| **R2 数量精确** | 载荷数与实参数之和必须**恰好等于**方法形参数量。不允许忽略载荷、自动补参或丢弃多余参数 | `GXML-EVENT-004` |
| **R3 类型逐项** | 先按事件声明顺序校验载荷类型，再按实参顺序校验实参类型。**v1 不做隐式类型转换** | `GXML-EVENT-006` |
| **R4 载荷属于组件契约** | 内置事件与自定义事件统一适用。Schema 必须提供事件参数的名称、类型与声明位置 | 无 Schema 则无法校验，属于契约缺失 |

**R2 的关键含义**：`ValueChanged="OnNameChanged"` 不写任何实参，`OnNameChanged` 仍必须接收 `value string`。这正是两类参数必须区分的原因。

**R4 的位置限制**：`contracts.EventParam` 当前没有独立 `Span`，只能沿用 `EventSchema.Span`。因此诊断位置**只能准确指向事件声明整体**，不得假装定位到了某个独立参数。若将来需要参数级定位，再为 `EventParam` 增加 `Span`。

#### 8.4.3 实现必须避免的歧义

**不得把事件载荷当作隐式的 XML 调用表达式**，也不得把它拼成 `OnNameChanged(value)` 这样的源码字符串再解析。

Analyzer 必须**分别构建并校验**三项独立信息：

```text
① 事件 Schema 提供的载荷参数   （来自 ComponentSchema.Events[i].Params）
② XML 绑定中显式写出的调用点实参（来自 EventBinding.Args）
③ 目标 Go 方法的真实签名        （来自 go/types）
```

然后按 R1–R3 做匹配，把**结论**写入 IR。Codegen 只消费结论，**不重新推断参数含义**。

这条约束的理由：若载荷被当作隐式实参插入到表达式里，那么「哪些参数是编译器生成的、哪些是用户写的」这一区分会在整条流水线上丢失，Codegen 将无法正确生成调用代码。

#### 8.4.4 对既有规范的影响

| 条款 | 影响 |
|---|---|
| `Click="Delete(item.ID)"` | ✅ 保持合法，不因 `Click` 无载荷而改变既有调用语义 |
| `ValueChanged="OnNameChanged"` | ✅ 不要求 XML 显式写出 `value`；事件触发时由组件提供载荷 |
| 规范 §5.5「实参必须与处理方法签名匹配」 | ⚠️ 需修订为「**载荷与实参拼接后**必须与处理方法签名匹配」 |

最后一条是规范正文的实质性修订，需同步更新 `GXML语言规范.md` §5.5。


## 9. 冻结状态

| 项目 | 状态 |
|---|---|
| §3 输入 A（Expander 输出） | ✅ 已定稿 |
| §4 输入 B（Go 类型环境） | ✅ 已定稿（Q1/Q2 已决） |
| §5 Analyzer 行为 | ✅ 已定稿 |
| §6 语义 IR（输出与门禁） | ✅ 已定稿（本次细化：§6.4–§6.10 字段级设计） |
| §7 诊断码增补 | ✅ 已定稿（Q7 已决，`EVENT-004`/`006` 规则见 §8.4） |
| §8.2 事件方法形态 | ✅ 已定稿（Q4 已决） |
| §8.3 取反类型要求 | ✅ 已定稿（Q6 已决） |
| §8.4 事件载荷与实参（Q7） | ✅ 已定稿（方案 A） |

**契约已全部冻结，三项代码变更可以开始。**

Q7 确认后，`SchemaRegistry` 的接口形态已经明确：它必须能提供事件声明参数（名称 + 类型），供 §8.4 的 R2/R3 校验使用。


## 10. 冻结前必须完成的契约变更

以下三项是**实现前置条件**。建议按此顺序**分三次提交**，这样出现回归时能清楚判断是 Schema 接口、源码位置还是诊断契约引入的问题。

| 顺序 | 变更 | 文件 | 说明 |
|---|---|---|---|
| 1 | `Result` 增加 `Schemas` | `internal/compiler/expander/expander.go` | 导出 `ComponentSchema` 与只读查询接口；合并 `builtinSchemas` |
| 2 | `DocumentIR` 记录 `ViewModel` 位置 | `contracts/ir_schema.go` | 现在只有 `ViewModel string`，报 `ROOT-002` 时无法指向属性。改为 `ViewModelDecl{Value string; Span SourceSpan}` |
| 3 | 诊断码定稿 | `docs/03-架构/诊断码目录.md` + 各包常量 | 按 §7 定稿后再实现，避免实现中反复改号 |
| 4 | 语义 IR 类型定义 | 新建 `internal/compiler/analyzer/ir.go` | 按 §6.4–§6.9 定义 `Status`/`Result`/`Facts`/`Resolved*`/`TypeInfo` 等类型，**暂不实现校验逻辑** |
| 5 | 契约测试夹具 | `internal/compiler/analyzer/testdata/` | 按 §6.12 建 15 个夹具的目录骨架与期望文件，先不实现 Analyzer |

**提交顺序**：第 1–3 项已完成（见 Git 提交 `5e71510`、`ef92216`、`f203443`）。第 4–5 项是**纯结构定义**，不含校验逻辑——先固定类型与夹具，再实现，避免临时 IR 被推翻。

每项的验收要求：

| 变更 | 验收 |
|---|---|
| 1. `Schemas` | 能查询到内置控件与用户组件的契约；布局标签返回 nil；现有测试全绿 |
| 2. `ViewModelDecl` | 合法与非法 `ViewModel` 声明都能定位到属性列；`ROOT-002` 的 Span 非零 |
| 3. 诊断码 | 目录与代码一致性门禁通过；`GXML-BIND-001` 的既有承诺调整有明确记录 |
| 4. 语义 IR 类型 | 能编译；`contracts` 仍不依赖 `go/types`；`Verified()` 在非 OK 时返回 error；类型定义有注释说明「消费者不得解析 `Display`」 |
| 5. 夹具骨架 | 15 个夹具目录齐全，每个含 `.gxml` 与期望状态/诊断码；夹具 Go 类型包可被 `go/packages` 加载 |

## 11. 类型兼容契约（Go 类型 → `PropType`）

本节是 Analyzer 与组件 Schema 之间的类型契约。`BIND-004` 只按本节判定。

### 11.1 三层不能合并

| 层 | 问题 | 归属 |
|---|---|---|
| **类型兼容** | 绑定值的 Go 类型能否赋给该属性？ | Analyzer（本节） |
| **特殊表达式约束** | `{!Path}` 这类形式对类型有额外要求 | Analyzer（§11.6） |
| **值转换** | 兼容的值如何变成 Gio 调用参数 | **Codegen**（不在本节） |

把三者合成一张映射表会让「兼容」与「如何生成」纠缠：同一个 `int` 属性可能因为控件不同而需要不同的格式化方式，那是 Codegen 的事，不是类型契约。

### 11.2 兼容矩阵

`TypeInfo.Kind` 是**底层类别**（命名类型已剥壳）。参数 `PropType` 见 `contracts.PropType`。

| 绑定值 `Kind` | `string` | `bool` | `int` | `float` | `color` | `insets` |
|---|:--:|:--:|:--:|:--:|:--:|:--:|
| `ValueString` | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `ValueBool` | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |
| `ValueInt`（有符号） | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| `ValueUint`（无符号） | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `ValueFloat` | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ |
| `ValueStruct` | ❌ | ❌ | ❌ | ❌ | 见 §11.5 | 见 §11.5 |
| `ValuePointer` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `ValueSlice` / `ValueMap` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `ValueInterface` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| `ValueOther` / `ValueUnknown` | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

三条关键决定：

| 决定 | 理由 |
|---|---|
| **`int` → `string` 拒绝** | 规范 §4.5 已规定「不进行隐式类型转换」。若允许，同一个绑定在不同属性上可能触发不同格式化，Analyzer 与 Codegen 的语义就会分叉。需要格式化时应在 ViewModel 中提供计算字段。 |
| **`int` → `float` 拒绝** | 同上。虽然数值上无损扩展，但会开启「哪些转换算无损」的连锁判断。 |
| **`ValueUint` 一律拒绝** | 无符号类型需要显式的范围检查（`uint64` 可能超出 `int`），这属于值转换而非类型兼容。v1 要求 ViewModel 使用有符号类型或先在自身代码里转换。 |

### 11.3 命名类型与别名

| 情形 | 处理 |
|---|---|
| `type Status string` | `Kind = ValueString`、`Named = true` → **兼容 `string`** |
| `type Enabled bool` | `Kind = ValueBool`、`Named = true` → **兼容 `bool`** |
| `type Count int` | `Kind = ValueInt`、`Named = true` → **兼容 `int`** |
| `type StatusAlias = string` | 别名：`Kind = ValueString`、`Named = false` → **兼容 `string`** |

**兼容按底层类别判定，但 `Named` 必须保留。** 类型身份可能影响 Codegen 的生成（例如需要调用 `String()`），不能在判定兼容后就丢弃。

别名与定义类型在兼容上等价，但 `go/types` 对它们的表示不同（别名可能是 `*types.Alias`）。测试必须同时覆盖两者，避免表示差异造成意外。

### 11.4 整数与浮点范围

| 类别 | 接受的 Go 类型 |
|---|---|
| `int` | `int`、`int8`、`int16`、`int32`、`int64` |
| `float` | `float32`、`float64` |
| `int` **不**接受 | `uint`、`uint8`~`uint64`、`uintptr` |

接受全部有符号宽度是因为它们在**语言层面**都是整数语义，宽度差异属于 Codegen 的取值处理（`int8` → `int` 是保值的扩展）。无符号被排除，理由见 §11.2。

⚠️ 已知边界：`int64` → `int` 在 32 位平台上可能溢出。这是 Codegen 的责任，Analyzer 不做范围检查（值在编译期通常未知）。

### 11.5 `color` 与 `insets`：精确类型身份

这两类**不能按 `Kind` 判定**——`ValueStruct` 太宽，任何含 RGBA 字段或四个 float 字段的结构体都会被误接受。

因此按**类型身份**（`types.Identical`）匹配：

| `PropType` | 接受的 Go 类型 | 来源 |
|---|---|---|
| `color` | `color.NRGBA` 或 `color.RGBA` | 标准库 `image/color` |
| `insets` | `contracts.EdgeInsets` | 本项目 `github.com/goxml/goxml/contracts` |

理由与限定：

- **只用标准库与本项目自有类型**，不引入 Gio 类型：编译器当前不依赖 `gioui.org`（`go.mod` 只有 `golang.org/x/tools`），让 `contracts`/Analyzer 引用 Gio 会把渲染库拖进编译器依赖图。
- **主题引用是静态的**：`$Theme.Primary` 在编译期解析为具体颜色，不是运行时绑定。因此绑定到 `color` 属性的值只可能是具体颜色，不需要 `IsThemeRef` 那种表示。
- **`contracts.NRGBAColor` 不作为绑定的目标类型**：它是静态字面量与主题引用的编译期表示（含 `IsThemeRef`/`ThemeField`），语义是「已经确定的颜色」，与「运行时取值的绑定」不同。把它作为绑定类型会让两套表示混在一起。

### 11.6 `{!Path}` 的独立规则

`{!Path}` **不复用**上面的兼容矩阵，先做表达式自身的合法性检查：

| 路径解析结果 | `{!Path}` |
|---|---|
| `ValueBool` | ✅ 允许 |
| 命名布尔类型（`Kind = ValueBool`） | ✅ 允许 |
| `ValueString` / `ValueInt` / `ValueFloat` / 其他 | ❌ `BIND-004` |
| 路径本身失败（不存在/不可访问） | 沿用 `BIND-001`/`002`/`003`，**不再额外报类型不匹配** |

最后一条是为了避免级联诊断：一条路径解析失败后，再报「不能取反」会让用户以为有两个问题。

**不得把未知类型推断为 `bool`。** 若类型信息不完整，按 `StatusBlocked` 处理，而不是猜。

### 11.7 `PropUnknown` 是内部契约错误

`Target == PropUnknown` 表示 Schema 没有声明该属性的类型。这**不是**用户错误，也不得默认放行。

处理方式：该绑定不进入 `Facts`（视为未验证），且不产生用户诊断。原因是 Schema 完整性是编译器自身的责任，不该让用户看到一条他们无法修复的错误。

> **前置条件**：内置 Schema 必须为所有属性声明 `Type`。此前 `Text`/`Button`/`Input`/`Image` 的属性只写了 `Name`，`Type` 全为 `PropUnknown`——这是本契约落地前必须补齐的缺口（见 `TASK-EXPANDER-001` 的 Schema 补全）。

### 11.8 Analyzer 与 Codegen 的分工

| 职责 | 归属 |
|---|---|
| 判定「这个 Go 类型能否赋给这个属性」 | Analyzer |
| 判定 `{!Path}` 是否合法 | Analyzer |
| 生成取值表达式（字段链） | Codegen |
| `int8` → `int` 的宽度处理、`int` → 显示字符串的格式化、`color.NRGBA` → Gio 参数的转换 | **Codegen** |
| 范围检查（`int64` 溢出、`uint64` 越界） | Codegen（编译期未知值时不做） |

Codegen **不得**重新判断兼容性——它只消费 Analyzer 已验证的事实。

## 12. 版本演进边界

- 引入表达式求值 → 推翻 ADR-0002，需新 ADR。
- 允许第三方模块类型或完整导入路径 → 修改 §4.2，需评审。
- Analyzer 输出中引入 `go/types` 类型进 `contracts` → 破坏依赖方向，需新 ADR。
- 让 Codegen 自行推断绑定类型 → 违反 §6.3 边界。
- `Status` 语义变化 → 影响 Codegen 的准入判断，需同步修改 `代码生成输出契约.md`。