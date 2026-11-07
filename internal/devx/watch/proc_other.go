//go:build !windows

package watch

// 非 Windows 平台：进程树所有权/有界 tree 命令**未实现**。为使公共包可编译（F-A8），提供
// 明确的 unsupported 实现：不声称已支持 POSIX 进程树收口；ProcessManager 在这些平台构造时可编译，
// 但 start/stop 会返回明确错误（调用方据此 fail-closed，不误以为已管理）。

import (
	"context"
	"errors"
	"time"
)

type jobHandle = uintptr

var errTreeOwnershipUnsupported = errors.New("watch: 进程树所有权在非 Windows 平台未实现（unsupported）")

func newJobOwnership() (jobHandle, error)             { return 0, errTreeOwnershipUnsupported }
func assignJobOwnership(job jobHandle, pid int) error { return errTreeOwnershipUnsupported }
func closeJobOwnership(job jobHandle)                 {}
func terminateAndWaitJob(job jobHandle, deadline time.Time) error {
	return errTreeOwnershipUnsupported
}
func killProcessTree(ctx context.Context, pid int) error { return errTreeOwnershipUnsupported }
