// Package watchadapter 是 internal/devx/watch 核心与 internal/cli 之间的**独立适配层**。
//
// 依赖方向（消除 cli↔watch 导入环，F-A6）：
//
//	watch  ——不 import——> cli        （核心只定义 GenerateFunc 注入点）
//	cli    ——不 import——> watch       （cli 保持原样）
//	watchadapter ——import——> cli + watch   （唯一把两者接起来的包）
//
// 真实 CLI 分发（cmd/goxml/main.go、internal/cli/cli.go）由协调者接线；本 adapter 只提供
// 「把 cli.Run 适配成 watch.GenerateFunc」以及「构造已接线的 RealBuilder / Watch」，供协调者
// 在 CLI 里一行调用，避免在 cli 包内 import watch 形成环。
package watchadapter

import (
	"context"
	"strings"

	"github.com/goxml/goxml/internal/cli"
	"github.com/goxml/goxml/internal/devx/watch"
)

// CLIGenerate 用真实 internal/cli.Run 执行目录 `generate <dir> --check`，适配成 watch.GenerateFunc。
//
// 取消/超时：cli.Run 是同步调用；在其自身 goroutine 内运行，ctx 取消时返回（不阻塞），
// 且保证**等到该 goroutine 真正返回**再退出（用 buffered channel + 排空），避免迟到写盘在
// 调用方返回后继续（F-A3 的“真正停止生成”要求：返回值前 join 生成完成/已取消）。
func CLIGenerate(ctx context.Context, projectDir string) (int, string) {
	type res struct {
		rc  int
		err string
	}
	ch := make(chan res, 1) // buffered：即使调用方不接收也不泄漏 goroutine
	go func() {
		var stdout, stderr strings.Builder
		rc := cli.Run([]string{"generate", projectDir, "--check"}, &stdout, &stderr)
		ch <- res{rc: rc, err: stderr.String()}
	}()
	select {
	case r := <-ch:
		return r.rc, r.err
	case <-ctx.Done():
		// 等 goroutine 真正收口（cli.Run 内部完成或自然结束）后再返回，杜绝迟到写盘。
		r := <-ch
		return -1, "canceled/timeout: " + ctx.Err().Error() + "\n" + r.err
	}
}

// NewRealBuilder 构造一个已用真实 CLI 生成接线好的 RealBuilder（entry 相对 ProjectDir）。
func NewRealBuilder(projectDir, entryPkg, outBinary string) *watch.RealBuilder {
	return &watch.RealBuilder{
		ProjectDir: projectDir,
		EntryPkg:   entryPkg,
		OutBinary:  outBinary,
		Generate:   CLIGenerate,
	}
}
