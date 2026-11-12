package hydrationchannel

// R28-A1：**有效 JSON 超限**响应的确定性拒绝证明（不以畸形/全零响应代替）。
// 服务端返回一个结构良好、但 Payload 为合法 JSON 大值时总字节超过 MaxResponseBytes 的响应；
// 客户端必须在解析前按大小上限拒绝，返回确定错误类别 ErrTooLarge，且不交出 payload。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

func TestClientRejectsOversizeValidJSONResponse(t *testing.T) {
	// 构造一份**合法 JSON** 的 captureResponse，其 payload 为合法 JSON 字符串大值，令总字节 > MaxResponseBytes。
	bigValue := make([]byte, MaxResponseBytes) // 合法 JSON 字符串内容（ASCII 空间 + 结尾引号）
	for i := range bigValue {
		bigValue[i] = 'a'
	}
	payload := append(append([]byte(`"`), bigValue...), '"') // 合法 JSON 字符串值
	resp := captureResponse{
		Version: ProtocolVersion, OK: true, Session: "s", ReqID: "will-be-overridden",
		Gen: 1, Payload: json.RawMessage(payload),
	}
	// 需回显请求 reqID；服务端先读请求解析 reqID 再回带该 reqID 的响应（仍保持合法 JSON + 超限）。
	addr := fakeEchoingServer(t, func(creq captureRequest) []byte {
		resp.ReqID = creq.ReqID
		resp.Session = creq.Session
		b, _ := json.Marshal(&resp)
		return b // 合法 JSON、总字节 > MaxResponseBytes
	})
	c, _ := NewClient(addr, "s", "a", "v1", "tok")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := c.Capture(ctx, 1)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize valid-JSON response must be ErrTooLarge, got err=%v payloadLen=%d", err, len(got))
	}
	if len(got) != 0 {
		t.Fatalf("oversize response must not yield payload, len=%d", len(got))
	}
}

// fakeEchoingServer 起一个假 TCP 服务端：读请求->解析 captureRequest->用回调生成响应字节回写。
func fakeEchoingServer(t *testing.T, reply func(creq captureRequest) []byte) string {
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
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				raw, _ := readUpTo(c, MaxRequestBytes+1)
				var creq captureRequest
				_ = json.Unmarshal(raw, &creq)
				b := reply(creq)
				if b != nil {
					_, _ = c.Write(b)
				}
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}
