package theme

// 显式选面的**异步接线**：让所选面成为 Lazy 后台任务的最终产出。
//
// # 为什么需要这一层
//
// `LoadSelectedFaces` 是同步库函数：调用方必须先付出读取+解析成本。
// 若把它放在 `NewLazyApp`/`NewApp` 之前，只是把成本挪到 M1 起点之外（禁手），
// 并没有真正改善启动。本文件把同一套解析放进**既有后台 worker**：
//
//	Request() → go l.load →（选面分支）读源 → 解析所选面 → newThemeWithFaces
//	         → pending → App 帧内 Take 交换
//
// 复用范围（精确表述，避免"复用状态机"这种含糊说法）：
//
//   - **复用**：`Request` 的调度与幂等、`Attempts` 守卫、`Take` 一次性交接、
//     `App` 帧内交换，以及 `Lazy` 的状态数据与终态时间戳字段；
//   - **独立实现但语义相同**：选面路径的终态发布代码（`loadSelection`）是另写的，
//     仍保持"状态与报告在同一临界区发布"、`Probe=nil`、
//     `CompletedAt`/`LoadElapsed`/`LoadedAt` 事实一致。
//
// 不把两条终态发布硬合成一个函数：默认路径稳定且被大量既有用例锁定，
// 为文案漂亮去重构它，会把风险带到无需改动的路径上。
//
// # 与 Provider 的关系（容易误解，写清楚）
//
// 选面路径**不**使用 `opts.Provider` 做最终字体解析：不 `Resolve`、不 `Faces`、
// 不 `Read`。它只在 `opts.Temp == nil` 时仍复用既有临时主题构造
// （`newTempTheme(l.provider)` → `loadEmbedded`），因为首帧占位仍需要嵌入子集。
// 因此"零调用 Provider"的准确表述是：**最终链**零调用；默认临时主题仍走既有
// 嵌入加载路径（该路径由 `LoadReport.TempSource` 如实反映）。
//
// # 不伪造探测事实
//
// 选面路径不做 Provider 探测 ⇒ `LoadReport.Probe == nil`、`ProbeElapsed == 0`。
// 不得把解析耗时冒充 `ProbeElapsed`，也不得改写 `ProbeRan`。

import (
	"errors"
	"fmt"
	"time"

	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
)

// NewLazyWithSelection 构造一个"最终字体来自显式选面"的 Lazy。
//
// 构造期只做**纯配置校验**与必要的冻结（`freezeSelection`）：
// 不读 Path、不解析字体、不构建最终 Shaper。因此未知/损坏字体、面数越界、
// 覆盖不足等错误**不会**在这里返回，而是在 `Request()` 之后的后台 worker 里
// 进入 `LoadFailed` 终态。
//
// 参数：
//   - sel：显式源 + 面索引 + 覆盖策略（见 facesel.go）；
//   - opts：复用既有 `LazyOptions`。`Provider` 仅用于默认临时主题所需的
//     嵌入加载（`newTempTheme`），不参与最终字体解析；`Temp` 非 nil 时沿用
//     既有 custom 临时主题分支；`Family` 仅作报告标签与合法值校验。
//
// 与 `NewLazy` 的关系：默认路径零改动。本构造器设置的是一份**私有的**
// 最终加载策略，不新增 `LazyOptions` 字段，因此不影响外部位置式字面量。
func NewLazyWithSelection(sel FaceSelection, opts LazyOptions) (*Lazy, error) {
	family := opts.Family
	if family == "" {
		family = FamilyCJK
	}
	// 与 NewLazy 同一套族名校验：显式选面不再用 family 触发系统探测，
	// 但 family 仍是报告标签，故合法值必须一致。
	switch family {
	case FamilyCJK, FamilyBody, FamilyMono:
	default:
		return nil, fmt.Errorf("%w: unknown family %q (合法值: %s, %s, %s)",
			contracts.ErrFontNotFound, family, FamilyCJK, FamilyBody, FamilyMono)
	}

	cfg, err := freezeSelection(sel)
	if err != nil {
		return nil, err
	}

	p := opts.Provider
	if p == nil {
		p = defaultProvider
	}

	lz := &Lazy{
		provider: p,
		family:   family,
		tempOpt:  opts.Temp,
		state:    LoadUnloaded,
		rep:      LoadReport{State: LoadUnloaded, Family: family},
		sel:      cfg,
	}
	// 内联字节由 Lazy 独占持有（cfg 不再持有第二份），避免同一份数据两处引用。
	if cfg.hasInlineBytes {
		lz.selInline = cfg.inlineBytes
		cfg.inlineBytes = nil
	}
	return lz, nil
}

// selSnapshotOwnedErr 在**报告读取边界**为调用方构造独立的错误链副本。
//
// 为什么必须在读取边界而不是"发布前 clone 一次"：
// l.rep.Err 只发布一次，但 Report() 可以被调用任意多次；若不多次隔离，
// 多个调用方会共享同一个 *SelectionError（及其 Report 切片），
// 一方修改就会污染另一方的观测——这与 LoadReport 文档承诺的"返回副本"矛盾。
//
// 做法：重建与发布时**相同格式**的外层包装（保持 errors.Is(ErrFontNotFound)
// 与 errors.As(*SelectionError)），内部换成 clone 出来的自有对象；
// 非自有错误（IO 原因等）原样保留，不做通用深拷贝、不用反射/unsafe。
func selSnapshotOwnedErr(err error) error {
	if err == nil {
		return nil
	}
	var se *SelectionError
	if !errors.As(err, &se) {
		return err // 非本层自有错误：沿用 Go 只读惯例
	}
	return fmt.Errorf("%w: %w", contracts.ErrFontNotFound, se.clone())
}

// SelectionReport 返回最近一次"选面尝试"的诊断**深拷贝**。
//
// 与 `LoadReport` 分开：`LoadReport` 是冻结的四指标报告，语义属 Provider 探测链；
// 选面诊断只描述本次选择（请求/实际索引、面元数据、覆盖事实、错误分类）。
//
// 尚未发起过尝试、或该 Lazy 不是选面路径时返回 nil。
func (l *Lazy) SelectionReport() *SelectionReport {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.selRep == nil {
		return nil
	}
	cp := l.selRep.clone()
	return &cp
}

// loadSelection 是选面路径的 worker 主体（由 `Lazy.load` 分派进入）。
//
// 它必须与 `load` 的终态写入语义**完全一致**：
//   - attempt 守卫（被新尝试取代则丢弃结果）；
//   - 状态与报告在**同一临界区**发布；
//   - `Probe=nil`、`ProbeElapsed=0`（本轮确实没有探测）；
//   - `CompletedAt`/`LoadElapsed`/`LoadedAt` 事实如实；
//   - 通知在**锁外**发（回调可能重入 State()/Report()）。
func (l *Lazy) loadSelection(attempt int, notify func()) {
	// ① 取下本次尝试需要的快照（避免与释放逻辑竞争）。
	l.mu.Lock()
	if attempt != l.attempts {
		l.mu.Unlock()
		return
	}
	cfg := l.sel
	inline := l.selInline
	l.mu.Unlock()

	if cfg == nil {
		// 理论上不可达：只有 NewLazyWithSelection 会设置 sel。
		l.publishSelectionFailure(attempt, notify, SelectionInvalidConfig, selPhaseSource,
			fmt.Errorf("内部错误：选面路径缺少冻结配置"), nil)
		return
	}

	// ② 读取源（Path 在 worker 内读；Bytes 用构造期快照）。
	var data []byte
	if cfg.hasInlineBytes {
		if len(inline) == 0 {
			l.publishSelectionFailure(attempt, notify, SelectionInvalidConfig, selPhaseSource,
				fmt.Errorf("内联字体字节已被释放或为空，无法重试"), nil)
			return
		}
		data = inline
	} else {
		b, _, rerr := readSelectionSource(cfg)
		if rerr != nil {
			var se *SelectionError
			kind, phase := SelectionSourceIO, selPhaseSource
			if errors.As(rerr, &se) {
				kind, phase = se.Report.ErrorKind, se.Report.ErrorPhase
			}
			l.publishSelectionFailure(attempt, notify, kind, phase, rerr, nil)
			return
		}
		data = b
	}

	// ③ 解析所选面（与同步 loader 共用同一核心）。
	faces, rep, lerr := loadSelectionBytes(cfg, data)

	var next *material.Theme
	if lerr == nil {
		next = newThemeWithFaces(faces)
	}

	// ④ 终态发布（与 lazy.load 同构）。
	now := time.Now()
	l.mu.Lock()
	if attempt != l.attempts {
		l.mu.Unlock()
		return
	}
	// 选面路径没有探测：Probe 保持 nil、ProbeElapsed 保持 0（Request 已复位）。
	l.rep.CompletedAt = now
	l.rep.LoadElapsed = now.Sub(l.rep.RequestedAt)
	repCopy := rep.clone()
	l.selRep = &repCopy
	if lerr != nil {
		l.state = LoadFailed
		l.rep.State = LoadFailed
		// 同时满足两件事：errors.Is(err, ErrFontNotFound) 为真（既有
		// StrictFontFailure 与诊断约定继续可用），且 errors.As 能取回
		// *SelectionError 的原因链。Go 1.20+ 支持多 %w。
		l.rep.Err = fmt.Errorf("%w: %w", contracts.ErrFontNotFound, lerr)
		l.pending = nil
		// LoadedAt 保持零值：没有产出可用主题，不得伪造。
	} else {
		l.state = LoadReady
		l.rep.State = LoadReady
		l.rep.Err = nil
		l.rep.LoadedAt = now
		l.pending = next
		// Ready 之后不再需要内联快照（不会再用它重试）；释放以免长期占用。
		l.selInline = nil
	}
	l.mu.Unlock()

	if notify != nil {
		notify()
	}
}

// publishSelectionFailure 是"读源之前就失败"的统一出口，避免重复临界区代码。
func (l *Lazy) publishSelectionFailure(attempt int, notify func(), kind, phase string, err error, extra *SelectionReport) {
	now := time.Now()
	l.mu.Lock()
	if attempt != l.attempts {
		l.mu.Unlock()
		return
	}
	l.rep.CompletedAt = now
	l.rep.LoadElapsed = now.Sub(l.rep.RequestedAt)
	base := SelectionReport{}
	if extra != nil {
		base = extra.clone()
	} else if l.sel != nil {
		base.Label = l.sel.label
		base.SourceName = l.sel.name
		base.RequestedIndices = append([]int(nil), l.sel.indices...)
	}
	se := newSelectionError(kind, phase, base, err)
	rc := se.Report.clone()
	l.selRep = &rc
	l.state = LoadFailed
	l.rep.State = LoadFailed
	l.rep.Err = fmt.Errorf("%w: %w", contracts.ErrFontNotFound, se)
	l.pending = nil
	l.mu.Unlock()

	if notify != nil {
		notify()
	}
}
