# Generate CLI 验收报告

## 1. 报告信息

| 项目 | 内容 |
|---|---|
| 报告名称 | Generate CLI 验收（TASK-GENERATE-CLI-001 收口） |
| 关联任务 | [TASK-GENERATE-CLI-001](../04-WBS/任务卡/TASK-GENERATE-CLI-001.md)（1-1 至 1-7） |
| 关联需求/用例 | REQ-002：AC-002-01、AC-002-02、AC-002-03、AC-002-04 |
| 提交 SHA | 见 §5（本报告随收口提交更新） |
| 执行日期（北京时间） | 2026-10-20 |
| 执行人 | 本会话（与任务卡执行同一会话） |
| 操作系统/架构 | Windows 11 / amd64 |
| Go/Gio 版本 | Go 1.26.1（go.mod 声明 1.26）/ Gio v0.10.3 |
| `CGO_ENABLED` | 0（`GOPROXY=off`） |

## 2. 验收范围

覆盖 `goxml generate` 的参数解析、退出码、单文件与目录生成、`--out-dir`
映射、两阶段事务回滚、Manifest 登记与 `--clean` 清理。

**不覆盖**（按任务卡 §3 排除）：

- Watch / Preview / Live Reload（归 `TASK-DEVX-001/002`）；
- `--check` 的 L3/L4 层（`go/types` / `go build`）——当前承诺到 L1+L2
  （G6 决策，见 [短检查记录 §6](../04-WBS/任务卡/GENERATE-CLI-001-短检查记录.md)）；
- 多文件批次的部分回滚策略细化（G4 已冻结为全或无，两阶段实现）；
- Grid、Def/Slot 在 Codegen 侧的完整覆盖（REQ-002 其余部分）。

依据契约：[代码生成输出契约.md](../03-架构/代码生成输出契约.md) §3/§6/§9/§10/§12。

## 3. 命令与结果

### 3.1 AC-002-01 基础生成

| 编号 | 命令 | 退出码 | 结果 | 证据 |
|---|---|---:|---|---|
| 1 | `go build -o goxml.exe ./cmd/goxml/` | 0 | 通过 | 二进制构建成功 |
| 2 | `goxml.exe generate ui/home.gxml`（仓库内工程） | 0 | 通过 | stdout：`已生成 …\ui\home_gen.go` |
| 3 | 检查 `ui/home_gen.go` | — | 通过 | 文件存在；`codegen.HasProtectedHeader` 三行保护头（G1 格式） |

### 3.2 AC-002-02 重复生成确定性

| 编号 | 命令 | 退出码 | 结果 | 证据 |
|---|---|---:|---|---|
| 4 | 连续两次 `goxml.exe generate ui/home.gxml` | 0 / 0 | 通过 | `Get-FileHash -Algorithm SHA256` 两次一致 |

自动化等价证据：`TestGenerationDeterminism`（两次独立目录 SHA-256 一致）、
`TestGenerationDeterministicAcrossCreationOrder`（相反创建顺序哈希一致）、
`TestGenerateNestedDirsDeterministic`（跨目录两产物逐字节一致）。

### 3.3 AC-002-03 失败回滚

| 编号 | 命令 | 退出码 | 结果 | 证据 |
|---|---|---:|---|---|
| 5 | 构造 `broken.gxml`（`<Goxml Version="1"><Unclosed>`）后 `goxml.exe generate broken.gxml` | 2 | 通过 | stderr 诊断：`broken.gxml: GXML-ROOT-001: unexpected element <Unclosed> …`；`生成失败（…）——批次全部放弃，磁盘无任何变更` |
| 6 | 校验 `home_gen.go` | — | 通过 | SHA-256 与失败前一致；mtime 未变（两阶段事务：阶段一失败不触碰磁盘） |

自动化等价证据：`TestGenerationSyntaxFailureDiag`（诊断 + 无半成品）、
`TestExistingFileUntouchedOnFailure`（内容与 mtime 原样）、
`TestEndToEndBatchAllOrNothing`（批次内一败俱废）。

> 排查记录：首次人工取证时 PowerShell 管道（`ForEach-Object`）覆盖了
> `$LASTEXITCODE`，误判为 exit=0；直接执行实测 **exit=2**，
> 与 `TestGenerateSyntaxFailureDiag` 一致。已核实为取证方法问题，
> 非实现缺陷。

### 3.4 AC-002-04 `--clean`

| 编号 | 命令 | 退出码 | 结果 | 证据 |
|---|---|---:|---|---|
| 7 | 生成 `home.gxml` + `second.gxml`，删除 `home.gxml` 后 `goxml.exe generate second.gxml --clean` | 0 | 通过 | stdout：`已清理 internal/…/home_gen.go`、`清理完成：删除 1，跳过 0` |
| 8 | 校验 | — | 通过 | 孤儿 `home_gen.go` 已删；`second_gen.go` 保留；Manifest 条目同步移除 |

安全锁负例（自动化）：`TestCleanSkipsUserModifiedHeader`（保护头被改 →
跳过「视为用户文件」）、`TestCleanSkipsUnregisteredFiles`（未登记永不删）、
`TestCleanCorruptedManifestFailsClosed`（损坏自锁 → 报错且零删除）。

## 4. 单元测试汇总

| 包 | 命令 | 结果 |
|---|---|---|
| `internal/cli` | `go test ./internal/cli/` | ok（38 项，覆盖率 78.0%） |
| 全库 | `go test ./...` | ok |
| 门禁 | `gofmt` / `go build ./...` / `go vet ./...` / `diagdoc` / `docgate` | 全部通过 |

## 5. 提交与过程证据

| 提交 | 内容 |
|---|---|
| `1f7b2f2` | 短检查（范围核对 + G1–G8 缺口清单） |
| `ae333e2` | G1 保护头对齐契约 §3.1 + `HasProtectedHeader` |
| `b4d4218` | 1-1 参数解析/退出码（真实二进制实测） |
| `41d9f52` | 1-2 单文件真实管线（G2/G6/G7 决策冻结） |
| `7165a05` | 1-3 目录发现/排序/`--out-dir` |
| `501c072` | 1-4 确定性（SHA-256 判据 + 跨创建顺序） |
| `0b07a18` | 1-5 两阶段事务（G4 落地） |
| `ae9fab7` | 1-6 Manifest + `--clean`（三把安全锁） |

## 6. 已知限制（如实记录）

1. **`--check` 当前与默认行为相同**（G6：只承诺 L1+L2；L3/L4 未实现）。
   使用者不得据此推断「go 工程一定能编译」。
2. **多文件批次无部分回滚**（G4 冻结为全或无）：一个文件失败，
   整批放弃——已成功文件在本切片中不落盘（阶段一先全部生成）。
3. **阶段二 rename 失败**属环境级故障（磁盘满/权限突变），已 rename 的
   文件无法进程内还原；工具会清理未提交临时文件并如实报错。
4. **多个独立输入参数未支持**：批量形态是一个目录；多根归并未做。
5. **`--out-dir` 需要输出目录可写**；跨 module 输入批次被拒绝
   （Manifest 在项目根维护）。
6. Watch/Preview 归 `TASK-DEVX-001/002`，本卡未涉及。

## 7. 结论

AC-002-01 至 AC-002-04 **全部通过**，均有真实命令、退出码与自动化等价
测试支撑。`TASK-GENERATE-CLI-001` 的 1-1 至 1-7 全部完成，任务卡可关闭。
