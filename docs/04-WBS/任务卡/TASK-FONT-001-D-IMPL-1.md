# TASK-FONT-001-D-IMPL-1：异步加载器与状态机（已实现）

## 元信息

- 状态：**已实现**（按第 3 版接口，17 个用例全 PASS，结构门禁反向验证通过）
- 实现提交：**`948c406`**（2026-10-23 补记，补记前此处为「见 §11（提交后补记 SHA）」）
  - 该提交是**混合提交**：同批还含 C1 命名预检的 `internal/compiler/codegen/naming.go`
    与 `naming_test.go`；字体交付物（`lazy.go` / `theme_build.go` / `lazy_test.go`）同在其内
  - 归属登记：`aeb4fd3`（登记 IMPL-1 落在 `948c406`）与 `cc177ad`（dev-log 登记 IMPL-1 提交链）
  - 2026-10-23 复核（基线 `b33ce6d`）：`-run TestFontLazyLoad` 17 PASS / 0 SKIP；
    theme 全包 44 PASS / 0 SKIP（12.265s）
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

### 修订记录（第 3 版）

| # | 评审问题 | 处理 |
|---|---|---|
| A | `Request()` 声称"不阻塞任何调用方"，但首次需同步构造临时主题（含一次嵌入解析） | 不再宣称零阻塞：冻结为"**不等后台，但允许一次有界同步初始化**"，成本单列 `TempElapsed`，且不得据"没等加载器"推断首帧达标。新增并发首次请求只构造一次的用例，见 §1.1 / §4.1 |
| B | 失败路径 `LoadElapsed = LoadedAt - RequestedAt` 不成立（`LoadedAt` 为零） | 新增 **`CompletedAt`**（后台进入成功或失败终态的时刻），`LoadElapsed = CompletedAt - RequestedAt`，失败路径同样记录真实后台耗时，见 §5 / §5.1 |
| C | `NotifyLateBound` 归属 IMPL-2，但只有 `SetNotify` 能发现晚绑定 | 写入责任归 **IMPL-1**（`SetNotify` 内部记录）；IMPL-2 只按返回值补一次 `Invalidate()`。同时冻结 `SetNotify(nil)` 行为，见 §1 / §2.2 |

## 0. 本卡只做一件事

把"**加载**"从同步路径里拿出来，做成**有明确状态机、有唯一入口、不等待后台完成**
的后台过程。**本卡不碰渲染**：

| 不做 | 归属 |
|---|---|
| 在 `FrameEvent` 内替换 `th.Shaper` | `TASK-FONT-001-D-IMPL-2` |
| 严格失败语义、四指标汇总报告 | `TASK-FONT-001-D-IMPL-3` |
| 首帧预算/布局跳动的端到端验收 | `TASK-FONT-001-D-ACCEPT` |

**措辞更正（第 3 版）**：第 2 版此处写的是"不阻塞任何调用方"，与 §4.1
"首次 `Request` 之前构造一次临时主题（含一次嵌入解析，1.4–1.7 ms）"直接冲突。
本版把主张收窄为**可验证的那一句**：

> `Request()` **不等待**系统字体探测、字体解析与最终主题加载完成；
> 但**允许**在首次调用时同步构造一次临时主题（有界、约 1.4–1.7 ms）。
> 这段成本单列 `TempElapsed`，**不得**把这种有界同步初始化描述成"零阻塞"。

理由：`Request()` 返回后调用方马上要 `Theme()` 取第一帧可用的字体，
临时主题必须**已经存在**；放在后台构造会导致首帧没有可用主题（汉字变 notdef，
已被 §4.2 否决）。所以这不是"要不要阻塞"的选择题，而是**必须承认的一小段同步成本**。
真正被移出首帧的是那 97–109 ms 的探测——那才是 D 的目标。

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
//   - **fn == nil 视为无效绑定**：返回 false、不绑定、不 panic、永不触发；
//   - 绑定成功后，加载完成时在**后台 goroutine 内**同步调用一次 fn；
//   - fn 内**只允许请求重绘**（生产形态是 w.Invalidate()），不得替换 shaper；
//   - **重复绑定 / 晚绑定 / nil 绑定这三种失败，都由本方法在自己的锁内把
//     Report().NotifyLateBound 置为 true**（第 3 版问题 C）——调用方与
//     IMPL-2 **不得**直接写这个字段，该字段归属本卡。
func (l *Lazy) SetNotify(fn func()) bool

// Theme 返回当前可用主题，**纯读取、永不产生副作用**（见 §3 规则 3）。
func (l *Lazy) Theme() *material.Theme

// State 返回当前状态。
func (l *Lazy) State() LoadState

// Request 是**唯一**的加载入口：首次加载 + 失败后的显式重试。
// 任意 goroutine 可调用；Loading 期间调用立即返回，不重复触发。
//
// 同步边界（第 3 版冻结，见 §1.1）：首次调用会在返回前**同步构造一次**
// 临时主题（有界，约 1.4–1.7 ms，记 TempElapsed）；此后只做
// "投递后台任务 + 立即返回"，**不等待**探测/解析/最终主题。
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

### 1.1 `Request()` 的同步边界（第 3 版问题 A，冻结）

"不阻塞"这三个字在本项目里必须能被测试判定，所以拆成三句话，分别成立或不成立：

| 断言 | 是否成立 | 由谁证明 |
|---|---|---|
| `Request()` 不等待**后台加载器**完成 | **成立** | `TestFontLazyLoadNonBlockingOnLoading`（注入永不完成的加载器 + 超时护栏） |
| `Request()` 首次调用**不花任何同步时间** | **不成立，也不承诺** | 用 `TestFontLazyLoadTempBuiltOnceConcurrent` 把这段成本钉住（只花一次、可度量） |
| 因此首帧预算 M1 必然达标 | **不可推断** | 只能由 `TASK-FONT-001-D-ACCEPT` 端到端实测判定 |

**为什么不把临时主题放后台构造**：`Request()` 返回后调用方立刻要用 `Theme()`
取第一帧可用的字体，临时主题必须**已经存在**。放后台 → 首帧没有可用中文，
汉字变 notdef，已被 §4.2 否决。所以这题不是"要不要阻塞"，而是
**必须如实承认的一小段同步成本**；真正被移出首帧的是那 97–109 ms 的探测。

实现约束（三条，缺一即判实现错误）：

1. **临时主题只构造一次**：用 `sync.Once`（或与状态机
   `LoadUnloaded→LoadLoading` 同一个 CAS 临界区）保证；32 个 goroutine 并发首次
   `Request()` 时，**嵌入字体只能被解析一次**。断言方式为
   **解析计数器 == 1**，不是断言 `TempElapsed` 的数值大小。
2. **`TempElapsed` 只写一次**：它是"那一次构造"的耗时，后续重试不得覆盖，
   否则会随重试漂移成一个没有意义的字段。
3. **构造完立即返回**，后台任务投递出去就走；后台不得回头读渲染线程的
   `*material.Theme` 或 `th.Shaper`（§3）。

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
| `fn == nil` 怎么办？ | 返回 false、不绑定、不 panic、永不触发（**不得**把 nil 存进去等"以后再说"） |
| 绑定前加载已完成怎么办？ | 推荐构造顺序不会发生；调用方反序操作时SetNotify返回false、pending保留，NewLazyApp按ADR-0014补偿终态唤醒。Take仍只在帧内消费，不能仅凭结果保留断言下一帧必然存在 |
| `NotifyLateBound` 谁写？ | **`SetNotify` 自己写**（本卡）。重复/晚绑定/nil 三种失败都在其锁内置 true；调用方与 IMPL-2 不得直接写（第 3 版问题 C） |

### 2.2 结果保留与唤醒分别保证（2026-10-30 缺陷修正）

原“只要 Take 每帧执行，通知就不影响正确性”的论证未保证下一帧必然存在，已由 [ADR-0014](../../03-架构/adr/ADR-0014-Lazy晚绑定的终态观察与帧内交接.md) 纠正。SetNotify 的一次性/早绑定返回规则不变；结果仍由 pending 保留。

1. SetNotify 失败时，NewLazyApp 保留构造期一次 Invalidate，并在首帧开始后通过 `Lazy.Done()` 观察当前尝试；完成后再请求一帧，不替换原回调。
2. Done 不加载、不重试；失败显式重试使用新的通道。三处默认/选面终态出口均在同锁发布完成后关闭信号；正常未观察 Done 的路径不分配通道。
3. 诊断字段仍仅由 Lazy 写入。加载成功看 State/Report/pending，唤醒成功须额外证明后续帧实际 Take 并提交，不能只数回调。

### 2.3 生产接线顺序（`NewLazyApp` 内，冻结）

```text
w := new(app.Window); w.Option(...)
a := &App{w: w, view: view, lz: lz}
if !lz.SetNotify(func() { a.Invalidate() }) {   // 闭包捕获已构造完成的 a
        a.Invalidate()                          // 反序操作：补一次唤醒，不报错
        // 注意：NotifyLateBound 已由 SetNotify 内部置 true，这里不要再写
}
lz.Request()                                    // 首次加载的唯一触发点，不等待后台
return a, nil
```

上面是原构造顺序示意；晚绑定的完整补偿还包含 ADR-0014 规定的首帧后完成观察及事件循环退出取消，单次构造期 Invalidate 不能独立充当唤醒保证。

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

- `NewLazy` 内**不构造**临时主题（`Temp == nil` 时只记录配置，`TestFontLazyLoadNoIOOnConstruct`
  守着这条）；在**首次 `Request()` 内、返回之前**同步构造一次，之后复用，
  **严禁每帧重建**（每帧重建会把嵌入解析成本乘上帧数）。
- **"同步构造一次"是本卡承认的有界阻塞**（第 3 版问题 A）：它让 `Request()` 首次
  调用带上约 1.4–1.7 ms 成本，换取"`Request()` 返回后 `Theme()` 立刻可用"。
  不得在注释或文档里把它说成"零阻塞"，也不得据它推断首帧预算达标（§1.1）。
- **只构造一次**由 `sync.Once`（或与状态机 `LoadUnloaded→LoadLoading` 的同一个
  CAS 临界区）保证；32 goroutine 并发首次 `Request()` 也只能解析一次嵌入。
- 构造内容：`material.NewTheme()` + `Shaper = text.NewShaper(NoSystemFonts(),
  WithCollection(嵌入子集))`（方案 c，D 卡 §4.2 已冻结）。
- 成本记 `Report.TempElapsed`（实测 1.4–1.7 ms，D1 读数 4），**只写一次、重试不覆盖**，
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
// 字段归属：本卡填 §5.1 的字段；`FrameSubmittedAt` / `FirstLayoutAfterSwap`
// 由 IMPL-2 填、`Err` 由 IMPL-3 填，本卡留零值即可，
// 但**类型必须在本卡一次性定死**，后续卡不得再改结构。
type LoadReport struct {
    // ---- 本卡（IMPL-1）负责 ----
    State      LoadState
    Family     string
    Attempts   int          // Request 实际触发加载的次数（Loading 期间的合并调用不计）
    TempSource string       // 临时主题来源："embedded-subset" / "custom"
    Probe      *ProbeReport // 后台探测诊断的副本（含 Candidates / Embedded / Coverage / Err）

    RequestedAt  time.Time     // 最近一次**实际开始**加载的时刻
    LoadedAt     time.Time     // 后台**产出可用主题**的时刻；零值 = 没有产出（失败必为零）
    CompletedAt  time.Time     // 后台进入**终态**（Ready 或 Failed）的时刻；成功/失败都非零
    LoadElapsed  time.Duration // CompletedAt - RequestedAt，后台墙钟总量（口径见 §6）
    ProbeElapsed time.Duration // = Probe.Elapsed，**候选循环**实耗时（口径见 §6）
    TempElapsed  time.Duration // 临时主题构造耗时（含一次嵌入解析），首帧内，只写一次

    // 通知绑定失败的可见性（第 3 版从 IMPL-2 收回本卡）
    NotifyLateBound bool // 重复绑定 / 晚绑定 / nil 绑定，由 SetNotify 内部置位

    // ---- IMPL-2 负责 ----
    FrameSubmittedAt     time.Time     // 真主题首次**提交返回后**的时刻（代理指标，非屏幕呈现）
    FirstLayoutAfterSwap time.Duration // 切换后首次排版耗时

    // ---- IMPL-3 负责 ----
    Err error // 终态错误；失败时 errors.Is(err, contracts.ErrFontNotFound) 为真
}
```

### 5.0 为什么必须有 `CompletedAt`（第 3 版问题 B）

第 2 版把 `LoadElapsed` 定义成 `LoadedAt - RequestedAt`，而 `LoadedAt` 只在**成功**
时非零、失败时强制零值。于是失败路径会出现两个都不能接受的结论：

- 照公式算：`0 - RequestedAt` 得到一个**巨大负数**；
- 按"失败记 0"补丁：`LoadElapsed == 0`，但 IMPL-3 又要求它表示"后台总耗时"
  —— **失败也是一次真实的后台加载，耗时不可能为 0**。

两者都让"失败场景下的后台耗时"变成不可观测。所以新增一个语义独立的字段：

| 字段 | 成功路径 | 失败路径 | 含义 |
|---|---|---|---|
| `LoadedAt` | 非零，= 真主题产出时刻 | **零值** | "有没有产出可用主题" |
| `CompletedAt` | 非零，= 后台终结时刻 | **非零**，= 失败终结时刻 | "后台跑完了没有" |
| `LoadElapsed` | `CompletedAt - RequestedAt` | 同式，**> 0** | 后台墙钟总量，不区分成败 |

`CompletedAt` 的写入点冻结为**后台 goroutine 内、状态切到 `Ready`/`Failed` 的同一个
临界区**（不是渲染线程、不是 `Take()`、不是 `Report()`）。`LoadedAt` 的语义一字不改，
不采用"把 `LoadedAt` 改定义为终结时刻"的方案——那会让字段名与成功语义脱节。

### 5.1 本卡的字段级完成判据

| 字段 | 成功路径 | 失败路径 |
|---|---|---|
| `State` | `LoadReady` | `LoadFailed` |
| `Attempts` | ≥1，且 32 并发请求仍为 1 | 同上 |
| `TempSource` | `embedded-subset` | 同 |
| `Probe` | 非 nil，`Err == nil` | 非 nil，`Candidates` 有原因 |
| `LoadedAt` | 非零，且 ≥ `RequestedAt` | **零值**（不得伪造） |
| `CompletedAt` | 非零，且 ≥ `LoadedAt` | **非零**，且 ≥ `RequestedAt` |
| `LoadElapsed` | > 0，且 ≈ `CompletedAt-RequestedAt` | **> 0**（失败也记真实耗时） |
| `ProbeElapsed` | > 0 | > 0（探测跑过就有值） |
| `TempElapsed` | > 0，且多次 `Take`/`Theme`/重试都不增长 | > 0 |
| `NotifyLateBound` | 正常接线为 false；晚绑定用例为 true | 同 |

`Err` 在本卡只要求"失败时非 nil"；错误链与 `ErrFontNotFound` 的断言归 IMPL-3。

## 6. 指标口径（评审阻塞点 3，冻结；并更正原定义）

### 6.1 先承认三个事实

1. `Provider.ResolveWithReport` 的区间**不等于**"探测"：还包含 `supplement`
   （C 卡补链，可能触发嵌入解析）与嵌入兜底（`loadEmbedded`）。
2. `probe()` 把 `Read` + `ParseCollection` + `checkCoverage` 放在**同一个循环**，
   `CandidateResult` **没有**分段计时字段——**现有 API 无法把"解析"从"探测"中分离**。
3. D1 实测 `ResolveWithReport` 冷启动 **97–109 ms**、进程内重复 **81–101 ms**，
   规范 §3.2 的预算是 **≤20 ms**。这是**既有缺口**，A/B/C 阶段就存在，不是 D 引入的。

**关于 97–109 ms 的三个不许（第 3 版按评审补，容易搞反）**：

- 它测的是 **`ResolveWithReport` 冷启动**，**不是** `Probe.Elapsed`；
- 它**可以**用来证明"把整个 `ResolveWithReport` 叫成探测"不严谨；
- 它**不可以**被当成新 M2（`ProbeElapsed`）的实测值或基线，也**不可以**拿来跟
  20 ms 直接比。M2 的真实数值只能由本卡实现后实测得到，此前一律标注"待实测"。

### 6.2 冻结口径

| 指标 | 字段 | 计量边界 | 判定方式 |
|---|---|---|---|
| M2 探测段 | `ProbeElapsed` = `Probe.Elapsed` | **仅 `probe()` 的候选循环**（读文件 + 解析 + 覆盖判定），不含补链与兜底 | 记录实际值；20 ms 是**规范预算**不是通过阈值，超了如实登记 |
| M3 后台总量 | `LoadElapsed` | `CompletedAt - RequestedAt` 墙钟总量，**成功/失败都记** | 记录实际值；`LoadElapsed - ProbeElapsed` 即补链/兜底/主题构造余量 |

**字段名与注释的硬要求**：`ProbeElapsed` 这个名字**可以在实现时改得更准确**
（例如 `CandidateLoopElapsed`），但无论用哪个名字，**每一处引用**（类型注释、
`LoadReport` 注释、IMPL-3 指标表、ACCEPT 验收表的"计量边界"列）都必须写明
"候选循环（读 + 解析 + 覆盖判定）"，**不得**简写成"探测耗时"或"字体加载耗时"
—— 前者会重新把补链算进来，后者会重新变成总量。这条是评审点名要求的。

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
| `TestFontLazyLoadTempBuiltOnceConcurrent` | **32 goroutine 同时首次 `Request()`**：嵌入解析计数器 `== 1`，`TempElapsed` 只写一次且重试后不变（第 3 版问题 A） | §1.1 / §4.1 |
| `TestFontLazyLoadSetNotifyNil` | `SetNotify(nil)` → false、不 panic、永不触发；`NotifyLateBound==true`；随后正常 `SetNotify(fn)` 仍返回 false（一旦失败不再给第二次机会） | §2.1（第 3 版问题 C） |
| `TestFontLazyLoadFailureCompletedAt` | 失败路径：`LoadedAt` 零值、`CompletedAt` 非零、`LoadElapsed > 0` 且 ≈ `CompletedAt-RequestedAt`（第 3 版问题 B） | §5.0 |
| `TestFontLazyLoadReportIsCopy` | 改 `Report()` 返回的 `Candidates` 元素/切片长度、`State`，内部状态纹丝不动（深拷贝三处：`Candidates` / `Embedded` / `Coverage.Missing`） | §5 |

**关于 `TestFontLazyLoadNonBlockingOnLoading` 的边界（第 3 版补）**：它只证明
`Request()` 不等后台加载器，**不能**推断首帧预算达标——首帧预算要含窗口创建与
`TempElapsed`，由 ACCEPT 端到端判定。用例注释里必须写这一句，避免后人误读。

**假加载器怎么造**：复用 B 卡 `countingLoader` / `failingLoader` 的手法
（注入 `ProviderOptions.Embedded` + `Read`），不要用 `time.Sleep` 制造竞态；
"永不完成"用 `select {}` + 测试结束时的释放通道，避免 goroutine 泄漏。
`TempBuiltOnceConcurrent` 的"嵌入解析计数器"要挂在 `ProviderOptions.Embedded.Read`
上（真实解析路径的唯一入口），不能数 `TempElapsed` 的数值。

## 8. 验收命令（本卡自测）

```text
go test -v -run TestFontLazyLoad ./pkg/runtime/theme/
go test -run TestAsyncPathNoRenderShaper ./internal/docgate/
go vet ./pkg/runtime/theme/ ./internal/docgate/
```

**空转警告**：上述命令在用例创建前会打印 `no tests to run` 并**返回 0**。
本卡的完成证据**必须**包含 `--- PASS: TestFontLazyLoad...` 的实际输出行。

## 9. 完成判据

1. §7 **十七个**用例全部 PASS（贴 `--- PASS:` 输出，不贴退出码）；
2. `TestAsyncPathNoRenderShaper` 生效，且**做过一次反向验证**（故意在 `lazy.go`
   里加一行 `_ = th.Shaper`，门禁必须变红，然后撤回）；
3. **失败路径的后台耗时真实可读**：`LoadedAt` 零值 而 `CompletedAt` 非零、
   `LoadElapsed > 0`（第 3 版问题 B 的落地证据，不能只靠读代码声称）；
4. `NotifyLateBound` 只由 `SetNotify` 写：`grep -n "NotifyLateBound" pkg/runtime/`
   的**写入点恰好 1 处**（在 `lazy.go` 的 `SetNotify` 内），`pkg/runtime/app` 零命中
   （第 3 版问题 C）；
5. `go test ./pkg/runtime/theme/` 全绿（不得打断既有 A/B/C 用例）；
6. 产品行为**未改变**：`theme.NewTheme()` / `app.Run` 的同步路径一行未改；
7. 文档措辞自检：`lazy.go` 里**不得**出现"零阻塞""不阻塞任何调用方"之类的绝对表述，
   只能写"不等待后台加载完成"（第 3 版问题 A）。

## 10. 明确不做

- 不改 `pkg/runtime/app`（哪怕一行）；
- 不改 `theme.NewTheme()` 的语义与签名（D 卡 §11 冻结）；
- 不实现 `MarkFrameSubmitted` / `MarkFirstLayout` 的调用点（只提供幂等方法）；
- 不给 `CandidateResult` 加分段计时（A 卡范围）；
- 不碰 Linux（独立待办，见 D 卡 §12.5）。

## 11. 实施记录（2026-10-21）

### 11.1 交付物

| 文件 | 内容 |
|---|---|
| `pkg/runtime/theme/lazy.go` | `LoadState` / `LazyOptions` / `LoadReport` / `Lazy` 全部实现；**零 `.Shaper` 选择子** |
| `pkg/runtime/theme/theme_build.go` | 两个构造入口：`newTempTheme`（方案 c）与 `newThemeWithFaces`（真主题）。**两处 `.Shaper =` 集中在此** |
| `pkg/runtime/theme/lazy_test.go` | 17 个用例（§7 十六条 + `ReportIsCopy`） |
| `internal/docgate/font_lazy_test.go` | `TestAsyncPathNoRenderShaper` + 两条自检子用例 |
| `pkg/runtime/theme/font_pending_test.go` | 删除 `TestFontLazyLoad` 的 `t.Skip` 占位（D 已实现），保留前缀约定说明 |

### 11.2 实测结果

- `go test -run TestFontLazyLoad ./pkg/runtime/theme/`：**17/17 PASS**（顶层用例，含子测试共 21 个断言组）。
- `go test ./pkg/runtime/theme/ ./pkg/runtime/app/ ./internal/docgate/`：全 ok
  （theme 14.8s / app 1.4s / docgate 0.6s），既有 A/B/C 用例未被打破。
- `TestAsyncPathNoRenderShaper` **反向验证**：在 `lazy.go` 的 `load()` 里插入
  `_ = next.Shaper` → 门禁退出码 **1**，报错精确指向该文件与原因；撤回后复跑退出码 **0**。
- `grep -rn "NotifyLateBound = " pkg/ | grep -v _test.go` → **恰好 1 处**（`SetNotify` 内）；
  `pkg/runtime/app` 零命中。
- `theme.NewTheme()` / `NewThemeWithReport()` / `pkg/runtime/app` **一行未改**。

### 11.3 实现期发现的三件事（写进卡，避免后来人重踩）

1. **`Provider` 在整段探测期间持有 `Provider.mu`**（`ResolveWithReport` 里
   `defer Unlock`，`probe` 在锁内跑）。所以**探测进行中不得查询 `Scans()` /
   `Faces()` / `LastReport()`**——会一直阻塞到探测结束。
   并发合并用例的第一版就因此把测试自己测成了死锁（主 goroutine 卡在
   `p.Scans()`，后台卡在注入的 `Read`）。改为：探测中只读 `Lazy.Report().Attempts`
   （只受 `Lazy.mu` 保护），等状态终结后再查 `Scans()`。
2. **本机时钟粒度会让"快速失败"的耗时读成 0**。一次「文件不存在」的失败
   可以整个落在同一个时钟刻度内，`LoadElapsed` 与 `ProbeElapsed` 都**真的**是 0。
   这不是公式错，但让"失败也记耗时"的断言变得依赖"它恰好慢"。
   测试改为注入 2ms 读取延迟，给公式一个可测下限。
   **边界**：0 值在真实世界里仍会出现，只能说明"它快"，不能说明"没记录"。
3. **`NotifyLateBound` 的写入点要刻意收敛成一处**。初版按语义分了三段
   `return false`，自然就写了三次；验收判据是 `grep` 命中 **1 处**，
   于是合并成单点判定。判据这样定的好处是**人手可核对**：

   ```bash
   grep -rn "NotifyLateBound = " --include=*.go pkg/ | grep -v _test.go | wc -l   # 期望 1
   ```

### 11.4 未做（严格按 §10）

不改 `pkg/runtime/app`；不改同步 `NewTheme` 路径；`MarkFrameSubmitted` /
`MarkFirstLayout` 只提供幂等入口、不接线；不给 `CandidateResult` 加分段计时；
不碰 Linux。

**验收边界**：本卡全绿只代表**状态机满足契约**，不代表字体已在真实窗口中正确
切换（IMPL-2），也不代表首帧预算达标（ACCEPT）。
