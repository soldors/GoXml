package hydration

// R27-A：协议/存储的**错误路径真实测试**（非镜像），覆盖 NewStore 非法输入、Encode nil、
// DecodePayload 空/坏、Restore apply 失败与冷启动、默认注入时钟/超时。

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

func TestNewStoreRejectsBadSession(t *testing.T) {
	for _, s := range []string{"", "  ", "a/b", "a\\b", "..", "."} {
		if _, err := NewStore(t.TempDir(), s); err == nil {
			t.Fatalf("session %q must be rejected", s)
		}
	}
	if _, err := NewStore("", "sess"); err == nil {
		t.Fatal("empty projectDir must be rejected")
	}
}

func TestEncodeNilEnvelope(t *testing.T) {
	var e *rst.Envelope
	if _, err := e.Encode(); !errors.Is(err, rst.ErrCorrupt) {
		t.Fatalf("nil envelope Encode want ErrCorrupt, got %v", err)
	}
}

func TestDecodePayloadEmptyAndBad(t *testing.T) {
	if err := (&rst.Envelope{}).DecodePayload(&struct{}{}); !errors.Is(err, rst.ErrCorrupt) {
		t.Fatalf("empty payload want ErrCorrupt, got %v", err)
	}
	e := &rst.Envelope{Payload: []byte("not-json")}
	var d struct{}
	if err := e.DecodePayload(&d); !errors.Is(err, rst.ErrCorrupt) {
		t.Fatalf("bad payload want ErrCorrupt, got %v", err)
	}
}

func TestRestoreNoStateAndApplyFailure(t *testing.T) {
	p := testProto(t, "sess-1")
	// 冷启动：无状态 => ErrNoState, applied=false
	applied, err := p.Restore(context.Background(), 0, func(context.Context, []byte) error { return nil })
	if applied || !errors.Is(err, ErrNoState) {
		t.Fatalf("no state: applied=%v err=%v", applied, err)
	}
	// 写入后再恢复：apply 返回错误 => 不部分应用的语义（返回错误，applied=false）
	if _, serr := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"x":1}`), nil }, 1); serr != nil {
		t.Fatal(serr)
	}
	applied, err = p.Restore(context.Background(), 0, func(context.Context, []byte) error { return errors.New("apply boom") })
	if applied || err == nil {
		t.Fatalf("apply failure must return error and applied=false; applied=%v err=%v", applied, err)
	}
	// apply nil => 错误
	if _, err := p.Restore(context.Background(), 0, nil); err == nil {
		t.Fatal("nil apply must error")
	}
}

func TestProtocolDefaults(t *testing.T) {
	p := &Protocol{}
	if p.now().IsZero() {
		t.Fatal("now() default should be time.Now (non-zero)")
	}
	if p.captureTimeout() != 2*time.Second {
		t.Fatalf("default capture timeout want 2s, got %v", p.captureTimeout())
	}
	// Snapshot with nil Store / nil capture must error.
	if _, err := p.Snapshot(context.Background(), nil, 1); err == nil {
		t.Fatal("nil Store/capture must error")
	}
}

func TestReadVerifiedWrongSessionRejected(t *testing.T) {
	dir := t.TempDir()
	a, _ := NewStore(dir, "sess-A")
	pa := &Protocol{Store: a, Schema: "demo.v1", AppID: "app-1", Now: time.Now}
	if _, err := pa.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"x":1}`), nil }, 1); err != nil {
		t.Fatal(err)
	}
	// 用错误期望会话读取同一路径（模拟跨会话误读）=> ErrSessionMismatch
	_, err := a.ReadVerified(rst.Expect{Schema: "demo.v1", AppID: "app-1", SessionID: "sess-OTHER"})
	if !errors.Is(err, rst.ErrSessionMismatch) {
		t.Fatalf("wrong session want ErrSessionMismatch, got %v", err)
	}
	_ = os.Remove(dir) // 防御：t.TempDir 自清；此处仅占位
}
