// 示例应用（R29-A S2）：演示一个应用如何用 hydrationchannel.Session 接入跨进程状态恢复，**只用既有 API**，
// 不重写第二套协议。无窗口、自有有界 console（非用户业务 main）。
//
// 用法（由父/Watch 侧注入环境变量后启动本 console）：
//
//	APP_PROJECT=<模块根> APP_SESSION=<会话> APP_APPID=<app> APP_SCHEMA=<schema> APP_TOKEN=<会话密钥>
//	APP_GEN=1|2              # 本代次（1=旧代，>=2=新代恢复）
//	APP_VALUES=<json文件>     # 旧代初值（非默认字段），仅 gen==1 使用
//	APP_REPORT=<json文件>     # 本进程自报（pid/gen/restored/DTO），供父端核对
//
// 行为：
//   - gen>=2：Session.Restore 读+校验+在 owner 应用；自报 restored=true 与精确 DTO。
//   - gen==1：NewOldGenEndpoint 起端点（Capture 在 owner 串行点返回当前 DTO 与 servingGen），
//     同时把初值设为非默认；端点存活供父端 Capture。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	hc "github.com/goxml/goxml/pkg/hydrationapp"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

type demoDTO struct {
	Count      int      `json:"count"`
	Items      []string `json:"items"`
	NonDefault string   `json:"nonDefault"`
}

// serialOwner 是应用的单 owner 串行调度器（此处用互斥实现，真实 UI 由应用 Dispatcher 实现同一 Do 契约）。
type serialOwner struct{ ch chan func() }

func newSerialOwner() *serialOwner {
	o := &serialOwner{ch: make(chan func(), 16)}
	go func() {
		for fn := range o.ch {
			fn()
		}
	}()
	return o
}
func (o *serialOwner) Do(ctx context.Context, fn func(context.Context) error) error {
	done := make(chan error, 1)
	select {
	case o.ch <- func() { done <- fn(ctx) }:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// envOr2 首选键 HYD_*，回落 APP_*，再默认（双读收敛协议、向后兼容）。
func envOr2(k1, k2, d string) string {
	if v := os.Getenv(k1); v != "" {
		return v
	}
	if v := os.Getenv(k2); v != "" {
		return v
	}
	return d
}

func main() {
	proj := envOr2("HYD_PROJECT", "APP_PROJECT", ".")
	session := envOr2("HYD_SESSION", "APP_SESSION", "sess-1")
	appID := envOr2("HYD_APP", "APP_APPID", "app-1")
	schema := envOr2("HYD_SCHEMA", "APP_SCHEMA", "demo.v1")
	token := envOr2("HYD_TOKEN", "APP_TOKEN", "tok")
	gen := 1
	fmt.Sscanf(envOr2("HYD_GEN", "APP_GEN", "1"), "%d", &gen)
	valuesFile := envOr2("HYD_VALUES", "APP_VALUES", "")
	reportFile := envOr2("HYD_REPORT", "APP_REPORT", "")

	sess, err := hc.NewSession(hc.SessionConfig{ProjectDir: proj, SessionID: session, AppID: appID, Schema: rst.Schema(schema), Token: token})
	if err != nil {
		fmt.Printf("APP_ERROR new session: %v\n", err)
		return
	}
	owner := newSerialOwner()

	cur := demoDTO{Items: []string{}, NonDefault: "default"}
	restored := false

	// 新代：恢复（owner 串行应用）。
	if gen >= 2 {
		applied, rerr := sess.Restore(context.Background(), 0, owner, func(_ context.Context, payload []byte) error {
			var d demoDTO
			if err := json.Unmarshal(payload, &d); err != nil {
				return err
			}
			cur = d
			return nil
		})
		if rerr != nil {
			fmt.Printf("APP_RESTORE_REJECT %v\n", rerr)
		} else if applied {
			restored = true
		}
	}
	// 旧代初值：优先 HYD_VALUES 文件；否则 HYD_SEED/APP_SEED 指定非默认 Count（用于跨代精确 DTO 断言）。
	if gen == 1 && valuesFile == "" {
		if seed := envOr2("HYD_SEED", "APP_SEED", ""); seed != "" {
			var n int
			if _, e := fmt.Sscanf(seed, "%d", &n); e == nil {
				cur.Count = n
				cur.NonDefault = "seed-" + seed
			}
		}
	}
	// 旧代：设置非默认初值。
	if gen == 1 && valuesFile != "" {
		if b, e := os.ReadFile(valuesFile); e == nil {
			var d demoDTO
			if json.Unmarshal(b, &d) == nil {
				cur = d
			}
		}
	}
	fmt.Printf("APP_RUNNING pid=%d gen=%d count=%d items=%v nonDefault=%s restored=%v\n",
		os.Getpid(), gen, cur.Count, cur.Items, cur.NonDefault, restored)

	if reportFile != "" {
		b, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "gen": gen, "restored": restored,
			"count": cur.Count, "items": cur.Items, "nonDefault": cur.NonDefault})
		tmp := reportFile + ".tmp"
		_ = os.WriteFile(tmp, b, 0o644)
		_ = os.Rename(tmp, reportFile)
	}

	// 每一发布代都具备“下一次捕获”能力：起端点提供服务（Capture 在 owner 串行点读 cur，servingGen=本代）。
	// gen>=2 先完成恢复再起端点，使下一代可发现本代端点。
	_, eperr := sess.NewOldGenEndpoint(owner, func(_ context.Context) ([]byte, int, error) {
		b, _ := json.Marshal(cur)
		return b, gen, nil
	}, 4)
	if eperr != nil {
		fmt.Printf("APP_ENDPOINT_ERROR %v\n", eperr)
		return
	}
	time.Sleep(60 * time.Second) // 有界存活（受控上限）
}
