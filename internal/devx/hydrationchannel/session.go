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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// ownerID 唯一标识**本运行实例**（随机，非鉴权 token；用于区分同名会话的不同实例与发现记录归属）。
	ownerID string
	// publishedGen 本实例最近一次发布发现记录的 serving 代次（0=未发布）。
	publishedGen int
	// runID 实例级命名空间后缀（由 token 派生，同一运行的三代共享；不同运行隔离状态文件）。
	runID string
	// legacyStore 指向 R28 兼容路径 <session>.state（仅用于只读回退，不删除）。
	legacyStore *devxh.Store
}

// NewSession 构造会话（校验维度、创建状态目录归属）。
func NewSession(cfg SessionConfig) (*Session, error) {
	if cfg.ProjectDir == "" || cfg.SessionID == "" || cfg.AppID == "" || cfg.Schema == "" || cfg.Token == "" {
		return nil, fmt.Errorf("%w: projectDir/session/app/schema/token 必填", ErrBadRequest)
	}
	runID := runIDFromToken(cfg.Token)
	store, err := devxh.NewStoreWithRun(cfg.ProjectDir, cfg.SessionID, runID)
	if err != nil {
		return nil, err
	}
	legacy, _ := devxh.NewStore(cfg.ProjectDir, cfg.SessionID)
	ct := cfg.CaptureTimeout
	if ct <= 0 {
		ct = DefaultCaptureTimeout
	}
	proto := &devxh.Protocol{Store: store, Schema: cfg.Schema, AppID: cfg.AppID, CaptureTimeout: ct}
	return &Session{cfg: cfg, store: store, proto: proto, ownerID: newOwnerID(), runID: runID, legacyStore: legacy}, nil
}

// Store 返回本会话状态存储（诊断/测试）。
func (s *Session) Store() *devxh.Store { return s.store }

// Proto 返回会话协议（供 NewWatchHook 复用）。
func (s *Session) Proto() *devxh.Protocol { return s.proto }

// portfilePath 返回本**运行实例**的旧代发现文件路径。
// 运行身份 = runID（由 token 派生，同一运行三代共享；不同运行隔离）：
//
//	<session>.<runID>.port.json
//
// 这样同名会话的两个不同 token 运行不会覆盖彼此的发现记录（R29-A R4 修复）。
func (s *Session) portfilePath() string {
	name := s.cfg.SessionID + ".port.json"
	if s.runID != "" {
		name = s.cfg.SessionID + "." + s.runID + ".port.json"
	}
	return filepath.Join(s.store.Dir(), name)
}

// legacyPortfilePath 返回 R28 旧共享发现文件名（<session>.port.json），仅用于只读兼容回退。
func (s *Session) legacyPortfilePath() string {
	return filepath.Join(s.store.Dir(), s.cfg.SessionID+".port.json")
}

// NewOldGenEndpoint **旧代**：起一个绑定本会话的端点在 owner 串行点捕获 DTO，并把 {addr} 写 portfile。
// 返回端点（调用方负责在旧代生命周期内持有；Close 时 Stop）。portfile 不含 token。
func (s *Session) NewOldGenEndpoint(owner OwnerRunner, capture CaptureFunc, maxInflight int) (*Endpoint, error) {
	return s.newEndpointGen(owner, capture, maxInflight, 1)
}

// NewEndpointForGen 发布指定 serving 代次的端点（每代具备下一次捕获能力时使用 gen>=2）。
func (s *Session) NewEndpointForGen(owner OwnerRunner, capture CaptureFunc, maxInflight, gen int) (*Endpoint, error) {
	return s.newEndpointGen(owner, capture, maxInflight, gen)
}

func (s *Session) newEndpointGen(owner OwnerRunner, capture CaptureFunc, maxInflight, gen int) (*Endpoint, error) {
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
	if werr := s.writePortfile(addr, gen); werr != nil {
		_ = ep.Stop()
		return nil, werr
	}
	return ep, nil
}

// portRecord 是发现记录（不含 token）。ownerID 区分同名会话的不同运行实例，gen 为服务代次。
type portRecord struct {
	OwnerID string `json:"ownerId"`
	Gen     int    `json:"gen"`
	Addr    string `json:"addr"`
	Session string `json:"session"`
	App     string `json:"app"`
	Schema  string `json:"schema"`
}

// maxDiscoveryBytes 限制发现/临时文件读取（防超大/畸形文件）。
const maxDiscoveryBytes = 64 << 10

// readBoundedFileLimit 读取至多 limit 字节；返回真实 I/O 错误（除不存在）。超限返回错误（不整份读入）。
func readBoundedFileLimit(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for len(buf) <= limit {
		n, rerr := f.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return nil, rerr
		}
	}
	if len(buf) > limit {
		return nil, fmt.Errorf("discovery file exceeds %d bytes", limit)
	}
	return buf, nil
}

// readBoundedFile 读取至多 maxDiscoveryBytes；返回真实 I/O 错误（除不存在）。
func readBoundedFile(path string) ([]byte, error) {
	return readBoundedFileLimit(path, maxDiscoveryBytes)
}

// writePortfile 发布发现记录，共享所有权边界：
//   - 使用**独占命名**临时文件 <port>.<ownerID>.<nonce>.tmp（O_EXCL），仅清理本方临时文件。
//   - 目标已存在且**不属于本实例**（外来字节/坏 JSON/其他 live 实例记录）=> 拒绝覆盖，保留原字节，返回错误。
//   - 目标不存在或属于本实例 => 原子 rename 发布（携带 ownerID+gen）。
func (s *Session) writePortfile(addr string, gen int) error {
	if err := os.MkdirAll(s.store.Dir(), 0o755); err != nil {
		return err
	}
	target := s.portfilePath()
	// 目标存在性/归属检查（发布前）。
	if b, rerr := readBoundedFile(target); rerr == nil {
		var rec portRecord
		// 视为“本会话合法发现记录”需：session/app/schema 匹配 **且** 含 ownerId 与 generation(>=1)。
		// 缺失 ownerId/gen 的同会话记录属于**外来/旧格式**，拒绝覆盖（保留原字节）。
		if json.Unmarshal(b, &rec) != nil || rec.Session != s.cfg.SessionID || rec.App != s.cfg.AppID ||
			rec.Schema != string(s.cfg.Schema) || rec.OwnerID == "" || rec.Gen < 1 {
			return fmt.Errorf("拒绝覆盖不属于本会话（或缺 ownerId/gen）的发现记录（保留外来字节）")
		}
		// 本会话另一实例的合法记录：允许接管（新实例发布），写入本实例 ownerID（旧实例已 Stop/Close）。
	} else if !os.IsNotExist(rerr) {
		return rerr // 真实 I/O 错误（非不存在）如实上抛
	}
	rec := portRecord{OwnerID: s.ownerID, Gen: gen, Addr: addr, Session: s.cfg.SessionID, App: s.cfg.AppID, Schema: string(s.cfg.Schema)}
	b, _ := json.Marshal(rec)
	tmp := fmt.Sprintf("%s.%s.%s.tmp", target, s.ownerID, newOwnerID())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp) // 只清理本方临时文件
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	s.publishedGen = gen
	return nil
}

// ReadPortInfo 读取本运行实例的发现记录（有界）；返回 (record, error)。ok=false 表示无记录或会话不匹配。
// 优先读运行身份文件 <session>.<runID>.port.json；仅当它不存在时，只读回退 R28 旧共享文件
// <session>.port.json（且只接受不含 ownerId 的旧格式记录）——保持 R28 默认入口，绝不删除旧文件。
func (s *Session) ReadPortInfo() (portRecord, bool, error) {
	rec, ok, err := s.readPortInfoAt(s.portfilePath(), true)
	if err != nil || ok {
		return rec, ok, err
	}
	legacy, lok, lerr := s.readPortInfoAt(s.legacyPortfilePath(), false)
	if lerr != nil {
		return portRecord{}, false, lerr
	}
	if lok && legacy.OwnerID == "" {
		return legacy, true, nil
	}
	return portRecord{}, false, nil
}

// readPortInfoAt 解析指定路径的发现记录。requireOwner=true 时要求含 ownerId 与 gen（新格式）；
// false 时允许 R28 旧格式（无 ownerId/gen）。坏 JSON / 会话不符 / 无 addr 均返回 ok=false（保留文件）。
func (s *Session) readPortInfoAt(path string, requireOwner bool) (portRecord, bool, error) {
	b, err := readBoundedFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return portRecord{}, false, nil
		}
		return portRecord{}, false, err
	}
	var rec portRecord
	if json.Unmarshal(b, &rec) != nil {
		return portRecord{}, false, nil // 坏/外来 JSON => 不匹配（保留）
	}
	if rec.Session != s.cfg.SessionID || rec.App != s.cfg.AppID || rec.Schema != string(s.cfg.Schema) || rec.Addr == "" {
		return portRecord{}, false, nil
	}
	if requireOwner && (rec.OwnerID == "" || rec.Gen < 1) {
		return portRecord{}, false, nil // 缺失 ownerId/gen：新格式下视外来（不接管、不删）
	}
	return rec, true, nil
}

// ReadPort returns (addr, true) 若本会话 portfile 存在且会话匹配（父端发现旧代端点；不含 token）。
func (s *Session) ReadPort() (string, bool) {
	rec, ok, _ := s.ReadPortInfo()
	return rec.Addr, ok
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
	applied, rerr := ap.RestoreOnOwner(ctx, minGen)
	if applied || rerr == nil {
		return applied, rerr
	}
	// 实例级命名空间无状态时回退读取 R28 旧格式 <session>.state（只读兼容；不迁移不删除）。
	if s.legacyStore != nil && errors.Is(rerr, devxh.ErrNoState) {
		legacyProto := &devxh.Protocol{Store: s.legacyStore, Schema: s.cfg.Schema, AppID: s.cfg.AppID, CaptureTimeout: s.proto.CaptureTimeout}
		lap, lerr := NewApplier(legacyProto, owner, apply)
		if lerr == nil {
			return lap.RestoreOnOwner(ctx, minGen)
		}
	}
	return applied, rerr
}

// Close 删除本会话状态文件与 portfile，但**仅在证明当前会话拥有该文件时**：
//   - 状态文件：必须能被本会话 Expect（schema/app/session）完整校验通过（即确为本会话写入的合法状态）；
//     校验失败（坏文件/他会话/外来字节）=> **保留原字节与 mtime**，不删除。
//   - portfile：内容 session/app/schema 必须与本会话一致，才删除；否则保留。
//
// 返回首个真实错误（不存在不算错误）；所有权不成立时不报错、保留文件。
func (s *Session) Close() error {
	var firstErr error
	// 状态文件：有界读取 + 完整校验证明所有权才删除；真实读取错误如实上报且不删除（区分不存在）。
	if own, oerr := s.storeOwningDetail(); oerr != nil {
		firstErr = oerr
	} else if own {
		if err := os.Remove(s.store.Path()); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	// 发现记录：仅当它属于**本实例**（ownerID 匹配）才删除；否则保留（旧代释放不得删新代记录）。
	if rec, ok, rerr := s.ReadPortInfo(); rerr == nil && ok && rec.OwnerID == s.ownerID {
		if err := os.Remove(s.portfilePath()); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	// 只清理**本方**临时文件（<port>.<ownerID>.*.tmp），不触碰他人临时文件。
	if matches, gerr := filepath.Glob(fmt.Sprintf("%s.%s.*.tmp", s.portfilePath(), s.ownerID)); gerr == nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
	return firstErr
}

// storeOwning 判断状态文件是否为**本会话**拥有的合法状态（用于 Close 删除前的所有权证明）。
// 只读判定，不改动文件（保留错误路径原字节与 mtime）。
func (s *Session) storeOwning() bool {
	own, _ := s.storeOwningDetail()
	return own
}

// storeOwningDetail 是 storeOwning 的带错误版本：用**有界读取**（至多 MaxEnvelopeBytes+1）替代 os.ReadFile，
// 区分「不存在」与「真实读取错误」，并对超限/外来/坏文件一律返回 (false,nil) 以保留原字节与 mtime。
func (s *Session) storeOwningDetail() (bool, error) {
	// 仅本实例命名空间路径，绝不读 legacy/他人路径判定删除。
	raw, err := readBoundedFileLimit(s.store.Path(), rst.MaxEnvelopeBytes+1)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // 不存在：不拥有、非错误
		}
		return false, err // 真实读取错误：区分并如实返回（Close 上报，不删除）
	}
	if len(raw) > rst.MaxEnvelopeBytes {
		return false, nil // 超限文件：不视本会话，保留（字节/mtime 不变）
	}
	exp := rst.Expect{Schema: s.cfg.Schema, AppID: s.cfg.AppID, SessionID: s.cfg.SessionID, MinGeneration: 0}
	if _, derr := rst.DecodeAndVerify(raw, exp); derr != nil {
		return false, nil // 非本会话合法状态（坏文件/他人/外来字节）=> 不拥有，保留。
	}
	return true, nil
}

// newOwnerID 生成随机不透明实例标识（非鉴权 token；不写日志）。
func newOwnerID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// runIDFromToken 由会话 token 派生**非机密**实例命名空间后缀（同一运行三代同 token => 同 runID）。
// 仅取 sha256 前 8 字节，不可逆推 token；不用于鉴权、不写日志。
func runIDFromToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:8])
}
