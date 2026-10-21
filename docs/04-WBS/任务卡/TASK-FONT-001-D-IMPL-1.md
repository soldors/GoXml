# TASK-FONT-001-D-IMPL-1：异步加载器与状态机（接口已冻结，实现未开始）

## 元信息

- 状态：**接口与并发语义已冻结，实现未开始**（2026-10-21 冻结）
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D` §10 第 2 步（2a `d7268e5` + 2b `2fc61a7`）**均已完成**
- 交付物：`pkg/runtime/theme/lazy.go` + 同包测试 + `internal/docgate` 一条结构门禁
- 验收命令见 §6（**命令里的测试名必须先由本卡创建**，否则会 `no tests to run` 且退出码 0）

## 0. 本卡只做一件事

把"**加载**"从同步路径里拿出来，做成**有明确状态机、有唯一入口、不阻塞任何调用方**
的后台过程。**本卡不碰渲染**：

| 不做 | 归属 |
|---|---|
| 在 `FrameEvent` 内替换 `th.Shaper` | `TASK-FONT-001-D-IMPL-2` |
| 诊断报告对外暴露、严格失败语义 | `TASK-FONT-001-D-IMPL-3` |
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
type LazyOptions struct {
    Provider *Provider       // nil → 包内 defaultProvider（C/A/B 的既有实例）
    Family   string          // "" → FamilyCJK
    Temp     *material.Theme // nil → 方案 c（NoSystemFonts + 嵌入子集），见 D 卡 §4.2
    OnReady  func()          // 成功时在**后台 goroutine 内**同步调用；仅允许用来请求重绘
}

// Lazy 持有"当前可用主题"与后台加载进度。
type Lazy struct{ /* 未导出字段 */ }

// NewLazy 构造。**不做任何 IO**（规范 §3.3：禁止 init/构造期同步加载）。
func NewLazy(opts LazyOptions) (*Lazy, error)

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

// Report 返回最近一次加载诊断的副本（字段在本卡只填 §4 列出的那部分）。
func (l *Lazy) Report() LoadReport
```

**为什么 `Request` 同时是"首次加载"和"重试"**：D 卡 §3/§8-3 冻结了
"重试只允许显式请求触发"，但没有定义第二个入口名。合并成一个入口的好处是
**不存在"隐式触发"的路径**——所有加载都必然来自某次显式 `Request()` 调用。

## 2. 线程所有权（本卡的核心，R2 规则的前提）

| 谁 | 允许 | 禁止 |
|---|---|---|
| 调用方（任意 goroutine） | `NewLazy` / `Request` / `State` / `Theme` / `Report` | 直接读写 `Lazy` 内部字段 |
| 后台加载 goroutine | `Provider.ResolveWithReport` / `Faces` / 构造**新** `material.Theme`、填 `Report`、调 `OnReady` | **读或写任何已交给渲染线程的 `*material.Theme`**；调用 `LayoutString`；调 `OnReady` 以外的回调 |
| 渲染线程 | `Theme` / `Take` | 在 `OnReady` 内替换 shaper（必须是 `Invalidate`，替换留到帧内） |

**为什么后台只允许"构造新主题"而不是"改旧主题"**：`text.Shaper` 的集合在
构造时由 `WithCollection` 给定，没有公开换集合的 API；原地改会让内部缓存与
新集合不一致（D 卡 §5）。另外跨 goroutine 共用一个 shaper 会崩（§4.3.2，
6/6 必现）——**即使只是查询字形**。

**`OnReady` 的契约要写进注释并在测试里锚定**：生产方只允许
`func(){ w.Invalidate() }`。若在 `OnReady` 里替换 shaper，就退化成"后台替换"，
直接违反 R2。本卡用一个测试锁定：`OnReady` 回调内**断言当前 goroutine 不是渲染线程**
（由 `app` 侧在 IMPL-2 提供探针），本卡只要求 API 注释与文档冻结，实际锚定在 IMPL-2。

## 3. 三条必须在本卡落地的规则

### 规则 1（R1 的一半）：加载完成 ≠ 已显示

本卡只负责产出 **`LoadedAt`**（后台拿到可用主题的时刻）。
**不得**在这里写"显示完成"或"首帧呈现"之类字段——那属于渲染线程，归 IMPL-2。
`Report()` 必须能同时给出 `RequestedAt` 与 `LoadedAt`，且断言
`LoadedAt >= RequestedAt`。

### 规则 2（R2）：后台不得查询渲染线程的 shaper

- 代码层：`lazy.go` 内**不得出现**对 `.Shaper` 字段的引用（构造新主题时用
  `material.NewTheme()` 自己创建，不从外部借用）。
- 门禁层：新增 `internal/docgate` 用例 `TestAsyncPathNoRenderShaper`——
  用 AST 解析 `pkg/runtime/theme/lazy.go`，若出现选择子 `.Shaper` 即报红。
  理由：这条约束一旦破了，症状是**偶发 fatal error**（`recover` 无效），
  靠运行时测试抓不稳，必须前移到结构门禁。

### 规则 3：`Theme()` 永不产生副作用

这是"失败后不自动重试"能否成立的**唯一保证**。若 `Theme()` 在 `Failed` 时
顺手重试一次，那么每帧渲染都会重试——正是 D 卡 §3 明令禁止的行为。
因此：

- `Theme()` 只读；
- 首次加载由 `Request()` 显式发起（`NewLazyApp` 会调一次，见 IMPL-2）；
- `Failed` 后只有再次 `Request()` 才会重试。

## 4. 本卡填的 `Report` 字段（其余字段留给 IMPL-2/3，先占位不实现）

| 字段 | 本卡 | 含义 |
|---|---|---|
| `State LoadState` | ✅ | 当前状态 |
| `Family string` | ✅ | 请求的字体族 |
| `Attempts int` | ✅ | `Request()` 实际触发加载的次数（合并的调用不计） |
| `TempSource string` | ✅ | 临时主题来源标识（如 `embedded-subset`） |
| `Probe *ProbeReport` | ✅ | 后台探测诊断（复用 A/B/C 的既有结构） |
| `RequestedAt time.Time` | ✅ | 最近一次**实际开始**加载的时刻 |
| `LoadedAt time.Time` | ✅ | 后台产出可用主题的时刻 |
| `ProbeElapsed time.Duration` | ✅ | 探测耗时（不含等待帧） |
| `ParseElapsed time.Duration` | ✅ | 解析耗时（不含等待帧） |
| `DisplayedAt time.Time` | ❌ IMPL-2 | 真主题**首次完成呈现**的时刻 |
| `FirstLayoutAfterSwap time.Duration` | ❌ IMPL-2 | 切换后首次排版耗时 |

**`ProbeElapsed`/`ParseElapsed` 的两个禁止**（R1 明确点名）：
不得包含 `Invalidate` 之后等待下一帧的时间；不得用"从 `Request` 到某帧出现"
的差值冒充解析耗时。

## 5. 测试要求（全部以 `TestFontLazyLoad` 开头）

前缀必须保持 —— 父卡 `TASK-FONT-001.md` 的验收命令是
`go test -run TestFontLazyLoad ./pkg/runtime/theme/...`，`-run` 是**前缀匹配**，
新用例只要沿用前缀就能被父卡命令命中，不会退化成空转。

| 用例 | 断言 | 对应 D 卡 |
|---|---|---|
| `TestFontLazyLoadStateMachine` | 四态迁移合法：Unloaded→Loading→Ready/Failed；Failed→Loading 仅由 `Request` | §3 |
| `TestFontLazyLoadNoIOOnConstruct` | `NewLazy` 后 `Attempts==0`、加载器未被调用、`State==LoadUnloaded` | 规范 §3.3 |
| `TestFontLazyLoadCoalesce` | N=32 并发 `Request`：加载器**只被调用 1 次**，`Attempts==1` | D6-5 |
| `TestFontLazyLoadNonBlockingOnLoading` | 注入"永不完成"加载器：每个 `Request`/`Theme` **立即返回**（有超时护栏），加载器仍只 1 次 | D6-5a |
| `TestFontLazyLoadNoAutoRetry` | 注入失败加载器：等待后加载器计数仍为 1；连续 `Theme()` 100 次不增加 | D6-5b |
| `TestFontLazyLoadExplicitRetry` | 失败后显式 `Request()` → 计数 1→2，`Attempts==2` | D6-5b |
| `TestFontLazyLoadThemeHasNoSideEffects` | `Theme()` 在 Unloaded/Failed 下都不触发加载（计数不变） | §3 规则 3 |
| `TestFontLazyLoadTakeOnce` | `Ready` 后 `Take()` 返回真主题；再次 `Take()` 返回 nil | §1 |
| `TestFontLazyLoadFailureKeepsTemp` | `Failed` 后 `Theme()` 仍返回**临时主题**（不得返回 nil、不得伪造成功） | §6 D5 |

**假加载器怎么造**：复用 B 卡 `countingLoader` / `failingLoader` 的手法
（注入 `ProviderOptions.Embedded` + `Read`），不要用 `time.Sleep` 制造竞态；
"永不完成"用 `select {}` + 测试结束时的释放通道，避免 goroutine 泄漏到其它用例。

## 6. 验收命令（本卡自测）

```text
go test -v -run TestFontLazyLoad ./pkg/runtime/theme/
go test -run TestAsyncPathNoRenderShaper ./internal/docgate/
go vet ./pkg/runtime/theme/ ./internal/docgate/
```

**空转警告（写在这里防复发）**：上述命令在用例创建前会打印
`no tests to run` 并**返回 0**。所以本卡的完成证据**必须**包含
`--- PASS: TestFontLazyLoad...` 的实际输出行，不能只贴退出码。
（这正是 `TASK-FONT-001-D-PROBE` §1.1 同一个坑。）

## 7. 完成判据

1. §5 九个用例全部 PASS（贴输出，不贴退出码）；
2. `TestAsyncPathNoRenderShaper` 生效，且**做过一次反向验证**（故意在 `lazy.go`
   里加一行 `_ = th.Shaper`，门禁必须变红，然后撤回）；
3. `go test ./pkg/runtime/theme/` 全绿（不得因新增文件打断既有 A/B/C 用例）；
4. 产品行为**未改变**：`theme.NewTheme()` / `app.Run` 的同步路径一行未改——
   本卡只新增类型，不接线（接线是 IMPL-2）。

## 8. 明确不做

- 不改 `pkg/runtime/app`（哪怕一行）；
- 不改 `theme.NewTheme()` 的语义与签名（D 卡 §11 明确该函数签名稳定）；
- 不做布局跳动阈值（IMPL-2/ACCEPT）；
- 不碰 Linux（独立待办，见 D 卡 §12.5）。
