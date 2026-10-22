# TASK-CODEGEN-NAMING-001：生成符号命名与包级冲突契约

## 元信息

- 状态：**已裁决（D-1~D-5）；C1 预检检测已实现（**未单独验收**，见 §10.4 边界）；C2 **验收通过，附带非阻塞遗留项**（项目负责人裁决 2026-10-22，见 §10.4）；C3 实现完成（**独立复审 PASS WITH NON-BLOCKING，复审发现已修复**）；C4 **项目负责人最终验收通过**（2026-10-22，附带一项非阻塞文档事项，见 §10.8）；C5 实现完成（**待独立复审**，见 §10.9）**。**C5 实现完成 ≠ 命名流程完成**（§10.4.5、§10.5.3、§10.8.3、§10.9.6）
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
| **N1 唯一化（已裁决采用）** | `renderKey` → `goxmlRenderKey<PascalSource>`；`textCache` → `goxmlTextCache<PascalSource>` | ✅ | 产物变长；§16.5.1 须重写；若干测试的字符串断言要跟着改；**唯一性由 §5.1 的五条约束保证，不能把「加了后缀」当成不撞的证明** |
| N2 每包一份共享 | 首个文件生成基础设施，其余引用 | ✅ | 引入跨文件隐式依赖；删除/重生成顺序脆弱；**建议否决** |
| **N3 冲突即失败** | 生成前检测同包已有生成物，冲突则报错 | ❌（显式不支持） | 实现小、语义清晰；但同包多页需求被永久关闭 |
| N4 仅加前缀不加源 | `goxmlRenderKey` | ❌ | 同包第二个文件仍撞；只在 P0=不支持时有意义 |

**建议**：P0=支持 + N1（前缀 + 源派生后缀）。理由：CLI 已经做成"给一个目录就全生成"，
用户没有理由认为"同包多页"是非法用法；用报错关掉能力，代价是一个真实用例永久缺失。

### 5.1 N1 的唯一性与稳定性约束（D-2 裁决补强，冻结）

「追加源文件名」本身**不构成**唯一性证明（`PascalCase` 会把不同路径塌缩成同一标识符）。
以下五条为硬约束：

1. **稳定**：同一源文件 + 相同生成配置 ⇒ 标识符逐字符稳定，重复生成字节一致；
2. **碰撞前移**：不同源派生出相同 Go 标识符时，**必须在写出任何产物之前**检测并报错；
3. **检测范围**：本次生成的全部输入文件 + 目标包内已有生成标识符 + 用户包级声明，
   三者取并集判交，不能只看当前这一个文件；
4. **不得只靠 `PascalCase`**：转换只负责可读性，唯一性由第 2/3 条的检测兜底；
5. **禁止不稳定后缀**：不得使用随机串、时间戳、递增编号等每次生成都变的后缀——
   那会让产物逐次漂移，破坏契约 §13 第 1 条的确定性。

> 若将来评估用**哈希后缀**（如 `goxmlRenderKeyAb12`），必须同时冻结：哈希输入
> （哪几段路径/配置）、长度、稳定性、以及哈希本身碰撞时的处理。
> 本卡不因引入哈希而推翻 N1；哈希只是「后缀取值」的一种候选实现。

## 6. 冲突检测规则（任何方案都必须有，落地 §5 第 5 条）

### 6.1 两阶段流程（冻结）

```text
阶段 1：预检（不写任何文件）
  a. 发现本次全部输入（目录递归结果）
  b. 为每个输入算出目标包目录与将发出的包级符号集合
  c. 并入「目标包已有生成标识符」与「用户包级声明」
  d. 判交：有任何重合 → 非零退出，指出符号、冲突文件、双方来源

阶段 2：仅在阶段 1 无冲突时执行
  a. 逐文件生成产物
  b. package / 格式校验（契约 §13 第 14 条）
  c. 暂存 → 原子提交 → 更新 Manifest → 写缓存（沿用 CLI 既有顺序）
```

**预检必须先于任何写入**：不得边扫边写、写完再回头发现冲突。

### 6.2 失败路径的零副作用约束（与缺陷复现直接对应）

冲突导致的失败必须满足：

- **不写入、不覆盖任何生成文件**（已有产物内容与 mtime 都不变）；
- **不修改 Manifest**（不新增登记、不改既有条目）；
- **不写入生成缓存**（`.goxml/cache/` 不新增记录）；
- 退出码非零，诊断包含冲突符号与来源文件。

> 现有缓存实现的顺序本来就是「产物 → Manifest → 缓存」，预检失败时三者都不触发，
> 因此天然满足本条。**但仍要写成契约并加验收**：一旦将来把缓存改成「扫描时顺手写」，
> 这条会被悄悄破坏，症状是「生成失败但缓存已被污染」。

### 6.3 检测细节

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

验收分**两组**：冲突验收证明「该失败时确实失败且指出来源」；副作用验收证明
「失败时什么都没留下」。只证明错误消息出现，不足以覆盖契约 §5 与 §13。

### 8.1 冲突验收

| # | 场景 | 预期 | 测试（实际函数名） |
|---|---|---|---|
| A1 | 同包两个 `.gxml`（N1） | `go build` 该包通过；两个 `_gen.go` 符号互不冲突 | `TestTwoStaticSourcesSamePackage` / `TestTwoBindingSourcesSamePackage`（均真实 `go build ./<dir>`） |
| A1' | 同包两个 `.gxml`（N3） | 第二个文件生成失败，诊断指明冲突源与符号 | `TestNamingDetectsSameBatchConflict`（codegen）+ `TestNamingConflictSameBatchRejected`（CLI） |
| A2 | 用户已有 `renderKey` 类型 | 生成失败并报冲突（不得覆盖、不得改名） | `TestNamingDetectsExistingUserSymbol` + CLI `TestNamingConflictNoWrites` |
| A3 | 不同目录同名 `home.gxml` | 两个包各自 `GoxmlHomeView`，互不影响 | `TestNamingSameBaseDifferentDirsOK` / `...NoConflict` / `...NoDiagnostics` |
| A4 | pascal 塌缩：`a-b.gxml` 与 `a_b.gxml` 同包 | 检测并报错（或以可区分命名生成），**不得静默产出重名** | `TestNamingDetectsPascalCollapse` + `TestNamingSameDirCollapseStillConflicts` |
| A5 | 静态页 + 绑定页同包 | 两种形态都能编译 | `TestStaticAndBindingSourcesSamePackage` + `TestNamingPageKindsSymbolSets` |
| A6 | 重复生成两次 | 字节一致 | 既有 `TestGenerationDeterminism`（`internal/cli/determinism_test.go`） |
| A7 | 单文件产物 | 除本次改名涉及的符号外，其余字节不变（防止顺手改坏） | `TestNamingSingleFileStable`（C5 新增） |

> **计划名与实现名的重映射（2026-10-22，C5 开工核对）**：本表初版写的 8 个
> 「计划测试」名里，只有 `TestGenerationDeterminism` 真实存在，其余 7 个
> 从未创建——实现用了别的名字（如 A1 落在 `TestTwoStaticSourcesSamePackage`）。
> 矩阵若照旧挂着计划名，会让读者误判「A1/A2/A3/A4/A5/A7 都没做」。
> 本表已按**行为等价**重映射到真实函数名；A7 是核对时唯一**确实缺失**的一项，
> 由 C5 补齐。核对方法与逐项结论见 dev-log `2026-10-22.md` §6.1。
### 8.2 副作用验收（预置既有状态，再触发冲突）

| # | 预置状态 | 触发方式 | 断言 |
|---|---|---|---|
| S1 | 目标包已有一个 `_gen.go`（上一版产物） | 制造冲突后生成 | 该文件内容与 mtime 均不变 |
| S2 | Manifest 已登记若干条目 | 同上 | Manifest 字节不变 |
| S3 | `.goxml/cache/` 已有记录 | 同上 | 缓存目录文件集合与每个文件内容均不变 |
| S4 | 三者同时存在 | 同上 | 三者同时不变，且 stdout 不出现「已生成」「共 N 个文件生成成功」 |
| S5 | 无任何预置（首次） | 同上 | 目录内不出现新文件、不出现 `.tmp` 残骸 |

> **S4 的 stdout 断言必须带正对照**（C5 核对时发现）：`共 %d 个文件生成成功`
> 只在本次写入文件数 **> 1** 时打印。若在单文件场景里断言它「不得出现」，
> 那条断言**永远成立**——是装饰品，证明不了任何事。因此 S4 必须构造
> **两个源**：先成功生成一次证明该行**确实会出现**，再触发冲突断言它不出现。
> 实现见 `TestNamingConflictStdoutSilentMultiFile`。
> 注意适用范围：「已生成」在单文件成功时**也会**打印，所以 C4 的单文件用例里
> 断言它不出现是有意义的；只有 `共 N 个文件生成成功` 受 `> 1` 条件限制。

## 9. 裁决结果（2026-10-21）

| # | 问题 | 裁决 | 落地位置 |
|---|---|---|---|
| D-1 | 同包多个 `.gxml` 是否支持 | **支持** | §4 P0 结论；§5 采用 N1 |
| D-2 | 方案 | **N1**（`goxml` 前缀 + 源派生后缀），并受 §5.1 五条约束 | §5 / §5.1 |
| D-3 | 命名形态 | **小写 `goxml` 前缀**（如 `goxmlRenderKeyHome`） | §5 方案表 |
| D-4 | pascal 塌缩 | **检测并报错**，不隐式改名 | §5.1 第 2/3 条 + A4 |
| D-5 | 诊断码 | **无法直接复用**，改为新增具体码 | §9.1 |

### 9.1 D-5 的修正：现有 `CODEGEN-NAME-*` 没有具体码

裁决要求「优先复用 `CODEGEN-NAME-*`，但必须核对语义」。核对结果：

| 检查项 | 结果 |
|---|---|
| `代码生成输出契约.md` 错误分类 | 只有通配符 `CODEGEN-NAME-*`，**没有具体编号与触发条件** |
| `internal/compiler/codegen/*.go` | **零个** `CODEGEN-*` 诊断码常量（全仓检索只命中任务编号 `TASK-CODEGEN-001`） |
| `诊断码目录.md` | 无 `CODEGEN-NAME-*` 条目 |

结论：**没有可复用的具体码**，「复用」在实现上是空操作。因此改为新增，但沿用既有分类前缀：

- `CODEGEN-NAME-001`：生成符号与目标包内既有包级声明冲突（用户代码或其它生成物）；
- `CODEGEN-NAME-002`：不同源文件派生出相同符号（pascal 塌缩 / 重名）；
- 两码都要登记到 `诊断码目录.md`，并按仓库既有做法进入 diagdoc 的
  `implementedCodes`/`frozenCodes` 校验（否则文档门禁会红）；
- 码值发布后不得改语义；出现第三类冲突时扩号，不复用旧号。

## 10. 实施切片（裁决后才有意义）

| 切片 | 内容 | 验收 |
|---|---|---|
| C1 | 符号枚举 + 预检式冲突检测（纯读，`go/ast`），先只报错不改名 | ✅ **完成（2026-10-21）**：`naming.go`（`generatedPackageSymbols`/`detectNamingConflicts`/`isOwnArtifact`）+ `naming_test.go` 8 用例；`go test -count=1 -run '^TestNaming' ./internal/compiler/codegen/` → PASS=8；4 条变异全部能红（dev-log §95） |
| C2 | 命名唯一化（N1），含 pascal 塌缩检测 | ✅ **验收通过，附带非阻塞遗留项（2026-10-22 裁决，见 §10.4）**；实现与补丁过程见 §10.2/§10.3：`sourceSymbolSuffix`/`generatedTypeNames`；类型名 `goxmlRenderKey<Source>` / `goxmlTextCache<Source>`；契约 §16.5.1 同步；红灯由「变异回固定名」取证（dev-log §97.1）；`go test -count=1 ./internal/compiler/codegen/` ok（124s）、全量 `go test ./...` exit 0。**+ 复核轮（dev-log §97.7/§97.8）**：补 `sourceSymbolSuffix` 直接断言与三形态/稳定性断言（两项此前无覆盖）；两次变异取证（退回固定名、破坏分隔符规则）；`cacheFieldLines` 单点变异复现两个基准失败并证伪"夹具问题"说 |
| C3 | 诊断码定义 + 目录登记 + diagdoc 门禁同步 | ✅ **实现完成（2026-10-22）；独立复审 PASS WITH NON-BLOCKING，发现项已修复（§10.6）**：`codegen.CodeNamingExisting`/`CodeNamingSameBatch`（`CODEGEN-NAME-001/002`）；语义先冻结于 §10.5.2；目录 §2.8 登记 + 门禁 5 处同步；`naming_diag_test.go` 6 用例；两码互换的变异取证已做 |

> C3 只做「定义 + 发射 + 登记 + 门禁」，**不接 CLI**（归 C4）、**不修 F-06**
> （归 C4-PRE-1）、**不把 C1 预检标记为已验收**（判定规则仍待 C4-PRE-2）。
| C4 | 零副作用接线：预检失败 → 不写盘/不改 Manifest/不写缓存（§8.2） | ✅ **项目负责人最终验收通过（2026-10-22，见 §10.8）**：`precheckPackageSymbols` 挂在阶段零 C（缓存判定之后、阶段一 `MkdirAll` 之前），只对未命中输入；`codegen.CheckNamingConflicts` 为唯一入口；三项前置 C4-PRE-1/2/3 均完成；接线暴露并修复「批内冲突未按目标包分组」（§10.7.5）；首轮复审 R1–R4 + fail-open 已修、二轮复审通过（§10.7.8–§10.7.10）；变异共 9 组。**附带一项非阻塞文档事项：命令文本已统一**（§10.8.2） |
| C5 | §8 全矩阵 + 契约 §5/§16.5.1 与规范同步 | ⏳ **实现完成（2026-10-22），待独立复审**（§10.9）：`TextCache` 按 `bindSlots > 0` 收窄（`slots.go` 单一派生 + 前端复用）；A7 补齐；S4 补正对照；§8.1 计划名重映射；契约 §5/§16.5.1 同步；变异 7 组。**未获验收，不宣称通过** |

### 10.1 C2 复核轮补记（2026-10-22，dev-log §97.7/§97.8）

复核不采信"文档已写完成"，逐项重跑后补了三处：

| 项 | 复核发现 | 处理 |
|---|---|---|
| 测试覆盖 | `sourceSymbolSuffix`（派生规则本身）此前**无直接测试**；View/key/cache 三形态与"重复派生稳定"也未断言 | 新增 `TestNamingSourceSymbolSuffixRules`、`TestNamingViewAndTypesShareSuffix`，均做变异取证 |
| 契约一致性 | §16.5.1 只补了规则与示例，**标题与首段仍是旧固定名规则**；§16.2 结构体示例仍写 `cacheKey renderKey`；示例注释与生成器实际输出不一致 | 逐处改为 `goxmlRenderKey<Source>` 口径，示例对齐生成器输出，并补 text cache 同源说明 |
| 章节引用 | 生成器注释写「契约 §5.1」，但契约没有 §5.1（那是**本卡** §5.1） | 改为「TASK-CODEGEN-NAMING-001 卡 §5.1」（5 处，注释级） |

**docgate 在本分支的红不是 C2 引起的**：缺口是分支基线 `d9cf0b9`（脚本护栏线），
其记录随 `27aa03a`/`827ab41` 在 master 线的 `2026-10-21.md` §38，本分支不含。
已在 dev-log §97.7 登记该 SHA 的去向（不复制正文，避免后续合并冲突），
登记后 `TestDevLogCoversRecentCommits` 转绿。

### 10.2 C2 验收补丁（2026-10-22，dev-log `2026-10-22.md` §1）

维护者在 GitHub 上直接审查分支 `feat/codegen-naming-c2`（HEAD `2dd83b0`）后给出的结论是
**核心实现合理，但暂不标记最终验收通过**：1 个阻塞项 + 1 个待处理项。
本节记录该补丁对本卡的改动，不含 C3/C4/C5 内容。

| 项 | 审查结论 | 本轮处理 | 结论 |
|---|---|---|---|
| 阻塞项 | 缺少「双文件真实编译」回归测试——`TestGeneratedCompiles` 只编译单个生成文件，不能独立证明两个不同 `.gxml` 的产物放进同一 Go 包后可一起编译 | 新增 `naming_samepkg_test.go`：静态+静态、静态+含绑定（混合组合）、含绑定+含绑定 三组**真实 `go build`**，加一组纯逻辑类型名唯一性断言；退回固定名的变异下三组均在 `go build` 层报 `redeclared` | **已关闭** |
| 待处理项 | `generatedPackageSymbols` 无条件纳入 `goxmlTextCache<Source>`，纯静态页可能被过度报告冲突 | 新增 `naming_textcache_boundary_test.go`，实测确认"静态页产物不声明该类型、预检却计入、真实 build 仍通过"→ 误报可复现；反向对照证明含绑定页里该预检是**必要**的 | **已定性，不在 C2 修**：归属 C4/C5，理由见 dev-log §1.3 |

验收状态：**C2 实现完成，验收补丁已补；最终验收待独立 Reviewer 复审后由维护者裁决。**
本卡不再自行标"最终验收通过"。

重新执行的门禁（本轮实测，非沿用旧记录）：

| 命令 | 结果 |
|---|---|
| `gofmt -l` / `go vet ./internal/compiler/codegen/` / `go build ./...` | 空 / 0 / 0 |
| 新增用例定向跑 | **9 个 PASS**（首轮 8 个 17.7s；复审后补纯事件页用例，共 9 个） |
| `go test -timeout 30m ./internal/compiler/codegen/` | 首轮 ok 525.4s；独立复审实测 523.1s；**订正后重跑 ok 568.991s**（三次同量级，波动来自子进程编译） |
| 其余 17 个包（排除 codegen） | 13 个 `ok` + 4 个 `[no test files]`，无 FAIL |
| `go test -timeout 20m ./internal/docgate/` | 13/13 PASS |

> 上表初版把 docgate 写成 14/14、其余包写成 14 ok + 3 无测试文件，两个数字都记错
> （实际 13 / 13+4），由独立复审指出后订正——见 dev-log 2026-10-22 §2。

### 10.3 独立复审后的订正（2026-10-22，dev-log `2026-10-22.md` §2）

复审结论 **PASS WITH NON-BLOCKING**，无 P0/P1。已处理的 P2/P3：

| 编号 | 等级 | 问题 | 处理 |
|---|---|---|---|
| P2-3 | P2 | 边界测试文件头称 `bindCacheDecl` 只在 `usesVM` 为真时生成，与生产代码冲突（实际条件 `slots > 0`） | 注释订正 + **补纯事件页用例** `TestPrecheckOverreportsForEventOnlyPage` + 给 C4 留判据提醒 |
| P2-4 | P2 | `naming.go` 注释称保守上界"影响仅限于同后缀塌缩"，被本补丁自己的测试证伪 | 注释补记第二类误报与收窄判据 |
| P3-1 | P3 | 3 处硬编码符号字面量 | 改为 `viewSymbol`/`generatedTypeNames` 派生 |
| P3-2 | P3 | `go build ./<dir>/...` 对空目录退出 0（潜在空转） | 改用显式路径（去掉 `...`） |
| P3-3 | P3 | 唯一性用例的注释把"样本互不冲突"写成普遍不变量 | 注释订正（塌缩是预期撞名，归 C1 预检） |

复审登记但**未处理**的残余风险：跨平台未实测（仅 Windows）；冷 module cache
+ 无网络未验证；`testdata/` 孤儿包场景未构造。均属环境类，不阻塞本轮验收。

### 10.4 C2 最终验收裁决（项目负责人，2026-10-22）

**裁决：C2 验收通过，附带非阻塞遗留项。**

- 裁决人：项目负责人（在 GitHub 上核对远端提交与公司比较接口后作出）
- 裁决时点分支状态：`feat/codegen-naming-c2` HEAD `de5031c`，领先 `master` 7 笔、落后 1 笔
- 裁决依据：本节 §10.2/§10.3 的实测记录 + 两轮独立复审报告；
  **项目负责人未重新执行本地 Go 测试，测试结果依据执行记录与复审报告**
- 边界提醒：远端存在 `de5031c`、GitHub 比较接口确认提交数，
  **但当前提交没有可见的 GitHub CI 状态记录，不得等同于 CI 全绿**

#### 10.4.1 四项验收工作的判断

| 验收项 | 判断 | 依据与边界 |
|---|---|---|
| 双文件同包真实编译 | **通过** | 三种页面组合的真实编译回归 + 退回固定旧符号名的变异能触发真实编译失败 |
| `docgate` 基线核查 | **通过（按报告）** | 补丁前后均运行，记录 13/13；**不得写成 14/14** |
| text cache 预检边界 | **已查明，留待后续修复** | 静态页与纯事件页的保守预检误报已证明；绑定槽位边界已记录 |
| 全量回归与文档 | **通过（按报告）** | codegen 全包与其余包均无失败；任务卡/WBS/dev-log 已更新并经两轮复审订正 |

#### 10.4.2 必须保留的三个限制（不因 C2 通过而关闭）

1. **预检误报没有被修掉。** 留 C4/C5 是合理的：C2 的目标是**生成符号唯一化**，
   不是提前完成 CLI 预检的精确判定。收窄判据见 §10.2 与
   `naming_textcache_boundary_test.go` 顶部（`bindSlots > 0`，不是 `usesVM`）。
2. **F-06 路径归一化必须在 C4 接线前处理。** `isOwnArtifact` 用 raw `SourcePath`
   比对，而保护头写的是 `normalizeSourcePath()`（`\`→`/`）→ Windows 风格路径下
   自家生成产物会被误判成"用户声明的冲突"。**列为 C4 的前置验收项**（见 §10.4.3），
   不因 C2 测试通过而关闭。
3. **测试环境覆盖仍不完整。** 跨平台（仅 Windows 实测）、冷 module cache + 无网络、
   `testdata/` 孤儿包场景均未验证。写"**未覆盖**"，**不得写成已通过**。

#### 10.4.3 C4 接线的前置验收项（进 C4 前必须完成）

| 编号 | 前置项 | 说明 |
|---|---|---|
| C4-PRE-1 | F-06 路径归一化 | `isOwnArtifact` 改用 `normalizeSourcePath()` 比对并配 Windows 风格路径回归测试 |
| C4-PRE-2 | 预检对三类页面的判定规则 | 明确：静态页（`bindSlots == 0`）/ 纯事件页（`usesVM` 真、`bindSlots == 0`）/ 含绑定页（`bindSlots > 0`）分别是否计入 `goxmlTextCache<Source>`，并写回 `naming.go` 与本节 |
| C4-PRE-3 | 诊断码就绪 | 依赖 C3：`CODEGEN-NAME-001/002` 已登记并进 diagdoc 校验 |

#### 10.4.4 后续顺序（项目负责人确认）

C3（诊断码）→ C4（接线，**含 §10.4.3 三项前置**）→ C5（冲突矩阵 + 零副作用 +
跨层验收）→ 各任务分别通过审查后**再评估与 `master` 整合**。
当前分支落后 `master` 1 笔，**先核对差异、不直接合并、不重放或改写已推送历史**。

#### 10.4.5 C2 通过的边界（不可外推）

**C2 验收通过 ≠ C1 预检与整个命名流程已完成。** 具体而言：

- C1 的"检测已实现"**不等于**已验收：它的判定规则（含 §10.4.3 C4-PRE-2 的三类页面）
  仍有待 C4 补齐，零副作用也尚未验证；
- 预检**至今零接线**（`detectNamingConflicts` 仅 codegen 包内引用），
  因此"两个 .gxml 同包"这个原始缺陷在 **CLI 路径上仍未消除**——
  C2 修的是产物层（生成的代码能一起编译），不是 CLI 层的提前拦截；
- 整个命名流程完成 = C3 + C4 + C5 全部完成并分别通过审查。

### 10.5 C3 开工：诊断码定义、目录登记与门禁同步（2026-10-22）

#### 10.5.1 开工时的分支真实状态（非裁决时快照）

| 项 | 值 |
|---|---|
| 分支 | `feat/codegen-naming-c2` |
| 开工 HEAD | **`5ae2252`** |
| 相对 `master` | **领先 9 笔 / 落后 1 笔**（落后的是 `27aa03a`，见 §10.4.4 与 dev-log §3.2） |
| merge base | `d9cf0b9` |

> §10.4 裁决正文里的 `de5031c` 与"领先 7 笔"是**裁决当时的历史快照**，
> 不代表当前状态；后续引用一律以本节为准。

#### 10.5.2 冻结语义（先冻结，再实现）

码值沿用 `CODEGEN-NAME-001/002`，**不再重新讨论命名**。

| 码 | 常量（codegen 包） | 冻结语义 |
|---|---|---|
| `CODEGEN-NAME-001` | `CodeNamingExisting` | 生成符号与目标包**已有**包级声明冲突，包括用户声明或其他生成物 |
| `CODEGEN-NAME-002` | `CodeNamingSameBatch` | **不同源文件**派生出相同生成符号，例如 Pascal 归一化后发生碰撞 |

**触发条件**

- `001`：预检检测到「待生成符号 ∈ 目标包既有包级符号集合」，
  来源为**用户代码**或**别的生成物**（自家上一版产物不算，仍按 §8 的
  `isOwnArtifact` 排除；**F-06 的路径归一化为 C4-PRE-1，不在 C3 修**）。
- `002`：同一批输入中，两个不同 `.gxml` 派生出同一符号（`a-b.gxml` 与
  `a_b.gxml` 的 pascal 塌缩是代表场景）。

**消息参数（冻结后不得随意改文案）**

| 码 | 消息格式 | 实参顺序 |
|---|---|---|
| `001` | `generated symbol %q (from %s) is already declared in package: %s` | 符号、源 `.gxml` 路径、占用者文件绝对路径 |
| `002` | `sources %q and %q both generate symbol %q` | 源 A、源 B、符号 |

**冲突来源表达**

- `File` = 触发方的 `.gxml` 绝对路径；`Line/Col/EndLine/EndCol` = **零值**
  （命名冲突是包级事实，不落在源码某个行列；`CompilerError.Error()` 会输出
  `file:0:0: CODE: message`，与"零值表示未知"的既有约定一致）。
- `namingConflict.Source` 在 `namingConflictExisting` 分支此前**未填**——
  C3 补上（= 触发方的 `SourceAbs`），否则 `001` 的消息缺少第一个实参。
  这是补信息，不改变冲突判定结果。

#### 10.5.3 范围边界（严格遵守）

只做三件事：**诊断码定义、目录登记、门禁同步及相应测试**。

- **不提前接 CLI**（接线归 C4）；
- **不顺手修 F-06**（归 C4-PRE-1）；
- **不把 C1 预检标记为已验收**（C1 判定规则仍待 C4-PRE-2 补齐）；
- 不改动 `sourceSymbolSuffix` / `generatedTypeNames` 的已冻结算法。

### 10.6 C3 独立复审结论（2026-10-22，dev-log `2026-10-22.md` §4.8/§4.10）

两轮复审，均**未越界**（未接 CLI、未修 F-06、未把 C1 标为已验收、未动冻结算法）：

| 轮次 | 对象 | 结论 | 处理 |
|---|---|---|---|
| 1 | `c15ee4a` | **PASS WITH NON-BLOCKING**（无 P0/P1） | P2-1 章节错位、P2-2 **装饰性断言**（`HasPrefix` 判不出内联字面量）已修；P3-1~P3-5 已修 |
| 2 | `a850e28`（修复） | **CONFIRMED WITH MINOR** | 7 条全部改对；P2-2 新判据经两种变异实证有牙；N-1/N-2 已修 |

**遗留（登记未处理，均属门禁既有弱点）**：`codePattern` 会误匹配裸写的
`CODEGEN-NAMING-001`、会漏掉前导字母与连字符连写的第二个码；`pkgConstants`
用 `HasPrefix(name,"Code")` 与包名 `Codegen` 天然冲突；`TestEveryConstantIsEmitted`
不查生产链路可达性。

**C3 结束时的边界**：两码**生产链路不可达**（C3 时的名字 `namingDiagnostics`
无生产调用方），要等 C4 接线后用户才看得到——目录与 dev-log 均已如实写明，
未伪装成"已接线"。C4 已把它导出为 `CheckNamingConflicts` 并完成接线（§10.7）。

### 10.7 C4 开工与实现：预检接线 + 零副作用（2026-10-22，dev-log `2026-10-22.md` §5）

#### 10.7.1 开工时的分支真实状态（非裁决/C3 快照）

| 项 | 值 |
|---|---|
| 分支 | `feat/codegen-naming-c2` |
| 开工 HEAD | **`cb6d47f`** |
| 相对 `master` | **领先 14 笔 / 落后 1 笔**（落后的仍是 `27aa03a`） |
| merge base | `d9cf0b9` |

#### 10.7.2 三项前置的处理结果

| 编号 | 前置项 | 处理 |
|---|---|---|
| C4-PRE-1 | F-06 路径归一化 | **已修**：`isOwnArtifact` 的 `// Source:` 比对两侧都过 `normalizeSourcePath()`；新增 `naming_ownartifact_test.go`（3 用例，含反向对照）。变异：去掉归一化 → 2 红（build 退出 0，非编译错误） |
| C4-PRE-2 | 三类页面判定规则 | **已明确并可执行化**：规则表见 §10.7.4，钉在 `naming_pagekinds_test.go`（产物实际声明 × 预检集合对照） |
| C4-PRE-3 | 诊断码就绪 | C3 已完成（`CODEGEN-NAME-001/002` 已登记、已进 diagdoc 校验） |

#### 10.7.3 接线设计（冻结）

| 决策 | 选择 | 理由 |
|---|---|---|
| 位置 | **阶段零 C**：缓存判定之后、阶段一（`MkdirAll`+生成+暂存）之前 | 卡 §6.1「预检必须先于任何写入」。阶段一里已经有 `MkdirAll`，放到之后就保不住"磁盘零变更" |
| 覆盖范围 | **只对未命中（miss）的输入** | 命中的产物本次不被重写；把它们算进来会让「全部命中、磁盘毫无变化」的运行因既有状态而失败。命中产物仍在磁盘上，会被扫进 existing 集合，撞名照样报得出（归类为 `001` 而非 `002`） |
| 失败表现 | 逐条诊断打到 stderr（`file:0:0: CODE: message`），返回 error → 退出码 2 | 卡 §6.2 与 §8.2 的 S4：stdout 不得出现「已生成」 |
| 导出形态 | `codegen.CheckNamingConflicts([]codegen.NamingTarget)`；`NamingTarget` 由 `namingTarget` 导出而来 | C3 把"导出形态留给 C4 决定"，此处定案：单一入口，字段不变 |

#### 10.7.4 C4-PRE-2：三类页面的判定规则（冻结）

生成器事实（读 `codegen.go` / `binding.go` 得出，已由 `naming_pagekinds_test.go` 钉住）：

- `renderKeyDecl` **无条件**发射（两条返回路径都写它）；
- `bindCacheDecl` 的调用点在 `if g.usesVM` 之内，且函数首行 `if slots == 0 { return "" }`。

| 页面形态 | `Goxml<S>View` + `New…` | `goxmlRenderKey<S>` | `goxmlTextCache<S>` |
|---|:---:|:---:|:---:|
| 静态页（无 VM、无绑定、无事件） | ✓ | ✓ | ✗ |
| 纯事件页（有 VM、有 Click、无绑定） | ✓ | ✓ | ✗ |
| 绑定页（有 `{...}` 绑定） | ✓ | ✓ | ✓ |

预检集合 `generatedPackageSymbols` **只看路径**，对三类页面一律四项全含 →

- 对 `RenderKey` 是**恰好**（三类都声明，不算保守）；
- 对 `TextCache` 是**保守上界**：静态页与纯事件页不声明它，若包内恰好有同名
  用户符号会被误报。代价已由 CLI 侧 `TestNamingStaticPageTextCacheOverreport`
  钉住；**收窄判据是 `bindSlots > 0`（不是 `usesVM`），归 C5**。

#### 10.7.5 接线暴露的既有缺陷：批内冲突没有按目标包分组

接线后第一批红灯不是新代码写错，而是**既有实现的一个真缺陷**：

- 现象：`alpha/row.gxml` 与 `beta/row.gxml`（**不同目录、不同包**）被判
  `CODEGEN-NAME-002`，批次全部放弃；
- 根因：`detectNamingConflicts` 第 1 段用**全局** `firstOwner` 比较符号，
  没有按 `TargetDir` 分组。C1/C2 阶段预检零接线，缺陷不显形；
- 这正是验收矩阵 **A3**（不同目录同名源互不影响）要防的误报；
- 修复：第 1 段改为按 `filepath.Clean(TargetDir)` 分组后再比较；
- 证据：既有 CLI 用例（`TestGenerateNestedDirsRun` 等）从红转绿，
  并补 `naming_batch_group_test.go`（3 用例）。变异（退回全局比较，
  build 退出 0）→ 2 红。

#### 10.7.6 测试与变异取证（本轮实测）

| 项 | 结果 |
|---|---|
| `go test -v -run '^TestNaming' ./internal/compiler/codegen/` | **28 PASS / 0 SKIP**（C3 结束时 18 → C4 首版 26 → 复审修复后 28） |
| `go test -v -run '^TestNaming' ./internal/cli/` | **6 PASS / 0 SKIP**（本轮新增，CLI 侧此前 0） |
| `go test -count=1 -timeout 20m ./internal/cli/`（单包 · 修复后） | ok **135.198s** |
| `go test -count=1 ./internal/cli/`（单包 · 首版） | ok 115.443s |
| `go test -count=1 -timeout 30m ./internal/compiler/codegen/`（单包 · 修复后） | ok **706.869s** |
| `go test -timeout 30m ./internal/compiler/codegen/`（单包 · 首版） | ok 591.952s |
| `go test -timeout 30m ./...`（**全量**，14 包 · 修复后） | cli **147.479s** / codegen **701.849s** |

> 命令文本**逐字对应实际执行**；权威形态在 dev-log §5.11。项目负责人最终验收
> 裁定指出本表此前把 CLI 单包命令简写成 `go test -count=1 ./internal/cli/`
> （少了 `-timeout 20m`），已按实际命令统一。

**耗时口径（回应二轮复审的文档核对）**：上表三种采集方式的值**互不可比**——
单包跑与全量跑的并行度不同（全量时多包 `-p` 并发，cli 从 135.198s 变为
147.479s；codegen 从 706.869s 变为 701.849s，双向小幅波动），且首版与修复后
的用例数不同（26 → 28）。因此这些数字**只用于记录"该命令跑过且通过"**，
不得横向相减当作"新增用例耗时"，也不得当作契约值。完整对照见 dev-log §5.11。

**SKIP 数为 0 是可核对的判据**：C4 首版有两处 `t.Skip` 兜底（复审 R3 与
`textCacheTypeName`），指出后已改为硬断言——"0 SKIP"就是这两条 fail-open
被清掉的实测证据（`-v` 输出里 `--- SKIP` 计数为 0）。
| 变异 1：删掉 `precheckPackageSymbols` 调用 | 3 个冲突用例红（build 0） |
| 变异 2：把预检挪到阶段一之后 | `TestNamingConflictNoWrites` 与 `…DoesNotCreateOutDir` 红 |
| 变异 3：F-06 去掉归一化 | 2 红 |
| 变异 4：批内退回全局比较 | 2 红 |

变异 2 的失败详情记录一条**有用的事实**：冲突来自"包内既有声明"时，
Analyzer 也会报 `GXML-ROOT-002`（它加载目标包时看到 `redeclared`），但要等
阶段一跑完才出现。预检的不可替代之处是**批内冲突**——两个产物都还没写盘，
磁盘上没有冲突可加载，Analyzer 不会报错。

#### 10.7.7 范围边界与遗留

- 只做 C4：**接线 + 零副作用 + 三项前置**；
- **不合并 `master`**、不改写已推送历史（仍落后 1 笔 `27aa03a`）；
- **不收窄** text cache 的保守上界（归 C5）；
- 遗留（登记未处理）：预检不校验"目标包能否成功加载"，因此
  `001` 与 Analyzer 的 `GXML-ROOT-002` 在"包内既有声明"场景会重叠——
  当前以预检为准（更早、码更明确），C5 决定是否去重。

#### 10.7.8 独立复审（针对 `b7754a4`）与修复（2026-10-22）

复审结论：**需要修正后再复审，暂不批准 C4 验收**。四条发现经逐条核对**全部属实**，
其中 R1/R2 是我自己的**注释与代码不一致**（文档失真），比功能缺陷更需要纠正。

| 编号 | 问题 | 处理 |
|---|---|---|
| **R1**（P1） | CLI 诊断输出手拼 `%s: %s: %s`，缺 `:0:0:`，未走统一格式化入口 | 阶段一与预检**两处**都改为 `fmt.Fprintln(stderr, dg.Error())`（`contracts.CompilerError.Error()` → `file:line:col: CODE: message`）；CLI 测试改为精确断言 `:0:0: CODEGEN-NAME-001:` / `-002:`，不再只查码前缀 |
| **R2**（P1） | `isOwnArtifact` 只归一化右侧，与注释、卡、提交说明所称"两侧都过"不符 | 改为**两侧都过** `normalizeSourcePath`；新增"保护头为原生分隔符形态"用例专测左侧；双侧变异各自有专属红灯（见 §10.7.9） |
| **R3**（P2） | `TestNamingSameDirCollapseStillConflicts` 用 `t.Skip` 兜底 → 能力丢失时会静默通过 | 去掉 Skip，改为硬断言必须出现 `namingConflictBatch`；同类 fail-open 的 `TestNamingDetectsPascalCollapse`（C1 遗留）一并去掉 `t.Skipf`，前提不成立即 `t.Fatalf` |
| **R4**（P2） | 排序键只到 `Owner`，三个以上输入撞同符号时等价元素顺序无规定 | 补第四级键 `Source`；新增三源塌缩用例（声明顺序**刻意与 Source 字典序相反**），判据为"同 (Symbol,Kind,Owner) 组内 Source 升序"+ 重复运行结果一致 |
| 补充 | `textCacheTypeName` 也用 `t.Skip` | 改 `t.Fatalf`：绑定页产物按 §10.7.4 必须声明 `goxmlTextCache<Source>`，找不到即契约/实现变更，须响亮失败要求复核 |

复审对"接线与零副作用"的判断（位置、只查 miss、按 TargetDir 分组、静态页误报、
产物/Manifest/缓存三项断言）与本卡一致，未要求改动。

**复审提出的正确提醒（已接受）**：预检自身只读 ≠ 整个失败路径零副作用，
必须放回 CLI 真实调用链验证。C4 的 `TestNamingConflictNoWrites`（产物+mtime、
Manifest 字节、缓存目录集合与内容、stdout 无「已生成」）与
`TestNamingConflictDoesNotCreateOutDir`（不得创建输出目录）正是这条的落地，
但**完整失败矩阵（S1–S5 全项）仍属 C5**，本卡不宣称已完成。

#### 10.7.9 复审修复的变异取证（2026-10-22）

| # | 变异 | 结果 |
|---|---|---|
| 6 | 预检诊断输出改回手拼三元组 | **3 红**（NoWrites / SameBatchRejected / StaticPageOverreport，均锁精确格式） |
| 7 | `isOwnArtifact` 去掉**左侧**归一化 | **1 红**（`TestNamingOwnArtifactNormalizesHeaderSide`） |
| 8 | `isOwnArtifact` 去掉**右侧**归一化 | **2 红**（`UsesNormalizedSourcePath` / `DiagnosticCodesF06`） |
| 9 | 排序去掉 `Source` 键 | **1 红**（`TestNamingConflictOrderIsFullyDetermined`，报 4 条同组逆序） |

每组 `go build` 退出码均为 0（变异本身可编译），恢复后 SHA-256 与变异前一致。

#### 10.7.10 二轮独立复审结论（针对 `e971dad` / `7b86fc2`，2026-10-22）

结论：**R1–R4 与 `textCacheTypeName` 的 fail-open 修复均通过静态代码复核**，
未发现阻止本轮修复通过的新代码问题。

| 项 | 复审判断 |
|---|---|
| R1 诊断格式 | 通过：两处统一 `dg.Error()`；测试锁 `:0:0: CODEGEN-NAME-001:` / `-002:` |
| R2 双侧归一化 | 通过：头侧测试构造真实反斜杠头 + 正斜杠调用；左右变异分别覆盖 |
| R3 去 fail-open | 通过：两处硬断言 + `textCacheTypeName` 直接失败 |
| R4 排序确定性 | 通过：四级键；三源测试反转声明顺序，能检出漏 `Source` 的回归 |

**一处非阻塞文档核对已处理**：我口述的 codegen `701.849s` / CLI `147.479s`
与卡/dev-log 记录的 `706.869s` / `135.198s` 来自**不同命令**（全量 vs 单包），
两组都是真值。已在 §10.7.6 加"耗时口径"说明、dev-log §5.11 给出逐条对照，
并明确：**耗时只记录"该命令跑过且通过"，不可横向比较、不可当契约值**。

**边界（照此执行）**：

- C4 的结论是"**可提交项目负责人作最终验收裁定**"，本卡**未**标"验收通过"；
- 复审**未亲自重跑** Go 测试，测试门禁以已记录的实测证据为准——
  文档不写成"复审已复跑"；
- **C5 未开始**：`TextCache` 保守上界收窄（`bindSlots > 0`）、完整 S1–S5
  副作用矩阵、契约 §5/§16.5.1 同步；
- **不合并 `master`**（仍落后 1 笔 `27aa03a`）。

### 10.8 项目负责人最终验收裁定（2026-10-22）

**裁定：C4 通过，附带一项非阻塞文档事项。**

#### 10.8.1 验收依据

| 验收项 | 裁定 | 依据 |
|---|---|---|
| R1 诊断格式 | 通过 | 两处统一 `dg.Error()`；测试锁 `:0:0: CODEGEN-NAME-001:` / `-002:` |
| R2 路径归一化 | 通过 | 头侧与调用侧都归一化；专用测试覆盖分隔符差异 |
| R3 消除 fail-open | 通过 | 原 `t.Skip` 改硬断言，关键测试不能被跳过 |
| R4 冲突排序确定性 | 通过 | 排序补 `Source`；三源测试覆盖排序键遗漏 |
| 测试门禁证据 | 接受已记录结果 | codegen 28 PASS / 0 SKIP、CLI 6 PASS / 0 SKIP；全量 14 包通过（**已记录实测**） |
| 文档与状态记录 | 基本通过 | 耗时口径与复审边界已登记，章节结构正常 |

**证据边界（须保持这个说法）**：项目负责人核对的是**远端提交、diff 与文档**，
**没有**亲自重新运行 Go 全量测试。因此全量测试结论是"依据已有实测记录接受"，
**不是**"本轮重新实测通过"。本卡后续引用一律保持这一区分。

#### 10.8.2 非阻塞文档事项：命令文本统一（已处理）

裁定指出一处逐字差异：任务卡把 CLI 单包命令记为
`go test -count=1 ./internal/cli/`，而 dev-log §5.11 记为
`go test -count=1 -timeout 20m ./internal/cli/`。两者结果相同，但若要求逐字记录
实际执行命令，应以后者为准。

处理：**以 dev-log §5.11 为命令文本的权威形态**（逐字对应实际执行），
本卡 §10.7.6 已按实际命令逐行拆开重写，dev-log §5.0/§5.5/§5.9 的简写也一并补全。

#### 10.8.3 裁定后的状态与边界（不得外推）

- **C4：最终验收通过。**
- **C5：未开始。** `TextCache` 保守上界收窄（判据 `bindSlots > 0`）、
  完整 S1–S5 副作用矩阵、契约 §5 / §16.5.1 同步，仍全部属于 C5。
- **`master`：不合并。** 当前分支领先 20 笔、落后 1 笔（`27aa03a`）；
  本次验收**不改变**这一分支治理决定。
- **命名流程整体：尚未完成。** C4 通过不代表 C1–C5 全链条闭环
  （C1 预检仍未单独验收，见 §10.4.5）。
- 本次状态更新是**文档-only**：不重跑全量测试，也**不得**把 docgate / diagdoc
  这类文档检查描述成"代码测试通过"。

### 10.9 C5 开工与实现：TextCache 收窄 + 矩阵收口（2026-10-22，dev-log §6）

**状态：实现完成，待独立复审。本节不宣称 C5 通过。**

#### 10.9.1 开工前的现状核对（不照抄本卡的声称）

逐项对照 §8.1 的"计划测试"列与仓库实际函数名，结论：

- **8 项里 7 项的计划名从未创建**（只有 `TestGenerationDeterminism` 存在），
  实现用了别的名字；
- **A7 是唯一确实缺失的项**。

已按行为等价重映射 §8.1 表（表后附说明），避免"照旧挂计划名 → 读者误判
A1–A5/A7 都没做"。

#### 10.9.2 S4 的断言陷阱（本轮发现）

`共 N 个文件生成成功` 只在写入文件数 **> 1** 时打印（`internal/cli/pipeline.go:208`）。
在 C4 的单文件用例里补"不得出现该行"是**恒真断言**——不会红，也证明不了
§8.2 S4 想证明的事。

处理：新增 `TestNamingConflictStdoutSilentMultiFile`，先做**正对照**（两源成功 →
该行必须出现），再触发冲突断言两行文案都不出现。§8.2 表后已加说明。

#### 10.9.3 收窄机制：判据已冻结，机制需裁决（已裁决 A）

卡冻结的是判据 `bindSlots > 0`；`bindSlots` 本身是 generator 私有累加字段，
预检发生在任何解析之前，因此"槽位数从哪来"是架构选择。三选项与代价：

| 方案 | 做法 | 代价 |
|---|---|---|
| **A（项目负责人裁决）** | 抽共享 `CountBindSlots`，预检侧自行解析 | 冷路径多一次前端；**不动 §6.1 形状、不动诊断顺序** |
| B | 纯编译上移到预检之前 | 零额外解析，但改 §6.1 冻结结构 + 诊断顺序变化 |
| C | 不收窄，写进契约 | 把"合法工程生成不了"合法化 |

#### 10.9.4 实现要点

| 文件 | 改动 |
|---|---|
| `codegen/slots.go`（新增） | `textBindSlot`（**唯一**占槽规则）+ `CountBindSlots`（纯遍历） |
| `codegen/codegen.go` | `nodeExpr` 的 Text 分支改调 `textBindSlot`，与预检共用规则 |
| `codegen/naming.go` | `NamingTarget` 增 `BindSlots`/`BindSlotsKnown`；`generatedPackageSymbols(sourcePath, slots, known)` 三态 |
| `pipeline/pipeline.go` | 前端拆出 `Prepare`/`Prepared.Slots()`/`RunPrepared`；`Run` 签名不变 |
| `cli/pipeline.go` | 阶段零 C 每个未命中输入 `Prepare` 一次并存 `preparedFE`，阶段一复用；`Slots()` 失败 → 保守回退 |

**前端复用不是优化洁癖**：双跑前端的实测代价是 3 文件 +0.86 s（≈+285 ms/文件，
+40%，主因 `go/types` 环境构造）。复用后回到基线（中位数 1770 ms vs 收窄前 2162 ms，
差异在噪声量级，不主张"更快"）。

#### 10.9.5 护栏与变异（7 组，`go build` 退出码均为 0）

- 护栏 `TestSlotCountAgreesWithDeclaredTextCache`：断言「槽位为零 ⟺ 不声明 TextCache」
  互为充要。**它首次运行即抓出真分叉**：`<If>` 的分支在 `If.Then/Else` 而非
  `Children`，只走 `Children` 会低估槽位 → 漏报。
- 变异 M1–M7 见 dev-log §6.7：分别为 If 不遍历、For 不传播、永不收窄、
  去未知回退、多发射类型、保护头源名变异、前端失败提前返回。
  M3 同时证明 codegen 层与 CLI 层都能抓到"永不收窄"；M4 钉住"去掉回退"方向。

#### 10.9.6 边界（照此登记，不外推）

- **A7 只部分覆盖"防止顺手改坏"**：归一化字节比对抓不到**全局形状变化**
  （两侧一起变仍相等）。要抓那类需要签入 golden（契约 §6 要求），本仓**没有**
  golden 机制，本轮不新建。
- **C5 实现完成 ≠ 命名流程完成**：C1 预检仍未单独验收；C5 尚未独立复审；
  本卡不宣称整链闭环。
- 未处理（C4 已登记的遗留）：`binding_compile_test.go`/`binding_runtime_test.go`
  的 `go build ./<dir>/...` 通配空转面；`001` 与 `GXML-ROOT-002` 是否去重。
- 本轮顺带修了测试基建：`tempProjectInRepo` 先 `RemoveAll` 再建（否则被
  SIGTERM 掐断的残留会让下一次运行在预置阶段假红）。

## 11. 纪律

- 先改契约再改实现；本卡 §4–§7 冻结后不得在实现中静默放宽；
- 每条断言先证明能变红（临时破坏检测逻辑），再让它变绿；
- 不在本卡修其它 Codegen 问题（诊断码目录、命名风格、import 别名各自归口）；
- 共享工作树禁止 `git add -A`；提交前核对暂存列表。
