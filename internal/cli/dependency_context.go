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

// measuredContextValues 是 `go env -json` 实测的有效构建上下文原始值。
// 全部来自 Go 自己（含 GOENV 持久化与默认值），不是父进程环境变量的投影。
type measuredContextValues struct {
	GOOS        string
	GOARCH      string
	CGO_ENABLED string
	GOFLAGS     string
	GOWORK      string
	GOVERSION   string
	GOROOT      string
	// R9-A S1：GOEXPERIMENT 是 Go 实际提供的 raw delta（用户显式增减，非启用全集）。
	// 与 CPU 特性一起构成「选文件身份」的必要输入（实验/架构默认集决定哪些文件被选中）；
	// 纳入 K10 缓存身份防跨实验假命中。语义等价的 raw 不强行归一（保守 miss 可接受）。
	GOEXPERIMENT string
	// CPU 特性（按 GOARCH 生效）：影响 go list 选文件与包名。
	GOAMD64   string
	GOARM     string
	GOARM64   string
	GO386     string
	GOMIPS    string
	GOMIPS64  string
	GOPPC64   string
	GORISCV64 string
	GOWASM    string
}

// measureContextValues 用受控子进程测量 Go 实际生效的构建上下文。
//
// envOverrides 追加在继承环境**之后**（后者胜出），供测试把 GOENV 指到
// 自有文件或显式 off；生产调用不传，让子进程看到与 go list/Analyzer
// 相同的有效配置。返回 error 即「上下文不可测」，调用方必须 fail-closed
// 或保守 miss，不得拿空值继续。
func measureContextValues(goExe string, moduleRoot string, envOverrides ...string) (measuredContextValues, error) {
	if goExe == "" {
		goExe = "go"
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheContextTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goExe, "env", "-json",
		"GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK", "GOVERSION", "GOROOT", "GOEXPERIMENT",
		"GOAMD64", "GOARM", "GOARM64", "GO386", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM")
	if moduleRoot != "" {
		cmd.Dir = moduleRoot
	}
	// 显式禁用网络类工具链拉动：只读本地 env，不触发下载。
	cmd.Env = append(cmdEnvBase(), "GOTOOLCHAIN=local")
	cmd.Env = append(cmd.Env, envOverrides...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return measuredContextValues{}, fmt.Errorf("go env 超时（%s）", cacheContextTimeout)
		}
		return measuredContextValues{}, fmt.Errorf("go env 失败: %w", err)
	}
	var values map[string]string
	if err := json.Unmarshal(out, &values); err != nil {
		return measuredContextValues{}, fmt.Errorf("解析 go env JSON: %w", err)
	}
	if values["GOOS"] == "" || values["GOARCH"] == "" {
		return measuredContextValues{}, fmt.Errorf("Go 构建上下文不完整（GOOS/GOARCH 为空）")
	}
	// Workspace does not invalidate measured package-selection values.
	// Its dependency closure is rejected separately by cacheString, not by generation.
	return measuredContextValues{
		GOOS:         values["GOOS"],
		GOARCH:       values["GOARCH"],
		CGO_ENABLED:  values["CGO_ENABLED"],
		GOFLAGS:      values["GOFLAGS"],
		GOWORK:       values["GOWORK"],
		GOVERSION:    values["GOVERSION"],
		GOROOT:       values["GOROOT"],
		GOEXPERIMENT: values["GOEXPERIMENT"],
		GOAMD64:      values["GOAMD64"],
		GOARM:        values["GOARM"],
		GOARM64:      values["GOARM64"],
		GO386:        values["GO386"],
		GOMIPS:       values["GOMIPS"],
		GOMIPS64:     values["GOMIPS64"],
		GOPPC64:      values["GOPPC64"],
		GORISCV64:    values["GORISCV64"],
		GOWASM:       values["GOWASM"],
	}, nil
}

// measuredCacheContext 返回 Go **实际生效**的构建上下文（确定性 JSON 串）。
//
// goExe 为空时用 "go"（依赖 PATH）；cli 会传 -GoExe 或解析到的 Go 路径。
// 返回 error 即「上下文不可测」，调用方必须保守 miss，不得用返回值当键。
func measuredCacheContext(goExe string, moduleRoots ...string) (string, error) {
	var root string
	if len(moduleRoots) > 0 {
		root = moduleRoots[0]
	}
	m, err := measureContextValues(goExe, root)
	if err != nil {
		return "", err
	}
	return m.cacheString()
}

// cacheString 把实测值序列化为确定性的稳定子集 JSON（K10 的键分量）。
// 只取对「文件选择/类型口径」有影响的项：GOOS/GOARCH/CGO_ENABLED/GOFLAGS/
// GOVERSION/GOROOT。GOFLAGS 影响标签选择（含 GOENV 持久化值），纳入。
func (m measuredContextValues) cacheString() (string, error) {
	if m.GOWORK != "" && m.GOWORK != "off" {
		return "", fmt.Errorf("检测到 go.work 工作区（%s），当前快照不支持跨模块建模", m.GOWORK)
	}
	stable := map[string]string{
		"GOOS":        m.GOOS,
		"GOARCH":      m.GOARCH,
		"CGO_ENABLED": m.CGO_ENABLED,
		"GOFLAGS":     m.GOFLAGS,
		"GOVERSION":   m.GOVERSION,
		"GOROOT":      m.GOROOT,
		// R9-A S1：实验 raw delta + CPU 特性进入 K10（决定 go list 选文件身份）。
		"GOEXPERIMENT": m.GOEXPERIMENT,
		"GOAMD64":      m.GOAMD64,
		"GOARM":        m.GOARM,
		"GOARM64":      m.GOARM64,
		"GO386":        m.GO386,
		"GOMIPS":       m.GOMIPS,
		"GOMIPS64":     m.GOMIPS64,
		"GOPPC64":      m.GOPPC64,
		"GORISCV64":    m.GORISCV64,
		"GOWASM":       m.GOWASM,
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
