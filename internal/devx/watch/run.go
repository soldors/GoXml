package watch

import (
	"context"
	"fmt"
	"time"
)

// scanFn 是 Run 循环的快照读取 seam（默认 Scan；测试可注入受控瞬时错误以证明恢复守卫）。
var scanFn = Scan

// maxTransientScanSkips 是**连续**瞬时扫描错误的最大容忍 tick 数（有界）；超过则以最后错误终止（保留真实失败）。
const maxTransientScanSkips = 40

// Run 是**可接线的轮询驱动**（F-A7）：周期性对 ProjectDir 做快照、diff、把用户变更登记进防抖，
// 并推进防抖窗口执行「构建→交接」。真实时间由 time.Ticker 提供（间隔 Config.poll()），
// 但**构建次数/触发判据由快照 diff + 防抖状态机决定**，不以墙钟推断次数。
//
// 停止：ctx 取消或 w.Stop() 后，循环在下一次 tick 前退出；退出前若正处于构建由 Tick 内部完成，
// Run 自身不另起 goroutine 残留。调用方负责在返回后 pm.Shutdown() 收口子进程树（Run 不隐式杀进程）。
//
// 返回：ctx 结束或 Stop 后 nil；快照/登记致命错误则返回该错误（不当作成功）。
func (w *Watch) Run(ctx context.Context) error {
	interval := time.Duration(w.cfg.poll()) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// F-A7b-R2：以 w.runStart 为统一时钟原点（与 Note/Pump/stale 同一坐标）。
	start := w.runStart
	transientSkips := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if w.stopped.Load() {
				return nil
			}
			// 每 tick：扫描并 diff，把**用户**变更登记进防抖（owned/ignored 不触发）。
			next, err := scanFn(w.cfg)
			if err != nil {
				// 有界、保旧：**瞬时**文件共享/锁冲突（如并发构建/编辑器短暂持有文件）不终止 Watch，
				// 跳过本 tick 保旧代继续服务；连续超过上限才以真实错误终止（保留首败诊断）。
				if isTransientScanErr(err) && transientSkips < maxTransientScanSkips {
					transientSkips++
					continue
				}
				return fmt.Errorf("scan: %w", err)
			}
			transientSkips = 0
			changed := Diff(w.prev, next)
			if len(changed) > 0 {
				atMS := int(now.Sub(start) / time.Millisecond)
				if _, nerr := w.NotifyChange(ctx, atMS, changed); nerr != nil {
					return nerr
				}
			}
			// 推进防抖窗口（注入时钟=经过毫秒），到期则构建+交接。
			nowMS := int(now.Sub(start) / time.Millisecond)
			if _, terr := w.Tick(ctx, nowMS); terr != nil {
				return terr
			}
		}
	}
}
