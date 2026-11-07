//go:build windows

package watch

// Windows 平台实现：Job Object 进程树所有权 + 有界 tree 命令。仅此文件 import x/sys/windows，
// 使公共包在非 Windows 下可编译（F-A8）。

import (
	"context"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobHandle 是 Windows Job 对象句柄。
type jobHandle = windows.Handle

// newJobOwnership 创建带 KILL_ON_JOB_CLOSE 的 Job。
func newJobOwnership() (jobHandle, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return 0, err
	}
	return h, nil
}

// assignJobOwnership 把 PID 纳入 Job（错误必须上报，不忽略）。
func assignJobOwnership(job jobHandle, pid int) error {
	if job == 0 {
		return nil
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(job, ph)
}

// closeJobOwnership 关闭句柄（KILL_ON_JOB_CLOSE 触发终止整棵树）。
func closeJobOwnership(job jobHandle) {
	if job != 0 {
		windows.CloseHandle(job)
	}
}

// jobAccounting 对应 JOBOBJECT_BASIC_ACCOUNTING_INFORMATION（ActiveProcesses 偏移 40）。
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// terminateAndWaitJob 显式终止 Job 内全部进程并**有界等待 ActiveProcesses 归零**后关闭句柄。
// 任何失败（Terminate/Query 错误、超时未归零）都如实返回 error，不伪装成功。
func terminateAndWaitJob(job jobHandle, deadline time.Time) error {
	if job == 0 {
		return nil
	}
	if err := windows.TerminateJobObject(job, 1); err != nil {
		// Terminate 失败仍尝试等待/关闭，但错误必须上报。
		windows.CloseHandle(job)
		return fmt.Errorf("TerminateJobObject: %w", err)
	}
	var lastErr error
	for time.Now().Before(deadline) {
		var info jobAccounting
		err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		if err == nil && info.ActiveProcesses == 0 {
			windows.CloseHandle(job)
			return nil
		}
		if err != nil {
			lastErr = err
		}
		time.Sleep(5 * time.Millisecond)
	}
	windows.CloseHandle(job)
	if lastErr != nil {
		return fmt.Errorf("QueryInformationJobObject: %w", lastErr)
	}
	return fmt.Errorf("job 未在 StopTimeout 内清空（ActiveProcesses 未归零）")
}

// killProcessTree 在**有界 ctx**内执行 taskkill /T /F（受 StopTimeout 约束，不无限卡）。
func killProcessTree(ctx context.Context, pid int) error {
	cmd := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", pid))
	return cmd.Run()
}
