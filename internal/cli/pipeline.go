// Package cli 的 generate 执行阶段（任务卡 1-2）：
// 接通 Parser → Expander → Analyzer → Codegen → Commit。
//
// 范围纪律：本文件只做**单文件**真实路径（TASK-GENERATE-CLI-001-2）；
// 目录发现（1-3）、Manifest 与多文件提交（1-5/1-6）不在这里提前实现。
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// pipelineFunc 是 generate 执行阶段的函数形态，包级变量便于测试注入。
var pipelineFunc = runPipeline

// runPipeline 执行单文件生成：读源 → 管线 → Commit。
//
// 决策落实（见 GENERATE-CLI-001-短检查记录 §6）：
//   - G2：SourceDir = 该 .gxml 实际所在目录（单文件解析上下文）；
//   - G6：--check 暂与默认行为相同（L1+L2 已是默认路径）；
//   - G7：最小夹具在 internal/cli/testdata/miniproj。
func runPipeline(opts *generateOptions, stdout, stderr io.Writer) error {
	if len(opts.paths) != 1 {
		// 多文件的事务粒度在 1-5 前冻结（G4），此处明确不做。
		return fmt.Errorf("当前切片只支持单个输入路径（多文件归任务卡 1-3/1-5）")
	}
	srcPath := opts.paths[0]

	// 输入必须存在且是文件。
	fi, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("输入路径不可用: %w", err)
	}
	if fi.IsDir() {
		return fmt.Errorf("%q 是目录；当前切片只支持单文件（目录发现归 1-3）", srcPath)
	}

	// ModuleRoot 向上找 go.mod（与 examples/demo 的 moduleRoot 同规则）。
	moduleRoot, err := findModuleRoot(srcPath)
	if err != nil {
		return err
	}

	// G2：SourceDir 是该文件所在目录（绝对路径）。
	absSrc, err := filepath.Abs(srcPath)
	if err != nil {
		return fmt.Errorf("解析输入绝对路径: %w", err)
	}
	sourceDir := filepath.Dir(absSrc)

	src, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取源文件: %w", err)
	}

	// PackageName 取所在目录的 Go 包名：与生成文件同包（契约 §4）。
	pkgName, err := packageDirName(sourceDir)
	if err != nil {
		return err
	}

	// SourcePath 相对目标包目录（= 源目录，契约 §3.1 默认输出形态）。
	sourcePath := filepath.Base(srcPath)

	res := pipeline.Run(sourcePath, src, pipeline.Config{
		ModuleRoot:  moduleRoot,
		SourceDir:   sourceDir,
		PackageName: pkgName,
		SourcePath:  sourcePath,
	}, pipeline.Hooks{})

	// 诊断输出：无论成败都如实呈现（失败归 ExitFailure，由调用方映射）。
	for _, d := range res.Diags {
		fmt.Fprintf(stderr, "%s: %s: %s\n", d.File, d.Code, d.Message)
	}
	if !res.OK() {
		return fmt.Errorf("生成失败（状态 %v，%d 条诊断）", res.Status, len(res.Diags))
	}

	// Commit：同目录落盘（契约 §3.1 默认形态）。
	// --out-dir 在 1-3 与目录发现一起实现（需要相对结构映射）。
	if opts.outDir != "" {
		return fmt.Errorf("--out-dir 将随目录发现（任务卡 1-3）一起实现")
	}
	target, err := pipeline.Commit(res.Artifact, sourceDir)
	if err != nil {
		return fmt.Errorf("提交生成文件: %w", err)
	}
	fmt.Fprintf(stdout, "已生成 %s\n", target)
	return nil
}

// findModuleRoot 从 start 向上找 go.mod。
func findModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod（%s 不在任何 Go module 内）", start)
		}
		dir = parent
	}
}

// packageDirName 读目录内任一 .go 文件的 package 子句作为包名。
//
// 目录没有 Go 文件时报错——生成文件必须落在真实 Go 包里（契约 §3.2：
// 输出目录必须能解析为明确的 Go package）。
func packageDirName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("读取目录 %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if name, ok := parsePackageName(data); ok {
			return name, nil
		}
	}
	return "", fmt.Errorf("目录 %s 中没有 Go 源文件，无法确定包名", dir)
}

// parsePackageName 从 Go 源码提取 package 子句（极简实现，够用即可）。
func parsePackageName(src []byte) (string, bool) {
	for _, line := range errors_(src) {
		line = trimSpace(line)
		if rest, ok := cutPrefix(line, "package "); ok && rest != "" {
			return rest, true
		}
	}
	return "", false
}

// ---- 轻量文本辅助（避免为三行解析引入 go/ast）----

// errors_ 把源码按行切出（名字带下划线避免与 errors 包冲突的阅读负担）。
func errors_(src []byte) []string {
	return splitLines(string(src))
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

// runGeneratePipeline 执行 generate 的运行阶段（1-1 的入口点，1-2 接真实管线）。
func runGeneratePipeline(opts *generateOptions, stdout, stderr io.Writer) error {
	if pipelineFunc == nil {
		return errors.New("internal: generate pipeline is nil")
	}
	return pipelineFunc(opts, stdout, stderr)
}
