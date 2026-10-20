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
	"strings"

	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// pipelineFunc 是 generate 执行阶段的函数形态，包级变量便于测试注入。
var pipelineFunc = runPipeline

// runPipeline 执行生成（1-3）：发现输入 → 逐文件 管线 → Commit。
//
// 决策落实（见 GENERATE-CLI-001-短检查记录 §6 与 1-3 边界冻结）：
//   - G2：SourceDir = 每个 .gxml 实际所在目录（单文件解析上下文）；
//   - 输入路径与输出路径**分开计算**：SourceDir 服务解析，--out-dir 服务布局；
//   - 多文件**逐文件提交、无批次事务**——部分失败的回滚归 1-5（G4 未冻结，
//     本切片明确不宣称原子性），失败前已成功的文件保持原样；
//   - G6：--check 暂与默认行为相同（L1+L2 已是默认路径）。
func runPipeline(opts *generateOptions, stdout, stderr io.Writer) error {
	if len(opts.paths) > 1 {
		// 多个独立输入参数仍不支持：批量形态是「一个目录」，
		// 多根发现的归并留待需要时再议（避免与相对路径映射规则纠缠）。
		return fmt.Errorf("当前支持一个输入（文件或目录）；多个独立输入参数未支持")
	}
	// 发现：单文件或目录递归，结果已按 rel 排序（确定性，G5）。
	inputs, err := discoverInputs(opts.paths[0])
	if err != nil {
		return err
	}

	// 输出根：默认与各文件同目录（契约 §3.2）；
	// --out-dir 时映射为 outRoot/rel（去掉 .gxml 换 _gen.go）。
	var outRoot string
	if opts.outDir != "" {
		outRoot, err = filepath.Abs(opts.outDir)
		if err != nil {
			return fmt.Errorf("解析输出根目录: %w", err)
		}
		if fi, err := os.Stat(outRoot); err == nil && !fi.IsDir() {
			return fmt.Errorf("输出根 %q 不是目录", outRoot)
		}
	}

	// 输出冲突检测：不同输入不得映射到同一目标文件。
	seen := make(map[string]string, len(inputs))

	okCount := 0
	for _, in := range inputs {
		// 输入侧：SourceDir 是该文件所在目录（G2）。
		moduleRoot, err := findModuleRoot(in.abs)
		if err != nil {
			return err
		}
		pkgName, err := packageDirName(in.sourceDir)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(in.abs)
		if err != nil {
			return fmt.Errorf("读取源文件: %w", err)
		}

		// 输出侧：与输入分开计算。
		// SourcePath 必须相对**目标包目录**（生成头与符号推导的基准）：
		// 默认输出 = 源目录 → SourcePath = 文件名；
		// --out-dir → 目标包目录 = outRoot/<rel 目录> → SourcePath = 文件名。
		targetDir := in.sourceDir
		if outRoot != "" {
			targetDir = filepath.Join(outRoot, filepath.FromSlash(filepath.Dir(in.rel)))
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				return fmt.Errorf("创建输出目录 %s: %w", targetDir, err)
			}
		}
		sourcePath := filepath.Base(in.abs)
		outputName := strings.TrimSuffix(sourcePath, ".gxml") + "_gen.go"
		target := filepath.Join(targetDir, outputName)
		if prev, dup := seen[target]; dup {
			return fmt.Errorf("输出冲突：%q 与 %q 都映射到 %s", prev, in.rel, target)
		}
		seen[target] = in.rel

		res := pipeline.Run(sourcePath, src, pipeline.Config{
			ModuleRoot:  moduleRoot,
			SourceDir:   in.sourceDir,
			PackageName: pkgName,
			SourcePath:  sourcePath,
		}, pipeline.Hooks{})

		for _, d := range res.Diags {
			fmt.Fprintf(stderr, "%s: %s: %s\n", d.File, d.Code, d.Message)
		}
		if !res.OK() {
			return fmt.Errorf("生成失败（%s，状态 %v，%d 条诊断）——已成功的文件保持原样，无批次回滚", in.rel, res.Status, len(res.Diags))
		}

		if _, err := pipeline.Commit(res.Artifact, targetDir); err != nil {
			return fmt.Errorf("提交生成文件: %w", err)
		}
		fmt.Fprintf(stdout, "已生成 %s\n", target)
		okCount++
	}

	if okCount > 1 {
		fmt.Fprintf(stdout, "共 %d 个文件生成成功（按 %s 顺序处理）\n", okCount, "路径排序")
	}
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
