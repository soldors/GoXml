# TASK-CODEGEN-NAMING-001：生成符号命名与包级冲突契约

## 元信息

- 状态：**已裁决（D-1~D-5）；C1 完成（预检）；C2 实现完成 + 验收补丁已补（见 §10.2），最终验收待独立复审——维护者已明确"暂不标记最终验收通过"；C3–C5 未开始**。CLI 报错路径仍归 C4，诊断码归 C3，均未完成
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
### 8.2 副作用验收（预置既有状态，再触发冲突）

| # | 预置状态 | 触发方式 | 断言 |
|---|---|---|---|
| S1 | 目标包已有一个 `_gen.go`（上一版产物） | 制造冲突后生成 | 该文件内容与 mtime 均不变 |
| S2 | Manifest 已登记若干条目 | 同上 | Manifest 字节不变 |
| S3 | `.goxml/cache/` 已有记录 | 同上 | 缓存目录文件集合与每个文件内容均不变 |
| S4 | 三者同时存在 | 同上 | 三者同时不变，且 stdout 不出现「已生成」「共 N 个文件生成成功」 |
| S5 | 无任何预置（首次） | 同上 | 目录内不出现新文件、不出现 `.tmp` 残骸 |

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
| C2 | 命名唯一化（N1），含 pascal 塌缩检测 | ✅ **实现完成（2026-10-22），验收补丁见 §10.2**：`sourceSymbolSuffix`/`generatedTypeNames`；类型名 `goxmlRenderKey<Source>` / `goxmlTextCache<Source>`；契约 §16.5.1 同步；红灯由「变异回固定名」取证（dev-log §97.1）；`go test -count=1 ./internal/compiler/codegen/` ok（124s）、全量 `go test ./...` exit 0。**+ 复核轮（dev-log §97.7/§97.8）**：补 `sourceSymbolSuffix` 直接断言与三形态/稳定性断言（两项此前无覆盖）；两次变异取证（退回固定名、破坏分隔符规则）；`cacheFieldLines` 单点变异复现两个基准失败并证伪"夹具问题"说 |
| C3 | 诊断码定义 + 目录登记 + diagdoc 门禁同步 | `go test ./internal/docgate/ ./internal/compiler/diagdoc/` |
| C4 | 零副作用接线：预检失败 → 不写盘/不改 Manifest/不写缓存（§8.2） | `go test -run TestNamingConflictNoWrites ./internal/cli/` |
| C5 | §8 全矩阵 + 契约 §5/§16.5.1 与规范同步 | `go test -count=1 ./internal/compiler/codegen/ ./internal/cli/` |

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
| 新增 8 用例定向跑 | PASS=8 |
| `go test -timeout 30m ./internal/compiler/codegen/` | ok 525.4s（复审实测 523.1s） |
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

## 11. 纪律

- 先改契约再改实现；本卡 §4–§7 冻结后不得在实现中静默放宽；
- 每条断言先证明能变红（临时破坏检测逻辑），再让它变绿；
- 不在本卡修其它 Codegen 问题（诊断码目录、命名风格、import 别名各自归口）；
- 共享工作树禁止 `git add -A`；提交前核对暂存列表。
