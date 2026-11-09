package hydration

import (
	"testing"
	"time"
)

func TestCoordR27PrettyPayloadRoundTrip(t *testing.T) {
	for _, payload := range []string{`{"x":1}`, "{\n  \"x\": 1\n}", `{"x":"<tag>"}`} {
		e, err := NewEnvelope("s", "a", "session", 1, time.Unix(0, 0), []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := e.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeAndVerify(raw, Expect{Schema: "s", AppID: "a", SessionID: "session"}); err != nil {
			t.Errorf("own valid JSON roundtrip rejected: payload=%q err=%v encoded=%s", payload, err, raw)
		}
	}
}
