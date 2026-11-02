// GOFLAGS 构建上下文：让包名判定与 `go build` / `go list` / Analyzer 消费
// 同一份 `-tags`，修掉「CLI 用 build.Default（不读 GOFLAGS）选文件、
// 而 go 用带 tag 的集合编译」导致产物包名与真实包名不一致的问题。
//
// 为什么不自己写标签表达式、也不用 strings.Fields：
//   - GOFLAGS 的权威切词是 cmd/internal/quoted.Split——它认单/双引号包裹
//     整个字段（例如 `"-tags=a b"`，引号必须位于字段起首），而
//     strings.Fields 会把它错切成两段。该包是 cmd 内部包，不能 import，
//     所以下面 goFlagsSplit 逐行复刻其算法，并保证行为一致。
//   - `-tags` 值的解析复刻 cmd/go/internal/work.tagsFlag.Set 与
//     cmd/go/internal/base.StringsFlag.Set：值里含空格或单引号 → 走
//     quoted.Split；否则按逗号切、忽略空串。每次 Set 都重置切片，所以
//     重复出现的 `-tags` 以最后一次为准。
//   - flag 形状校验复刻 base.InitGOFLAGS：非 `-x`/`--x`/`-x=v`/`--x=v`
//     形态（含裸 `-`、`--`、`---*`、`-=*`、`--=*`）与未闭合引号一律报错，
//     调用方据此 fail-closed，绝不静默降级到 build.Default。
//
// 边界（如实说明，不过度承诺）：go 还会用命令自身的 flag 注册表校验
// `-tags` 之类是否为「该命令已知 flag」（InitGOFLAGS 的 hasFlag）。CLI 只
// 关心哪些 build tag 参与文件选择，无法在不 import cmd 内部包的前提下复制
// 那张注册表，因此**不**照搬未知 flag 拒绝——未知 flag 与文件选择无关，
// 忽略它们与 go 的 SetFromGOFLAGS「对本命令未知 flag 选择忽略」一致。
package cli

import (
	"fmt"
	"go/build"
	"strings"
)

// isSpaceByte 复刻 cmd/internal/quoted.isSpaceByte：空格 / 制表 / 换行 / 回车。
func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// goFlagsSplit 复刻 cmd/internal/quoted.Split：按空白切词，允许单或双引号
// 包裹整个字段（字段内不再做任何转义），引号出现在字段中间不算引号。
// 未闭合的引号返回 error。返回值与 go 对同串 GOFLAGS 的切分逐元素一致。
func goFlagsSplit(s string) ([]string, error) {
	var f []string
	for len(s) > 0 {
		for len(s) > 0 && isSpaceByte(s[0]) {
			s = s[1:]
		}
		if len(s) == 0 {
			break
		}
		// 引号包裹的字段：无转义，取到匹配的引号为止。
		if s[0] == '"' || s[0] == '\'' {
			quote := s[0]
			s = s[1:]
			i := 0
			for i < len(s) && s[i] != quote {
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			f = append(f, s[:i])
			s = s[i+1:]
			continue
		}
		// 裸字段：取到下一个空白为止。
		i := 0
		for i < len(s) && !isSpaceByte(s[i]) {
			i++
		}
		f = append(f, s[:i])
		s = s[i:]
	}
	return f, nil
}

// tagsFromGOFLAGS 从 GOFLAGS 串里提取参与构建的 build tags，语义与
// go 命令一致：先按 quoted.Split 切词并做 flag 形状校验（非法即 error），
// 再对每个 `-tags=v` / `--tags=v` 用 tagsFlag.Set 的规则解析其值。
// 重复 `-tags` 以最后一次为准（每次解析重置）。不含 `-tags` 时返回空切片、
// nil error——这与「GOFLAGS 为空」等价，调用方据此保持既有行为。
func tagsFromGOFLAGS(goflags string) ([]string, error) {
	if goflags == "" {
		return nil, nil
	}
	fields, err := goFlagsSplit(goflags)
	if err != nil {
		return nil, fmt.Errorf("parsing $GOFLAGS: %w", err)
	}
	var tags []string
	for _, f := range fields {
		// flag 形状校验：复刻 base.InitGOFLAGS 对每个 token 的检查。
		if !strings.HasPrefix(f, "-") || f == "-" || f == "--" ||
			strings.HasPrefix(f, "---") || strings.HasPrefix(f, "-=") || strings.HasPrefix(f, "--=") {
			return nil, fmt.Errorf("parsing $GOFLAGS: non-flag %q", f)
		}
		name := f[1:]
		if name[0] == '-' {
			name = name[1:]
		}
		var value string
		hasValue := false
		if i := strings.Index(name, "="); i >= 0 {
			value = name[i+1:]
			hasValue = true
			name = name[:i]
		}
		if name != "tags" {
			// 与文件选择无关的 flag（-v、-mod=…、未知 flag 等）忽略。
			continue
		}
		if !hasValue {
			// 裸 -tags 无值：go 报「flag needs an argument」，此处 fail-closed。
			return nil, fmt.Errorf("parsing $GOFLAGS: flag needs an argument: -tags")
		}
		// tagsFlag.Set：值含空格或单引号 → 走 quoted.Split；否则逗号切、忽略空串。
		// 每次覆盖 tags，保证重复 -tags 以最后一次为准。
		parsed, err := parseTagValue(value)
		if err != nil {
			return nil, fmt.Errorf("parsing $GOFLAGS for -tags: %w", err)
		}
		tags = parsed
	}
	return tags, nil
}

// parseTagValue 复刻 cmd/go/internal/work.tagsFlag.Set 对单个 -tags 值的解析。
func parseTagValue(value string) ([]string, error) {
	// 兼容 Go 1.12 及更早："-tags='a b'" 甚至 "-tags='a'" 允许空格/引号形态。
	if strings.Contains(value, " ") || strings.Contains(value, "'") {
		parts, err := goFlagsSplit(value)
		if err != nil {
			return nil, err
		}
		if parts == nil {
			parts = []string{}
		}
		return parts, nil
	}
	// 逗号切分，忽略空串。
	out := []string{}
	for _, s := range strings.Split(value, ",") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// buildContextFromMeasured 从 go env **实测值**构造与 go build/go list/
// Analyzer 一致的构建上下文（R7-B B1：最小统一 helper）。
//
// 为什么不能再用 buildContextFromGOFLAGS(os.Getenv("GOFLAGS"))（已删除）：
// 它以 build.Default 为基础、只读**进程环境**的 GOFLAGS/GOOS/GOARCH。
// 而 go 自身还消费 **GOENV 持久化值**（`go env -w` 写入的文件）——
// 真实复现（night-product-r7/zcode/raw/repro-b1）：GOENV 文件写
// `GOFLAGS=-tags=goxmlr3` 时 `go list` 选 pkgtag，CLI 却生成
// `package pkgbase`（rc=0 静默分叉）；GOENV 写 `GOOS=linux` 时同理
// （go list=pkglinux，CLI=package pkgwin）。缓存侧 K10（dependency_context.go）
// 早在 R5 就用 go env 实测——同一有效上下文，两条实现两个视图。
//
// 本函数与 K10 同源：输入是 measureContextValues 的实测值，
// GOOS/GOARCH/CGO_ENABLED 直接采用（不再信 build.Default 的环境投影），
// 仅把 GOFLAGS 里的 -tags 合并进 BuildTags（复用 tagsFromGOFLAGS，
// 引号/重复/非法语义不变）。GOFLAGS 非法时返回 error，绝不返回
// 一个「假装成功」的上下文。
//
// ReleaseTags/ToolTags 沿用 build.Default：文件选择用到的 go1.x 版本
// 标签由本进程 Go 版本决定，与子进程是同一工具链（goExe 解析自本机），
// GOVERSION 差异已由 K10 入键兜底。
func buildContextFromMeasured(m measuredContextValues) (build.Context, error) {
	ctxt := build.Default
	if m.GOOS != "" {
		ctxt.GOOS = m.GOOS
	}
	if m.GOARCH != "" {
		ctxt.GOARCH = m.GOARCH
	}
	switch m.CGO_ENABLED {
	case "0":
		ctxt.CgoEnabled = false
	case "1":
		ctxt.CgoEnabled = true
	}
	tags, err := tagsFromGOFLAGS(m.GOFLAGS)
	if err != nil {
		return build.Context{}, err
	}
	if len(tags) > 0 {
		ctxt.BuildTags = append([]string{}, tags...)
	}
	return ctxt, nil
}
