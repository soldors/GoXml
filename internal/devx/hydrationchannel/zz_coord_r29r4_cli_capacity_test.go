package hydrationchannel_test

// R29-A R4 · item3：真实应用进程（exampleapp 二进制）+ 自有端点 + 固定小容量（cap=2）
// 实际填满 nonce 预算，证明三种策略区别：
//   默认(reject) / 显式 reject：窗口满后新 nonce 被拒（ErrReplayWindowFull），窗口内重复仍 ErrReplay；
//   evict：窗口满后新 nonce 淘汰最旧并接受（size 保持 cap），被淘汰的旧 nonce 重复不再判重放。
// 走真实 loopback TCP（raw 请求 JSON），token 只经子进程环境，不写日志/不打印。

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rawCapture 发送一条显式 JSON 捕获请求到 addr，返回响应体（ok/error/gen/payload）。
func rawCapture(t *testing.T, addr, token, session, app, schema, nonce, reqID string, gen int) map[string]any {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	body := map[string]any{
		"version": 1, "token": token, "session": session, "app": app, "schema": schema,
		"servingGen": gen, "reqId": reqID, "nonce": nonce,
	}
	b, _ := json.Marshal(body)
	if _, err := conn.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	rb, _ := io.ReadAll(conn)
	var resp map[string]any
	if err := json.Unmarshal(rb, &resp); err != nil {
		t.Fatalf("bad resp %q: %v", string(rb), err)
	}
	return resp
}

// waitPort 等待 exampleapp 写出发现文件，返回 addr。
func waitPort(t *testing.T, proj, session, app, schema string) string {
	t.Helper()
	dir := filepath.Join(proj, ".goxml", "hydration")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ms, _ := filepath.Glob(filepath.Join(dir, "*.port.json"))
		for _, m := range ms {
			b, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			var rec struct {
				Addr, Session, App, Schema string
			}
			if json.Unmarshal(b, &rec) == nil && rec.Addr != "" &&
				rec.Session == session && rec.App == app && rec.Schema == schema {
				return rec.Addr
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("超时未发现 exampleapp 端点（%s）", dir)
	return ""
}

// launchExample 启动 exampleapp gen1（自报 report），返回地址与清理函数。
func launchExample(t *testing.T, bin, policyEnv string, cap int) (string, func()) {
	t.Helper()
	proj := t.TempDir()
	report := filepath.Join(t.TempDir(), "report.json")
	env := append(os.Environ(),
		"HYD_PROJECT="+proj, "HYD_SESSION=sess", "HYD_APP=app", "HYD_SCHEMA=v1",
		"HYD_TOKEN=cap-token", "HYD_GEN=1", fmt.Sprintf("HYD_REPLAY_CAP=%d", cap), "HYD_REPORT="+report,
	)
	if policyEnv != "" {
		env = append(env, "HYD_REPLAY_POLICY="+policyEnv)
	}
	cmd := exec.Command(bin)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start exampleapp: %v", err)
	}
	stop := func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }
	addr := waitPort(t, proj, "sess", "app", "v1")
	return addr, stop
}

func TestR29R4RealAppReplayCapacityBoundary(t *testing.T) {
	bin := buildExampleApp(t)

	const (
		sess   = "sess"
		app    = "app"
		schema = "v1"
		token  = "cap-token"
	)
	errWindowFull := "hydrationchannel: nonce 防重放窗口已满（拒绝新请求）"
	errReplay := "hydrationchannel: 重复请求（nonce 已见）"

	// 三种策略：默认(不设 policy，exampleapp 默认 reject) / 显式 reject / evict。
	cases := []struct {
		name      string
		policyEnv string
		accept3rd bool // 是否接受第三个新 nonce
	}{
		{"default-reject", "", false},
		{"explicit-reject", "reject", false},
		{"evict", "evict", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			addr, stop := launchExample(t, bin, c.policyEnv, 2)
			defer stop()

			// 填满预算：n1、n2 均成功。
			r1 := rawCapture(t, addr, token, sess, app, schema, "n1", "r1", 1)
			if r1["ok"] != true {
				t.Fatalf("n1 应成功: %v", r1)
			}
			r2 := rawCapture(t, addr, token, sess, app, schema, "n2", "r2", 1)
			if r2["ok"] != true {
				t.Fatalf("n2 应成功: %v", r2)
			}

			// 第三个新 nonce：reject 家族应被窗口满拒绝；evict 应接受（淘汰 n1）。
			r3 := rawCapture(t, addr, token, sess, app, schema, "n3", "r3", 1)
			if c.accept3rd {
				if r3["ok"] != true {
					t.Fatalf("[%s] evict 应接受第三个新 nonce: %v", c.name, r3)
				}
			} else {
				if r3["ok"] == true || r3["error"] != errWindowFull {
					t.Fatalf("[%s] 满额应拒绝新 nonce(ErrReplayWindowFull): %v", c.name, r3)
				}
				// reject 家族：窗口内重复 n1 仍严格判重放。
				rr := rawCapture(t, addr, token, sess, app, schema, "n1", "r1b", 1)
				if rr["ok"] == true || rr["error"] != errReplay {
					t.Fatalf("[%s] 窗口内重复 n1 应 ErrReplay: %v", c.name, rr)
				}
			}

			if c.accept3rd {
				// evict：n3 插入后窗口={n2,n3}，n1 已被淘汰。
				// 先验窗口内 n2 重复仍 ErrReplay（重复不插入，窗口不变）；
				// 再验被淘汰的 n1 重复不再判重放（ok，插入时淘汰 n2）。
				rr2 := rawCapture(t, addr, token, sess, app, schema, "n2", "r2b", 1)
				if rr2["ok"] == true || rr2["error"] != errReplay {
					t.Fatalf("[evict] 窗口内 n2 重复应 ErrReplay: %v", rr2)
				}
				rr1 := rawCapture(t, addr, token, sess, app, schema, "n1", "r1b", 1)
				if rr1["ok"] != true {
					t.Fatalf("[evict] 被淘汰的 n1 重复不应再判重放（应 ok）: %v", rr1)
				}
			}

			// token 不得泄漏（本测试不回显子进程输出；此处仅断言错误串不含 token）。
			for _, r := range []map[string]any{r1, r2, r3} {
				if s, ok := r["error"].(string); ok && strings.Contains(s, token) {
					t.Fatalf("错误响应泄漏 token")
				}
			}
		})
	}
}
