// Package hydration（devx 层）实现跨进程状态的**原子文件存储**与**会话协议编排**（R27-A 候选）。
//
// 依赖方向：本包**消费** pkg/runtime/hydration 的 DTO 协议（Runtime 层不 import 本包；禁止反向）。
// 只依赖标准库 + runtime 层。
//
// 会话/所有权：
//   - 状态文件固定在 <projectDir>/.goxml/hydration/<sessionID>.state（.goxml 已在 Watch 快照排除，
//     状态写入**不自激**重建）。
//   - 本进程只写**自己 session** 的文件；不覆盖、不清理他人/旧文件（token=sessionID 证明“自有”范围，
//     不声称鉴权）。旧文件是否被读取仅由调用方显式 Expect.SessionID 决定。
//
// 原子性：写 = 同目录临时文件 → fsync → rename（Windows 用 ReplaceFile 语义的 rename 覆盖自己文件）。
// 读 = 读取 → schema/version/checksum/session/generation 全校验；任一失败返回具体错误，不部分应用。
package hydration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

// Store 是某工程/会话的状态文件存储。
type Store struct {
	projectDir string
	sessionID  string
}

// NewStore 构造存储。<projectDir>/.goxml/hydration 为状态目录。
func NewStore(projectDir, sessionID string) (*Store, error) {
	if strings.TrimSpace(projectDir) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("hydration: projectDir 与 sessionID 必填")
	}
	// sessionID 作为文件名：拒绝路径分隔/保留字符，防止越界写。
	if strings.ContainsAny(sessionID, `/\:*?"<>|`) || sessionID == "." || sessionID == ".." {
		return nil, errors.New("hydration: 非法 sessionID（含路径分隔/保留字符）")
	}
	return &Store{projectDir: projectDir, sessionID: sessionID}, nil
}

// Dir 返回状态目录（不含创建）。
func (s *Store) Dir() string {
	return filepath.Join(s.projectDir, ".goxml", "hydration")
}

// Path 返回本会话状态文件路径。
func (s *Store) Path() string {
	return filepath.Join(s.Dir(), s.sessionID+".state")
}

// SessionID 返回会话标识。
func (s *Store) SessionID() string { return s.sessionID }

// WriteAtomic 原子写入 Envelope 字节：临时文件（同目录，唯一后缀）→ fsync → rename 到本会话文件。
// 失败时清理自身临时文件；**不触碰**其它文件。
func (s *Store) WriteAtomic(raw []byte) error {
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		return fmt.Errorf("hydration: 创建状态目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(s.Dir(), "."+s.sessionID+".tmp-*")
	if err != nil {
		return fmt.Errorf("hydration: 创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, werr := tmp.Write(raw); werr != nil {
		_ = tmp.Close()
		return fmt.Errorf("hydration: 写临时文件失败: %w", werr)
	}
	if serr := tmp.Sync(); serr != nil {
		_ = tmp.Close()
		return fmt.Errorf("hydration: fsync 失败: %w", serr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("hydration: 关闭临时文件失败: %w", cerr)
	}
	// rename 覆盖本会话文件（原子）。目标若被他人替换成不同 session 文件，我们不覆盖（由调用方保证 Path 属本 session）。
	if rerr := os.Rename(tmpName, s.Path()); rerr != nil {
		return fmt.Errorf("hydration: rename 提交失败: %w", rerr)
	}
	cleanup = false
	return nil
}

// ReadVerified 读取本会话状态文件并按 Expect 完整校验（schema/app/session/版本/checksum/generation）。
// 文件不存在 => (nil, os.ErrNotExist 语义 via ErrNoState)。校验失败返回具体错误，不返回部分 enevelope。
func (s *Store) ReadVerified(exp rst.Expect) (*rst.Envelope, error) {
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoState
		}
		return nil, fmt.Errorf("hydration: 读取状态文件失败: %w", err)
	}
	// 强制会话一致：调用方期望的 SessionID 若为空，补成本会话；避免误读他人文件。
	if exp.SessionID == "" {
		exp.SessionID = s.sessionID
	}
	return rst.DecodeAndVerify(raw, exp)
}

// ErrNoState 表示无状态文件（首次/冷启动合法）。
var ErrNoState = errors.New("hydration: 无状态文件")

// NowFunc 便于确定性测试注入时钟。
type NowFunc func() time.Time
