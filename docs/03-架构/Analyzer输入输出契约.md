# Analyzer 输入输出契约

- 状态：**待评审**（未冻结）
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

### 4.2 `ViewModel` 的合法写法（**需评审确认**）

规范目前只写「Go 类型路径」，**从未定义精确语法**。以下为提议：

```text
ViewModel := TypeExpr
TypeExpr  := Ident                      // 同包类型
           | PkgDir "/" ... "/" Ident    // 模块内跨包类型，目录路径相对模块根

PkgDir    := Ident
Ident     := Go 标识符
```

**合法示例：**

| 写法 | 解析结果 |
|---|---|
| `ViewModel="AppState"` | 与 `.gxml` 同目录的包内类型 `AppState` |
| `ViewModel="demo.ViewModel"` | 模块根下 `demo/` 包中的 `ViewModel` |
| `ViewModel="internal/models.User"` | 模块根下 `internal/models/` 包中的 `User` |

**明确不支持：**

| 形式 | 不支持的理由 |
|---|---|
| 完整导入路径（`example.com/app/demo.ViewModel`） | 模块根已能定位全部内部包；引入完整路径会与 `go.mod` 形成两套来源 |
| 导入别名 | `.gxml` 没有导入表，别名无处声明 |
| 指针/泛型（`*demo.ViewModel`、`demo.Box[T]`） | 声明的是类型本身；指针由 Codegen 按需取地址。v1 不支持泛型组件 |
| 第三方模块中的类型 | `go/packages` 可以加载，但会引入版本解析问题；v1 限制在当前模块内 |

**代码生成侧**：生成签名固定为 `vm *demo.ViewModel`（见 `代码生成输出契约.md` §4.3）。指针由 Codegen 添加，不在声明中表达。

⚠️ **此项需评审确认**，尤其是「是否允许第三方模块类型」与「完整导入路径」。

### 4.3 类型解析的唯一入口

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

**只有 `StatusOK` 才允许进入 Codegen。** `StatusPartial` 的 `Doc` 仅用于 IDE 提示与连续诊断，不可生成代码。

## 6. Analyzer 输出

### 6.1 输出形态

```go
// 位于 internal/compiler/analyzer
type Result struct {
    Doc    *contracts.DocumentIR   // 原地补充了已解析的绑定信息
    Status Status
    Diags  []contracts.CompilerError
}
```

### 6.2 Codegen 需要的、Analyzer 必须提供的语义

| 信息 | 当前状态 | Analyzer 产出 |
|---|---|---|
| 绑定路径的字段链 | `BindingVal.Segments` 只有字符串，未验证 | 每段标注所属类型与是否导出 |
| 绑定值的 Go 类型 | ❌ 无 | 最终类型（如 `string`、`int`、`demo.Status`） |
| 绑定类型 → 目标 `PropType` 的匹配结论 | ❌ 无 | 直接给出匹配结果；Codegen 不重新判断 |
| 循环变量类型 | ❌ 无 | `<For>` 内 `item` 的类型 |
| 事件处理方法的签名 | ❌ 无 | 参数类型、返回值、是否可调用 |
| 事件实参的类型 | ❌ 无 | 每个实参解析后的类型 |

### 6.3 边界：Analyzer 不做的事

| 不做 | 归属 |
|---|---|
| 生成任何 Go 语句或表达式文本 | Codegen |
| 决定属性的渲染方式（如 `Color` 如何转 `color.NRGBA`） | Codegen |
| 判断静态子树能否固化、如何缓存 `op.CallOp` | Codegen |
| 重新判断属性名是否合法、事件是否已声明 | Expander（已完成） |
| 重新解析 `<Defs>` | Expander（已完成） |
| 解析 `.gxml` 语法、位置、属性值形态 | Parser（已完成） |

### 6.4 表示方式约束

`contracts` 包**不得依赖 `go/types`**。因此 Analyzer 的解析结果用字符串与枚举表达，实际 `types.Type` 保留在 Analyzer 内部：

```go
// 建议形态（待细化）
type ResolvedPath struct {
    Steps  []PathStep      // 逐段
    Type   string          // 最终类型的可打印形式，如 "string" / "demo.Status"
}

type PathStep struct {
    Name     string
    Type     string
    Exported bool
}
```

这样可以满足 Codegen 的需要，同时不把 `go/types` 泄漏进 `contracts`。

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
| `GXML-EVENT-004` | `CodeMethodArity` | 事件方法参数数量不匹配 | 实参数量 ≠ 形参数量 |
| `GXML-EVENT-006` | `CodeMethodArgType` | 事件方法参数类型不匹配 | 某个实参类型与形参不兼容 |
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

## 8. 冻结前的待评审项

| 编号 | 待确认 | 影响 |
|---|---|---|
| **Q1** | `ViewModel` 是否允许第三方模块类型 | 决定 `go/packages` 的加载范围与版本解析 |
| **Q2** | `ViewModel` 是否支持完整导入路径形式 | 决定语法与包定位实现 |
| **Q3** | `BIND-002` 与 `BIND-003` 是否合并 | 决定新增码数量（10 或 9） |
| **Q4** | 事件方法是否允许有返回值 | 影响签名校验规则与 Codegen 生成 |
| **Q5** | `Status` 三态是否需要更细粒度（如按节点） | 影响 IDE 集成方式 |
| **Q6** | 是否允许 `{!Path}` 取反作用于非布尔类型 | 决定 `BIND-004` 的触发范围 |

## 9. 冻结前必须完成的契约变更

以下三项是**实现前置条件**，需单独提交：

| 变更 | 文件 | 说明 |
|---|---|---|
| 1. `Result` 增加 `Schemas` | `internal/compiler/expander/expander.go` | 导出 `ComponentSchema` 与只读查询接口；合并 `builtinSchemas` |
| 2. `DocumentIR` 记录 `ViewModel` 位置 | `contracts/ir_schema.go` | 现在只有 `ViewModel string`，报 `ROOT-002` 时无法指向属性。建议改为 `ViewModelDecl{Value string; Span SourceSpan}` |
| 3. 诊断码定稿 | `docs/03-架构/诊断码目录.md` + 各包常量 | 按 §7 定稿后再实现，避免实现中反复改号 |

## 10. 版本演进边界

- 引入表达式求值 → 推翻 ADR-0002，需新 ADR。
- 允许第三方模块类型或完整导入路径 → 修改 §4.2，需评审。
- Analyzer 输出中引入 `go/types` 类型进 `contracts` → 破坏依赖方向，需新 ADR。
- 让 Codegen 自行推断绑定类型 → 违反 §6.3 边界。
- `Status` 语义变化 → 影响 Codegen 的准入判断，需同步修改 `代码生成输出契约.md`。
