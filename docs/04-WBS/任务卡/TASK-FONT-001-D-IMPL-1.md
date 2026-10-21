# TASK-FONT-001-D-IMPL-1：异步加载器与状态机（接口已冻结，实现未开始）

## 元信息

- 状态：**接口与并发语义已冻结（第 2 版，按评审意见修正 4 处），实现未开始**
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D` §10 第 2 步（2a `d7268e5` + 2b `2fc61a7`）**均已完成**
- 交付物：`pkg/runtime/theme/lazy.go` + 同包测试 + `internal/docgate` 一条结构门禁
- 验收命令见 §8（**测试名必须先由本卡创建**，否则 `no tests to run` 且退出码 0）

### 修订记录（第 2 版）

| # | 评审问题 | 处理 |
|---|---|---|
| 1 | `OnReady` 与 `NewLazyApp` 的初始化顺序无法接线 | **删除 `LazyOptions.OnReady`**，改为显式 `SetNotify` 后绑定；并冻结"通知不是正确性依赖"，见 §2 |
| 4 | `LoadReport` 缺完整类型定义 | 补齐完整声明、字段归属、可变性与副本规则，见 §5 |
| 3 | `ProbeElapsed` 与 20 ms 预算和实测 97–109 ms 矛盾 | 删除不可测的 `ParseElapsed`；改为 `ProbeElapsed`（候选循环）+ `LoadElapsed`（后台墙钟总量）；20 ms 口径见 §6 |
| 6 | 临时字体构造边界与 C 卡按需补充的关系 | 冻结为"临时主题必然解析一次嵌入，但**不写入**最终链诊断"，成本记 `TempElapsed`，见 §4 |

## 0. 本卡只做一件事

把"**加载**"从同步路径里拿出来，做成**有明确状态机、有唯一入口、不阻塞任何调用方**
的后台过程。**本卡不碰渲染**：

| 不做 | 归属 |
|---|---|
| 在 `FrameEvent` 内替换 `th.Shaper` | `TASK-FONT-001-D-IMPL-2` |
| 严格失败语义、四指标汇总报告 | `TASK-FONT-001-D-IMPL-3` |
| 首帧预算/布局跳动的端到端验收 | `TASK-FONT-001-D-ACCEPT` |

理由：状态机的正确性（合并、不等待、不自动重试）可以用**注入的假加载器**
在纯单元测试里判定，不需要真实窗口。把渲染接线混进来会让"状态机错了"和
"切换时机错了"混成同一个红灯，定位成本翻倍。

## 1. 冻结的 API 骨架（实现必须照此，不得改名/改语义）

```go
// LoadState 是异步加载状态（与 D 卡 §3 状态机一一对应）。
type LoadState int

const (
    LoadUnloaded LoadState = iota // 构造完成，未做任何 IO
    LoadLoading                   // 后台探测 + 解析中
    LoadReady                     // 真主题已产出，等待渲染线程取走
    LoadFailed                    // 系统与嵌入两条链路均失败
)

func (s LoadState) String() string

// LazyOptions 配置一次异步加载。
//
// 注意：这里**没有** OnReady 回调。通知必须在窗口句柄创建之后、
// Request 之前用 SetNotify 绑定（见 §2）——构造 Lazy 时窗口还不存在，
// 把回调塞进构造参数会造成"通知哪个窗口"无从冻结。
type LazyOptions struct {
    Provider *Provider       // nil → 包内 defaultProvider（A/B/C 的既有实例）
    Family   string          // "" → FamilyCJK
    Temp     *material.Theme // nil → 方案 c（NoSystemFonts + 嵌入子集），见 §4
}

// Lazy 持有"当前可用主题"与后台加载进度。
type Lazy struct{ /* 未导出字段 */ }

// NewLazy 构造。**不做任何 IO**（规范 §3.3：禁止 init/构造期同步加载）。
func NewLazy(opts LazyOptions) (*Lazy, error)

// SetNotify 绑定"加载完成"通知。返回是否绑定成功。
//
// 语义（逐条冻结，实现不得自行选择）：
//   - 只允许在 Request() 之前调用一次；重复调用返回 false，且**不改变任何状态**；
//   - 已 Request 过再调用返回 false（不得 panic，也不得静默吞掉：
//     调用方必须自己补一次 Invalidate，见 §2.2）；
//   - 绑定成功后，加载完成时在**后台 goroutine 内**同步调用一次 fn；
//   - fn 内**只允许请求重绘**（生产形态是 w.Invalidate()），不得替换 shaper。
func (l *Lazy) SetNotify(fn func()) bool

// Theme 返回当前可用主题，**纯读取、永不产生副作用**（见 §3 规则 3）。
func (l *Lazy) Theme() *material.Theme

// State 返回当前状态。
func (l *Lazy) State() LoadState

// Request 是**唯一**的加载入口：首次加载 + 失败后的显式重试。
// 任意 goroutine 可调用；Loading 期间调用立即返回，不重复触发。
func (l *Lazy) Request()

// Take 供渲染线程在帧内取走待应用的真主题；返回 nil 表示没有待应用项。
// **一次性**：取走后必须等下一次 Ready 才会有新值。**只允许在渲染线程调用**。
func (l *Lazy) Take() *material.Theme

// MarkFrameSubmitted / MarkFirstLayout 由渲染线程调用，**只生效一次**（幂等）。
// 语义见 TASK-FONT-001-D-IMPL-2 §1.3；本卡只提供类型与幂等保证。
func (l *Lazy) MarkFrameSubmitted(at time.Time)
func (l *Lazy) MarkFirstLayout(d time.Duration)

// Report 返回最近一次加载诊断的**副本**（完整类型见 §5）。
func (l *Lazy) Report() LoadReport
```

**为什么 `Request` 同时是"首次加载"和"重试"**：D 卡 §3/§8-3 冻结了
"重试只允许显式请求触发"，但没有定义第二个入口名。合并成一个入口的好处是
**不存在"隐式触发"的路径**——所有加载都必然来自某次显式 `Request()` 调用。

## 2. 通知的绑定时序（评审阻塞点 1，冻结）

### 2.1 谁持有、何时绑定

| 问题 | 冻结答案 |
|---|---|
| 谁持有通知目标？ | **`Lazy` 持有 `func()`**；目标窗口由调用方在绑定时决定 |
| 何时绑定？ | 窗口句柄创建之后、`Request()` 之前。`NewLazyApp` 内部完成这一对调用 |
| 是否允许设置多次？ | **只允许一次**（`SetNotify` 返回 true 仅第一次）。重复调用返回 false，不改状态、不 panic |
| 绑定前加载已完成怎么办？ | 结构上不可能（`Request` 必须在绑定之后）。若调用方反序操作，`SetNotify` 返回 false，**加载结果不会丢**——`Take()` 是每帧轮询的 |

### 2.2 通知不是正确性依赖（关键）

`Take()` 每帧都会被调用，所以**即使一次通知都没发出去，真主题也会在下一帧被取走**。
通知只缩短等待，不承担正确性。三条推论：

1. `SetNotify` 返回 false **不是错误**，调用方补一次 `Invalidate()` 即可；
2. `NewLazyApp` 在 `SetNotify` 返回 false 时**必须**立即 `Invalidate()` 一次，
   并置 `Report().NotifyLateBound = true`，让"通知没绑上"在诊断里可见；
3. 测试不得用"是否收到通知"证明"加载是否成功"。成功与否只看 `State` / `Ready` / `Take()`。

### 2.3 生产接线顺序（`NewLazyApp` 内，冻结）

```text
w := new(app.Window); w.Option(...)
a := &App{w: w, view: view, lz: lz}
if !lz.SetNotify(func() { a.Invalidate() }) {   // 闭包捕获已构造完成的 a
        a.Invalidate()                          // 反序操作：补一次唤醒，不报错
}
lz.Request()                                    // 首次加载的唯一触发点，不等待
return a, nil
```

关键：闭包捕获的是**已构造完成的 `a`**，不是"构造中的窗口"。
这解决了"构造 `lz` 时窗口还不存在"的矛盾——**绑定发生在 `NewLazyApp` 内部**，
`NewLazy` 不再需要提前知道窗口。

## 3. 线程所有权（R2 规则的前提）

| 谁 | 允许 | 禁止 |
|---|---|---|
| 调用方（任意 goroutine） | `SetNotify` / `Request` / `State` / `Theme` / `Report` | 直接读写 `Lazy` 内部字段 |
| 后台加载 goroutine | `Provider.ResolveWithReport` / `Faces` / 构造**新** `material.Theme`、填 `Report`、调通知 | **读或写任何已交给渲染线程的 `*material.Theme`**；调用 `LayoutString`；在通知里替换 shaper |
| 渲染线程 | `Theme` / `Take` / `MarkFrameSubmitted` / `MarkFirstLayout` | 在通知回调里做替换 |

**为什么后台只允许"构造新主题"而不是"改旧主题"**：`text.Shaper` 的集合在
构造时由 `WithCollection` 给定，没有公开换集合的 API；原地改会让内部缓存与
新集合不一致（D 卡 §5）。另外跨 goroutine 共用一个 shaper 会崩（§4.3.2，
6/6 必现）——**即使只是查询字形**。

### 3.1 规则 3：`Theme()` 永不产生副作用

这是"失败后不自动重试"能否成立的**唯一保证**。若 `Theme()` 在 `Failed` 时
顺手重试一次，那么每帧渲染都会重试——正是 D 卡 §3 明令禁止的行为。
因此 `Theme()` 只读；首次加载由 `NewLazyApp` 显式 `Request()` 一次；
`Failed` 后只有再次 `Request()` 才重试。

## 4. 临时主题的构造边界（评审阻塞点 6，冻结）

### 4.1 构造与生命周期

- `NewLazy` 内**不构造**临时主题（`Temp == nil` 时只记录配置）；在**首次
  `Request()` 之前**构造一次，之后复用，**严禁每帧重建**（每帧重建会把
  嵌入解析成本乘上帧数）。
- 构造内容：`material.NewTheme()` + `Shaper = text.NewShaper(NoSystemFonts(),
  WithCollection(嵌入子集))`（方案 c，D 卡 §4.2 已冻结）。
- 成本记 `Report.TempElapsed`（实测 1.4–1.7 ms，D1 读数 4），
  **属于首帧预算 M1 的一部分**，不得计入后台探测/加载段。

### 4.2 与 C 卡"按需补链"的关系（写清，否则会污染 B4 断言）

C 卡的规则是：**系统覆盖完整时不加载嵌入**。这条规则约束的是**最终选择链**
（`Provider` → `supplement`）。临时主题是另一件事：

| | 临时主题 | 最终主题（系统命中且覆盖完整） |
|---|---|---|
| 是否解析嵌入 | **必然解析一次**（首帧要有汉字） | **不解析** |
| 诊断字段 | `TempSource = "embedded-subset"` | `Embedded.Attempted = false` |

**硬约束**：临时主题的嵌入解析**不得**写入 `ProbeReport.Embedded`
（那是最终链的诊断；写进去会让 B4 的"系统命中 ⇒ `Attempted==false`"断言失效）。
两者用不同字段表达，验收时分别断言。

### 4.3 已知代价（如实登记）

系统命中的机器上，嵌入子集**仍会被解析一次**（只为首帧）。这是 D 卡 §9-5
已登记的张力：属于"首帧临时字体"的一次性成本，不是补充链被触发。
若首帧预算吃紧，可选优化是让临时主题只用拉丁 gofont——但那会让首帧汉字变
notdef，已被 §4.2 否决，因此本卡不做。

## 5. `LoadReport` 完整定义（评审问题 4）

```go
// LoadReport 是一次异步加载的完整诊断快照。
//
// 可变性：所有字段由 Lazy 在内部锁保护下写入；调用方拿到的永远是副本
// （切片与嵌套指针深拷贝，与 theme.Diagnostics 的既有做法一致），
// 改动副本不影响内部状态，也不影响后续 Report() 的返回值。
//
// 字段归属：本卡填 §5.1 的字段；§5.2 的字段由 IMPL-2/3 填，本卡留零值即可，
// 但**类型必须在本卡一次性定死**，后续卡不得再改结构。
type LoadReport struct {
    // ---- 本卡（IMPL-1）负责 ----
    State      LoadState
    Family     string
    Attempts   int          // Request 实际触发加载的次数（Loading 期间的合并调用不计）
    TempSource string       // 临时主题来源："embedded-subset" / "custom"
    Probe      *ProbeReport // 后台探测诊断的副本（含 Candidates / Embedded / Coverage / Err）

    RequestedAt  time.Time     // 最近一次**实际开始**加载的时刻
    LoadedAt     time.Time     // 后台产出可用主题的时刻；零值表示尚未产出
    LoadElapsed  time.Duration // LoadedAt - RequestedAt，**后台墙钟总量，不含等待帧**
    ProbeElapsed time.Duration // = Probe.Elapsed，候选循环实耗时（口径见 §6）
    TempElapsed  time.Duration // 临时主题构造耗时（含一次嵌入解析），首帧内

    // ---- IMPL-2 负责 ----
    FrameSubmittedAt     time.Time     // 真主题首次**提交返回后**的时刻（代理指标，非屏幕呈现）
    FirstLayoutAfterSwap time.Duration // 切换后首次排版耗时
    NotifyLateBound      bool          // SetNotify 晚于 Request（通知未绑上）

    // ---- IMPL-3 负责 ----
    Err error // 终态错误；失败时 errors.Is(err, contracts.ErrFontNotFound) 为真
}
```

### 5.1 本卡的字段级完成判据

| 字段 | 成功路径 | 失败路径 |
|---|---|---|
| `State` | `LoadReady` | `LoadFailed` |
| `Attempts` | ≥1，且 32 并发请求仍为 1 | 同上 |
| `TempSource` | `embedded-subset` | 同 |
| `Probe` | 非 nil，`Err == nil` | 非 nil，`Candidates` 有原因 |
| `LoadedAt` | 非零，且 ≥ `RequestedAt` | **零值**（不得伪造） |
| `LoadElapsed` | > 0 | 0 |
| `ProbeElapsed` | > 0 | > 0（探测跑过就有值） |
| `TempElapsed` | > 0，且多次 `Take`/`Theme` 不增长 | > 0 |

`Err` 在本卡只要求"失败时非 nil"；错误链与 `ErrFontNotFound` 的断言归 IMPL-3。

## 6. 指标口径（评审阻塞点 3，冻结；并更正原定义）

### 6.1 先承认三个事实

1. `Provider.ResolveWithReport` 的区间**不等于**"探测"：还包含 `supplement`
   （C 卡补链，可能触发嵌入解析）与嵌入兜底（`loadEmbedded`）。
2. `probe()` 把 `Read` + `ParseCollection` + `checkCoverage` 放在**同一个循环**，
   `CandidateResult` **没有**分段计时字段——**现有 API 无法把"解析"从"探测"中分离**。
3. D1 实测 `ResolveWithReport` 冷启动 **97–109 ms**、进程内重复 **81–101 ms**，
   规范 §3.2 的预算是 **≤20 ms**。这是**既有缺口**，A/B/C 阶段就存在，不是 D 引入的。

### 6.2 冻结口径

| 指标 | 字段 | 计量边界 | 判定方式 |
|---|---|---|---|
| M2 探测段 | `ProbeElapsed` = `Probe.Elapsed` | **仅 `probe()` 的候选循环**（读文件 + 解析 + 覆盖判定），不含补链与兜底 | 记录实际值；20 ms 是**规范预算**不是通过阈值，超了如实登记 |
| M3 后台总量 | `LoadElapsed` | `RequestedAt → LoadedAt` 墙钟总量，不含等待帧 | 记录实际值；`LoadElapsed - ProbeElapsed` 即补链/兜底/主题构造余量 |

**删除 `ParseElapsed`**：原定义声称"仅字体字节解析区间"，但现有 API 无法分离，
写一个测不出来的字段等于给验收埋假证据。真要分离需给 `CandidateResult` 增加
`ReadElapsed`/`ParseElapsed`——那是 **A 卡范围**，D 不擅自改，登记为可选后续。

### 6.3 20 ms 预算怎么交代

- 规范原文（`字体与文本渲染策略.md` §3.2）：「字体加载耗时（冷启动内）≤ 20 ms
  （**懒加载时应 ≤ 首帧 budget**）」。
- D 的职责是括号里那句：**把这段耗时移出首帧**，不是把它压到 20 ms。
- 真要做到 ≤20 ms 需要候选优化（Provider 层缓存/并行探测/预解析缓存），
  **归属独立待办**，本轮不承诺、不冒充达标。
- 因此 M2 在 ACCEPT 里**不设"通过/失败"**，只记录实际值与 `Probe.Timeout`
  是否触发（deadline 放弃后续候选是另一件事，不混为一谈）。

## 7. 测试要求（全部以 `TestFontLazyLoad` 开头）

前缀必须保持——父卡验收命令是 `go test -run TestFontLazyLoad ./pkg/runtime/theme/...`，
`-run` 是**前缀匹配**，沿用前缀就不会退化成空转。

| 用例 | 断言 | 对应 |
|---|---|---|
| `TestFontLazyLoadStateMachine` | 四态迁移合法；Failed→Loading 仅由 `Request` | D §3 |
| `TestFontLazyLoadNoIOOnConstruct` | `NewLazy` 后 `Attempts==0`、加载器未调用、`State==LoadUnloaded` | 规范 §3.3 |
| `TestFontLazyLoadCoalesce` | N=32 并发 `Request`：加载器**只调用 1 次**，`Attempts==1` | D6-5 |
| `TestFontLazyLoadNonBlockingOnLoading` | "永不完成"加载器：`Request`/`Theme` **立即返回**（超时护栏），加载器仍 1 次 | D6-5a |
| `TestFontLazyLoadNoAutoRetry` | 失败加载器：等待后计数仍 1；连调 `Theme()` 100 次不增 | D6-5b |
| `TestFontLazyLoadExplicitRetry` | 失败后显式 `Request()` → 计数 1→2，`Attempts==2` | D6-5b |
| `TestFontLazyLoadThemeHasNoSideEffects` | `Theme()` 在 Unloaded/Failed 下都不触发加载 | §3.1 |
| `TestFontLazyLoadTakeOnce` | `Ready` 后 `Take()` 返回真主题；再次 `Take()` 返回 nil | §1 |
| `TestFontLazyLoadFailureKeepsTemp` | `Failed` 后 `Theme()` 仍是临时主题（不返回 nil、不伪造成功） | D5 |
| `TestFontLazyLoadNotifyBeforeRequest` | `SetNotify` 在 `Request` 前 → true，完成后回调**恰好一次** | §2 |
| `TestFontLazyLoadNotifyAfterRequest` | 先 `Request` 再 `SetNotify` → false、不 panic；`Take()` 仍能取到真主题 | §2.2 |
| `TestFontLazyLoadTempOnlyBuiltOnce` | 多次 `Theme`/`Take` 不重建临时主题（`TempElapsed` 不增长） | §4.1 |
| `TestFontLazyLoadTempDoesNotPolluteProbe` | 临时主题解析嵌入后，系统命中时 `Probe.Embedded` 仍为 nil 或 `Attempted==false` | §4.2 |

**假加载器怎么造**：复用 B 卡 `countingLoader` / `failingLoader` 的手法
（注入 `ProviderOptions.Embedded` + `Read`），不要用 `time.Sleep` 制造竞态；
"永不完成"用 `select {}` + 测试结束时的释放通道，避免 goroutine 泄漏。

## 8. 验收命令（本卡自测）

```text
go test -v -run TestFontLazyLoad ./pkg/runtime/theme/
go test -run TestAsyncPathNoRenderShaper ./internal/docgate/
go vet ./pkg/runtime/theme/ ./internal/docgate/
```

**空转警告**：上述命令在用例创建前会打印 `no tests to run` 并**返回 0**。
本卡的完成证据**必须**包含 `--- PASS: TestFontLazyLoad...` 的实际输出行。

## 9. 完成判据

1. §7 十三个用例全部 PASS（贴输出，不贴退出码）；
2. `TestAsyncPathNoRenderShaper` 生效，且**做过一次反向验证**（故意在 `lazy.go`
   里加一行 `_ = th.Shaper`，门禁必须变红，然后撤回）；
3. `go test ./pkg/runtime/theme/` 全绿（不得打断既有 A/B/C 用例）；
4. 产品行为**未改变**：`theme.NewTheme()` / `app.Run` 的同步路径一行未改。

## 10. 明确不做

- 不改 `pkg/runtime/app`（哪怕一行）；
- 不改 `theme.NewTheme()` 的语义与签名（D 卡 §11 冻结）；
- 不实现 `MarkFrameSubmitted` / `MarkFirstLayout` 的调用点（只提供幂等方法）；
- 不给 `CandidateResult` 加分段计时（A 卡范围）；
- 不碰 Linux（独立待办，见 D 卡 §12.5）。
