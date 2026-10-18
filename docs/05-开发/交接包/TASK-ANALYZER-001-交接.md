# 交接包：TASK-ANALYZER-001

> **事后补齐**：本任务已在单次会话内闭环（有 Git 提交与验收报告）。
> 此文件按 `AGENT.md` §4 第 3 条「可以写」的情形补写，用于沉淀关键决策与易被误解的边界，
> 不作为跨会话续接的必需品。

## 1. 任务状态

- 状态：**Done**（2026-10-18，整体验收通过）| 优先级：P0 | 实际：约 10h
- 依赖：TASK-EXPANDER-001
- 目标：用 Go 类型信息在编译期校验绑定路径、类型兼容、事件签名与 `<For>` 可迭代性。
- 契约：`docs/03-架构/Analyzer输入输出契约.md`（已全部冻结）
- 验收报告：[Analyzer验收报告.md](../../06-测试/Analyzer验收报告.md)（**§5「仍未验证的部分」必读**）

## 2. 必读依据

1. [Analyzer输入输出契约.md](../../03-架构/Analyzer输入输出契约.md)（唯一语义依据）
2. [诊断码目录.md](../../03-架构/诊断码目录.md)（11 个码的语义与状态）
3. [TASK-ANALYZER-001.md](../../04-WBS/任务卡/TASK-ANALYZER-001.md)
4. 上游交接：[TASK-EXPANDER-001-交接.md](TASK-EXPANDER-001-交接.md)

本阶段是第一个使用 `go/packages` / `go/types` 的层。

## 3. 代码边界

| 文件/模块 | 责任 |
|---|---|
| `internal/compiler/analyzer/load.go` | `ParseTypeExpr`、`TypeEnv`（包缓存）、`ResolveViewModel` |
| `internal/compiler/analyzer/analyze.go` | `Analyze` 总入口、树遍历、`<For>` 作用域栈、路径解析 |
| `internal/compiler/analyzer/compat.go` | 类型兼容三层判定、`{!Path}` 处理 |
| `internal/compiler/analyzer/events.go` | 事件载荷 + 实参拼接 → 方法签名比对 |
| `internal/compiler/analyzer/ir.go` | 语义 IR：`Result`/`Facts`/`Resolved*`/`TypeInfo`/`VerifiedDoc` |
| `internal/compiler/analyzer/codes.go` | 诊断码常量 |
| `internal/compiler/analyzer/testdata/fixture/vm.go` 等 | 16 个夹具（15 启用，1 个因 v1 限制待启用） |

## 4. 不可违反的约束

| 约束 | 说明 |
|---|---|
| 不修改结构 IR | 语义事实存 `Facts` 旁表 |
| `contracts` 不依赖 `go/types` | 解析结果用字符串与枚举表达 |
| 只记录已验证的事实 | 失败项只产诊断，不进 `Facts` |
| 不生成 Go 代码 | 表达式生成归 Codegen |
| 不允许伪造成功 | `ViewModel` 无法解析时 `StatusBlocked`，不得跳过检查当通过 |

## 5. 验收命令

```text
go test ./internal/compiler/analyzer/
go test ./internal/compiler/analyzer/ -run TestBinding
go test ./internal/compiler/analyzer/ -run TestEvent
go test ./internal/compiler/diagdoc/          # 诊断码目录一致性（AST 门禁）
```

## 6. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | 分五步落地：`1cd7e56`（第 1 步 类型加载）、`bdd299b`（第 2 步 绑定路径）、`b077c81`（第 3 步 类型兼容）、`259033b`（第 4 步 事件签名）、`9381542`（整体验收与门禁修正）；第 5 步 `<For>` 随第 2 步一并实现。前置探针 `e48c5c1`、契约 `47a72a4`/`610fec8` |
| 已完成子任务 | 五步全部完成（见任务卡 §3）；`Analyze` 总入口与 `Status` 三态判定；11 个诊断码全部有生产发射点 |
| 未完成子任务 | 无（本任务范围内）。编排层门禁原属本卡遗留，已由 `TASK-ORCH-001`（`d5c083b`）关闭 |
| 已运行命令及结果 | `go test ./internal/compiler/analyzer/` → ok，coverage **92.1%**（141 个测试）；全仓 `go test ./...`（470 个）；`go test ./internal/compiler/diagdoc/` 通过 |
| 已知失败或风险 | ① **未验证项见验收报告 §5**：`<Def>` 调用点事件签名 v1 物理上无法校验（事件不传播）、`color`/`insets` 载荷仅单元测试覆盖、仅 Windows amd64 验证；② 索引访问、Map 取值、方法调用等表达式形式未设计（语法未冻结）；③ 审计曾发现手写清单型门禁「自证」问题，已改为 AST 驱动——**新增诊断码必须同时更新目录与 `diagdoc`**，不要只改常量 |
| 下一位接手人 | Codegen 进行中，见 `TASK-CODEGEN-001-交接.md`。Codegen 可依赖的已验证事实见验收报告 §6 |
