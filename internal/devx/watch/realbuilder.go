package watch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GenerateFunc 是 generate --check 的注入端口。**本包不 import internal/cli**（避免 cli↔watch
// 导入环，见 F-A6）。真实 CLI 生成由独立 adapter 包（internal/devx/watchadapter）提供实现，
// 其内部复用 internal/cli.Run。测试可注入 fake 统计次数。
type GenerateFunc func(ctx context.Context, projectDir string) (rc int, stderr string)

// BuildFunc 是 go build 入口的注入端口。生产实现用 go 命令（cwd=ProjectDir）；测试可替换。
type BuildFunc func(ctx context.Context, workDir string, args []string) (rc int, output string)

// RealBuilder 是 Builder 的真实实现：一次目录 `gxml generate --check` 生成+检查，再 `go build` 入口。
// 不另造生成器/缓存；区分 generate 失败（资产/生成）与 build 失败（入口编译）。
//
// 取消/超时：Build 以调用方 ctx 与 BuildTimeoutMS 的较短者驱动；ctx 取消即返回失败（Stage 分类），
// 不发布、不留后台生成（真实 generate 由其实现尊重 ctx；默认要求注入的 GenerateFunc 尊重 ctx）。
type RealBuilder struct {
	ProjectDir string
	EntryPkg   string // 相对入口包，如 "./app"；空则 "."
	OutBinary  string
	// Generate 必填（由 adapter 或测试注入，本包不内建 cli 依赖）。
	Generate GenerateFunc
	// BuildFn 可注入；nil 用真实 go build（cwd=ProjectDir）。
	BuildFn BuildFunc
	GoExe   string
	// BuildTimeout 是本代构建墙钟上限（0 取 Config 默认语义；由 Watch 传入更精确的 ctx 也可）。
	BuildTimeout time.Duration
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

func (b *RealBuilder) buildFn() BuildFunc {
	if b.BuildFn != nil {
		return b.BuildFn
	}
	return realGoBuild
}

// realGoBuild 在 **workDir（=ProjectDir）** 下执行 `go build -o <out> <entry>`，使相对入口与
// 有效构建上下文与真实工程一致（F-A2：此前未设 cwd，误编译 watch 库包）。尊重 ctx 取消/超时。
func realGoBuild(ctx context.Context, workDir string, args []string) (int, string) {
	if workDir == "" {
		return -1, "empty workDir"
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return -1, string(out) + "\ncanceled: " + ctx.Err().Error()
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(out)
		}
		return -1, string(out) + "\n" + err.Error()
	}
	return 0, string(out)
}

// Build 实现 Builder。generate 失败归类 "generate"；go build 失败归类 "build"；ctx 取消/超时按
// 所处分阶段分类并返回失败（不发布）。
func (b *RealBuilder) Build(ctx context.Context, gen int, changed []string) GenerationResult {
	if b.ProjectDir == "" {
		return GenerationResult{OK: false, Stage: "generate", Detail: "ProjectDir 必填", Gen: gen}
	}
	if b.Generate == nil {
		return GenerationResult{OK: false, Stage: "generate", Detail: "未注入 GenerateFunc（本包不内建 cli 依赖；请用 watchadapter 提供真实实现）", Gen: gen}
	}
	// 应用 BuildTimeout（若无则继承调用方 ctx）。
	buildCtx := ctx
	if b.BuildTimeout > 0 {
		var cancel context.CancelFunc
		buildCtx, cancel = context.WithTimeout(ctx, b.BuildTimeout)
		defer cancel()
	}
	if err := buildCtx.Err(); err != nil {
		return GenerationResult{OK: false, Stage: "generate", Detail: "canceled before build: " + err.Error(), Gen: gen}
	}

	// 1) generate --check
	rc, serr := b.Generate(buildCtx, b.ProjectDir)
	if buildCtx.Err() != nil {
		return GenerationResult{OK: false, Stage: "generate", Detail: "canceled/timeout: " + summarize(serr), Gen: gen}
	}
	if rc != 0 {
		return GenerationResult{OK: false, Stage: "generate", Detail: summarize(serr), Gen: gen}
	}
	// 2) go build 入口到本代唯一二进制（cwd=ProjectDir）
	outBin := b.OutBinary
	if outBin == "" {
		outBin = filepath.Join(b.ProjectDir, ".goxml", fmt.Sprintf("watch-bin-gen%d", gen))
		if err := os.MkdirAll(filepath.Dir(outBin), 0o755); err != nil {
			return GenerationResult{OK: false, Stage: "build", Detail: err.Error(), Gen: gen}
		}
	}
	args := []string{b.goBin(), "build", "-o", outBin, b.entry()}
	brc, bout := b.buildFn()(buildCtx, b.ProjectDir, args)
	if buildCtx.Err() != nil {
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
