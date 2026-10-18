# 交接包：TASK-PARSER-001

## 1. 任务状态

- 状态：**Done**（2026-10-18）
- 优先级：P0
- 依赖：TASK-CONTRACT-001
- 目标：实现带行列号的 GXML 词法/语法分析和 If/For 控制流解析。
- 当前前提：设计契约已冻结；**代码实现与测试均已完成**，本文件为事后补齐的交接记录。

## 2. 必读依据

开始编码前必须阅读：

1. [GXML 语言规范.md](../../03-架构/GXML语言规范.md)
2. [架构说明.md](../../03-架构/架构说明.md)
3. [代码生成输出契约.md](../../03-架构/代码生成输出契约.md)
4. [测试与质量门禁规范.md](../../06-测试/测试与质量门禁规范.md)
5. [验收用例.md](../../02-需求/验收用例.md) 中 AC-001-01 至 AC-001-04
6. `contracts/` 中的 DocumentIR、NodeIR、Binding 和诊断契约

Parser 只能实现 GXML v1 已冻结语义，不能在 Parser 中偷偷加入运行时表达式、动态组件或 CSS 解析。

## 3. 代码边界

| 文件/模块 | 责任 |
|---|---|
| `internal/compiler/parser/source.go` | 字节偏移到行列的映射、列号按 rune 计数 |
| `internal/compiler/parser/document.go` | 根节点、标签、属性和文档结构解析 |
| `internal/compiler/parser/element.go` | 元素、属性值形态与起始标签原文重扫（获取每个属性的 `SourceSpan`） |
| `internal/compiler/parser/attrs.go` | 强类型属性解析（EdgeInsets / Color / Binding / 尺寸） |
| `internal/compiler/parser/control_flow.go` | If/Then/Else、For、变量作用域和 Key 规则 |
| `internal/compiler/parser/layout.go` | 布局属性（`Align`/`Justify`/`Gap`/`Width`/`Height`/`Columns`） |
| `internal/compiler/parser/def_events.go` | `<Def>` 的 `Props` / `Events` 声明解析 |
| `internal/compiler/parser/diag.go` | 统一错误码、文件/行/列和源码范围 |
| `contracts/` | 共享 IR 和诊断类型；禁止在 parser 内重复定义公共契约 |

> 早期版本的本表列着 `scanner.go` 与 `diagnostics.go` 两个文件名，实际实现分别为
> `source.go` 与 `diag.go`；属性与布局解析后来又拆出 `attrs.go` / `layout.go`。已按真实文件更正。

Parser 输出必须是结构化 IR，不能直接生成 Go 源码，也不能依赖 Runtime。

## 4. 实现顺序

1. 先补契约层 fixture 和最小合法文档。
2. 实现 Token 和源码位置追踪。
3. 实现根节点、标签大小写、未知标签、重复属性和 XML 错误诊断。
4. 实现静态属性值与绑定路径的语法识别；表达式合法性留给 Analyzer。
5. 实现 If/For 结构和作用域校验。
6. 实现 Def/Slot 的语法节点，不在 Parser 阶段展开宏。
7. 为每个错误路径补快照或结构化诊断测试。

## 5. 验收命令

```text
make contract-lint
go test -v -run TestScannerPosition ./internal/compiler/parser/...
go test -v -run TestControlFlowNodes ./internal/compiler/parser/...
go test -v -run TestParserDiagnostics ./internal/compiler/parser/...
go test -v ./internal/compiler/parser/...
```

任务完成条件：AC-001-01 至 AC-001-04 全部通过，诊断快照稳定，且输出 IR 能被 Analyzer 消费。

## 6. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | **无独立特性提交**。Parser 的实现早于版本控制：仓库首个提交 `1ef9247`（chore: 建立仓库版本基线）即已包含 14 个 `parser/` 文件。因此该任务无法给出「实现它的那个提交」，只能追溯到纳入版本控制的时点。后续诊断码相关的修正见 `a9db4ed`、`e2d96d5` |
| 已完成子任务 | §4 的 1–7 全部完成：契约 fixture 与最小合法文档 → 行列位置追踪 → 根节点/标签/未知标签/重复属性/XML 错误诊断 → 静态值与绑定路径识别 → If/For 结构与作用域 → Def/Slot 语法节点（不在本阶段展开）→ 错误路径测试 |
| 未完成子任务 | 无。表达式合法性按契约留给 Analyzer；宏展开留给 Expander |
| 已运行命令及结果 | `go test ./internal/compiler/parser/` → ok，coverage **87.7%**（93 个顶层测试）；`go build ./...`、`go vet ./...` 通过；全仓 `go test ./...` 通过（含本包） |
| 已知失败或风险 | ① 整体覆盖率 87.7%，而 `测试与质量门禁规范.md` 要求「核心解析路径 ≥ 90%」——**该口径是按核心文件还是整包统计尚未逐文件核对**，若按整包则达标（≥85%），若按核心路径则需补测；② 无独立 SHA，历史追溯只能到基线提交；③ 仅在 Windows amd64 上验证 |
| 下一位接手人 | 后续 Expander / Analyzer / Codegen 均已完成或进行中。**当前断点是 `TASK-CODEGEN-001`**，交接见 `TASK-CODEGEN-001-交接.md` |

如果工作区不是 Git checkout，不执行分支创建命令；应由宿主工作流提供等价的隔离分支或提交标识。
