package cli

// ============================================================================
// PRODUCT-R5-B：源 VM 依赖闭包指纹（缓存键 K8）。
//
// 缺陷（真实 CLI 实测，repro-b1）：缓存键 K3 只指纹**目标包**目录。
// 跨包输出（.gxml 在 ui/、VM 类型在 vm/）时，改坏 vm/vm.go 的方法签名，
// 下一次 generate 仍缓存命中（rc=0），旧产物不变——类型错误被推迟到
// 用户 go build 才暴露（实测 rc=1, cannot use string as int）。
//
// 修复方向（任务书 B1/B2）：把「声明 VM 类型的包 + 其本模块传递闭包 +
// 模块身份（go.mod/go.sum）」指纹进缓存键（K8）。闭包按 import 收集、
// 有界（visited 集合 + 目录/文件上限），不遍历仓外、不跟随 symlink、
// 不联网；每个输入的依赖包只扫一次，本批共用产物排除集。
//
// 【候选】诊断不变：依赖指纹只影响**键**，让失效走到正常 Analyzer 路径；
// 不新增诊断编号，不改失败阶段。
// ============================================================================

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

// 依赖闭包的有界上限（防御病态工程；正常项目远低于此）。
const (
	maxDependencyDirs  = 64
	maxDependencyFiles = 4096
)

// readModulePath 从 go.mod 读取 module 路径（B2 模块身份的组成部分）。
func readModulePath(moduleRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(moduleRoot, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("读取 go.mod: %w", err)
	}
	path := modfile.ModulePath(data)
	if path == "" {
		return "", fmt.Errorf("go.mod 缺少 module 行")
	}
	return path, nil
}

// Read only the actual root attribute. A regex can miss legal quoting or match
// a commented-out root and thereby mistake a dynamic page for a static one.
func dependencyVM(source []byte) (string, error) {
	d := xml.NewDecoder(bytes.NewReader(source))
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		if root, ok := tok.(xml.StartElement); ok {
			if root.Name.Local != "Goxml" {
				return "", fmt.Errorf("unexpected root")
			}
			for _, attr := range root.Attr {
				if attr.Name.Local == "ViewModel" {
					return attr.Value, nil
				}
			}
			return "", nil
		}
	}
}

// The bounded snapshot covers a single real module directory, never a linked
// directory or a nested module. Unprovable paths must disable reuse, not merely
// contribute a stable sentinel that can authorize the same partial snapshot.
func dependencyDirectorySafe(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return false
	}
	for p := root; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return false
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	p := root
	if rel == "." {
		return true
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, part)
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return false
		}
		if _, e := os.Stat(filepath.Join(p, "go.mod")); e == nil || !os.IsNotExist(e) {
			return false
		}
	}
	return true
}

// typeExprDir 按冻结文法（load.go §4.2：PkgDir "." Ident，PkgDir := Ident ("/" Ident)*）
// 把 ViewModel 声明拆成「包目录（模块根相对）」与「类型名」。
//
// 返回 ok=false 表示形态无法解析（由调用方做保守处理）。
func typeExprDir(decl string) (dir string, ok bool) {
	decl = strings.TrimSpace(decl)
	if decl == "" {
		return "", false
	}
	i := strings.LastIndexByte(decl, '.')
	if i < 0 {
		return "", true // 同包形态：与 .gxml 同包
	}
	dir, name := decl[:i], decl[i+1:]
	if dir == "" || name == "" {
		return "", false
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == "" || !isGoIdentifier(seg) {
			return "", false
		}
	}
	if !isGoIdentifier(name) {
		return "", false
	}
	return dir, true
}

func isGoIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		alpha := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := r >= '0' && r <= '9'
		if i == 0 && !alpha {
			return false
		}
		if i > 0 && !alpha && !digit {
			return false
		}
	}
	return true
}

// collectDependencySources 收集 K8：声明 VM 类型的包与其本模块传递闭包的
// 源码指纹，外加模块身份（go.mod/go.sum）。
//
//   - .gxml 无 ViewModel 声明 → 空集（纯静态页没有 VM 依赖，空集是正确语义）；
//   - 声明无法解析/缺目录/超限/链接/外部模块 → 禁用复用，正常 Analyzer 处理；
//   - 哨兵只解释快照内容，不能把固定哨兵当作缓存复用凭证；
//   - exclude 与 K3 相同（Manifest 产物 ∪ 本批 target），不自噬产物；
//   - import 闭包只收本模块包目录，不遍历仓外、不跟 symlink；
//     非标准库的外部模块（含本地 replace）未完整建模，保守禁用复用。
func collectDependencySources(r *resolvedInput, modulePath string, exclude map[string]bool) ([]cachePackageSource, error) {
	decl, parseErr := dependencyVM(r.source)
	if parseErr != nil {
		r.dependencyUnsafe = true
		return nil, nil
	}
	if decl == "" {
		// 无 ViewModel 声明：纯静态页，无 VM 依赖（正确语义，非保守兜底）。
		return nil, nil
	}
	out := make([]cachePackageSource, 0, 8)
	if modulePath == "" {
		r.dependencyUnsafe = true
	}

	dir, ok := typeExprDir(decl)
	if !ok {
		r.dependencyUnsafe = true
		// 保守哨兵：声明形态怪异，任何源文本变化都换键。
		return append(out, cachePackageSource{Rel: "unresolved-vm-decl", Hash: inputHashOf([]byte(decl))}), nil
	}
	depDir := r.sourceDir // 同包：VM 与 .gxml 同包
	if dir != "" {
		depDir = filepath.Join(r.moduleRoot, filepath.FromSlash(dir))
	}
	if _, err := os.Stat(depDir); err != nil {
		r.dependencyUnsafe = true
		// 依赖包目录缺失（被删除/改名）：保守哨兵，让重分析报 ROOT-002。
		return append(out, cachePackageSource{Rel: "missing-vm-dir", Hash: inputHashOf([]byte(decl + "\x00" + depDir))}), nil
	}

	// 模块身份：go.mod/go.sum 内容哈希（B2）。
	for _, name := range []string{"go.mod", "go.sum"} {
		full := filepath.Join(r.moduleRoot, name)
		data, err := os.ReadFile(full)
		if err != nil {
			if os.IsNotExist(err) && name == "go.sum" {
				out = append(out, cachePackageSource{Rel: name, Hash: inputHashOf([]byte("absent"))})
				continue
			}
			return nil, fmt.Errorf("读取模块身份 %s: %w", full, err)
		}
		out = append(out, cachePackageSource{Rel: name, Hash: inputHashOf(data)})
	}

	// 有界闭包：从 VM 包目录沿本模块 import 收集。
	visited := map[string]bool{}
	queue := []string{depDir}
	files := 0
	for len(queue) > 0 {
		if len(visited) >= maxDependencyDirs {
			r.dependencyUnsafe = true
			// 超限：保守哨兵（键变化 → 重分析），不静默截断。
			out = append(out, cachePackageSource{Rel: "dependency-closure-truncated", Hash: inputHashOf([]byte(decl))})
			break
		}
		dir := queue[0]
		queue = queue[1:]
		if visited[dir] {
			continue
		}
		visited[dir] = true
		if !dependencyDirectorySafe(r.moduleRoot, dir) {
			r.dependencyUnsafe = true
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				r.dependencyUnsafe = true
				out = append(out, cachePackageSource{Rel: "missing-vm-dir", Hash: inputHashOf([]byte(dir))})
				continue
			}
			return nil, fmt.Errorf("读取依赖包目录 %s: %w", dir, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if exclude[filepath.Clean(filepath.Join(dir, name))] {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			full := filepath.Join(dir, name)
			files++
			if files > maxDependencyFiles {
				r.dependencyUnsafe = true
				out = append(out, cachePackageSource{Rel: "dependency-closure-truncated", Hash: inputHashOf([]byte(decl))})
				return sortDependencySources(out), nil
			}
			info, err := os.Lstat(full)
			if err != nil || !info.Mode().IsRegular() {
				r.dependencyUnsafe = true
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				return nil, fmt.Errorf("读取依赖源码 %s: %w", full, err)
			}
			out = append(out, cachePackageSource{Rel: relWithin(r.moduleRoot, full), Hash: inputHashOf(data)})
			// 本模块 import → 传递依赖包目录（B2）。ImportsOnly 轻量解析，
			// 不是完整前端；第三方 import 不匹配 modulePath 前缀，自然排除。
			fset := token.NewFileSet()
			if pf, perr := parser.ParseFile(fset, full, data, parser.ImportsOnly); perr == nil {
				for _, imp := range pf.Imports {
					path := unquoteImportPath(imp.Path.Value)
					if modulePath != "" && (path == modulePath || strings.HasPrefix(path, modulePath+"/")) {
						sub := r.moduleRoot
						if path != modulePath {
							sub = filepath.Join(r.moduleRoot, filepath.FromSlash(strings.TrimPrefix(path, modulePath+"/")))
						}
						if !visited[sub] {
							queue = append(queue, sub)
						}
					} else {
						// External modules (including local replace) are outside this
						// snapshot. Only installed standard-library source is stable.
						standard, e := os.Stat(filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(path)))
						if e != nil || !standard.IsDir() {
							r.dependencyUnsafe = true
						}
					}
				}
			} else {
				r.dependencyUnsafe = true
			}
		}
	}
	return sortDependencySources(out), nil
}

func sortDependencySources(in []cachePackageSource) []cachePackageSource {
	sort.Slice(in, func(i, j int) bool { return in[i].Rel < in[j].Rel })
	return in
}

func relWithin(moduleRoot, full string) string {
	rel, err := filepath.Rel(moduleRoot, full)
	if err != nil {
		return full
	}
	return filepath.ToSlash(rel)
}

func unquoteImportPath(s string) string {
	return strings.Trim(s, `"`)
}
