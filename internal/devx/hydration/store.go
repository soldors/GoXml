// Package hydration（devx 层）实现跨进程状态的**原子文件存储**与**会话协议编排**（R27 获批基础范围）。
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// F-A4：提交前证明目标文件属于本会话。若目标已存在，必须是**本会话**的合法状态文件（Envelope.SessionID
	// == 本会话），否则拒绝覆盖（保旧：字节/mtime 不变）。未证明所有权则不覆盖，避免毁掉旧/他人文件。
	if own, oerr := s.existingIsOwned(); oerr != nil {
		return oerr
	} else if !own {
		return fmt.Errorf("hydration: 目标状态文件非本会话所有（拒绝覆盖，保旧）: %s", s.Path())
	}
	// rename 覆盖本会话文件（原子）。
	if rerr := os.Rename(tmpName, s.Path()); rerr != nil {
		return fmt.Errorf("hydration: rename 提交失败: %w", rerr)
	}
	cleanup = false
	return nil
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// existingIsOwned 判断目标路径是否已存在且属于本会话（Envelope.SessionID==本会话且可解析）。
// 不存在 => (true,nil) 可写。存在但读取/解析失败 => (false,err)（不覆盖，保旧）。存在且会话不符=> (false,nil)。
func (s *Store) existingIsOwned() (bool, error) {
	f, err := os.Open(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("hydration: 检查目标文件失败: %w", err)
	}
	defer f.Close()
	// 有界读：最多 MaxEnvelopeBytes+1，避免整份超大文件读完才判限额（F-A4）。
	raw, rerr := readBounded(f, rst.MaxEnvelopeBytes+1)
	if rerr != nil {
		return false, fmt.Errorf("hydration: 读取既有状态文件失败（不覆盖）: %w", rerr)
	}
	if len(raw) > rst.MaxEnvelopeBytes {
		return false, nil // 超大既有文件：不视为本会话，拒绝覆盖
	}
	// 只解析外层 SessionID 判定所有权（无需完整校验；完整校验由 ReadVerified 负责）。
	var e rst.Envelope
	if jerr := jsonUnmarshal(raw, &e); jerr != nil {
		return false, nil // 无法解析的既有文件：不覆盖
	}
	return e.SessionID == s.sessionID, nil
}

// readBounded 从 r 读取最多 limit 字节；超过 limit 也最多读 limit（调用方据 len>max 判定）。
func readBounded(r io.Reader, limit int) ([]byte, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < limit {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return buf, err
		}
	}
	if len(buf) > limit {
		buf = buf[:limit]
	}
	return buf, nil
}

// ReadVerified 读取本会话状态文件并按 Expect 完整校验（schema/app/session/版本/checksum/generation）。
// 文件不存在 => (nil, os.ErrNotExist 语义 via ErrNoState)。校验失败返回具体错误，不返回部分 enevelope。
func (s *Store) ReadVerified(exp rst.Expect) (*rst.Envelope, error) {
	f, err := os.Open(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoState
		}
		return nil, fmt.Errorf("hydration: 读取状态文件失败: %w", err)
	}
	defer f.Close()
	// F-A4：有界读取（最多 MaxEnvelopeBytes+1），不先整份读完再判限额。
	raw, rerr := readBounded(f, rst.MaxEnvelopeBytes+1)
	if rerr != nil {
		return nil, fmt.Errorf("hydration: 读取状态文件失败: %w", rerr)
	}
	if len(raw) > rst.MaxEnvelopeBytes {
		return nil, rst.ErrTooLarge
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
