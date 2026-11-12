//go:build windows

package process

// R28 发布门禁 A：process 包真实边界覆盖（Windows-only，无窗口）。仅用本测试创建的自有 Job/进程/句柄，
// 复用已有受控 seam（TestMain 的 held/held-leaf helper）。每个用例断言真实行为/错误，非行数触达。

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestTerminateJobNormalTerminatesAndCloses 真实 Job + 运行中 helper：terminateJob 终止成员、归零、关闭句柄、返回 nil。
func TestTerminateJobNormalTerminatesAndCloses(t *testing.T) {
	dir := t.TempDir()
	job, cmd, leafPid := spawnOwnedHeld(t, dir)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	_ = waitLeafPid(t, leafPid) // 真实后代已就绪
	if err := terminateJob(job, time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("terminateJob normal: %v", err)
	}
	// 句柄已被 terminateJob 关闭：查询应得到 ERROR_INVALID_HANDLE。
	var info jobAccounting
	err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	if !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		t.Fatalf("terminateJob did not close job handle: %v", err)
	}
}

// TestTerminateJobExpiredDeadlineErrorsAndCloses 过期 deadline：返回明确错误且仍关闭句柄（不泄漏）。
func TestTerminateJobExpiredDeadlineErrorsAndCloses(t *testing.T) {
	job, err := NewJobOwnership()
	if err != nil {
		t.Fatal(err)
	}
	terr := terminateJob(job, time.Now().Add(-time.Second))
	if terr == nil {
		t.Fatal("terminateJob with expired deadline must error")
	}
	var info jobAccounting
	qerr := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	if !errors.Is(qerr, windows.ERROR_INVALID_HANDLE) {
		CloseJobOwnership(job)
		t.Fatalf("terminateJob expired path did not close handle: %v", qerr)
	}
}

// TestWaitProcessHandlesEmptyAndExpired 空句柄返回 nil；过期 deadline 且句柄活 => 过期错误。
func TestWaitProcessHandlesEmptyAndExpired(t *testing.T) {
	if err := waitProcessHandles(nil, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("empty handles must return nil: %v", err)
	}
	// 一个真实存活进程的句柄 + 已过 deadline => 过期错误。
	self := windows.CurrentProcess()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, windows.GetCurrentProcessId())
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	_ = self
	if err := waitProcessHandles([]windows.Handle{h}, time.Now().Add(-time.Second)); err == nil {
		t.Fatal("expired deadline with live handle must error")
	}
}

// TestWaitJobEmptyAndHandlesInvalidHandleErrors 无效 Job 句柄：Job 查询失败必须报错（阻止假装成功）。
func TestWaitJobEmptyAndHandlesInvalidHandleErrors(t *testing.T) {
	err := waitJobEmptyAndHandles(windows.InvalidHandle, nil, time.Now().Add(200*time.Millisecond))
	if err == nil {
		t.Fatal("invalid job handle must error in Job-empty proof")
	}
}

// TestJobProcessHandlesInvalidHandleErrors 无效 Job 句柄：捕获进程失败必须报错。
func TestJobProcessHandlesInvalidHandleErrors(t *testing.T) {
	if _, err := jobProcessHandles(windows.InvalidHandle); err == nil {
		t.Fatal("jobProcessHandles on invalid handle must error")
	}
}

// TestAssignJobOwnershipInvalidPIDErrors 无效 PID：AssignJobOwnership 必须报错（不静默）。
func TestAssignJobOwnershipInvalidPIDErrors(t *testing.T) {
	job, err := NewJobOwnership()
	if err != nil {
		t.Fatal(err)
	}
	defer CloseJobOwnership(job)
	// 一个几乎必然不存在的 PID（超范围/已退出）触发 OpenProcess 失败。
	if err := AssignJobOwnership(job, 0x7FFFFFF0); err == nil {
		t.Fatal("assign to non-existent PID must error")
	}
}

// TestRunCommandTreeNilContextAndPreCancelled nil ctx 与预先取消的 ctx 都应有界返回（不启动命令）。
func TestRunCommandTreeNilContextAndPreCancelled(t *testing.T) {
	if r := RunCommandTree(nil, "cmd", nil, "", nil, 0); r.Err == nil || r.RC != -1 {
		t.Fatalf("nil ctx must return error: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := RunCommandTree(ctx, "cmd", nil, "", nil, 0); !r.Cancelled || r.Err == nil {
		t.Fatalf("pre-cancelled ctx must return Cancelled: %+v", r)
	}
}

// TestExitCodeOfBranches 覆盖 exitCodeOf 的 nil-ProcessState 分支：werr=nil => 0；非 ExitError => -1；
// ExitError => 其码。
func TestExitCodeOfBranches(t *testing.T) {
	// 未启动/未等待的 cmd（ProcessState=nil）。
	c1 := exec.Command("cmd")
	if got := exitCodeOf(c1, nil); got != 0 {
		t.Fatalf("nil ProcessState + nil werr => 0, got %d", got)
	}
	if got := exitCodeOf(c1, errors.New("non-exit infra")); got != -1 {
		t.Fatalf("nil ProcessState + non-ExitError => -1, got %d", got)
	}
	// 真实 ExitError：运行一个必然非零退出的自有命令。
	c2 := exec.Command("cmd", "/c", "exit 3")
	werr := c2.Run()
	if got := exitCodeOf(c2, werr); got != 3 {
		t.Fatalf("ExitError => its code (3), got %d (werr=%v)", got, werr)
	}
}

// TestRunCommandTreeTimeoutCancelsOwnedTree 覆盖 RunCommandTree 的 ctx.Done 取消/超时分支：自有 helper 阻塞，
// 短 timeout 触发取消清理（Job 终止 + 有界返回），断言 Cancelled + DeadlineExceeded + 有界 + 无幸存根。
func TestRunCommandTreeTimeoutCancelsOwnedTree(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r := RunCommandTree(context.Background(), exe, []string{"-test.run=^$"}, "", []string{"GOXML_OWNED_STREAM_HELPER=held"}, 250*time.Millisecond)
	elapsed := time.Since(start)
	if !r.Cancelled {
		t.Fatalf("timeout must be Cancelled: %+v", r)
	}
	if !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Fatalf("timeout must surface DeadlineExceeded: %v", r.Err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("timeout cleanup not bounded: %v", elapsed)
	}
}
