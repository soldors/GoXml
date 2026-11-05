# 2026-11-05 R22-A ANALYZER-OVERLAY-CONSISTENCY（DeepSeek）dev-log

基线 `39f45ea07fdbd1c2aaab4fff24a1c452e4773a11`（开工核验 clean，porcelain 实测 40；common-task 记 68，按准备时点口径差异登记）。分支 `codex/r22-deepseek-analyzer-overlay`，树 `r22-deepseek-analyzer-overlay`。只拥有 `internal/cli` 与直接回归、dev-log。

> 覆盖提交：`39f45ea`（docs: clarify routed gallery evidence and record R21-C，父仓提交，此处一并记录以满足 docgate 的最近提交覆盖）与本线 `d753d7a`（R22-A A2 修复）。

## A1 事实矩阵（raw/A1-matrix.json，真实 Go + CLI）

线索：`analyzer.Config.Overlay` 注释限定 compiler-owned；CLI 目前给前端的是 `staleGeneratedOverlay`（仅工具自有旧产物 `//go:build ignore`），**不含用户 GOFLAGS/GOENV 的 -overlay**。

对照（磁盘 VM `Title string; Inc()`，用户 overlay 改 VM）：

| 场景 | 同环境 go build | 修复前 CLI | 结论 |
|---|---|---|---|
| s1 VM.Title string→int，绑定 `{Title}` 到 Text | rc0 | **rc0 生成**（假接受；候选实际 `cannot use int as string` 编译失败） | Analyzer 看磁盘 string，分歧 |
| s2 overlay 删除 `Inc`，`Click="Inc"` | rc0 | **rc0 生成**（假接受；候选 `v.vm.Inc undefined`） | Analyzer 看磁盘有 Inc，分歧 |
| s3 overlay **新增** `Inc`，`Click="Inc"` | rc0 | **rc2 假拒绝**「VM has no method Inc」 | Analyzer 看磁盘无 Inc，分歧 |
| s4 静态页 `--out-dir target`，target 仅 overlay 虚拟源 | — | rc0（R21 已覆盖定名） | Analyzer 静态页无需虚拟 VM |
| s5 **GOENV 持久化**入口（同 s1，经 GOENV 文件） | — | 修复后 rc2（与进程 GOFLAGS 一致） | 入口一致 |
| s6 `--check`（s3 overlay 新增 Inc） | — | 修复后通过 | 一致 |

**实际失败阶段**：Analyzer 的 `packages.Load` 用 `Config.Overlay=staleGeneratedOverlay`（不含用户条目）→ 类型/方法检查按**磁盘**。K11 依赖指纹（`buildInputsFingerprint(ctxValues.GOFLAGS)`，R15-A 起）已含用户 overlay 文件内容，缓存失效正确（非本缺陷）。

## A2 修复（commit `d753d7a`）

- 新增 `scopedUserOverlayForAnalyzer(flags, moduleRoot, vmDirs)`（`generated_overlay.go`）：用 R21 的 `userOverlayMap`（路径映射，空键/归一化重复 fail-closed），把**落在 Analyzer 加载包目录（vmDirs=miss 输入 sourceDir）内**的用户 overlay 条目**读成内存快照**并入 Analyzer overlay；**只读相关目录替换源（有界）**，不预读整份映射；删除语义（`""`）跳过。
- `pipeline.go`（runPipeline）：`goOverlay`（staleGeneratedOverlay）之上并入 `scopedUserOverlayForAnalyzer(ctxValues.GOFLAGS, ...)`，**已证工具自有旧产物优先**（不覆盖）。用**测量后** GOFLAGS（`ctxValues.GOFLAGS`），故 **GOENV 持久化**亦生效。
- 未改 `analyzer.Config.Overlay` 契约（仍是 compiler-owned、ModuleRoot 内、内存快照、构造即拷贝）；用户 overlay 源仍在 ModuleRoot 内，由既有 `Config.Validate` scope 校验。**正式 Analyzer 输入契约无扩展**，无需单独候选。
- R21 保留：归一化重复拒绝、未使用缺失映射由 Go 决定、删除语义、大小写与虚拟目录回归——`scopedUserOverlayForAnalyzer` 复用 `userOverlayMap`，不预读未用源。

**K10/K11/K12 与缓存版本**：均不变；`cacheFormatVersion` 仍 **8**（K11 已覆盖用户 overlay 内容，无需新键）。

### 判别回归 + 变异红
`internal/cli/analyzer_overlay_r22_test.go`：
- `TestScopedUserOverlayScopesToVmDirs`（只并入 vmDir 内、不预读 other）、`TestScopedUserOverlayDuplicateFailClosed`（R21 保留）、`TestAnalyzerSeesUserOverlayFieldTypeChange`（Analyzer overlay 含 Title int）。
- **变异红**：禁用 Analyzer 用户 overlay 合并 → 上述后两项 FAIL；CLI 级 s1 **假接受 rc0 写盘**（坏候选）；恢复全绿。

## A3 验收（raw/A3-acceptance.json、A3-querycount.json）

真实 CLI：
- 冷生成 rc0 → **真命中**（bytes/mtime/Manifest 不变）→ 只改 overlay 替换内容（Title→int）→ **无缓存假命中、类型拒绝、旧 gen/Manifest 保留**（注：同包 owned 旧 gen + 改字段类型时 Go 角载失败报 ROOT-002，属 R21 已证优先级，安全拒绝并保旧）→ 恢复 rc0。
- **混合命中批次**：p 命中 + p2 新生成。 **Image 对照**：冷+命中+`//go:embed` 存在。
- **25 目标 = 2 次 go list**（无退化）。

## 门禁
`go build ./...` ok；`go vet ./internal/cli ./internal/compiler/pipeline ./internal/compiler/analyzer` rc0；`gofmt -l internal/cli` 空；`go test ./internal/cli -cover` = **86.x%**、pipeline、analyzer 覆盖见 commands；台账 verify PASS。
**docgate 基线登记**：基线 `39f45ea`（docs commit 非纯 dev-log，未被豁免）在最近 10 提交覆盖内但历史 dev-log 未记录 → docgate 在本基线**既有失败**；本 dev-log 已把 `39f45ea` 与本线 `d753d7a` 一并记录，供合入后满足覆盖。

## 五态
实现 done(`d753d7a`) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
