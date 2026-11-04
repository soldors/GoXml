# 2026-11-04 R12-A GENERATOR-CACHE-IDENTITY（DeepSeek）dev-log

基线 `b12980c1c939892cb90221a620121e9070f55ef3`；树 `product-r12-deepseek`，分支 `codex/product-r12-deepseek-20261104`。
本线只改 `internal/cli` 的缓存键/记录/命中判定与测试，未改 Compiler 生产代码/契约/scripts/examples；未新增 CLI 选项或退出码。

## 0. 三个版本各管什么（先厘清）

| 标识 | 位置 | 管什么 | 是否因"生成器输出规则变化"而变（基线） |
|---|---|---|---|
| **GXML 语言版本** `codegen.GoxmlVersion="1"` | 保护头 `// Goxml-Version: 1`、Manifest 条目 `compilerVersion` | .gxml 语法/语义的对外版本 | ❌ 不该变（语言未变） |
| **缓存格式版本** `cacheFormatVersion=7`（K7） | 缓存记录 JSON 的 `version`、键字段 `cacheFormat` | 缓存**记录/键的结构与规范化** | 只在记录结构变化时提升 |
| **生成器版本** K4 = `cacheKeyInput.CompilerVersion` | 缓存键；记录 `compilerVersion` | 本应管"生成器输出规则" | ❌ 基线实际取 `GoxmlVersion`（=1） |

**基线的错配**：K4（`cache_wire.go:258`、`cache_store.go:221`）填的是 `codegen.GoxmlVersion`，即**语言版本**。
于是"生成器输出规则变了、语言版本没变"时缓存键不变 → 假命中。历史上遇到这种升级，只能靠手动提升
`cacheFormatVersion`（v3/v4/v5/v6/v7 注释可见）或将就 `--no-cache`——把"记录格式"和"输出规则"混为一谈。

## 1. 缺口证明（真实两 CLI 对照，raw/A1-gap-proof.json）

- `CLI_base`：基线源码构建。
- `CLI_probe`：**同一基线**的 detached worktree，只改 `codegen.headerFor` 在保护头后**多写一行注释**
  （`// generator-revision-probe: B`），`GoxmlVersion` 保持 `"1"`；构建后丢弃该 worktree。
  保护头前三行不变（`HasProtectedHeader` 只看前三行），因此所有权判定不受影响，但**产物内容字节变了**。
- 结果：
  - `CLI_base` 生成 → 产物无 probe 行（sha `010dff…`）。
  - `CLI_probe` 对**同一项目、缓存记录有效**再生成 → **`缓存命中`**、产物**未变**、仍无 probe 行
    （stdout：`跳过 ui/home_gen.go（缓存命中）`）。
  - `CLI_probe --no-cache` → 才出现 probe 行、产物变化。
- 结论：**缺口成立**——生成器输出规则升级后在有效缓存下沿用旧产物，用户必须手动 `--no-cache`。

## 2. 修复：内部生成器输出修订标识接入 K4

`internal/cli/cache_key.go`：
- 新增 `generatorOutputRevision = "gen-1"`（内部标识，注释说明何时必须提升：生成代码结构/模板、
  默认常量/样式/布局/占位值、绑定/事件接线形态、任何使同一输入产出不同**内容字节**的改动；
  不必提升：只改文案/日志/注释、只改 CLI 判定逻辑而不改产物字节、GXML 语言版本变化）。
- `cacheIdentity()` = `GoxmlVersion + "+" + generatorOutputRevision`，作为 **K4 的取值**。
- 只改**取值**、**不改键字段结构与规范化** ⇒ 按 K7 的定义（记录/键结构）**不提升** `cacheFormatVersion`（仍 7）。
- 首个取值 `gen-1` 有意 ≠ 旧值 `"1"` ⇒ **存量缓存一次性安全 miss**（正是本任务目标）。
- 另有 `cacheIdentityOverride`（**仅测试用**接缝，生产恒空），用于端到端模拟"输出规则升级"。

## 3. 接线登记（便于与 B 线组合）

| 文件 | 函数/位置 | 改动 |
|---|---|---|
| `internal/cli/cache_key.go` | 新增 `generatorOutputRevision`、`cacheIdentity()`、`cacheIdentityOverride`；`cacheKeyInput.CompilerVersion` 注释 | K4 取值来源 |
| `internal/cli/cache_wire.go` | `cacheKeyFor`（`CompilerVersion: cacheIdentity()`）、`explainMiss`（`prev.CompilerVersion != cacheIdentity()`） | K4 接入键/原因 |
| `internal/cli/cache_store.go` | `newCacheRecordFor`（记录 `CompilerVersion: cacheIdentity()`） | 记录里存 K4 取值 |

**未改**：`manifest.go` 的 `CompilerVersion`（= 语言版本，G3 与保护头同源）、`pipeline.go:360` 的
Manifest 条目写入、保护头格式、`GoxmlVersion`、`cacheFormatVersion`。

## 4. 真实 CLI 验收（raw/A3-e2e.json，全 PASS）

两 CLI：`goxml-base.exe`（基线，K4="1"）/ `goxml-new.exe`（含修复）。

| 步骤 | 期望 | 实测 |
|---|---|---|
| 旧 CLI 生成（存量缓存） | 记录 `compilerVersion="1"`, `version=7` | ✅ |
| 新 CLI 同项目首次 | **安全 miss**（`缓存未命中 … compiler-version-changed`），非命中；记录增至 2 且新记录含 `+gen-` | ✅ |
| 新 CLI 第二次 | **真命中**；产物字节 / mtime / Manifest 全不变 | ✅ |
| 升级后生成失败（目标包编译失败 + `--check`） | 非零退出；旧产物字节/mtime、Manifest、缓存记录数**全部保留**；无"已生成" | ✅ |
| 保护头 | `// Goxml-Version: 1` 前后一致 | ✅ |

## 5. 判别回归（internal/cli/cache_identity_r12_test.go）

- `TestCacheIdentityComposition`：K4 = 语言版本 + 修订；`GoxmlVersion=="1"`；`cacheIdentity()!=GoxmlVersion`；`cacheFormatVersion==7`。
- `TestCacheKeyChangesWithGeneratorRevision`：仅修订不同 ⇒ 键不同。
- `TestGeneratorRevisionUpgradeSafeMissThenHit`：端到端 upgrade ⇒ 安全 miss（非命中）⇒ 写新记录 ⇒ 同修订再跑真命中（mtime/size/Manifest 不变）。
- `TestUpgradeGenerationFailurePreservesOldArtifacts`：升级后 `--check` 失败 ⇒ 旧产物/Manifest 保留、无"已生成"。

**变异红**：把 `cacheIdentity()` 改成只返回 `codegen.GoxmlVersion`（模拟未接线）后，
`TestCacheIdentityComposition`（`cacheIdentity 应含输出规则修订: "1"`）与
`TestGeneratorRevisionUpgradeSafeMissThenHit` 立即失败；恢复后全绿。

## 6. 门禁

`go test ./internal/cli -count=1 -cover` = **86.5%**（≥85%）；`go build ./...` ok；`go vet` rc0；`gofmt -l` 空。

## 7. 五态

实现 done / 自测 pass / 外审 PENDING（ZCode 审 A）/ 主仓未合 / 远端未推送。
