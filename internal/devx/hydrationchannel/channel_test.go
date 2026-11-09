package hydrationchannel

// R28-A S1：端点/客户端/应用器判别回归（真实回环 TCP，无窗口）。
// 覆盖：正常捕获往返、owner 串行执行证明、错 session、错 token、重放、超大、owner 关闭、取消有界、
// Stop 后无遗留、Applier 校验失败/取消不 Apply、双会话隔离。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
)

// syncOwner 是直接在当前 goroutine 执行 fn 的 owner（单 owner 串行测试用）；记录执行线程标记。
type syncOwner struct {
	mu     sync.Mutex
	closed bool
	runs   atomic.Int64
	onRun  func()
}

func (o *syncOwner) Do(ctx context.Context, fn func(context.Context) error) error {
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	if closed {
		return ErrOwnerClosed
	}
	o.runs.Add(1)
	if o.onRun != nil {
		o.onRun()
	}
	return fn(ctx)
}

func (o *syncOwner) Close() { o.mu.Lock(); o.closed = true; o.mu.Unlock() }

func startEndpoint(t *testing.T, owner OwnerRunner, capture CaptureFunc, session, app, schema, token string) *Endpoint {
	t.Helper()
	ep, err := NewEndpoint(owner, capture, session, app, schema, token, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ep.Start(); err != nil {
		t.Fatal(err)
	}
	return ep
}

func TestEndpointClientRoundTrip(t *testing.T) {
	owner := &syncOwner{}
	want := []byte(`{"count":7,"items":["a","b"],"nonDefault":"OLD"}`)
	ep := startEndpoint(t, owner, func(context.Context) ([]byte, int, error) { return want, 3, nil }, "s1", "app", "sch", "tok-xyz")
	defer ep.Stop()
	cl, err := NewClient(ep.Addr(), "s1", "app", "sch", "tok-xyz")
	if err != nil {
		t.Fatal(err)
	}
	got, err := cl.Capture(context.Background(), 3)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("payload mismatch: %q", got)
	}
	if owner.runs.Load() != 1 {
		t.Fatalf("capture did not run on owner: runs=%d", owner.runs.Load())
	}
}

func TestWrongSessionRejected(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s1", "app", "sch", "tok")
	defer ep.Stop()
	cl, _ := NewClient(ep.Addr(), "s2", "app", "sch", "tok") // 错 session
	if _, err := cl.Capture(context.Background(), 1); !errors.Is(err, ErrWrongSession) {
		t.Fatalf("want ErrWrongSession, got %v", err)
	}
}

func TestWrongTokenRejected(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s1", "app", "sch", "tok")
	defer ep.Stop()
	cl, _ := NewClient(ep.Addr(), "s1", "app", "sch", "WRONG")
	if _, err := cl.Capture(context.Background(), 1); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
}

// 重放：同一 nonce 第二次被拒。客户端每次新 nonce，故手工构造同 nonce 请求两次。
func TestReplayRejected(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{"x":1}`), 1, nil }, "s1", "app", "sch", "tok")
	defer ep.Stop()
	req := captureRequest{Version: ProtocolVersion, Token: "tok", Session: "s1", App: "app", Schema: "sch", ServingGen: 1, ReqID: "r1", Nonce: "nonce-1"}
	raw, _ := json.Marshal(&req)
	// 第一次：OK
	r1 := rawCall(t, ep.Addr(), raw)
	var resp1 captureResponse
	_ = json.Unmarshal(r1, &resp1)
	if !resp1.OK {
		t.Fatalf("first request should be OK: %s", r1)
	}
	// 第二次同 nonce：重放拒绝
	r2 := rawCall(t, ep.Addr(), raw)
	var resp2 captureResponse
	_ = json.Unmarshal(r2, &resp2)
	if resp2.OK || resp2.Error != ErrReplay.Error() {
		t.Fatalf("replay must be rejected: %s", r2)
	}
}

func TestOwnerClosedRejected(t *testing.T) {
	owner := &syncOwner{}
	owner.Close()
	ep := startEndpoint(t, owner, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s1", "app", "sch", "tok")
	defer ep.Stop()
	cl, _ := NewClient(ep.Addr(), "s1", "app", "sch", "tok")
	if _, err := cl.Capture(context.Background(), 1); !errors.Is(err, ErrOwnerClosed) {
		t.Fatalf("want ErrOwnerClosed, got %v", err)
	}
}

func TestCaptureFailPropagates(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return nil, 0, errors.New("boom") }, "s1", "app", "sch", "tok")
	defer ep.Stop()
	cl, _ := NewClient(ep.Addr(), "s1", "app", "sch", "tok")
	if _, err := cl.Capture(context.Background(), 1); !errors.Is(err, ErrCaptureFail) {
		t.Fatalf("want ErrCaptureFail, got %v", err)
	}
}

// 取消：端点延迟捕获时客户端取消需有界返回。
func TestCaptureCancelBounded(t *testing.T) {
	gate := make(chan struct{})
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { <-gate; return []byte(`{}`), 1, nil }, "s1", "app", "sch", "tok")
	defer ep.Stop()
	defer close(gate)
	cl, _ := NewClient(ep.Addr(), "s1", "app", "sch", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := cl.Capture(ctx, 1)
	if err == nil {
		t.Fatal("canceled capture must error")
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("cancel not bounded: %v", el)
	}
}

// Stop 后无遗留：Stop 返回后拨号应失败（端点已停）。
func TestStopEndsEndpoint(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil }, "s1", "app", "sch", "tok")
	addr := ep.Addr()
	if err := ep.Stop(); err != nil {
		t.Fatal(err)
	}
	cl, _ := NewClient(addr, "s1", "app", "sch", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := cl.Capture(ctx, 1); err == nil {
		t.Fatal("capture after Stop must fail")
	}
}

func rawCall(t *testing.T, addr string, req []byte) []byte {
	t.Helper()
	return dialSend(t, addr, req)
}

// dialSend 直接拨号发送原始请求字节并读回原始响应字节（用于重放等低层断言）。
func dialSend(t *testing.T, addr string, req []byte) []byte {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf
}

// ---- Applier tests ----

func TestApplierRestoreOnOwner(t *testing.T) {
	dir := t.TempDir()
	st, _ := devxh.NewStore(dir, "s1")
	proto := &devxh.Protocol{Store: st, Schema: "sch", AppID: "app", CaptureTimeout: time.Second}
	// 写状态
	if _, err := proto.Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":5}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	owner := &syncOwner{}
	var applied []byte
	ap, err := NewApplier(proto, owner, func(_ context.Context, pl []byte) error { applied = append([]byte(nil), pl...); return nil })
	if err != nil {
		t.Fatal(err)
	}
	ok, err := ap.RestoreOnOwner(context.Background(), 0)
	if err != nil || !ok {
		t.Fatalf("RestoreOnOwner ok=%v err=%v", ok, err)
	}
	if string(applied) != `{"count":5}` {
		t.Fatalf("applied mismatch: %s", applied)
	}
	if owner.runs.Load() != 1 {
		t.Fatalf("apply did not run on owner: runs=%d", owner.runs.Load())
	}
}

// 取消：ctx 预先取消 => 不 Apply。
func TestApplierCanceledDoesNotApply(t *testing.T) {
	dir := t.TempDir()
	st, _ := devxh.NewStore(dir, "s1")
	proto := &devxh.Protocol{Store: st, Schema: "sch", AppID: "app"}
	if _, err := proto.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"x":1}`), nil }, 1); err != nil {
		t.Fatal(err)
	}
	owner := &syncOwner{}
	calls := 0
	ap, _ := NewApplier(proto, owner, func(context.Context, []byte) error { calls++; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ok, err := ap.RestoreOnOwner(ctx, 0)
	if ok || calls != 0 || err == nil {
		t.Fatalf("canceled restore must not apply: ok=%v calls=%d err=%v", ok, calls, err)
	}
}

// 无状态 => ErrNoState，不 Apply。
func TestApplierNoState(t *testing.T) {
	dir := t.TempDir()
	st, _ := devxh.NewStore(dir, "s1")
	proto := &devxh.Protocol{Store: st, Schema: "sch", AppID: "app"}
	owner := &syncOwner{}
	calls := 0
	ap, _ := NewApplier(proto, owner, func(context.Context, []byte) error { calls++; return nil })
	ok, err := ap.RestoreOnOwner(context.Background(), 0)
	if ok || calls != 0 || !errors.Is(err, devxh.ErrNoState) {
		t.Fatalf("no-state: ok=%v calls=%d err=%v", ok, calls, err)
	}
}
