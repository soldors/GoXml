# 2026-11-05 R23-B FONT-LAZY-RELIABILITY（DeepSeek）dev-log

基线 `9268601e4365ffbf78faa2ee0121e6826d3a24b2`（开工核验 clean，主库 porcelain 40）。分支 `codex/r23-deepseek-font-lazy`，树 `r23-deepseek-font-lazy`。范围：`pkg/runtime/theme`（+必要 app 最小接线）、对应测试、字体验收文档；不接管 Router 预算/ThemeRef 颜色/CLI/编译器。
记录提交：`9268601`（base）→ `759e2dd`（B2/B3 fix+guard）→ `bbd8574`（B1 矩阵 doc）→ 本 dev-log。

## B1 字体正式验收差距矩阵（docs/03-架构/字体正式验收差距矩阵-R23B.md）
- 完整读 TASK-FONT-001-A/B/C/D、D-IMPL-1/2/3、D-ACCEPT、字体契约、ADR-0012 及字体测试/dev-log。
- 逐项（D-ACCEPT-01~08）列：卡内现行结论 / 本轮已证事实与证据 / 当前缺口 / 本机可收口 / 需负责人裁定。
- **旧"缺 gcc"核查**：本机已有 MinGW gcc 16.1.0（`D:/Codes/SQLMonitor/.tools/mingw64/bin/gcc.exe`），`-race` **现可执行** ⇒ D-ACCEPT-05 的 `-race` 从"环境限制"转可验（本机已补跑）。
- 关键裁决项分离：M1 阈值（需定值）、异步 vs 同步耗时比较（是否实施）、真实窗口 App+M1 探针（需授权开窗）、IMPL-3 冻结文件下游断言（需授权解冻）、跨 family（是否补第二族）、跨平台（需他机）。

## B2 无窗口可靠性：指标语义实证 + 真实缺陷修复

### 指标语义（注入虚拟时钟确定性；probe_elapsed_cache_semantics_test.go，TestAccept07 全 PASS）
| 场景 | 实测 | 语义 |
|---|---|---|
| 冷加载 | `Elapsed=14ms ProbeRan=true`，Now 调用 3 | 本轮真探测，`ProbeElapsed` 记本轮 |
| 缓存命中 | `ProbeRan=false`，第二阶段 `Now` 调用 **0**，`LoadReport.ProbeElapsed==0` | 命中本轮无探测 ⇒ 恰 0，不沿用历史 |
| 复用（第 2 Lazy 同 Provider） | `ProbeElapsed 14ms→0`，`Scans=1 Read=1` | 复用命中 ProbeElapsed=0，历史 `Probe.Elapsed` 保留 |
| 失败 | 步长 ×10 ⇒ `Elapsed 10.0x`，失败不缓存 | ProbeElapsed 仅本轮；LoadElapsed 后台墙钟总量 |
- **口径**：`ProbeReport.Elapsed` 可保留历史；`LoadReport.ProbeElapsed` 仅本轮、由 `Probe.ProbeRan` 门控；"有没有探测"看 `ProbeRan` 不看是否 0；缓存读取耗时不计入 `ProbeElapsed`。**未把缓存读取与首次解析混写**。

### 真实缺陷（B3 `-race` 实证）—— Provider.loadEmbedded 并发竞争
临时主题由**每个 Lazy 各自**构造；多个 Lazy **共享同一 Provider** 时并发进入 `newTempTheme → Provider.loadEmbedded → Embedded.Load()`。`loadEmbedded` 无 Provider 级同步 ⇒ 调用方注入的（可能非并发安全的）`EmbeddedFontLoader` 被并发调用，`-race` 报 **DATA RACE**（`TestNight07ConcurrentWarmLaziesNoRescan` 命中，读写在 `failingLoader.Load` 的 `calls++`）。
- **修复（合同内最小，`759e2dd`）**：`Provider` 增 `embMu sync.Mutex`，`loadEmbedded` 串行化。不改调用次数语义、不改诊断/失败合同、不改公开 API。
- 版本/合同：无新公开 API、无性能阈值、无失败合同变化 ⇒ 无需单独冻结候选。

## B3 无窗口 race + 变异 + 门禁
- **`-race`（MinGW）**：theme 全包 `-race` ×3 **0 DATA RACE**、ok；app 字体/关闭/notify 族 `-race` ok（排除原生窗口）。修复前 theme 全包 `-race` **DATA RACE FAIL**（已转绿）。
- **守卫**：`TestLoadEmbeddedConcurrentSharedProviderIsSerialized`（32 并发共享 Provider，非并发安全计数加载器）——非 race 下功能一致（Load 次数=N、id/faces 唯一），race 下无告警。
- **可编译变异**：独立源码副本去 `embMu.Lock/Unlock`（`-overlay`）→ 守卫 `-race` **DATA RACE / FAIL**；恢复（无 overlay）**PASS**。真实 rc1 与目标 FAIL 行保留于 raw。
- 门禁（本机）：theme/app `go test -count=1` ok；vet rc0；`gofmt -l pkg/runtime/theme` 空；docgate ok（本 dev-log 记录提交）；覆盖 **theme 86.7% / app 97.9%**。基线覆盖见命令台账。

## 边界（不得写成通过）
跨平台（Linux/macOS）未跑；M1 阈值未定；异步 vs 同步耗时比较未实现；真实窗口 App+M1 探针未接（本轮不开窗）；跨 family 仅 CJK 单族；IMPL-3 冻结候选（含 `font_lazy_strict_test.go` 下游 `LoadElapsed>=ProbeElapsed` 断言）**未解冻、未改**；本轮证据均本机执行，独立复核待协调者。

## 五态
实现 done(`bbd8574`+dev-log) / 自测 pass / 独立复核 PENDING / 主仓未合 / 远端未推送。
