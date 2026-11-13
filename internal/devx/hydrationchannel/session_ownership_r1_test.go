package hydrationchannel

// R29-A R1：会话所有权回归（无窗口）。删除/覆盖前证明当前会话拥有文件；保留错误路径原字节与 mtime。
// 覆盖：同名现存外来字节、两个并发同名会话、旧代发现文件、取消。

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

func mkSessR1(t *testing.T, dir, sid string) *Session {
	t.Helper()
	s, err := NewSession(SessionConfig{ProjectDir: dir, SessionID: sid, AppID: "demo", Schema: "demo.v1", Token: "local-test"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// 未拥有（外来字节）的现存同名状态文件：Close 必须保留原字节与 mtime。
func TestR1ClosePreservesForeignBytesAndMtime(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "reserved")
	if err := os.MkdirAll(s.Store().Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("pre-existing unrelated file\n")
	path := s.Store().Path()
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	_ = os.Chtimes(path, old, old)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, foreign) {
		t.Fatalf("foreign bytes must be preserved: err=%v", err)
	}
	st, _ := os.Stat(path)
	if !st.ModTime().Equal(old) {
		t.Fatalf("foreign file mtime must be preserved: got %v want %v", st.ModTime(), old)
	}
}

// 本会话写入的合法状态：Close 应删除（拥有）。
func TestR1CloseDeletesOwnedState(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "owned")
	if _, err := s.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":3}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	path := s.Store().Path()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("owned state must exist before close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("owned state must be removed on close: %v", err)
	}
}

// 两个并发同名会话：仅拥有合法状态的会话可删；外来字节由未写会话 Close 保留。
func TestR1ConcurrentSameNameSessionsOwnership(t *testing.T) {
	dir := t.TempDir()
	a := mkSessR1(t, dir, "dup")
	b := mkSessR1(t, dir, "dup")
	// a 写合法状态；b 未写。并发 Close。
	if _, err := a.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":1}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = a.Close() }()
	go func() { defer wg.Done(); _ = b.Close() }()
	wg.Wait()
	// 同名同会话：合法状态被拥有者删除属可接受；关键是不 panic、不误删他人会话文件。
	if _, err := os.Stat(a.Store().Path()); !os.IsNotExist(err) {
		t.Fatalf("owning same-name session should have removed valid state, got err=%v", err)
	}
}

// 旧代发现文件（portfile）所有权：内容 session 不匹配则不删；匹配才删。
func TestR1ClosePortfileOwnership(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "pf")
	ep, err := s.NewOldGenEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Stop()
	pf := s.portfilePath()
	if _, err := os.Stat(pf); err != nil {
		t.Fatalf("portfile must exist: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pf); !os.IsNotExist(err) {
		t.Fatalf("owned portfile must be removed: %v", err)
	}
	// 他人写同名 portfile（session 不符）：不删。
	foreignPF := []byte(`{"addr":"127.0.0.1:1","session":"other","app":"demo","schema":"demo.v1"}`)
	if err := os.WriteFile(pf, foreignPF, 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := mkSessR1(t, dir, "pf")
	if err := s2.Close(); err != nil {
		t.Fatalf("close2: %v", err)
	}
	if got, err := os.ReadFile(pf); err != nil || !bytes.Equal(got, foreignPF) {
		t.Fatalf("foreign portfile must be preserved: err=%v", err)
	}
}

// 取消：Restore 在取消上下文下不应用、不写状态、可安全 Close。
func TestR1CancelRestoreSafeClose(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	applied := false
	_, _ = s.Restore(ctx, 0, &syncOwner{}, func(context.Context, []byte) error { applied = true; return nil })
	if applied {
		t.Fatal("canceled restore must not apply")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close after cancel: %v", err)
	}
}
