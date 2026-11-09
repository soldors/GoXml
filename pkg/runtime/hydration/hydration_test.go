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
