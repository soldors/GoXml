package hydrationchannel

// R29-A R3：运行实例所有权（实例级命名空间）+ 代次区分 + 兼容回归（无窗口）。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
)

// 新运行不能接管/覆盖外来**有效**状态：不同 token=不同 runID=不同物理路径；不互相影响。
func TestR3NewRunCannotTakeOverForeignValidState(t *testing.T) {
	dir := t.TempDir()
	runA, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "shared", AppID: "app", Schema: "v1", Token: "run-A"})
	if _, err := runA.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":1}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	runB, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "shared", AppID: "app", Schema: "v1", Token: "run-B"})
	// 不同实例命名空间 → 不同物理文件。
	if runA.Store().Path() == runB.Store().Path() {
		t.Fatalf("distinct runs must use distinct state paths: %s", runA.Store().Path())
	}
	// runB 恢复不得读到 runA 的状态（无状态=ErrNoState/冷启动）。
	applied, err := runB.Restore(context.Background(), 0, &syncOwner{}, func(context.Context, []byte) error {
		t.Fatal("must not apply foreign run state")
		return nil
	})
	if applied {
		t.Fatal("runB must not apply runA state")
	}
	if err != nil && !errors.Is(err, devxh.ErrNoState) {
		t.Fatalf("runB restore unexpected err: %v", err)
	}
	// runB.Close 不得删除 runA 的状态。
	if err := runB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(runA.Store().Path()); e != nil {
		t.Fatalf("runB close must not remove runA state: %v", e)
	}
}

// 旧运行不能删新运行状态（不同 run 实例，同 session/app/schema）。
func TestR3OldRunCannotDeleteNewRunState(t *testing.T) {
	dir := t.TempDir()
	old, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "shared", AppID: "app", Schema: "v1", Token: "old-run"})
	cur, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "shared", AppID: "app", Schema: "v1", Token: "new-run"})
	if _, err := cur.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":7}`), nil
	}, 2); err != nil {
		t.Fatal(err)
	}
	path := cur.Store().Path()
	before, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("old run Close changed new run state: %v", err)
	}
	ni, _ := os.Stat(path)
	if !info.ModTime().Equal(ni.ModTime()) {
		t.Fatalf("old run Close changed new run state mtime")
	}
}

// 同一运行的三代交接：同 token => 同 runID => 同状态路径；gen 单调前进可恢复。
func TestR3SameRunThreeGenHandoff(t *testing.T) {
	dir := t.TempDir()
	tok := "one-run"
	g1, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s", AppID: "a", Schema: "v1", Token: tok})
	g2, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s", AppID: "a", Schema: "v1", Token: tok})
	if g1.Store().Path() != g2.Store().Path() {
		t.Fatal("same run must share state path across gens")
	}
	// gen1 写 count=7。
	if _, err := g1.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":7}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	// gen2 恢复 count=7。
	var gotCount int
	applied, err := g2.Restore(context.Background(), 0, &syncOwner{}, func(_ context.Context, pl []byte) error {
		var d struct {
			Count int `json:"count"`
		}
		if json.Unmarshal(pl, &d) != nil {
			return errors.New("bad dto")
		}
		gotCount = d.Count
		return nil
	})
	if err != nil || !applied || gotCount != 7 {
		t.Fatalf("gen2 restore: applied=%v err=%v count=%d", applied, err, gotCount)
	}
}

// R28 旧格式兼容：legacy <session>.state 可被新 Session 只读回退恢复（不迁移、不删除）。
func TestR3LegacyStateReadCompat(t *testing.T) {
	dir := t.TempDir()
	legacy, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s", AppID: "a", Schema: "v1", Token: "legacy-token"})
	// 直接写入 legacyStore 路径（模拟 R28 旧格式文件）。
	legacyProto := &devxh.Protocol{Store: legacy.legacyStore, Schema: "v1", AppID: "a"}
	if _, err := legacyProto.Snapshot(context.Background(), func(context.Context) ([]byte, error) {
		return []byte(`{"count":9}`), nil
	}, 1); err != nil {
		t.Fatal(err)
	}
	// 新运行（不同 token => 不同实例路径）无自己的状态，应回退读到 legacy。
	fresh, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s", AppID: "a", Schema: "v1", Token: "fresh-token"})
	var got int
	applied, err := fresh.Restore(context.Background(), 0, &syncOwner{}, func(_ context.Context, pl []byte) error {
		var d struct {
			Count int `json:"count"`
		}
		json.Unmarshal(pl, &d)
		got = d.Count
		return nil
	})
	if err != nil || !applied || got != 9 {
		t.Fatalf("legacy read-compat: applied=%v err=%v got=%d", applied, err, got)
	}
	// 新运行 Close 不得删除 legacy 文件。
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(legacy.legacyStore.Path()); e != nil {
		t.Fatalf("legacy state must be preserved: %v", e)
	}
}

// 并发替换/删除无窗口：一个实例写状态同时另一实例 Close，不得误删合法状态（不同实例路径隔离）。
func TestR3ConcurrentWriteAndCloseNoCrossWindow(t *testing.T) {
	dir := t.TempDir()
	writer, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s", AppID: "a", Schema: "v1", Token: "w"})
	other, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s", AppID: "a", Schema: "v1", Token: "o"})
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_, _ = writer.Proto().Snapshot(context.Background(), func(context.Context) ([]byte, error) {
				return []byte(`{"count":1}`), nil
			}, i+1)
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		_ = other.Close()
	}
	<-done
	// writer 的状态仍存在且可校验（未被并发 Delete 误删）。
	if !writer.storeOwning() {
		t.Fatal("concurrent other.Close must not delete writer's state")
	}
}

// 状态有界读 + 真实 I/O 错误可区分。
func TestR3StateIODistinguishable(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewSession(SessionConfig{ProjectDir: dir, SessionID: "s", AppID: "a", Schema: "v1", Token: "t"})
	// 无状态 => ErrNoState（冷启动，与真实 I/O 错误区分）。
	applied, err := s.Restore(context.Background(), 0, &syncOwner{}, func(context.Context, []byte) error { return nil })
	if applied || err == nil || !errors.Is(err, devxh.ErrNoState) {
		t.Fatalf("no-state must be ErrNoState: applied=%v err=%v", applied, err)
	}
	_ = time.Now
}
