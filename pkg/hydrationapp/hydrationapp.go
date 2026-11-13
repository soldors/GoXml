// Package hydrationapp 是**公开接入入口**（候选）：让外部普通 module 的应用在**不 import internal/** 的前提下，
// 消费 R29-A 的跨进程状态恢复能力。它单向依赖 internal/devx/hydrationchannel，仅做类型别名与函数转发，
// 不复制协议、不新增语义。正式公开 API 形态待负责人裁决（候选人选：本包名与导出面）。
//
// 依赖方向：外部应用 → pkg/hydrationapp → internal/devx/hydrationchannel →（devx 内部）。外部应用不需要、
// 也不能引用 internal/*（Go internal 规则），因此公共接入必须经本包。
package hydrationapp

import (
	"context"

	hc "github.com/goxml/goxml/internal/devx/hydrationchannel"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

// 消费者需要的类型（别名，签名与 internal 一致，无复制）。
type (
	// Session 是一次开发会话的应用接入句柄（详见 internal/devx/hydrationchannel.Session）。
	Session = hc.Session
	// SessionConfig 绑定一次开发会话的维度与端点参数。
	SessionConfig = hc.SessionConfig
	// OwnerRunner 是应用提供的 owner 串行调度器。
	OwnerRunner = hc.OwnerRunner
	// CaptureFunc 在 owner 串行点返回当前活跃 DTO 与 serving 代次。
	CaptureFunc = hc.CaptureFunc
	// ApplyFunc 在 owner 串行点用已校验 DTO 恢复应用状态。
	ApplyFunc = hc.ApplyFunc
	// ReplayPolicy 选择 nonce 防重放满额策略。
	ReplayPolicy = hc.ReplayPolicy
)

const (
	// ReplayEvictOldest / ReplayRejectOnFull 转发 nonce 满额策略。
	ReplayEvictOldest  = hc.ReplayEvictOldest
	ReplayRejectOnFull = hc.ReplayRejectOnFull
	// DefaultReplayCapacity 默认 nonce 窗口容量。
	DefaultReplayCapacity = hc.DefaultReplayCapacity
	// DefaultCaptureTimeout 默认捕获超时。
	DefaultCaptureTimeout = hc.DefaultCaptureTimeout
)

// NewSession 构造会话（转发 internal 实现）。
func NewSession(cfg SessionConfig) (*Session, error) { return hc.NewSession(cfg) }

// SchemaString 便于外部把字符串 schema 转为 rst.Schema（避免外部再 import pkg/runtime 细节）。
func SchemaString(s string) rst.Schema { return rst.Schema(s) }

var _ = context.Background // 保留 context 依赖以便未来门面扩展（避免未用导入）
