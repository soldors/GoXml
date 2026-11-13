//go:build !windows

package watch

// isTransientScanErr 在非 Windows 平台无 Windows 共享冲突语义，返回 false（不改变行为）。
func isTransientScanErr(err error) bool { return false }
