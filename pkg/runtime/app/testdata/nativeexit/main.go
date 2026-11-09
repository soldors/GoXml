// r27c-nativeexit 是 R27-C 自有的**单窗原生验收程序**（协调者专用）。
//
// 它消费显式进程级入口 app.RunSingleWindowProcess 的生产路径：
//   - Next == nil  => 真实 Gio 窗口事件泵 a.w.Event（会创建平台窗口）；
//   - mainFunc()   => 真实 gioapp.Main()，Windows 上永不返回；
//   - Exit  == nil => 事件循环结束后由入口显式 os.Exit(Code)。
//
// 作者阶段只构建、不执行；带标签的原生测试在协调者阶段才选中。
// 这不是通用 Run 已正常返回的证据，也不能冒充 Gio 真实 premature close。
//
// 用法：
//
//	r27c-nativeexit <evidence-dir> <normal|font-strict|theme-failure>
//
// evidence-dir 下产出：
//
//	marker.txt      callback 行（WithOnDestroy 回调）+ cleanup:<steps> 行
//	result.json     ExitResult 的可观察副本（含真实 Code / Err / Steps / PID）
//
// 预期（详见交付 native-acceptance.md）：
//
//	normal        人工点标题栏 X 正常关窗 -> rc 0，stderr 无 fatal/死锁
//	font-strict   注入严格字体失败源 + 人工关窗 -> rc 1，Err 含 ErrFontNotFound
//	theme-failure 开窗前默认主题失败，不建窗 -> rc 1，Steps 无 destroy/callback
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	goxmlapp "github.com/goxml/goxml/pkg/runtime/app"
)

type nativeView struct{}

func (nativeView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	inset := layout.Inset{Top: unit.Dp(28), Left: unit.Dp(28), Right: unit.Dp(28)}
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.Body1(th,
			"R27-C APP-EXIT-CONTRACT | 单窗原生验收：请点击标题栏 X 关闭窗口").Layout(gtx)
	})
}

type nativeResult struct {
	Scenario   string   `json:"scenario"`
	Code       int      `json:"code"`
	Err        string   `json:"err"`
	DestroyErr string   `json:"destroy_err"`
	FontErr    string   `json:"font_err"`
	Steps      []string `json:"steps"`
	PID        int      `json:"pid"`
	Injected   string   `json:"injected"`
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func appendMarker(path, line string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marker 打开失败:", err)
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: r27c-nativeexit <evidence-dir> <normal|font-strict|theme-failure>")
		os.Exit(64)
	}
	dir, scenario := os.Args[1], os.Args[2]
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "evidence-dir 创建失败:", err)
		os.Exit(64)
	}
	marker := filepath.Join(dir, "marker.txt")
	resultPath := filepath.Join(dir, "result.json")

	view, err := goxmlapp.WithOnDestroy(nativeView{}, func() {
		appendMarker(marker, "callback")
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "WithOnDestroy 失败:", err)
		os.Exit(64)
	}

	injected := "none"
	opts := goxmlapp.ProcessOptions{
		// Theme / ThemeErr / Next / Exit 全部零值 = 真实生产路径：
		// 真实默认主题构建（NewThemeWithReport）+ 真实 Gio 窗口事件泵 + 真实 os.Exit。
		Observer: func(res goxmlapp.ExitResult) {
			appendMarker(marker, "cleanup:"+strings.Join(res.Steps, ","))
			data, mErr := json.Marshal(nativeResult{
				Scenario:   scenario,
				Code:       res.Code,
				Err:        errText(res.Err),
				DestroyErr: errText(res.DestroyErr),
				FontErr:    errText(res.FontErr),
				Steps:      res.Steps,
				PID:        os.Getpid(),
				Injected:   injected,
			})
			if mErr != nil {
				fmt.Fprintln(os.Stderr, "result 序列化失败:", mErr)
				return
			}
			if wErr := os.WriteFile(resultPath, data, 0o600); wErr != nil {
				fmt.Fprintln(os.Stderr, "result.json 写入失败:", wErr)
			}
			fmt.Fprintf(os.Stderr, "R27C NATIVE result: scenario=%s code=%d steps=%v err=%q\n",
				scenario, res.Code, res.Steps, errText(res.Err))
		},
	}

	switch scenario {
	case "normal":
		// 真实默认主题 + 真实窗口；人工关闭标题栏 X。
	case "font-strict":
		// 显式注入严格字体失败源（受控，不是真实字体探测失败）。
		// 窗口仍是真实 Gio 窗口，仍由人工关闭，退出码应为 1。
		injected = "FontFailure(contracts.ErrFontNotFound)"
		opts.FontFailure = func() error {
			return fmt.Errorf("r27c native injected strict font failure: %w", contracts.ErrFontNotFound)
		}
	case "theme-failure":
		// 开窗前默认主题失败：不建窗、不启动事件循环，直接 rc 1。
		injected = "ThemeErr(contracts.ErrFontNotFound)"
		opts.ThemeErr = fmt.Errorf("r27c native injected theme failure: %w", contracts.ErrFontNotFound)
	default:
		fmt.Fprintf(os.Stderr, "unknown scenario %q\n", scenario)
		os.Exit(64)
	}

	fmt.Fprintf(os.Stderr,
		"R27C NATIVE start: argv=%v scenario=%s injected=%s pid=%d evidence=%s\n",
		os.Args, scenario, injected, os.Getpid(), dir)
	fmt.Fprintln(os.Stderr, "R27C NATIVE hint: 关闭标题栏 X 后观察退出码与 result.json；不要强杀。")

	goxmlapp.RunSingleWindowProcess(view, goxmlapp.Config{
		Title:  "R27-C nativeexit [" + scenario + "]",
		Width:  unit.Dp(960),
		Height: unit.Dp(320),
	}, opts)

	// 不可达：真实 Windows Main 不返回，进程由事件循环 goroutine 显式退出。
	fmt.Fprintln(os.Stderr, "unreachable: nativeexit returned without process exit")
	os.Exit(70)
}
