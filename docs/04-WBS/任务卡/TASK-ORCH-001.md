# TASK-ORCH-001：编译编排层与失败门禁

## 元信息
- 状态：**Done**（2026-10-19）| 优先级：P0 | 实际：约 2h
- 依赖：TASK-PARSER-001、TASK-EXPANDER-001、TASK-ANALYZER-001、TASK-CODEGEN-001（最小切片）
- 契约：`docs/03-架构/编译编排契约.md`（编排行为与产物提交）
- 关闭：`代码生成输出契约.md` §13 第 20 条

## 1. 目标

把 Parser、Expander、Analyzer、Codegen 串成一条管线，并保证：

- 任一阶段失败即中止，不进入后续阶段；
- 分析未通过时**不调用** Codegen；
- 生成成功后才产出可提交的 `Artifact`；
- 失败时不产生任何文件。

这一层是**控制流**的归属。Codegen 的类型入口挡住「把未验证 IR 传进来」，
编排层挡住「分析失败了还要往下走」——两者不可互相替代（代码生成输出契约 §2.2）。

## 2. 为什么现在做

Codegen 最小切片已完成：静态页面能生成，临时模块 `go build` 通过。但它只能被单元测试调用，
没有真实调用链。`代码生成输出契约.md` §13 第 20 条从冻结起就一直标着「不可执行」——
缺的正是这一层。

在扩展控件之前先补控制流，避免后续每加一个控件都要重新回答「失败时会不会写文件」。

## 3. 交付物

| 交付物 | 内容 |
|---|---|
| `docs/03-架构/编译编排契约.md` | 阶段中止点、行为表、产物与提交分离、可测试性约束 |
| `internal/compiler/pipeline` | `Run`（编译）、`Commit`（原子提交）、`Artifact`、`Hooks` |
| 门禁测试 | 用 `Hooks.Generate` 计数证明「Codegen 未被调用」 |

## 4. Subtask 与验收命令

| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-ORCH-001-1 | 串联四阶段；任一阶段失败即返回 | `go test -v -run TestRun ./internal/compiler/pipeline` |
| TASK-ORCH-001-2 | `Partial`/`Blocked` 不调用 Codegen（计数为 0） | `go test -v -run TestGate ./internal/compiler/pipeline` |
| TASK-ORCH-001-3 | 原子提交：失败不留残骸、目标文件不变 | `go test -v -run TestCommit ./internal/compiler/pipeline` |

## 5. 验收要求

| # | 要求 | 证据 |
|---|---|---|
| 1 | 正常静态页面走通完整管线并产出 `Artifact` | 单元测试 |
| 2 | `StatusPartial` 输入：Codegen 调用计数 **0** | `Hooks.Generate` 计数 |
| 3 | `StatusBlocked` 输入：Codegen 调用计数 **0** | 同上 |
| 4 | 上述两种失败：目标目录无新增文件 | 运行前后目录快照比对 |
| 5 | Codegen 返回错误：`Artifact == nil`，无文件 | 注入失败的 `Hooks.Generate` |
| 6 | 提交失败：目标文件保持原样 | 只读目录或非法路径 |
| 7 | 关闭契约 §13 第 20 条 | 第 2–4 项即该条要求的证据 |

**第 5–6 项是防御性补充**：契约 §13 第 20 条只要求 `Partial`/`Blocked` 两种情况。
不要把它们说成原契约已规定的内容。

## 6. 明确不做

- 发现 `.gxml`、遍历目录、文件监视（`TASK-DEVX-001`）
- manifest 与孤儿清理（契约 §10）、增量缓存（§11）
- 创建窗口、主循环（后续 demo 任务）
- 扩展控件与布局（`TASK-CODEGEN-001` 后续）

## 6.1 交付结果

```text
$ go test ./internal/compiler/pipeline/
ok  coverage: 88.9%   （14 个测试）
```

门禁证据（契约 §13 第 20 条）：

| 测试 | 证据 |
|---|---|
| `TestGatePartialDoesNotCallCodegen` | `Hooks.Generate` 计数 0；`Artifact == nil` |
| `TestGateBlockedDoesNotCallCodegen` | 计数 0；`Artifact == nil` |
| `TestGateParserFailureStopsPipeline` | Parser 失败即中止，计数 0 |
| `TestRunNeverWritesFiles` | Run 结束后目录仍为空——Run 不写盘 |
| `TestCommitFailureLeavesTargetUntouched` | 注入校验失败后旧文件字节不变、无临时文件残留 |
| `TestCodegenErrorProducesNoArtifact` | 防御性：生成失败不产出产物 |
| `TestCodegenEmptyOutputTreatedAsFailure` | 防御性：空输出视为失败 |
| `TestOKWithDiagnosticsIsRejected` | 防御性：内部不变量违反被第二道防线拦住 |

`TestOKWithDiagnosticsIsRejected` 需要构造正常路径无法产生的状态，因此
`Hooks` 增加了 `Analyze`。这是**为可测试性付出的最小代价**，不是阶段
注册机制——契约 §5 已限定。

## 7. 遗留

- `goui.App` 等运行入口**不存在**，从 `.gxml` 到真实窗口的链路尚未打通。
  那是独立阶段，不属于本任务。
