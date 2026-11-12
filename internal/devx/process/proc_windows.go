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
	// 一旦接管非零 Job 句柄，**任何**返回路径（含过期/错误）都必须释放该句柄（defer 保证）。
	// KILL_ON_JOB_CLOSE 关闭即兜底终止整棵树。
	defer CloseJobOwnership(job)

	// fail-closed：调用时截止已过 => 立即报告“退出证明过期”，但句柄仍由 defer 释放。
	if !time.Now().Before(deadline) {
		return errors.New("process exit proof deadline expired")
	}
	// 捕获退出见证（仍在 Job 内的自有进程句柄）。
	handles, captureErr := jobProcessHandles(job)
	defer func() {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
	}()
	// 权威终止：先用已持有的 Job 句柄 TerminateJobObject 同步 kill 全部 Job 成员（含后代）。
	var termErr error
	if terr := windows.TerminateJobObject(job, 1); terr != nil {
		termErr = fmt.Errorf("TerminateJobObject: %w", terr)
	}
	// 外部 tree 命令：与退出证明**并行**发起（各自有界于同一 StopTimeout），并在**返回前收口**自己启动的命令
	// （不留到返回后再按原 PID 动作）。并行保证慢命令/不存在目标不挤掉证明预算（证明不被 taskkill 串行阻塞），
	// 同时 taskkill 获得从发起到取消的完整 STOP 窗口以便真正启动（entered）。命令失败可观测，不掩盖证明结果。
	var killErr error
	var tkDone chan error
	if pid > 0 {
		// 为返回后的外层 c.done 收口保留预算：tree 命令使用**较早的子截止**（deadline - doneReserve），
		// 使其在被取消后能于外层 StopTimeout 之前被收口；并行发起保证启动窗口；证明仍用完整 deadline。
		doneReserve := time.Until(deadline) / 4
		if doneReserve > 300*time.Millisecond {
			doneReserve = 300 * time.Millisecond
		}
		tkDeadline := deadline.Add(-doneReserve)
		if !time.Now().Before(tkDeadline) {
			tkDeadline = time.Now().Add(time.Until(deadline) / 2)
		}
		tkDone = make(chan error, 1)
		ctx, cancel := context.WithDeadline(context.Background(), tkDeadline)
		go func() { err := KillProcessTree(ctx, pid); cancel(); tkDone <- err }()
	}
	// 退出证明（权威）：同一 StopTimeout 内 (a) Job ActiveProcesses==0 且 (b) 已捕获句柄全部 signaled。
	proofErr := waitJobEmptyAndHandles(job, handles, deadline)
	// 返回前收口自有 tree 命令（其子截止已早于 deadline，故此处等待有界且留出 c.done 余量）。
	if tkDone != nil {
		select {
		case killErr = <-tkDone:
		case <-time.After(time.Until(deadline)):
			killErr = errors.New("tree command not reaped within deadline")
		}
	}
	// 若 Job 句柄已**证明整棵树退出**（捕获/终止/双重退出证明均成功），tree 命令仅补充：其慢/无目标/
	// 被取消的错误**不**使 Shutdown 失败（不得误报）；但它已在返回前同步发起并收口（不留到返回后动作）。
	// 反之若退出证明失败，tree 命令错误必须保留（fail-closed，不吞错）。
	if captureErr == nil && termErr == nil && proofErr == nil {
		killErr = nil
	}
	return errors.Join(captureErr, termErr, proofErr, killErr)
}

// runTaskkillBounded 在返回前同步运行自有 tree 命令，并收口该命令本身；只占用总余量的有界子预算
// （<= 剩余一半），为 Job 终止与退出证明保留预算。命令被 ctx 取消后仍等待其退出（收口自有命令）。
func runTaskkillBounded(pid int, deadline time.Time) error {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return errors.New("no budget for tree command")
	}
	budget := remaining / 2
	if budget <= 0 {
		budget = remaining
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(budget))
	defer cancel()
	return KillProcessTree(ctx, pid)
}

// waitJobEmptyAndHandles 双重退出证明：Job ActiveProcesses==0 且已捕获句柄全部 signaled，均有界于 deadline。
// Job 查询失败或进程等待未达 WAIT_OBJECT_0 => 返回错误（阻止交接）。
func waitJobEmptyAndHandles(job JobHandle, handles []windows.Handle, deadline time.Time) error {
	// (b) 先等已捕获自有句柄：必须明确 WAIT_OBJECT_0。
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
	// (a) Job 记账归零：处理“句柄快照后新起的后代”或快照遗漏的残余；轮询 ActiveProcesses 至 0。
	var lastErr error
	for time.Now().Before(deadline) {
		var info jobAccounting
		err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		if err != nil {
			return fmt.Errorf("QueryInformationJobObject: %w", err)
		}
		if info.ActiveProcesses == 0 {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("job 未在 StopTimeout 内清空（ActiveProcesses 未归零）")
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
