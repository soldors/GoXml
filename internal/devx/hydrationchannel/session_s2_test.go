package hydrationchannel

// R29-A S2：应用接入（Session）可证明性：状态文件归属/清理、portfile 会话绑定、owner 串行 Restore。
// 仅用本测试自有目录/会话；断言不写 token/user 数据到输出。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type sdemo struct {
	Count int `json:"count"`
}

func TestSessionStateFileOwnershipAndCleanup(t *testing.T) {
	dir := t.TempDir()
	sess, err := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s2", AppID: "app", Schema: "v1", Token: "tok", CaptureTimeout: 0})
	if err != nil {
		t.Fatal(err)
	}
	// 旧代端点在 owner 串行点捕获；此处直接构造一个端点并写状态（经 Protocol.Snapshot）。
	if _, serr := sess.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":7}`), nil
	}, 1); serr != nil {
		t.Fatal(serr)
	}
	statePath := sess.Store().Path()
	if _, e := os.Stat(statePath); e != nil {
		t.Fatalf("state file not written: %v", e)
	}
	// 他会话文件（同目录不同名）不得被本会话 Close 删除。
	other, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "other", AppID: "app", Schema: "v1", Token: "tok"})
	if _, e := other.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"count":1}`), nil }, 1); e != nil {
		t.Fatal(e)
	}
	otherPath := other.Store().Path()

	// Close 仅删本会话文件，保留他会话。
	if e := sess.Close(); e != nil {
		t.Fatalf("close: %v", e)
	}
	if _, e := os.Stat(statePath); !os.IsNotExist(e) {
		t.Fatalf("session close must remove own state file, still exists err=%v", e)
	}
	if _, e := os.Stat(otherPath); e != nil {
		t.Fatalf("session close must NOT remove other session file: %v", e)
	}
}

func TestSessionPortfileBinding(t *testing.T) {
	dir := t.TempDir()
	sess, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s2", AppID: "app", Schema: "v1", Token: "tok"})
	ep, err := sess.NewOldGenEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Stop()
	addr, ok := sess.ReadPort()
	if !ok || addr != ep.Addr() {
		t.Fatalf("portfile binding mismatch: addr=%q ok=%v ep=%q", addr, ok, ep.Addr())
	}
	// portfile 不得含 token。
	raw, _ := os.ReadFile(sess.portfilePath())
	if string(raw) == "" || contains(string(raw), "tok") {
		t.Fatalf("portfile must not contain token; content=%s", raw)
	}
	// 不同会话读同目录 portfile 不匹配。
	other, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "other", AppID: "app", Schema: "v1", Token: "tok"})
	if _, ok := other.ReadPort(); ok {
		t.Fatal("wrong session must not read this session's portfile")
	}
}

func TestSessionRestoreOnOwnerApplies(t *testing.T) {
	dir := t.TempDir()
	sess, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s2", AppID: "app", Schema: "v1", Token: "tok"})
	if _, e := sess.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":9}`), nil
	}, 1); e != nil {
		t.Fatal(e)
	}
	var applied sdemo
	ok, err := sess.Restore(context.Background(), 0, &syncOwner{}, func(_ context.Context, pl []byte) error {
		return json.Unmarshal(pl, &applied)
	})
	if err != nil || !ok || applied.Count != 9 {
		t.Fatalf("restore: ok=%v err=%v applied=%+v", ok, err, applied)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ = filepath.Join
