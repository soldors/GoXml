package hydration

// R27-A A1/A2：devx 会话协议 + 原子存储判别回归。确定性（注入时钟/通道），无 sleep 猜时序。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

type dto struct {
	Count int    `json:"count"`
	Name  string `json:"name"`
}

func testProto(t *testing.T, session string) *Protocol {
	t.Helper()
	st, err := NewStore(t.TempDir(), session)
	if err != nil {
		t.Fatal(err)
	}
	return &Protocol{Store: st, Schema: "demo.v1", AppID: "app-1", CaptureTimeout: time.Second}
}

func payloadOf(t *testing.T, d dto) []byte {
	t.Helper()
	b, _ := json.Marshal(d)
	return b
}

// Snapshot 成功后 ReadVerified + DecodePayload 精确还原。
func TestSnapshotRestoreRoundTrip(t *testing.T) {
	p := testProto(t, "sess-1")
	env, err := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return payloadOf(t, dto{Count: 7, Name: "x"}), nil
	}, 3)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if env.Generation != 3 || env.SessionID != "sess-1" {
		t.Fatalf("env meta: gen=%d sess=%s", env.Generation, env.SessionID)
	}
	var got dto
	applied, err := p.Restore(context.Background(), 0, func(_ context.Context, pl []byte) error {
		return json.Unmarshal(pl, &got)
	})
	if err != nil || !applied {
		t.Fatalf("Restore applied=%v err=%v", applied, err)
	}
	if got.Count != 7 || got.Name != "x" {
		t.Fatalf("restored mismatch: %+v", got)
	}
}

// capture 失败 => Snapshot 返回错误，不提交（保旧不切换）。无状态文件。
func TestSnapshotCaptureFailureKeepsOld(t *testing.T) {
	p := testProto(t, "sess-1")
	_, err := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return nil, errors.New("owner busy")
	}, 1)
	if err == nil {
		t.Fatal("capture failure must return error")
	}
	if _, e := os.Stat(p.Store.Path()); !os.IsNotExist(e) {
		t.Fatal("capture failure must not write state file")
	}
}

// capture 超时 => 返回错误，不提交。
func TestSnapshotTimeoutNoCommit(t *testing.T) {
	p := testProto(t, "sess-1")
	p.CaptureTimeout = 50 * time.Millisecond
	_, err := p.Snapshot(context.Background(), func(ctx context.Context) ([]byte, error) {
		<-ctx.Done() // 阻塞到超时
		return nil, ctx.Err()
	}, 1)
	if err == nil {
		t.Fatal("timeout must return error")
	}
	if _, e := os.Stat(p.Store.Path()); !os.IsNotExist(e) {
		t.Fatal("timeout must not write state file")
	}
}

// 恢复：坏 JSON / checksum / schema 不符 => 不调用 apply（不部分应用），返回具体错误。
func TestRestoreRejectsNoPartialApply(t *testing.T) {
	p := testProto(t, "sess-1")
	// 先写一个合法状态
	if _, err := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return payloadOf(t, dto{Count: 1}), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	// 篡改文件（坏 JSON）
	raw, _ := os.ReadFile(p.Store.Path())
	_ = os.WriteFile(p.Store.Path(), append(raw, []byte("garbage")...), 0o644)
	called := false
	_, err := p.Restore(context.Background(), 0, func(context.Context, []byte) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("corrupt restore must reject and NOT call apply: err=%v called=%v", err, called)
	}
}

// 版本/会话不符 => 拒绝；删除无状态 => ErrNoState（冷启动）。
func TestRestoreSessionIsolationAndNoState(t *testing.T) {
	dir := t.TempDir()
	a, _ := NewStore(dir, "sess-A")
	b, _ := NewStore(dir, "sess-B")
	pa := &Protocol{Store: a, Schema: "demo.v1", AppID: "app-1", CaptureTimeout: time.Second}
	if _, err := pa.Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return payloadOf(t, dto{Count: 9}), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	// B 会话读自己的文件 => ErrNoState（不同文件，隔离）
	_, err := b.ReadVerified(rst.Expect{})
	if !errors.Is(err, ErrNoState) {
		t.Fatalf("isolated session B should have no state, got %v", err)
	}
}

// 原子写失败：目录不可写（用只读父目录近似）不易跨平台；改为注入非法状态目录名以触发错误路径，
// 断言无半文件残留（本会话文件不存在）。
func TestWriteAtomicCleansTempOnFailure(t *testing.T) {
	p := testProto(t, "sess-1")
	// 构造一个会让 rename/写失败的场景：先创建同名目录占位 Path 使 rename 到文件失败。
	if err := os.MkdirAll(filepath.Dir(p.Store.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p.Store.Path(), 0o755); err != nil { // Path 变成目录
		t.Fatal(err)
	}
	err := p.Store.WriteAtomic([]byte(`{"version":1}`))
	if err == nil {
		t.Fatal("write to dir-as-file path must fail")
	}
	// 目录内不应残留该会话临时文件
	ents, _ := os.ReadDir(p.Store.Dir())
	for _, e := range ents {
		if len(e.Name()) >= len(".sess-1.tmp-") && e.Name()[:len(".sess-1.tmp-")] == ".sess-1.tmp-" {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}
