package watch

import (
	"context"
	"fmt"
	"time"
)

// GenerationResult 报告一次 generate+build 的结果分类（区分资产/生成失败与入口 build 失败）。
type GenerationResult struct {
	// OK 表示整代成功（可发布）。
	OK bool
	// Stage 是失败阶段："" (成功) / "generate" / "build"。
	Stage string
	// Detail 是可读原因（真实 stderr 摘要；空则无）。
	Detail string
	// Gen 是本次构建的代次（由调用方带入）。
	Gen int
}

// Builder 是一次 generation 的执行端口（A2 提供真实实现：cli generate --check + go build）。
// 返回 GenerationResult；ctx 用于超时/取消（构建超时则整代取消、不重启旧进程）。
type Builder interface {
	Build(ctx context.Context, gen int, changedPaths []string) GenerationResult
}

// Swapper 是进程交接端口：把旧子进程替换为新构建发布（A2 真实实现负责停旧启新、PID/代次报告）。
// 返回错误表示交接失败（旧进程是否仍在由实现保证；上层不谎报已更新）。
type Swapper interface {
	Swap(ctx context.Context, gen int) error
}

// Observer 接收 Watch 生命周期事件（用于测试断言真实回调顺序与代次，不操作 UI）。
type Observer interface {
	OnGeneration(gen int, res GenerationResult, published bool)
}

// ports 打包注入端口。
type ports struct {
	builder Builder
	swapper Swapper
	obs     Observer
}

// Watch 是 Watch worker 的纯编排核心：把「扫描快照 -> 分类 -> 防抖 -> 构建 -> 交接」串成有界循环。
//
// 关键不变量：
//   - 生成中再次变更：旧代次完成不发布（final-wins），重排最新代次。
//   - 构建失败：不发布、不交接、不重启旧进程。
//   - 取消/超时：不发布。
//   - owned 变更（自激）不触发重建。
//
// Watch 不含真实定时器；由 Step/Tick 以注入时钟推进（确定性测试），或 Run 用真实 ticker（A2）。
type Watch struct {
	cfg           Config
	db            *Debouncer
	p             ports
	prev          Snapshot
	lastPublished int
	// genFailStreak 记录连续失败次数（诊断；不改变失败不重启旧进程的语义）。
	genFailStreak int
	stopped       bool
	runStart      time.Time // 注入时钟基准（供 staleness 登记 atMS 用；确定性测试可覆盖）
}

// New 构造 Watch。builder/swapper 必须非 nil（测试可注入 fake）。
func New(cfg Config, builder Builder, swapper Swapper, obs Observer) (*Watch, error) {
	if cfg.ProjectDir == "" {
		return nil, fmt.Errorf("ProjectDir 必填")
	}
	if builder == nil || swapper == nil {
		return nil, fmt.Errorf("builder/swapper 必须非 nil")
	}
	prev, err := Scan(cfg)
	if err != nil {
		return nil, fmt.Errorf("初始快照失败: %w", err)
	}
	return &Watch{cfg: cfg, db: NewDebouncer(cfg.debounce()), p: ports{builder, swapper, obs}, prev: prev, runStart: time.Now()}, nil
}

// Debouncer 暴露内部状态机（测试/诊断）。
func (w *Watch) Debouncer() *Debouncer { return w.db }

// LastPublishedGen 返回最近已发布的代次（0=尚未发布）。
func (w *Watch) LastPublishedGen() int { return w.lastPublished }

// NotifyChange 供文件系统事件/轮询发现变更时调用：登记变更路径并（按快照分类）区分 owned/用户。
// atMS 为注入时钟毫秒。返回本次登记的变更分类（供断言 owned 不触发）。
func (w *Watch) NotifyChange(ctx context.Context, atMS int, changed []string) (ChangeSet, error) {
	next, err := Scan(w.cfg)
	if err != nil {
		return ChangeSet{}, err
	}
	cs, err := ClassifyChanges(w.cfg.ProjectDir, next, changed)
	if err != nil {
		return ChangeSet{}, err
	}
	// 更新快照基线（owned 与 ignored 也更新，避免下轮重复判定）。
	w.prev = next
	// 只为**用户**变更登记防抖（owned 不触发重建）。
	for _, rel := range cs.Trigger {
		w.db.Note(atMS, rel)
	}
	return cs, nil
}

// Tick 推进注入时钟到 nowMS；若防抖窗口到期且需要构建，则执行一次「构建 -> 交接」。
// 返回本次是否实际执行了一个代次（true 表示进入了构建流程）。
//
// 该函数是 Watch 的确定性单步：测试通过固定时钟与 fake builder/swapper 驱动，断言：
//   - 突发合并为一次构建；
//   - 生成中再变更 => 旧代不发布 + 重排；
//   - 失败不交接；
//   - 取消不发布。
func (w *Watch) Tick(ctx context.Context, nowMS int) (bool, error) {
	gen := w.db.Pump(nowMS)
	if gen == 0 {
		return false, nil
	}
	return w.runGeneration(ctx, gen), nil
}

// runGeneration 执行一次构建与（条件）交接，驱动状态机的 BuildFinished。
//
// F-A3：在**构建前**与**发布/交接前**检查 ctx 取消——已取消则不构建、不发布（不丢弃成功结果来假装
// 取消，而是根本不发布交接）。F-A5：Builder 收到的是本代**冻结**的触发路径快照（GenPaths）。
func (w *Watch) runGeneration(ctx context.Context, gen int) bool {
	paths := w.db.GenPaths()

	// F-A3b-R1：本代时限 = min(外部 ctx deadline, now+BuildTimeoutMS)。作用于 generate+build 全链路。
	buildCtx := ctx
	if d := time.Duration(w.cfg.buildMS()) * time.Millisecond; d > 0 {
		var cancel context.CancelFunc
		buildCtx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	if err := buildCtx.Err(); err != nil {
		_, requeue := w.db.BuildFinished(gen, false)
		w.report(gen, GenerationResult{OK: false, Stage: "canceled", Detail: err.Error(), Gen: gen}, false, requeue)
		return true
	}

	// F-A7-R1：构建基于的输入快照基线（用于构建后重验证；Run 构建期间无法扫描，故此处比对）。
	base, baseErr := Scan(w.cfg)
	if baseErr != nil {
		_, requeue := w.db.BuildFinished(gen, false)
		w.report(gen, GenerationResult{OK: false, Stage: "scan", Detail: baseErr.Error(), Gen: gen}, false, requeue)
		return true
	}

	res := w.p.builder.Build(buildCtx, gen, paths)
	if !res.OK {
		_, requeue := w.db.BuildFinished(gen, false)
		w.genFailStreak++
		w.report(gen, res, false, requeue)
		return true
	}
	if err := buildCtx.Err(); err != nil {
		_, requeue := w.db.BuildFinished(gen, false)
		w.report(gen, GenerationResult{OK: false, Stage: "canceled", Detail: err.Error(), Gen: gen}, false, requeue)
		return true
	}

	// F-A7-R1：构建期间输入发生变化 => 该代已落后，**不发布**，重排（final-wins）。构建后重新扫描比对。
	post, postErr := Scan(w.cfg)
	if postErr != nil {
		_, requeue := w.db.BuildFinished(gen, false)
		w.report(gen, GenerationResult{OK: false, Stage: "scan", Detail: postErr.Error(), Gen: gen}, false, requeue)
		return true
	}
	changed := Diff(base, post)
	if len(changed) > 0 {
		// 只把**用户输入**变化算作“构建期间变更”；本代生成写入的 owned 产物（home_gen.go 等）
		// 不应触发 stale（否则生成即自我 stale）。用现有所有权分类过滤。
		cs, cerr := ClassifyChanges(w.cfg.ProjectDir, post, changed)
		if cerr != nil {
			_, requeue := w.db.BuildFinished(gen, false)
			w.report(gen, GenerationResult{OK: false, Stage: "scan", Detail: cerr.Error(), Gen: gen}, false, requeue)
			return true
		}
		if len(cs.Trigger) > 0 {
			atMS := int(time.Since(w.runStart).Milliseconds())
			_, _ = w.NotifyChange(ctx, atMS, cs.Trigger)
			_, requeue := w.db.BuildFinished(gen, false)
			w.report(gen, GenerationResult{OK: false, Stage: "stale", Detail: "user inputs changed during build", Gen: gen}, false, requeue)
			return true
		}
	}

	publish, requeue := w.db.BuildFinished(gen, true)
	if publish {
		if err := buildCtx.Err(); err != nil {
			w.report(gen, GenerationResult{OK: false, Stage: "canceled", Detail: err.Error(), Gen: gen}, false, false)
			return true
		}
		if serr := w.p.swapper.Swap(ctx, gen); serr != nil {
			p := res
			p.OK = false
			p.Stage = "swap"
			p.Detail = serr.Error()
			w.report(gen, p, false, false)
			return true
		}
		w.lastPublished = gen
		w.genFailStreak = 0
	}
	w.report(gen, res, publish, requeue)
	return true
}

func (w *Watch) report(gen int, res GenerationResult, published, requeue bool) {
	if w.p.obs != nil {
		w.p.obs.OnGeneration(gen, res, published)
	}
}

// Stop 标记停止（Run 循环退出条件）。
func (w *Watch) Stop() { w.stopped = true }

// Stopped 报告是否已停止。
func (w *Watch) Stopped() bool { return w.stopped }
