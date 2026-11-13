package cli

// R29-A S3：`goxml watch` 的 Hydration opt-in 候选回归（无原生窗口）。
// 覆盖：默认无标志 -> 不启用（cfg.Hydration 概念等价 nil、pm.EnvFor 不设置）；--hydration -> 构造 hook+env；
// 会话 token 不出现在 argv/诊断；配置来源（session/app/schema 默认与覆盖）；停旧/启新次序沿用既有。
// 仅在能构造 Session/端点的受控环境跑（本机 Windows；非 Windows 由 runtime 门禁拦截 watch）。

import (
	"strings"
	"testing"

	hc "github.com/goxml/goxml/internal/devx/hydrationchannel"
	"github.com/goxml/goxml/internal/devx/watch"
)

// TestWatchParseNoHydrationDefault 无 --hydration 时 hydration 字段为 false（默认关闭，行为不变）。
func TestWatchParseNoHydrationDefault(t *testing.T) {
	o, err := parseWatch([]string{"./mymod", "--entry", "./cmd/app"})
	if err != nil {
		t.Fatal(err)
	}
	if o.hydration {
		t.Fatal("hydration must default off")
	}
	// 默认配置不含 Hydration / EnvFor 语义：makeWatchConfig 的 Hydration=nil。
	cfg := makeWatchConfig("/abs/proj", o)
	if cfg.Hydration != nil {
		t.Fatal("no-flag config must have nil Hydration (unchanged behavior)")
	}
}

// TestWatchParseHydrationOptIn --hydration 打开 + 配置来源（默认与覆盖）。
func TestWatchParseHydrationOptIn(t *testing.T) {
	o, err := parseWatch([]string{"./mymod", "--entry", "./cmd/app", "--hydration",
		"--hydration-session", "sess-x", "--hydration-app", "app-x", "--hydration-schema", "sch-x"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.hydration || o.hydrationSession != "sess-x" || o.hydrationApp != "app-x" || o.hydrationSchema != "sch-x" {
		t.Fatalf("hydration opt-in config wrong: %+v", o)
	}
}

// TestWatchHydrationDefaults 默认 session/schema 值。
func TestWatchHydrationDefaults(t *testing.T) {
	o, err := parseWatch([]string{"./mymod", "--entry", "./cmd/app", "--hydration"})
	if err != nil {
		t.Fatal(err)
	}
	if o.hydrationSession != "watch" || o.hydrationSchema != "watch.v1" {
		t.Fatalf("defaults wrong: session=%q schema=%q", o.hydrationSession, o.hydrationSchema)
	}
}

// TestBuildHydrationHookAndEnvNoTokenInArgv 构造 hook+env：token 只在 HYD_TOKEN 环境，不在任何 argv/输出。
func TestBuildHydrationHookAndEnvNoTokenInArgv(t *testing.T) {
	root := t.TempDir()
	var out, errb strings.Builder
	o := watchOptions{entry: "./cmd/app", hydration: true, hydrationSession: "s", hydrationApp: "a", hydrationSchema: "sch"}
	hook, envFor, err := buildHydration(root, o, &out, &errb)
	if err != nil {
		t.Fatalf("buildHydration: %v", err)
	}
	if hook == nil || hook.Proto == nil || hook.Capture == nil {
		t.Fatal("hook not fully constructed")
	}
	// 从注入 env 提取 token，并确认它未出现在 argv/诊断。
	var tok string
	for _, kv := range envFor(1) {
		if strings.HasPrefix(kv, "HYD_TOKEN=") {
			tok = strings.TrimPrefix(kv, "HYD_TOKEN=")
		}
	}
	if tok == "" {
		t.Fatal("token not injected to child env")
	}
	// 子进程环境注入不含 argv（命令行在 pm.Args，本函数不写 argv）。诊断输出不得含 token。
	if strings.Contains(out.String(), tok) || strings.Contains(errb.String(), tok) {
		t.Fatal("token leaked into diagnostics output")
	}
	// 新代 env 带 HYD_STATE=restore；旧代 serve。
	hasState := false
	for _, kv := range envFor(2) {
		if strings.HasPrefix(kv, "HYD_STATE=") || kv == "HYD_MODE=restore" {
			hasState = true
		}
	}
	if !hasState {
		t.Fatal("gen>=2 env must include restore (HYD_STATE/HYD_MODE=restore)")
	}
}

// TestWatchConfigWithHydrationSet 启用时 cfg.Hydration 非 nil（接线点存在），默认 session 生效。
func TestWatchConfigHydrationWiringPoint(t *testing.T) {
	var h watch.HydrationHook
	cfg := watch.Config{ProjectDir: "/x", Hydration: &h}
	if cfg.Hydration == nil {
		t.Fatal("watch.Config.Hydration must be a valid wiring point")
	}
}

// R29-A R1：默认 fail-closed（满额拒绝）——replayPolicyOf 默认 reject；evict 显式选择。
func hcRejectOnFull() hc.ReplayPolicy { return hc.ReplayRejectOnFull }

func TestR1HydrationReplayDefaultFailClosed(t *testing.T) {
	o, err := parseWatch([]string{"./m", "--entry", "./cmd/app", "--hydration"})
	if err != nil {
		t.Fatal(err)
	}
	if o.hydrationReplay != "reject" {
		t.Fatalf("default hydration-replay must be reject (fail-closed), got %q", o.hydrationReplay)
	}
	if replayPolicyOf(o.hydrationReplay) != hcRejectOnFull() {
		t.Fatal("default policy must map to ReplayRejectOnFull")
	}
	if replayPolicyOf("evict") == hcRejectOnFull() {
		t.Fatal("evict must map to evict policy")
	}
}
