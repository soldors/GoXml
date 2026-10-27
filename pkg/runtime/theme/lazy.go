package theme

import (
	"fmt"
	"sync"
	"time"

	giofont "gioui.org/font"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// TASK-FONT-001-D-IMPL-1：异步加载器与状态机
//
// 契约见 docs/04-WBS/任务卡/TASK-FONT-001-D-IMPL-1.md（第 3 版）。
//
// # 本文件的三条硬约束
//
//  1. **状态只有一个事实来源**：`Lazy.mu` 保护的字段。`Request`、后台
//     goroutine、`Take`、`Theme`、`Report` 全部经由同一把锁读写，不存在
//     "状态已 Failed 但报告还写着 Loading"的中间态——状态与报告在**同一个
//     临界区**里一起更新。
//  2. **不碰渲染线程的 sha(p)er**：本文件不出现任何 `.Shaper` 选择子
//     （由 `internal/docgate` 的 `TestAsyncPathNoRenderShaper` 静态保证）。
//     构造主题的事在 `theme_build.go`，那产出的是**新的**主题对象。
//  3. **不阻塞等后台**：`Request` 只在首次做一次有界的同步初始化
//     （临时主题），随后立刻返回；后台只负责探测/解析/交付。
//
// # 为什么"加载完成"与"已提交"必须分开
//
// `LoadedAt` 是后台**产出**可用主题的时刻；主题真正被画到帧上要等渲染线程
// 下一帧 `Take` + 提交，那是 IMPL-2 的 `FrameSubmittedAt`。两者之间隔着一次
// `Invalidate` 调度（2b 实测帧在途时 75.7–105.2 ms），**不得**合并成一个字段，
// 也不得互相替代。
// ============================================================================

// LoadState 是异步加载状态（与 D 卡 §3 状态机一一对应）。
type LoadState int

const (
	// LoadUnloaded 构造完成，未做任何 IO。
	LoadUnloaded LoadState = iota
	// LoadLoading 后台探测 + 解析中。
	LoadLoading
	// LoadReady 真主题已产出，等待渲染线程取走。
	LoadReady
	// LoadFailed 系统与嵌入两条链路均失败。
	LoadFailed
)

// String 返回可读的状态名（诊断与测试输出用）。
func (s LoadState) String() string {
	switch s {
	case LoadUnloaded:
		return "unloaded"
	case LoadLoading:
		return "loading"
	case LoadReady:
		return "ready"
	case LoadFailed:
		return "failed"
	}
	return "unknown"
}

// LazyOptions 配置一次异步加载。
//
// 注意：这里**没有** OnReady 回调。通知必须在窗口句柄创建之后、`Request`
// 之前用 `SetNotify` 绑定——构造 `Lazy` 时窗口还不存在，把回调塞进构造参数
// 会造成"通知哪个窗口"无从冻结（IMPL-1 §2）。
type LazyOptions struct {
	// Provider 字体来源；nil 表示包内 `defaultProvider`（与同步路径同一实例）。
	Provider *Provider
	// Family 字体族；空表示 `FamilyCJK`。
	Family string
	// Temp 显式指定的临时主题；nil 表示方案 c（NoSystemFonts + 嵌入子集）。
	Temp *material.Theme
}

// LoadReport 是一次异步加载的完整诊断快照。
//
// # 可变性
//
// 所有字段由 `Lazy` 在内部锁保护下写入；调用方通过 `Report()` 拿到的**永远
// 是副本**（切片与嵌套指针深拷贝）。改动副本不影响内部状态，也不影响后续
// `Report()` 的返回值。
//
// # 字段归属
//
// 本卡填全部计时/状态字段；`FrameSubmittedAt` 与 `FirstLayoutAfterSwap`
// 由 IMPL-2 的渲染接线调用 `MarkFrameSubmitted` / `MarkFirstLayout` 写入
// （本卡只提供幂等入口）；`Err` 的语义细化归 IMPL-3。**类型在本卡一次定死**，
// 后续卡不得再改结构。
type LoadReport struct {
	// ---- 状态机 ----
	State      LoadState
	Family     string
	Attempts   int    // Request 实际触发加载的次数（Loading 期间的合并调用不计）
	TempSource string // 临时主题来源：embedded-subset / custom / gio-default
	// Probe 是探测诊断的副本（含 Candidates / Embedded / Coverage / Err）。
	//
	// **它可以保留历史探测留下的诊断内容**（D-ACCEPT-07 裁决 ②）：
	// 缓存命中时本轮没有执行探测，但历史报告里的候选状态、嵌入链路、
	// 覆盖判定对排查仍有价值，因此**不得**因为命中就清空。
	//
	// 由此必须与本轮指标分开看：
	// `Probe.Elapsed` 是"这份报告记录的那次探测"的耗时（可能是历史值），
	// 本轮的耗时看 `ProbeElapsed`，本轮有没有探测看 `Probe.ProbeRan`。
	Probe *ProbeReport

	// ---- 计时（四个指标见 IMPL-3 §4，禁止互相替代、禁止合数）----
	RequestedAt time.Time     // 最近一次**实际开始**加载的时刻
	LoadedAt    time.Time     // 后台**产出可用主题**的时刻；零值 = 没有产出（失败必为零）
	CompletedAt time.Time     // 后台进入**终态**（Ready 或 Failed）的时刻；成败都非零
	LoadElapsed time.Duration // CompletedAt - RequestedAt，后台墙钟总量
	// ProbeElapsed 是**本轮**加载实际执行探测所耗费的时间
	// （`probe()` 候选循环：读文件 + 解析 + 覆盖判定）。
	//
	// 四个"不是"（D-ACCEPT-07 冻结，避免与历史值混淆）：
	//
	//	- 不是 `ResolveWithReport` 的总量（那还含补链与嵌入兜底）；
	//	- 不是"纯解析耗时"（现有 API 分不开，见 IMPL-1 §6）；
	//	- 不是 `Probe.Elapsed`——后者是"该报告记录的那次探测"的耗时，
	//	  缓存命中时**可能是历史值**；
	//	- **0 不等于没耗时、也不等于失败**：它只表示"本轮没有记录到探测耗时"，
	//	  本轮要走没走探测看 `Probe.ProbeRan`。
	//
	// 本轮没执行探测时保持 0（由 `Request` 侧的字段清理归零，写入处只做门控）。
	ProbeElapsed time.Duration
	// TempElapsed 是临时主题构造耗时（含一次嵌入解析），属首帧成本，只写一次。
	TempElapsed time.Duration

	// NotifyLateBound 表示**曾有一次 `SetNotify` 没有生效**（重复绑定 / 晚绑定 / nil）。
	//
	// 语义边界：它为 true **不**等价于"完全没有通知通道"——若先成功绑定、
	// 之后又有一次重复调用失败，通道其实还在。要不要补 `Invalidate`，一律
	// 看 `SetNotify` 的返回值，不看这个字段。
	NotifyLateBound bool

	// ---- IMPL-2 负责 ----
	FrameSubmittedAt     time.Time     // 真主题首次**提交返回后**的时刻（代理指标，非屏幕呈现）
	FirstLayoutAfterSwap time.Duration // 切换后首次排版耗时

	// ---- IMPL-3 负责 ----
	Err error // 终态错误；失败时 errors.Is(err, contracts.ErrFontNotFound) 为真
}

// Lazy 持有"当前可用主题"与后台加载进度。
type Lazy struct {
	mu sync.Mutex

	provider *Provider
	family   string
	// tempOpt 是调用方显式给的临时主题；nil 表示走方案 c。
	tempOpt *material.Theme

	// 临时主题（首次 Request 内同步构造一次，之后复用）
	tempBuilt   bool
	temp        *material.Theme
	tempSource  string
	tempElapsed time.Duration

	// 状态机
	state    LoadState
	attempts int

	// 通知（绑定只允许一次，见 SetNotify）
	notify        func()
	notifyDecided bool

	// 主题交接
	// apply 是**当前生效**的主题：临时主题构造后指向它；Take 之后指向真主题。
	// pending 是已产出但尚未被渲染线程取走的真主题。
	apply   *material.Theme
	pending *material.Theme

	rep LoadReport

	// sel 非 nil 时本 Lazy 走"显式选面"后台加载：最终字体来自
	// FaceSelection，而不是 Provider 探测链（见 lazy_facesel.go）。
	// 该字段只在 NewLazyWithSelection 中赋值一次，之后只读。
	sel *selectionConfig
	// selInline 是 Bytes 源的构造期快照，由本 Lazy 独占；
	// Ready 后释放（不再用于重试）。访问时须持 mu。
	selInline []byte
	// selRep 是最近一次选面尝试的诊断；与 rep 在同一临界区发布，
	// 对外只经 SelectionReport() 深拷贝暴露。访问时须持 mu。
	selRep *SelectionReport

	// 一次性标记（幂等写入，见 MarkFrameSubmitted / MarkFirstLayout）
	frameSubmitted bool
	firstLayout    bool
}

// NewLazy 构造。**不做任何 IO**（规范 §3.3：禁止 init/构造期同步加载）。
func NewLazy(opts LazyOptions) (*Lazy, error) {
	family := opts.Family
	if family == "" {
		family = FamilyCJK
	}
	// 只校验族名，不探测：未知族名到 Request 后由 Provider 严格失败。
	switch family {
	case FamilyCJK, FamilyBody, FamilyMono:
	default:
		return nil, fmt.Errorf("%w: unknown family %q (合法值: %s, %s, %s)",
			contracts.ErrFontNotFound, family, FamilyCJK, FamilyBody, FamilyMono)
	}
	p := opts.Provider
	if p == nil {
		p = defaultProvider
	}
	return &Lazy{
		provider: p,
		family:   family,
		tempOpt:  opts.Temp,
		state:    LoadUnloaded,
		rep:      LoadReport{State: LoadUnloaded, Family: family},
	}, nil
}

// SetNotify 绑定"加载完成"通知。返回是否绑定成功。
//
// 语义（逐条冻结，实现不得自行选择）：
//   - 只允许在 `Request()` 之前成功调用**一次**；重复调用返回 false；
//   - 已 `Request` 过再调用返回 false（不得 panic，也不得静默吞掉：
//     调用方必须自己补一次 `Invalidate`）；
//   - `fn == nil` 视为无效绑定：返回 false、不绑定、不 panic、永不触发；
//   - 绑定成功后，加载进入终态（Ready **或** Failed）时在**后台 goroutine 内**
//     同步调用一次 `fn`；
//   - `fn` 内**只允许请求重绘**（生产形态是窗口的 `Invalidate`），
//     不得替换任何渲染状态；
//   - 上述三种失败都在本方法的锁内置 `NotifyLateBound`，调用方与
//     `pkg/runtime/app` 不得直接写该字段。
func (l *Lazy) SetNotify(fn func()) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 三个条件缺一即失败：还没做过决定、还没发过请求、回调非 nil。
	// 写成单点判定而不是三个 return 分支，是为了让 `NotifyLateBound`
	// **只有一个赋值点**（验收判据要求 grep 命中数 == 1，便于人手核对）。
	if l.notifyDecided || l.state != LoadUnloaded || fn == nil {
		if fn == nil {
			// nil 之后不再给第二次机会：否则"绑定成功与否"就没有确定答案。
			l.notifyDecided = true
		}
		l.rep.NotifyLateBound = true
		return false
	}
	l.notify = fn
	l.notifyDecided = true
	return true
}

// Theme 返回当前**生效**的主题，纯读取、永不产生副作用。
//
// 这条"无副作用"是"失败后不自动重试"能成立的唯一保证：若它在 Failed 时
// 顺手重试一次，那么每帧渲染都会重试（D 卡 §3 明令禁止）。
//
// 返回 nil 的唯一情形：还没调用过 `Request()`——构造不做 IO，此时没有主题
// 可以返回。调用方（`NewLazyApp`）必须 `Request` 之后再取。
func (l *Lazy) Theme() *material.Theme {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.apply
}

// State 返回当前状态。
func (l *Lazy) State() LoadState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

// Request 是**唯一**的加载入口：首次加载 + 失败后的显式重试。
//
// 任意 goroutine 可调用。语义（IMPL-1 §1.1 / §3 冻结）：
//
//   - **首次调用**会在返回前同步构造一次临时主题（有界，约 1.4–1.7 ms，
//     记 `TempElapsed`）——这是本卡承认的同步成本，不得表述为"零阻塞"；
//   - 此后只做"投递后台任务 + 立即返回"，**不等待**探测/解析/最终主题；
//   - `Loading` 期间调用立即返回且不计入 `Attempts`（合并）；
//   - `Ready` 之后调用是 no-op（不重新加载）；
//   - `Failed` 之后只有再次调用本方法才重试（不存在隐式重试路径）。
func (l *Lazy) Request() {
	l.mu.Lock()

	// ① 首次：同步构造临时主题，且只构造一次。
	//
	// 放在锁内是有意的：32 个 goroutine 并发首次调用时，只可能有一个真的
	// 进入这段，其余在锁上等待后看到 tempBuilt 已经为 true。
	if !l.tempBuilt {
		if l.tempOpt != nil {
			l.temp = l.tempOpt
			l.tempSource = tempSourceCustom
		} else {
			start := time.Now()
			th, source := newTempTheme(l.provider)
			l.tempElapsed = time.Since(start)
			l.temp = th
			l.tempSource = source
		}
		if l.apply == nil {
			l.apply = l.temp
		}
		l.tempBuilt = true
		l.rep.TempSource = l.tempSource
		l.rep.TempElapsed = l.tempElapsed
	}

	// ② 只在"未开始"或"已失败"时启动后台任务。
	switch l.state {
	case LoadLoading, LoadReady:
		l.mu.Unlock()
		return
	}
	l.state = LoadLoading
	l.attempts++
	l.rep.State = LoadLoading
	l.rep.Attempts = l.attempts
	l.rep.RequestedAt = time.Now()
	// 新的尝试开始：上一轮的终态字段必须清掉，否则失败重试时会读到上一轮的
	// 时间戳与探测诊断，把"这一轮还没完成"误读成"这一轮很快"。
	l.rep.LoadedAt = time.Time{}
	l.rep.CompletedAt = time.Time{}
	l.rep.LoadElapsed = 0
	l.rep.ProbeElapsed = 0
	l.rep.Probe = nil
	l.rep.Err = nil
	// 新尝试开始：上一轮的选面诊断必须清空，否则失败重试会读到上一轮索引/
	// 覆盖事实，把"这一轮还没完成"误读成"上一轮的结论就是这一轮的结论"。
	l.selRep = nil
	attempt := l.attempts
	notify := l.notify
	l.mu.Unlock()

	go l.load(attempt, notify)
}

// load 是后台加载的全部内容：探测 → 反查集合 → 构造真主题 → 终态写入 → 通知。
func (l *Lazy) load(attempt int, notify func()) {
	// 选面路径：最终字体来自 FaceSelection 而不是 Provider 探测链。
	// 分支放在最前，其余（状态机/终态/通知）与默认路径共用同一套语义。
	if l.sel != nil {
		l.loadSelection(attempt, notify)
		return
	}

	id, probeRep, err := l.provider.ResolveWithReport(l.family)

	var faces []giofont.FontFace
	if err == nil {
		var ok bool
		faces, ok = l.provider.Faces(id)
		if !ok || len(faces) == 0 {
			err = fmt.Errorf("%w: 标识 %q 无法反查到字体集合", contracts.ErrFontNotFound, id)
		}
	}

	var next *material.Theme
	if err == nil {
		next = newThemeWithFaces(faces)
	}

	now := time.Now()

	// ③ 状态与报告在**同一个临界区**里一起更新。
	//
	// 这是"状态机只有一个事实来源"的落点：不可能出现 State=Failed 而
	// CompletedAt 还是零、或 State=Ready 而报告仍写 Loading 的中间态。
	l.mu.Lock()
	if attempt != l.attempts {
		// 已被更新的尝试取代（理论上不该发生：重试只允许从 Failed 发起，
		// 而 Failed 意味着上一轮已经结束）。防御性丢弃，绝不覆盖新结果。
		l.mu.Unlock()
		return
	}
	l.rep.Probe = copyProbeReport(probeRep)
	// ProbeElapsed 是**本轮**指标：只有本轮真的执行了探测流程才记录。
	//
	// 缓存命中时 `ProbeRan == false`，本轮没有探测耗时 ⇒ **保持 0**，
	// **不得**沿用 `Probe.Elapsed` 里的历史值——那会把上一次的耗时
	// 伪装成本轮的耗时，进而让 `LoadElapsed >= ProbeElapsed` 这类判定
	// 建立在不同源的测量上（D-ACCEPT-07 实证：命中时 `Now()` 根本没被调用）。
	//
	// 不赋值即可为 0：`Request` 侧在每次新尝试时已把本字段归零。
	if probeRep != nil && probeRep.ProbeRan {
		l.rep.ProbeElapsed = probeRep.Elapsed
	}
	l.rep.CompletedAt = now
	l.rep.LoadElapsed = now.Sub(l.rep.RequestedAt)
	if err != nil {
		l.state = LoadFailed
		l.rep.State = LoadFailed
		l.rep.Err = err
		l.pending = nil
		// LoadedAt 保持零值：没有产出可用主题，不得伪造。
	} else {
		l.state = LoadReady
		l.rep.State = LoadReady
		l.rep.Err = nil
		l.rep.LoadedAt = now
		l.pending = next
	}
	l.mu.Unlock()

	// ④ 通知在锁外发：回调里可能调用 Report()/State()，持锁调用会自锁。
	if notify != nil {
		notify()
	}
}

// Take 供渲染线程在帧内取走待应用的真主题；返回 nil 表示没有待应用项。
//
// **一次性**：取走后 `apply` 指向它，再次调用返回 nil，直到下一次 Ready。
// **只允许在渲染线程调用**（并配合同一个 goroutine 内的替换动作，见 IMPL-2）。
func (l *Lazy) Take() *material.Theme {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending == nil {
		return nil
	}
	th := l.pending
	l.pending = nil
	l.apply = th
	return th
}

// MarkFrameSubmitted 记录真主题首次**提交返回后**的时刻，只生效一次（幂等）。
//
// 语义上界止于「帧提交已返回」：它**不是**屏幕呈现时刻（PROBE §8.4 未分离
// present 与调度）。写入责任在渲染线程，本卡只提供入口。
func (l *Lazy) MarkFrameSubmitted(at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.frameSubmitted {
		return
	}
	l.frameSubmitted = true
	l.rep.FrameSubmittedAt = at
}

// MarkFirstLayout 记录切换后首次排版耗时，只生效一次（幂等）。
func (l *Lazy) MarkFirstLayout(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.firstLayout {
		return
	}
	l.firstLayout = true
	l.rep.FirstLayoutAfterSwap = d
}

// Report 返回最近一次加载诊断的**副本**。
//
// 深拷贝三处：`Candidates` 切片、`Embedded` 指针、`Coverage.Missing` 切片。
// 只复制最外层结构是不够的——调用方 append 一下 `Missing` 就会写进内部状态。
func (l *Lazy) Report() LoadReport {
	l.mu.Lock()
	defer l.mu.Unlock()
	cp := l.rep
	cp.Probe = copyProbeReport(l.rep.Probe)
	// 选面路径：本层自有的 *SelectionError 是可变对象，必须**每次读取**都
	// 给调用方一份独立副本；否则多个 Report() 调用方共享同一指针，
	// 一方改 Report 字段就污染另一方的观测（默认路径无此分支，行为不变）。
	if l.sel != nil {
		cp.Err = selSnapshotOwnedErr(l.rep.Err)
	}
	return cp
}

// copyProbeReport 深拷贝一份探测诊断（nil 安全）。
func copyProbeReport(r *ProbeReport) *ProbeReport {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Candidates = append([]CandidateResult(nil), r.Candidates...)
	if r.Embedded != nil {
		e := *r.Embedded
		cp.Embedded = &e
	}
	if r.Coverage != nil {
		c := *r.Coverage
		c.Missing = append([]rune(nil), r.Coverage.Missing...)
		cp.Coverage = &c
	}
	return &cp
}
