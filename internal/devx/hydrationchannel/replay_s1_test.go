package hydrationchannel

// R29-A S1：有界 nonce 防重放 + 会话资源边界的真实 TCP 回归。
// 复现“累计 nonce 无上限增长”→ 修复后窗口有界。以真实回环端点证明错代/重复/错会话/取消/并发/慢回调语义，
// 并比较满额拒绝 vs 窗口淘汰对**同会话重放保证**的影响。测试不写入 token/用户数据到输出（仅状态计数/错误类别）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

// mkReq 构造一个请求原始字节（token 值在测试内部，不打印）。
func mkReq(nonce, reqID string, gen int, session, app, schema, token string) []byte {
	r := captureRequest{Version: ProtocolVersion, Token: token, Session: session, App: app, Schema: schema,
		ServingGen: gen, ReqID: reqID, Nonce: nonce}
	b, _ := json.Marshal(&r)
	return b
}

func parseResp(t *testing.T, raw []byte) captureResponse {
	t.Helper()
	var r captureResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("bad response json: %v", err)
	}
	return r
}

func newReplayEndpoint(t *testing.T, cap int, policy ReplayPolicy) (*Endpoint, string) {
	t.Helper()
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) {
		return []byte(`{"v":1}`), 1, nil
	}, "s1", "app", "v1", "tok", 4)
	if err != nil {
		t.Fatal(err)
	}
	e.SetReplayBounds(cap, policy)
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	return e, addr
}

func reqResp(t *testing.T, addr string, raw []byte) captureResponse {
	return parseResp(t, dialSend(t, addr, raw))
}

// TestReplayWindowBounded_NoUnboundedGrowth 复现并验证：大量不同 nonce 后窗口占用**不超过容量**（有界）。
func TestReplayWindowBounded_NoUnboundedGrowth(t *testing.T) {
	const cap = 16
	e, addr := newReplayEndpoint(t, cap, ReplayEvictOldest)
	for i := 0; i < 200; i++ {
		resp := reqResp(t, addr, mkReq(fmt.Sprintf("n-%d", i), fmt.Sprintf("r-%d", i), 1, "s1", "app", "v1", "tok"))
		if !resp.OK {
			t.Fatalf("request %d unexpectedly failed: %q", i, resp.Error)
		}
	}
	if got := e.ReplaySize(); got > cap {
		t.Fatalf("replay window unbounded: size=%d > cap=%d", got, cap)
	}
}

// TestReplayDuplicateRejected 重复 nonce -> ErrReplay。
func TestReplayDuplicateRejected(t *testing.T) {
	_, addr := newReplayEndpoint(t, 16, ReplayEvictOldest)
	raw := mkReq("dup", "r1", 1, "s1", "app", "v1", "tok")
	if r := reqResp(t, addr, raw); !r.OK {
		t.Fatalf("first must be OK: %q", r.Error)
	}
	if r := reqResp(t, addr, raw); r.OK || r.Error != ErrReplay.Error() {
		t.Fatalf("duplicate must be ErrReplay, got ok=%v err=%q", r.OK, r.Error)
	}
}

// TestReplayPolicyComparison_SameSessionReplay 比较两策略对同会话重放的影响：
//   - EvictOldest(cap=2)：插入 n1,n2,n3(淘汰n1) 后重放 n1 -> **不再被判重放**（保护窗口过期）。
//   - RejectOnFull(cap=2)：插入 n1,n2 后第三个新 nonce -> ErrReplayWindowFull（新请求被拒，但保留窗口内重放严格）。
func TestReplayPolicyComparison_SameSessionReplay(t *testing.T) {
	// EvictOldest
	_, addrEvict := newReplayEndpoint(t, 2, ReplayEvictOldest)
	_ = reqResp(t, addrEvict, mkReq("e1", "r1", 1, "s1", "app", "v1", "tok"))
	_ = reqResp(t, addrEvict, mkReq("e2", "r2", 1, "s1", "app", "v1", "tok"))
	_ = reqResp(t, addrEvict, mkReq("e3", "r3", 1, "s1", "app", "v1", "tok")) // 淘汰 e1
	if r := reqResp(t, addrEvict, mkReq("e1", "r4", 1, "s1", "app", "v1", "tok")); !r.OK {
		t.Fatalf("EvictOldest: evicted nonce re-use should be ACCEPTED (protection expired), got err=%q", r.Error)
	}

	// RejectOnFull
	_, addrReject := newReplayEndpoint(t, 2, ReplayRejectOnFull)
	if r := reqResp(t, addrReject, mkReq("p1", "r1", 1, "s1", "app", "v1", "tok")); !r.OK {
		t.Fatalf("reject: first must OK: %q", r.Error)
	}
	if r := reqResp(t, addrReject, mkReq("p2", "r2", 1, "s1", "app", "v1", "tok")); !r.OK {
		t.Fatalf("reject: second must OK: %q", r.Error)
	}
	// 窗口已满：新 nonce p3 被拒（ErrReplayWindowFull），但 p1 的重放仍严格判 ErrReplay。
	if r := reqResp(t, addrReject, mkReq("p3", "r3", 1, "s1", "app", "v1", "tok")); r.OK || r.Error != ErrReplayWindowFull.Error() {
		t.Fatalf("RejectOnFull: full window must reject new with ErrReplayWindowFull, got ok=%v err=%q", r.OK, r.Error)
	}
	if r := reqResp(t, addrReject, mkReq("p1", "r4", 1, "s1", "app", "v1", "tok")); r.OK || r.Error != ErrReplay.Error() {
		t.Fatalf("RejectOnFull: retained nonce replay must still be ErrReplay, got ok=%v err=%q", r.OK, r.Error)
	}
}

// TestReplayWrongGenerationIsMismatch 错代：ServingGen!=捕获代 -> ErrMismatch 无 payload。
func TestReplayWrongGenerationIsMismatch(t *testing.T) {
	_, addr := newReplayEndpoint(t, 16, ReplayEvictOldest)
	r := reqResp(t, addr, mkReq("wg", "r1", 9 /*wrong*/, "s1", "app", "v1", "tok"))
	if r.OK || r.Error != ErrMismatch.Error() || len(r.Payload) != 0 {
		t.Fatalf("wrong gen must ErrMismatch no payload, got ok=%v err=%q len=%d", r.OK, r.Error, len(r.Payload))
	}
}

// TestReplayWrongSessionRejected 错会话 -> ErrWrongSession（不取走本会话 DTO）。
func TestReplayWrongSessionRejected(t *testing.T) {
	_, addr := newReplayEndpoint(t, 16, ReplayEvictOldest)
	r := reqResp(t, addr, mkReq("ws", "r1", 1, "OTHER", "app", "v1", "tok"))
	if r.OK || r.Error != ErrWrongSession.Error() {
		t.Fatalf("wrong session must ErrWrongSession, got ok=%v err=%q", r.OK, r.Error)
	}
}

// TestReplayConcurrentDistinctNoncesBounded 并发不同 nonce：全部受理且窗口仍 <= 容量（有界）。
func TestReplayConcurrentDistinctNoncesBounded(t *testing.T) {
	const cap = 32
	e, addr := newReplayEndpoint(t, cap, ReplayEvictOldest)
	var wg sync.WaitGroup
	errs := make(chan string, 128)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := reqResp(t, addr, mkReq(fmt.Sprintf("c-%d", i), fmt.Sprintf("r-%d", i), 1, "s1", "app", "v1", "tok"))
			if !r.OK {
				errs <- r.Error
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("concurrent distinct nonce rejected: %q", e)
	}
	if got := e.ReplaySize(); got > cap {
		t.Fatalf("concurrent window unbounded: %d > %d", got, cap)
	}
}

// TestReplayClientCancelBounded 客户端短超时取消：客户端有界返回错误，端点不 hang。
func TestReplayClientCancelBounded(t *testing.T) {
	gate := make(chan struct{})
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { <-gate; return []byte(`{}`), 1, nil },
		"s1", "app", "v1", "tok", 4)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(gate); _ = e.Stop() })
	c, _ := NewClient(addr, "s1", "app", "v1", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.Capture(ctx, 1); err == nil {
		t.Fatal("canceled capture must error")
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("cancel not bounded: %v", el)
	}
}

// TestReplaySlowCallbackOwnerBounded 慢回调（owner 阻塞）：短 IO deadline 下端点有界返回 capture-fail/超时，不 hang。
func TestReplaySlowCallbackOwnerBounded(t *testing.T) {
	release := make(chan struct{})
	e, err := NewEndpoint(&syncOwner{}, func(ctx context.Context) ([]byte, int, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, 0, errors.New("slow")
	}, "s1", "app", "v1", "tok", 4)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(release); _ = e.Stop() })
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second)) // 客户端上限 8s（端点自身 ioDdl=5s 内应收口）
	_, _ = conn.Write(mkReq("slow", "r1", 1, "s1", "app", "v1", "tok"))
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	start := time.Now()
	raw, _ := readUpTo(conn, MaxResponseBytes+1)
	if el := time.Since(start); el > 6*time.Second {
		t.Fatalf("slow callback not bounded (endpoint ioDdl=5s must bound): %v", el)
	}
	var r captureResponse
	_ = json.Unmarshal(raw, &r)
	if r.OK {
		t.Fatal("slow/error capture must not be OK")
	}
}
