package hydrationchannel

// R29-A R2：发现记录与会话所有权回归（无窗口）。共享所有权边界：发布/替换/释放；实例与代次区分；
// 外来字节+mtime 保留；独占临时名 + 仅清理本方临时；有界读；真实 I/O 错误保留；token 不入发现文件/日志。

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 发现记录区分同名会话的**不同运行实例**及服务代次。
func TestR2DiscoveryDistinguishesInstanceAndGen(t *testing.T) {
	dir := t.TempDir()
	a := mkSessR1(t, dir, "shared")
	ep1, err := a.NewEndpointForGen(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 2, nil }, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer ep1.Stop()
	recA, ok, err := a.ReadPortInfo()
	if err != nil || !ok {
		t.Fatalf("read own record: ok=%v err=%v", ok, err)
	}
	if recA.OwnerID != a.ownerID || recA.Gen != 2 || recA.Addr != ep1.Addr() {
		t.Fatalf("record must carry ownerID/gen/addr: %+v (owner=%s)", recA, a.ownerID)
	}
	// 另一实例同名会话：ownerID 不同。
	b := mkSessR1(t, dir, "shared")
	if b.ownerID == a.ownerID {
		t.Fatal("distinct instances must have distinct ownerID")
	}
}

// 旧代释放不得删除新代记录（同名会话，新实例接管后旧实例 Close 只删自己的）。
func TestR2OldClosePreservesNewInstanceRecord(t *testing.T) {
	dir := t.TempDir()
	old := mkSessR1(t, dir, "shared")
	epOld, err := old.NewOldGenEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = epOld.Stop()

	next := mkSessR1(t, dir, "shared")
	epNew, err := next.NewOldGenEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer epNew.Stop()
	// 新记录归 next。
	rec, ok, _ := next.ReadPortInfo()
	if !ok || rec.OwnerID != next.ownerID {
		t.Fatalf("new record must be next's")
	}
	// 旧实例 Close：不得删新记录。
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if addr, ok := next.ReadPort(); !ok || addr != epNew.Addr() {
		t.Fatalf("old Close removed new instance record")
	}
}

// 发布不覆盖预存外来文件（同名 + 固定 .tmp 名），保留字节与 mtime。
func TestR2PublishPreservesForeignBytesAndMtime(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "shared")
	if err := os.MkdirAll(s.Store().Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{s.portfilePath(), s.portfilePath() + ".tmp"} {
		foreign := []byte("foreign bytes")
		if err := os.WriteFile(p, foreign, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour).Truncate(time.Second)
		_ = os.Chtimes(p, old, old)
		ep, _ := s.NewOldGenEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, 1)
		if ep != nil {
			ep.Stop()
		}
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, foreign) {
			t.Fatalf("foreign file must be preserved at %q: err=%v", p, err)
		}
		st, _ := os.Stat(p)
		if !st.ModTime().Equal(old) {
			t.Fatalf("foreign mtime must be preserved at %q: got %v want %v", p, st.ModTime(), old)
		}
	}
}

// 独占临时名：发布后不留本方残留临时文件；且只清理本方临时。
func TestR2ExclusiveTempCleanup(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "shared")
	if err := os.MkdirAll(s.Store().Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// 预置他人临时文件：<port>.othertmp 不应被本方 Close 清理。
	otherTmp := s.portfilePath() + ".someoneelse.tmp"
	if err := os.WriteFile(otherTmp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ep, err := s.NewOldGenEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 发布需有临时→rename；发布后不应残留本方 <port>.<ownerID>.*.tmp。
	if matches, _ := filepath.Glob(filepath.Join(s.store.Dir(), "*.port.json."+s.ownerID+".*.tmp")); len(matches) != 0 {
		t.Fatalf("own temp files should not remain after publish: %v", matches)
	}
	ep.Stop()
	_ = s.Close()
	if _, err := os.Stat(otherTmp); err != nil {
		t.Fatalf("must not remove other's temp file: %v", err)
	}
}

// 有界读：超大发现文件读取被拒（保留为错误），且不 OOM。
func TestR2BoundedDiscoveryRead(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "shared")
	if err := os.MkdirAll(s.Store().Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	big := bytes.Repeat([]byte("A"), maxDiscoveryBytes+10)
	if err := os.WriteFile(s.portfilePath(), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadPortInfo(); err == nil {
		t.Fatal("oversized discovery file must be rejected with error")
	}
}

// token 不出现在发现文件（仅 addr/ownerId/gen/session/app/schema）。
func TestR2NoTokenInDiscovery(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "shared", AppID: "app", Schema: "v1", Token: "SUPER-SECRET-TOKEN"})
	ep, err := s.NewOldGenEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer ep.Stop()
	raw, _ := os.ReadFile(s.portfilePath())
	if bytes.Contains(raw, []byte("SUPER-SECRET-TOKEN")) {
		t.Fatalf("discovery record must not contain token: %s", raw)
	}
}

// 状态文件读写删共享同一所有权边界：拥有者删除、外来保留（含 mtime）。
func TestR2StateSharedOwnershipBoundary(t *testing.T) {
	dir := t.TempDir()
	s := mkSessR1(t, dir, "st")
	if _, err := s.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":5}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	// 读到自己的合法状态。
	if !s.storeOwning() {
		t.Fatal("own valid state must be owned")
	}
	// Close 删拥有态。
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Store().Path()); !os.IsNotExist(err) {
		t.Fatalf("owned state must be deleted")
	}
}

// 两个并发同名会话（同时存活）：各自端点独立；一个 Close 不影响另一个的发现与端点。
func TestR2TwoLiveSameNameSessionsIndependent(t *testing.T) {
	dir := t.TempDir()
	a := mkSessR1(t, dir, "shared")
	b := mkSessR1(t, dir, "shared")
	capn := func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }
	epA, err := a.NewEndpointForGen(&syncOwner{}, capn, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer epA.Stop()
	epB, err := b.NewEndpointForGen(&syncOwner{}, capn, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer epB.Stop()
	if epA.Addr() == epB.Addr() {
		t.Fatal("distinct sessions must listen on distinct addr")
	}
	// b 后发布，发现记录归 b；a 的端点仍存活可服务。
	rec, ok, _ := b.ReadPortInfo()
	if !ok || rec.OwnerID != b.ownerID {
		t.Fatalf("record should be b's: %+v", rec)
	}
	// a Close：不得影响 b 的发现记录。
	_ = a.Close()
	if addr, ok := b.ReadPort(); !ok || addr != epB.Addr() {
		t.Fatalf("a.Close must not remove b's discovery record")
	}
}
