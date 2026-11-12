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
	// fail-closed：若调用时截止已过，立即报告“退出证明过期”，不得报告成功（保留 expired-deadline 契约）。
	if !time.Now().Before(deadline) {
		return errors.New("process exit proof deadline expired")
	}
	// 先捕获退出见证（仍在 Job 内的进程句柄），确保即便成员随后退出也有可等待句柄。
	handles, captureErr := jobProcessHandles(job)
	defer func() {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
	}()
	// 关键顺序（最小修复）：**先用已持有的 Windows Job 句柄终止整棵自有树**（TerminateJobObject
	// 同步 kill 全部 Job 成员，含后代），不被外部 taskkill 的等待期挤占单一 StopTimeout。
	var termErr error
	if terr := windows.TerminateJobObject(job, 1); terr != nil {
		termErr = fmt.Errorf("TerminateJobObject: %w", terr)
	}
	// 退出证明：等待已捕获的自有句柄在截止前全部 signal（在 taskkill 之前做，避免被其耗时饿死）。
	waitErr := waitProcessHandles(handles, deadline)
	// taskkill 仅作**有界补充**（gate 契约要求 Manager 仍**调用** tree 命令）。此时树已被 Job 句柄终止；
	// 为避免外部 taskkill 的等待期挤占单一 StopTimeout、拖延有界返回，**异步**发起（bounded 于同一 deadline），
	// 不在收口路径同步等待。Job 终止与退出证明才是权威收口依据。
	var killErr error
	if pid > 0 {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		done := make(chan error, 1)
		go func() { done <- KillProcessTree(ctx, pid); cancel() }()
		// 仅在 Job 终止或退出证明**失败**时，才在有界余量内等待并保留 taskkill 错误（fail-closed，不吞错）。
		if captureErr != nil || termErr != nil || waitErr != nil {
			select {
			case killErr = <-done:
			case <-time.After(time.Until(deadline)):
				killErr = errors.New("taskkill not completed within deadline")
			}
		}
	}
	// 关闭 Job 句柄（KILL_ON_JOB_CLOSE 兜底）。
	CloseJobOwnership(job)
	return errors.Join(captureErr, termErr, waitErr, killErr)
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
