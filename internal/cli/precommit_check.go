package cli

// ============================================================================
// R11-A 提交前检查引擎（落实既有 --check 合同）。
//
// 合同（cli.go 帮助与输出契约）：`--check` 在提交前执行 target package 的类型/构建检查。
// 本文件实现的检查**消费本批候选内容**：
//   - 未命中（miss）：用本阶段一刚生成、尚未落盘的内存字节（overlay 注入），
//     因此检查看的是**本批新内容**，不是磁盘上的旧产物；
//   - 命中（hit）：产物本次不重写，读取磁盘最终字节也显式加入候选 overlay；
//   - 组合对象是**真实目标包**（该目录下已有的用户源 + 本批候选），
//     不是只重复 Analyzer 的源 VM 检查。
//
// 检查方式：`go build`（仅编译/type-check），绝不 `go test`/`go run`、
// 不执行测试初始化或用户程序；子进程 cwd 保持 moduleRoot，全部编译输出落**自有临时目录**，
// 不在用户目录写任何东西（不临时覆盖用户 target 再恢复）。
//
// 失败 ⇒ 返回含真实 cause 的 error ⇒ runPipeline 在阶段二写盘前返回：
// 产物、Manifest、成功缓存记录都不落盘，且 --clean 尚未执行。
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// precommitCheckTimeout 是提交前检查子进程的上限（编译整批目标包）。
const precommitCheckTimeout = 180 * time.Second

// precommitBatchSize 是单次 `go build` 覆盖的目标包数上限（R13-A 有界分组）。
//
// 多包组用 Go 的空设备输出检查编译和链接。单包组使用自有临时输出路径。
const precommitBatchSize = 12

// precommitCandidate 是一个目标包在本批中的候选来源。
type precommitCandidate struct {
	// dir 是目标包目录（绝对路径）。
	dir string
	// output 是候选产物文件绝对路径（dir 下的 _gen.go）。
	output string
	// content 非 nil 表示本批新生成、尚未落盘的字节（未命中）；nil 表示命中，
	// 磁盘字节即本批最终内容。
	content []byte
}

// checkPrecommitCandidates 用有效 Go 环境对本批目标包做编译级检查（只编译）。
//
// goExe/环境沿用调用方已测量的权威上下文（生产不传 env 覆写 ⇒ 继承真实环境，
// 与 go env/go list/Analyzer 同一视角）；不复制宿主标签逻辑。
func checkPrecommitCandidates(goExe, moduleRoot string, cands []precommitCandidate, measuredFlags ...string) error {
	if len(cands) == 0 {
		return nil
	}
	if goExe == "" {
		goExe = "go"
	}
	flags := ""
	if len(measuredFlags) > 0 {
		flags = measuredFlags[0]
	} else {
		m, err := measureContextValues(goExe, moduleRoot)
		if err != nil {
			return fmt.Errorf("测量检查上下文失败: %w", err)
		}
		flags = m.GOFLAGS
	}

	// 1) 归并同一目录的多个候选（同包多页）。
	type dirGroup struct {
		dir      string
		contents map[string][]byte // output(abs) → 本批最终字节（包含命中）
	}
	order := make([]string, 0, len(cands))
	groups := make(map[string]*dirGroup, len(cands))
	for _, c := range cands {
		key := filepath.Clean(c.dir)
		g := groups[key]
		if g == nil {
			g = &dirGroup{dir: c.dir, contents: map[string][]byte{}}
			groups[key] = g
			order = append(order, key)
		}
		content := c.content
		if content == nil {
			b, err := os.ReadFile(c.output)
			if err != nil {
				return fmt.Errorf("读取命中候选产物 %s: %w", c.output, err)
			}
			content = b
		}
		g.contents[filepath.Clean(c.output)] = content
	}

	// 2) 自有临时工作区：overlay 替换文件、overlay JSON 与单包输出。
	workDir, err := os.MkdirTemp("", "goxml-precommit-")
	if err != nil {
		return fmt.Errorf("创建提交前检查临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 3) 组装所有最终候选。即使命中也显式覆盖用户 overlay 中同一路径，
	// 避免用户 overlay 的另一份内容替代本批最终磁盘产物而绕过检查。
	replace := make(map[string]string, len(cands))
	for i, key := range order {
		g := groups[key]
		for out, content := range g.contents {
			backing := filepath.Join(workDir, fmt.Sprintf("cand-%03d-%03d.go", i, len(replace)))
			if werr := os.WriteFile(backing, content, 0o644); werr != nil {
				return fmt.Errorf("写提交前检查候选文件失败: %w", werr)
			}
			replace[out] = backing
		}
	}
	if err := mergePrecommitUserOverlay(flags, moduleRoot, replace); err != nil {
		return err
	}
	overlayPath := ""
	if len(replace) > 0 {
		b, jerr := json.Marshal(map[string]any{"Replace": replace})
		if jerr != nil {
			return fmt.Errorf("序列化提交前检查 overlay: %w", jerr)
		}
		overlayPath = filepath.Join(workDir, "overlay.json")
		if werr := os.WriteFile(overlayPath, b, 0o644); werr != nil {
			return fmt.Errorf("写提交前检查 overlay: %w", werr)
		}
	}

	// Go overlay 支持虚拟目录：即使目标目录还不存在，也保留它**真实的 import path**，
	// 让跨候选引用与 internal/ 规则在最终身份上被检查，而不是换了路径。
	//
	fields, err := goFlagsSplit(flags)
	if err != nil {
		return fmt.Errorf("解析检查 GOFLAGS: %w", err)
	}
	buildMode := "default"
	for _, field := range fields {
		flag := strings.TrimPrefix(field, "-")
		if value, ok := strings.CutPrefix(strings.TrimPrefix(flag, "-"), "buildmode="); ok {
			buildMode = value
		}
	}
	batchSize := precommitBatchSize
	if buildMode != "default" {
		// Explicit build modes can require a single package. Keep the previous
		// per-package check for these contexts.
		batchSize = 1
	}
	for start := 0; start < len(order); start += batchSize {
		end := start + batchSize
		if end > len(order) {
			end = len(order)
		}
		pkgs := make([]string, 0, end-start)
		for _, key := range order[start:end] {
			pkgs = append(pkgs, groups[key].dir)
		}
		// Explicit -o overrides any caller output path in GOFLAGS. Go treats
		// its null device as a request to check without publishing a binary.
		outputFile := os.DevNull
		if len(pkgs) == 1 {
			outputFile = filepath.Join(workDir, fmt.Sprintf("out-%03d", start))
		}
		if err := precommitBuildFunc(goExe, moduleRoot, overlayPath, pkgs, outputFile); err != nil {
			return err
		}
	}

	return nil
}

// precommitBuildFunc 是执行一次 go build 检查的接缝（默认 runGoBuildCheck）。
// 存在的唯一理由与仓库既有 pipelineFunc 相同：让测试**统计构建调用次数**，用执行计数
// 证明有界分组真的减少了调用，而不是拿耗时推断。生产环境从不改写它。
var precommitBuildFunc = runGoBuildCheck

// runGoBuildCheck 执行一次 `go build`（仅编译），失败时把真实原因带回。
func runGoBuildCheck(goExe, workDir, overlayPath string, pkgs []string, outputFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), precommitCheckTimeout)
	defer cancel()
	args := []string{"build"}
	if outputFile != "" {
		args = append(args, "-o", outputFile)
	}
	if overlayPath != "" {
		args = append(args, "-overlay", overlayPath)
	}
	args = append(args, pkgs...)
	cmd := exec.CommandContext(ctx, goExe, args...)
	cmd.Dir = workDir
	// Retain the original GOFLAGS and GOENV so quoted values keep their
	// meaning. The explicit merged -overlay takes precedence over GOFLAGS.
	cmd.Env = append(cmdEnvBase(), "GOTOOLCHAIN=local")
	var stderr, stdout strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("提交前检查超时（%s）：目标包 %s 未在时限内完成编译: %w", precommitCheckTimeout, strings.Join(pkgs, " "), ctx.Err())
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = strings.TrimSpace(stdout.String())
	}
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Errorf("提交前检查失败：目标包未能通过编译（本批未写入任何产物/Manifest/缓存）\n%s: %w", detail, err)
}

// Preserve the effective user overlay (including GOENV-persisted flags), then
// give the newly generated candidates precedence for their own output paths.
func mergePrecommitUserOverlay(flags, moduleRoot string, candidates map[string]string) error {
	fields, err := goFlagsSplit(flags)
	if err != nil {
		return fmt.Errorf("解析检查GOFLAGS: %w", err)
	}
	path := ""
	for _, f := range fields {
		f = strings.TrimPrefix(f, "-")
		if strings.HasPrefix(f, "-overlay=") {
			path = strings.TrimPrefix(f, "-overlay=")
		} else if strings.HasPrefix(f, "overlay=") {
			path = strings.TrimPrefix(f, "overlay=")
		}
	}
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(moduleRoot, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取用户检查overlay: %w", err)
	}
	var cfg struct{ Replace map[string]string }
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("解析用户检查overlay: %w", err)
	}
	for from, to := range cfg.Replace {
		if !filepath.IsAbs(from) {
			from = filepath.Join(moduleRoot, from)
		}
		if to != "" && !filepath.IsAbs(to) {
			to = filepath.Join(moduleRoot, to)
		}
		if _, own := candidates[from]; !own {
			candidates[from] = to
		}
	}
	return nil
}
