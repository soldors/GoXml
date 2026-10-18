# 交接包：TASK-PARSER-001

## 1. 任务状态

- 状态：Todo
- 优先级：P0
- 依赖：TASK-CONTRACT-001
- 目标：实现带行列号的 GXML 词法/语法分析和 If/For 控制流解析。
- 当前前提：设计契约已冻结，代码实现和测试尚未开始。

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
| `internal/compiler/parser/scanner.go` | Token 扫描、源码位置、XML 转义和注释处理 |
| `internal/compiler/parser/document.go` | 根节点、标签、属性和文档结构解析 |
| `internal/compiler/parser/control_flow.go` | If/Then/Else、For、变量作用域和 Key 规则 |
| `internal/compiler/parser/diagnostics.go` | 统一错误码、文件/行/列和源码范围 |
| `contracts/` | 共享 IR 和诊断类型；禁止在 parser 内重复定义公共契约 |

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
| 当前提交 SHA | |
| 已完成子任务 | |
| 未完成子任务 | |
| 已运行命令及结果 | |
| 已知失败或风险 | |
| 下一位接手人 | |

如果工作区不是 Git checkout，不执行分支创建命令；应由宿主工作流提供等价的隔离分支或提交标识。
