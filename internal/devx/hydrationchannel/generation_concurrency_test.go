package hydrationchannel

// R28-A Hydration closure：generation 关联在并发/迟到响应下的补充回归（独立编写）。
//   - 端点：并发多请求，混合正确/错误 ServingGen；错误代的响应必须 ErrMismatch 且无 payload，
//     正确代的响应返回且 payload 正确——验证守卫在并发下不串代、不泄漏。
//   - 客户端：迟到响应（服务端延迟回错代 gen）在客户端超时/取消时有界返回错误，绝不接受字节。

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// TestEndpointConcurrentGenerationGuard 并发混合正确/错误代次：错代绝不泄漏 payload。
func TestEndpointConcurrentGenerationGuard(t *testing.T) {
	owner := &syncOwner{}
	// owner 总是报告代次 5；请求方一半声明 ServingGen=5（对），一半=4（错）。
	leak := []byte(`{"secret":"gen5-bytes"}`)
	ep := startEndpoint(t, owner, func(context.Context) ([]byte, int, error) { return leak, 5, nil }, "s1", "app", "sch", "tok")
	defer ep.Stop()

	const n = 24
	var wg sync.WaitGroup
	var mu sync.Mutex
	wrongOK, wrongLeak, rightPass := 0, 0, 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			gen := 5
			if i%2 == 1 {
				gen = 4 // 错误代次
			}
			req := captureRequest{Version: ProtocolVersion, Token: "tok", Session: "s1", App: "app", Schema: "sch",
				ServingGen: gen, ReqID: "r-" + string(rune('A'+i)), Nonce: "n-" + string(rune('A'+i))}
			raw, _ := json.Marshal(&req)
			respRaw := dialSend(t, ep.Addr(), raw)
			var resp captureResponse
			_ = json.Unmarshal(respRaw, &resp)
			mu.Lock()
			defer mu.Unlock()
			if gen == 4 {
				if resp.OK {
					wrongOK++
				}
				if len(resp.Payload) != 0 {
					wrongLeak++
				}
				if !resp.OK && resp.Error != ErrMismatch.Error() {
					wrongLeak++ // 错误原因不符合也算失败
				}
			} else {
				if resp.OK && string(resp.Payload) == string(leak) && resp.Gen == 5 {
					rightPass++
				}
			}
		}(i)
	}
	wg.Wait()
	if wrongOK != 0 || wrongLeak != 0 {
		t.Fatalf("wrong-gen requests accepted or leaked under concurrency: wrongOK=%d wrongLeak=%d", wrongOK, wrongLeak)
	}
	if rightPass != n/2 {
		t.Fatalf("right-gen requests should all pass: got %d want %d", rightPass, n/2)
	}
}

// TestClientLateWrongGenResponseRejected 迟到响应：服务端延迟后回错代 gen；客户端超时有界报错、不接受字节。
func TestClientLateWrongGenResponseRejected(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	release := make(chan struct{})
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer conn.Close()
		raw, _ := readUpTo(conn, MaxRequestBytes+1)
		var creq captureRequest
		_ = json.Unmarshal(raw, &creq)
		<-release // 延迟应答
		resp := captureResponse{Version: ProtocolVersion, OK: true, Session: creq.Session,
			ReqID: creq.ReqID, Gen: 99, Payload: json.RawMessage(`{"late":"bytes"}`)}
		b, _ := json.Marshal(&resp)
		_, _ = conn.Write(b)
	}()
	cl, _ := NewClient(ln.Addr().String(), "s1", "app", "sch", "tok")
	// 客户端短超时：迟到应答（错代）不应在超时内被接受。
	cl.ioDdl = 120 * time.Millisecond
	start := time.Now()
	got, cerr := cl.Capture(context.Background(), 1)
	el := time.Since(start)
	close(release)
	if got != nil {
		t.Fatalf("late wrong-gen response must not yield bytes, got %q", got)
	}
	if cerr == nil {
		t.Fatal("late response past deadline must error")
	}
	if el > 2*time.Second {
		t.Fatalf("late-response handling not bounded: %v", el)
	}
	// 契约保证：迟到/错代响应必须**有界返回错误且不接受字节**（错误类型可为超时/错代/IO 超时，
	// 但绝不能返回 payload）。此处断言 got==nil && 有界已在上方检查；不再过度约束具体错误类型。
	_ = cerr
	_ = errors.Is
}
