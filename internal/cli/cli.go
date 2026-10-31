// Package cli 实现 goxml 命令行的参数解析、退出码与诊断输出。
//
// 职责边界（TASK-GENERATE-CLI-001-1）：
//   - 子命令路由与参数解析；
//   - 用法错误与运行失败的退出码区分；
//   - 诊断信息输出到 stderr，帮助信息输出到 stdout。
//
// 不在本包：目录发现、Manifest、--clean 语义、多文件事务——
// 分别归任务卡 1-3/1-5/1-6。generate 与编译管线的接线归 1-2。
//
// 退出码（契约 §12：失败必须非零；本表在 1-1 定稿）：
//
//	0 成功（包括「成功但无变更」的 skipped）
//	1 用法错误（未知命令、未知参数、缺参数、参数值缺失）
//	2 运行失败（生成、提交、检查等管线阶段失败）
package cli

import (
	"fmt"
	"io"
	"strings"
)

// 退出码（见包注释）。
const (
	ExitOK      = 0
	ExitUsage   = 1
	ExitFailure = 2
)

// usage 是帮助文本（stdout）。
const usage = `goxml — Goxml 的生成命令行

用法:

  goxml generate <path> [flags]    生成 .gxml 对应的 Go 代码
  goxml help                       显示本帮助

generate 参数:

  --out-dir <dir>    指定输出根目录（默认与 .gxml 同目录）
  --check            提交前执行 package/build 检查
  --clean            按 Manifest 清理已登记的孤儿产物
  --no-cache         忽略增量缓存（仍更新 Manifest）

退出码:

  0  成功（含 skipped）
  1  用法错误
  2  运行失败
`

// Run 执行命令行入口并返回退出码。
//
// argv 不含程序名；stdout 收帮助与正常输出，stderr 收诊断。
func Run(argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "goxml: 缺少子命令")
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch argv[0] {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	case "generate":
		return runGenerate(argv[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "goxml: 未知子命令 %q\n", argv[0])
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
}

// generateOptions 是 generate 子命令的解析结果。
type generateOptions struct {
	paths   []string
	outDir  string
	check   bool
	clean   bool
	noCache bool
}

// runGenerate 解析 generate 参数，执行生成管线并映射稳定退出码。
func runGenerate(argv []string, stdout, stderr io.Writer) int {
	opts, err := parseGenerate(argv)
	if err != nil {
		fmt.Fprintf(stderr, "goxml generate: %v\n", err)
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	if err := runGeneratePipeline(opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "goxml generate: %v\n", err)
		return ExitFailure
	}
	return ExitOK
}

// parseGenerate 解析 generate 的参数。
//
// 规则：
//   - 长参数 `--name=value` 与 `--name value` 两种形态都接受；
//   - 未知参数、缺值、缺路径都是用法错误；
//   - 路径可以有多个（多文件发现归 1-3，这里先接受并校验非空）。
func parseGenerate(argv []string) (*generateOptions, error) {
	opts := &generateOptions{}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--out-dir":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--out-dir 需要一个目录参数")
			}
			i++
			opts.outDir = argv[i]
			if strings.TrimSpace(opts.outDir) == "" {
				return nil, fmt.Errorf("--out-dir 不能为空")
			}
		case strings.HasPrefix(arg, "--out-dir="):
			opts.outDir = strings.TrimPrefix(arg, "--out-dir=")
			if strings.TrimSpace(opts.outDir) == "" {
				return nil, fmt.Errorf("--out-dir 不能为空")
			}
		case arg == "--check":
			opts.check = true
		case arg == "--clean":
			opts.clean = true
		case arg == "--no-cache":
			opts.noCache = true
		case strings.HasPrefix(arg, "--"):
			return nil, fmt.Errorf("未知参数 %q", arg)
		default:
			opts.paths = append(opts.paths, arg)
		}
	}
	if len(opts.paths) == 0 {
		return nil, fmt.Errorf("缺少输入路径")
	}
	for _, p := range opts.paths {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("输入路径不能为空")
		}
	}
	return opts, nil
}
