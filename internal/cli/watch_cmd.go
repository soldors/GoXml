package cli

import (
	"context"
	"flag"
	"fmt"
	"github.com/goxml/goxml/internal/devx/process"
	"github.com/goxml/goxml/internal/devx/watch"
	"github.com/goxml/goxml/internal/devx/watchadapter"
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
`

type watchOptions struct {
	root, entry string
	poll        int
	timeout     int
	args        []string
	excludes    devStringList
}

func parseWatch(args []string) (watchOptions, error) {
	var o watchOptions
	pos, opts, tail, err := splitDevArgs(args, map[string]bool{"exclude-dir": true, "entry": true, "poll-ms": true, "build-timeout-seconds": true})
	if err != nil {
		return o, err
	}
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&o.excludes, "exclude-dir", "")
	fs.StringVar(&o.entry, "entry", "", "")
	fs.IntVar(&o.poll, "poll-ms", 50, "")
	fs.IntVar(&o.timeout, "build-timeout-seconds", 120, "")
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
	obs := watchObserver{pm, stdout, stderr}
	w, err := watch.New(makeWatchConfig(root, o), b, pm, obs)
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
