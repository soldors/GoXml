// console 是 R27-A hydration 两进程整链的**自有有界 console helper**（无窗口，非用户业务 main/init）。
// 它演示：可选启动时从状态文件恢复（经 pkg/runtime/hydration 校验），并响应“快照请求”把当前 DTO
// 写到响应文件（供 Watch 的 Capture 读取）。不打开窗口、不执行任何用户业务。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

type demoDTO struct {
	Count int      `json:"count"`
	Items []string `json:"items"`
	// NonDefault 是“非默认字段”，用于证明精确恢复（旧代设了非零值，新代应恢复同值）。
	NonDefault string `json:"nonDefault"`
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	state := envOr("HYDRATE_STATE", "")
	session := envOr("HYDRATE_SESSION", "sess-1")
	schema := envOr("HYDRATE_SCHEMA", "demo.v1")
	app := envOr("HYDRATE_APP", "app-1")
	valuesFile := envOr("HYDRATE_VALUES", "")
	reqFile := envOr("HYDRATE_SNAPSHOT_REQ", "")
	respFile := envOr("HYDRATE_SNAPSHOT_RESP", "")
	respReady := envOr("HYDRATE_SNAPSHOT_READY", "")

	cur := demoDTO{Count: 0, Items: []string{}, NonDefault: "default"}

	// 1) 恢复：状态文件存在则经协议校验并应用。
	if state != "" {
		if raw, err := os.ReadFile(state); err == nil {
			exp := rst.Expect{Schema: rst.Schema(schema), AppID: app, SessionID: session}
			if env, verr := rst.DecodeAndVerify(raw, exp); verr != nil {
				fmt.Printf("RESTORE_REJECT err=%v\n", verr)
			} else {
				var d demoDTO
				if perr := env.DecodePayload(&d); perr != nil {
					fmt.Printf("RESTORE_REJECT decode=%v\n", perr)
				} else {
					cur = d
					fmt.Printf("RESTORED pid=%d count=%d items=%v nonDefault=%s gen=%d\n",
						os.Getpid(), cur.Count, cur.Items, cur.NonDefault, env.Generation)
				}
			}
		}
	}
	// 2) 初始值（旧代据此设置非默认字段；在有状态文件时不覆盖恢复值，除非无状态）。
	if valuesFile != "" {
		if raw, err := os.ReadFile(valuesFile); err == nil && state == "" {
			var d demoDTO
			if json.Unmarshal(raw, &d) == nil {
				cur = d
			}
		}
	}
	fmt.Printf("RUNNING pid=%d count=%d items=%v nonDefault=%s\n", os.Getpid(), cur.Count, cur.Items, cur.NonDefault)

	if reqFile == "" || respFile == "" {
		// 无快照请求配置：短暂存活后退出（受控上限）。
		time.Sleep(200 * time.Millisecond)
		return
	}

	// 3) 有界轮询快照请求文件；存在即写当前 DTO 到响应文件 + ready 标记，然后删除请求。
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(reqFile); err == nil {
			b, _ := json.Marshal(cur)
			_ = os.WriteFile(respFile, b, 0o644)
			if respReady != "" {
				_ = os.WriteFile(respReady, []byte("ready"), 0o644)
			}
			_ = os.Remove(reqFile)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
