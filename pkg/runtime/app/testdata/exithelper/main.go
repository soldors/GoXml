// exithelper 是 R27-C 自有的有界 console 子进程（不打开任何原生窗口）。
//
// 它消费显式进程级入口 app.RunSingleWindowProcess：主线程执行 Gio Main
// （Windows 上 select{} 阻塞），事件循环 goroutine 在受控 DestroyEvent 后
// 完成回调/清理、写出 marker 与结果 JSON，然后由入口显式 os.Exit(code)。
// 测试进程只启动它，不断言自己被终止；真实退出码由父进程 observed rc 取得。
//
// 用法：exithelper <marker-file> <result.json> <normal|destroy|theme|strict>
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"strings"
	"time"

	gioapp "gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	app "github.com/goxml/goxml/pkg/runtime/app"
)

type helperView struct{}

func (helperView) Layout(layout.Context, *material.Theme) layout.Dimensions {
	return layout.Dimensions{}
}

type helperResult struct {
	Code       int      `json:"code"`
	Err        string   `json:"err"`
	DestroyErr string   `json:"destroy_err"`
	FontErr    string   `json:"font_err"`
	Steps      []string `json:"steps"`
	Marker     string   `json:"marker"`
}

func appendMarker(path, line string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = f.WriteString(line + "\n")
		_ = f.Close()
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: exithelper <marker> <result.json> <scenario>")
		os.Exit(64)
	}
	marker, resultPath, scenario := os.Args[1], os.Args[2], os.Args[3]

	view, err := app.WithOnDestroy(helperView{}, func() { appendMarker(marker, "callback") })
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}

	opts := app.ProcessOptions{
		Theme: material.NewTheme(), // 注入受控主题，避免真实字体探测
		Next:  controlledEvents(scenario),
		Observer: func(res app.ExitResult) {
			appendMarker(marker, "cleanup:"+strings.Join(res.Steps, ","))
			data, _ := json.Marshal(helperResult{
				Code:       res.Code,
				Err:        errText(res.Err),
				DestroyErr: errText(res.DestroyErr),
				FontErr:    errText(res.FontErr),
				Steps:      res.Steps,
				Marker:     "cleanup-before-exit",
			})
			_ = os.WriteFile(resultPath, data, 0o600)
		},
		// Exit 故意不注入：使用真实 os.Exit(res.Code)，父进程观察真实 rc。
	}
	switch scenario {
	case "normal":
		// 正常关窗：无销毁原因、无字体失败。
	case "destroy":
		// 窗口带 Gio DestroyEvent.Err 提前销毁。
	case "theme":
		opts.ThemeErr = fmt.Errorf("helper theme failure: %w", errors.New("font family not found"))
		opts.Next = nil // 不应被调用：开窗前就失败
	case "strict":
		opts.FontFailure = func() error {
			return fmt.Errorf("helper strict font failure: %w", errors.New("goxml: font family not found"))
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown scenario %q\n", scenario)
		os.Exit(64)
	}

	app.RunSingleWindowProcess(view, app.Config{Title: "r27c-exithelper"}, opts)
	// 不可达：生产形态 Main 不返回，进程由事件循环 goroutine 显式退出。
	fmt.Fprintln(os.Stderr, "unreachable: helper returned without process exit")
	os.Exit(70)
}

func controlledEvents(scenario string) func() event.Event {
	ch := make(chan event.Event, 2)
	ch <- gioapp.FrameEvent{
		Now:    time.Now(),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Size:   image.Pt(200, 100),
		Frame:  func(*op.Ops) {},
	}
	if scenario == "destroy" {
		ch <- gioapp.DestroyEvent{Err: errors.New("helper native premature close")}
	} else {
		ch <- gioapp.DestroyEvent{}
	}
	return func() event.Event { return <-ch }
}
