package cli

import (
	"fmt"
	"github.com/goxml/goxml/internal/compiler/codegen"
	"os"
	"path/filepath"
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

// scopedUserOverlayForAnalyzer 构造**Analyzer 阶段**的用户 overlay 字节快照（R22-A A2）。
//
// 背景：Analyzer 通过 packages.Load 的 Config.Overlay 只接收 staleGeneratedOverlay
// （工具自有旧产物的 //go:build ignore 占位），**不含用户 GOFLAGS/GOENV 的 -overlay**，
// 于是类型/方法检查按磁盘进行，与 go build/--check/定名口径不一致（s1 假接受、s3/s4 假拒绝）。
//
// 本函数把用户 overlay 中**落在 Analyzer 实际加载的包目录**（vmDirs）内的条目，读成内存快照
// 字节并入 Analyzer 的 overlay；不预读整份映射（只读相关目录的替换源，有界）。
// 复用 userOverlayMap 的路径映射与 fail-closed（空键/归一化重复已在解析期拒绝）。
// 已证工具自有条目优先（由调用方以 owned 覆盖）；此函数只负责用户条目。
//
// 语义边界：`from`（被覆盖的文件）必须在某 vmDir 内才读；删除语义（"" 目标）跳过；
// 选中替换源缺失 → 返回错误（诚实失败，不静默）。跨模块/越界由后续 analyzer.Config.Validate 拦截。
func scopedUserOverlayForAnalyzer(flags, moduleRoot string, vmDirs []string) (map[string][]byte, error) {
	userMap, err := userOverlayMap(flags, moduleRoot)
	if err != nil {
		return nil, err
	}
	if len(userMap) == 0 {
		return nil, nil
	}
	// 归一化 vmDirs 到 Clean 绝对路径集合。
	absDirs := make([]string, 0, len(vmDirs))
	for _, d := range vmDirs {
		if d == "" {
			continue
		}
		a := d
		if !filepath.IsAbs(a) {
			a = filepath.Join(moduleRoot, a)
		}
		absDirs = append(absDirs, filepath.Clean(a))
	}
	inScope := func(from string) bool {
		fd := filepath.Dir(filepath.Clean(from))
		for _, ad := range absDirs {
			if samePathDir(fd, ad) {
				return true
			}
		}
		return false
	}
	out := make(map[string][]byte, len(userMap))
	for from, to := range userMap {
		if !inScope(from) {
			continue // 不在 Analyzer 加载的包目录内：不读、不并入（有界）
		}
		if to == "" {
			continue // 删除语义：Go 侧处理；Analyzer overlay 不提供该文件字节
		}
		data, rerr := os.ReadFile(to)
		if rerr != nil {
			return nil, fmt.Errorf("读取用户 overlay 替换文件 %s: %w", to, rerr)
		}
		out[filepath.Clean(from)] = data
	}
	return out, nil
}
