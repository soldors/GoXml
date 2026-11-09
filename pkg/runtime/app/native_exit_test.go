//go:build nativewindow

// 本文件是 R27-C 的**原生验收测试**，默认 go test 不选中。
// 只有协调者显式 `go test -tags nativewindow -run TestNativeSingleWindowExit`
// 才会执行；作者阶段只用 `go test -tags nativewindow -run '^$'` 验证可编译，
// 不打开任何原生窗口。
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const nativeTimeout = 180 * time.Second

type nativeEvidence struct {
	Scenario     string `json:"scenario"`
	Code         int    `json:"code"`
	Err          string `json:"err"`
	DestroyErr   string `json:"destroy_err"`
	FontErr      string `json:"font_err"`
	CallbackDone bool   `json:"callback_done"`
	CleanupDone  bool   `json:"cleanup_done"`
	PID          int    `json:"pid"`
	FontSource   string `json:"font_source"`
	Frames       int    `json:"frames"`
	AutoClose    bool   `json:"auto_close_requested"`
	FontNotFound bool   `json:"font_not_found"`
}

// nativeRepoRoot 从包目录 pkg/runtime/app 上溯三级到仓库根。
func nativeRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// nativeTaskkill 用绝对路径解析 taskkill，避免 PATH 被清空或劫持
// （R26-C R1 F-C3 的既有教训）。
func nativeTaskkill() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "taskkill.exe")
}

// reapNativeTree 只针对本测试自己启动的 PID 做进程树收口，不扫全局进程。
func reapNativeTree(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		return
	}
	cmd := exec.Command(nativeTaskkill(), "/PID", fmt.Sprint(pid), "/T", "/F")
	out, err := cmd.CombinedOutput()
	t.Logf("原生超时收口 pid=%d err=%v out=%s", pid, err, strings.TrimSpace(string(out)))
}

func buildNativeExit(t *testing.T) string {
	t.Helper()
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("需要 PATH 上的 Go:", err)
	}
	exe := filepath.Join(t.TempDir(), "r27c-nativeexit.exe")
	cmd := exec.Command(goExe, "build", "-o", exe, "./pkg/runtime/app/testdata/nativeexit")
	cmd.Dir = nativeRepoRoot(t)
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local",
		"GOFLAGS=-mod=readonly")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("构建原生验收程序失败: %v\n%s", err, out)
	}
	return exe
}

// TestNativeSingleWindowExit defaults to a self-closing native lifecycle test.
// Manual scenarios still require title-bar close; no desktop input is automated.
func TestNativeSingleWindowExit(t *testing.T) {
	scenario := os.Getenv("R27C_NATIVE_SCENARIO")
	if scenario == "" {
		scenario = "normal-auto"
	}
	wantRC := 1
	switch scenario {
	case "normal", "normal-auto", "grid":
		wantRC = 0
	case "font-strict", "font-strict-auto", "invalid-options":
		wantRC = 1
	default:
		t.Fatalf("未知原生场景 %q", scenario)
	}

	exe := buildNativeExit(t)
	dir := os.Getenv("R27C_NATIVE_EVIDENCE_DIR")
	if dir == "" {
		dir = filepath.Join(t.TempDir(), "owned-native-run") // child creates and owns this fresh directory
	}
	t.Logf("证据目录 %s；二进制 %s；scenario %s", dir, exe, scenario)

	ctx, cancel := context.WithTimeout(context.Background(), nativeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, dir, scenario)
	stdout, stderr := new(strings.Builder), new(strings.Builder)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	_ = os.WriteFile(filepath.Join(dir, "stdout.raw"), []byte(stdout.String()), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "stderr.raw"), []byte(stderr.String()), 0o600)

	if ctx.Err() == context.DeadlineExceeded {
		reapNativeTree(t, cmd.Process.Pid)
		t.Fatalf("原生窗口在 %s 内未关闭；已定向终止自有进程树 pid=%d", nativeTimeout, cmd.Process.Pid)
	}
	rc := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("原生程序未启动或非退出错误: %v\nstdout=%s\nstderr=%s",
				runErr, stdout.String(), stderr.String())
		}
		rc = exitErr.ExitCode()
	}
	if rc != wantRC {
		t.Fatalf("真实退出码 rc=%d want=%d\nstderr=%s", rc, wantRC, stderr.String())
	}

	resultBytes, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		t.Fatalf("result.json 缺失: %v\nstderr=%s", err, stderr.String())
	}
	var res nativeEvidence
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("result.json 不可解析: %v\n%s", err, resultBytes)
	}
	if res.Code != wantRC || res.Scenario != scenario {
		t.Fatalf("结果与真实 rc 不一致: %+v", res)
	}
	if scenario != "invalid-options" && (!res.CallbackDone || !res.CleanupDone) {
		t.Fatalf("callback/cleanup did not complete before observer: %+v", res)
	}
	if (scenario == "normal-auto" || scenario == "font-strict-auto") && (!res.AutoClose || res.Frames < 2) {
		t.Fatalf("automatic native close did not follow real frames: %+v", res)
	}

	markerBytes, err := os.ReadFile(filepath.Join(dir, "marker.txt"))
	if err != nil {
		t.Fatalf("marker.txt 缺失: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(markerBytes)), "\n")

	switch scenario {
	case "normal", "normal-auto", "grid":
		if res.Err != "" || res.DestroyErr != "" {
			t.Fatalf("normal close contains error: %+v", res)
		}
		if len(lines) != 2 || lines[0] != "callback" || lines[1] != "observer:callback=true cleanup=true" {
			t.Fatalf("callback/cleanup must precede observer: %q", markerBytes)
		}
	case "font-strict", "font-strict-auto":
		if res.FontErr == "" || res.Err == "" || !res.FontNotFound {
			t.Fatalf("strict Provider failure lost: %+v", res)
		}
	case "invalid-options":
		if !strings.Contains(res.Err, "mutually exclusive") || res.CallbackDone || res.CleanupDone {
			t.Fatalf("invalid options entered window: %+v", res)
		}
		if len(lines) != 1 || lines[0] != "observer:callback=false cleanup=false" {
			t.Fatalf("unexpected validation marker: %q", markerBytes)
		}
	}

	for _, banned := range []string{"fatal error", "all goroutines are asleep", "unreachable"} {
		if strings.Contains(stderr.String(), banned) {
			t.Fatalf("stderr 含不应有的诊断 %q: %s", banned, stderr.String())
		}
	}
	t.Logf("OBS native %s: rc=%d callback=%t cleanup=%t err=%q source=%s",
		scenario, rc, res.CallbackDone, res.CleanupDone, res.Err, res.FontSource)
}
