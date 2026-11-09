package hydration_test

// R27-A A2：真实两进程 + 状态文件的恢复整链（无窗口）。旧代设非默认 DTO → 经协议快照 → 停旧 →
// 新代（新 PID）从状态文件恢复精确值。真实 ProcessManager 进程 + 真实协议/文件；不 mock 整链。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
	"github.com/goxml/goxml/internal/devx/watch"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

type demoDTO struct {
	Count      int      `json:"count"`
	Items      []string `json:"items"`
	NonDefault string   `json:"nonDefault"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		p := filepath.Dir(dir)
		if p == dir {
			t.Fatal("go.mod not found")
		}
		dir = p
	}
}

func buildHelper(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skip helper build in -short")
	}
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "console.exe")
	cmd := exec.Command("go", "build", "-o", out, "./internal/devx/hydration/testdata/console")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, b)
	}
	return out
}

// TestTwoProcessHydrationRestore 真实两进程恢复整链。
func TestTwoProcessHydrationRestore(t *testing.T) {
	bin := buildHelper(t)
	proj := t.TempDir()
	st, err := devxh.NewStore(proj, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	proto := &devxh.Protocol{Store: st, Schema: "demo.v1", AppID: "app-1", CaptureTimeout: 5 * time.Second}

	// 旧代输入值（非默认）
	valsFile := filepath.Join(proj, "old_values.json")
	oldVals := demoDTO{Count: 7, Items: []string{"a", "b", "c"}, NonDefault: "OLD-NON-DEFAULT"}
	b, _ := json.Marshal(oldVals)
	_ = os.WriteFile(valsFile, b, 0o644)

	// 快照请求/响应文件
	reqFile := filepath.Join(proj, "snap.req")
	respFile := filepath.Join(proj, "snap.resp")
	respReady := filepath.Join(proj, "snap.ready")

	pm := watch.NewProcessManager(func(gen int) string { return bin })
	pm.StopTimeout = 5 * time.Second
	pm.Stdout = os.Stderr
	pm.Stderr = os.Stderr

	oldEnv := []string{
		"HYDRATE_VALUES=" + valsFile,
		"HYDRATE_SESSION=sess-1", "HYDRATE_SCHEMA=demo.v1", "HYDRATE_APP=app-1",
		"HYDRATE_SNAPSHOT_REQ=" + reqFile, "HYDRATE_SNAPSHOT_RESP=" + respFile, "HYDRATE_SNAPSHOT_READY=" + respReady,
	}
	// 用 EnvFor 为 gen1 注入 oldEnv。
	pm.EnvFor = func(gen int) []string {
		if gen == 1 {
			return oldEnv
		}
		// gen2：指向状态文件（恢复）
		return []string{"HYDRATE_STATE=" + st.Path(), "HYDRATE_SESSION=sess-1", "HYDRATE_SCHEMA=demo.v1", "HYDRATE_APP=app-1"}
	}
	ctx := context.Background()
	if err := pm.Swap(ctx, 1); err != nil {
		t.Fatalf("start gen1: %v", err)
	}
	oldPID := pm.Current().PID

	// Capture：写请求文件 → 等 ready → 读响应（该 DTO 为旧代当前值）。
	capture := func(c context.Context, oldGen int) ([]byte, error) {
		_ = os.Remove(respReady)
		_ = os.Remove(respFile)
		if err := os.WriteFile(reqFile, []byte("snap"), 0o644); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(respReady); err == nil {
				return os.ReadFile(respFile)
			}
			select {
			case <-c.Done():
				return nil, c.Err()
			case <-time.After(2 * time.Millisecond):
			}
		}
		return nil, fmt.Errorf("snapshot response timeout")
	}

	// 经协议快照（写入状态文件）。
	env, err := proto.Snapshot(ctx, func(c context.Context) ([]byte, error) { return capture(c, 1) }, 2)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if env.Generation != 2 {
		t.Fatalf("env gen=%d want 2", env.Generation)
	}
	// 校验快照内容为旧代非默认值
	var snapDTO demoDTO
	if err := env.DecodePayload(&snapDTO); err != nil {
		t.Fatalf("decode snap: %v", err)
	}
	if snapDTO.NonDefault != "OLD-NON-DEFAULT" || snapDTO.Count != 7 || len(snapDTO.Items) != 3 {
		t.Fatalf("snapshot did not capture old non-default DTO: %+v", snapDTO)
	}

	// 停旧启新（新代从状态文件恢复）。
	if err := pm.Swap(ctx, 2); err != nil {
		t.Fatalf("swap gen2: %v", err)
	}
	newPID := pm.Current().PID
	if newPID == oldPID {
		t.Fatalf("new PID must differ from old (%d)", oldPID)
	}

	// 独立用协议在“新代视角”读取并校验状态，断言精确恢复值（不改新代进程也可验证状态文件内容，
	// 新代进程自身也打印 RESTORED；这里以协议侧对同一文件校验为准，真实读取同一文件）。
	env2, err := st.ReadVerified(rst.Expect{Schema: "demo.v1", AppID: "app-1", SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("new-gen verify: %v", err)
	}
	var restored demoDTO
	if err := env2.DecodePayload(&restored); err != nil {
		t.Fatalf("new-gen decode: %v", err)
	}
	if restored.NonDefault != "OLD-NON-DEFAULT" || restored.Count != 7 || strings.Join(restored.Items, ",") != "a,b,c" {
		t.Fatalf("restored mismatch: %+v", restored)
	}

	_ = pm.Shutdown()
	if pm.ActiveAfter() != 0 {
		t.Fatalf("survivors after shutdown: %d", pm.ActiveAfter())
	}
}
