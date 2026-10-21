# TASK-CODEGEN-NAMING-001：生成符号命名与包级冲突契约

## 元信息

- 状态：**边界已冻结，待裁决（2026-10-21）**；本卡**只定契约**，不含实现
- 前置：`TASK-CODEGEN-001` ✅；缺陷首次登记于 [TASK-GENERATE-CACHE-001 §13](TASK-GENERATE-CACHE-001.md)
- 契约依据：[代码生成输出契约](../../03-架构/代码生成输出契约.md)
  §5（`Goxml` 前缀 + 冲突即失败）、§16.5.1（`renderKey` 每文件私有）、§13 第 1/14 条
- 触发原因：同包两个 `.gxml` 生成的产物**不可编译**，而 `generate` 仍报成功

## 1. 缺陷复现（2026-10-21，当前远端实测）

```text
go run ./cmd/goxml generate <dir>          # dir 下有 home.gxml 与 second.gxml（同包）
已生成 …\home_gen.go
已生成 …\second_gen.go
共 2 个文件生成成功（按路径排序处理）        # ← 退出码 0，报告成功

go build ./<dir>/
second_gen.go:68:6:  renderKey redeclared in this block
                    home_gen.go:68:6: other declaration of renderKey
second_gen.go:76:20: method renderKey.equal already declared at home_gen.go:76:20
second_gen.go:97:6:  textCache redeclared in this block
                    home_gen.go:97:6: other declaration of textCache
```

两条契约同时被违反：

- §5 第 5 条「仍与同包符号冲突 → 生成失败并报告冲突来源」：**没有检测，也没有失败**；
- §13 第 14 条「生成源码通过 Go parser、gofmt 和 package 检查」：产物**过不了编译**。

用户视角的后果比报错更糟：`generate` 说成功，`go build` 才炸，且错误指向生成文件，
看不出是「两个 `.gxml` 同包」造成的。

## 2. 现状枚举：生成物会发出的包级符号

| 符号 | 命名来源 | 每个文件都发 | 同包多文件是否撞 |
|---|---|---|---|
| `<View>` | `Goxml` + `Pascal(SourcePath 切段)` + `View`【`codegen.go:1122-1141`】 | 是 | 按路径唯一；但 **pascal 塌缩**会让 `a-b.gxml` 与 `a_b.gxml` 互撞 |
| `New<View>` / `*<View>` | 派生自 `<View>` | 是 | 同上 |
| `<View>.Layout` / `currentKey` / `bindText` | 方法，挂 View | 视需要 | 无独立风险（随类型） |
| **`renderKey`** + `renderKey.equal` | **固定名** | **是**（静态页也发，`codegen.go:942`） | **必撞** |
| **`textCache`** | **固定名** | 有绑定槽位才发（`codegen.go:791`） | **同包两个绑定页必撞** |

结论：`renderKey` 与 `textCache` 是**两个固定名**，且 `renderKey` 无条件发射，
因此「同包 ≥2 个生成文件」在当前形态下**必然**不可编译。这不是边缘情况。

## 3. 契约内部矛盾（必须先解决，否则实现没有判据）

| 条款 | 原文要求 | 与谁冲突 |
|---|---|---|
| §5 第 4/5 条 | 用 `Goxml` 前缀避免与用户冲突；仍冲突则**生成失败并报告来源**，不静默改名 | 实现里 `renderKey`/`textCache` 既无前缀、也无检测 |
| §16.5.1 | `renderKey` 由**每个生成文件自行定义**为私有类型，不新增项目包依赖 | 私有 = 同包可见，**不隔离**；同包多文件时两个定义直接重名 |

一句话：**「每文件私有定义」与「同包多个生成文件」在 Go 里不能同时成立**，
除非（a）类型名按文件唯一，或（b）明确禁止同包多文件并前移报错。

## 4. 前提问题 P0：同包多个 `.gxml` 是否支持？

| 事实 | 依据 |
|---|---|
| CLI 会把同一目录下所有 `.gxml` 一起生成 | `discoverInputs` 递归；本次复现即一个目录两个源 |
| 没有任何文档或诊断阻止同包多文件 | 全仓检索「同包」无禁令 |
| 缓存卡的混合批次用例因此改用「两个子包」规避 | `TASK-GENERATE-CACHE-001` §13.1 |

P0 决定了方案走向，**必须由裁决给出**：

- P0 = 支持 → 类型名必须唯一（N1），且要接受产物变长、§16.5.1 重写；
- P0 = 不支持 → 必须在生成前检测并报错（N3），诊断指出与哪个源冲突。

## 5. 候选方案

| 方案 | 做法 | 支持同包多文件 | 代价 |
|---|---|---|---|
| **N1 唯一化** | `renderKey` → `goxmlRenderKey<PascalSource>`；`textCache` → `goxmlTextCache<PascalSource>` | ✅ | 产物变长；§16.5.1 须重写；若干测试的字符串断言要跟着改 |
| N2 每包一份共享 | 首个文件生成基础设施，其余引用 | ✅ | 引入跨文件隐式依赖；删除/重生成顺序脆弱；**建议否决** |
| **N3 冲突即失败** | 生成前检测同包已有生成物，冲突则报错 | ❌（显式不支持） | 实现小、语义清晰；但同包多页需求被永久关闭 |
| N4 仅加前缀不加源 | `goxmlRenderKey` | ❌ | 同包第二个文件仍撞；只在 P0=不支持时有意义 |

**建议**：P0=支持 + N1（前缀 + 源派生后缀）。理由：CLI 已经做成"给一个目录就全生成"，
用户没有理由认为"同包多页"是非法用法；用报错关掉能力，代价是一个真实用例永久缺失。

## 6. 冲突检测规则（任何方案都必须有，落地 §5 第 5 条）

1. 收集**本文件将发出的包级符号集合**（类型、包级函数、包级变量/常量）；
2. 用 `go/ast` 解析目标包内**已有 `.go`** 的包级声明（不含 `_test.go`）；
3. 排除本文件自己的旧产物：同时满足「带保护头」且「`// Source:` 等于本次 SourcePath」；
4. 有交集 → **生成失败**，诊断必须包含：符号名、冲突文件路径、双方来源（本文件 / 对方文件）；
5. 禁止静默改名、禁止静默覆盖用户文件（与 `--clean` 的所有权规则一致）。

## 7. 稳定性与可见性要求

- 同一输入重复生成**字节一致**（既有 `TestGenerationDeterminism` 必须继续通过）；
- 命名不得依赖绝对路径、map 迭代顺序、时间戳；
- 唯一化后，**私有符号不得跨生成文件引用**（N1 会让跨文件引用立刻断掉）；
- 产物内容变化会让缓存键自然失效一次，缓存侧无需特殊处理。

## 8. 验收矩阵

| # | 场景 | 预期 | 计划测试 |
|---|---|---|---|
| A1 | 同包两个 `.gxml`（N1） | `go build` 该包通过；两个 `_gen.go` 符号互不冲突 | `TestNamingTwoFilesSamePackageCompiles` |
| A1' | 同包两个 `.gxml`（N3） | 第二个文件生成失败，诊断指明冲突源与符号 | `TestNamingSamePackageRejected` |
| A2 | 用户已有 `renderKey` 类型 | 生成失败并报冲突（不得覆盖、不得改名） | `TestNamingUserSymbolConflict` |
| A3 | 不同目录同名 `home.gxml` | 两个包各自 `GoxmlHomeView`，互不影响 | `TestNamingSameBaseDiffDirs` |
| A4 | pascal 塌缩：`a-b.gxml` 与 `a_b.gxml` 同包 | 检测并报错（或以可区分命名生成），**不得静默产出重名** | `TestNamingPascalCollapse` |
| A5 | 静态页 + 绑定页同包 | 两种形态都能编译 | `TestNamingMixedPagesSamePackage` |
| A6 | 重复生成两次 | 字节一致 | 既有 `TestGenerationDeterminism` |
| A7 | 单文件产物 | 除本次改名涉及的符号外，其余字节不变（防止顺手改坏） | `TestNamingSingleFileStable` |
| A8 | 冲突时磁盘状态 | 不写任何文件、不改 Manifest、不写缓存 | `TestNamingConflictNoWrites` |

## 9. 待裁决

| # | 问题 | 选项 | 建议 |
|---|---|---|---|
| D-1 | P0：同包多个 `.gxml` 是否支持 | 支持 / 不支持 | **支持**（CLI 已递归生成） |
| D-2 | 采用方案 | N1 / N3 | **N1**（若 D-1 支持）；D-1 不支持则 N3 |
| D-3 | 命名形态 | `goxmlRenderKeyHome` / `renderKeyHome` | **带 `goxml` 前缀**（符合 §5 第 4 条） |
| D-4 | pascal 塌缩 | 检测报错 / 以可区分命名 | **检测报错**（A4），避免隐式改名 |
| D-5 | 是否新增诊断码 | 复用 `CODEGEN-NAME-*` / 新增 | 优先复用；确有语义缺口再新增并同步诊断码目录 |

## 10. 实施切片（裁决后才有意义）

| 切片 | 内容 | 验收 |
|---|---|---|
| C1 | 符号枚举 + 冲突检测（纯读，`go/ast`），先只报错不改名 | `go test -run TestNamingDetect ./internal/compiler/codegen/` |
| C2 | 命名唯一化（若 N1），含 pascal 塌缩处理 | `go test -run TestNamingUnique ./internal/compiler/codegen/` |
| C3 | 诊断与 CLI 接线：失败时不写盘、不改 Manifest、不写缓存 | `go test -run TestNamingConflictNoWrites ./internal/cli/` |
| C4 | §8 全矩阵 + 契约 §5/§16.5.1 与规范同步 | `go test -count=1 ./internal/compiler/codegen/ ./internal/cli/` |

## 11. 纪律

- 先改契约再改实现；本卡 §4–§7 冻结后不得在实现中静默放宽；
- 每条断言先证明能变红（临时破坏检测逻辑），再让它变绿；
- 不在本卡修其它 Codegen 问题（诊断码目录、命名风格、import 别名各自归口）；
- 共享工作树禁止 `git add -A`；提交前核对暂存列表。
