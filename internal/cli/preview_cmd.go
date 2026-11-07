package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/devx/preview"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const previewUsage = `用法: goxml preview <file.gxml> [flags]
  --build-only                  仅验证构建，不启动窗口
  --keep-host-dir               成功后保留隔离宿主目录
  --tool-root <dir>             匹配的goxml源码/模块缓存根（自动定位失败时指定）
  --build-timeout-seconds <n>    宿主构建上限，默认300s
仅支持无VM的纯静态页；不加载用户业务程序。默认构建后打开独立窗口。
Ctrl+C停止宿主；失败保留证据。Windows首版。
`

type previewOptions struct {
	path, toolRoot  string
	buildOnly, keep bool
	timeout         int
}

func parsePreview(args []string) (previewOptions, error) {
	var o previewOptions
	pos, opts, tail, err := splitDevArgs(args, map[string]bool{"build-only": false, "keep-host-dir": false, "tool-root": true, "build-timeout-seconds": true})
	if err != nil {
		return o, err
	}
	fs := flag.NewFlagSet("preview", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.buildOnly, "build-only", false, "")
	fs.BoolVar(&o.keep, "keep-host-dir", false, "")
	fs.StringVar(&o.toolRoot, "tool-root", "", "")
	fs.IntVar(&o.timeout, "build-timeout-seconds", 300, "")
	if err = fs.Parse(opts); err != nil {
		return o, err
	}
	if len(pos) != 1 || len(tail) != 0 {
		return o, fmt.Errorf("需要单个.gxml文件，不接受应用参数")
	}
	o.path = pos[0]
	if strings.TrimSpace(o.path) == "" {
		return o, fmt.Errorf("输入文件不能为空")
	}
	if o.timeout <= 0 || o.timeout > 86400 {
		return o, fmt.Errorf("构建时限必须为正数且≤86400s")
	}
	return o, nil
}
func runPreview(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprint(stdout, previewUsage)
		return ExitOK
	}
	o, err := parsePreview(args)
	if err != nil {
		fmt.Fprintf(stderr, "goxml preview: %v\n%s", err, previewUsage)
		return ExitUsage
	}
	if runtime.GOOS != "windows" {
		fmt.Fprintln(stderr, "goxml preview: host execution/build currently supports Windows only")
		return ExitFailure
	}
	if ctx.Err() != nil {
		return ExitOK
	}
	abs, err := filepath.Abs(o.path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	moduleRoot, err := findModuleRoot(abs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	values, err := measureContextValuesContext(ctx, resolveGoExe(), moduleRoot)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	overlay, err := userOverlayMap(values.GOFLAGS, moduleRoot)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitFailure
	}
	owned, err := previewOwnedOutputs(moduleRoot)
	if err != nil {
		fmt.Fprintf(stderr, "goxml preview: %v\n", err)
		return ExitFailure
	}
	r, err := preview.Run(preview.Options{Context: ctx, File: abs, AssetOverlay: overlay, AssetOutputPaths: owned, ToolRoot: o.toolRoot, KeepHostDir: o.keep, BuildTimeout: time.Duration(o.timeout) * time.Second, Stdout: stdout, Stderr: stderr})
	if err != nil {
		fmt.Fprintf(stderr, "goxml preview: %v\n", err)
		if r != nil && r.HostDir != "" {
			fmt.Fprintf(stderr, "证据保留：%s\n", r.HostDir)
		}
		return ExitFailure
	}
	if !r.Built() {
		for _, d := range r.Diags {
			fmt.Fprintln(stderr, d.Error())
		}
		return ExitFailure
	}
	if o.buildOnly {
		return finishPreview(r, o.keep, stdout, stderr)
	}
	runner, err := preview.StartHost(r.HostExe, r.HostDir)
	if err != nil {
		fmt.Fprintf(stderr, "goxml preview: %v；证据保留：%s\n", err, r.HostDir)
		return ExitFailure
	}
	exitErr, waitErr := runner.WaitContext(ctx)
	if waitErr != nil {
		if ctx.Err() != nil && runner.StopError() == nil && (errors.Is(waitErr, preview.ErrHostCanceled) || errors.Is(waitErr, preview.ErrHostTimeout)) {
			return finishPreview(r, o.keep, stdout, stderr)
		}
		fmt.Fprintf(stderr, "goxml preview: %v；证据保留：%s\n", waitErr, r.HostDir)
		return ExitFailure
	}
	if exitErr != nil {
		fmt.Fprintf(stderr, "goxml preview: 宿主异常退出 %v；证据保留：%s\n", exitErr, r.HostDir)
		return ExitFailure
	}
	return finishPreview(r, o.keep, stdout, stderr)
}
func finishPreview(r *preview.Result, keep bool, out, errout io.Writer) int {
	if keep {
		fmt.Fprintf(out, "静态预览构建通过；宿主保留：%s\n", r.HostDir)
		return ExitOK
	}
	if err := r.Cleanup(); err != nil {
		fmt.Fprintf(errout, "goxml preview: 清理失败: %v（目录 %s）\n", err, r.HostDir)
		return ExitFailure
	}
	fmt.Fprintln(out, "静态预览验证完成，隔离宿主已清理")
	return ExitOK
}
func previewOwnedOutputs(moduleRoot string) (map[string]bool, error) {
	man, err := loadManifest(manifestPath(moduleRoot))
	if err != nil {
		return nil, err
	}
	if err = validateManifestOutputPaths(moduleRoot, man); err != nil {
		return nil, err
	}
	owned := map[string]bool{}
	for _, e := range man.Entries {
		full := filepath.Join(moduleRoot, filepath.FromSlash(e.Output))
		data, exists, err := readOutput(full)
		if err != nil {
			return nil, err
		}
		if exists && codegen.HasProtectedHeader(data) && headerSourceMatches(data, filepath.Base(filepath.FromSlash(e.Source))) {
			abs, err := filepath.Abs(full)
			if err != nil {
				return nil, err
			}
			owned[filepath.Clean(abs)] = true
		}
	}
	return owned, nil
}
