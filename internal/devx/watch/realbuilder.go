package watch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goxml/goxml/internal/cli"
)

// GenerateFunc 是 generate --check 的注入端口。生产实现复用 internal/cli.Run。
// 返回 observed rc 与 stderr 摘要（不吞错）。
type GenerateFunc func(ctx context.Context, projectDir string) (rc int, stderr string)

// BuildFunc 是 go build 入口的注入端口。生产实现用 go 命令；测试可替换为 fake 以统计次数。
type BuildFunc func(ctx context.Context, args []string) (rc int, output string)

// RealBuilder 是 Builder 的真实实现：一次目录 `gxml generate --check` 生成+检查，再 `go build` 入口。
// 不另造生成器/缓存；区分 generate 失败（资产/生成）与 build 失败（入口编译）。
type RealBuilder struct {
	// ProjectDir 是模块根。
	ProjectDir string
	// EntryPkg 是被构建的入口包（如 "./app"）。空则默认 "."。
	EntryPkg string
	// OutBinary 是新代二进制输出路径（每代唯一，由调用方按 gen 生成）。空则用 ProjectDir/.goxml/watch-bin。
	OutBinary string
	// Generate/Build 可注入；nil 用真实实现。
	Generate GenerateFunc
	BuildFn  BuildFunc

	// goExe 是 go 可执行名（默认 "go"）。
	GoExe string
}

func (b *RealBuilder) goBin() string {
	if b.GoExe != "" {
		return b.GoExe
	}
	return "go"
}

func (b *RealBuilder) entry() string {
	if b.EntryPkg != "" {
		return b.EntryPkg
	}
	return "."
}

func (b *RealBuilder) generate() GenerateFunc {
	if b.Generate != nil {
		return b.Generate
	}
	return realGenerate
}

func (b *RealBuilder) build() BuildFunc {
	if b.BuildFn != nil {
		return b.BuildFn
	}
	return realGoBuild
}

// realGenerate 复用 internal/cli.Run 执行目录 generate --check（真实 CLI，不重造）。
// 在**自身 goroutine 内**运行，配合 ctx 超时；stdout/stderr 捕获到缓冲。
func realGenerate(ctx context.Context, projectDir string) (int, string) {
	var stdout, stderr strings.Builder
	done := make(chan int, 1)
	go func() {
		rc := cli.Run([]string{"generate", projectDir, "--check"}, &stdout, &stderr)
		done <- rc
	}()
	select {
	case rc := <-done:
		return rc, stderr.String()
	case <-ctx.Done():
		return -1, "generate canceled/timeout: " + ctx.Err().Error()
	}
}

// realGoBuild 执行 `go build -o <out> <entry>`，从 env 读取 PWD 由调用方通过 args 给出。
func realGoBuild(ctx context.Context, args []string) (int, string) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		return -1, string(out) + "\n" + err.Error()
	}
	return 0, string(out)
}

// Build 实现 Builder：generate 失败归类 "generate"；go build 失败归类 "build"。成功返回 OK。
func (b *RealBuilder) Build(ctx context.Context, gen int, changed []string) GenerationResult {
	if b.ProjectDir == "" {
		return GenerationResult{OK: false, Stage: "generate", Detail: "ProjectDir 必填", Gen: gen}
	}
	// 1) generate --check
	rc, serr := b.generate()(ctx, b.ProjectDir)
	if ctx.Err() != nil {
		return GenerationResult{OK: false, Stage: "generate", Detail: "canceled/timeout: " + summarize(serr), Gen: gen}
	}
	if rc != 0 {
		return GenerationResult{OK: false, Stage: "generate", Detail: summarize(serr), Gen: gen}
	}
	// 2) go build 入口到本代唯一二进制
	outBin := b.OutBinary
	if outBin == "" {
		outBin = filepath.Join(b.ProjectDir, ".goxml", fmt.Sprintf("watch-bin-gen%d", gen))
		if err := os.MkdirAll(filepath.Dir(outBin), 0o755); err != nil {
			return GenerationResult{OK: false, Stage: "build", Detail: err.Error(), Gen: gen}
		}
	}
	args := []string{b.goBin(), "build", "-o", outBin, b.entry()}
	brc, bout := b.build()(ctx, args)
	if ctx.Err() != nil {
		return GenerationResult{OK: false, Stage: "build", Detail: "canceled/timeout: " + summarize(bout), Gen: gen}
	}
	if brc != 0 {
		return GenerationResult{OK: false, Stage: "build", Detail: summarize(bout), Gen: gen}
	}
	return GenerationResult{OK: true, Gen: gen, Detail: outBin}
}

// summarize 截断诊断文本，避免超长日志（保留尾部关键行）。
func summarize(s string) string {
	s = strings.TrimSpace(s)
	const max = 2000
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max:]
}
