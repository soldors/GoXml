package hydrationchannel

// R28-A release follow-up：错代错误传播合同的**真实回环**回归（真实 Endpoint + 真实 Client，非伪端点）。
//   - 同代正控制：真实端点捕获成功且 gen==ServingGen -> 返回 payload。
//   - 错代：owner 捕获代次 != 请求 ServingGen -> 端点 ErrMismatch；客户端还原 ErrMismatch 且 payloadLen=0。
//   - 真实捕获失败正控制：owner CaptureFunc 返回 error -> 客户端 ErrCaptureFail（与 ErrMismatch 区分）。
// 现有 raw 客户端/伪端点测试不得替代本文件（使用真实 TCP 端点）。

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRealEndpointSameGenerationControl(t *testing.T) {
	want := []byte(`{"v":"same-gen"}`)
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return want, 4, nil },
		"rel-sess", "rel-app", "v1", "tok", 2)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	c, err := NewClient(addr, "rel-sess", "rel-app", "v1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.Capture(ctx, 4)
	if err != nil || string(got) != string(want) {
		t.Fatalf("same-gen control: err=%v payload=%q", err, got)
	}
}

func TestRealEndpointWrongGenerationIsErrMismatchNoPayload(t *testing.T) {
	// owner 报告代次 2；请求声明 ServingGen=1（错代）。
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) {
		return []byte(`{"leak":"must-not-return"}`), 2, nil
	}, "rel-sess", "rel-app", "v1", "tok", 2)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	c, err := NewClient(addr, "rel-sess", "rel-app", "v1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.Capture(ctx, 1)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("wrong generation must map to ErrMismatch (not ErrCaptureFail), got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("wrong generation must not return payload, got %q", got)
	}
}

func TestRealEndpointCaptureFailureIsErrCaptureFail(t *testing.T) {
	// 真实 owner 捕获失败（非错代）——必须仍是 ErrCaptureFail，不能与 ErrMismatch 混淆。
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) {
		return nil, 0, errors.New("owner capture boom")
	}, "rel-sess", "rel-app", "v1", "tok", 2)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	c, err := NewClient(addr, "rel-sess", "rel-app", "v1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.Capture(ctx, 2)
	if !errors.Is(err, ErrCaptureFail) {
		t.Fatalf("real capture failure must be ErrCaptureFail, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("capture failure must not return payload, got %q", got)
	}
}
