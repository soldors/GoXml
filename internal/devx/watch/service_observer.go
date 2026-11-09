package watch

// R27-A 返修 R2（F-A5-R1）：真实在服务身份观察接缝。
//
// lastPublished 只是“发布历史”，不等于“当前仍有进程在服务”。真实 ProcessManager 在 gen2 启动失败后
// Current()==nil（旧已停、新未起）——此时不得再向已死进程索取快照。提供可选 ServiceObserver 接口让
// Swapper 报告**实际在服务**的代次；Watch 优先使用它，缺失时回退 lastPublished（兼容只实现 Swap(error)
// 的自定义 Swapper，不要求其凭空提供运行状态）。

// ServiceObserver 是可选端口：报告当前实际在服务的代次。
type ServiceObserver interface {
	// ServingGeneration 返回 (在服务的代次, true)；无进程在服务（冷启动/失服）返回 (0, false)。
	ServingGeneration() (int, bool)
}

// servingGeneration 由 Watch 决定请求快照的旧代身份：
//   - swapper 实现 ServiceObserver => 用其真实在服务身份（0,false 表示无旧服务，冷启动/失服）。
//   - 否则回退 w.lastPublished（兼容仅实现 Swap 的 Swapper）。
func (w *Watch) servingGeneration() int {
	if obs, ok := w.p.swapper.(ServiceObserver); ok {
		if gen, serving := obs.ServingGeneration(); serving {
			return gen
		}
		return 0 // 无在服务进程：冷启动/失服，不向死进程索取
	}
	return w.lastPublished
}

// ServingGeneration 实现 ServiceObserver：以 Current() 是否仍在运行为准。
func (m *ProcessManager) ServingGeneration() (int, bool) {
	m.mu.Lock()
	c := m.current
	m.mu.Unlock()
	if c == nil || !c.Running() {
		return 0, false
	}
	return c.Gen, true
}
