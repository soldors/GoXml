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
