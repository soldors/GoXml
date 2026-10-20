# TASK-GENERATE-CACHE-001：增量生成与缓存（契约 §11 落地）

## 元信息

- 状态：**边界已冻结，待实施（2026-10-20）**
- 前置：`TASK-GENERATE-CLI-001` ✅（Manifest、`--clean`、`--no-cache` 解析均已就位）；
  其 1-7 的注记写明「增量缓存本体未实现，等价性由确定性测试保证」——本卡补这个缺口
- 契约依据：[代码生成输出契约](../../03-架构/代码生成输出契约.md) **§11（缓存键/命中条件）**、
  §10（Manifest）、§12（CLI 行为）、§13 第 12/13 条
- **与 `TASK-INPUT-001-D5-F` 完全独立**：本卡完成不构成 Input 阶段关闭（见 §9）

## 1. 为什么本卡不能退化成「只比 `inputHash`」

当前 Manifest 的一条登记只有四个字段【项目证据 `internal/cli/manifest.go:34-43`】：

```json
{ "source": "...", "output": "...", "inputHash": "...", "compilerVersion": "..." }
```

而 `inputHashOf` 只是 **`.gxml` 源字节的 SHA-256**【`manifest.go:117-121`】。
契约 §11 要求的缓存键**至少**六项，其中四项不在 `inputHash` 里：

| 契约 §11 要求 | 当前是否有 | 缺失后果（漏掉会「假命中」） |
|---|---|---|
| 当前 `.gxml` 内容 | ✅ `inputHash` | — |
| 传递引用的 `<Def>` 内容 | ✅ 被源字节覆盖（v1 的 `<Def>` 只在同文件内） | 将来支持跨文件 Def 时必须显式扩键 |
| 目标 Go package 的相关源码/类型指纹 | ❌ 无 | 包被改名/搬包后仍判命中 |
| 编译器（生成器）版本 | ✅ `compilerVersion`（`codegen.GoxmlVersion="1"`） | — |
| CLI 选项 | ❌ 无 | 换 `--out-dir`、不同包名仍判命中 |
| 输出 package 路径 | ⚠️ 仅在 manifest 的 `output` 里，未参与比较 | `--out-dir` 变化后旧缓存被复用 |

**结论**：必须先定义完整键，再谈命中。只比 `inputHash` 会把「跳过」变成「跳过且可能错」。

## 2. 冻结：缓存键（K1–K7）

键 = `SHA-256` 对**规范化序列**求值；每个分量按固定顺序写入
`[4 字节大端长度][标签字节][值字节]`，避免拼接歧义（例如 `("ab","c")` 与 `("a","bc")`）。
**全部使用项目相对路径（`/` 分隔），禁止绝对路径**——否则同一项目在不同磁盘位置不能复用缓存。

| 分量 | 内容 | 为何参与 | 获取方式 |
|---|---|---|---|
| K1 源内容 | `.gxml` 字节的 SHA-256 | 语言输入 | 已有 `inputHashOf` |
| K2 `<Def>` 内容 | v1 视为被 K1 覆盖 | 字段展开影响产物 | 同 K1；**跨文件 Def 尚未支持，支持时必须拆出 K2** |
| K3 包源码指纹 | 目标包目录内 `*.go`（**排除** `*_test.go` 与「已登记/本批计划」的生成产物）按相对路径排序后逐文件喂入 | 包名解析来自包源码；契约 §11 要求 | 新增 |
| K4 生成器版本 | `codegen.GoxmlVersion` | 生成逻辑变化必须失效 | 已有 |
| K5 CLI 选项 | 解析出的 `packageName`（含默认推导结果）；`Check` 不影响产物故不入键 | 包名直接写进产物 | 新增 |
| K6 输出路径 | 产物项目相对路径 + 头里 `SourcePath` | 保护头 `// Source:` 是产物内容 | 新增 |
| K7 缓存格式版本 | `cacheFormatVersion = 1` | 缓存语义演进 | 新增 |

### 2.1 两个必须写进卡的陷阱

**T1：K3 若包含「本轮刚写出的产物」，命中率恒为 0。**
生成后再跑，包目录里多了 `<name>_gen.go`，若它进入 K3，第二次运行键必变。
→ 冻结：K3 的排除集 = Manifest 已登记的全部 `output` ∪ **本批计划写入的 target**。
（只排除「已登记」不够：Manifest 在提交后才更新，本批刚写的产物当时还没登记。）

**T2：K3 会因「改同包任意 `.go`」而频繁失效。**
这是契约 §11 要求带来的**保守方向**成本：多失效、绝不误命中。
本卡不因此擅自收窄键——是否收窄见 §10 的 D-1，需用户裁决。

## 3. 冻结：命中条件（全部满足才算命中）

| # | 条件 | 不满足时 |
|---|---|---|
| H1 | Manifest 有该 `source` 登记，且 `output`/`compilerVersion` 与本次一致 | 重新生成 |
| H2 | 缓存文件存在、可解析、`cacheFormatVersion` 匹配 | 重新生成（坏缓存 best-effort 删除） |
| H3 | 缓存记录的 `keyHash` == 本次计算的键 | 重新生成 |
| H4 | 目标产物**存在** | 重新生成 |
| H5 | 产物 SHA-256 == 缓存记录的 `artifactHash` | 重新生成（判定为「产物被改」） |
| H6 | 产物保护头合法，且 `// Source:` 与本次一致 | 见下 |

**H6 的分支裁决（关键）**：

- 保护头合法但正文被改（H5 不符）→ **重新生成并覆盖**：保护头存在即表示工具拥有所有权
  （与 `--clean` 的「保护头不符 = 用户接管」互为镜像）。
- 保护头**不符**（用户接管该文件）→ **错误退出，不覆盖**。
  契约 §13 第 3 条「用户手写同名文件不会被覆盖」目前**未在覆盖路径落地**
  【项目证据 `internal/cli/commit.go` 的 rename 覆盖无保护头检查】，
  本卡把它落地为显式错误（见 §10 的 D-3：这是行为变更，需用户确认）。

**损坏的不对称必须保留**：

| 对象 | 损坏时行为 | 理由 |
|---|---|---|
| Manifest | **硬失败**，禁止任何删除（既有安全锁） | Manifest 决定所有权与清理范围 |
| 缓存 | **安全回退**：视为未命中，重新生成，不改变退出码 | 缓存只是提速，不能成为新的失败源 |

## 4. 冻结：写入顺序与原子性

```text
逐文件：生成（miss）→ 暂存 tmp
批次：全部成功后 rename 提交（既有两阶段事务，全或无）
→ Manifest 原子保存（既有）
→ 缓存逐条原子保存（tmp + rename，文件名 = <keyHash>.json）
```

- **顺序不可倒置**：先产物、后 Manifest、最后缓存。
  任一步中断，最坏结果是「产物已更新但无缓存」→ 下次未命中重新生成（安全）；
  反之（先写缓存后写产物）会留下**指向旧产物的有效缓存** → 假命中（禁止）。
- 缓存写入失败 → `stderr` 警告 + 生成仍算成功（退出码 0）；不因缓存失败让构建失败。
- 缓存目录 `.goxml/cache/`（契约 §11）；加载只认 `<64位 hex>.json`，忽略 `*.tmp` 残骸。
- 整个 `.goxml/cache/` 可随时删除，生成结果不变（契约 §11 明文）。

## 5. 冻结：可观测性（稳定词表，供测试断言）

| 场景 | stdout/stderr | 说明 |
|---|---|---|
| 命中 | `跳过 <rel-output>（缓存命中）` | 契约 §12「输出明确的 skipped 状态」 |
| 未命中 | `缓存未命中 <rel-output>：<reason>` | reason 取下面枚举 |
| 生成 | `已生成 <rel-output>` | 沿用现有措辞 |
| 汇总 | `命中 N，生成 M`（仅在 N>0 时追加） | 避免改变既有输出基线 |
| 缓存写失败 | `stderr: 缓存写入失败（不影响生成结果）：<err>` | 不静默 |

`reason` 枚举（冻结，测试按此断言）：`input-changed` / `package-sources-changed` /
`compiler-version-changed` / `options-changed` / `output-missing` / `output-modified` /
`output-user-owned`（此项目前是错误而非未命中）/ `cache-missing` / `cache-corrupt`。

退出码：全命中 → 0；任一生成失败 → 非 0；坏缓存/缓存写失败 → 仍 0。

## 6. 冻结：`--no-cache` 与 `--clean`

- `--no-cache`：**不读**缓存、**照样写**缓存与 Manifest（契约 §12 原文）；
  输出只出现 `已生成`，不出现 `跳过`。
- `--clean`：语义不变，只按 Manifest 清理孤儿产物；**不清理缓存**（本卡不扩大到缓存清理）。
- `--check`：当前实现与默认行为相同【`internal/cli/pipeline.go:30`】，本卡不改其语义；
  记录一条约束：**将来实现校验时，命中不得跳过校验**（契约 §11 末句）。

## 7. 验收矩阵

| # | 场景 | 构造 | 断言 | 计划测试 |
|---|---|---|---|---|
| C1 | 首次生成 | 空目录 | 生成成功，Manifest 与缓存各 1 条，`keyHash` 可复算 | `TestCacheFirstGenerate` |
| C2 | 相同输入再跑 | 紧接 C1 | 命中；**产物 mtime 与字节不变**；输出含 `跳过` | `TestCacheHitKeepsMtime` |
| C3 | 改 `.gxml` | 追加一个字符 | 未命中且 reason=`input-changed`；产物更新 | `TestCacheInputChange` |
| C4 | 改同包 Go 源码 | 新增/编辑同包 `.go` | 未命中且 reason=`package-sources-changed` | `TestCachePackageSourcesChange` |
| C5 | 改 `--out-dir` | 换输出根 | 未命中；产物落在新路径；旧登记按 §10 语义保留 | `TestCacheOutDirChange` |
| C6 | 删产物留缓存 | 删 `_gen.go` | 未命中且 reason=`output-missing`；重新生成 | `TestCacheOutputMissing` |
| C7 | 缓存损坏 | 写入非法 JSON | **不报错**、未命中 reason=`cache-corrupt`、重新生成、坏文件被删 | `TestCacheCorruptFallsBack` |
| C8 | Manifest 损坏 | 写入非法 JSON | **仍然硬失败**（既有安全锁不被缓存弱化） | `TestCacheManifestCorruptStillFails` |
| C9 | 手改产物正文（留头） | 改一行 | 未命中 reason=`output-modified`，产物被重写 | `TestCacheOutputModifiedRebuilds` |
| C10 | 用户接管（改头） | 改保护头 | **错误退出**、文件不被覆盖（落地 §13 第 3 条） | `TestCacheUserOwnedIsNotOverwritten` |
| C11 | 生成器版本变化 | 手写 Manifest 的 `compilerVersion` 为 `0` | 未命中 reason=`compiler-version-changed` | `TestCacheCompilerVersionChange` |
| C12 | `--no-cache` 等价性 | 两个隔离目录分别跑 | 产物**逐字节相同**；`--no-cache` 也更新了缓存 | `TestCacheNoCacheEquivalence` |
| C13 | 失败不留缓存 | 构造 Parser 错误 | 退出非 0；**无新缓存、无新产物**；Manifest 不变 | `TestCacheNoWriteOnFailure` |
| C14 | 同包多文件批次 | 同目录两个 `.gxml` | 连跑两次**都命中**（验证 T1 排除集正确） | `TestCacheMultiInputSamePackageHits` |
| C15 | 混合批次 | 两个 `.gxml`，只改一个 | 一个命中一个生成；批次仍原子 | `TestCacheMixedBatchAtomic` |
| C16 | 跳过是真的跳过 | 注入 `pipeline.Hooks.Generate` 计数器 | 命中时**计数为 0**；未命中时为 1（不靠时间推断） | `TestCacheHitSkipsCodegen` |
| C17 | 删整个缓存目录 | `rm -rf .goxml/cache` | 全未命中、生成结果与之前一致（契约 §11） | `TestCacheDirRemovalIsHarmless` |

契约映射：C2→§13-12；C12→§13-13；C1/C3→§13-1（确定性）；C8→§10；C16→本卡性能主张的硬证据。

## 8. 性能分账（与正确性分开记录）

**命中路径仍然要做的工作**（不许省略）：读 `.gxml`、读产物算 SHA-256、读包内 `*.go`
算 K3、读 Manifest、读缓存。
**省掉的工作**：Parser → Expander → Analyzer → Codegen（含 gofmt）→ 暂存写 → rename。

要求：

1. **证明「跳过」用计数器**（C16），不用耗时推断——耗时受磁盘缓存影响，不能证明阶段未执行。
2. 记录 1 / 10 / 50 个 `.gxml` 的 miss 与 hit 墙钟时间（同机、`-count=1`、取中位数），
   以及命中路径读取的字节数。
3. **如实记录反例**：当包很小且 `*.go` 很少时，命中路径的 I/O + 两次 SHA-256 可能
   接近甚至超过生成耗时。若实测收益不显著，结论就写「收益有限，理由是 X」，
   **不得**用「缓存命中」四个字宣称优化有效。
4. 缓存维护成本单列：每次 miss 多写一个 JSON（大小量级）、Manifest 不变。

## 9. 与 D5-F 的边界（不可合并关闭）

| 项 | 归属 | 关闭条件 |
|---|---|---|
| 增量生成正确性/性能 | 本卡 | 本卡 §7 全绿 + §8 数据入账 |
| AC-002-24 IME 组合输入 | `TASK-INPUT-001-D5-F` | **人工**：真实窗口 + 中文 IME 的组合/确认/取消 |
| 焦点、连续输入、程序外部更新 | D5-F | **人工**：`go run ./examples/demo` |
| Demo 冒烟 | D5-F | 人工运行并记录环境与未覆盖场景 |

**明令**：本卡自动化全绿**不得**用于把 AC-002-24 改成「通过」；
Input 阶段的关闭条件仍以 D5-F 的独立记录为准。

## 10. 待用户裁决（实施前必须有结论）

| # | 问题 | 选项 | 建议 |
|---|---|---|---|
| D-1 | K3 包源码指纹范围 | (a) 全部非测试 `*.go`（保守，改同包任意文件即失效）(b) 仅被引用类型的文件（需类型分析）(c) 不纳入（违反 §11） | **(a)**：简单、保守、可自证；代价是失效偏多 |
| D-2 | 产物被手改但保护头仍在 | (a) 重新生成覆盖 (b) 报错交给用户 | **(a)**：保护头即所有权标记，与 `--clean` 镜像 |
| D-3 | 用户接管（保护头被改）时 | (a) 错误退出不覆盖 (b) 跳过并警告 (c) 覆盖 | **(a)**：落地契约 §13 第 3 条；属行为变更，需确认 |
| D-4 | 缓存写失败是否影响退出码 | (a) 警告 + 0 (b) 失败 | **(a)**：缓存不得成为新失败源 |
| D-5 | `--clean` 是否顺带清缓存 | (a) 否 (b) 是 | **(a)**：缓存可整目录删除，无需命令 |

## 11. 实施切片（每片独立可验收）

| 切片 | 内容 | 验收 |
|---|---|---|
| B1 | 键计算与规范化（纯函数，无 I/O） | ✅ **完成（2026-10-21）** `internal/cli/cache_key.go`：`cacheKey`（K1–K7 + 长度自定界）、`validateKeyInput`、`fingerprintExclusion`（T1 排除集）。`go test -count=1 -run '^TestCacheKey' ./internal/cli/` 实测 PASS=10 / RUN=28；四条变异验证能变红（dev-log §83） |
| B2 | 缓存读写 + 损坏回退 + 原子写 | ✅ **完成（2026-10-21）** `internal/cli/cache_store.go`：`cacheRecord`/`cacheLoadStatus`、`loadCacheRecord`（missing/ok/corrupt 三态，损坏不报错且删残骸）、`saveCacheRecord`（tmp+rename）、`removeCacheRecord`、`validateKeyHash`。`go test -count=1 -run '^TestCacheStore' ./internal/cli/` 实测 PASS=10；五条变异验证能变红（dev-log §84）。**证据边界**：崩溃原子性无法进程内测，只覆盖其后果链 |
| B3 | 命中判定 + reason 枚举 + 跳过接线（含 T1 排除集） | `go test -count=1 -run '^TestCacheHit' ./internal/cli/` 与 `-run '^TestCacheMultiInput'`（**分开跑**，见 §11.1） |
| B4 | 可观测性输出与汇总 | `go test -count=1 -run '^TestCacheOutput' ./internal/cli/` |
| B5 | §7 全矩阵 + §8 性能数据 + 契约/运维手册同步 | `go test -count=1 ./internal/cli/` + 基准记录 |

### 11.1 验收命令本身不得空转（本轮新增约束）

**禁止**用 `\|` 拼多条测试名：

```text
# 错（空转报绿）
go test -run 'TestCacheHit\|TestCacheMultiInput' ./internal/cli/
```

PowerShell 单引号把 `\|` 原样传给 Go 正则，而 RE2 的 `\|` 是**字面量竖线**，
于是匹配 0 个测试、输出 `ok ... [no tests to run]`、**退出码 0**。
这就是 docgate 修过的「空转门禁」形态。

冻结规则：

1. 用单一前缀（`^TestCacheKey`）或**分成多条命令**跑；
2. 每次验收带 `-v`，核对 `--- PASS` 行数 ≥ 预期用例数，把计数写进 dev-log；
   只看 `ok` 不足以证明测试真的执行了。


## 12. 纪律

- 共享工作树禁止 `git add -A`；提交前必须 `git diff --cached --name-only` 核对。
- 门禁失败若是并发会话写入竞态，记录**首败与复跑**两段结果，不只报最终绿。
- 每条断言先证明能变红（临时破坏实现），再让它变绿；不得用「跑通了」替代「能失败」。
- 任何契约语义调整先改契约再改代码；本卡已冻结的部分（§2–§6）不得在实现中静默放宽。
