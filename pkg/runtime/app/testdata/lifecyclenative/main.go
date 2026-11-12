//go:build nativewindow

// Command lifecyclenative 是 R28-C/C1/C2 Lifecycle **原生关窗验收** 的编译专用 fixture。
//
// 纪律（务必与受控证据分开记录）：
//   - 本程序位于 pkg/runtime/app/testdata/，Go 的 ./... 默认忽略；并带
//     //go:build nativewindow，显式构建也必须给该标签。本轮**只编译、不执行**。
//   - 不注入事件、不手工触发 WithOnDestroy 回调、不合成 gioapp.DestroyEvent。
//     销毁事件只能来自 Gio 真实窗口事件泵：RunSingleWindowProcess 在
//     ProcessOptions 未公开任何事件/错误/退出注入字段时，生产路径取
//     next = a.w.Event（见 process_entry.go）。公开 ProcessOptions 只有
//     Theme / Lazy / Observer。
//   - op.InvalidateCmd 是 Gio 公开的**重绘请求**，由页面 Layout 提交到
//     gtx.Ops；它请求后续真实 FrameEvent，不是事件注入，也不合成帧。
//   - 自然关闭由授权的人工点击标题栏 X 完成；步骤见同目录 README.md。
//   - 只用公开 app.WithOnDestroy / app.RunSingleWindowProcess / app.ProcessOptions
//     / app.ExitResult / router.Context 与真实 router.Router；不新增公开 API，
//     不调用 NotifyOwnerDestroyed，不复制资产管线。
//   - **唯一的就绪发布点**是 recorder.publishReady()：在同一互斥锁临界区内
//     先成功把 ready-to-close 写入 marker.txt，再置 readyPublished。不存在
//     「flag 已置但 marker 未写」的中间态；写盘失败则不发布。关窗检查
//     checkCloseArrival() 在同一把锁里只读已发布的落盘状态，早到判定不可逆，
//     之后补写 marker 也不能把先到的关闭改成通过。
//   - Finalize 不隐式 Shutdown 的**可观察证据**是真实 Router 根 Context：
//     公开 router.Context() 返回的根在 LifecycleFinalize 前后仍活跃；
//     Router.Shutdown() 会取消该根，两者可由受控对照区分。fixture 自己的
//     shutdown_calls 计数只说明 fixture 调过几次，不作为 Router 内部行为证据。
//
// 本 fixture 输出的字段是**证据字段定义**；编译成功、log_judge PASS 都不是
// 原生验收证据。真实 DestroyEvent、runner rc、进程无残留、native race 仍需授权执行者填写。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/router"
)

// framesNeededForShutdown 是 shutdown-first 需要的真实 FrameEvent 数。
// 第 1 帧由 Gio 自然投递；第 2、3 帧由 fixture 在 Layout 内用公开
// op.InvalidateCmd 请求，不依赖操作员碰巧移动/缩放窗口。第 3 帧完成后
// 才通知后台 goroutine 执行 Shutdown，避免同帧竞态。
const framesNeededForShutdown = 3

// markerEarlyPrefix 由销毁回调在「关窗早到 / 后台观测缺失」时写入；
// judge.go 同包引用它作为 fail-closed 原因，保证写入方与判定方逐字一致。
const markerEarlyPrefix = "close-arrived-early:"

// recorder 记录 UI owner 上真实发生的顺序；mutex 同时保护 marker.txt 写入、
// markers 快照、就绪发布与关窗检查，保证发布点是唯一且原子的。
type recorder struct {
	mu                          sync.Mutex
	markerPath                  string
	scenario                    string
	framesNeeded                int
	steps                       []string
	markers                     []string
	layouts                     int
	frameRequests               int
	mounts                      int
	unmounts                    int
	mountBeforeFirstLayout      bool
	pageCtx                     context.Context
	pageCtxErrAtUnmount         error
	shutdownObserved            bool
	shutdownUnmounts            int
	shutdownCleanupDone         bool
	shutdownCalls               int
	shutdownCallsBeforeFinalize int
	shutdownCallsAtFinalize     int
	// 唯一发布态：只有 marker.txt 中 ready-to-close 成功落盘后才为 true。
	readyPublished      bool
	readyPublishErr     string
	readyRefusedReason  string
	closeArrivedEarly   bool
	closeEarlyReason    string
	routerRootErrBefore string
	routerRootErrAfter  string
	ready               chan struct{}
	readyOnce           sync.Once
}

func (r *recorder) add(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

// setMarkerPath 只在初始化/受控测试里调用；与发布、关窗检查共用同一把锁。
func (r *recorder) setMarkerPath(path string) {
	r.mu.Lock()
	r.markerPath = path
	r.mu.Unlock()
}

// writeMarkerLocked 必须在持锁时调用：**写盘成功**才追加 markers，
// 保证 marker.txt 与 result.json 的 marker_lines 逐字一致。失败不追加。
func (r *recorder) writeMarkerLocked(line string) error {
	if r.markerPath == "" {
		return errors.New("marker path not set")
	}
	f, err := os.OpenFile(r.markerPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	r.markers = append(r.markers, line)
	return nil
}

// writeMarker 保持 (path, line) 签名以兼容协调者 R1 受控探针；
// 返回 error 让「写入失败」成为可观察、可拒绝的真实状态。
func (r *recorder) writeMarker(path, line string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if path != "" {
		r.markerPath = path
	}
	return r.writeMarkerLocked(line)
}

// publishReady 是**唯一的就绪发布点**。写盘与发布态置位在同一个临界区内，
// 不存在「flag 已置但 marker 尚未写入」的可观察交错；写盘失败绝不发布；
// 也绝不清除已记录的 closeArrivedEarly（早到后补写不能翻案）。
func (r *recorder) publishReady() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.writeMarkerLocked(markerReady); err != nil {
		r.readyPublishErr = err.Error()
		r.readyRefusedReason = "ready marker write failed: " + err.Error()
		return err
	}
	r.readyPublished = true
	return nil
}

// setReadyWritten 仅为兼容协调者 R1 受控探针保留：修复后**不再存在**
// 可提前置位的就绪 flag，因此它是显式 no-op。就绪状态只由 publishReady 改变。
// 探针用它模拟「后台观测完成、尚未发布」的交错，关窗检查必须仍判早到。
func (r *recorder) setReadyWritten() {}

func (r *recorder) setReadyRefused(reason string) {
	r.mu.Lock()
	r.readyRefusedReason = reason
	r.mu.Unlock()
}

func (r *recorder) onMount(ctx context.Context) {
	r.mu.Lock()
	r.mounts++
	r.pageCtx = ctx
	r.mu.Unlock()
	r.add("mount:home")
}

func (r *recorder) onUnmount() {
	r.mu.Lock()
	r.unmounts++
	if r.pageCtx != nil {
		r.pageCtxErrAtUnmount = r.pageCtx.Err()
	}
	r.mu.Unlock()
	r.add("unmount:home")
}

// noteLayout 返回当前是第几帧；第 1 帧检查 Mount 是否已早于业务 Layout。
func (r *recorder) noteLayout() int {
	r.mu.Lock()
	r.layouts++
	n := r.layouts
	if n == 1 {
		r.mountBeforeFirstLayout = r.mounts == 1
	}
	r.mu.Unlock()
	r.add("layout:home")
	return n
}

func (r *recorder) noteFrameRequest() {
	r.mu.Lock()
	r.frameRequests++
	r.mu.Unlock()
}

func (r *recorder) signalReady() {
	if r.ready == nil {
		return
	}
	r.readyOnce.Do(func() { close(r.ready) })
}

func (r *recorder) noteShutdownCall() {
	r.mu.Lock()
	r.shutdownCalls++
	r.mu.Unlock()
}

func (r *recorder) noteShutdownObservation(unmounts int, cleanupDone bool) (bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shutdownObserved = true
	r.shutdownUnmounts = unmounts
	r.shutdownCleanupDone = cleanupDone
	return r.shutdownObserved, r.shutdownUnmounts
}

// checkCloseArrival 与 publishReady 共用同一把锁：只读「已成功落盘发布」的
// 状态。后台观测缺失或未发布都记为证据不足；fixture **不阻止**真实关闭，
// 但判定会拒通过。closeArrivedEarly 一旦置位不可逆。
func (r *recorder) checkCloseArrival() (bool, string) {
	if r.scenario != scenarioShutdownFirst {
		return false, ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.shutdownObserved {
		r.closeArrivedEarly = true
		r.closeEarlyReason = "background shutdown observation missing"
		return true, r.closeEarlyReason
	}
	if !r.readyPublished {
		r.closeArrivedEarly = true
		r.closeEarlyReason = "ready-to-close marker not published"
		if r.readyPublishErr != "" {
			r.closeEarlyReason += ": " + r.readyPublishErr
		}
		return true, r.closeEarlyReason
	}
	return false, ""
}

func (r *recorder) noteFinalizeShutdownCounts() {
	r.mu.Lock()
	r.shutdownCallsBeforeFinalize = r.shutdownCalls
	r.mu.Unlock()
}

func (r *recorder) noteFinalizeDoneShutdownCounts() {
	r.mu.Lock()
	r.shutdownCallsAtFinalize = r.shutdownCalls
	r.mu.Unlock()
}

// noteRouterRoot 记录公开 router.Context() 的可观察状态。
func (r *recorder) noteRouterRoot(before bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	text := ""
	if err != nil {
		text = err.Error()
	}
	if before {
		r.routerRootErrBefore = text
	} else {
		r.routerRootErrAfter = text
	}
}

// aux 在锁内读取仅供展示的辅助字段，避免 Observer 与后台 goroutine 竞态。
func (r *recorder) aux() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]any{
		"ready_published":      r.readyPublished,
		"ready_publish_err":    r.readyPublishErr,
		"ready_refused_reason": r.readyRefusedReason,
		"close_early_reason":   r.closeEarlyReason,
		"layouts":              r.layouts,
	}
}

// trace 生成机器判据的唯一输入投影；与 Judge / README §5 逐字段对应。
func (r *recorder) trace(res app.ExitResult) Trace {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctxErr := ""
	if r.pageCtxErrAtUnmount != nil {
		ctxErr = r.pageCtxErrAtUnmount.Error()
	}
	return Trace{
		Scenario:                         r.scenario,
		Markers:                          append([]string(nil), r.markers...),
		Steps:                            append([]string(nil), r.steps...),
		Mounts:                           r.mounts,
		Unmounts:                         r.unmounts,
		MountBeforeFirstLayout:           r.mountBeforeFirstLayout,
		PageContextCanceledBeforeUnmount: errors.Is(r.pageCtxErrAtUnmount, context.Canceled),
		PageContextErrAtUnmount:          ctxErr,
		ObserverAfterCleanup:             res.CleanupDone,
		ProcessCode:                      res.Code,
		ShutdownObserved:                 r.shutdownObserved,
		ShutdownUnmounts:                 r.shutdownUnmounts,
		ShutdownCleanupDone:              r.shutdownCleanupDone,
		ShutdownCalls:                    r.shutdownCalls,
		ShutdownCallsBeforeFinalize:      r.shutdownCallsBeforeFinalize,
		ShutdownCallsAtFinalize:          r.shutdownCallsAtFinalize,
		ReadyPublished:                   r.readyPublished,
		RouterRootErrBeforeFinalize:      r.routerRootErrBefore,
		RouterRootErrAfterFinalize:       r.routerRootErrAfter,
		FrameRequests:                    r.frameRequests,
		FramesNeeded:                     r.framesNeeded,
		CloseArrivedEarly:                r.closeArrivedEarly,
	}
}

type nativePage struct{ rec *recorder }

// Layout 在 shutdown-first 场景主动请求后续真实 FrameEvent（公开 Gio 重绘
// 命令，不是事件注入）；达到所需帧数后停止请求并通知后台 Shutdown。
func (p *nativePage) Layout(gtx layout.Context, _ *material.Theme) layout.Dimensions {
	n := p.rec.noteLayout()
	if p.rec.scenario == scenarioShutdownFirst {
		if n < p.rec.framesNeeded {
			gtx.Execute(op.InvalidateCmd{})
			p.rec.noteFrameRequest()
		} else {
			p.rec.signalReady()
		}
	}
	return layout.Dimensions{}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: lifecyclenative <new-evidence-dir> <natural|shutdown-first>")
		os.Exit(64)
	}
	dir, scenario := os.Args[1], os.Args[2]
	if scenario != scenarioNatural && scenario != scenarioShutdownFirst {
		fmt.Fprintln(os.Stderr, "unknown scenario", scenario)
		os.Exit(64)
	}
	// 每次运行独占新目录；旧 PASS 不可复用。
	if err := os.Mkdir(dir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}
	markerPath := filepath.Join(dir, "marker.txt")

	rec := &recorder{scenario: scenario}
	rec.setMarkerPath(markerPath)
	if scenario == scenarioShutdownFirst {
		rec.framesNeeded = framesNeededForShutdown
		rec.ready = make(chan struct{})
	}
	mustMarker := func(line string) {
		if err := rec.writeMarker(markerPath, line); err != nil {
			panic(err)
		}
	}

	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		fmt.Fprintln(os.Stderr, "EnableLifecycle:", err)
		os.Exit(70)
	}
	owner, err := r.LifecycleOwner()
	if err != nil {
		fmt.Fprintln(os.Stderr, "LifecycleOwner:", err)
		os.Exit(70)
	}
	page := &nativePage{rec: rec}
	if err := r.Register("/home", func(ctx context.Context) (router.View, error) {
		rec.add("construct:home")
		return &router.LifecyclePage{
			View:  page,
			Mount: rec.onMount,
			Unmount: func() {
				rec.onUnmount()
				mustMarker(markerUnmount)
			},
		}, nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "Register:", err)
		os.Exit(70)
	}
	if err := r.Navigate("/home"); err != nil {
		fmt.Fprintln(os.Stderr, "Navigate:", err)
		os.Exit(70)
	}

	if scenario == scenarioShutdownFirst {
		go func() {
			<-rec.ready  // 由 fixture 请求的真实 FrameEvent 达到所需帧数后
			r.Shutdown() // 后台：只 CancelOnly，绝不执行 UI OnUnmount
			rec.noteShutdownCall()
			cleanupDone := owner.CleanupDone()
			rec.mu.Lock()
			unmounts := rec.unmounts
			rec.mu.Unlock()
			observed, atShutdown := rec.noteShutdownObservation(unmounts, cleanupDone)
			mustMarker(fmt.Sprintf("background-shutdown:unmounts=%d cleanup=%t",
				atShutdown, cleanupDone))
			// 三条件不满足：不发布，写 ready-refused。
			ok, reason := readyToClose(ReadyState{
				ShutdownCompleted: observed,
				Unmounts:          atShutdown,
				CleanupDone:       cleanupDone,
			})
			if !ok {
				rec.setReadyRefused(reason)
				mustMarker(markerReadyRefusedPrefix + reason)
				return
			}
			// 唯一发布点：写盘成功与发布态置位在同一临界区；失败不发布。
			if err := rec.publishReady(); err != nil {
				mustMarker(markerReadyRefusedPrefix + "ready marker write failed: " + err.Error())
			}
		}()
	}

	// 唯一的销毁回调：由生产事件循环在真实 DestroyEvent 上调用，绝不手工触发。
	// 真实顺序：callback → unmount:home（Finalize 内部同步触发）→
	// finalize（Finalize 返回后 = 完成）→ observer（cleanup 之后）。
	view, err := app.WithOnDestroy(r.View(), func() {
		if early, reason := rec.checkCloseArrival(); early {
			mustMarker(markerEarlyPrefix + reason)
		}
		mustMarker(markerCallback)
		rec.add("callback")
		rec.noteFinalizeShutdownCounts()
		// 真实 Router 根 Context 观察（公开 API）：若 Finalize 隐式触发
		// Shutdown，这里会后变为 context canceled，是可区分的可见副作用。
		rec.noteRouterRoot(true, r.Context().Err())
		r.LifecycleFinalize() // UI owner：一次最终化；销毁回调不调用 Shutdown
		rec.noteRouterRoot(false, r.Context().Err())
		rec.noteFinalizeDoneShutdownCounts()
		rec.add("finalize")
		mustMarker(markerFinalize)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "WithOnDestroy:", err)
		os.Exit(70)
	}

	title := fmt.Sprintf("R28-C2 lifecycle native [%s] pid=%d", scenario, os.Getpid())
	opts := app.ProcessOptions{Observer: func(res app.ExitResult) {
		mustMarker(fmt.Sprintf("observer:callback=%t cleanup=%t code=%d",
			res.CallbackDone, res.CleanupDone, res.Code))
		tr := rec.trace(res)
		verdict := Judge(tr)
		verdictText := "PASS"
		if !verdict.Pass {
			verdictText = "FAIL"
		}
		var obs map[string]any
		raw, err := json.Marshal(tr)
		if err != nil {
			panic(err)
		}
		if err := json.Unmarshal(raw, &obs); err != nil {
			panic(err)
		}
		obs["log_judge"] = map[string]any{
			"verdict": verdictText,
			"reasons": verdict.Reasons,
			"scope":   "日志/状态判据，不是原生 DestroyEvent 验收结论",
		}
		obs["pid"] = os.Getpid()
		obs["title"] = title
		for k, v := range rec.aux() {
			obs[k] = v
		}
		// 真实关闭路径：生产事件泵，无注入；正常人工关窗 Err=nil。
		obs["event_source"] = "production app.Window.Event (no injection; ProcessOptions exposes no event/failure/exit injection)"
		obs["frame_event_source"] = "fixture Layout submits gioui.org/op.InvalidateCmd (public redraw request, not injected events)"
		obs["real_close_path"] = map[string]any{
			"destroy_reached":      res.CallbackDone,
			"destroy_err":          errText(res.DestroyErr),
			"window_destroy_error": errors.Is(res.Err, app.ErrWindowDestroy),
			"callback_done":        res.CallbackDone,
			"cleanup_done":         res.CleanupDone,
		}
		obs["process"] = map[string]any{
			"code":          res.Code,
			"err":           errText(res.Err),
			"destroy_err":   errText(res.DestroyErr),
			"font_err":      errText(res.FontErr),
			"callback_done": res.CallbackDone,
			"cleanup_done":  res.CleanupDone,
		}
		// 证据口径：shutdown_calls 只说明 fixture 显式调用次数；
		// Router 内部行为以 router.Context() 的可观察状态为准。
		obs["evidence_scope"] = map[string]any{
			"shutdown_calls": "fixture 侧显式调用计数，只证明 fixture 调过几次，" +
				"不作为 Router 内部行为的证据",
			"router_root_context": "公开 router.Context() 在 LifecycleFinalize 前后的真实状态；" +
				"Router.Shutdown() 会取消该根，受控对照可区分",
			"close_arrived_early": "一旦记录不可逆，随后补写 ready marker 不翻案",
		}
		obs["native_destroy_event_verdict"] = "未验证：需授权人工标题栏关闭；编译与 log_judge 均不等于原生验收"
		// 以下字段只能由授权执行者填写，编译/静态分析/受控回归无法产生。
		obs["runner_fields_required"] = []string{
			"runner_observed_rc",
			"no_residual_process",
			"natural_titlebar_close_steps_performed",
			"plain_binary_sha256",
			"race_binary_sha256",
			"native_race_result",
			"cross_platform_run_result",
		}
		data, err := json.MarshalIndent(obs, "", "  ")
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "result.json"), data, 0600); err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stderr, "R28-C2 lifecycle observer code=%d callback=%t cleanup=%t unmounts=%d log_judge=%s reasons=%v\n",
			res.Code, res.CallbackDone, res.CleanupDone, tr.Unmounts, verdictText, verdict.Reasons)
	}}

	fmt.Fprintf(os.Stderr, "R28-C2 lifecycle start pid=%d scenario=%s title=%q (manual title-bar X to close)\n",
		os.Getpid(), scenario, title)
	app.RunSingleWindowProcess(view, app.Config{Title: title, Width: 960, Height: 540}, opts)
	fmt.Fprintln(os.Stderr, "unreachable: process entry returned")
	os.Exit(70)
}
