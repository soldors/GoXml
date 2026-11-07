package watch

import (
	"context"
	"time"
)

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
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if w.stopped.Load() {
				return nil
			}
			// 每 tick：扫描并 diff，把**用户**变更登记进防抖（owned/ignored 不触发）。
			next, err := Scan(w.cfg)
			if err != nil {
				return err
			}
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
