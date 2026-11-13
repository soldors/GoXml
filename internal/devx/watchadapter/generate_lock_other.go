//go:build !windows

package watchadapter

import "context"

// withGenerateLock 在非 Windows 平台直接执行（Windows 首发范围；并行 generate 串行化仅 Windows 需要）。
func withGenerateLock(ctx context.Context, projectDir string, fn func() (int, string)) (int, string) {
	return fn()
}
