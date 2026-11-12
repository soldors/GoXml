//go:build windows

package process

// R28 Manager 停止路径集中返修（R1）：专有回归（Windows-only，无窗口，自有根/后代）。
//   - 过期仍清理：TerminateAndWaitJob 在过期提前返回时仍关闭 Job 句柄（原句柄变无效），且返回明确错误；
//     用一个真实自有后代证明“错误路径释放后后代退出”（KILL_ON_JOB_CLOSE 关闭即终止）。
//   - 退出证明完整：正常路径要求 ActiveProcesses==0 且已捕获句柄 WAIT_OBJECT_0；Job 查询失败阻止交接。
// 后代退出断言显式要求 `WaitForSingleObject` 无错且 `WAIT_OBJECT_0`。

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// spawnOwnedHeld 启动一个自有 helper（TestMain 的 held 分支），纳入 Job，返回 job 句柄、根 cmd、leaf pid 文件。
func spawnOwnedHeld(t *testing.T, dir string) (JobHandle, *exec.Cmd, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	leafPid := filepath.Join(dir, "leaf.pid")
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), "GOXML_OWNED_STREAM_HELPER=held", "GOXML_LEAF_PID="+leafPid)
	job, err := StartOwnedCommand(cmd)
	if err != nil {
		t.Fatalf("start owned: %v", err)
	}
	return job, cmd, leafPid
}

func waitLeafPid(t *testing.T, leafPid string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(leafPid); e == nil && len(b) > 0 {
			var pid int
			for _, ch := range string(b) {
				if ch < '0' || ch > '9' {
					break
				}
				pid = pid*10 + int(ch-'0')
			}
			if pid > 0 {
				return pid
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("leaf pid not reported")
	return 0
}

// requireExited 显式要求进程句柄无错且 WAIT_OBJECT_0（拒绝 WAIT_TIMEOUT）。
func requireExited(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// 打不开通常意味着进程已消失（退出）。
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return
		}
		t.Fatalf("open leaf %d: %v", pid, err)
	}
	defer windows.CloseHandle(h)
	state, werr := windows.WaitForSingleObject(h, uint32(within.Milliseconds()))
	if werr != nil {
		t.Fatalf("WaitForSingleObject error (must require WAIT_OBJECT_0): %v", werr)
	}
	if state != windows.WAIT_OBJECT_0 {
		t.Fatalf("leaf %d not exited: state=%d (want WAIT_OBJECT_0)", pid, state)
	}
}

// TestExpiredCleanupReleasesJobAndDescendantExits 过期错误路径：句柄仍被关闭、返回明确错误、真实后代退出。
func TestExpiredCleanupReleasesJobAndDescendantExits(t *testing.T) {
	dir := t.TempDir()
	job, cmd, leafPid := spawnOwnedHeld(t, dir)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	leaf := waitLeafPid(t, leafPid)

	// 过期 deadline：TerminateAndWaitJob 必须返回明确错误（不得报告成功）。
	err := TerminateAndWaitJob(job, time.Now().Add(-time.Second))
	if err == nil {
		t.Fatal("expired proof reported success")
	}
	// 句柄已被接管并关闭：用原句柄查询应得到 ERROR_INVALID_HANDLE（不泄漏）。
	var info jobAccounting
	qerr := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	if !errors.Is(qerr, windows.ERROR_INVALID_HANDLE) {
		CloseJobOwnership(job)
		t.Fatalf("expired cleanup did not release the owned Job handle: err=%v", qerr)
	}
	// 错误路径释放（KILL_ON_JOB_CLOSE 关闭）后，真实自有后代必须退出。
	requireExited(t, leaf, 5*time.Second)
}

// TestNormalStopProvesJobEmptyAndHandles 正常停止：ActiveProcesses==0 且已捕获句柄 WAIT_OBJECT_0。
func TestNormalStopProvesJobEmptyAndHandles(t *testing.T) {
	dir := t.TempDir()
	job, cmd, leafPid := spawnOwnedHeld(t, dir)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	leaf := waitLeafPid(t, leafPid)
	if err := StopJobTree(job, cmd.Process.Pid, time.Now().Add(5*time.Second)); err != nil {
		t.Fatalf("normal stop must prove exit: %v", err)
	}
	requireExited(t, leaf, 2*time.Second)
}

// TestJobZeroProofRequiredBeyondHandles 定向守卫：已捕获句柄为空、但 Job 内仍有活跃进程时，
// waitJobEmptyAndHandles 必须靠 ActiveProcesses==0 证明失败（不能被空句柄快照当成功）。
// 移除此 Job 归零证明（MUT-no-jobzero）会使其错误通过 => 该守卫具判别力。
func TestJobZeroProofRequiredBeyondHandles(t *testing.T) {
	dir := t.TempDir()
	job, cmd, leafPid := spawnOwnedHeld(t, dir)
	_ = job
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	_ = waitLeafPid(t, leafPid)
	// 直接以空句柄 + 未终止的 Job 调用：Job 内 root+leaf 活跃，ActiveProcesses != 0。
	// 用独立 Job 句柄? 这里复用 job 但**不** TerminateJobObject，模拟“只等句柄”的缺口。
	err := waitJobEmptyAndHandles(job, nil, time.Now().Add(300*time.Millisecond))
	if err == nil {
		t.Fatal("job-not-empty must not be reported as proven exit (handles-only is insufficient)")
	}
	// 收口：真正终止以清理。
	_ = TerminateAndWaitJob(job, time.Now().Add(5*time.Second))
}
