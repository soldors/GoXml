//go:build windows

package process

// Windows 平台实现：Job Object 进程树所有权 + 有界 tree 命令。仅此文件 import x/sys/windows，
// 使公共包在非 Windows 下可编译（F-A8）。

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// JobHandle 是 Windows Job 对象句柄。
type JobHandle = windows.Handle

func prepareSuspended(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
}

func resumeOwnedCommand(cmd *exec.Cmd) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(cmd.Process.Pid) {
			continue
		}
		thread, e := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if e != nil {
			return e
		}
		previous, e := windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if e != nil {
			return e
		}
		if previous > 0 {
			return nil
		}
	}
	return fmt.Errorf("no suspended startup thread found for owned PID %d: %w", cmd.Process.Pid, err)
}

// NewJobOwnership 创建带 KILL_ON_JOB_CLOSE 的 Job。
func NewJobOwnership() (JobHandle, error) {
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

// AssignJobOwnership 把 PID 纳入 Job（错误必须上报，不忽略）。
func AssignJobOwnership(job JobHandle, pid int) error {
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

// CloseJobOwnership 关闭句柄（KILL_ON_JOB_CLOSE 触发终止整棵树）。
func CloseJobOwnership(job JobHandle) {
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

// TerminateAndWaitJob retains process handles before requesting termination.
// Job accounting can reach zero before those handles are signaled.
func TerminateAndWaitJob(job JobHandle, deadline time.Time) error {
	return stopJob(job, 0, deadline)
}

// StopJobTree captures exit witnesses before taskkill can remove job members.
func StopJobTree(job JobHandle, pid int, deadline time.Time) error {
	return stopJob(job, pid, deadline)
}

func stopJob(job JobHandle, pid int, deadline time.Time) error {
	if job == 0 {
		return nil
	}
	handles, captureErr := jobProcessHandles(job)
	defer func() {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
	}()
	if pid > 0 {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		// Job termination remains the fallback if taskkill fails or times out.
		_ = KillProcessTree(ctx, pid)
		cancel()
	}
	return errors.Join(captureErr, terminateJob(job, deadline), waitProcessHandles(handles, deadline))
}

func jobProcessHandles(job JobHandle) ([]windows.Handle, error) {
	// A bounded snapshot; oversized trees fail closed instead of silently losing
	// exit witnesses. Only PIDs returned by this exact owned job are opened.
	var list struct {
		assigned, listed uint32
		ids              [4096]uintptr
	}
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return nil, fmt.Errorf("capture job processes: %w", err)
	}
	if list.listed > uint32(len(list.ids)) || list.assigned > list.listed {
		return nil, errors.New("job process snapshot incomplete")
	}
	var handles []windows.Handle
	for _, pid := range list.ids[:list.listed] {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			continue // The process exited and its PID disappeared before OpenProcess.
		}
		if err != nil {
			return handles, fmt.Errorf("capture job process %d: %w", pid, err)
		}
		handles = append(handles, h)
	}
	return handles, nil
}

func waitProcessHandles(handles []windows.Handle, deadline time.Time) error {
	for _, h := range handles {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("process exit proof deadline expired")
		}
		state, err := windows.WaitForSingleObject(h, uint32((remaining+time.Millisecond-1)/time.Millisecond))
		if err != nil || state != windows.WAIT_OBJECT_0 {
			return fmt.Errorf("process exit not proven: state=%d err=%v", state, err)
		}
	}
	return nil
}

func terminateJob(job JobHandle, deadline time.Time) error {
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

// KillProcessTree 在**有界 ctx**内执行 taskkill /T /F（受 StopTimeout 约束，不无限卡）。
func KillProcessTree(ctx context.Context, pid int) error {
	cmd := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", pid))
	return cmd.Run()
}
