//go:build nativewindow

// Command windowhostnative 是 R29-C S3 的**双窗自有原生验收 fixture**。
//
// 只编译、默认不执行；执行需实机、依赖与协调者授权的人工桌面操作。
// 程序本身是显式的进程所有者：最后一窗收口后 Wait() 返回，写
// result.json 并 os.Exit(code)。两个窗口各有自己的 Router / Owner，
// 每个窗口由操作员亲手点击标题栏 X 关闭（不注入事件、不 ActionClose
// 代点、不全局扫杀）。
//
// 用法：windowhostnative.exe <new-evidence-dir> <natural|cancel-first>
//
//	natural      两窗就绪 → 人工关 A → 确认 B 仍显示/响应 → 人工关 B
//	cancel-first 两窗就绪后程序对各窗 Router 后台 Shutdown（只取消），
//	             记录 background-cancel 行；随后人工关 A、关 B，卸载由
//	             各窗 UI owner 在 DestroyEvent 回调里完成
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/lifecycle"
	"github.com/goxml/goxml/pkg/runtime/router"
)

const (
	scenarioNatural     = "natural"
	scenarioCancelFirst = "cancel-first"
	framesBeforeCancel  = 3
)

// recorder 是唯一的 marker 写入点（写盘与内存快照同锁，保证 marker.txt
// 与 result.json 的 marker_lines 逐字一致；禁止事后补写）。
type recorder struct {
	mu      sync.Mutex
	path    string
	markers []string
	once    map[string]bool
}

func (r *recorder) write(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
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
	r.markers = append(r.markers, line)
}

func (r *recorder) writeOnce(line string) {
	r.mu.Lock()
	if r.once[line] {
		r.mu.Unlock()
		return
	}
	r.once[line] = true
	r.mu.Unlock()
	r.write(line)
}

func (r *recorder) has(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range r.markers {
		if len(line) >= len(prefix) && line[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.markers...)
}

// windowState 是每个窗口**自己**的 Router / Owner 与观测值。
// 所有字段由 mu 保护：Layout 在窗口 loop goroutine 写，后台取消
// goroutine 与宿主收口 goroutine 可能并发读（native race 门禁）。
type windowState struct {
	mu          sync.Mutex
	label       string
	r           *router.Router
	owner       *lifecycle.Owner
	page        *nativePage
	mounts      int
	unmounts    int
	pageCtx     context.Context
	ctxCanceled bool
	rootBefore  string
	rootAfter   string
	frames      int
}

func (s *windowState) noteFrame() {
	s.mu.Lock()
	s.frames++
	s.mu.Unlock()
}

func (s *windowState) getFrames() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames
}

// cancelObservation 给后台取消行提供一致快照。
func (s *windowState) cancelObservation() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unmounts, s.owner.CleanupDone()
}

type nativePage struct {
	rec   *recorder
	state *windowState
}

func (p *nativePage) Layout(gtx layout.Context, _ *material.Theme) layout.Dimensions {
	p.state.noteFrame()
	p.rec.writeOnce("frame:" + p.state.label)
	// 关 A 之后 B 的后续帧由操作员目视确认；公开重绘请求不是事件注入。
	if p.state.label == "B" && p.rec.has("close:A") {
		p.rec.writeOnce("frame:B-after-A")
	}
	gtx.Execute(op.InvalidateCmd{})
	return layout.Dimensions{}
}

func newWindowState(rec *recorder, label string) (*windowState, app.View, error) {
	s := &windowState{label: label, r: router.New()}
	if err := s.r.EnableLifecycle(); err != nil {
		return nil, nil, err
	}
	owner, err := s.r.LifecycleOwner()
	if err != nil {
		return nil, nil, err
	}
	s.owner = owner
	s.page = &nativePage{rec: rec, state: s}
	if err := s.r.Register("/home", func(context.Context) (router.View, error) {
		return &router.LifecyclePage{
			View: s.page,
			Mount: func(ctx context.Context) {
				s.mu.Lock()
				s.mounts++
				s.pageCtx = ctx
				s.mu.Unlock()
			},
			Unmount: func() {
				s.mu.Lock()
				s.unmounts++
				s.ctxCanceled = errors.Is(s.pageCtx.Err(), context.Canceled)
				count, canceled := s.unmounts, s.ctxCanceled
				s.mu.Unlock()
				rec.write(fmt.Sprintf("unmount:%s canceled=%t count=%d", label, canceled, count))
			},
		}, nil
	}); err != nil {
		return nil, nil, err
	}
	if err := s.r.Navigate("/home"); err != nil {
		return nil, nil, err
	}
	// ADR-0018：真实 DestroyEvent 由该窗 UI owner 完成 Finalize（恰一次）。
	view, err := app.WithOnDestroy(s.r.View(), func() {
		rec.write("close:" + label)
		s.mu.Lock()
		s.rootBefore = errText(s.r.Context().Err())
		s.mu.Unlock()
		s.r.LifecycleFinalize()
		s.mu.Lock()
		s.rootAfter = errText(s.r.Context().Err())
		s.mu.Unlock()
		rec.write("finalize:" + label)
	})
	if err != nil {
		return nil, nil, err
	}
	return s, view, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: windowhostnative <new-evidence-dir> <natural|cancel-first>")
		os.Exit(64)
	}
	dir, scenario := os.Args[1], os.Args[2]
	if scenario != scenarioNatural && scenario != scenarioCancelFirst {
		fmt.Fprintln(os.Stderr, "unknown scenario", scenario)
		os.Exit(64)
	}
	if err := os.Mkdir(dir, 0o700); err != nil { // 独占新目录，旧证据不可复用
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}
	rec := &recorder{path: filepath.Join(dir, "marker.txt"), once: make(map[string]bool)}
	rec.write("scenario:" + scenario)

	host := app.NewWindowHost()
	th := material.NewTheme()
	states := map[string]*windowState{}
	for _, label := range []string{"A", "B"} {
		s, view, err := newWindowState(rec, label)
		if err != nil {
			fmt.Fprintln(os.Stderr, label, err)
			os.Exit(70)
		}
		states[label] = s
		title := fmt.Sprintf("R29-C windowhost [%s] pid=%d", label, os.Getpid())
		if _, err := host.Open(label, view, th, app.Config{Title: title, Width: 640, Height: 400}); err != nil {
			fmt.Fprintln(os.Stderr, "open", label, err)
			os.Exit(70)
		}
		rec.write("open:" + label)
	}

	if scenario == scenarioCancelFirst {
		go func() {
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				if states["A"].getFrames() >= framesBeforeCancel && states["B"].getFrames() >= framesBeforeCancel {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			var wg sync.WaitGroup
			for _, label := range []string{"A", "B"} {
				wg.Add(1)
				go func(l string) {
					defer wg.Done()
					s := states[l]
					s.r.Shutdown() // 只取消，不卸载
					unmounts, cleanup := s.cancelObservation()
					rec.write(fmt.Sprintf("background-cancel:%s unmounts=%d cleanup=%t root=%s",
						l, unmounts, cleanup, errText(s.r.Context().Err())))
				}(label)
			}
			wg.Wait()
		}()
	}

	// 宿主进程所有者：等所有窗口各自清理完成后显式退出。
	go func() {
		res := host.Wait()
		code := 0
		if len(res.Errors) > 0 {
			code = 1
		}
		rec.write("exit:code=" + strconv.Itoa(code))
		trace := traceFrom(rec.snapshot(), states, scenario, code)
		out := map[string]any{
			"scenario":     scenario,
			"marker_lines": rec.snapshot(),
			"pid":          os.Getpid(),
			"host_result": map[string]any{
				"code": res.Code, "explicit": res.Explicit,
				"drained": res.Drained, "closed": res.Closed, "errors": fmt.Sprint(res.Errors),
			},
			"windows":   windowReports(states),
			"log_judge": LogJudge(trace),
			"judge_fields_required": []string{
				"runner_observed_rc", "no_residual_process", "data_race",
				"manual_b_serving_after_a_close", "binary_sha256", "log_sha256",
				"operator", "native_race_result",
			},
			"native_destroy_event_verdict": "未验证：编译/日志判据不等于原生验收，需人工标题栏 X 与 runner 证据",
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "result.json"), data, 0o600); err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stderr, "R29-C windowhost exit code=%d scenario=%s\n", code, scenario)
		os.Exit(code)
	}()

	// 主线程：一次性 Gio Main（生产永不返回）。
	host.Run()
	fmt.Fprintln(os.Stderr, "unreachable: host Run returned")
	os.Exit(70)
}

func windowReports(states map[string]*windowState) map[string]any {
	out := map[string]any{}
	for label, s := range states {
		s.mu.Lock()
		rep := map[string]any{
			"mounts": s.mounts, "unmounts": s.unmounts,
			"page_context_canceled_before_unmount": s.ctxCanceled,
			"router_root_before":                   s.rootBefore, "router_root_after": s.rootAfter,
			"cleanup_done": s.owner.CleanupDone(), "frames": s.frames,
		}
		s.mu.Unlock()
		out[label] = rep
	}
	return out
}

// traceFrom 只从已落盘 marker 与受控观测构造机器判据输入；
// runner/人工字段不在此处伪造（RunnerRc=-1 表示执行者尚未观察）。
func traceFrom(markers []string, states map[string]*windowState, scenario string, code int) Trace {
	t := Trace{
		Scenario: scenario, Markers: markers, ExitCode: code,
		Unmounts: map[string]int{}, PageContextCanceled: map[string]bool{},
		CleanupDone: map[string]bool{}, RouterRootAfter: map[string]string{},
		RunnerRc: -1,
	}
	for label, s := range states {
		s.mu.Lock()
		t.Unmounts[label] = s.unmounts
		t.PageContextCanceled[label] = s.ctxCanceled
		t.CleanupDone[label] = s.owner.CleanupDone()
		t.RouterRootAfter[label] = s.rootAfter
		s.mu.Unlock()
	}
	return t
}
