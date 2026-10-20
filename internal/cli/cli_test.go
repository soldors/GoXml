package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// ============================================================================
// TASK-GENERATE-CLI-001-1：参数解析、退出码与诊断输出
//
// 边界（采纳评审意见）：1-1 不做目录发现、Manifest、--clean、多文件事务。
// 命令级测试不依赖 GUI（天然满足——本包纯参数层）。
// ============================================================================

// TestExitCodes 退出码表（契约 §12 + 1-1 定稿）。
func TestExitCodes(t *testing.T) {
	if ExitOK != 0 || ExitUsage != 1 || ExitFailure != 2 {
		t.Fatalf("退出码常量与文档不一致: OK=%d Usage=%d Failure=%d", ExitOK, ExitUsage, ExitFailure)
	}
}

// TestHelpCommand help 输出到 stdout 且退出码 0。
func TestHelpCommand(t *testing.T) {
	for _, argv := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		t.Run(argv[0], func(t *testing.T) {
			var out, errb bytes.Buffer
			code := Run(argv, &out, &errb)
			if code != ExitOK {
				t.Fatalf("退出码 = %d, 期望 0", code)
			}
			if !strings.Contains(out.String(), "goxml generate <path>") {
				t.Errorf("帮助应包含 generate 用法:\n%s", out.String())
			}
			if errb.Len() != 0 {
				t.Errorf("帮助不应写 stderr:\n%s", errb.String())
			}
		})
	}
}

// TestUsageErrors 用法错误：退出码 1，诊断到 stderr，帮助随附。
func TestUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string // stderr 应包含的片段
	}{
		{"无参数", nil, "缺少子命令"},
		{"未知命令", []string{"build"}, `未知子命令 "build"`},
		{"未知参数", []string{"generate", "a.gxml", "--bogus"}, `未知参数 "--bogus"`},
		{"缺路径", []string{"generate"}, "缺少输入路径"},
		{"out-dir 缺值", []string{"generate", "a.gxml", "--out-dir"}, "--out-dir 需要一个目录参数"},
		{"out-dir 空值", []string{"generate", "a.gxml", "--out-dir="}, "--out-dir 不能为空"},
		{"out-dir 空白值", []string{"generate", "a.gxml", "--out-dir", "  "}, "--out-dir 不能为空"},
		{"空路径", []string{"generate", "  "}, "输入路径不能为空"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := Run(c.argv, &out, &errb)
			if code != ExitUsage {
				t.Fatalf("退出码 = %d, 期望 %d", code, ExitUsage)
			}
			if !strings.Contains(errb.String(), c.want) {
				t.Errorf("stderr 应包含 %q:\n%s", c.want, errb.String())
			}
			// 用法错误必须随附帮助，方便用户自查。
			if !strings.Contains(errb.String(), "goxml generate <path>") {
				t.Errorf("用法错误应随附帮助:\n%s", errb.String())
			}
		})
	}
}

// TestParseGenerateFlags 四个已冻结参数的解析（`--name value` 与 `--name=value`）。
func TestParseGenerateFlags(t *testing.T) {
	t.Run("空格形态", func(t *testing.T) {
		opts, err := parseGenerate([]string{"a.gxml", "--out-dir", "gen", "--check", "--clean", "--no-cache"})
		if err != nil {
			t.Fatal(err)
		}
		if opts.outDir != "gen" || !opts.check || !opts.clean || !opts.noCache {
			t.Errorf("解析结果不对: %+v", opts)
		}
		if len(opts.paths) != 1 || opts.paths[0] != "a.gxml" {
			t.Errorf("路径解析不对: %v", opts.paths)
		}
	})
	t.Run("等号形态", func(t *testing.T) {
		opts, err := parseGenerate([]string{"--out-dir=gen", "a.gxml"})
		if err != nil {
			t.Fatal(err)
		}
		if opts.outDir != "gen" {
			t.Errorf("outDir = %q", opts.outDir)
		}
	})
	t.Run("多路径", func(t *testing.T) {
		opts, err := parseGenerate([]string{"a.gxml", "b.gxml"})
		if err != nil {
			t.Fatal(err)
		}
		if len(opts.paths) != 2 {
			t.Errorf("应接受多路径: %v", opts.paths)
		}
	})
}

// TestGenerateNotYetWired 运行阶段在 1-1 返回确定的运行失败（退出码 2），
// 且**不静默成功**——静默成功会让用户以为已生成。
func TestGenerateNotYetWired(t *testing.T) {
	var out, errb bytes.Buffer
	code := Run([]string{"generate", "a.gxml"}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d, 期望 %d（管线未接线必须是运行失败）", code, ExitFailure)
	}
	if !strings.Contains(errb.String(), "尚未接线") {
		t.Errorf("stderr 应明确说明未接线:\n%s", errb.String())
	}
}

// TestPipelineStubInjectedFailure 桩可注入：退出码与诊断由桩的错误决定。
func TestPipelineStubInjectedFailure(t *testing.T) {
	orig := pipelineStub
	t.Cleanup(func() { pipelineStub = orig })

	pipelineStub = func(opts *generateOptions, stdout, stderr io.Writer) error {
		stdout.Write([]byte("written\n"))
		return nil
	}
	var out, errb bytes.Buffer
	code := Run([]string{"generate", "a.gxml"}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("桩成功时退出码 = %d, 期望 0", code)
	}
	if !strings.Contains(out.String(), "written") {
		t.Errorf("stdout 应收到桩输出:\n%s", out.String())
	}
}
