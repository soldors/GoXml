package hydrationchannel

// R29-A R4 判别性回归测试（交付夹具）：
//   1) 同名会话、不同 token 的两个运行 → 发现文件按 runID 隔离，互不覆盖；
//   2) Close 只删本实例记录，不删另一运行的记录；
//   3) 无 runID 文件的 R28 旧格式发现记录（含 addr、无 ownerId）只读兼容可被发现；
//   4) storeOwning 用有界读取：超限/外来文件不拥有且保留原字节与 mtime；不存在返回 false 不报错。
// 这些在修复前的旧实现上会失败（旧版共享 <session>.port.json；旧版 os.ReadFile 无界）。

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newRunSession(t *testing.T, dir, token string) *Session {
	t.Helper()
	s, err := NewSession(SessionConfig{ProjectDir: dir, SessionID: "shared", AppID: "app", Schema: "v1", Token: token})
	if err != nil {
		t.Fatalf("NewSession(%s): %v", token, err)
	}
	return s
}

func TestR29R4DiscoveryFilesArePerRunInstance(t *testing.T) {
	dir := t.TempDir()
	a := newRunSession(t, dir, "token-A")
	b := newRunSession(t, dir, "token-B")

	if a.portfilePath() == b.portfilePath() {
		t.Fatalf("不同 token 的运行不应共享发现文件: %s", a.portfilePath())
	}
	if err := a.writePortfile("127.0.0.1:1001", 1); err != nil {
		t.Fatalf("A 发布失败: %v", err)
	}
	if err := b.writePortfile("127.0.0.1:1002", 1); err != nil {
		t.Fatalf("B 发布失败: %v", err)
	}
	ra, ok, err := a.ReadPortInfo()
	if err != nil || !ok || ra.Addr != "127.0.0.1:1001" {
		t.Fatalf("A 应读回自己的端点: ok=%v err=%v rec=%+v", ok, err, ra)
	}
	rb, ok, err := b.ReadPortInfo()
	if err != nil || !ok || rb.Addr != "127.0.0.1:1002" {
		t.Fatalf("B 应读回自己的端点: ok=%v err=%v rec=%+v", ok, err, rb)
	}

	// Close(A) 不得删除 B 的发现记录。
	if err := a.Close(); err != nil {
		t.Fatalf("A Close: %v", err)
	}
	if _, ok, _ := b.ReadPortInfo(); !ok {
		t.Fatalf("A 关闭后 B 的发现记录被误删（应保留）")
	}
}

func TestR29R4LegacyDiscoveryReadOnlyFallback(t *testing.T) {
	dir := t.TempDir()
	s := newRunSession(t, dir, "token-legacy")
	if err := os.MkdirAll(s.store.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := s.legacyPortfilePath()
	// R28 旧格式：含会话/应用/schema/addr，但无 ownerId/gen。
	body := []byte(`{"addr":"127.0.0.1:1900","session":"shared","app":"app","schema":"v1"}`)
	if err := os.WriteFile(legacy, body, 0o644); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.ReadPortInfo()
	if err != nil || !ok || rec.Addr != "127.0.0.1:1900" {
		t.Fatalf("应只读回退发现 R28 旧记录: ok=%v err=%v rec=%+v", ok, err, rec)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, statErr := os.Stat(legacy); statErr != nil {
		t.Fatalf("R28 旧发现文件必须保留（不被删除）: %v", statErr)
	}
}

func TestR29R4StoreOwningBoundedPreservesForeignAndOversize(t *testing.T) {
	dir := t.TempDir()
	s := newRunSession(t, dir, "token-own")
	if err := os.MkdirAll(s.store.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := s.store.Path()

	// 不存在 => 不拥有，且无错误。
	if s.storeOwning() {
		t.Fatalf("缺失状态文件不应判定为拥有")
	}

	// 外来字节（非本会话合法 envelope）：不拥有、保留原字节与 mtime。
	foreign := []byte(`{"not":"our-envelope"}`)
	if err := os.WriteFile(path, foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("设置 mtime: %v", err)
	}
	before, _ := os.Stat(path)
	if s.storeOwning() {
		t.Fatalf("外来文件不应判定为拥有")
	}
	after, _ := os.Stat(path)
	got, _ := os.ReadFile(path)
	if string(got) != string(foreign) {
		t.Fatalf("外来文件内容被改动")
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("外来文件 mtime 被改动: %v -> %v", before.ModTime(), after.ModTime())
	}

	// 超限文件（> MaxEnvelopeBytes）：不整份读入判为不拥有，保留。
	// 这里用显著大于 64KiB 的外来块（bounded reader 上限大于 64KiB 的 State 由 MaxEnvelopeBytes 判定）。
	big := make([]byte, 128<<10)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(path, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if s.storeOwning() {
		t.Fatalf("超限/坏文件不应判定为拥有")
	}
	if fi, _ := os.Stat(path); fi.Size() != int64(len(big)) {
		t.Fatalf("超限文件应被保留原大小")
	}
}

// 用真实 loopback 证明：两个运行各自 Capture 命中自己的端点（端到端，不仅读文件）。
func TestR29R4TwoRunsCaptureOwnEndpointOverLoopback(t *testing.T) {
	dir := t.TempDir()
	a := newRunSession(t, dir, "token-loop-A")
	b := newRunSession(t, dir, "token-loop-B")
	start := func(s *Session, payload string) *Endpoint {
		ep, err := s.NewEndpointForGen(&syncOwner{}, func(context.Context) ([]byte, int, error) {
			return []byte(payload), 1, nil
		}, 1, 1)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		t.Cleanup(func() { _ = ep.Stop() })
		return ep
	}
	start(a, `{"run":"A"}`)
	start(b, `{"run":"B"}`)
	check := func(s *Session, token, want string) {
		t.Helper()
		cl, ok := s.NewClientFromPort(token)
		if !ok {
			t.Fatalf("未发现本运行端点")
		}
		got, err := cl.Capture(context.Background(), 1)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if string(got) != want {
			t.Fatalf("命中错误端点: want=%s got=%s", want, got)
		}
	}
	check(a, "token-loop-A", `{"run":"A"}`)
	check(b, "token-loop-B", `{"run":"B"}`)
	check(a, "token-loop-A", `{"run":"A"}`)
}

var _ = filepath.Join
