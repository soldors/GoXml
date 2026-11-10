package hydrationchannel

// R28-A generation-correlation closure：两道独立守卫的判别回归（独立编写，验证合同）。
//   - Endpoint：owner 实际捕获代次 != request.ServingGen => ErrMismatch，绝不返回 payload。
//   - Client：response.Gen != oldGen => ErrMismatch，不接受 payload。
// 用真实回环 TCP；Endpoint 侧用原始请求直连检查响应 payload 缺失（绕开 Client 以免双重守卫混淆）。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// TestEndpointRejectsServingGenerationMismatch 端点：请求 ServingGen=1，但 owner 捕获代次=2 => 拒绝。
func TestEndpointRejectsServingGenerationMismatch(t *testing.T) {
	owner := &syncOwner{}
	// owner 报告实际代次 2，但请求方声明 ServingGen=1。
	ep := startEndpoint(t, owner, func(context.Context) ([]byte, int, error) {
		return []byte(`{"leak":"must-not-return"}`), 2, nil
	}, "s1", "app", "sch", "tok")
	defer ep.Stop()

	req := captureRequest{Version: ProtocolVersion, Token: "tok", Session: "s1", App: "app", Schema: "sch",
		ServingGen: 1, ReqID: "req-1", Nonce: "nonce-1"}
	raw, _ := json.Marshal(&req)
	respRaw := dialSend(t, ep.Addr(), raw)
	var resp captureResponse
	if err := json.Unmarshal(respRaw, &resp); err != nil {
		t.Fatalf("bad response: %v %s", err, respRaw)
	}
	if resp.OK {
		t.Fatalf("endpoint returned OK on generation mismatch: %s", respRaw)
	}
	if resp.Error != ErrMismatch.Error() {
		t.Fatalf("want ErrMismatch, got %q", resp.Error)
	}
	if len(resp.Payload) != 0 {
		t.Fatalf("endpoint leaked payload on mismatch: %s", resp.Payload)
	}
}

// TestEndpointMatchingGenerationReturnsPayload 正控制：代次一致才返回 payload。
func TestEndpointMatchingGenerationReturnsPayload(t *testing.T) {
	owner := &syncOwner{}
	want := []byte(`{"ok":true}`)
	ep := startEndpoint(t, owner, func(context.Context) ([]byte, int, error) { return want, 3, nil }, "s1", "app", "sch", "tok")
	defer ep.Stop()
	req := captureRequest{Version: ProtocolVersion, Token: "tok", Session: "s1", App: "app", Schema: "sch",
		ServingGen: 3, ReqID: "req-2", Nonce: "nonce-2"}
	raw, _ := json.Marshal(&req)
	respRaw := dialSend(t, ep.Addr(), raw)
	var resp captureResponse
	_ = json.Unmarshal(respRaw, &resp)
	if !resp.OK || string(resp.Payload) != string(want) || resp.Gen != 3 {
		t.Fatalf("matching generation must return payload: %s", respRaw)
	}
}

// fakeServer 是一个最小 TCP 服务，返回固定 captureResponse（用于独立测 Client 守卫）。
func fakeServer(t *testing.T, resp captureResponse) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			once.Do(func() {}) // keep
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				_, _ = readUpTo(c, MaxRequestBytes+1) // drain request
				b, _ := json.Marshal(&resp)
				_, _ = c.Write(b)
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// TestClientRejectsResponseGenerationMismatch 客户端：oldGen=1，但响应 Gen=2 => ErrMismatch，不接受 payload。
func TestClientRejectsResponseGenerationMismatch(t *testing.T) {
	// 用一个会回显请求 reqID 的假服务：先读取请求解析 reqID，再回 Gen=2 的 OK。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		raw, _ := readUpTo(conn, MaxRequestBytes+1)
		var creq captureRequest
		_ = json.Unmarshal(raw, &creq)
		resp := captureResponse{Version: ProtocolVersion, OK: true, Session: creq.Session,
			ReqID: creq.ReqID, Gen: 2, Payload: json.RawMessage(`{"leak":"must-not-accept"}`)}
		b, _ := json.Marshal(&resp)
		_, _ = conn.Write(b)
	}()
	cl, _ := NewClient(ln.Addr().String(), "s1", "app", "sch", "tok")
	_, cerr := cl.Capture(context.Background(), 1) // oldGen=1, server returns Gen=2
	if !errors.Is(cerr, ErrMismatch) {
		t.Fatalf("client must reject wrong-gen response with ErrMismatch, got %v", cerr)
	}
}
