package hydrationchannel

// R29-A S1：**有界** nonce 防重放。原实现 `seen map[string]struct{}` 随端点生命周期**无上限增长**
// （累计 nonce 越多内存越大）。本文件提供有界候选，两种策略：
//
//   - ReplayRejectOnFull（满额拒绝）：容量用尽后，**新** nonce 一律拒绝（ErrReplayWindowFull），已保留
//     nonce 的重复仍判 ErrReplay。对**已保留窗口内**的重放严格；代价是持续高频新请求会因窗口满而被拒
//     （可用性下降）。适合“重放必须严格且请求速率可控”的场景。
//   - ReplayEvictOldest（窗口淘汰，默认）：容量用尽时淘汰最旧记录再插入；内存严格有界。**重放保证 weakened**：
//     被淘汰的旧 nonce 若再次出现将不再被判重放（其重放保护已过期）。适合同会话请求速率高、可容忍旧 nonce
//     重放窗口的场景。
//
// 会话结束回收：随 Endpoint.Stop 关闭，整个 replayGuard 随端点对象释放（无额外全局状态）。

import "sync"

// ReplayPolicy 选择满额时的行为。
type ReplayPolicy int

const (
	// ReplayEvictOldest：容量满时淘汰最旧 nonce 再插入（有界、窗口淘汰）。
	ReplayEvictOldest ReplayPolicy = iota
	// ReplayRejectOnFull：容量满时拒绝新的 nonce（满额拒绝，保留窗口内严格重放判定）。
	ReplayRejectOnFull
)

// DefaultReplayCapacity 是默认 nonce 窗口容量（候选值）。
const DefaultReplayCapacity = 4096

// replayGuard 是有界的 nonce 防重放集合（FIFO 顺序 + O(1) 成员判断）。
type replayGuard struct {
	mu       sync.Mutex
	capacity int
	policy   ReplayPolicy
	order    []string            // 插入顺序（用于淘汰最旧）
	set      map[string]struct{} // 快速判重
}

func newReplayGuard(capacity int, policy ReplayPolicy) *replayGuard {
	if capacity <= 0 {
		capacity = DefaultReplayCapacity
	}
	return &replayGuard{capacity: capacity, policy: policy, set: make(map[string]struct{})}
}

// checkAndAdd 判定并登记一个 nonce。返回：
//   - (replay=true) 若该 nonce 已在窗口内（重复请求）。
//   - (fullReject=true) 若策略为 RejectOnFull 且容量已满（拒绝该新请求，不登记）。
//   - 否则登记成功（必要时按策略淘汰最旧）。
func (g *replayGuard) checkAndAdd(nonce string) (replay bool, fullReject bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, dup := g.set[nonce]; dup {
		return true, false
	}
	if len(g.set) >= g.capacity {
		if g.policy == ReplayRejectOnFull {
			return false, true
		}
		// 窗口淘汰：删除最旧。
		if len(g.order) > 0 {
			oldest := g.order[0]
			g.order = g.order[1:]
			delete(g.set, oldest)
		}
	}
	g.set[nonce] = struct{}{}
	g.order = append(g.order, nonce)
	return false, false
}

// size 返回当前窗口中的 nonce 数（测试/诊断）。
func (g *replayGuard) size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.set)
}

// capacityOf 返回配置容量。
func (g *replayGuard) capacityOf() int { return g.capacity }
