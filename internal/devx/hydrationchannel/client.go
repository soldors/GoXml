package hydrationchannel

// 客户端：在**Watch/worker 侧**连接旧应用端点，发送快照请求并取回不可变 DTO 字节。
// 与 Watch HydrationHook 的 CaptureOld 形状兼容（func(ctx, oldGen) ([]byte, error)），
// 只搬字节；序列化/原子提交仍由 devx/hydration.Protocol 负责。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

// Client 连接某个会话端点。
type Client struct {
	addr   string
	token  string
	dims   dims
	ioDdl  time.Duration
	reqSeq atomic.Uint64
}

// NewClient 构造客户端。addr 为端点 Start() 返回的回环地址。
func NewClient(addr, session, app, schema, token string) (*Client, error) {
	d := dims{Session: session, App: app, Schema: schema}
	if err := d.validate(); err != nil {
		return nil, err
	}
	if addr == "" || token == "" {
		return nil, errors.New("hydrationchannel: addr/token 必填")
	}
	return &Client{addr: addr, token: token, dims: d, ioDdl: DefaultIODeadline}, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Capture 请求旧代快照（oldGen 为调用方认为在服务的代次）。返回不可变 DTO 字节。
// ctx 取消/超时 => 有界返回错误；端点/owner 关闭、错会话、重放、关联不符均报错。
func (c *Client) Capture(ctx context.Context, oldGen int) ([]byte, error) {
	deadline := time.Now().Add(c.ioDdl)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	reqID := "req-" + randHex(8)
	nonce := randHex(16)
	req := captureRequest{Version: ProtocolVersion, Token: c.token, Session: c.dims.Session,
		App: c.dims.App, Schema: c.dims.Schema, ServingGen: oldGen, ReqID: reqID, Nonce: nonce}
	raw, err := json.Marshal(&req)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxRequestBytes {
		return nil, ErrTooLarge
	}

	d := net.Dialer{Deadline: deadline}
	var conn net.Conn
	// 在有界 ctx 内拨号。
	type dialRes struct {
		c   net.Conn
		err error
	}
	ch := make(chan dialRes, 1)
	go func() {
		cc, ce := d.Dial("tcp", c.addr)
		ch <- dialRes{cc, ce}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		conn = r.c
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Until(deadline)):
		return nil, context.DeadlineExceeded
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	if _, werr := conn.Write(raw); werr != nil {
		return nil, werr
	}
	// 半关写端，让端点 readUpTo 读到 EOF。
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	respRaw, rerr := readUpTo(conn, MaxResponseBytes+1)
	if rerr != nil {
		return nil, rerr
	}
	if len(respRaw) > MaxResponseBytes {
		return nil, ErrTooLarge
	}
	var resp captureResponse
	if jerr := json.Unmarshal(respRaw, &resp); jerr != nil {
		return nil, ErrBadRequest
	}
	if resp.Version != ProtocolVersion {
		return nil, ErrBadRequest
	}
	// 错误关联：reqID 与 session 必须回显一致。
	if resp.ReqID != reqID || resp.Session != c.dims.Session {
		return nil, ErrMismatch
	}
	if !resp.OK {
		switch resp.Error {
		case ErrUnauthorized.Error():
			return nil, ErrUnauthorized
		case ErrWrongSession.Error():
			return nil, ErrWrongSession
		case ErrReplay.Error():
			return nil, ErrReplay
		case ErrBusy.Error():
			return nil, ErrBusy
		case ErrOwnerClosed.Error():
			return nil, ErrOwnerClosed
		case ErrTooLarge.Error():
			return nil, ErrTooLarge
		default:
			return nil, ErrCaptureFail
		}
	}
	// 代次关联守卫（Client）：成功响应的 Gen 必须等于请求的 oldGen；不等即 ErrMismatch，
	// 不接受 payload（双重防线：即使端点被绕过，客户端也拒绝错代字节）。
	if resp.Gen != oldGen {
		return nil, ErrMismatch
	}
	if len(resp.Payload) > MaxResponseBytes {
		return nil, ErrTooLarge
	}
	return append([]byte(nil), resp.Payload...), nil
}
