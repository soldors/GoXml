# 2026-11-02 R5-FOLLOWUP-B DeepSeek：缓存与孤儿清理收口（非作者审修）

- 工作树 `r5-followup-deepseek`，分支 `codex/r5-followup-deepseek`，基线 `5c387918e2cee3256f5671fdee095569c6a9bf8e`（核验 clean）。
- 角色：**非作者**审修 B 线（原作者 ZCode / maintenance-r2）缓存安全、有效构建上下文、孤儿清理记账，并补真实维护流程与文档候选。原作者树 `night-r5-zcode-20261102`、`night-zcode-maintenance-r2` 与主仓只读；仅写本树 `internal/cli/` 与本 dev-log。
- 只本地提交，未 push、未合主仓。

## 片1（`f2d6f11`）：有效构建上下文 + 依赖失效的保守复用

### 独立审到的真实漏洞（先行为反例，后修）
- **基线缺陷 [实测]**：`cacheKeyFor` 的 `BuildContext` 只读 `os.Getenv("GOOS"/"GOARCH"/"CGO_ENABLED")`。但 Go 的**有效**上下文还含 **GOENV 持久化值**（`go env -w`）。实测：`GOENV=<file with GOOS=js>` 时 `go env GOOS`=`js` 而 `os.Getenv("GOOS")`=`""`。→ GOENV 改 GOOS/GOARCH/CGO/GOFLAGS 时键不变 → **跨上下文假命中**。
- 依赖闭包收集失败基线直接 `return err`（整批失败）而非保守 miss；草稿方向（`dependencyUnsafe` 保守 miss）正确但 `measuredCacheContext` 缺 Go 路径/超时/失败策略。

### 修复（`internal/cli/{dependency_context.go,cache_wire.go,cache_key.go,pipeline.go}`）
- 新 `dependency_context.go`：`measuredCacheContext(goExe)` 用 `go env -json GOOS GOARCH CGO_ENABLED GOFLAGS GOWORK GOVERSION` 取**实际生效**值；**受控子进程**（`context.WithTimeout` 10s、显式 `resolveGoExe()`=LookPath、`GOTOOLCHAIN=local` 禁网）；失败/输出不全/go.work 工作区 → 返回 error（调用方 `dependencyUnsafe` 保守 miss）。
- `pipeline.go`：每批**只测一次**上下文（不每源起 go env 进程），批内共享；失败则批内全标 `dependencyUnsafe`。
- `cache_wire.go`：`resolvedInput` 加 `cacheContext/cacheContextReady/dependencyUnsafe/goExe`；`cacheKeyFor` 依赖失败不再整批失败而标 `dependencyUnsafe`；`decideCacheHit` 在 `noCache` 前 `dependencyUnsafe` 强制 miss（原因 `package-sources-changed`）。
- `cache_key.go`：`cacheFormatVersion` 3→4（键含义变化，v3/v2 旧记录安全 miss，无迁移）；同步更新 `dependency_fingerprint_test.go` 版本锚点至 v4（合法版本演进断言，非削弱）。

### 验证
- **单测 [实测]**：`cache_context_followup_test.go` 4 例绿——GOENV 持久化 GOOS=js 被读到、不同 GOENV 上下文不同、go 不存在→error、`dependencyUnsafe` 强制 miss。
- **真实 CLI E2E [实测]**（`repro/f1`，跨包 vm→ui，本树 CLI）：
  - 态1 初始生成+build rc0；**态2 二次命中** 字节/mtime/Manifest 全不变 + "命中"；
  - **态3 GOENV GOOS=js** → 上下文变 → 键变 → 未命中重分析（rc=2 暴露真实错误，**未假命中**）；**态3b GOARCH=arm64** → `缓存未命中…package-sources-changed`+重生成 rc0；
  - **态4 源 VM 改坏**（`Record(name int)`）→ rc=2 `GXML-EVENT-006`，旧 gen/Manifest 字节不变；态5 修好恢复 rc0+build0。
  - （`raw/slice1-e2e.json`）

## 片2（`cfec15c`）：孤儿清理多 cause 记账 + 部分失败保留已完事实

### 独立审到的真实缺陷（先行为红，后修）
- **缺陷A [实测红]**：多个 `remove` 失败只保留**最后一个** cause（`lastFailedCause`），`failed` 列多路径但前面失败原因 `errors.Is/As` 不可穿透。
- **缺陷B（片2 任务书重点）[实测红]**：`readFile`/源 `stat` 中途失败 → `return nil,nil,err` **丢弃此前已 deleted/skipped 事实**。
- 附带：pending 判定用 `containsStr(skipped, e.Output)`（skipped 存「路径 (原因)」文本、比纯路径恒不等）——安全性上不误排除、但检查失效；改纯路径集合更严谨（`skipSet/failSet/delSet`）。

### 修复（`internal/cli/{clean_txn.go,manifest.go}`）
- `cleanFailure.cause` → `causes []error`，`Unwrap() []error`（全部原因 errors.Is/As 穿透）；`Error()` 主展示首个 cause。
- `cleanOrphansOps`：逐项累积 `causes` + `firstFailedPath`；`readFile`/源 `stat` 失败改为返回携带 `deleted/skipped` 的 `*cleanFailure`（保留已完事实）；pending 判定改用纯路径集合。

### 验证
- **回归 [实测]**：`clean_followup_test.go`——多 remove 失败两 cause 均 `errors.Is`；readFile 失败保留已删事实 + cause 可 Is。修前两例红、修后绿；`Clean|Orphan|Manifest` 全量与 `internal/cli` 全包绿。

## 片3（本文件所在提交）：真实维护全链流程 + 文档候选 + 对原作者分级

### 真实 CLI 全链 [实测]（`repro/f3`，本树 CLI）
生成 a,b → 二次命中（字节/mtime 不变 + "命中"） → 删源 b 后 `--clean`（`已清理 ui/b_gen.go`、存活 a 保留） → 再生成 a 命中不变 → 用户接管（改写 a_gen.go 去保护头）`generate` 拒绝覆盖 rc=2 **用户文件保留**。（`raw/slice3-flow.json`）

### 维护使用说明候选（供协调者并入正式文档；本线不抢索引）
1. 生成：`goxml generate <源.gxml 或目录> [--out-dir <目录>]`（单输入；目录会批量）。
2. 缓存：`.goxml/cache/` 键覆盖 K1 源/K3 目标包源码/**K8 依赖闭包+模块身份**/**K10 有效构建上下文（含 GOENV 持久化 GOOS/GOARCH/CGO/GOFLAGS）**/K4 版本/K6 输出路径；删整个 cache 目录安全（只变慢）。
3. 清理：`goxml generate <存活源或目录> --clean` 删「源已消失 ∧ 保护头 Source 与登记一致 ∧ 无存活源争议」的孤儿；存活源永远保护。
4. 用户接管绝不删：保护头不全 / Source 与登记不符（歧义）/ 同输出被存活源争议。
5. 部分清理失败：非零退出码 + 四类清单（已删除/已跳过/本次失败/尚未执行），**全部失败 cause 可 errors.Is/As 穿透**；Manifest 失败路径不更新，重跑安全（已删按不存在跳过）。

### 对原作者（ZCode B 缓存 / maintenance-r2）的非作者评审分级
- **R5-B 缓存线（`3c3d122`/`4584899`/`70152d5`）**：**方向正确、可组合，但本线补 2 项真实缺口**——
  - K10 只读 `os.Getenv` 漏 GOENV 持久化值（**P1 已由本线片1修**）；
  - 依赖收集失败整批报错而非保守 miss（**P2 已由本线片1修**）。
  - 其余（K8 闭包有界/哨兵、模块身份、排除集、v3→v4 演进）审读与实测通过。
- **maintenance-r2（`c02f07f`）**：**方向正确、可组合，但本线补 2 项真实缺口**——
  - 多 remove 失败 cause 只留最后一个（**P2 已由本线片2修**）；
  - readFile/源 stat 失败丢弃已完成事实（**P2 已由本线片2修**）。
  - 其余（三条所有权判据、结构化四清单、Manifest 失败保留事实）审读与实测通过。
- **证据分级**：以上「通过/缺口」= 本线 **独立实测**（真实 CLI + 单测红→绿）；原作者报告为**自报**，未据其自证宣称通过。本线新增修复仅标 **待协调者/ZCode 独立复核**，不自报外审通过。

## 剩余边界 / 未做
- Background 覆盖区域 A/B、ThemeRef、Image、Router、Expanded、SCOPE-006、新公开 API/诊断号：未批准，未触。
- K10 的 CLI 级跨 GOOS 真构建（js 平台整链编译）未做（js 下包不可加载属预期重分析）；用 GOENV/GOARCH 键判别 + 真实 CLI miss 证据替代，已注明。
- CLI 单输入限制（多文件需目录）为既有语义，未改。
- 未做 race/跨平台真构建/性能验收；未过滤全仓测试未跑（仅 `internal/cli` 全包）。

## 提交
| 片 | SHA | 内容 |
|---|---|---|
| 1 | `f2d6f11` | 有效构建上下文 go env + 依赖 unsafe 保守 miss（v4） |
| 2 | `cfec15c` | 清理多 cause 记账 + readFile/stat 失败保留已完事实 |
| 3 | 本文件所在提交 | 维护全链流程 + 文档候选 + 原作者分级 |

五态：源实现 done / 自测 pass（真实 CLI + 单测 + cover 88.1%）/ 外审 PENDING（协调者/ZCode 复核本线新增修复）/ 未合主仓 / 未推送。
