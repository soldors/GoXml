# TASK-FONT-001-D-ACCEPT：懒加载整体验收与回归（设计已冻结，执行未开始）

## 元信息

- 状态：**验收矩阵已冻结，执行未开始**（2026-10-21 冻结）
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D-IMPL-1` / `-2` / `-3` **全部完成**
- 交付物：整体验收记录 + Windows 回归结果 + 文档同步；**不改产品代码**
  （若验收发现缺陷，缺陷修复回各自子卡，不在本卡里顺手改）

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
| 1 | 冷启动首帧不等待加载 | IMPL-2 + 本卡 | `TestFontLazyLoadFirstFrameNotBlocked` | 待实测 |
| 2 | 后台完成时间可观测 | IMPL-1 | `TestFontLazyLoadDiagnosticsFields` | 待实测 |
| 3 | 后续帧稳定（无重复解析/无重复 shaping） | IMPL-2 | `TestFontLazyLoadNoRepeatShaping` | 待实测 |
| 4 | **系统命中时不解析嵌入**（既有 B4 约束） | 本卡（回归） | `TestEmbeddedFallbackSelectionOrder`（既有） | 待复核 |
| 5 | 并发首次请求合并 | IMPL-1 | `TestFontLazyLoadCoalesce` | 待实测 |
| 5a | Loading 期间重复请求立即返回临时字体 | IMPL-1 | `TestFontLazyLoadNonBlockingOnLoading` | 待实测 |
| 5b | Failed 后不自动重试，显式重试才 1→2 | IMPL-1 | `TestFontLazyLoadNoAutoRetry` + `TestFontLazyLoadExplicitRetry` | 待实测 |
| 6 | 布局跳动可测（含阈值） | IMPL-2 | `TestFontLazyLoadLayoutDelta` | 待实测（delta） |
| 7 | 失败诊断可见 | IMPL-3 | `TestFontLazyLoadFailureVisible` | 待实测 |
| 8 | 两个预算分开报告 | IMPL-3 + 本卡 | `TestFontLazyLoadMetricSeparation` | 待实测 |

## 2. 四个指标必须分四行报（R3）

验收报告里**必须**是这样一张表，四行缺一不可，**不得**出现"总耗时"：

| 指标 | 定义 | 实测 | 阈值/预算 | 判定 |
|---|---|---|---|---|
| M1 首帧预算 | 进程启动 → 首帧处理开始 | 待实测 | 实测后定 | — |
| M2 字体探测预算 | `Report.ProbeElapsed` | 待实测 | ≤ 20 ms（规范 §3.2） | — |
| M3 后台解析耗时 | `Report.ParseElapsed` | 待实测 | 无硬预算 | — |
| M4 切换后首次排版 | `Report.FirstLayoutAfterSwap` | 待实测 | 实测后定 | — |

**M1 阈值的定法（D 卡 §9-1 强制）**：先测"完全不加载字体的最小窗口"首帧耗时作基线，
再测懒加载路径首帧耗时，两者同量级即可接受；**不得**先写一个数字再去找数据。
基线数据可用 `pkg/runtime/theme/testdata/coldstartprobe` 与
`pkg/runtime/app/testdata/windowprobe` 的口径，但首帧预算必须在**同一个探针**里
用同一种计时方式取，不得跨探针拼数。

## 3. 回归清单（一项都不得省）

| 检查 | 命令/方法 | 通过判据 |
|---|---|---|
| 全仓库测试 | `go test -timeout 30m ./...` | 全 ok（codegen 包需长超时，见 dev-log 10-20 §22.12） |
| 静态检查 | `gofmt -l .` / `go build ./...` / `go vet ./...` | 空 / 0 / 0 |
| 探针单独 vet | 四个 `testdata` 探针显式 vet | 0（`./...` 跳过 testdata，见 dev-log 10-21） |
| 二进制体积 | `go build ./testdata/sizeprobe`（**不是** `examples/demo`） | 增量 ≤ 3 MiB（B 卡预算仍成立） |
| B4 约束未失效 | `TestEmbeddedFallbackSelectionOrder` | 系统命中 ⇒ `Embedded.Attempted==false` |
| 同步严格语义未退化 | `TestFontProbeErrFontNotFound` + `app.Run` 严格失败用例 | 仍返回 `ErrFontNotFound` |
| 路由 B 边界 | `TestThemePackageBoundary`、`TestGeneratedLayoutSignatureMatchesRouteB` | 全绿（生成代码仍只认 `*material.Theme`） |
| `th.Shaper` 替换点唯一 | `grep -rn "\.Shaper *=" --include=*.go` | 仅 `pkg/runtime/app` 的帧内分支一处 |
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
3. §3 九项回归全通过（贴输出）；
4. §4 未覆盖项在报告里单列，未混入通过结论；
5. 文档同步：D 卡状态改为"已实现"、父卡与 WBS 更新、dev-log 记录本轮实测数字。

## 6. 明确不做

- 不修缺陷（回 IMPL-1/2/3）；
- 不新增字体能力（如 emoji、更多家族）；
- 不碰 Linux / macOS（独立待办）；
- 不改 B/C 的既有预算与阈值。
