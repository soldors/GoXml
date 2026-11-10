package hydrationchannel

// 可消费适配器：把 hydrationchannel 的 Client（取旧代快照）接到现有 watch.HydrationHook，
// 使真实 Watch 编排（runGeneration → BeforeSwap → stop-old/start-new）使用本应用端通道。
// 本文件只引用 watch 的**导出**类型（HydrationHook/ProcessManager 的 EnvFor 由接线方设），
// 不改 watch 生产代码；worker 只搬字节，序列化/提交仍在 devx/hydration.Protocol。

import (
	"context"
	"fmt"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
	"github.com/goxml/goxml/internal/devx/watch"
)

// HookConfig 组装一个基于通道的 Watch HydrationHook。
type HookConfig struct {
	// Proto 是会话协议（Store/Schema/AppID/CaptureTimeout）。必填。
	Proto *devxh.Protocol
	// ClientFor 返回**当前在服务旧代**的通道客户端（父端在旧代启动后据 portfile 构造）。
	// 返回 (nil,false) 表示无可用端点（例如旧代刚退出）——此时不进行捕获（保旧/hook 返回错误）。
	ClientFor func(oldGen int) (*Client, bool)
	// ServingGen 可选：返回父端认为在服务的代次（默认由 Watch 传入的 oldGen）。
	ServingGen func() (int, bool)
	// RestoreEnv 返回新代启动注入（指向状态文件/会话）。必填（否则新代无法恢复）。
	RestoreEnv func(gen int) []string
}

// NewWatchHook 构造一个把 Capture 指向 Client.Capture 的 watch.HydrationHook。
func NewWatchHook(cfg HookConfig) (*watch.HydrationHook, error) {
	if cfg.Proto == nil {
		return nil, fmt.Errorf("hydrationchannel: HookConfig.Proto 必填")
	}
	if cfg.ClientFor == nil {
		return nil, fmt.Errorf("hydrationchannel: HookConfig.ClientFor 必填")
	}
	capture := func(ctx context.Context, oldGen int) ([]byte, error) {
		cl, ok := cfg.ClientFor(oldGen)
		if !ok || cl == nil {
			return nil, fmt.Errorf("hydrationchannel: 无可用旧代端点（gen=%d）", oldGen)
		}
		return cl.Capture(ctx, oldGen)
	}
	hook := &watch.HydrationHook{Proto: cfg.Proto, Capture: capture, RestoreEnv: cfg.RestoreEnv}
	return hook, nil
}

// DefaultCaptureTimeout 供接线示例使用。
const DefaultCaptureTimeout = 5 * time.Second
