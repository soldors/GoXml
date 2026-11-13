package hydrationchannel

// R29-A S2：**应用接入**的可消费单一入口。把"显式 DTO + owner 串行 Capture/Apply + 现有 Hook"封装为
// 应用可用的两个动作，避免应用重写第二套协议：
//   - 旧代（服务端）：NewSessionEndpoint 起端点（Capture 在 owner 串行点），把 {addr} 写到 portfile 供父端发现。
//   - 新代（恢复端）：Session.Restore 读+校验+在 owner 应用（复用 hydrationchannel.Applier 与 devx/hydration.Protocol）。
//   - 父端（Watch）：NewWatchHook 复用现有适配器（见 adapter.go），据 portfile 构造 Client。
//
// 绑定：session/app/schema/token 一并绑定端点与客户端；代次由 Watch 传入的 oldGen 与端点 servingGen 关联。
// 状态文件归属：devx/hydration.Store 固定在 <project>/.goxml/hydration/<session>.state；仅本会话文件读写，
// 不覆盖他人文件（见 store.existingIsOwned）。清理：Session.Close 删除本会话状态文件（仅自己路径）。
//
// runtime 不反向 import devx：本文件在 internal/devx 层，只消费 pkg/runtime/hydration（DTO）与既有 devx 包。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

// SessionConfig 绑定一次开发会话的维度与端点参数。
type SessionConfig struct {
	ProjectDir string
	SessionID  string
	AppID      string
	Schema     rst.Schema
	// Token 是会话绑定密钥（不写日志/不写 portfile）。
	Token string
	// ReplayCap / ReplayPolicy 可选：有界 nonce 防重放（S1）。0/零值取默认。
	ReplayCap    int
	ReplayPolicy ReplayPolicy
	// CaptureTimeout 可选（默认 DefaultCaptureTimeout）。
	CaptureTimeout time.Duration
}

// Session 是一次开发会话的应用接入句柄。
type Session struct {
	cfg   SessionConfig
	store *devxh.Store
	proto *devxh.Protocol
}

// NewSession 构造会话（校验维度、创建状态目录归属）。
func NewSession(cfg SessionConfig) (*Session, error) {
	if cfg.ProjectDir == "" || cfg.SessionID == "" || cfg.AppID == "" || cfg.Schema == "" || cfg.Token == "" {
		return nil, fmt.Errorf("%w: projectDir/session/app/schema/token 必填", ErrBadRequest)
	}
	store, err := devxh.NewStore(cfg.ProjectDir, cfg.SessionID)
	if err != nil {
		return nil, err
	}
	ct := cfg.CaptureTimeout
	if ct <= 0 {
		ct = DefaultCaptureTimeout
	}
	proto := &devxh.Protocol{Store: store, Schema: cfg.Schema, AppID: cfg.AppID, CaptureTimeout: ct}
	return &Session{cfg: cfg, store: store, proto: proto}, nil
}

// Store 返回本会话状态存储（诊断/测试）。
func (s *Session) Store() *devxh.Store { return s.store }

// Proto 返回会话协议（供 NewWatchHook 复用）。
func (s *Session) Proto() *devxh.Protocol { return s.proto }

// PortfilePath 返回旧代发现文件路径（与状态目录同处 .goxml，被 Watch 快照排除）。
func (s *Session) portfilePath() string {
	return filepath.Join(s.store.Dir(), s.cfg.SessionID+".port.json")
}

// NewOldGenEndpoint **旧代**：起一个绑定本会话的端点在 owner 串行点捕获 DTO，并把 {addr} 写 portfile。
// 返回端点（调用方负责在旧代生命周期内持有；Close 时 Stop）。portfile 不含 token。
func (s *Session) NewOldGenEndpoint(owner OwnerRunner, capture CaptureFunc, maxInflight int) (*Endpoint, error) {
	ep, err := NewEndpoint(owner, capture, s.cfg.SessionID, s.cfg.AppID, string(s.cfg.Schema), s.cfg.Token, maxInflight)
	if err != nil {
		return nil, err
	}
	if s.cfg.ReplayCap > 0 || s.cfg.ReplayPolicy != ReplayEvictOldest {
		ep.SetReplayBounds(s.cfg.ReplayCap, s.cfg.ReplayPolicy)
	}
	addr, err := ep.Start()
	if err != nil {
		return nil, err
	}
	if werr := s.writePortfile(addr); werr != nil {
		_ = ep.Stop()
		return nil, werr
	}
	return ep, nil
}

func (s *Session) writePortfile(addr string) error {
	if err := os.MkdirAll(s.store.Dir(), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]string{"addr": addr, "session": s.cfg.SessionID, "app": s.cfg.AppID, "schema": string(s.cfg.Schema)})
	tmp := s.portfilePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.portfilePath())
}

// ReadPort returns (addr, true) 若本会话 portfile 存在且会话匹配（父端发现旧代端点；不含 token）。
func (s *Session) ReadPort() (string, bool) {
	b, err := os.ReadFile(s.portfilePath())
	if err != nil {
		return "", false
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return "", false
	}
	if m["session"] != s.cfg.SessionID || m["app"] != s.cfg.AppID || m["schema"] != string(s.cfg.Schema) {
		return "", false
	}
	return m["addr"], m["addr"] != ""
}

// NewClientFromPort 父端：据 portfile 构造 Client（token 由父端另行持有，不来自文件）。
func (s *Session) NewClientFromPort(token string) (*Client, bool) {
	addr, ok := s.ReadPort()
	if !ok {
		return nil, false
	}
	c, err := NewClient(addr, s.cfg.SessionID, s.cfg.AppID, string(s.cfg.Schema), token)
	if err != nil {
		return nil, false
	}
	return c, true
}

// Restore **新代**：读取+校验+（owner 串行）应用本会话状态。返回 (applied,err)；ErrNoState=冷启动。
func (s *Session) Restore(ctx context.Context, minGen int, owner OwnerRunner, apply ApplyFunc) (bool, error) {
	ap, err := NewApplier(s.proto, owner, apply)
	if err != nil {
		return false, err
	}
	return ap.RestoreOnOwner(ctx, minGen)
}

// Close 删除本会话状态文件与 portfile（**仅本会话路径**，不触碰他人文件）。
func (s *Session) Close() error {
	var firstErr error
	if err := os.Remove(s.store.Path()); err != nil && !os.IsNotExist(err) {
		firstErr = err
	}
	if err := os.Remove(s.portfilePath()); err != nil && !os.IsNotExist(err) && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
