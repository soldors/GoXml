package hydrationchannel

// R28-A release follow-up：安全与资源边界检查（有界自有回环环境）。区分**已证缺陷/风险**与**已防护项**。
// 本文件为证据性回归（不改变协议/预算/API）；其中 nonce 状态增长为**已登记风险**（未在批准合同中定上限，
// 需负责人裁决是否加界）。状态文件所有权/失败保旧由 internal/devx/hydration 既有测试覆盖（见交付 handoff 引用）。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

// helper: 起一个假 TCP 服务端，对每个连接先读请求再回固定原始字节。
func fakeRawServer(t *testing.T, reply func(req []byte) []byte) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				req, _ := readUpTo(c, MaxRequestBytes+1)
				if b := reply(req); b != nil {
					_, _ = c.Write(b)
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

// 客户端对**畸形（非 JSON）响应**必须以错误返回，绝不当 payload 接受。
func TestClientRejectsMalformedResponse(t *testing.T) {
	addr := fakeRawServer(t, func([]byte) []byte { return []byte("not-a-valid-json-response") })
	c, _ := NewClient(addr, "s", "a", "v1", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := c.Capture(ctx, 1)
	if err == nil || len(got) != 0 {
		t.Fatalf("malformed response must error with no payload: err=%v len=%d", err, len(got))
	}
}

// 客户端对**超限响应**（> MaxResponseBytes）必须拒绝（有界读取后判超限，不整份恶意吃内存）。
func TestClientRejectsOversizeResponse(t *testing.T) {
	addr := fakeRawServer(t, func([]byte) []byte { return make([]byte, MaxResponseBytes+1024) })
	c, _ := NewClient(addr, "s", "a", "v1", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := c.Capture(ctx, 1)
	if err == nil {
		t.Fatalf("oversize response must error, got len=%d", len(got))
	}
	if len(got) != 0 {
		t.Fatalf("oversize response must not yield payload, len=%d", len(got))
	}
}

// 未授权半连接/异常关闭：连接后不发送即关闭、发送部分字节即关闭——端点不得 hang，后续正常请求仍可用。
func TestEndpointSurvivesHalfOpenAndAbruptClose(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{"ok":1}`), 1, nil },
		"s", "a", "v1", "tok")
	defer ep.Stop()
	// 1) 连接即关闭（无请求）
	c1, err := net.Dial("tcp", ep.Addr())
	if err != nil {
		t.Fatal(err)
	}
	_ = c1.Close()
	// 2) 发送部分字节即关闭
	c2, err := net.Dial("tcp", ep.Addr())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c2.Write([]byte(`{"ver`))
	_ = c2.Close()
	// 3) 端点仍正常服务合法请求
	cl, _ := NewClient(ep.Addr(), "s", "a", "v1", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, cerr := cl.Capture(ctx, 1)
	if cerr != nil || len(got) == 0 {
		t.Fatalf("endpoint not serving after half-open/abrupt close: err=%v len=%d", cerr, len(got))
	}
}

// 非回环地址风险：Start 只会绑定 127.0.0.1；此处验证返回地址确为回环，不监听公网/非本机接口。
func TestEndpointBindsLoopbackOnly(t *testing.T) {
	ep := startEndpoint(t, &syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil },
		"s", "a", "v1", "tok")
	defer ep.Stop()
	host, _, err := net.SplitHostPort(ep.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("endpoint must bind loopback only, got %q", host)
	}
}

// R29-A S1：nonce 防重放窗口**有界**（此前为无上限增长风险；本轮以有界候选修复）。
// 断言不同 nonce 累计请求后窗口占用**不超过容量**（bounded），不再单调无界增长。
func TestEndpointNonceWindowBounded(t *testing.T) {
	const cap = 8
	ep, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return []byte(`{}`), 1, nil },
		"s", "a", "v1", "tok", 4)
	if err != nil {
		t.Fatal(err)
	}
	ep.SetReplayBounds(cap, ReplayEvictOldest)
	if _, err := ep.Start(); err != nil {
		t.Fatal(err)
	}
	defer ep.Stop()
	const n = 64
	for i := 0; i < n; i++ {
		req := captureRequest{Version: ProtocolVersion, Token: "tok", Session: "s", App: "a", Schema: "v1",
			ServingGen: 1, ReqID: "q-" + itoa(i), Nonce: "nonce-" + itoa(i)}
		raw, _ := json.Marshal(&req)
		_ = dialSend(t, ep.Addr(), raw)
	}
	if got := ep.ReplaySize(); got > cap {
		t.Fatalf("replay window must stay bounded (<=%d), got %d", cap, got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

var _ = errors.Is
