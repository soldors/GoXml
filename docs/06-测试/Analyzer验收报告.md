# Analyzer 整体验收报告

> 本文件是**验收记录**，记录实际执行结果与仍存在的缺口。
> 设计说明见 `docs/03-架构/Analyzer输入输出契约.md`。
>
> 验收日期：2026-10-18 · 对应提交：Analyzer 第 4 步之后

## 1. 验收范围

从局部功能实现切换到 Codegen 之前的质量关口。**不重写功能**，集中核查：

1. 11 个诊断码的定义、发射点、测试覆盖与状态影响
2. `Status` 传播的不变量与多错优先级
3. 结构 IR / 语义事实 / 事件映射的完整性
4. 夹具一致性、待启用理由、限制测试真实性

## 2. 结论

| 项目 | 结果 |
|---|---|
| 诊断码发射点 | ✅ 11/11 有生产发射点，无死常量 |
| 诊断码测试覆盖 | ✅ 11/11 有测试引用 |
| 状态不变量 | ✅ 10 条全部通过（含 1 条修复后通过） |
| IR 只读性 | ✅ `Analyze` 不修改输入 `DocumentIR` |
| 失败隔离 | ✅ 失败项不进 `Facts`，独立成功项保留 |
| 凭证门禁 | ✅ `Partial`/`Blocked` 均无法取得 `VerifiedDoc` |
| 夹具 | ✅ 16 个，15 启用，1 个有明确理由待启用 |
| 诊断码目录一致性 | ✅ 门禁通过（含新增 AST 校验） |

```text
gofmt / build / vet / test    PASS
analyzer                    141 测试 · 92.1% 覆盖
全仓测试                     470 个
```

## 3. 审计发现的问题

本次审计发现 **4 个问题**，其中 2 个是真实缺陷、2 个是门禁失效。

### 3.1 真实缺陷：`<For>` 不可迭代时级联报 `BIND-001`

**现象**：`<For Each="ch" In="{Title}">`（`Title` 是 string）会同时报
`GXML-SCOPE-004` 和 `GXML-BIND-001`（`ch` 无法解析）。

**根因**：无法建立循环作用域时，仍推入了一个空作用域并继续遍历循环体，
导致循环变量被当成 ViewModel 字段查找。

**修复**：无法建立作用域时**跳过整个子树**，只报 `SCOPE-004`。
一条根因只有一条诊断，不制造「看起来有两个问题」的假象。

### 3.2 真实缺陷：`EventArg.Literal` 违反自身契约

已在第 4 步修复并记录：注释称「保留原始字面量文本」，实际剥掉了字符串引号，
导致 `'42'` 与 `42` 无法区分。

### 3.3 门禁失效：诊断码清单声称 Analyzer 未实现

**现象**：`diagdoc` 的 `frozenCodes` 仍列着 Analyzer 的 11 个码，注释写
「代码中没有常量、也不会发射」——但它们全部已实现并发射。

**根因**：旧测试只校验 `implementedCodes` 与 `frozenCodes` 两个**手写清单**
互不重叠，从不校验真实代码。门禁在自证。

**修复**：
1. 11 个码移入 `implementedCodes`，`frozenCodes` 清空
2. 新增 `emission_test.go`，用 `go/ast` 解析源码，验证：
   - 每个 `Code*` 常量都有发射点（不是死常量）
   - 每个常量都登记在 `implementedCodes`
   - 登记的符号都真实存在
   - `frozenCodes` 里的码确实没有常量
3. 反向验证：临时加入死常量 `CodeDeadProbe`，门禁确实失败；移除后恢复通过

### 3.4 门禁失效：目录中存在过期表述

| 位置 | 过期内容 | 修正 |
|---|---|---|
| §1.1 前缀表 | `GXML-BIND`「当前无激活码」「Analyzer（未实现）」 | 5 个激活码 |
| §1.3 | 「当前定稿态的 10 个码全部属于 Analyzer」 | 定稿态已清空 |
| §1.4 | Analyzer「需要 go/types，尚未实现」 | 已实现 |
| §2.4 | `GXML-EVENT-007` 列入空置 | 已启用 |

## 4. Status 不变量清单

| # | 不变量 | 测试 |
|---|---|---|
| 1 | `OK` ⇔ 无诊断 ⇔ 可取得凭证 | `TestStatusInvariantOK` |
| 2 | 有诊断时绝不报 `OK` | `TestStatusInvariantNoOKWithDiagnostics`（遍历所有失败夹具） |
| 3 | `Blocked` 优先于所有局部错误，只报一条 | `TestBlockedTakesPriorityOverLocalErrors` |
| 4 | `Partial` 保留独立成功项 | `TestPartialKeepsIndependentSuccesses` |
| 5 | 跨类诊断不级联 | `TestNoCascadeAcrossDiagnosticKinds` |
| 6 | `Analyze` 不修改输入 `DocumentIR` | `TestAnalyzeDoesNotMutateDocument` |
| 7 | 同一位置同一码只报一次 | `TestDiagnosticsDeduplicated` |
| 8 | 诊断带源码位置与文件名 | `TestDiagnosticSpanPointsAtSource` |
| 9 | 签发凭证后 `Facts` 冻结（幂等） | `TestVerifiedFreezesFactsThroughPublicAPI` |
| 10 | 事实中的 `TypeInfo` 与真实类型一致 | `TestFactsTypeInfoConsistent` |
| 11 | 失败节点无半成品事实 | `TestFailedNodesLeaveNoPartialFacts` |

## 5. 仍未验证的部分

**这一节是报告的重点：以下项目没有通过验证，不得视为已完成。**

| 项目 | 状态 | 原因 |
|---|---|---|
| ~~**编排层门禁**~~ | ✅ **已验证**（2026-10-19，`d5c083b`） | 原状态为「❌ 未验证」（基准 `9381542`）：Codegen 与编排层都不存在。`d5c083b` 实现 `internal/compiler/pipeline` 后，由 `TestGatePartialDoesNotCallCodegen` / `TestGateBlockedDoesNotCallCodegen` 断言 Codegen 调用计数为 0，`TestRunNeverWritesFiles` 断言无文件产生。证据见 `TASK-ORCH-001` §6.1 |
| **`<Def>` 调用点的事件签名** | ❌ 未校验 | v1 不传播 Def 事件（规范 §7.2.3），展开后事件消失，物理上无法校验。由 `TestDefEventNotSignatureChecked` 锁定 |
| **端到端生成** | ❌ 不存在 | 无 `codegen` 包，无 CLI |
| **`color`/`insets` 载荷** | ⚠️ 仅单元测试 | 没有内置事件使用这两类载荷，无法通过 `.gxml` 走到该分支 |
| **macOS / Linux** | ❌ 未验证 | 仅在 Windows amd64 上运行过 |

## 6. 对 Codegen 的输入约束

Codegen 可以依赖以下**已验证**的事实：

1. `VerifiedDoc` 只在 `StatusOK`、无诊断、`Doc` 与 `Facts` 均非 nil 时产生
2. `Facts` 中只包含**路径与类型均已验证**的属性和事件
3. `ResolvedPath.Type` 的 `Kind`/`Named`/`Stringer` 反映真实 Go 类型
4. `ResolvedEvent.Mapping` 长度恒等于 `Method.Params` 长度，逐项标注来源
5. 静态字面量属性不在 `Facts` 中（无需类型解析）
6. 布局属性（`Padding`/`Gap`/`Align` 等）不在 `Facts` 中，它们是 `NodeIR` 的字段

**不得依赖**：

- `TypeInfo.Display` 的格式（仅用于诊断，随时可能变化）
- `Facts` 中存在某个节点的条目（未校验的节点不会有）
- `StatusOK` 意味着整条管线安全——**编排层门禁已于 `d5c083b` 验证**，
  但它只证明「失败不产生产物」，不证明生成结果正确

> **状态基准规则**：本节的「未验证」结论均须注明基准提交。结论发生变化时
> 更新当前状态并保留原基准，不得让历史结论冒充当前事实；也不得仅凭
> 「代码已实现」就改判为已验证——必须有对应验收项的实测证据。
