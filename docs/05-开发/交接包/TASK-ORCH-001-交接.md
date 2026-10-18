# 交接包：TASK-ORCH-001

## 1. 任务状态

- 状态：**Done**（2026-10-19）| 优先级：P0 | 实际：约 2h
- 依赖：TASK-PARSER-001、TASK-EXPANDER-001、TASK-ANALYZER-001、TASK-CODEGEN-001（最小切片）
- 契约：`docs/03-架构/编译编排契约.md`（本任务新建）
- 关闭：`代码生成输出契约.md` §13 第 20 条

## 2. 必读依据

1. [编译编排契约.md](../../03-架构/编译编排契约.md)
2. [代码生成输出契约.md](../../03-架构/代码生成输出契约.md) §13 第 20 条
3. [TASK-ORCH-001.md](../../04-WBS/任务卡/TASK-ORCH-001.md)
4. 上游交接：[TASK-CODEGEN-001-交接.md](TASK-CODEGEN-001-交接.md)

本包是**控制流**的归属：Codegen 的类型入口挡住「把未验证 IR 传进来」，
编排层挡住「分析失败了还要往下走」——两者不可互相替代。

## 3. 代码边界

| 文件/模块 | 责任 |
|---|---|
| `internal/compiler/pipeline/pipeline.go` | `Run`（四阶段串联）、`Commit`（原子提交）、`Artifact`、`Hooks` |

核心建模：**产物与提交分开**——`Artifact` 存在只表示「允许提交」，不代表已写盘。
`Run` 不写盘；`Commit` 走「临时文件 → 复读校验 → 原子替换」，失败删临时文件、旧文件不变。

## 4. 门禁证据（关闭契约 §13 第 20 条）

| 测试 | 证据 |
|---|---|
| `TestGatePartialDoesNotCallCodegen` | `Hooks.Generate` 计数 0；`Artifact == nil` |
| `TestGateBlockedDoesNotCallCodegen` | 计数 0；`Artifact == nil` |
| `TestGateParserFailureStopsPipeline` | Parser 失败即中止 |
| `TestRunNeverWritesFiles` | Run 结束后目录仍为空 |
| `TestCommitFailureLeavesTargetUntouched` | 旧文件字节不变、无临时残留 |
| `TestCodegenErrorProducesNoArtifact` | 防御性：生成失败不产出产物 |
| `TestCodegenEmptyOutputTreatedAsFailure` | 防御性：空输出视为失败 |
| `TestOKWithDiagnosticsIsRejected` | 防御性：内部不变量违反被第二道防线拦住 |

> 后三项是**防御性补充**：契约 §13 第 20 条只要求 `Partial`/`Blocked` 两种情形。
> 不要把它们说成原契约已规定的内容。

## 5. 验收命令

```text
go test ./internal/compiler/pipeline/
go test ./internal/compiler/pipeline/ -run TestGate
go test ./internal/compiler/pipeline/ -run TestCommit
```

## 6. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | `d5c083b`（feat(pipeline): 编译编排层与失败门禁，关闭 Codegen 契约 §13 第 20 条） |
| 已完成子任务 | ORCH-001-1（串联四阶段、失败即返回）、-2（`Partial`/`Blocked` 不调用 Codegen）、-3（原子提交，失败不留残骸） |
| 未完成子任务 | 无（本任务范围内）。发现 `.gxml`、目录遍历、文件监视归 `TASK-DEVX-001`；manifest 与孤儿清理（契约 §10）、增量缓存（§11）未做 |
| 已运行命令及结果 | `go test ./internal/compiler/pipeline/` → ok，coverage **88.9%**（14 个测试）；当时点全仓 510 个测试；`gofmt`/`build`/`vet` 全绿 |
| 已知失败或风险 | ① `goui.App` 等运行入口**不存在**，从 `.gxml` 到真实窗口的链路尚未打通——这是独立阶段，不属于本任务；② `Hooks` 为可测试性引入了 `Analyze` 替换点，契约 §5 已限定「不是插件体系」，**不得扩展成通用阶段注册机制**；③ 仅 Windows 验证 |
| 下一位接手人 | 当前主力是扩展 `TASK-CODEGEN-001`（布局 → 绑定 → 事件 → `<For>`）；其后是阶段二「最小可运行窗口」，需先定运行入口包的位置与契约。另建议追平 `TASK-SEC-001`（流程偏差） |
