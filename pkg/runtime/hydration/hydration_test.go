package hydration

// R27-A A1：显式 DTO 协议的判别回归（纯，无 I/O）。覆盖制造/编码/校验全分类：正常、checksum 不符、
// 超大、坏 JSON、版本/schema/app/session 不符、过期代次、payload 损坏。

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type demoDTO struct {
	Count int      `json:"count"`
	Items []string `json:"items"`
}

func mk(t *testing.T, gen int, payload []byte) []byte {
	t.Helper()
	e, err := NewEnvelope("demo.counter.v1", "app-1", "sess-1", gen, time.Now(), payload)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	raw, err := e.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return raw
}

func exp() Expect {
	return Expect{Schema: "demo.counter.v1", AppID: "app-1", SessionID: "sess-1", MinGeneration: 1}
}

func TestRoundTrip(t *testing.T) {
	payload, _ := json.Marshal(demoDTO{Count: 3, Items: []string{"a", "b"}})
	raw := mk(t, 5, payload)
	e, err := DecodeAndVerify(raw, exp())
	if err != nil {
		t.Fatalf("DecodeAndVerify: %v", err)
	}
	var got demoDTO
	if err := e.DecodePayload(&got); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if got.Count != 3 || len(got.Items) != 2 {
		t.Fatalf("payload mismatch: %+v", got)
	}
}

func TestChecksumMismatch(t *testing.T) {
	payload, _ := json.Marshal(demoDTO{Count: 1})
	raw := mk(t, 1, payload)
	var e Envelope
	_ = json.Unmarshal(raw, &e)
	e.Payload = json.RawMessage(`{"count":2}`) // 篡改 payload，checksum 不变
	bad, _ := json.Marshal(&e)
	if _, err := DecodeAndVerify(bad, exp()); !errors.Is(err, ErrChecksum) {
		t.Fatalf("want ErrChecksum, got %v", err)
	}
}

func TestCorrupt(t *testing.T) {
	if _, err := DecodeAndVerify([]byte("{not json"), exp()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestVersionMismatch(t *testing.T) {
	payload, _ := json.Marshal(demoDTO{Count: 1})
	e, _ := NewEnvelope("demo.counter.v1", "app-1", "sess-1", 1, time.Now(), payload)
	e.Version = 99
	raw, _ := json.Marshal(e)
	if _, err := DecodeAndVerify(raw, exp()); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("want ErrVersionMismatch, got %v", err)
	}
}

func TestSchemaAppSessionMismatch(t *testing.T) {
	payload, _ := json.Marshal(demoDTO{Count: 1})
	raw := mk(t, 1, payload)
	if _, err := DecodeAndVerify(raw, Expect{Schema: "other", AppID: "app-1", SessionID: "sess-1"}); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("schema: got %v", err)
	}
	if _, err := DecodeAndVerify(raw, Expect{AppID: "app-2", SessionID: "sess-1"}); !errors.Is(err, ErrAppMismatch) {
		t.Fatalf("app: got %v", err)
	}
	if _, err := DecodeAndVerify(raw, Expect{AppID: "app-1", SessionID: "sess-2"}); !errors.Is(err, ErrSessionMismatch) {
		t.Fatalf("session: got %v", err)
	}
}

func TestStale(t *testing.T) {
	payload, _ := json.Marshal(demoDTO{Count: 1})
	raw := mk(t, 2, payload)
	if _, err := DecodeAndVerify(raw, Expect{MinGeneration: 5}); !errors.Is(err, ErrStale) {
		t.Fatalf("want ErrStale, got %v", err)
	}
}

func TestTooLargePayload(t *testing.T) {
	big := make([]byte, MaxPayloadBytes+1)
	if _, err := NewEnvelope("demo.counter.v1", "app-1", "sess-1", 1, time.Now(), big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge (NewEnvelope), got %v", err)
	}
}

func TestChecksumOnlyContentEquality(t *testing.T) {
	// checksum 只证明内容一致：两个相同 payload 的 checksum 相同；不同 payload 不同。
	a := ChecksumOf([]byte("x"))
	b := ChecksumOf([]byte("x"))
	c := ChecksumOf([]byte("y"))
	if a != b || a == c {
		t.Fatal("checksum not content-address only")
	}
	if len(a) != 64 {
		t.Fatalf("checksum should be sha256 hex (64), got %d", len(a))
	}
}

// 大 payload Encode/Decode 拒绝（走 Envelope 直接构造以触发 ErrTooLarge 分支）。
func TestEnvelopeLargePayloadRejected(t *testing.T) {
	e := &Envelope{Version: EnvelopeVersion, Payload: make([]byte, MaxPayloadBytes+1)}
	if _, err := e.Encode(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Encode large want ErrTooLarge, got %v", err)
	}
	if _, err := DecodeAndVerify(make([]byte, MaxEnvelopeBytes+1), Expect{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("DecodeAndVerify raw-large want ErrTooLarge, got %v", err)
	}
}

// Encode nil / oversized Envelope 分支（真实失败路径）。
func TestEncodeNilAndOversize(t *testing.T) {
	var nilEnv *Envelope
	if _, err := nilEnv.Encode(); err == nil {
		t.Fatal("nil envelope Encode must error")
	}
	big := &Envelope{Version: EnvelopeVersion, Payload: make([]byte, MaxPayloadBytes+1)}
	if _, err := big.Encode(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize payload Encode want ErrTooLarge, got %v", err)
	}
}

// DecodeAndVerify 非法 JSON 且 canonicalChecksum 失败路径 + DecodePayload 坏 payload。
func TestDecodeInvalidJSONAndBadPayload(t *testing.T) {
	// 构造一个 payload 非法 JSON 的 Envelope（直接 marshal 绕过 NewEnvelope）。
	e := map[string]any{"version": EnvelopeVersion, "schema": "s", "appId": "a", "sessionId": "x",
		"generation": 1, "payload": "not-json", "checksum": "deadbeef"}
	raw, _ := json.Marshal(e)
	if _, err := DecodeAndVerify(raw, Expect{}); !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrChecksum) {
		t.Fatalf("invalid payload want corrupt/checksum, got %v", err)
	}
	if err := (&Envelope{Payload: []byte("{bad")}).DecodePayload(&struct{}{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("bad DecodePayload want ErrCorrupt, got %v", err)
	}
	if err := (&Envelope{}).DecodePayload(&struct{}{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("empty DecodePayload want ErrCorrupt, got %v", err)
	}
}
