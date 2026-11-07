package watch

import (
	"context"
	"github.com/goxml/goxml/internal/devx/process"
	"time"
)

type ExecResult = process.ExecResult

func RunCommandTree(ctx context.Context, exe string, args []string, dir string, extraEnv []string, timeout time.Duration) ExecResult {
	return process.RunCommandTree(ctx, exe, args, dir, extraEnv, timeout)
}
