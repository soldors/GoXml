package hydrationchannel

// 端点服务端：在**旧应用进程**内监听回环 TCP，响应客户端的快照请求——经 OwnerRunner 在 owner 串行点
// 调用应用提供的 CaptureFunc 取不可变 DTO，回传 {session,reqID,gen,payload}。端点**不**提供 Apply/提交
// 能力，只搬字节；token/session/nonce/generation 校验在此完成。

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// CaptureFunc 在 owner 串行点执行：返回当前活跃 DTO 的显式 JSON 字节与当前 serving 代次。
// 若无法安全捕获返回 error（端点回 capture-fail，客户端据此保旧不切换）。
type CaptureFunc func(ctx context.Context) (payload []byte, servingGen int, err error)

// Endpoint 是旧应用进程内的快照端点。
type Endpoint struct {
	owner   OwnerRunner
	capture CaptureFunc
	dims    dims
	token   string
	ioDdl   time.Duration

	mu       sync.Mutex
	ln       net.Listener
	addr     string
	closed   bool
	seen     map[string]struct{} // nonce 防重放（有界：随进程生命周期，小规模）
	inflight int
	maxInfl  int
	wg       sync.WaitGroup
}

// NewEndpoint 构造端点（不启动监听）。token 为会话绑定密钥（不写日志）。maxInflight<=0 取默认 4。
func NewEndpoint(owner OwnerRunner, capture CaptureFunc, session, app, schema, token string, maxInflight int) (*Endpoint, error) {
	d := dims{Session: session, App: app, Schema: schema}
	if err := d.validate(); err != nil {
		return nil, err
	}
	if owner == nil || capture == nil {
		return nil, errors.New("hydrationchannel: owner/capture 必填")
	}
	if token == "" {
		return nil, errors.New("hydrationchannel: token 必填")
	}
	if maxInflight <= 0 {
		maxInflight = 4
	}
	return &Endpoint{owner: owner, capture: capture, dims: d, token: token,
		ioDdl: DefaultIODeadline, seen: make(map[string]struct{}), maxInfl: maxInflight}, nil
}

// Start 在 127.0.0.1:0 上监听并返回 dial 地址（host:port）。仅回环。
func (e *Endpoint) Start() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ln != nil {
		return e.addr, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	e.ln = ln
	e.addr = ln.Addr().String()
	e.wg.Add(1)
	go e.serve()
	return e.addr, nil
}

// Addr 返回当前监听地址（未启动为空）。
func (e *Endpoint) Addr() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.addr
}

func (e *Endpoint) serve() {
	defer e.wg.Done()
	for {
		conn, err := e.ln.Accept()
		if err != nil {
			return // 关闭监听后 Accept 报错退出
		}
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			e.handle(conn)
		}()
	}
}

func (e *Endpoint) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(e.ioDdl))

	// 有界读取请求（不先整份读完才判限额）。
	raw, rerr := readUpTo(conn, MaxRequestBytes+1)
	if rerr != nil {
		e.writeErr(conn, ErrBadRequest, "", "")
		return
	}
	if len(raw) > MaxRequestBytes {
		e.writeErr(conn, ErrTooLarge, "", "")
		return
	}
	var req captureRequest
	if jerr := json.Unmarshal(raw, &req); jerr != nil {
		e.writeErr(conn, ErrBadRequest, req.ReqID, req.Session)
		return
	}
	if req.Version != ProtocolVersion {
		e.writeErr(conn, ErrBadRequest, req.ReqID, req.Session)
		return
	}
	// token 恒定时间比较（不泄露长度/内容）；不写日志。
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(e.token)) != 1 {
		e.writeErr(conn, ErrUnauthorized, req.ReqID, req.Session)
		return
	}
	// 会话/应用/schema 绑定：错会话不能取走本会话 DTO。
	if req.Session != e.dims.Session || req.App != e.dims.App || req.Schema != e.dims.Schema {
		e.writeErr(conn, ErrWrongSession, req.ReqID, req.Session)
		return
	}
	if req.ReqID == "" || req.Nonce == "" {
		e.writeErr(conn, ErrBadRequest, req.ReqID, req.Session)
		return
	}
	// 防重放：同一 nonce 只接受一次。
	e.mu.Lock()
	if _, dup := e.seen[req.Nonce]; dup {
		e.mu.Unlock()
		e.writeErr(conn, ErrReplay, req.ReqID, req.Session)
		return
	}
	if e.inflight >= e.maxInfl {
		e.mu.Unlock()
		e.writeErr(conn, ErrBusy, req.ReqID, req.Session)
		return
	}
	e.seen[req.Nonce] = struct{}{}
	e.inflight++
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.inflight--
		e.mu.Unlock()
	}()

	// 在 owner 串行点捕获（不在此 transport goroutine 直接读活动模型）。
	var payload []byte
	var gen int
	var capErr error
	rctx, cancel := context.WithTimeout(context.Background(), e.ioDdl)
	defer cancel()
	derr := e.owner.Do(rctx, func(c context.Context) error {
		payload, gen, capErr = e.capture(c)
		return nil
	})
	if errors.Is(derr, ErrOwnerClosed) {
		e.writeErr(conn, ErrOwnerClosed, req.ReqID, req.Session)
		return
	} else if derr != nil {
		e.writeErr(conn, ErrCaptureFail, req.ReqID, req.Session)
		return
	}
	if capErr != nil {
		e.writeErr(conn, ErrCaptureFail, req.ReqID, req.Session)
		return
	}
	// 代次关联守卫（Endpoint）：owner 实际捕获的代次必须等于请求方声明的 ServingGen。
	// 不等即 ErrMismatch，**绝不**返回 payload（防止旧请求拿到新代快照字节）。
	if gen != req.ServingGen {
		e.writeErr(conn, ErrMismatch, req.ReqID, req.Session)
		return
	}
	if len(payload) > MaxResponseBytes {
		e.writeErr(conn, ErrTooLarge, req.ReqID, req.Session)
		return
	}
	resp := captureResponse{Version: ProtocolVersion, OK: true, Session: e.dims.Session,
		ReqID: req.ReqID, Gen: gen, Payload: append(json.RawMessage(nil), payload...)}
	b, _ := json.Marshal(&resp)
	if len(b) > MaxResponseBytes {
		e.writeErr(conn, ErrTooLarge, req.ReqID, req.Session)
		return
	}
	_, _ = conn.Write(b)
}

func (e *Endpoint) writeErr(conn net.Conn, cause error, reqID, session string) {
	resp := captureResponse{Version: ProtocolVersion, OK: false, Error: cause.Error(),
		Session: session, ReqID: reqID}
	if b, err := json.Marshal(&resp); err == nil {
		_, _ = conn.Write(b)
	}
}

// Stop 停止监听并有界等待在途请求收口（返回后无遗留连接 goroutine）。
func (e *Endpoint) Stop() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		e.wg.Wait()
		return nil
	}
	e.closed = true
	ln := e.ln
	e.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	e.wg.Wait()
	return nil
}

// readUpTo 从 r 读取最多 limit 字节；超过则最多读 limit（调用方据 len>max 判超限）。
func readUpTo(r io.Reader, limit int) ([]byte, error) {
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for len(buf) < limit {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				return buf, nil
			}
			return buf, err
		}
	}
	if len(buf) > limit {
		buf = buf[:limit]
	}
	return buf, nil
}
