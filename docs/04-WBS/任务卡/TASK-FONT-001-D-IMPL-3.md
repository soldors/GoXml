# TASK-FONT-001-D-IMPL-3：诊断、失败终态与严格语义（实现已本地提交，正式验收未完成）

## 元信息

- 状态（三段式，**不得合并表述**）：
  - **实现状态：已完成并本地提交**（`e7ad53036854abee9f34bacb9e5c21d86b23e7be`）；2026-10-27 集成快照为 `eaa8487`，未推送。
  - **实现自检：已通过**——2026-10-23 原记录属于实现方自检；后续独立复验、补充测试与合并态回归见下，不据此宣布正式验收完成
  - **正式验收：待 `TASK-FONT-001-D-ACCEPT`**——**不得**标为"正式验收完成"
- 接口与失败语义：**第 3 版已冻结**，本次实现未改动冻结接口
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D-IMPL-1`（状态机与 `Report`）、`TASK-FONT-001-D-IMPL-2`（帧内切换）
- 交付物：`Report` 完整字段、`Config.StrictFontFailure`、指标分离报告、测试
- 验收命令见 §6

### 进度补记（2026-10-27：提交与补充证据同步）

原标题“实现已完成，待提交”及“未经独立复验”是早期快照；本次按实际提交和复验记录更正当前进度。
`2f03384` 新增独立下游测试：冷加载按同源耗时检查区间包含，warm 路径独立证明命中并检查 `ProbeElapsed==0`，
另覆盖失败、空报告和虚拟时钟边界。既有 `font_lazy_strict_test.go` 及本卡 §4.1／§5 的历史断言未在本次改写，
不能把新增测试说成原冻结断言已完成修订。第 3 版公共接口与失败语义继续保持。
`abc571d` 已将 M1 起点接到构造函数入口，首帧处理开始仍是止点；`e4b0612` 合并态全仓回归 rc=0。
正式验收、跨 family 成功路径和环境边界见 [ACCEPT 卡第 18 版](TASK-FONT-001-D-ACCEPT.md)。

### 修订记录（第 2 版）

| # | 评审问题 | 处理 |
|---|---|---|
| 3 | M2 的 20 ms 与既有实测 97–109 ms 矛盾，且 `ResolveWithReport` 区间不等于探测 | 重定口径：M2 只算 `probe()` 候选循环、**不设通过阈值**；M3 改为 `LoadElapsed` 后台总量；删除不可测的 `ParseElapsed`，见 §4.1 / §4.2 |
| 2 | `DisplayedAt` 语义超出证据 | 全文改为 `FrameSubmittedAt`（提交返回，代理指标），见 §3 字段表 |

### 修订记录（第 3 版）

| # | 评审问题 | 处理 |
|---|---|---|
| B | 失败路径 `LoadElapsed` 无法成立（`LoadedAt` 为零），但 M3 又要求它表示后台总耗时 | 字段表补 `CompletedAt`；M3 计量边界改为 `RequestedAt→CompletedAt`（成功/失败都记）；`TestFontLazyLoadFailureVisible` 增加失败路径耗时断言，见 §3 / §4.1 |
| C | `NotifyLateBound` 产出方写成本卡前置的 IMPL-2 | 改为**IMPL-1**（`SetNotify` 内部置位），见 §3 字段表 |
| — | 97–109 ms 易被当成 M2 实测值 | §4.2 第 3 条补"它不是 M2 实测值、不得与 20 ms 直接比" |

### 修订记录（第 4 版：仅职责描述与实现登记，接口与错误语义未变）

| # | 问题 | 处理 |
|---|---|---|
| D | §3 字段表把 `Err` 的产出方写成"本卡" | 按实际职责更正为 **IMPL-1 写入 / 本卡验证可观测性**；**不**据此回头修改 IMPL-1，见 §3 表下说明 |
| E | M1 的起止点未在冻结接口里定义 | 授权**最小可测化**：只记录与暴露、不定阈值、不混入候选循环 `Elapsed`；起止事件与零值语义见 §4.4 |
| F | §5 只列 7 个用例，实际实现 10 个 | §5.1 登记超出最小集的 3 个及其依据；§5 七用例未删减 |

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
| `RequestedAt` / `LoadedAt` | IMPL-1 | 请求开始 / **产出可用主题**（失败时零值） |
| `CompletedAt` | IMPL-1 | 后台进入终态（Ready 或 Failed）的时刻，**成功/失败都非零** |
| `TempElapsed` | IMPL-1 | 临时主题构造（含一次嵌入解析），属首帧成本 |
| `ProbeElapsed` | IMPL-1 | `probe()` **候选循环**实耗时（**不含**补链/兜底，见 §4） |
| `LoadElapsed` | IMPL-1 | 后台墙钟总量 `CompletedAt-RequestedAt`，**失败路径同样记录** |
| `FrameSubmittedAt` | IMPL-2 | 真主题首次**提交返回后**的时刻（代理指标，非屏幕呈现） |
| `FirstLayoutAfterSwap` | IMPL-2 | 切换后首次排版耗时 |
| `NotifyLateBound` | IMPL-1 | `SetNotify` 重复/晚绑定/nil 绑定未成功（第 3 版从 IMPL-2 收回） |
| `Err error` | **IMPL-1**（写入）/ 本卡（**只验证可观测性**） | 终态错误；失败时 `errors.Is(err, contracts.ErrFontNotFound)` 为真 |

> **`Err` 产出方的职责划分（第 4 版更正，不改变错误语义）**
>
> 第 3 版把 `Err` 的产出方写成"本卡"，是**归口**（本卡负责它的语义与验收），
> 不是产出实现。实际分工：
>
> - **IMPL-1**：后台加载进入终态（`Ready`/`Failed`）时写入 `l.rep.Err`
>   （`pkg/runtime/theme/lazy.go`）。失败值含 `contracts.ErrFontNotFound`。
> - **本卡（IMPL-3）**：**不写入**该字段，只让它可观测——
>   ① `App.Lazy()` 交出句柄；② 用例断言 `errors.Is(rep.Err, contracts.ErrFontNotFound)`；
>   ③ `StrictFontFailure=true` 时由 `Run()` 包装返回。
>
> 因此**不**为迎合"产出方=本卡"这句表述去改 IMPL-1。D 卡 §6.1 已同步同一描述。

## 4. 规则 R3：四个指标独立验收（不得互相替代）

### 4.1 四个指标的口径（第 3 版更正 M2/M3）

| # | 指标 | 字段/来源 | 计量边界 | 判定 |
|---|---|---|---|---|
| M1 | 首帧预算 | `app` 侧记录（进程启动→首帧处理开始），内含 `TempElapsed` | 含窗口创建与临时主题构造（这段是**有界同步成本**，不是零）；**不含等待字体** | 实测后定阈值（D 卡 §9-1 禁止先拍数字） |
| M2 | 探测段 | `Report.ProbeElapsed` = `Probe.Elapsed` | **仅 `probe()` 候选循环**（读文件 + 解析 + 覆盖判定）；不含补链与嵌入兜底 | **记录实际值，不设通过阈值**，见 §4.2 |
| M3 | 后台总量 | `Report.LoadElapsed` | `RequestedAt → CompletedAt` 墙钟总量，**成功/失败都记**；不含等待帧 | 记录实际值；`LoadElapsed-ProbeElapsed` 即补链/兜底/主题构造余量 |
| M4 | 切换后首次排版 | `Report.FirstLayoutAfterSwap` | 仅替换 `th.Shaper` 后第一次 `Layout` 区间 | 实测后定 |

### 4.2 为什么 M2 不是"≤20ms 通过"（评审阻塞点 3）

第 1 版把 M2 写成"`ResolveWithReport` 区间 ≤20ms"，与既有实测直接冲突，
而且计量边界本身也是错的。核对代码后的实情：

1. `ResolveWithReport` 的区间**不等于探测**：还包含 `supplement`（C 卡补链，
   可能触发嵌入解析）与 `loadEmbedded`（系统全失败时的兜底）；
2. `probe()` 把 `Read` + `ParseCollection` + `checkCoverage` 放在同一循环，
   `CandidateResult` 没有分段计时 → **现有 API 无法把"解析"从"探测"里分离**。
   第 1 版那个 `ParseElapsed` 字段因此已从 `LoadReport` 删除（IMPL-1 §6）；
3. 实测 `ResolveWithReport` 冷启 **97–109 ms**，规范 §3.2 预算 **≤20 ms**。
   这是 **A/B/C 阶段就存在的既有缺口**，不是 D 引入的。
   **但这个数字不是 M2 的实测值**——它测的是 `ResolveWithReport` 整个区间；
   它可以证明"把整个调用叫探测"不严谨，**不可以**拿来当 `ProbeElapsed` 的基线
   或与 20 ms 直接比较。M2 的真实数值只能由 IMPL-1 实现后实测，此前一律"待实测"。

规范原文（`字体与文本渲染策略.md` §3.2）：「字体加载耗时（冷启动内）≤ 20 ms
（**懒加载时应 ≤ 首帧 budget**）」。D 负责的是括号里那一句：**把它移出首帧**。

因此：

- M2 的 20 ms 是**规范预算**，不是本卡的通过条件；实测超了如实登记；
- 若真要 ≤20ms，需要候选优化（Provider 层缓存 / 并行探测 / 预解析缓存），
  **归属独立待办**，本卡不承诺、不冒充；
- `Probe.Timeout`（deadline 放弃后续候选）与"耗时超预算"是**两件事**，
  报告里分开写，不得混为一谈。

### 4.3 禁止事项（写进注释与测试）

- 不得用 M2 或 M3 冒充 M1（"探测 <20ms ≠ 首帧不阻塞"的同一原则）；
- 不得把 `FrameSubmittedAt - LoadedAt`（含 `Invalidate` 调度等待，
  2b 实测 75.7–105.2 ms）计入任何一项；
- 不得输出"总耗时"合数——四个数各自有边界，加起来没有物理含义；
- 首帧预算的阈值**必须**在 ACCEPT 卡拿到实测值后写死，本卡只负责让它可测。

### 4.4 M1 的最小可测化（授权范围，第 4 版登记）

冻结接口里没有 M1 的起止点定义。本卡按授权做**最小可测化**——
**只记录与暴露，不定阈值，不做通用性能监控框架**。

| 项 | 定义 |
|---|---|
| 起点 | `NewApp` / `NewLazyApp` **构造入口**（`createdAt = time.Now()`，构造时快照） |
| 止点 | **第一个 `FrameEvent` 开始处理**的瞬间（`view.Layout` **之前**，`frame()` 入口） |
| 时钟来源 | `time.Now()` / `time.Now().UnixNano()`，**墙钟**，不是单调时钟也不是 CPU 时间 |
| 读取时机 | `Run()` 期间任意时刻可读；语义上在窗口关闭后读才有完整含义 |
| 零值语义 | `FirstFrameReport.Measured() == false`（尚未有过帧）时 `Elapsed` **为 0 且无意义** |
| 不可用时 | 用例必须报「**未验证**」并 `t.Fatalf`，**不得默认通过** |
| 阈值 | **不在本卡设定**，归 `D-ACCEPT` |
| 明确不做 | 不混入候选循环 `Elapsed`（M2）；不加"总耗时"；不做分位数/多轮/ warmup 隔离 |

**为什么止点定在"`Layout` 之前"而不是"`e.Frame` 返回后"**：M1 的语义是
"首帧**开始处理**之前等了多久"，即"有没有被字体加载阻塞"；
`e.Frame` 之后的提交耗时属于渲染路径，**不属于**首帧等待。
`FrameSubmittedAt`（IMPL-2，M1 之外）才是提交侧的代理指标，两者不得混用。

**实测边界（本机，不得固化为跨机器阈值）**：本机 `time.Now()` 有效粒度约 **0.53 ms**
（2026-10-23 实测：空区间 2000/2000 恒为 0 ns；忙等翻动值 523–548 µs）。
因此单帧级微秒断言在本机是**空判据**；本卡的 M1 读数同样受此限制，
用例只断言"可测出非零"与"起止顺序"，**不**断言具体数值区间。

## 5. 测试要求（全部以 `TestFontLazyLoad` 开头）

| 用例 | 断言 |
|---|---|
| `TestFontLazyLoadDiagnosticsFields` | 成功路径：`Report()` 各字段就位（含 `CompletedAt` 非零且 `>= LoadedAt`），`Err==nil`，`State==LoadReady` |
| `TestFontLazyLoadFailureVisible` | 系统 + 嵌入两条链路全失败：`State==LoadFailed`、`errors.Is(Err, contracts.ErrFontNotFound)`、`Probe.Candidates` 有失败原因、**`LoadedAt` 零值而 `CompletedAt`/`LoadElapsed` 非零** |
| `TestFontLazyLoadFailureDistinguishable` | `Loading` 与 `Failed` 在读数值上可区分（`State`/`Err` 二元组不同），界面同为临时字体 |
| `TestFontLazyLoadStrictFailure` | `StrictFontFailure=true`：窗口正常运行、无权 panic，`Run()` 正常返回后错误含 `ErrFontNotFound` |
| `TestFontLazyLoadNonStrictKeepsRunning` | 默认 `false`：`Run()` 返回 nil，但 `Report().Err` 仍可读到失败原因 |
| `TestFontLazyLoadMetricSeparation` | 四个指标字段**分别**可读、互不相同源；断言不存在"合数"字段；`ProbeElapsed` 只覆盖候选循环、`LoadElapsed >= ProbeElapsed`、`CompletedAt-RequestedAt ≈ LoadElapsed` |
| `TestFontLazyLoadNoSilentFallback` | 失败后界面不得表现为"字体正常但内容错"——用 `MissingGlyphs` 或 `Coverage` 证明临时字体缺字形是可观测的 |

### 5.1 实际实现清单（10 个，§5 七用例之外多 3 个）

文件：`pkg/runtime/app/font_lazy_strict_test.go`（新增）。

| # | 用例 | 来源 |
|---|---|---|
| 1 | `TestFontLazyLoadDiagnosticsFields` | §5 |
| 2 | `TestFontLazyLoadFailureVisible` | §5 |
| 3 | `TestFontLazyLoadFailureDistinguishable` | §5 |
| 4 | `TestFontLazyLoadStrictFailure` | §5 |
| 5 | `TestFontLazyLoadNonStrictKeepsRunning` | §5 |
| 6 | `TestFontLazyLoadMetricSeparation` | §5 |
| 7 | `TestFontLazyLoadNoSilentFallback` | §5 |
| 8 | `TestFontLazyLoadStrictFlagNoEffectOnSyncPath` | **超出 §5**：完成判据 3 要求"同步路径在开关两种取值下行为一致"，需要一个显式用例，否则只靠"没写"来证明 |
| 9 | `TestFontLazyLoadFirstFrameReport` | **超出 §5**：M1 最小可测化的读数与 `Measured()` 零值语义（§4.4） |
| 10 | `TestFontLazyLoadNoImplicitRetry` | **超出 §5**：D 卡 §7 第 5b 项要求"计数器 1 → 2"，`TestFontLazyLoadNoAutoRetry`（IMPL-1）在 `theme` 层，本用例从 **`App` 侧**复核 `Attempts` |

**两条实现约束（写死，防退化）**：

- `TestFontLazyLoadNoSilentFallback` **必须构造可验证的缺字形场景**，
  **不得**依赖"本机嵌入字体恰好缺字"这种不可控前提。实现做法是
  对照组（`material.NewTheme()`，断言缺字数为 **0**，证明检测器**不是恒真**）
  + 实验组（`text.NewShaper(text.NoSystemFonts())`，断言**必报**缺字）。
- 失败路径用例注入 `failReadDelay = 50ms` 的读取延迟，否则真实"文件不存在"
  只有几微秒、`LoadElapsed` 读数恒为 0（本机计时粒度 0.53 ms），断言会变成空判据。

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

## 9. 本轮实现登记（2026-10-23）

### 9.1 生产代码

| 文件 | 改动 |
|---|---|
| `pkg/runtime/app/app.go` | `Config.StrictFontFailure`；`App` 增加 `strict` / `createdAt` / `firstFrameAt atomic.Int64`；`NewApp` / `NewLazyApp` 快照；`frame()` 入口记 M1 止点（只记第一次）；`Run()` 改为 `return a.fontFailure()` |
| `pkg/runtime/app/lazy_status.go`（新增） | `App.Lazy()`、`FirstFrameReport` + `Measured()` + `App.FirstFrame()`、`App.fontFailure()` |

### 9.2 自查状态（**自检，不是验收**）

复跑时间 2026-10-23，**全部命令取真实退出码**（未走 `| tail` 管道）：

| 命令 | 退出码 | 计数（**口径已标注**） |
|---|---|---|
| `go test -v -run TestFontLazyLoad ./pkg/runtime/theme/ ./pkg/runtime/app/` | 0 | 顶层 PASS **34**、子测试 PASS **15**、FAIL **0**、SKIP **0** |
| `go test -count=1 -run TestFontProbe ./pkg/runtime/theme/` | 0 | —（0.966s） |
| `go test -count=1 -run TestEmbeddedFallback ./pkg/runtime/theme/` | 0 | —（12.371s） |
| `go test -v -count=1 -run TestFontLazyLoad ./pkg/runtime/app/` | 0 | 顶层 PASS **17**（IMPL-3 的 10 个 + IMPL-2 的 7 个，逐个列名） |
| `go test -v -count=1 ./internal/docgate/` | 0 | 顶层 PASS **13**、FAIL 0、SKIP 0 |
| `go vet ./pkg/runtime/app/` | 0 | — |
| `go build ./...` | 0 | — |

> 计数口径：**顶层测试函数数 ≠ 子测试数 ≠ 表内形态数**。34 是两包合计的
> 顶层 `TestFontLazyLoad*` 函数数，15 是其子测试；app 包单独跑是 17 个顶层函数。

- 变异取证 M7–M12 全部使对应用例变红，文件按 SHA-256 还原后复跑通过；
- **未推送、未提交**；详细命令输出与实测值见 `docs/05-开发/dev-log/2026-10-23.md` §12。

### 9.3 明确"未验证 / 待测"（交 `D-ACCEPT`）

1. **M1 阈值**未定（§4.4），本卡只做到可测；
2. D 卡 §7 第 2 项"不超过同步路径总耗时"的**比较**未实现；
3. D 卡 §7 第 4 项"异步化后系统命中仍不解析嵌入"**无本卡专门用例**（依赖 IMPL-1 既有断言传递）；
4. **真实窗口**（非合成 `FrameEvent`）下的 M1 读数未测；
5. 跨平台（Linux/macOS）、`-race` 均未跑；
6. 上述结论**均来自本机单次执行**，未经独立复验。
