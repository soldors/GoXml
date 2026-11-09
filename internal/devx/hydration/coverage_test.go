package hydration

// R27-A：协议/存储的**错误路径真实测试**（非镜像），覆盖 NewStore 非法输入、Encode nil、
// DecodePayload 空/坏、Restore apply 失败与冷启动、默认注入时钟/超时。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// 所有权：本会话已有合法文件时允许覆盖（覆盖成功路径）；非本会话不可解析文件拒绝覆盖。
func TestStoreOwnFileOverwriteAndUnparsableReject(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir, "s1")
	p := &Protocol{Store: st, Schema: "demo.v1", AppID: "app-1"}
	if _, err := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"v":1}`), nil }, 1); err != nil {
		t.Fatal(err)
	}
	// 第二次写（既有本会话文件）应成功覆盖。
	if _, err := p.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return []byte(`{"v":2}`), nil }, 2); err != nil {
		t.Fatalf("overwrite own session file must succeed: %v", err)
	}
	// 非本会话、不可解析的既有文件：WriteAtomic 拒绝且不改字节。
	_ = os.MkdirAll(st.Dir(), 0o755)
	_ = os.WriteFile(st.Path(), []byte("not-a-json-envelope"), 0o644)
	raw, _ := rst.NewEnvelope("demo.v1", "app-1", "s1", 3, time.Now(), []byte(`{}`))
	b, _ := raw.Encode()
	if err := st.WriteAtomic(b); err == nil {
		t.Fatal("unparsable existing file must be rejected (no overwrite)")
	}
	after, _ := os.ReadFile(st.Path())
	if string(after) != "not-a-json-envelope" {
		t.Fatal("unparsable existing file bytes must be preserved")
	}
}

// ReadVerified 目标为目录（open/read 失败路径）。
func TestReadVerifiedPathIsDir(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir, "s1")
	_ = os.MkdirAll(st.Path(), 0o755) // Path 是目录
	_, err := st.ReadVerified(rst.Expect{})
	if err == nil {
		t.Fatal("ReadVerified on dir path must error")
	}
}

// WriteAtomic 失败路径：Dir 位置被普通文件占用 => MkdirAll/创建临时文件失败（真实 os 失败，非退化开关）。
func TestWriteAtomicDirIsFile(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir, "s1")
	// 把 .goxml/hydration 的父 .goxml 建成文件，使 MkdirAll 失败。
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".goxml"), []byte("x"), 0o644)
	if err := st.WriteAtomic([]byte(`{"a":1}`)); err == nil {
		t.Fatal("WriteAtomic must fail when .goxml is a file")
	}
}

// existingIsOwned：目标为目录（open 成功但读取失败 => 错误分支，不覆盖）。
func TestWriteAtomicExistingPathIsDir(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir, "s1")
	_ = os.MkdirAll(st.Path(), 0o755) // Path 是目录
	if err := st.WriteAtomic([]byte(`{"a":1}`)); err == nil {
		t.Fatal("WriteAtomic must fail when target path is a directory")
	}
}
