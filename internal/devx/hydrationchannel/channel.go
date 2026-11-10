// Package hydrationchannel 是 R28-A 的**应用端控制通道候选**：把 R27 的 fixture 专用“请求文件”
// 提升为可复用的本地端点 + 客户端 + 恢复应用器，经现有 Watch HydrationHook 完成真实两进程捕获/恢复。
//
// 传输选择（S1）：**仅回环（127.0.0.1:0）的显式 TCP 端点**。理由：仅用标准库 net，无安装/新依赖，
// Windows 下可用，跨平台可编译；不监听非本机地址。备选（会话目录邮箱/匿名管道/命名管道）要么平台特定
// 要么需 fd 传递，本标准库方案成本最低且可复用。成本：每端点一个监听 socket + 每请求一次 RTT（本机环回）。
//
// 边界/不变量：
//   - **Capture/Apply 只在应用 owner 串行点**执行：端点/应用器把工作投递给 OwnerRunner（应用提供的
//     串行调度器），transport goroutine 绝不直接读写活动模型。worker 只搬不可变字节。
//   - 校验：session/app/schema + serving generation + 请求 nonce/reqID + 消息上限 + 超时/取消 + 重放/
//     错会话/错误关联。checksum 只证完整性，不冒充鉴权；本地 token 只做会话绑定，绝不写日志。
//   - 无效请求不能取走他会话 DTO、改他方状态或触发 Apply；端点不暴露任何“应用/提交”能力（仅 Capture）。
//   - 未启用（无端点）时无监听/无新线程/I/O，R27 行为不变。
//
// 新协议/API 为**隔离候选**：完整实现与验证，最终语义由负责人裁决。
package hydrationchannel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// 协议版本与上限（候选值）。
const (
	// ProtocolVersion 是通道请求/响应协议版本。
	ProtocolVersion = 1
	// MaxRequestBytes 是单个请求的最大字节数。
	MaxRequestBytes = 64 << 10 // 64 KiB
	// MaxResponseBytes 是单个响应的最大字节数（含 DTO，受 runtime/hydration Payload 上限约束）。
	MaxResponseBytes = 8 << 20 // 8 MiB
	// DefaultIODeadline 是单次请求端到端默认上限（客户端/服务端读写）。
	DefaultIODeadline = 5 * time.Second
)

// 通道错误类别。
var (
	ErrUnauthorized = errors.New("hydrationchannel: token/session 未授权")
	ErrWrongSession = errors.New("hydrationchannel: 会话不匹配")
	ErrBadRequest   = errors.New("hydrationchannel: 请求格式非法")
	ErrTooLarge     = errors.New("hydrationchannel: 消息超过上限")
	ErrReplay       = errors.New("hydrationchannel: 重复请求（nonce 已见）")
	ErrOwnerClosed  = errors.New("hydrationchannel: owner 已关闭")
	ErrEndpointDown = errors.New("hydrationchannel: 端点未监听/已停止")
	ErrMismatch     = errors.New("hydrationchannel: 响应关联不符（reqID/session/gen）")
	ErrCaptureFail  = errors.New("hydrationchannel: 应用侧捕获失败")
	ErrBusy         = errors.New("hydrationchannel: 端点在途请求已达上限（busy）")
)

// captureRequest 是客户端→端点的一次快照请求（显式 JSON）。
type captureRequest struct {
	Version int    `json:"version"`
	Token   string `json:"token"`
	Session string `json:"session"`
	App     string `json:"app"`
	Schema  string `json:"schema"`
	// ServingGen 是请求端认为在服务的旧代次（端点据此与自身 owner 现状比对，防止错误关联）。
	ServingGen int `json:"servingGen"`
	// ReqID 是客户端生成的一次性请求标识（响应原样回显，用于错误关联）。
	ReqID string `json:"reqId"`
	// Nonce 是防重放的一次性随机串（端点记录已见集合，重复拒绝）。
	Nonce string `json:"nonce"`
}

// captureResponse 是端点→客户端的响应（显式 JSON）。
type captureResponse struct {
	Version int    `json:"version"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Session string `json:"session"`
	ReqID   string `json:"reqId"`
	// Gen 是捕获时 owner 报告的实际 serving 代次。
	Gen     int             `json:"gen"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// OwnerRunner 是应用提供的**串行 owner 调度器**：Do 把 fn 投递到 owner 串行点执行并等待完成。
// 端点据此保证 Capture 在 owner 上执行；Applier 据此保证 Apply 在 owner 上执行。UI owner 不阻塞
// I/O/等 worker——Do 只承载应用自身的捕获/应用闭包（快速、无阻塞）。
type OwnerRunner interface {
	// Do 在 owner 串行点执行 fn 并返回其 error（若 owner 已关闭返回 ErrOwnerClosed）。
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// dims 打包端点/客户端的会话维度。
type dims struct {
	Session string
	App     string
	Schema  string
}

func (d dims) validate() error {
	if d.Session == "" || d.App == "" || d.Schema == "" {
		return fmt.Errorf("%w: session/app/schema 必填", ErrBadRequest)
	}
	return nil
}
