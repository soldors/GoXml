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

- ~~`goui.App` 等运行入口**不存在**，从 `.gxml` 到真实窗口的链路尚未打通~~
  → 已由 `TASK-DEMO-001`（`pkg/runtime/app`，提交 `eb8a7b2`）补齐：
  `examples/demo` 现已打通 `.gxml → 窗口` 的端到端链路。
  该遗留在本卡落地时（00:59）成立，属当时的正确记录，保留删除线而非抹去。

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **有通过记录（限失败门禁范围），且是本组 13 项里证据最强的一项**：跨文档复核 + 8 个可定位命名测试 + 本轮现行重放；仍不等于现行完整 pipeline 验收 |
| 归类 | 有通过记录（限声明范围） |
| 实现 SHA | d5c083b (feat(pipeline): 编译编排层与失败门禁, 2026-10-19) |
| 验收 SHA / 记录 | 卡 §6.1 + 交接包 §6（事后补齐）+ Analyzer验收报告 §5（把「编排层门禁」由未验证改为已验证，点名 3 个测试） |
| 执行方是否独立 | 部分：Analyzer验收报告是另一份文档里的复核段落，但仍在同一仓库/同一批次，不是独立会话重放 |
| 命令 / rc / raw 可定位性 | go test ./internal/compiler/pipeline/ → ok 88.9%；8 个门禁测试逐条列出；交接包给出提交 SHA d5c083b；E2：有版本化报告的结论与提交号，无 stdout 原始字节。本轮 `r1c-c1-orch` / `-v` 用标准台账重放 8 个门禁测试并留 raw |
| 当时范围 | 失败不调用 Codegen、Run 不写盘、Commit 失败保旧、关闭契约 §13 第 20 条；卡 §6 明确不做 CLI/Manifest/缓存/窗口 |
| 现行适用性 | 8 个门禁测试在基线全部存在且本轮窄跑 rc=0；但 pipeline 此后经 R9–R22 大幅扩展（overlay、资产、缓存、批次检查），88.9% 是历史覆盖值 |
| 仍缺哪一层资格 | 历史 raw、独立会话复核、现行完整 pipeline 门禁矩阵 |
| 下一动作 | 保持「关闭契约 §13 第 20 条」这一结论；后续 pipeline 能力按各自 R 轮证据另计 |

