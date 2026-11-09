// Package hydration 定义跨进程状态暂存/恢复的**显式 DTO 协议**（R27 Windows 基础范围）。
//
// 边界（务必与本包注释一起理解）：
//   - **显式 opt-in**：调用方用自己的 DTO 类型 + codec（本包提供基于标准库 encoding/json 的通用
//     封装，但 DTO 的类型/字段由调用方显式声明），**不反射扫描任意 ViewModel**、不恢复 Widget/Gio
//     Ops/Context/函数/锁对象。恢复只在其声明的所有者线程应用。
//   - **快照时点**：快照代表“在所有者（UI/Dispatcher）串行执行点上捕获的不可变 DTO”。它**不承诺**
//     停止前所有后续编辑都已保存，也**不宣称**无损持续同步或零停机。
//   - 本包只依赖标准库；开发工具层（internal/devx/hydration）消费本包，反向禁止（Runtime 不 import devx）。
//
// 基础范围已获批并通过限定验收；通用控制通道、CLI 自动恢复与整卡仍未闭环。
package hydration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Schema 标识一份状态 DTO 的 schema（调用方定义，如 "demo.counter.v1"）。
type Schema string

// Envelope 是跨进程状态文件的**显式外层协议**。字段全为显式 JSON，不承载任意反射数据。
type Envelope struct {
	// Version 是 Envelope 协议版本（本包为 1）。与调用方 DTO 的 Schema 版本相互独立。
	Version int `json:"version"`
	// Schema 是 DTO 的 schema 标识（调用方声明，用于拒绝不匹配的恢复）。
	Schema Schema `json:"schema"`
	// AppID 是应用/工程标识（候选新增值；与 cacheFormatVersion/gen-N 无关）。
	AppID string `json:"appId"`
	// SessionID 是一次进程/开发会话标识（用于隔离双会话/双工程）。
	SessionID string `json:"sessionId"`
	// Generation 是产生该快照的代次（由 Watch 代际号给出；用于过期判定）。
	Generation int `json:"generation"`
	// CreatedAtUnixNano 是捕获时点（UTC，仅供诊断/过期判断，不用于事件顺序推断）。
	CreatedAtUnixNano int64 `json:"createdAtUnixNano"`
	// Payload 是调用方 DTO 的显式 JSON 字节（Base64 由 encoding/json 处理）。大小受 MaxPayloadBytes 限制。
	Payload json.RawMessage `json:"payload"`
	// Checksum 是 Payload 的 SHA-256（hex）。仅证明内容一致，**不声称鉴权/不可伪造**。
	Checksum string `json:"checksum"`
}

// 协议常量与上限（候选新增值，独立于缓存格式/输出修订）。
const (
	// EnvelopeVersion 是本包 Envelope 协议版本。
	EnvelopeVersion = 1
	// MaxPayloadBytes 是 Payload 的最大字节数（超出即拒绝，不截断）。
	MaxPayloadBytes = 4 << 20 // 4 MiB
	// MaxEnvelopeBytes 是整份状态文件的最大字节（防御超大文件）。
	MaxEnvelopeBytes = 8 << 20 // 8 MiB
)

// 错误类别（供上层精确分类，避免把“损坏/过期/不匹配”混作“成功”）。
var (
	ErrCorrupt         = errors.New("hydration: 状态文件损坏或格式非法")
	ErrChecksum        = errors.New("hydration: checksum 不符")
	ErrTooLarge        = errors.New("hydration: 状态超过大小上限")
	ErrSchemaMismatch  = errors.New("hydration: schema 不匹配")
	ErrVersionMismatch = errors.New("hydration: Envelope 版本不匹配")
	ErrAppMismatch     = errors.New("hydration: appId 不匹配")
	ErrSessionMismatch = errors.New("hydration: sessionId 不匹配")
	ErrStale           = errors.New("hydration: 快照代次已过期（stale）")
)

// ChecksumOf 计算原始字节的 SHA-256 hex（诊断用；协议使用 canonicalChecksum）。
func ChecksumOf(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// canonicalChecksum 计算 Payload 的**规范（紧凑 JSON）**形式的 SHA-256 hex。
//
// F-A2：Encode 重新序列化 Envelope 时，encoding/json 会对 <tag>/& 做 HTML 转义、对缩进/空白做紧凑化，
// 使落盘 Payload 字节与原始入参不同——若对原始字节取校验和，则“自己的 Encode→Decode”会自毁。
// 因此校验和基于**规范形式**：无论原始 payload 是紧凑/缩进/含 HTML 字符，json.Compact 归一后一致；
// 真实篡改（值改变）仍改变规范形式从而被拒。payload 非法 JSON 时返回 ErrCorrupt（不校验非 JSON）。
func canonicalChecksum(payload []byte) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, payload); err != nil {
		return "", fmt.Errorf("%w: payload 非合法 JSON: %v", ErrCorrupt, err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// NewEnvelope 用显式参数构造 Envelope（含校验和）。payload 必须 <= MaxPayloadBytes。
func NewEnvelope(schema Schema, appID, sessionID string, generation int, now time.Time, payload []byte) (*Envelope, error) {
	if len(payload) > MaxPayloadBytes {
		return nil, ErrTooLarge
	}
	cs, cerr := canonicalChecksum(payload)
	if cerr != nil {
		return nil, cerr
	}
	return &Envelope{
		Version:           EnvelopeVersion,
		Schema:            schema,
		AppID:             appID,
		SessionID:         sessionID,
		Generation:        generation,
		CreatedAtUnixNano: now.UTC().UnixNano(),
		Payload:           append(json.RawMessage(nil), payload...),
		Checksum:          cs,
	}, nil
}

// Encode 把 Envelope 序列化为字节（紧凑 JSON）。不做原子写——原子写由 devx 存储层负责。
func (e *Envelope) Encode() ([]byte, error) {
	if e == nil {
		return nil, ErrCorrupt
	}
	if len(e.Payload) > MaxPayloadBytes {
		return nil, ErrTooLarge
	}
	// 用 Encoder 并关闭 HTML 转义：Payload 为 RawMessage，保持其 JSON 值不被 <>& 转义。
	// 编码器仍会紧凑化（不改变规范形式），与 canonicalChecksum 口径一致（F-A2）。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(e); err != nil {
		return nil, fmt.Errorf("hydration: 编码 Envelope 失败: %w", err)
	}
	b := bytes.TrimRight(buf.Bytes(), "\n")
	if len(b) > MaxEnvelopeBytes {
		return nil, ErrTooLarge
	}
	return b, nil
}

// Expect 描述恢复端期望的上下文（用于校验）。
type Expect struct {
	Schema    Schema
	AppID     string
	SessionID string
	// MinGeneration 是“不早于”的代次下限（用于拒绝过期快照）。
	MinGeneration int
}

// DecodeAndVerify 解析并**完整校验**一份状态文件字节：大小、JSON、版本、checksum、schema/app/session、
// 过期代次。任一项不满足即返回对应错误（不部分接受）。成功返回 Envelope（其 Payload 已通过校验和）。
func DecodeAndVerify(raw []byte, exp Expect) (*Envelope, error) {
	if len(raw) > MaxEnvelopeBytes {
		return nil, ErrTooLarge
	}
	var e Envelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if e.Version != EnvelopeVersion {
		return nil, ErrVersionMismatch
	}
	if len(e.Payload) > MaxPayloadBytes {
		return nil, ErrTooLarge
	}
	if cs, cerr := canonicalChecksum(e.Payload); cerr != nil {
		return nil, cerr
	} else if cs != e.Checksum {
		return nil, ErrChecksum
	}
	if exp.Schema != "" && e.Schema != exp.Schema {
		return nil, ErrSchemaMismatch
	}
	if exp.AppID != "" && e.AppID != exp.AppID {
		return nil, ErrAppMismatch
	}
	if exp.SessionID != "" && e.SessionID != exp.SessionID {
		return nil, ErrSessionMismatch
	}
	if exp.MinGeneration > 0 && e.Generation < exp.MinGeneration {
		return nil, ErrStale
	}
	return &e, nil
}

// DecodePayload 用调用方给定的目标 DTO 解码（显式类型，非反射扫描）。调用方负责 DTO 自身的字段校验。
func (e *Envelope) DecodePayload(dst any) error {
	if e == nil || len(e.Payload) == 0 {
		return ErrCorrupt
	}
	if err := json.Unmarshal(e.Payload, dst); err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return nil
}
