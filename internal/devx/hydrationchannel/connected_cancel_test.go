package hydrationchannel

// R28-A1 已连接取消：真实回环、不依赖短 context deadline 的显式 cancel 回归。
//   - TestConnectedCancelNoResponseReturnsEarly：对端（端点捕获）永不返回；客户端连上并发送后，只触发
//     显式 cancel（握手信号同步，墙钟仅作上限）就必须提前收口，返回 context.Canceled 且无 payload。
//   - TestConnectedCancelAfterReleaseRejectsPayload：捕获进入后先 cancel 再放行同代响应；取消已发生 => 不交出 payload。
//   - 正控制：未取消的正常捕获仍返回 payload（防误杀正常路径）。

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// 对端永不返回：捕获阻塞在 handshake 上；客户端连上后仅 cancel（无短 deadline），必须提前收口。
func TestConnectedCancelNoResponseReturnsEarly(t *testing.T) {
	entered := make(chan struct{})
	never := make(chan struct{}) // 永不关闭，模拟对端不返回
	var once sync.Once
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) {
		once.Do(func() { close(entered) })
		<-never
		return []byte(`{"never":"should-not-arrive"}`), 1, nil
	}, "cancel-session", "cancel-app", "v1", "tok", 1)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(never); _ = e.Stop() })
	c, err := NewClient(addr, "cancel-session", "cancel-app", "v1", "tok")
	if err != nil {
		t.Fatal(err)
	}

	// 长 deadline（10s）——**不**用短 deadline 代替 cancel。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		payload []byte
		err     error
	}
	done := make(chan result, 1)
	go func() {
		b, err := c.Capture(ctx, 1)
		done <- result{b, err}
	}()
	// 握手：等捕获真正进入（说明连接已建立并发出请求）。
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("capture never reached endpoint (handshake timeout)")
	}
	// 仅显式 cancel（对端仍阻塞不返回）。
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", got.err)
		}
		if len(got.payload) != 0 {
			t.Fatalf("canceled capture must not yield payload, got len=%d", len(got.payload))
		}
	case <-time.After(2 * time.Second): // 墙钟仅作上限
		t.Fatal("connected capture did not return early after explicit cancel (peer never responded)")
	}
}

// 取消已发生后再放行同代响应：不交出 payload。
func TestConnectedCancelAfterReleaseRejectsPayload(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) {
		close(entered)
		<-release
		return []byte(`{"value":"arrived-after-cancel"}`), 1, nil
	}, "cancel-session", "cancel-app", "v1", "tok", 1)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	c, err := NewClient(addr, "cancel-session", "cancel-app", "v1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		payload []byte
		err     error
	}
	done := make(chan result, 1)
	go func() {
		b, err := c.Capture(ctx, 1)
		done <- result{b, err}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("capture never reached endpoint")
	}
	cancel()
	unblock()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || len(got.payload) != 0 {
			t.Fatalf("want context.Canceled and no payload, got err=%v payloadLen=%d", got.err, len(got.payload))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not return promptly after cancel+release")
	}
}

// 正控制：未取消的正常捕获返回 payload（保证修复不误杀正常路径）。
func TestConnectedCaptureNormalUnaffected(t *testing.T) {
	want := []byte(`{"ok":"normal"}`)
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return want, 7, nil },
		"cancel-session", "cancel-app", "v1", "tok", 1)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	c, _ := NewClient(addr, "cancel-session", "cancel-app", "v1", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.Capture(ctx, 7)
	if err != nil || string(got) != string(want) {
		t.Fatalf("normal capture must succeed: err=%v payload=%q", err, got)
	}
}
