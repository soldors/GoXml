//go:build nativewindow

// Command lifecyclenative 是 R28-C Lifecycle **原生关窗验收** 的编译专用 fixture。
//
// 纪律（务必与受控证据分开记录）：
//   - 本程序位于 pkg/runtime/app/testdata/，Go 的 ./... 默认忽略；并带
//     //go:build nativewindow，显式构建也必须给该标签。本轮**只编译、不执行**。
//   - 不注入事件、不手工触发 WithOnDestroy 回调、不合成 gioapp.DestroyEvent。
//     销毁事件只能来自 Gio 真实窗口事件泵：RunSingleWindowProcess 在
//     ProcessOptions 未公开任何事件/错误/退出注入字段时，生产路径取
//     next = a.w.Event（见 process_entry.go）。公开 ProcessOptions 只有
//     Theme / Lazy / Observer。
//   - 自然关闭由授权的人工点击标题栏 X 完成；步骤见同目录 README.md。
//   - 只用公开 app.WithOnDestroy / app.RunSingleWindowProcess / app.ProcessOptions
//     / app.ExitResult 与真实 router.Router；不新增公开 API，不调用
//     NotifyOwnerDestroyed，不复制资产管线。
//
// 本 fixture 输出的字段是**证据字段定义**；编译成功本身不是原生验收证据。
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
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/router"
)

// recorder 记录 UI owner 上真实发生的顺序；mutex 只用于后台 Shutdown
// goroutine 与 UI owner 之间的快照同步，不改变任何产品行为。
type recorder struct {
	mu                     sync.Mutex
	steps                  []string
	layouts                int
	mounts                 int
	unmounts               int
	mountBeforeFirstLayout bool
	pageCtx                context.Context
	pageCtxErrAtUnmount    error
	shutdownObserved       bool
	shutdownUnmounts       int
	shutdownCleanupDone    bool
	ready                  chan struct{}
	readyOnce              sync.Once
}

func (r *recorder) add(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
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

func (r *recorder) layout() {
	r.mu.Lock()
	r.layouts++
	first, second := r.layouts == 1, r.layouts == 2
	if first {
		r.mountBeforeFirstLayout = r.mounts == 1
	}
	r.mu.Unlock()
	r.add("layout:home")
	if second && r.ready != nil {
		r.readyOnce.Do(func() { close(r.ready) })
	}
}

func (r *recorder) snapshot() (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	steps := append([]string(nil), r.steps...)
	ctxErr := ""
	if r.pageCtxErrAtUnmount != nil {
		ctxErr = r.pageCtxErrAtUnmount.Error()
	}
	return map[string]any{
		"steps":                                steps,
		"layouts":                              r.layouts,
		"mounts":                               r.mounts,
		"unmounts":                             r.unmounts,
		"mount_before_first_layout":            r.mountBeforeFirstLayout,
		"page_context_err_at_unmount":          ctxErr,
		"page_context_canceled_before_unmount": errors.Is(r.pageCtxErrAtUnmount, context.Canceled),
		"shutdown_observed":                    r.shutdownObserved,
		"shutdown_unmounts":                    r.shutdownUnmounts,
		"shutdown_cleanup_done":                r.shutdownCleanupDone,
	}, nil
}

type nativePage struct{ rec *recorder }

func (p *nativePage) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.rec.layout()
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
	if scenario != "natural" && scenario != "shutdown-first" {
		fmt.Fprintln(os.Stderr, "unknown scenario", scenario)
		os.Exit(64)
	}
	// 每次运行独占新目录；旧 PASS 不可复用。
	if err := os.Mkdir(dir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}
	marker := filepath.Join(dir, "marker.txt")
	appendMarker := func(line string) {
		f, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		_, err = f.WriteString(line + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			panic(err)
		}
	}

	rec := &recorder{}
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
			View:    page,
			Mount:   rec.onMount,
			Unmount: rec.onUnmount,
		}, nil
	}); err != nil {
		fmt.Fprintln(os.Stderr, "Register:", err)
		os.Exit(70)
	}
	if err := r.Navigate("/home"); err != nil {
		fmt.Fprintln(os.Stderr, "Navigate:", err)
		os.Exit(70)
	}

	if scenario == "shutdown-first" {
		rec.ready = make(chan struct{})
		go func() {
			<-rec.ready  // 两次真实业务 Layout 之后
			r.Shutdown() // 后台：只 CancelOnly，绝不执行 UI OnUnmount
			cleanupDone := owner.CleanupDone()
			rec.mu.Lock()
			rec.shutdownObserved = true
			rec.shutdownUnmounts = rec.unmounts
			rec.shutdownCleanupDone = cleanupDone
			unmountsAtShutdown := rec.shutdownUnmounts
			rec.mu.Unlock()
			appendMarker(fmt.Sprintf("background-shutdown:unmounts=%d cleanup=%t",
				unmountsAtShutdown, cleanupDone))
		}()
	}

	// 唯一的销毁回调：由生产事件循环在真实 DestroyEvent 上调用，绝不手工触发。
	view, err := app.WithOnDestroy(r.View(), func() {
		appendMarker("callback")
		rec.add("callback")
		r.LifecycleFinalize() // UI owner：一次最终化
		rec.add("finalize")
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "WithOnDestroy:", err)
		os.Exit(70)
	}

	title := fmt.Sprintf("R28-C lifecycle native [%s] pid=%d", scenario, os.Getpid())
	opts := app.ProcessOptions{Observer: func(res app.ExitResult) {
		appendMarker(fmt.Sprintf("observer:callback=%t cleanup=%t code=%d",
			res.CallbackDone, res.CleanupDone, res.Code))
		obs, _ := rec.snapshot()
		obs["scenario"] = scenario
		obs["pid"] = os.Getpid()
		obs["title"] = title
		// 真实关闭路径：生产事件泵，无注入；正常人工关窗 Err=nil。
		obs["event_source"] = "production app.Window.Event (no injection; ProcessOptions exposes no event/failure/exit injection)"
		obs["real_close_path"] = map[string]any{
			"destroy_reached":      res.CallbackDone,
			"destroy_err":          errText(res.DestroyErr),
			"window_destroy_error": errors.Is(res.Err, app.ErrWindowDestroy),
			"callback_done":        res.CallbackDone,
			"cleanup_done":         res.CleanupDone,
		}
		// cleanup 之后才是 observer：ExitResult 在事件循环 cleanup 之后生成，
		// finishProcess 在 observer/exit 两步写入之后才调用 Observer。
		obs["observer_after_cleanup"] = res.CleanupDone
		obs["process"] = map[string]any{
			"code":          res.Code,
			"err":           errText(res.Err),
			"destroy_err":   errText(res.DestroyErr),
			"font_err":      errText(res.FontErr),
			"callback_done": res.CallbackDone,
			"cleanup_done":  res.CleanupDone,
		}
		// 以下字段只能由授权执行者填写，编译/静态分析无法产生。
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
		fmt.Fprintf(os.Stderr, "R28-C lifecycle observer code=%d callback=%t cleanup=%t unmounts=%v\n",
			res.Code, res.CallbackDone, res.CleanupDone, obs["unmounts"])
	}}

	fmt.Fprintf(os.Stderr, "R28-C lifecycle start pid=%d scenario=%s title=%q (manual title-bar X to close)\n",
		os.Getpid(), scenario, title)
	app.RunSingleWindowProcess(view, app.Config{Title: title, Width: 960, Height: 540}, opts)
	fmt.Fprintln(os.Stderr, "unreachable: process entry returned")
	os.Exit(70)
}
