package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// Only current, cache-missed, manifest-owned outputs with a matching Source
// header may be hidden from this load. All disk changes remain in the existing
// commit transaction; failures keep both old products and their manifest.
func staleGeneratedOverlay(root string, man *manifest, rs []*resolvedInput) (map[string][]byte, error) {
	// Excluding all owned files while regenerating only one page could leave a
	// second, stale package on disk. Refuse that partial switch before writing.
	for _, e := range man.Entries {
		for _, r := range rs {
			sameDir, err := sameProjectFile(root, filepath.ToSlash(filepath.Dir(e.Output)), filepath.ToSlash(filepath.Dir(r.outputRel)), os.Stat)
			if err != nil {
				return nil, err
			}
			if !sameDir {
				continue
			}
			path := filepath.Join(root, filepath.FromSlash(e.Output))
			data, exists, err := readOutput(path)
			if err != nil {
				return nil, err
			}
			if !exists || !codegen.HasProtectedHeader(data) || !headerSourceMatches(data, filepath.Base(filepath.FromSlash(e.Source))) {
				continue
			}
			name, ok := parsePackageName(data)
			if !ok || name == r.pkgName {
				continue
			}
			scheduled := false
			for _, other := range rs {
				same, err := sameProjectFile(root, e.Output, other.outputRel, os.Stat)
				if err != nil {
					return nil, err
				}
				if same {
					scheduled = true
					break
				}
			}
			if !scheduled {
				return nil, fmt.Errorf("构建上下文改变后 %s 仍属于旧 package %s；请将同包源文件一并生成，当前批次未写入", e.Output, name)
			}
		}
	}
	var overlay map[string][]byte
	for _, r := range rs {
		e, ok := manifestEntryOf(man, r.sourceRel)
		if !ok {
			continue
		}
		same, err := sameProjectFile(root, e.Output, r.outputRel, os.Stat)
		if err != nil {
			return nil, err
		}
		if !same {
			continue
		}
		data, exists, err := readOutput(r.target)
		if err != nil {
			return nil, err
		}
		if !exists || !codegen.HasProtectedHeader(data) || !headerSourceMatches(data, r.sourcePath) {
			continue
		}
		name, ok := parsePackageName(data)
		if !ok || name == r.pkgName {
			continue
		}
		if overlay == nil {
			overlay = make(map[string][]byte)
		}
		overlay[r.target] = []byte(fmt.Sprintf("//go:build ignore\n\npackage %s\n", r.pkgName))
	}
	return overlay, nil
}

// scopedUserOverlayForAnalyzer 构造 **Analyzer 阶段**的用户 overlay 字节快照（R22-A A2/R1）。
//
// 背景：Analyzer 通过 packages.Load 的 Config.Overlay 只接收 staleGeneratedOverlay
// （工具自有旧产物 ignore），不含用户 GOFLAGS/GOENV 的 -overlay，于是类型/方法检查
// 按磁盘进行，与 go build/--check/定名口径不一致（跨包 VM 假接受、依赖闭包假接受、
// 新增方法假拒绝）。
//
// 修正（R1，按 Go 权威选源）：
//   - 不再按目录无差别预读；先对每个 VM 包导入路径执行一次
//     `go list -deps -e -json -overlay=<用户overlay>`，得到该构建上下文下 Go **实际选中**
//     的全部源文件（含 VM 包与依赖闭包、已按 GOOS/GOARCH/构建标签筛选）。
//   - 只读取**被 Go 选中**且属于用户 overlay 的 `from` 的替换源字节，并入 Analyzer 快照。
//   - 未被选中的映射（非 Go 的 notes.txt、被平台排除的 platform_linux.go、无关目录）不读取，
//     保持 R21「未使用缺失映射由 Go 决定」边界；**被选中**的替换源缺失 → 返回错误（诚实失败）。
//
// 契约：仍为 compiler-owned、ModuleRoot 内、内存快照；用户 overlay 源也在 ModuleRoot 内，
// 由 analyzer.Config.Validate 的 scope 校验兜底；未扩展正式 Analyzer 输入契约。
func scopedUserOverlayForAnalyzer(goExe, moduleRoot string, vmImportPaths []string, user map[string]string) (map[string][]byte, error) {
	if len(user) == 0 {
		return nil, nil
	}
	if goExe == "" {
		goExe = "go"
	}
	// 测试专用变异钩子（默认空；仅供 overlay_r1_mutation_test.go 覆盖率量退化路径）。
	switch r1MutationMode {
	case "skip-go-selection": // 退化①：完全不给 Analyzer 用户 overlay（复现 F1/F2 假接受：看磁盘）
		return nil, nil
	case "ignore-selection-filter": // 退化②：读全部映射（复现 F3 预读误拒绝）
		out := make(map[string][]byte, len(user))
		for from, to := range user {
			if to == "" {
				continue
			}
			ff := from
			if !filepath.IsAbs(ff) {
				ff = filepath.Join(moduleRoot, filepath.FromSlash(ff))
			}
			d, rerr := os.ReadFile(toFullPath(moduleRoot, to))
			if rerr != nil {
				return nil, fmt.Errorf("读取用户 overlay 替换文件 %s: %w", toFullPath(moduleRoot, to), rerr)
			}
			out[filepath.Clean(ff)] = d
		}
		return out, nil
	}
	// 构造用户 overlay 的 Go JSON（from(abs)→to(abs) 或 ""删除），供权威 go list 选源。
	repl := make(map[string]string, len(user))
	for from, to := range user {
		repl[from] = to
	}
	// selected 收集 Go 实际选中的绝对源文件路径。
	selected := make(map[string]bool)
	seenPath := make(map[string]bool)
	for _, ip := range vmImportPaths {
		if ip == "" || seenPath[ip] {
			continue
		}
		seenPath[ip] = true
		dirs, err := goListDepSourceFiles(goExe, moduleRoot, ip, repl)
		if err != nil {
			return nil, err
		}
		for d := range dirs {
			selected[d] = true
		}
	}
	out := make(map[string][]byte, len(user))
	for from, to := range user {
		full := from
		if !filepath.IsAbs(full) {
			full = filepath.Join(moduleRoot, filepath.FromSlash(full))
		}
		full = filepath.Clean(full)
		if !selected[full] {
			continue // 未被 Go 选中：不读取（notes.txt / 平台排除 / 无关目录 / 未用缺失）
		}
		if to == "" {
			continue // 删除语义：Go 侧处理；Analyzer overlay 不提供字节
		}
		toFull := to
		if !filepath.IsAbs(toFull) {
			toFull = filepath.Join(moduleRoot, filepath.FromSlash(toFull))
		}
		data, rerr := os.ReadFile(toFull)
		if rerr != nil {
			return nil, fmt.Errorf("读取用户 overlay 替换文件 %s: %w", toFull, rerr)
		}
		out[full] = data
	}
	return out, nil
}

// goListDepSourceFiles 用 `go list -deps -e -json -overlay=<repl>` 从 importPath 出发，
// 返回该构建上下文中 Go 实际选中的**绝对源文件路径集合**（含 VM 包与依赖闭包、已按平台/标签筛选）。
func goListDepSourceFiles(goExe, moduleRoot, importPath string, repl map[string]string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), targetListTimeout)
	defer cancel()
	args := []string{"list", "-deps", "-e", "-json"}
	var overlayFile string
	if len(repl) > 0 {
		ovJSON, jerr := jsonMarshalOverlay(repl)
		if jerr != nil {
			return nil, fmt.Errorf("序列化分析 overlay: %w", jerr)
		}
		f, ferr := os.CreateTemp("", "goxml-analyzer-overlay-*.json")
		if ferr != nil {
			return nil, fmt.Errorf("写分析 overlay 文件: %w", ferr)
		}
		overlayFile = f.Name()
		if _, werr := f.Write(ovJSON); werr != nil {
			f.Close()
			os.Remove(overlayFile)
			return nil, fmt.Errorf("写分析 overlay 文件: %w", werr)
		}
		f.Close()
		defer os.Remove(overlayFile)
		args = append(args, "-overlay", overlayFile)
	}
	args = append(args, importPath)
	cmd := exec.CommandContext(ctx, goExe, args...)
	if moduleRoot != "" {
		cmd.Dir = moduleRoot
	}
	cmd.Env = append(cmdEnvBase(), "GOTOOLCHAIN=local")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("go list -deps -json 超时（%s）", targetListTimeout)
		}
		return nil, fmt.Errorf("go list -deps -json 执行失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	pkgs, derr := decodeGoListJSON(out)
	if derr != nil {
		return nil, derr
	}
	files := make(map[string]bool)
	add := func(dir string, names []string) {
		for _, n := range names {
			files[filepath.Clean(filepath.Join(dir, n))] = true
		}
	}
	for _, pk := range pkgs {
		if pk.Dir == "" {
			continue
		}
		add(pk.Dir, pk.GoFiles)
		add(pk.Dir, pk.CgoFiles)
	}
	return files, nil
}

// vmImportPathOf 计算输入 r 的 ViewModel 包导入路径（供 go list -deps 权威选源）。
//
// 同包（声明无 "pkg." 前缀）→ 源目录包；跨包（如 model.VM）→ modulePath/model。
// 无法解析/无 ViewModel → 空（不参与，保持既有保守）。
func vmImportPathOf(r *resolvedInput) string {
	decl, err := dependencyVM(r.source)
	if err != nil || decl == "" {
		return ""
	}
	dir, ok := typeExprDir(decl)
	if !ok {
		return ""
	}
	if dir == "" {
		// 同包：源目录相对 moduleRoot 的导入路径。
		rel, rerr := filepath.Rel(r.moduleRoot, r.sourceDir)
		if rerr != nil {
			return ""
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "" {
			return r.modulePath
		}
		return r.modulePath + "/" + rel
	}
	return r.modulePath + "/" + dir
}

// r1MutationMode 是测试专用变异开关（默认空）。仅用于覆盖率量退化路径，不参与生产逻辑。
var r1MutationMode = ""

func toFullPath(moduleRoot, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(moduleRoot, filepath.FromSlash(p))
}
