//go:build windows

package watch

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// isTransientScanErr 报告快照读取错误是否为**瞬时**文件共享/锁冲突（Windows 共享冲突/锁冲突）。
// 这类错误通常由并发构建或编辑器短暂持有文件引起，应跳过本 tick 保旧代，而非终止 Watch。
func isTransientScanErr(err error) bool {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return errors.Is(pe.Err, windows.ERROR_SHARING_VIOLATION) || errors.Is(pe.Err, windows.ERROR_LOCK_VIOLATION)
	}
	return false
}
