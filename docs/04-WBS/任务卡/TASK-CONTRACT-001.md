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

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **有通过记录（限 2026-10-18 基线 IR 范围）**：IR 契约与 19 个命名测试在基线可定位且包级测试仍绿；但无独立复核、无历史 raw、无任务专属提交，不能升级为「现行版本独立验收通过」 |
| 归类 | 有通过记录（限声明范围） |
| 实现 SHA | 1ef9247 (chore: 建立仓库版本基线, 2026-10-18；contracts/ir_schema.go 81→441 行 + 19 测试)。**没有以本任务命名的实现提交** |
| 验收 SHA / 记录 | 无独立验收提交；dev-log 2026-10-18 §00.3 与任务卡 §3 复述同一组结果 |
| 执行方是否独立 | **未证（缺具名执行者/独立会话记录）**；上一版措辞「否：基线提交与 dev-log 同一会话自验」已撤销。依据：dev-log/交接包文字，无具名执行者或独立会话记录。本轮标准台账 `-v` 重放另列，E3 可复跑不自动等于独立历史验收。 |
| 命令 / rc / raw 可定位性 | go build ./... exit 0；go vet ./... exit 0；gofmt -l . 无输出；go test ./... PASS coverage 91.8%（卡内文本，非 raw）；E1：卡/dev-log 有命令与退出码文字，无 stdout 字节；本轮 `r1c-c1-contracts` 用标准台账重跑 ./contracts/... rc=0（未加 -v，只证明包级不失败） |
| 当时范围 | GXML 规范 §10 的 IR 契约 8 项 + 两个关键缺陷（ForKey 缺失、BindingVal 不可分段） |
| 现行适用性 | contracts 包在基线可编译且测试通过；但 R7–R22 给它加过 FontProvider/主题接缝/诊断码等，91.8% 是 2026-10-18 的历史覆盖率，不能当现行值 |
| 仍缺哪一层资格 | 独立复核 / 历史 raw / 任务专属实现提交 |
| 下一动作 | 需要独立资格时，按现行 contracts 测试集另跑一次 -v 并留 raw；不要回填 2026-10-18 的覆盖率 |

## 2026-11-06 R23-R1 返修 R1 补正（覆盖上一版补记中被撤销的结论）

> 本次返修关闭协调者 F-C1 / F-C2 / F-C3。完整逐项字段见 [13 项证据资格复核（R1 返修补正版）](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md) 与交接包 `repair-r1/raw/closure-matrix.json`。

| 字段 | R1 返修后结论 |
|---|---|
| 单一状态（每项只计一次） | A-有可定位通过记录（限声明范围） |
| 证据层 | E1（历史文字）+ 本轮 E3 可复跑（包级） |
| 历史原命令是否有效 | `./contracts` 存在且 47 个真实顶层入口；历史 91.8% 覆盖率无 raw，不追认 |
| 本轮另选命令 | `go test -count=1 -timeout 10m -v ./contracts`（r1r-replay-contracts） |
| 真实用例集合 | `r1r-replay-contracts`：47 个顶层 PASS / 0 个子测试 PASS / 0 SKIP / 0 FAIL |
| 能证明的范围 | 现行基线 contracts 包 47 个顶层用例全部 PASS / 0 SKIP / 0 FAIL |
| 未覆盖的范围 | 2026-10-18 的历史覆盖率、独立历史验收、现行全仓功能验收 |
| 历史执行方独立性 | 未证（缺具名执行者/独立会话记录） |
| 本轮 E3 重放 | r1r-replay-contracts 是本轮 WorkBuddy 标准台账重放，不等于独立历史验收 |

