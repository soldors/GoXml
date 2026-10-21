# TASK-FONT-001-D-IMPL-3：诊断、失败终态与严格语义（接口已冻结，实现未开始）

## 元信息

- 状态：**接口与失败语义已冻结，实现未开始**（2026-10-21 冻结）
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D-IMPL-1`（状态机与 `Report`）、`TASK-FONT-001-D-IMPL-2`（帧内切换）
- 交付物：`Report` 完整字段、`Config.StrictFontFailure`、指标分离报告、测试
- 验收命令见 §6

## 0. 本卡要解决的是**语义冲突**，不是加日志

异步化带来一个不能含糊的问题：**`app.Run` 原本是"字体找不到就返回错误"的严格语义
（`ErrFontNotFound`），改成异步后，启动时不可能再同步知道结果。**
如果不管，严格语义就静默消失了——这比没有严格语义更糟，因为调用方以为自己还被保护着。

本卡冻结这条冲突的裁决、失败终态的可见性，以及四个性能指标的分离口径。

## 1. 冻结的裁决：两套语义并存，不互相污染

| 入口 | 字体失败时的行为 | 保留理由 |
|---|---|---|
| `theme.NewTheme()` | **不变**：返回 Gio 默认主题，诊断走 `theme.Diagnostics()` | D 卡 §11 冻结"签名不因字体接线而改变" |
| `app.Run` / `RunWith` | **不变**：同步严格失败，返回 `ErrFontNotFound` | B6 既有验收，不得因 D 而退化 |
| `app.NewLazyApp` + `App.Run`（异步） | 由 `Config.StrictFontFailure` 决定，见下 | 异步下启动时无法同步得知结果 |

```go
// pkg/runtime/app Config 新增字段（其余字段不变）

// StrictFontFailure 只在懒加载路径（NewLazyApp）生效。
//
//   false（默认）：字体加载失败**不终止窗口**，UI 停留在临时字体，
//                  失败原因从 App.Lazy().Report().Err 读取（含 ErrFontNotFound）。
//   true：窗口照常运行（**不得**闪退、不得 panic），但 Run() 在窗口正常关闭后
//         返回包装了 ErrFontNotFound 的错误，供 CI/上层据此判定失败。
//
// 对同步路径（NewApp/Run/RunWith）无影响。
StrictFontFailure bool
```

**三条明令禁止**：

1. **禁止**在加载失败时 panic 或强制关窗——那会把"可诊断的失败"变成"崩溃"，
   比同步失败更糟（D 卡 §6 原话：静默故障比同步失败更糟）。
2. **禁止**为"保持严格"而在 `NewLazyApp` 里同步等一次加载结果——那等于取消懒加载。
3. **禁止**让 `StrictFontFailure` 影响同步路径——两套语义各自独立，
   不得让一个开关同时改两条链路。

## 2. 失败终态必须可区分（D5 核心）

三种"界面上都显示临时字体"的情形必须能被诊断区分开：

| 情形 | `State` | `Err` | 界面 |
|---|---|---|---|
| 加载中 | `LoadLoading` | nil | 临时字体 |
| 加载失败 | `LoadFailed` | 非 nil（含 `ErrFontNotFound`） | 临时字体 |
| 成功但尚未取走 | `LoadReady` | nil | 仍可能是临时字体（尚未到帧） |

**要求**：`State` 与 `Err` 必须能被同时读到，且存在用例证明
`Loading` 与 `Failed` 不能用同一种读数描述。
否则"永久停在临时字体、诊断看不出原因"这个评审点名的静默故障就成立。

## 3. 诊断入口（不做全局单例）

```go
// App.Lazy 返回构造时传入的 *theme.Lazy，供调用方读诊断。
// 返回 nil 表示本 App 走的是同步路径（NewApp）。
func (a *App) Lazy() *theme.Lazy
```

**为什么不做 `theme.LazyDiagnostics()` 这样的全局函数**：D 卡 §4.1 要求
**每个顶层窗口有独立的 Theme/Shaper**；全局单例会引入跨窗口污染，
且与"每次 `NewTheme()` 返回独立实例"的既有承诺冲突。诊断跟着句柄走。

`Report()` 的完整字段（IMPL-1/2 已产出的不再重复定义）：

| 字段 | 产出方 | 含义 |
|---|---|---|
| `State` / `Family` / `Attempts` / `TempSource` | IMPL-1 | 状态机与临时字体来源 |
| `Probe *ProbeReport` | IMPL-1 | 探测诊断（含 `Candidates`/`Embedded`/`Coverage`/`Err`） |
| `RequestedAt` / `LoadedAt` | IMPL-1 | 请求开始 / 产出可用主题 |
| `ProbeElapsed` / `ParseElapsed` | IMPL-1 | 后台两段耗时（**不含等待帧**） |
| `DisplayedAt` | IMPL-2 | 真主题首次提交呈现之后 |
| `FirstLayoutAfterSwap` | IMPL-2 | 切换后首次排版耗时 |
| `Err error` | 本卡 | 终态错误；失败时 `errors.Is(err, contracts.ErrFontNotFound)` 为真 |

## 4. 规则 R3：四个指标独立验收（不得互相替代）

| # | 指标 | 字段/来源 | 计量边界 | 预算 |
|---|---|---|---|---|
| M1 | 首帧预算 | `app` 侧记录（进程启动→首帧处理开始） | 含窗口创建，不含等待字体 | **实测后定阈值**（D 卡 §9-1 禁止先拍数字） |
| M2 | 字体探测预算 | `Report.ProbeElapsed` | 仅 `Provider.ResolveWithReport` 区间 | ≤ 20 ms（规范 §3.2） |
| M3 | 后台解析耗时 | `Report.ParseElapsed` | 仅字体字节解析区间 | 无硬预算，记录 |
| M4 | 切换后首次排版 | `Report.FirstLayoutAfterSwap` | 仅替换 `th.Shaper` 后第一次 `Layout` 区间 | 实测后定 |

**禁止事项（写进注释与测试）**：

- 不得用 M2 或 M3 冒充 M1（这是"探测 <20ms ≠ 首帧不阻塞"的同一原则）；
- 不得把 `DisplayedAt - LoadedAt`（含 `Invalidate` 调度等待）计入任何一项；
- 不得输出"总耗时"合数——四个数各自有边界，加起来没有物理含义；
- 首帧预算的阈值**必须**在 ACCEPT 卡拿到实测值后写死，本卡只负责让它可测。

## 5. 测试要求（全部以 `TestFontLazyLoad` 开头）

| 用例 | 断言 |
|---|---|
| `TestFontLazyLoadDiagnosticsFields` | 成功路径：`Report()` 各字段就位，`Err==nil`，`State==LoadReady` |
| `TestFontLazyLoadFailureVisible` | 系统 + 嵌入两条链路全失败：`State==LoadFailed`、`errors.Is(Err, contracts.ErrFontNotFound)`、`Probe.Candidates` 有失败原因 |
| `TestFontLazyLoadFailureDistinguishable` | `Loading` 与 `Failed` 在读数值上可区分（`State`/`Err` 二元组不同），界面同为临时字体 |
| `TestFontLazyLoadStrictFailure` | `StrictFontFailure=true`：窗口正常运行、无权 panic，`Run()` 正常返回后错误含 `ErrFontNotFound` |
| `TestFontLazyLoadNonStrictKeepsRunning` | 默认 `false`：`Run()` 返回 nil，但 `Report().Err` 仍可读到失败原因 |
| `TestFontLazyLoadMetricSeparation` | 四个指标字段**分别**可读、互不相同源；断言不存在"合数"字段 |
| `TestFontLazyLoadNoSilentFallback` | 失败后界面不得表现为"字体正常但内容错"——用 `MissingGlyphs` 或 `Coverage` 证明临时字体缺字形是可观测的 |

## 6. 验收命令

```text
go test -v -run TestFontLazyLoad ./pkg/runtime/theme/ ./pkg/runtime/app/
go test -run TestFontProbe ./pkg/runtime/theme/
go test -run TestEmbeddedFallback ./pkg/runtime/theme/
```

第二、三条是**回归保护**：本卡改的是失败语义，A/B 的既有失败语义不得被带坏
（`TestFontProbeErrFontNotFound`、`TestEmbeddedFallbackFailureSemantics`）。

## 7. 完成判据

1. §5 七个用例 PASS；
2. A/B 的失败语义用例仍全绿（贴输出）；
3. `StrictFontFailure` 的两种取值都各有一个用例，且**同步路径在其两种取值下行为一致**；
4. 四个指标在报告里分四行出现，任何一行缺失都算未完成；
5. 文档同步：D 卡 §6/§7 的验收项指向本卡的具体用例名。

## 8. 明确不做

- 不实现状态机（IMPL-1）、不实现帧内切换（IMPL-2）；
- 不新增全局诊断单例；
- 不改 `theme.NewTheme()` / `app.Run` 的既有语义；
- 不定首帧预算阈值（ACCEPT，需实测）；
- 不碰 Linux（独立待办）。
