# TASK-GENERATE-CLI-001：实现 Generate CLI 与输出管理

## 元信息

- 状态：**Todo（批次 0 已建立任务卡；2026-10-20 短检查完成，见 [GENERATE-CLI-001-短检查记录.md](GENERATE-CLI-001-短检查记录.md)——范围与验收标准核对无变化，8 项缺口已列出，G1（保护头对齐契约 §3.1）为 1-6 的前置必修）** | 优先级：P0
- 依赖：`TASK-CODEGEN-001`、`TASK-ORCH-001`、`TASK-SEC-001`
- 规范依据：[`代码生成输出契约.md`](../../03-架构/代码生成输出契约.md)、[`安全边界与威胁模型.md`](../../03-架构/安全边界与威胁模型.md)
- 关联需求：REQ-002
- 关联验收：AC-002-01、AC-002-02、AC-002-03、AC-002-04

## 1. 目标

实现 `goxml generate` 的命令入口和文件输出管理，使冻结的代码生成输出契约具备可执行的用户入口。任务只负责 CLI 编排、输出路径、Manifest、缓存开关和清理边界；Parser、Expander、Analyzer、Codegen 的语义实现仍由各自任务负责。

## 2. 范围

- `goxml generate <path>`：发现输入、解析 Go package 上下文、调用编译管线并提交生成文件。
- `--out-dir`：校验输出目录属于当前 Go module，并保持批量输入的相对路径结构。
- `--check`：生成前执行契约规定的 package/build 检查；失败时旧产物保持不变。
- `--clean`：只清理 Manifest 登记、仍带 Goxml 保护头且位于受保护输出边界内的孤儿文件。
- `--no-cache`：绕过增量缓存但保持输出结果和 Manifest 语义一致。
- Manifest 的确定性序列化、损坏处理、原子更新和失败回滚。
- CLI 错误码、退出码、诊断输出和日志字段。

## 3. 排除

- 不实现 Watch、Preview、Live Reload 或状态 Hydration；分别归 `TASK-DEVX-001/002`。
- 不改变 GXML、Analyzer、Codegen 或运行时契约。
- 不允许用户输入改变输出根目录边界，不允许覆盖没有 Goxml 保护头的用户文件。
- 不把生成失败、Manifest 损坏或 package 检查失败降级为成功。

## 4. 子任务与验收命令

| 子任务 ID | 内容 | 验收命令/证据 |
|---|---|---|
| TASK-GENERATE-CLI-001-1 | 建立 `cmd/goxml` 与 `generate` 参数解析、退出码和诊断输出 | ✅ 完成（2026-10-20）：`internal/cli` 20 项测试 + 真实二进制退出码实测；`go test ./internal/cli/` |
| TASK-GENERATE-CLI-001-2 | 接通 Parser → Expander → Analyzer → Codegen → Commit | ✅ 完成（2026-10-20）：`internal/cli` 单文件真实路径，8 场景验收（含确定性、失败不提交、范围拒绝）；`go test ./internal/cli/` |
| TASK-GENERATE-CLI-001-3 | 实现输出目录、包名、文件名和保护头校验 | ✅ 完成（2026-10-20）：目录递归发现 + rel 排序 + 每文件 SourceDir（G2）+ --out-dir 映射与冲突检测；7 项新测试；明确「无批次回滚」（G4 留 1-5）；`go test ./internal/cli/` |
| TASK-GENERATE-CLI-001-4 | 实现生成确定性和重复生成 | ✅ 完成（2026-10-20）：SHA-256 判据升级 + 跨创建顺序证明 + 保护头 Source 稳定性；`go test -run TestGenerationDeterminism ./internal/cli/` |
| TASK-GENERATE-CLI-001-5 | 实现失败回滚，旧文件内容、权限和修改时间保持不变 | ✅ 完成（2026-10-20）：G4 两阶段事务（stageBatch/commitBatch/cleanup），6 项验收含 mtime 不变与端到端全或无；`go test ./internal/cli/` |
| TASK-GENERATE-CLI-001-6 | 实现 Manifest 原子更新、损坏保护和 `--clean` 边界 | ✅ 完成（2026-10-20）：三把安全锁（双重所有权/原子保存/损坏自锁）；6 项验收含用户接管跳过与 fail-safe 零删除；`go test ./internal/cli/` |
| TASK-GENERATE-CLI-001-7 | 验证 `--no-cache` 与缓存模式输出一致 | `go test -v -run TestGenerateNoCacheEquivalent ./cmd/goxml/...` |

## 5. 退出条件

1. AC-002-01 至 AC-002-04 均有真实 CLI 或命令包测试，命令、提交、环境和退出码写入里程碑验收报告。
2. 同一输入连续生成的源码、SHA-256、import 别名、符号名和 Manifest 字节稳定。
3. Parser、Analyzer、Codegen、格式化、package/build 检查任一失败时，旧生成文件、权限、修改时间和 Manifest 保持不变。
4. `--clean` 不能删除用户文件、越界文件或无保护头文件；Manifest 损坏时停止清理并返回失败。
5. 生成输出和 Manifest 的临时文件在失败后清理干净；成功提交使用原子替换。
6. 任务范围内的输入路径、输出路径和 import 来源均通过 `TASK-SEC-001` 的安全准入条件。

## 6. 证据与交接

- 里程碑报告写入 `docs/06-测试/`，使用 [`测试报告格式.md`](../../06-测试/测试报告格式.md)。
- 生成失败、Manifest 损坏、路径越界和用户文件保护必须各有负例证据。
- 完成后同步本任务卡、`docs/02-需求/验收用例.md`、`docs/05-开发/dev-log/`、发布单和运维手册。
- 在本任务完成前，AC-002-01 至 AC-002-04 保持“未执行”，不能以 `internal/compiler/pipeline` 的单元测试替代 CLI 验收。
