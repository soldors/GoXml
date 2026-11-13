//go:build windows

package watchadapter

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// withGenerateLock 用跨进程独占锁串行化**共享 generate 事务**（.goxml manifest/cache/资产写入）。
// 同一项目的多个 Watch 并行时，generate --check 对共享 .goxml 的写入被串行化，避免交错破坏；
// 使用 Windows LockFileEx：进程异常退出时由 OS 自动释放，无需清理陈旧锁文件。锁获取受 ctx 有界。
func withGenerateLock(ctx context.Context, projectDir string, fn func() (int, string)) (int, string) {
	dir := filepath.Join(projectDir, ".goxml")
	_ = os.MkdirAll(dir, 0o755)
	lockPath := filepath.Join(dir, "generate.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return -1, "watchadapter: open generate.lock: " + err.Error()
	}
	defer f.Close()
	// 有界轮询获取独占锁（受 ctx）。
	overlapped := new(windows.Overlapped)
	for {
		err = windows.LockFileEx(windows.Handle(f.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
		if err == nil {
			break
		}
		if e := ctx.Err(); e != nil {
			return -1, "watchadapter: generate lock wait canceled: " + e.Error()
		}
		time.Sleep(15 * time.Millisecond)
	}
	defer func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
	}()
	return fn()
}
