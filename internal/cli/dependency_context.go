package cli

// ============================================================================
// PRODUCT-R5-B 非作者审修（片1）：真实有效构建上下文测量。
//
// 基线缺陷（DeepSeek 实测）：cacheKeyFor 的 BuildContext 只读
// `os.Getenv("GOOS"/"GOARCH"/"CGO_ENABLED")`。但 Go 的有效构建上下文还包含
// **GOENV 持久化值**（`go env -w` 写入 GOENV 文件）与 GO 工具默认值——这些
// 不出现在父进程环境变量里。实测：`GOENV=<file with GOOS=js>` 时
// `go env GOOS` = "js" 而 `os.Getenv("GOOS")` = ""。于是父 env 为空、而真实
// 上下文变化（GOENV 改 GOOS/GOARCH/CGO/GOFLAGS）时，键不变 → **跨上下文假命中**。
//
// 修复：用 `go env -json` 取 Go **实际生效**的上下文值（含 GOENV 持久化与默认），
// 确定性序列化后入键（K10）。约束：
//   - 受控子进程：显式 Go 路径（-GoExe），带超时（避免挂死）；
//   - 失败保守：go env 调用失败/输出不全/workspace 形态 → 返回 error，调用方置
//     dependencyUnsafe 强制 miss（绝不拿空上下文假命中）；
//   - 不改语言/协议版本；仅内部 cacheFormatVersion 随键变化升到 4。
//
// 说明：GOWORK 非 off（go.work 多模块）时当前单模块快照无法完整建模，保守判
// 「上下文不可测」→ miss（不猜跨模块依赖）。
// ============================================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// cacheContextTimeout 限制 `go env` 子进程时长；超时视为上下文不可测（保守 miss）。
const cacheContextTimeout = 10 * time.Second

// measuredCacheContext 返回 Go **实际生效**的构建上下文（确定性 JSON 串）。
//
// goExe 为空时用 "go"（依赖 PATH）；cli 会传 -GoExe 或解析到的 Go 路径。
// 返回 error 即「上下文不可测」，调用方必须保守 miss，不得用返回值当键。
func measuredCacheContext(goExe string, moduleRoots ...string) (string, error) {
	if goExe == "" {
		goExe = "go"
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheContextTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goExe, "env", "-json",
		"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK", "GOVERSION", "GOROOT")
	if len(moduleRoots) > 0 {
		cmd.Dir = moduleRoots[0]
	}
	// 显式禁用网络类工具链拉动：只读本地 env，不触发下载。
	cmd.Env = append(cmdEnvBase(), "GOTOOLCHAIN=local")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("go env 超时（%s）", cacheContextTimeout)
		}
		return "", fmt.Errorf("go env 失败: %w", err)
	}
	var values map[string]string
	if err := json.Unmarshal(out, &values); err != nil {
		return "", fmt.Errorf("解析 go env JSON: %w", err)
	}
	if values["GOOS"] == "" || values["GOARCH"] == "" {
		return "", fmt.Errorf("Go 构建上下文不完整（GOOS/GOARCH 为空）")
	}
	// go.work（多模块）当前单模块依赖快照无法完整建模：保守判不可测。
	if w := values["GOWORK"]; w != "" && w != "off" {
		return "", fmt.Errorf("检测到 go.work 工作区（%s），当前快照不支持跨模块建模", w)
	}
	// 只取对「文件选择/类型口径」有影响的稳定项，确定性序列化（map 经 json.Marshal
	// 键序确定）。GOVERSION 影响类型口径，纳入；GOFLAGS 影响 tag 选择，纳入。
	stable := map[string]string{
		"GOOS":        values["GOOS"],
		"GOARCH":      values["GOARCH"],
		"CGO_ENABLED": values["CGO_ENABLED"],
		"GOFLAGS":     values["GOFLAGS"],
		"GOVERSION":   values["GOVERSION"],
		"GOROOT":      values["GOROOT"],
	}
	b, err := json.Marshal(stable)
	if err != nil {
		return "", fmt.Errorf("序列化构建上下文: %w", err)
	}
	return string(b), nil
}

// cmdEnvBase 返回子进程基础环境：沿用父环境（Go 需要 PATH/HOME/GOCACHE 等）。
// 不主动注入 TMP/TEMP/HOME 等通用变量，只透传。
func cmdEnvBase() []string {
	return os.Environ()
}

// resolveGoExe 解析 Go 可执行路径：优先 PATH 上的 go，失败回退 "go"（让 go env
// 自行报错并走保守 miss）。不引入新公开 CLI 选项。
func resolveGoExe() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return "go"
}
