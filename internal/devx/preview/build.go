package preview

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// buildHost 在宿主目录里执行 go build（只构建，不运行）。
//
// 环境口径与仓库工程测试一致（离线、零 CGO、本地工具链）：
//   - GOPROXY=off / GOSUMDB=off：宿主依赖必须来自本地模块缓存，
//     不允许预览悄悄联网拉依赖；
//   - CGO_ENABLED=0：Windows 零 CGO 红线（AGENT.md）；
//   - GOFLAGS=-mod=readonly：宿主 go.mod 由预览生成，构建不得顺手改它；
//     用户 GOFLAGS（含 -overlay）被显式覆盖——宿主是本线自有模块，
//     用户 overlay 不应影响它的构建；
//   - GOTOOLCHAIN=local：不因 go.mod 的 go 指令自动下载工具链。
//
// 返回（exe 路径、编译器输出、错误）。失败时编译器输出原样保留在
// 返回值与调用方错误里（证据），宿主目录由调用方保留。
func buildHost(h *hostLayout, opts Options) (string, string, error) {
	goExe, err := exec.LookPath("go")
	if err != nil {
		return "", "", fmt.Errorf("preview: 构建宿主需要 PATH 上的 Go: %w", err)
	}
	timeout := opts.BuildTimeout
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	exe := filepath.Join(h.Dir, "preview-host.exe")
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"build", "-mod=readonly", "-o", exe, "."}
	cmd := exec.CommandContext(ctx, goExe, args...)
	cmd.Dir = h.Dir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOMAXPROCS=2",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
		"GOFLAGS=-mod=readonly",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return exe, out.String(), fmt.Errorf("preview: 宿主构建超时（>%s）: %w", timeout, err)
		}
		return exe, out.String(), fmt.Errorf("preview: go build 退出错误: %w", err)
	}
	return exe, out.String(), nil
}
