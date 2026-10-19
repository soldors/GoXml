# TASK-ANALYZER-001：实现语义校验（绑定路径、事件签名、For 作用域）

## 元信息
- 状态：**Done**（2026-10-18，整体验收通过）| 优先级：P0 | 实际：约 10h
- 依赖：TASK-EXPANDER-001
- 契约：`docs/03-架构/Analyzer输入输出契约.md`（已全部冻结）

## 1. 目标

在编译期用 Go 类型信息校验 Expander 输出的语义，产出**已验证的语义 IR** 供 Codegen 消费。

它是第一个需要 `go/packages` 的阶段——绑定路径是否存在、事件方法签名是否匹配、循环值是否可迭代，都只能靠真实类型信息判断。

## 2. 执行顺序（与常见直觉不同）

原计划「先做静态属性校验，再做 Go 类型解析」。实际调整为**先做类型加载**，原因：

- 未知属性名已由 Expander 检出（`GXML-ATTR-004`），Analyzer 不需要重复检查；
- 绑定路径、类型匹配、事件签名、`For` 可迭代性**全部**依赖类型信息；
- 没有可靠的类型来源时，后续每一步都建立在假定之上。

## 3. 五步与进度

| 步骤 | 内容 | 诊断码 | 状态 |
|---|---|---|---|
| 1 | Go 类型加载 + `ViewModel` 解析 | `ROOT-002` | ✅ 完成 |
| 2 | 绑定路径解析（首段/中间段/未导出） | `BIND-001`/`002`/`003` | ✅ 完成 |
| 3 | 类型匹配（含 `{!Path}` 只作用于 bool） | `BIND-004` | ✅ 完成 |
| 4 | 事件签名（载荷 + 实参拼接） | `EVENT-003`/`004`/`006`/`007` | ✅ 完成 |
| 5 | `<For>` 可迭代性与循环变量作用域 | `SCOPE-004` | ✅ 完成（随步骤 2 一并实现） |
| — | **整体验收**（诊断码 / Status / IR / 夹具） | — | ✅ 通过，见 `docs/06-测试/Analyzer验收报告.md` |
| — | `Analyze` 总入口与 `Status` 判定 | — | ✅ 完成 |
| — | 编排层门禁（仅 `StatusOK` 且无诊断时进入 Codegen） | — | ✅ 已由 `TASK-ORCH-001` 验收 |

### 第 4 步已完成的内容

- `events.go`：解析 Schema 载荷 → 调用点实参 → 方法签名 → 拼接比对 → 构造 `ResolvedEvent`
- 严格按「载荷在前、实参在后」拼接，`Mapping` 与 `Method.Params` 一一对应
- 四类诊断：`EVENT-003` 方法不存在、`EVENT-004` 数量、`EVENT-006` 类型、`EVENT-007` 有返回值
- `EVENT-007` 是本次**新分配**的码：契约 §8.2（Q4）早已规定「方法不得有返回值」，
  但此前没有编号。已同步诊断码目录与门禁
- **载荷与实参用不同判定**：载荷无具体 Go 类型，按类别匹配（`PropInt` 接受 int64）；
  实参有确定类型，按 `types.AssignableTo` 严格判定
- **修复一处 IR 契约违反**：`EventArg.Literal` 注释称「保留原始字面量文本」，
  但 Expander 剥掉了字符串引号，导致 `'42'` 与 `42` 无法区分。已改为保留引号
- 夹具启用 6 个事件类（共 15 个已启用，仅 `valid-event-combined` 因 v1 限制待启用）

### 第 3 步已完成的内容

- **契约 §11「类型兼容契约」已冻结**：拆成三层（类型兼容 / 特殊表达式约束 / 值转换），
  矩阵覆盖 6 种 `PropType` × 10 种 `Kind`
- `compat.go`：`checkCompat` 三态（允许/拒绝/Schema 缺类型），
  `negatable` 处理 `{!Path}`，`propTypeName` 供诊断消息
- `ValueKind` 新增 `ValueUint`：无符号整数必须与有符号区分，
  否则 `int` 属性会误接受 `uint`
- 前置修复：**内置 Schema 的属性此前未声明 `Type`**（全为 `PropUnknown`），
  已补齐 Text/Button/Input/Image 的全部属性类型
- `color` / `insets` 按**精确类型身份**判定（`types.Identical`），
  不接受「结构相似」的结构体
- 夹具启用 `type-mismatch`、`negate-non-bool`

### 第 2 步已完成的内容

- `analyze.go`：`Analyze` 总入口、树遍历、`<For>` 作用域栈、路径解析
- 支持：字段、零参单返回值**方法**（规范 §4 允许「方法结果字段」）、指针解引用、嵌入字段提升
- 三类诊断精确分工：`BIND-001` 首段 / `BIND-002` 中间段 / `BIND-003` 未导出 / `BIND-005` 形式不受支持
- `SCOPE-004` 提前实现：建立 `<For>` 作用域依赖 `In` 的可迭代性；若不检查，循环体内的
  `{item.X}` 会被误报成 `BIND-001`，掩盖真正原因
- 夹具期望表**分步启用**：7 个绑定类夹具已启用，9 个依赖步骤 3/4 的保持待启用并有测试守护

### 第 1 步已完成的内容

- `load.go`：`ParseTypeExpr`（冻结文法）、`TypeEnv`（包缓存）、`ResolveViewModel`
- `ir.go`：语义 IR 类型（`Result`/`Facts`/`Resolved*`/`TypeInfo`/`VerifiedDoc`）
- `codes.go`：10 个已定稿诊断码常量
- 17 个夹具 + 期望表（Go 结构体字面量，无 `want.txt`）
- 阶段性数据见下方各步；最终数据见本节末尾

## 4. 关键约束（来自契约）

| 约束 | 说明 |
|---|---|
| 不修改结构 IR | 语义事实存 `Facts` 旁表 |
| `contracts` 不依赖 `go/types` | 解析结果用字符串与枚举表达 |
| 只记录已验证的事实 | 失败项只产诊断，不进 `Facts` |
| 不生成 Go 代码 | 表达式生成归 Codegen |
| 不允许伪造成功 | `ViewModel` 无法解析时 `StatusBlocked`，不得跳过检查当通过 |

## 5. 验收要求

### 现在已可验收

```text
$ go test ./internal/compiler/analyzer/
ok  coverage: 92.1%   （141 个测试）

# 整体验收（2026-10-18，提交 9381542）
$ go test ./...                              # 全仓 470 测试
$ go test ./internal/compiler/diagdoc/       # 诊断码门禁（AST）

$ go test ./internal/compiler/diagdoc/
ok  （诊断码目录一致性门禁）
```

### 完成后的证据与边界

- `Verified()` 对 `Partial`/`Blocked` 拒绝签发：由 `TestPartialCannotGetVerifiedDoc` 和 `TestBlockedCannotGetVerifiedDoc` 锁定。
- 编排层以真实调用链验证失败门禁：见 `TASK-ORCH-001` §6.1。
- 行为夹具 15/16 启用；剩余组合的 v1 限制和待启用理由见 Analyzer 验收报告，不把它计作未解释的缺口。

## 6. 验收结论

整体验收通过，详见 [Analyzer验收报告.md](../../06-测试/Analyzer验收报告.md)。

审计修复 4 个问题：`<For>` 级联误报、`EventArg.Literal` 丢引号、
诊断码清单声称未实现、目录过期表述。后两者说明**手写门禁会自证**，
已改为 AST 驱动的真实校验。

## 7. 遗留

- ~~编排层尚不存在~~ → 已由 `TASK-ORCH-001`（`d5c083b`）实现，
  `StatusBlocked` 已接到管线总入口。
- 索引访问、Map 取值与任意方法调用不属于 GXML v1 的点分字段路径；扩展须更新语言规范与 Analyzer 契约。
