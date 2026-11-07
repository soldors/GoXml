package watch

import "sort"

// Debouncer 是纯状态机：以**注入的时钟毫秒**驱动，不含真实定时器/goroutine，便于确定性断言。
//
// 语义（§8 100ms 防抖 + 末代获胜 + 取消 + 构建中再变更）：
//   - Note(atMS, path)：登记一次用户文件变更；返回 (立即触发?, 目标代次)。
//   - 每次登记都会把“当前候选代次”累加并将 deadline 推到 atMS+debounce；突发被合并为一次。
//   - 只有在窗口静默期满（Pump 且 now>=deadline 且无进行中构建）时才触发一次构建。
//   - Pump(nowMS)：推进时钟；若窗口到期且空闲，返回要构建的“目标代次”（>0 表示开始一次构建），
//     否则返回 0。
//   - BuildStarted(gen)：标记代次 gen 开始构建；此时若又有变更，会被记录为“已落后”。
//   - BuildFinished(gen, ok)：构建结束；返回 (是否需要发布该代次?, 是否需要立即重排下一次构建?)。
//   - 若该 gen < 最新目标代次（构建期间又有变更）=> 不发布（旧构建完成不得发布已落后的应用代次），
//     并重排一次构建（final-wins）。
//   - 失败 => 不发布、不重启旧进程（发布判定 false），后续由调用方决定是否等待下一次变更。
//   - Cancel()：取消当前待发布/进行中的构建；之后不得发布该代次。
//
// 该状态机不涉及真实时间；墙钟只由调用方（Pump 的入参）提供，用于超时/定时，不作为执行次数证明。
type Debouncer struct {
	debounce   int
	now        int
	deadline   int
	latestGen  int // 最新目标代次（随每次变更 ++）
	pending    bool
	building   bool
	buildGen   int  // 正在构建的代次（0=无）
	cancelled  bool // 当前构建/待发布是否被取消
	paths      map[string]bool
	dirtySince int      // 最近一次变更时间（诊断）
	genPaths   []string // 进入当前构建时冻结的触发路径快照（F-A5：Pump 消费前冻结，避免被清空）
}

// NewDebouncer 构造防抖状态机；debounceMS<=0 取 DefaultDebounceMS。
func NewDebouncer(debounceMS int) *Debouncer {
	if debounceMS <= 0 {
		debounceMS = DefaultDebounceMS
	}
	return &Debouncer{debounce: debounceMS, paths: map[string]bool{}}
}

// Debounce 返回防抖窗口毫秒。
func (d *Debouncer) Debounce() int { return d.debounce }

// Note 登记一次用户变更路径（atMS 为注入时钟毫秒）。返回当前最新目标代次。
func (d *Debouncer) Note(atMS int, path string) int {
	if atMS > d.now {
		d.now = atMS
	}
	d.latestGen++
	d.pending = true
	d.deadline = atMS + d.debounce
	d.paths[path] = true
	d.dirtySince = atMS
	return d.latestGen
}

// PendingPaths 返回当前累积、尚未被一次构建消费的变更路径（稳定排序）。
func (d *Debouncer) PendingPaths() []string {
	out := make([]string, 0, len(d.paths))
	for p := range d.paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// sortedPaths 返回 paths 的稳定排序副本（/ 分隔，字符串序）。
func (d *Debouncer) sortedPaths() []string {
	out := make([]string, 0, len(d.paths))
	for p := range d.paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// GenPaths 返回**当前构建代次冻结的触发路径快照**（F-A5）。空闲时返回 nil。
func (d *Debouncer) GenPaths() []string {
	if !d.building {
		return nil
	}
	out := make([]string, len(d.genPaths))
	copy(out, d.genPaths)
	return out
}

// LatestGen 返回最新目标代次。
func (d *Debouncer) LatestGen() int { return d.latestGen }

// BuildGen 返回正在构建的代次（0 表示空闲）。
func (d *Debouncer) BuildGen() int { return d.buildGen }

// Pump 推进注入时钟到 nowMS。窗口到期且空闲时返回 (目标代次, 本代冻结路径快照)；否则 (0, nil)。
// **冻结**：进入构建前把当前 pending 路径排序后冻结到 genPaths（F-A5），再清空 pending，
// 使 Builder 始终拿到本代触发路径，不被后续清空/新代变化覆盖。
func (d *Debouncer) Pump(nowMS int) int {
	if nowMS > d.now {
		d.now = nowMS
	}
	if d.building || !d.pending {
		return 0
	}
	if d.now < d.deadline {
		return 0
	}
	// 窗口静默期满且空闲：开始构建最新目标代次。
	d.genPaths = d.sortedPaths() // F-A5：先冻结本代触发路径快照
	d.building = true
	d.buildGen = d.latestGen
	d.pending = false
	d.cancelled = false
	d.paths = map[string]bool{}
	return d.buildGen
}

// BuildFinished 报告代次 gen 的构建结果。返回值：
//   - publish：是否应发布（切换）该代次。
//   - requeue：是否应立即安排一次新的构建（构建期间又有变更，final-wins）。
func (d *Debouncer) BuildFinished(gen int, ok bool) (publish, requeue bool) {
	if !d.building || gen != d.buildGen {
		// 非当前构建（已被取消/替换）=> 不发布。
		return false, false
	}
	d.building = false
	d.buildGen = 0
	if d.cancelled {
		// 取消：不发布；若期间又有变更，仍需按最新重新构建。
		return false, d.pending
	}
	if !ok {
		// 构建失败：不发布、不重启旧进程；不自动重排（等待下一次显式变更）。
		return false, false
	}
	if gen < d.latestGen {
		// 构建期间又有变更：旧构建已落后，不发布；立刻重排（final-wins）。
		d.deadline = d.now + d.debounce
		if !d.pending {
			d.pending = true
		}
		return false, true
	}
	// 最新代次且成功：发布。
	return true, false
}

// Cancel 取消当前待发布/进行中的构建。此后该代次不得发布。
func (d *Debouncer) Cancel() {
	if d.building || d.pending {
		d.cancelled = true
	}
	// 若在等待窗口（未开始构建），取消也应清掉待办，避免随后被 Pump 触发。
	if !d.building {
		d.pending = false
		d.paths = map[string]bool{}
	}
}
