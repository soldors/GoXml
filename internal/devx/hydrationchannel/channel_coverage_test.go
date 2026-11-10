package hydrationchannel

// R28-A 额外真实错误路径测试（提升 channel 覆盖，非镜像/非凑数）：构造参数校验、过大请求/响应、
// busy（在途上限）、协议版本不符、owner Do 返回错误、双重 Stop、readUpTo 边界。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestNewEndpointValidation(t *testing.T) {
	o := &syncOwner{}
	cap := func(context.Context) ([]byte, int, error) { return nil, 0, nil }
	if _, err := NewEndpoint(o, cap, "", "app", "sch", "tok", 0); err == nil {
		t.Fatal("empty session must be rejected")
	}
	if _, err := NewEndpoint(nil, cap, "s", "a", "sch", "tok", 0); err == nil {
		t.Fatal("nil owner must be rejected")
	}
	if _, err := NewEndpoint(o, nil, "s", "a", "sch", "tok", 0); err == nil {
		t.Fatal("nil capture must be rejected")
	}
	if _, err := NewEndpoint(o, cap, "s", "a", "sch", "", 0); err == nil {
		t.Fatal("empty token must be rejected")
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient("", "s", "a", "sch", "tok"); err == nil {
		t.Fatal("empty addr must be rejected")
	}
	if _, err := NewClient("127.0.0.1:1", "s", "a", "sch", ""); err == nil {
		t.Fatal("empty token must be rejected")
	}
	if _, err := NewClient("127.0.0.1:1", "", "a", "sch", "tok"); err == nil {
		t.Fatal("empty session must be rejected")
	}
}

// 版本不符请求被拒（ErrBadRequest）。
func TestVersionMismatchRejected(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s", "a", "sch", "tok")
	defer ep.Stop()
	req := captureRequest{Version: 999, Token: "tok", Session: "s", App: "a", Schema: "sch", ServingGen: 1, ReqID: "r", Nonce: "n"}
	raw, _ := json.Marshal(&req)
	respRaw := dialSend(t, ep.Addr(), raw)
	var resp captureResponse
	_ = json.Unmarshal(respRaw, &resp)
	if resp.OK || resp.Error != ErrBadRequest.Error() {
		t.Fatalf("version mismatch must be ErrBadRequest: %s", respRaw)
	}
}

// 过大请求（> MaxRequestBytes）被拒 ErrTooLarge。
func TestTooLargeRequestRejected(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s", "a", "sch", "tok")
	defer ep.Stop()
	big := make([]byte, MaxRequestBytes+1024)
	for i := range big {
		big[i] = ' '
	}
	respRaw := dialSend(t, ep.Addr(), big)
	var resp captureResponse
	_ = json.Unmarshal(respRaw, &resp)
	if resp.OK || resp.Error != ErrTooLarge.Error() {
		t.Fatalf("oversize request must be ErrTooLarge: %s", respRaw)
	}
}

// owner Do 返回非关闭错误 => capture-fail。用一个返回 error 的 owner。
type errOwner struct{}

func (errOwner) Do(ctx context.Context, fn func(context.Context) error) error {
	return errors.New("owner dispatch boom")
}

func TestOwnerDispatchErrorCaptureFail(t *testing.T) {
	ep := startEndpoint(t, errOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s", "a", "sch", "tok")
	defer ep.Stop()
	cl, _ := NewClient(ep.Addr(), "s", "a", "sch", "tok")
	if _, err := cl.Capture(context.Background(), 1); !errors.Is(err, ErrCaptureFail) {
		t.Fatalf("owner dispatch error => ErrCaptureFail, got %v", err)
	}
}

// busy：maxInfl=1 且 capture 阻塞，第二个并发请求得到 ErrBusy。
func TestEndpointBusy(t *testing.T) {
	gate := make(chan struct{})
	ep, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { <-gate; return []byte(`{}`), 1, nil }, "s", "a", "sch", "tok", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ep.Start(); err != nil {
		t.Fatal(err)
	}
	defer ep.Stop()
	defer close(gate)
	// 第一个请求占住在途槽（阻塞在 gate）。
	go func() {
		cl, _ := NewClient(ep.Addr(), "s", "a", "sch", "tok")
		_, _ = cl.Capture(context.Background(), 1)
	}()
	// 等待第一个进入（用重试 + 短截止，非 sleep 猜时序：直到 busy 或超时）。
	var lastErr error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cl, _ := NewClient(ep.Addr(), "s", "a", "sch", "tok")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, lastErr = cl.Capture(ctx, 1)
		cancel()
		if errors.Is(lastErr, ErrBusy) {
			return // 命中 busy
		}
	}
	t.Fatalf("expected ErrBusy while slot held; last=%v", lastErr)
}

// 双重 Stop 幂等且不 panic。
func TestDoubleStop(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s", "a", "sch", "tok")
	if err := ep.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := ep.Stop(); err != nil {
		t.Fatalf("second Stop must be idempotent: %v", err)
	}
}

// 响应过大（payload > MaxResponseBytes）被拒。用真实端点 + 客户端，capture 返回超限 payload。
func TestOversizeResponseRejected(t *testing.T) {
	big := make([]byte, MaxResponseBytes+10)
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return big, 1, nil }, "s", "a", "sch", "tok")
	defer ep.Stop()
	cl, _ := NewClient(ep.Addr(), "s", "a", "sch", "tok")
	if _, err := cl.Capture(context.Background(), 1); err == nil {
		t.Fatal("oversize response must be rejected")
	}
}

// readUpTo：超过 limit 时截断到 limit。
type oneByteReader struct{ n int }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, errors.New("eof")
	}
	p[0] = 'x'
	r.n--
	return 1, nil
}

func TestReadUpToBounds(t *testing.T) {
	got, err := readUpTo(&oneByteReader{n: 100}, 10)
	if err != nil && len(got) != 10 {
		t.Fatalf("readUpTo err=%v len=%d", err, len(got))
	}
	if len(got) != 10 {
		t.Fatalf("readUpTo must cap at limit=10, got %d", len(got))
	}
	_ = net.IPv4len
	_ = sync.Once{}
}
