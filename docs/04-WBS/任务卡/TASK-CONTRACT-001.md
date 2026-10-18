# TASK-CONTRACT-001：定义编译器 IR 与运行时核心接口契约

## 元信息
- 状态：**Done**（2026-10-18）| 优先级：P0 | 实际：约 3h
- 分支：feature/TASK-CONTRACT-001
- 依赖：无

## 1. 目标
定义编译器、生成代码与运行时共同使用的 IR、源码诊断和生命周期/主题接口，作为各模块唯一共享契约。

## 2. 完成情况

按 `GXML语言规范.md` §10「对编译器契约的要求」逐条落实：

| 规范 §10 要求 | 契约实现 | 状态 |
|---|---|---|
| `NodeKind`，不能只依赖 `Tag string` | `NodeKind` 枚举（9 种）+ `String()` + `IsStructural()` | ✅ |
| 每个节点和属性的 `SourceSpan` | `Position` / `SourceSpan`；`NodeIR.Span`、`Attr.Span`、`EventBinding.Span`、`DefProp.Span` | ✅ |
| 结构化的 `If`、`For`、`Def`、`Slot` | `IfClause` / `ForClause` / `DefClause` + `KindSlot` | ✅ |
| 绑定路径的分段表示 | `BindingVal.Segments []string` + `Path()` | ✅ |
| Def 属性的名称、类型和默认值 | `DefProp{Name, Type, Default, HasDefault, Span}` + `PropType` / `ParsePropType` | ✅ |
| 事件名称、方法名、参数列表和源码位置 | `EventBinding{Name, Method, Args, Span}` + `EventArg{Kind, Literal, Binding}` | ✅ |
| `DocumentIR.Package` 只作生成上下文 | 已加注释明确，不承载 GXML 语义 | ✅ |

### 修复的两个关键缺陷

1. **`ForKey` 完全缺失**：原 `NodeIR` 只有 `ForEach`/`ForIndex`/`ForIn`，无处存放 Key。而 `AOT状态Diff语义.md` §5 整章依赖 Key，缺失会导致循环内控件状态串位原样复现（机理见 `AOT状态Diff语义.md` §5.1）。现由 `ForClause.Key` + `ForClause.HasKey` 承载（`Key` 零值无法区分「未提供」，故必须配 `HasKey`）。
2. **`BindingVal.Path` 是不可再切分的整串**：Analyzer 每次都要重新解析。现改为 `Segments []string`，`Path()` 只负责回显。

## 3. 验收命令与结果

```text
$ go build ./...                    → exit 0
$ go vet ./...                      → exit 0
$ gofmt -l contracts                → （无输出，已格式化）
$ go test ./contracts/...           → PASS, coverage: 91.8% of statements
$ make contract-lint                → 对应上述命令组合
```

覆盖率 91.8%，超过 `测试与质量门禁规范.md` 对 `contracts/` 的 85% 门槛。19 个测试覆盖：错误格式、源码范围、EdgeInsets、绑定分段、NodeKind 分类、属性查找、ForKey 存在性、虚拟化缺省值、PropType 解析、nil 安全、诊断码前缀唯一性。

## 4. 遗留项

以下依赖运行时/平台实现，不在本任务范围：

- `FontProvider` 契约（见 `TASK-FONT-001`）。
- `Capabilities` 能力探测结构（见 `TASK-OS-001`）。
- `Dispatcher` 接口（见 `运行时线程模型.md` §4，实现于 `TASK-LIFE-001`）。
