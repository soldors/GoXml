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

// scopedUserOverlayForAnalyzer 构造 **Analyzer 阶段**的用户 overlay 快照（R22-A A2/R1/R2）。
//
// 背景：Analyzer 通过 packages.Load 的 Config.Overlay（go/packages 会把它转成一个
// `-overlay` JSON 传给 go list）加载源码。此前只看磁盘/只快照替换源，导致跨包/依赖
// 类型分叉（F1/F2），以及删除语义丢失（F5）。R2 修正 F5：
//   - 删除项（to==""）：若其 from 是**已加载包目录内的磁盘 Go 源**，用一个
//     `//go:build ignore` 的占位**替换字节**表达删除——使 go/packages 的 overlay
//     也把该文件排除出编译（与工具自有旧产物 ignore 同机制，Config.Overlay 无法直接表达
//     "文件不存在"，但可用 ignore 标签等效隐藏）。
//   - 替换项：仅当 Go 在用户 overlay 下**实际选中**该 from 时才读取替换源字节
//     （未选中：平台排除/无关/未用缺失 → 不读，F3 保留）。
//   - 新增虚拟源：Go 选中（出现在 -deps GoFiles）→ 读取其替换源字节并入。
//
// 契约：仍 compiler-owned、ModuleRoot 内、内存快照；未扩展正式 Analyzer 输入契约。
// 已证工具自有条目（staleGeneratedOverlay）在调用方合并时优先，不被本函数覆盖。
func scopedUserOverlayForAnalyzer(goExe, moduleRoot string, vmImportPaths []string, user map[string]string) (map[string][]byte, error) {
	if len(user) == 0 {
		return nil, nil
	}
	if goExe == "" {
		goExe = "go"
	}
	// 用户 overlay 的 Go JSON（from(abs)→to(abs) 或 ""删除），供权威 go list 选源。
	repl := make(map[string]string, len(user))
	for from, to := range user {
		repl[from] = to
	}
	selected := make(map[string]bool)   // Go 实际选中的绝对源文件
	loadedDirs := make(map[string]bool) // Go 实际加载的包目录（绝对、Clean）
	seenPath := make(map[string]bool)
	for _, ip := range vmImportPaths {
		if ip == "" || seenPath[ip] {
			continue
		}
		seenPath[ip] = true
		sel, dirs, err := goListDepSourceFiles(goExe, moduleRoot, ip, repl)
		if err != nil {
			return nil, err
		}
		for d := range sel {
			selected[d] = true
		}
		for d := range dirs {
			loadedDirs[d] = true
		}
	}
	out := make(map[string][]byte, len(user))
	for from, to := range user {
		full := filepath.Clean(toFullPath(moduleRoot, from))
		if to == "" {
			// 删除：仅对已加载包目录内、磁盘上确实存在的非测试 Go 源，用 ignore 占位隐藏。
			if loadedDirs[filepath.Dir(full)] && isDelGvable(full) {
				out[full] = []byte(ignoreStub)
			}
			continue
		}
		if !selected[full] {
			continue // 未被 Go 选中：不读取（F3：平台排除/无关/未用缺失）
		}
		data, rerr := os.ReadFile(toFullPath(moduleRoot, to))
		if rerr != nil {
			return nil, fmt.Errorf("读取用户 overlay 替换文件 %s: %w", toFullPath(moduleRoot, to), rerr)
		}
		out[full] = data
	}
	return out, nil
}

// ignoreStub 是表达"文件被排除编译"的占位内容（与工具自有旧产物 ignore 同机制）。
const ignoreStub = "//go:build ignore\n\npackage ignore\n"

// isDelGvable 判断删除项 from 是否为磁盘上存在的非测试 Go 源（只有这种才需隐藏）。
func isDelGvable(full string) bool {
	if filepath.Ext(full) != ".go" || strings.HasSuffix(full, "_test.go") {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && !info.IsDir()
}

// toFullPath 把（可能相对的）路径按 moduleRoot 归一化为绝对路径。
func toFullPath(moduleRoot, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(moduleRoot, filepath.FromSlash(p))
}

// goListDepSourceFiles 用 `go list -deps -e -json -overlay=<repl>` 从 importPath 出发，
// 返回该构建上下文中 Go 实际选中的**绝对源文件路径集合**（含 VM 包与依赖闭包、已按平台/标签筛选）。
func goListDepSourceFiles(goExe, moduleRoot, importPath string, repl map[string]string) (map[string]bool, map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), targetListTimeout)
	defer cancel()
	args := []string{"list", "-deps", "-e", "-json"}
	var overlayFile string
	if len(repl) > 0 {
		ovJSON, jerr := jsonMarshalOverlay(repl)
		if jerr != nil {
			return nil, nil, fmt.Errorf("序列化分析 overlay: %w", jerr)
		}
		f, ferr := os.CreateTemp("", "goxml-analyzer-overlay-*.json")
		if ferr != nil {
			return nil, nil, fmt.Errorf("写分析 overlay 文件: %w", ferr)
		}
		overlayFile = f.Name()
		if _, werr := f.Write(ovJSON); werr != nil {
			f.Close()
			os.Remove(overlayFile)
			return nil, nil, fmt.Errorf("写分析 overlay 文件: %w", werr)
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
			return nil, nil, fmt.Errorf("go list -deps -json 超时（%s）", targetListTimeout)
		}
		return nil, nil, fmt.Errorf("go list -deps -json 执行失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	pkgs, derr := decodeGoListJSON(out)
	if derr != nil {
		return nil, nil, derr
	}
	files := make(map[string]bool)
	dirs := make(map[string]bool)
	add := func(dir string, names []string) {
		for _, n := range names {
			files[filepath.Clean(filepath.Join(dir, n))] = true
		}
	}
	for _, pk := range pkgs {
		if pk.Dir == "" {
			continue
		}
		dirs[filepath.Clean(pk.Dir)] = true
		add(pk.Dir, pk.GoFiles)
		add(pk.Dir, pk.CgoFiles)
	}
	return files, dirs, nil
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
