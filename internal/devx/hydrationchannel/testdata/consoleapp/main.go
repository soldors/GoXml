// R28-A S2：自有有界 console 应用（无窗口，非用户业务 main/init）。
// 旧代：持非默认 DTO + 单一 owner 循环；用 hydrationchannel.Endpoint 在回环暴露快照端点，
//
//	把 {addr,token} 写到 portfile，供父进程构造 Client。
//
// 新代：从状态文件经 hydrationchannel.Applier 在 owner 串行点恢复；自报 pid/session/gen/nonce/
//
//	精确 DTO/restored；随后也启动本代端点（供下一次链）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
	"github.com/goxml/goxml/internal/devx/hydrationchannel"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

type demoDTO struct {
	Count      int      `json:"count"`
	Items      []string `json:"items"`
	NonDefault string   `json:"nonDefault"`
}

// ownerLoop 是单一 owner：所有捕获/应用都投递到此串行执行（不在 transport goroutine 直接读写模型）。
type ownerLoop struct {
	req     chan func()
	stopped chan struct{}
}

func newOwnerLoop() *ownerLoop {
	o := &ownerLoop{req: make(chan func(), 16), stopped: make(chan struct{})}
	go o.run()
	return o
}

func (o *ownerLoop) run() {
	defer close(o.stopped)
	for fn := range o.req {
		fn()
	}
}

// Do 实现 hydrationchannel.OwnerRunner：把 fn 投递到 owner 串行点执行并等待完成（有界）。
func (o *ownerLoop) Do(ctx context.Context, fn func(context.Context) error) error {
	done := make(chan error, 1)
	select {
	case o.req <- func() { done <- fn(ctx) }:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err() // 排队后取消：不应用（fn 可能仍执行但结果丢弃；由调用方取消语义保证）
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func writeJSONAtomic(path string, v any) {
	b, _ := json.Marshal(v)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

func main() {
	session := envOr("HYD_SESSION", "sess-1")
	app := envOr("HYD_APP", "app-1")
	schema := envOr("HYD_SCHEMA", "demo.v1")
	state := envOr("HYD_STATE", "")
	valuesFile := envOr("HYD_VALUES", "")
	portFile := envOr("HYD_PORTFILE", "")
	token := envOr("HYD_TOKEN", "")
	reportFile := envOr("HYD_REPORT", "")
	nonce := envOr("HYD_NONCE", "")
	thisGen := 0
	if g := envOr("HYD_GEN", ""); g != "" {
		fmt.Sscanf(g, "%d", &thisGen)
	}

	st, err := devxh.NewStore(envOr("HYD_PROJECT", "."), session)
	if err != nil {
		fmt.Printf("STORE_ERR %v\n", err)
		return
	}
	owner := newOwnerLoop()

	// 当前 DTO：默认；旧代从 valuesFile 读非默认；新代从状态文件恢复（覆盖）。
	cur := demoDTO{Items: []string{}, NonDefault: "default"}
	restored := false
	restoredGen := 0

	// 新代恢复：若注入 state 且文件存在。
	if state != "" {
		if _, statErr := os.Stat(state); statErr == nil {
			proto := &devxh.Protocol{Store: st, Schema: rst.Schema(schema), AppID: app}
			ap, aerr := hydrationchannel.NewApplier(proto, owner, func(_ context.Context, pl []byte) error {
				var d demoDTO
				if uerr := json.Unmarshal(pl, &d); uerr != nil {
					return uerr
				}
				cur = d
				return nil
			})
			if aerr == nil {
				ok, rerr := ap.RestoreOnOwner(context.Background(), 0)
				if rerr != nil {
					fmt.Printf("RESTORE_REJECT err=%v\n", rerr)
				} else if ok {
					restored = true
					restoredGen = thisGen // 记录本代次
				}
			}
		}
	}
	// 旧代：从 valuesFile 设置非默认初值（仅在未恢复时）。
	if valuesFile != "" && !restored {
		if raw, e := os.ReadFile(valuesFile); e == nil {
			var d demoDTO
			if json.Unmarshal(raw, &d) == nil {
				cur = d
			}
		}
	}

	fmt.Printf("RUNNING pid=%d gen=%d count=%d items=%v nonDefault=%s restored=%v\n",
		os.Getpid(), thisGen, cur.Count, cur.Items, cur.NonDefault, restored)

	// 自报（新代）
	if reportFile != "" {
		writeJSONAtomic(reportFile, map[string]any{
			"pid": os.Getpid(), "session": session, "gen": restoredGen, "restored": restored,
			"nonce": nonce, "count": cur.Count, "items": cur.Items, "nonDefault": cur.NonDefault,
			"requestGen": thisGen,
		})
	}

	// 启动本代端点（供下一次快照），把 addr/token 写 portfile。
	if portFile != "" && token != "" {
		ep, eerr := hydrationchannel.NewEndpoint(owner, func(ctx context.Context) ([]byte, int, error) {
			b, merr := json.Marshal(cur)
			return b, thisGen, merr
		}, session, app, schema, token, 4)
		if eerr != nil {
			fmt.Printf("ENDPOINT_ERR %v\n", eerr)
			return
		}
		addr, serr := ep.Start()
		if serr != nil {
			fmt.Printf("ENDPOINT_START_ERR %v\n", serr)
			return
		}
		// 写 addr 到 portfile（token 不放文件；父已从环境/生成端持有 token；此处仅回显 addr）
		writeJSONAtomic(portFile, map[string]any{"addr": addr, "pid": os.Getpid(), "gen": thisGen})
		defer func() { _ = ep.Stop() }()
	}

	// 有界存活（受控上限）。
	time.Sleep(time.Duration(60) * time.Second)
}
