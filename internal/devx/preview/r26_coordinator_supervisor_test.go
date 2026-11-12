package preview

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCoordHostKillFailureIsBounded 验证「终止命令不可用」前提下 Wait 的
// 有界回收，并保留正常工具/健康根与后代正对照。
//
// 前提修正（R28 全树阻断 2-B 判别，preview-shutdown-delivery-hy4
// probe-b）：原夹具只清 PATH，但生产 taskkillExe 从
// SystemRoot/WINDIR/SystemDrive 解析绝对路径，taskkill 实际仍可用——
// 旧前提 8/8 次 Wait(20ms) 全程 1111–1354ms 且错误链无 taskkill，
// 量的是 taskkill 成功路径耗时（必超 1s 守卫），「终止工具不可用」
// 从未被制造。改用 disableTaskkill（全部解析路径真实失效）：实测
// 274–276ms、0/8 超 1s、错误链保留 taskkill。
func TestCoordHostKillFailureIsBounded(t *testing.T) {
	root, e := resolveToolRoot("")
	if e != nil {
		t.Fatal(e)
	}
	helper := filepath.Join(t.TempDir(), "owned-console.exe")
	cmd := exec.Command("go", "build", "-o", helper, "./internal/devx/preview/testdata/consolehelper")
	cmd.Dir = root
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("helper build: %v %s", e, out)
	}

	t.Run("终止命令不可用时有限回收且错误可见", func(t *testing.T) {
		runner, e := StartHost(helper, "", "sleep", "30")
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { runner.cmd.Process.Kill() })
		disableTaskkill(t) // 真实制造「终止命令不可用」，不再只清 PATH。
		type outcome struct {
			exitErr error
			err     error
		}
		done := make(chan outcome, 1)
		go func() {
			exitErr, werr := runner.Wait(20 * time.Millisecond)
			done <- outcome{exitErr: exitErr, err: werr}
		}()
		select {
		case got := <-done:
			// 有界回收（1s 守卫保留：disable-all 实测 ~275ms，余量充足）。
			// 真实错误仍可见：超时类别保留 + 终止失败点名 taskkill。
			if !errors.Is(got.err, ErrHostTimeout) {
				t.Fatalf("应保留超时类别: %v", got.err)
			}
			if got.err == nil || !strings.Contains(got.err.Error(), "taskkill") {
				t.Fatalf("终止失败必须可见且点名终止命令: %v", got.err)
			}
			if got.exitErr != nil {
				t.Fatalf("超时路径首返回值应保持 nil: %v", got.exitErr)
			}
			return
		case <-time.After(time.Second):
			runner.cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("coordinator cleanup failed")
			}
			t.Fatal("Wait timeout hung after Stop observed taskkill failure; required direct owned-PID cleanup to return")
		}
	})

	t.Run("正常工具健康根与后代正对照", func(t *testing.T) {
		// 正对照不用 1s 守卫：正常路径含真实 taskkill 执行（本机实测
		// 685ms–2.4s 依负载），按有界语义给预算。
		marker := filepath.Join(t.TempDir(), "healthy-marker.txt")
		out := &lockedBuffer{}
		r, e := StartHostWithOutput(helper, "", out, nil, "tree", "30", "--child-out", marker)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { r.cmd.Process.Kill() })
		// 就绪门闩：READY 在子进程 spawn 之后打印，读到才开始计时。
		readyDeadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(out.String(), "READY") {
			if time.Now().After(readyDeadline) {
				t.Fatalf("宿主未在就绪窗口内打印 READY: %q", out.String())
			}
			time.Sleep(50 * time.Millisecond)
		}
		exitErr, werr := r.Wait(2 * time.Second)
		if !errors.Is(werr, ErrHostTimeout) || exitErr != nil {
			t.Fatalf("健康树 30 应超时: exitErr=%v waitErr=%v", exitErr, werr)
		}
		if serr := r.StopError(); serr != nil {
			t.Fatalf("正常工具下树终止应成功: %v", serr)
		}
		if !r.waitBriefly(5 * time.Second) {
			t.Fatal("正常工具下宿主应有界回收")
		}
		// 子进程 10 秒写 marker：等待窗口必须覆盖其自然写入窗口
		// （10s）+ 余量，与进程树收口判据同源（见 consolehelper 注释）。
		time.Sleep(11500 * time.Millisecond)
		if _, serr := os.Stat(marker); !os.IsNotExist(serr) {
			t.Fatalf("健康树应收口（子进程不应存活到写 marker）: stat=%v", serr)
		}
	})
}
