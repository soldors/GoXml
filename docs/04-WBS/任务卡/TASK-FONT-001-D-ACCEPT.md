# TASK-FONT-001-D-ACCEPT：懒加载整体验收与回归（设计已冻结，执行未开始）

## 元信息

- 状态：**验收矩阵已冻结（第 3 版，按评审意见同步 `CompletedAt` 与 `NotifyLateBound` 口径），执行未开始**
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D-IMPL-1` / `-2` / `-3` **全部完成**
- 交付物：整体验收记录 + Windows 回归结果 + 文档同步；**不改产品代码**
  （若验收发现缺陷，缺陷修复回各自子卡，不在本卡里顺手改）

### 修订记录（第 2 版）

| # | 评审问题 | 处理 |
|---|---|---|
| 3 | M2 的 20 ms 与本仓库实测 97–109 ms 冲突，且"探测/解析"无法分离 | M2 取消通过阈值（只记录）、M3 改 `LoadElapsed`、补"为什么"三段理由，见 §2 |
| 5 | "`th.Shaper` 全仓库唯一赋值点"与既有同步路径冲突 | 改为"恰好 2 处，既有 `theme.go` 不计入"，见 §3 |
| 6 | 临时主题是否重复解析嵌入、是否污染 B4 断言 | 回归清单增加 `TestFontLazyLoadTempDoesNotPolluteProbe`，见 §3 |
| — | D6-1 的覆盖方写成了 IMPL-2，但 IMPL-2 没有对应用例 | 改由本卡创建 `TestFontLazyLoadFirstFrameNotBlocked`（端到端），见 §1 |

### 修订记录（第 3 版）

| # | 评审问题 | 处理 |
|---|---|---|
| B | 失败路径 `LoadElapsed = LoadedAt-RequestedAt` 不成立（`LoadedAt` 为零） | §2 的 M3 计量边界改为 `RequestedAt→CompletedAt` 并注明**失败也记**；§1 D6-2 增加 `TestFontLazyLoadFailureCompletedAt` 作为失败路径证据 |
| C | `NotifyLateBound` 归属 IMPL-2 但 IMPL-2 无写入口 | §3 新增独立回归行：写入点**恰好 1 处**（`SetNotify` 内）、`pkg/runtime/app/` 零命中 |
| A | 临时主题的同步构造成本未被钉住 | §1 M1 行注明 `TempElapsed` 是**有界同步成本**；§3 新增 `TestFontLazyLoadTempBuiltOnceConcurrent` 回归行 |
| — | 97–109 ms 容易被误当 M2 实测值 | §2 M2 段落写死"它不是 M2 的实测值，不得填进 M2 格、不得与 20 ms 直接比" |

## 0. 定位

本卡不写新功能，只回答两个问题：

1. **D 的 8 项验收（D 卡 §7）是否每一项都有真实用例支撑**，以及每一项的
   实测值是多少；
2. **异步化是否破坏了既有行为**（A/B/C 的字体语义、`app.Run` 的同步严格语义、
   二进制体积、既有测试）。

顺带完成一件事：把 `pkg/runtime/theme/font_pending_test.go` 里的
`TestFontLazyLoad` 占位 `t.Skip` 换成真实断言或删除，**但必须保证仍有至少一个
以 `TestFontLazyLoad` 开头的真实用例**——否则父卡的验收命令
`go test -run TestFontLazyLoad ./pkg/runtime/theme/...` 会退化成
`no tests to run` + 退出码 0（空转报绿，仓库已多次踩过）。

## 1. D6 八项映射（每项必须有具体用例名，不许写"已覆盖"）

| # | 验收点（D 卡 §7） | 由谁覆盖 | 用例名 | 实测值 |
|---|---|---|---|---|
| 1 | 冷启动首帧不等待加载 | **本卡**（端到端实测，IMPL-2 只提供切换点） | `TestFontLazyLoadFirstFrameNotBlocked`（本卡创建） | 待实测 |
| 2 | 后台完成时间可观测 | IMPL-1 | `TestFontLazyLoadDiagnosticsFields`（成功：`CompletedAt >= LoadedAt`、`LoadElapsed>0` 且 `>=ProbeElapsed`）+ `TestFontLazyLoadFailureCompletedAt`（失败：`LoadedAt` 零值但 `CompletedAt`/`LoadElapsed` 非零） | 待实测（IMPL-1 部分已 PASS，`DiagnosticsFields` 属 IMPL-3） |
| 3 | 后续帧稳定（无重复解析/无重复 shaping） | IMPL-2 | `TestFontLazyLoadNoRepeatShaping` | 待实测 |
| 4 | **系统命中时不解析嵌入**（既有 B4 约束） | 本卡（回归） | `TestEmbeddedFallbackSelectionOrder`（既有） | 待复核 |
| 5 | 并发首次请求合并 | IMPL-1 | `TestFontLazyLoadCoalesce` | 待实测 |
| 5a | Loading 期间重复请求立即返回临时字体 | IMPL-1 | `TestFontLazyLoadNonBlockingOnLoading` | 待实测 |
| 5b | Failed 后不自动重试，显式重试才 1→2 | IMPL-1 | `TestFontLazyLoadNoAutoRetry` + `TestFontLazyLoadExplicitRetry` | 待实测 |
| 6 | 布局跳动可测（含阈值） | IMPL-2 | `TestFontLazyLoadLayoutDelta` | 待实测（delta） |
| 7 | 失败诊断可见 | IMPL-3 | `TestFontLazyLoadFailureVisible` | 待实测 |
| 8 | 两个预算分开报告（M1–M4 分行） | IMPL-3 + 本卡 | `TestFontLazyLoadMetricSeparation` | 待实测 |

## 2. 四个指标必须分四行报（R3）

验收报告里**必须**是这样一张表，四行缺一不可，**不得**出现"总耗时"：

| 指标 | 定义 | 实测 | 判定口径 |
|---|---|---|---|
| M1 首帧预算 | 进程启动 → 首帧处理开始（含 `TempElapsed` 这段**有界同步成本**） | 待实测 | 实测后定阈值 |
| M2 探测段 | `Report.ProbeElapsed`（仅 `probe()` 候选循环） | 待实测 | **只记录，不设通过阈值** |
| M3 后台总量 | `Report.LoadElapsed`（`RequestedAt→CompletedAt` 墙钟，成功/失败都记） | 待实测 | 只记录；与 M2 的差即补链/兜底/主题构造余量 |
| M4 切换后首次排版 | `Report.FirstLayoutAfterSwap` | 待实测 | 实测后定阈值 |

**M2 为什么没有通过阈值（评审阻塞点 3 的结论，不要再改回去）**：

- 规范 §3.2 的 20 ms 是**冷启动内加载耗时的预算**，原文括号写明
  「懒加载时应 ≤ 首帧 budget」——D 的职责是**移出首帧**，不是压到 20 ms；
- 实测 `ResolveWithReport` 冷启 **97–109 ms**，是 **A/B/C 阶段就存在的既有缺口**，
  不是 D 引入的；把 20 ms 当本卡通过条件，等于让 D 去还 A/B/C 的债。
  **但 97–109 ms 不是 M2 的实测值**——它测的是 `ResolveWithReport` 整段，
  不能填进 §2 的"M2 实测"格，也不能拿来跟 20 ms 直接比；
- `ResolveWithReport` 区间还含 `supplement`（C 卡补链）与嵌入兜底，
  与"探测"不是一回事；而 `probe()` 内部把读文件/解析/覆盖判定混在同一循环，
  **现有 API 无法分离解析耗时**（详见 IMPL-1 §6）。
- 真要 ≤20 ms，需要候选优化（Provider 层缓存/并行探测/预解析缓存），
  **独立待办**，本轮不承诺。报告里必须写清这一点，不得用"异步已达标"含糊过去。

**M1 阈值的定法（D 卡 §9-1 强制）**：先测"完全不加载字体的最小窗口"首帧耗时作基线，
再测懒加载路径首帧耗时，两者同量级即可接受；**不得**先写一个数字再去找数据。
首帧预算必须在**同一个探针**里用同一种计时方式取，不得跨探针拼数。

## 3. 回归清单（一项都不得省）

| 检查 | 命令/方法 | 通过判据 |
|---|---|---|
| 全仓库测试 | `go test -timeout 30m ./...` | 全 ok（codegen 包需长超时，见 dev-log 10-20 §22.12） |
| 静态检查 | `gofmt -l .` / `go build ./...` / `go vet ./...` | 空 / 0 / 0 |
| 探针单独 vet | 四个 `testdata` 探针显式 vet | 0（`./...` 跳过 testdata，见 dev-log 10-21） |
| 二进制体积 | `go build ./testdata/sizeprobe`（**不是** `examples/demo`） | 增量 ≤ 3 MiB（B 卡预算仍成立） |
| B4 约束未失效 | `TestEmbeddedFallbackSelectionOrder` + `TestFontLazyLoadTempDoesNotPolluteProbe` | 系统命中 ⇒ `Embedded.Attempted==false`（临时主题的嵌入解析不得写进该字段） |
| 同步严格语义未退化 | `TestFontProbeErrFontNotFound` + `app.Run` 严格失败用例 | 仍返回 `ErrFontNotFound` |
| 路由 B 边界 | `TestThemePackageBoundary`、`TestGeneratedLayoutSignatureMatchesRouteB` | 全绿（生成代码仍只认 `*material.Theme`） |
| `th.Shaper` 赋值点 | `grep -rn "\.Shaper *=" --include=*.go pkg/ \| grep -v _test.go \| grep -v /testdata/` | **恰好 3 处**（IMPL-1 阶段）：`theme.go` 既有同步构建 1 处 + `theme_build.go` 的两处构造（临时主题 / 真主题）。`pkg/runtime/app` 的**生产代码 0 处**（帧内切换归 IMPL-2，接入后应为 1，总数变 4）。`testdata/` 下的探针程序不计入（它们是实验代码，不是产品路径）。出现额外处即失败 |
| `NotifyLateBound` 写入点 | `grep -rn "NotifyLateBound" --include=*.go pkg/` | 写入点**恰好 1 处**（`lazy.go` 的 `SetNotify` 内）；`pkg/runtime/app/` **零命中**（第 3 版问题 C：责任归 IMPL-1） |
| 临时主题并发只构造一次 | `TestFontLazyLoadTempBuiltOnceConcurrent` | 32 goroutine 并发首次 `Request()` → 嵌入解析计数器 `== 1`（第 3 版问题 A） |
| 占位不再空转 | `go test -v -run TestFontLazyLoad ./pkg/runtime/theme/` | 有 `--- PASS`/`--- FAIL` 行，**不是** `no tests to run` |

## 4. 未覆盖项必须在报告里单列

以下项**不属于**本卡，也**不得**因为本卡通过而被视为已验证：

| 项 | 归属 |
|---|---|
| **Linux 无 CJK 字体环境的嵌入增量** | **独立待办**（C 卡 §7-6 未竟项），不并入 D 任何子卡 |
| macOS / X11 / Wayland 的探测与唤醒 | 独立待办（C 卡 §7） |
| 窗口最小化/不可见时的唤醒 | `TASK-FONT-001-D-PROBE` §8.4 |
| emoji 字形来源 | 独立待办（只报告不补） |
| `-race` 数据竞争报告 | 本机不可用（缺 C 工具链） |

## 5. 完成判据

1. §1 十行映射里每一行都有**实际用例名**和实测值，没有"已覆盖"这种模糊表述；
2. §2 四行指标齐全，M1 阈值的定法符合"先测后定"；
3. §3 **十一项**回归全通过（贴输出）；
4. §4 未覆盖项在报告里单列，未混入通过结论；
5. 文档同步：D 卡状态改为"已实现"、父卡与 WBS 更新、dev-log 记录本轮实测数字。

## 6. 明确不做

- 不修缺陷（回 IMPL-1/2/3）；
- 不新增字体能力（如 emoji、更多家族）；
- 不碰 Linux / macOS（独立待办）；
- 不改 B/C 的既有预算与阈值。
