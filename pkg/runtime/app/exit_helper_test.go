package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	gioapp "gioui.org/app"
	"gioui.org/widget/material"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// R27-C C2：真实 console 子进程的退出码与清理证据
//
// helper 是自有、有界、无窗口的子进程。它真实调用显式进程级入口并
// os.Exit；父进程只 observed rc / marker / result.json，测试进程本身
// 不被终止。不执行任何用户业务 main/init、不打开原生窗口。
// ============================================================================

type helperOutcome struct {
	Code       int      `json:"code"`
	Err        string   `json:"err"`
	DestroyErr string   `json:"destroy_err"`
	FontErr    string   `json:"font_err"`
	Steps      []string `json:"steps"`
	Marker     string   `json:"marker"`
}

func buildExitHelper(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func runExitHelper(t *testing.T, exe, scenario string) (int, string, string, helperOutcome) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "cleanup.marker")
	resultPath := filepath.Join(dir, "result.json")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestExitHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "R27_EXIT_CHILD=1", "R27_EXIT_MARKER="+marker, "R27_EXIT_RESULT="+resultPath, "R27_EXIT_SCENARIO="+scenario)
	out, runErr := cmd.CombinedOutput()

	rc := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("helper 未启动或非退出错误: %v\n%s", runErr, out)
		}
		rc = exitErr.ExitCode()
	}
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("helper 超时未收口: %s", out)
	}
	markerBytes, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("cleanup marker 缺失: %v\n%s", err, out)
	}
	resultBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("result.json 缺失: %v\n%s", err, out)
	}
	var res helperOutcome
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("result.json 不可解析: %v\n%s", err, resultBytes)
	}
	return rc, string(markerBytes), string(out), res
}

// TestExitHelperControlledProcessExitCodes 四个预登记场景各跑一次：
// 正常关窗 rc0；窗口提前销毁 / 开窗前主题失败 / 严格字体失败 rc1。
// 每例都断言 cleanup marker、结果与真实退出码一致，且测试进程存活。
func TestExitHelperControlledProcessExitCodes(t *testing.T) {
	exe := buildExitHelper(t)

	cases := []struct {
		scenario string
		wantRC   int
		wantErr  string
		wantStep string
	}{
		{"normal", 0, "", "destroy"},
		{"destroy", 1, "helper native premature close", "destroy"},
		{"theme", 1, "helper theme failure", "theme"},
		{"strict", 1, "helper strict font failure", "font"},
	}
	for _, tc := range cases {
		t.Run(tc.scenario, func(t *testing.T) {
			rc, marker, out, res := runExitHelper(t, exe, tc.scenario)
			if rc != tc.wantRC {
				t.Fatalf("真实退出码 rc=%d want=%d；stdout/stderr=%s", rc, tc.wantRC, out)
			}
			if res.Code != tc.wantRC {
				t.Fatalf("结果 Code=%d 与真实 rc=%d 不一致: %+v", res.Code, rc, res)
			}
			if tc.wantErr == "" {
				if res.Err != "" || res.DestroyErr != "" {
					t.Fatalf("正常关窗不应有错误: %+v", res)
				}
			} else if !strings.Contains(res.Err, tc.wantErr) && !strings.Contains(res.DestroyErr, tc.wantErr) && !strings.Contains(res.FontErr, tc.wantErr) {
				t.Fatalf("结果缺少预期错误片段 %q: %+v", tc.wantErr, res)
			}
			if res.Marker != "cleanup-before-exit" {
				t.Fatalf("cleanup marker 未由 observer 写出: %+v", res)
			}
			// 回调先于清理：事件循环场景 marker 第一行必须是 callback；
			// 开窗前失败（theme）没有事件循环，只应有 cleanup 行。
			lines := strings.Split(strings.TrimSpace(marker), "\n")
			if tc.scenario == "theme" {
				if len(lines) != 1 || !strings.HasPrefix(lines[0], "cleanup:") {
					t.Fatalf("开窗前失败应只写 cleanup marker: %q", marker)
				}
			} else if len(lines) < 2 || lines[0] != "callback" || !strings.HasPrefix(lines[1], "cleanup:") {
				t.Fatalf("marker 顺序不是 callback→cleanup: %q", marker)
			}
			// Steps 里 destroy/callback/cleanup/font/result/observer/exit 的相对顺序。
			joined := strings.Join(res.Steps, ",")
			if !strings.Contains(joined, tc.wantStep) {
				t.Fatalf("Steps 缺少阶段 %q: %v", tc.wantStep, res.Steps)
			}
			if !strings.HasSuffix(joined, "observer,exit") {
				t.Fatalf("observer 必须先于进程退出: %v", res.Steps)
			}
			if !strings.Contains(joined, "cleanup,font,result") && tc.scenario != "theme" {
				t.Fatalf("清理未先于结果生成: %v", res.Steps)
			}
			// 主题失败在开窗前：不应出现事件循环步骤。
			if tc.scenario == "theme" && (strings.Contains(joined, "destroy") || strings.Contains(joined, "callback")) {
				t.Fatalf("开窗前失败不应启动事件循环: %v", res.Steps)
			}
			// 测试进程仍在运行（没有误杀调用者）。
			if err := os.WriteFile(filepath.Join(t.TempDir(), "parent-alive"), []byte("alive"), 0o600); err != nil {
				t.Fatalf("测试进程已被终止: %v", err)
			}
			t.Logf("OBS helper %s: rc=%d steps=%v err=%q", tc.scenario, rc, res.Steps, res.Err)
		})
	}
}

// TestExitHelperNoWindowAndNoStrayStderr 断言 helper 没有打开原生窗口
// （受控事件源，不创建 app.Window 事件泵）、stderr 无 fatal/死锁诊断。
func TestExitHelperNoWindowAndNoStrayStderr(t *testing.T) {
	exe := buildExitHelper(t)
	rc, _, out, _ := runExitHelper(t, exe, "normal")
	if rc != 0 {
		t.Fatalf("normal rc=%d", rc)
	}
	for _, banned := range []string{"fatal error", "all goroutines are asleep", "unreachable"} {
		if strings.Contains(out, banned) {
			t.Fatalf("helper 输出含不应有的诊断 %q: %s", banned, out)
		}
	}
}

// TestExitHelperProcess is selected only in a self-owned child process. Its
// package-local seams retain the real public entry and real os.Exit boundary.
func TestExitHelperProcess(t *testing.T) {
	if os.Getenv("R27_EXIT_CHILD") != "1" {
		return
	}
	marker, resultPath, scenario := os.Getenv("R27_EXIT_MARKER"), os.Getenv("R27_EXIT_RESULT"), os.Getenv("R27_EXIT_SCENARIO")
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
	errText := func(err error) string {
		if err == nil {
			return ""
		}
		return err.Error()
	}
	view, err := WithOnDestroy(&countingView{text: "helper"}, func() { appendMarker("callback") })
	if err != nil {
		t.Fatal(err)
	}
	destroy := gioapp.DestroyEvent{}
	if scenario == "destroy" {
		destroy.Err = errors.New("helper native premature close")
	}
	next, _ := exitControlled(synthFrameEvent(&frameRecorder{}), destroy)
	opts := ProcessOptions{Theme: material.NewTheme(), next: next,
		Observer: func(res ExitResult) {
			appendMarker("cleanup:" + strings.Join(res.steps, ","))
			b, err := json.Marshal(helperOutcome{Code: res.Code, Err: errText(res.Err), DestroyErr: errText(res.DestroyErr), FontErr: errText(res.FontErr), Steps: res.steps, Marker: "cleanup-before-exit"})
			if err != nil {
				panic(err)
			}
			if err := os.WriteFile(resultPath, b, 0600); err != nil {
				panic(err)
			}
		},
	}
	switch scenario {
	case "normal", "destroy":
	case "theme":
		opts.themeErr = errors.New("helper theme failure: font family not found")
		opts.next = nil
	case "strict":
		opts.fontFailure = func() error { return fmt.Errorf("helper strict font failure: goxml: font family not found") }
	default:
		t.Fatalf("unknown child scenario %q", scenario)
	}
	// Main intentionally blocks; only the public entry's real os.Exit can end
	// this child. No Window.Event or platform Main is called.
	mainFunc = func() { select {} }
	RunSingleWindowProcess(view, Config{Title: "r27-exit-child"}, opts)
	t.Fatal("unreachable: process entry returned without exiting child")
}
