package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	hc "github.com/goxml/goxml/internal/devx/hydrationchannel"
	"github.com/goxml/goxml/internal/devx/process"
	"github.com/goxml/goxml/internal/devx/watch"
	"github.com/goxml/goxml/internal/devx/watchadapter"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const watchUsage = `用法: goxml watch <module-dir> --entry <relative-main-package> [flags] [-- app-args...]
  --entry <package>              必须指定模块内的应用入口，例如 ./cmd/app
  --exclude-dir <dir>           额外排除目录，可重复（例如运行日志目录）
  --poll-ms <n>                  轮询间隔，默认50ms；防抖固定100ms
  --build-timeout-seconds <n>    每代生成与构建上限，默认120s
仅监听模块内文件；构建成功后停止旧进程再启动新代。没有内存状态恢复。
Ctrl+C停止监听并清理自有进程树。Windows首版。

可选 Hydration（默认关闭；显式 --hydration 才启用）：
  --hydration                     启用跨进程状态恢复（需应用经 hydrationchannel 实现 Capture/恢复）
  --hydration-session <id>        会话标识（默认 watch）
  --hydration-app <id>            应用标识（默认来自 --entry）
  --hydration-schema <id>         DTO schema（默认 watch.v1）
启用后：旧代在停旧前于应用 owner 串行点捕获显式 DTO 并原子提交；新代据注入的环境恢复到 owner；
捕获失败/取消保旧不切换；新代启动失败不自动复活旧代。会话 token 仅经子进程环境注入，不出现在 argv/诊断。
`

type watchOptions struct {
	root, entry string
	poll        int
	timeout     int
	args        []string
	excludes    devStringList
	// Hydration opt-in（候选；默认关闭）。
	hydration          bool
	hydrationSession   string
	hydrationApp       string
	hydrationSchema    string
	hydrationReplay    string // reject(默认,fail-closed) | evict
	hydrationReplayCap int
}

func parseWatch(args []string) (watchOptions, error) {
	var o watchOptions
	pos, opts, tail, err := splitDevArgs(args, map[string]bool{"exclude-dir": true, "entry": true, "poll-ms": true, "build-timeout-seconds": true,
		"hydration": false, "hydration-session": true, "hydration-app": true, "hydration-schema": true,
		"hydration-replay": true, "hydration-replay-cap": true})
	if err != nil {
		return o, err
	}
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&o.excludes, "exclude-dir", "")
	fs.StringVar(&o.entry, "entry", "", "")
	fs.IntVar(&o.poll, "poll-ms", 50, "")
	fs.IntVar(&o.timeout, "build-timeout-seconds", 120, "")
	fs.BoolVar(&o.hydration, "hydration", false, "")
	fs.StringVar(&o.hydrationSession, "hydration-session", "watch", "")
	fs.StringVar(&o.hydrationApp, "hydration-app", "", "")
	fs.StringVar(&o.hydrationSchema, "hydration-schema", "watch.v1", "")
	fs.StringVar(&o.hydrationReplay, "hydration-replay", "reject", "")
	fs.IntVar(&o.hydrationReplayCap, "hydration-replay-cap", 0, "")
	if err = fs.Parse(opts); err != nil {
		return o, err
	}
	if len(pos) != 1 {
		return o, fmt.Errorf("需要一个模块根目录")
	}
	o.root = pos[0]
	if strings.TrimSpace(o.root) == "" {
		return o, fmt.Errorf("模块路径不能为空")
	}
	o.args = tail
	if strings.TrimSpace(o.entry) == "" {
		return o, fmt.Errorf("必须指定 --entry")
	}
	if o.poll <= 0 || o.poll > 60000 || o.timeout <= 0 || o.timeout > 86400 {
		return o, fmt.Errorf("轮询/构建时限必须为正数（poll≤60000ms、timeout≤86400s）")
	}
	e := filepath.Clean(o.entry)
	if filepath.IsAbs(e) || e == ".." || strings.HasPrefix(e, ".."+string(filepath.Separator)) {
		return o, fmt.Errorf("--entry 必须位于模块内")
	}
	o.entry = filepath.ToSlash(e)
	if o.entry != "." && !strings.HasPrefix(o.entry, "./") {
		o.entry = "./" + o.entry
	}
	return o, nil
}
func runWatch(ctx context.Context, args []string, stdout, stderr io.Writer, tool string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, watchUsage)
		return ExitOK
	}
	o, err := parseWatch(args)
	if err != nil {
		fmt.Fprintf(stderr, "goxml watch: %v\n%s", err, watchUsage)
		return ExitUsage
	}
	if runtime.GOOS != "windows" {
		fmt.Fprintln(stderr, "goxml watch: process-tree execution is currently supported on Windows only")
		return ExitFailure
	}
	root, err := filepath.Abs(o.root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	if st, e := os.Stat(filepath.Join(root, "go.mod")); e != nil || st.IsDir() {
		fmt.Fprintln(stderr, "goxml watch: 输入必须是含 go.mod 的模块根目录")
		return ExitFailure
	}
	if tool == "" {
		fmt.Fprintln(stderr, "goxml watch: 无法确定工具二进制身份")
		return ExitFailure
	}
	if ctx.Err() != nil {
		return ExitOK
	}
	query := process.RunCommandTree(ctx, resolveGoExe(), []string{"list", "-e", "-f", "{{.Name}}", o.entry}, root, nil, 10*time.Second)
	if ctx.Err() != nil {
		return ExitOK
	}
	if query.Err != nil || query.RC != 0 {
		fmt.Fprintf(stderr, "goxml watch: 入口查询失败: %v %s\n", query.Err, query.Output)
		return ExitFailure
	}
	if strings.TrimSpace(query.Stdout) != "main" {
		fmt.Fprintln(stderr, "goxml watch: --entry 必须指向 package main")
		return ExitUsage
	}
	b := watchadapter.NewRealBuilder(root, o.entry, "", tool)
	pm := watch.NewProcessManager(b.BinaryPath)
	pm.Args = o.args
	pm.WorkDir = root
	pm.Stdout = stdout
	pm.Stderr = stderr
	cfg := makeWatchConfig(root, o)
	// S3：opt-in Hydration。默认关闭——不设置时 cfg.Hydration=nil、pm.EnvFor=nil，行为与既有完全一致。
	if o.hydration {
		hook, envFor, runID, hErr := buildHydration(root, o, stdout, stderr)
		if hErr != nil {
			fmt.Fprintf(stderr, "goxml watch: hydration 启用失败: %v\n", hErr)
			return ExitFailure
		}
		cfg.Hydration = hook
		pm.EnvFor = envFor
		// 并行安全：本运行实例的构建产物写到专属目录 <root>/.goxml/run/<runID>/，
		// 使同项目并行 Watch 的同代二进制互不覆盖（R28 默认路径仅在不启用 Hydration 时使用）。
		b.OutDir = filepath.Join(root, ".goxml", "run", runID)
	}
	obs := watchObserver{pm, stdout, stderr}
	w, err := watch.New(cfg, b, pm, obs)
	if err != nil {
		fmt.Fprintf(stderr, "goxml watch: %v\n", err)
		return ExitFailure
	}
	if _, err = w.NotifyChange(ctx, 0, []string{"go.mod"}); err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	fmt.Fprintf(stdout, "watch: 监听 %s，入口 %s\n", root, o.entry)
	runErr := w.Run(ctx)
	stopErr := pm.Shutdown()
	if runErr != nil || stopErr != nil {
		fmt.Fprintf(stderr, "goxml watch: 监听/清理失败: %v %v\n", runErr, stopErr)
		return ExitFailure
	}
	fmt.Fprintln(stdout, "watch: 已停止，进程树已收口")
	return ExitOK
}

type watchObserver struct {
	pm       *watch.ProcessManager
	out, err io.Writer
}

func (o watchObserver) OnGeneration(gen int, r watch.GenerationResult, published bool) {
	if published {
		pid := 0
		if c := o.pm.Current(); c != nil {
			pid = c.PID
		}
		fmt.Fprintf(o.out, "watch: 已启动第%d代 PID=%d\n", gen, pid)
		return
	}
	state := "当前没有运行进程"
	if c := o.pm.Current(); c != nil && c.Running() {
		state = "原进程继续运行"
	}
	fmt.Fprintf(o.err, "watch: 第%d代未发布 [%s] %s；%s\n", gen, r.Stage, r.Detail, state)
}

type devStringList []string

func (s *devStringList) String() string { return strings.Join(*s, ",") }
func (s *devStringList) Set(v string) error {
	if strings.TrimSpace(v) == "" || filepath.Clean(v) == "." {
		return fmt.Errorf("排除目录不能为空或模块根")
	}
	*s = append(*s, v)
	return nil
}
func makeWatchConfig(root string, o watchOptions) watch.Config {
	return watch.Config{ProjectDir: root, PollIntervalMS: o.poll, DebounceMS: 100, BuildTimeoutMS: o.timeout * 1000, ExcludeDirs: append([]string(nil), o.excludes...)}
}

// buildHydration 构造 opt-in Hydration 的 Session、Watch Hook 与子进程环境注入。
// 语义（候选）：
//   - 会话 token 由父端随机生成（crypto/rand）；**仅经子进程环境** (HYD_TOKEN) 注入，绝不进入 argv 或诊断输出。
//   - 旧代（gen==1）：子进程以 HYD_MODE=serve 起端点；父端在停旧前经 portfile 发现端点并 Capture（owner 串行，应用侧）。
//   - 新代（gen>=2）：子进程以 HYD_MODE=restore 从 HYD_STATE 恢复；注入 HYD_STATE=<本会话状态文件>。
//   - Capture 失败/取消 -> hook.BeforeSwap 返回错误 -> Watch 保旧不切换（既有配置）。
//   - 新代启动失败沿用既有 Watch 次序（如实报告当前无服务，不自动复活旧代）。
func buildHydration(root string, o watchOptions, stdout, stderr io.Writer) (*watch.HydrationHook, func(int) []string, string, error) {
	tok, terr := newSessionToken()
	if terr != nil {
		return nil, nil, "", terr
	}
	appID := o.hydrationApp
	if strings.TrimSpace(appID) == "" {
		appID = strings.TrimPrefix(o.entry, "./")
	}
	// 启动前校验 replay 配置（空=默认 reject；非法值在启动前拒绝）。
	if o.hydrationReplay != "" && o.hydrationReplay != "reject" && o.hydrationReplay != "evict" {
		return nil, nil, "", fmt.Errorf("非法 --hydration-replay=%q（仅 reject|evict）", o.hydrationReplay)
	}
	if o.hydrationReplay == "" {
		o.hydrationReplay = "reject"
	}
	if o.hydrationReplayCap < 0 {
		return nil, nil, "", fmt.Errorf("非法 --hydration-replay-cap=%d（须 >=0）", o.hydrationReplayCap)
	}
	sess, serr := hc.NewSession(hc.SessionConfig{
		ProjectDir: root, SessionID: o.hydrationSession, AppID: appID,
		Schema: rstSchema(o.hydrationSchema), Token: tok, CaptureTimeout: hc.DefaultCaptureTimeout,
		ReplayPolicy: replayPolicyOf(o.hydrationReplay), ReplayCap: o.hydrationReplayCap,
	})
	if serr != nil {
		return nil, nil, "", serr
	}
	statePath := sess.Store().Path()
	baseEnv := func(gen int) []string {
		e := []string{
			"HYD_PROJECT=" + root, "HYD_SESSION=" + o.hydrationSession,
			"HYD_APP=" + appID, "HYD_SCHEMA=" + o.hydrationSchema, "HYD_TOKEN=" + tok,
			fmt.Sprintf("HYD_GEN=%d", gen),
			"HYD_REPLAY_POLICY=" + o.hydrationReplay, fmt.Sprintf("HYD_REPLAY_CAP=%d", o.hydrationReplayCap),
		}
		if gen >= 2 {
			e = append(e, "HYD_MODE=restore", "HYD_STATE="+statePath)
		} else {
			e = append(e, "HYD_MODE=serve")
		}
		return e
	}
	capture := func(ctx context.Context, oldGen int) ([]byte, error) {
		cl, ok := sess.NewClientFromPort(tok)
		if !ok {
			return nil, fmt.Errorf("无可用旧代端点（gen=%d）", oldGen)
		}
		return cl.Capture(ctx, oldGen)
	}
	hook := &watch.HydrationHook{Proto: sess.Proto(), Capture: capture, RestoreEnv: nil}
	// runID 与 session.go 派生一致（sha256(token)[:8]），用于本运行专属构建产物目录。
	return hook, baseEnv, runIDFromToken(tok), nil
}

// newSessionToken 生成一次性随机会话 token（仅内存/子进程环境，不打印）。
func newSessionToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func rstSchema(s string) rst.Schema { return rst.Schema(s) }

// replayPolicyOf 映射 CLI 选择到 nonce 满额策略：默认 "reject"（fail-closed 满额拒绝）；"evict" 窗口淘汰。
func replayPolicyOf(s string) hc.ReplayPolicy {
	if s == "evict" {
		return hc.ReplayEvictOldest
	}
	return hc.ReplayRejectOnFull
}

// runIDFromToken 与 hydrationchannel 派生一致：sha256(token)[:8]（非机密、不写日志）。
func runIDFromToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:8])
}
