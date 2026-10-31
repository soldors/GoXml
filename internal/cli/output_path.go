package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateProjectFilePath applies the same module-relative boundary to persisted
// ownership records and new outputs. IsLocal also rejects Windows drive-relative
// paths, device names and alternate data streams on Windows.
func validateProjectFilePath(label, path string) error {
	if err := validateRelativePath(label, path); err != nil {
		return err
	}
	if !filepath.IsLocal(filepath.FromSlash(path)) || filepath.Clean(path) == "." || strings.IndexByte(path, 0) >= 0 {
		return fmt.Errorf("%s 必须是项目内的文件相对路径: %q", label, path)
	}
	return nil
}

// validateOutputBoundary checks existing filesystem ancestors too: a lexical
// module-relative path may still escape through a directory symlink/junction,
// or enter a nested module. Missing output directories are checked through their
// nearest existing ancestor, without creating any directories.
func validateOutputBoundary(moduleRoot, target string) error {
	rel, err := filepath.Rel(moduleRoot, target)
	if err != nil {
		return fmt.Errorf("解析输出路径: %w", err)
	}
	if err := validateProjectFilePath("输出路径", rel); err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(moduleRoot)
	if err != nil {
		return fmt.Errorf("解析项目根目录: %w", err)
	}
	ancestor := target
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			// Go 1.26 reports Windows junctions as ModeIrregular, and
			// EvalSymlinks can return an unresolved junction leaf with no
			// error. Do not treat that lexical path as a resolved boundary.
			if info.Mode()&os.ModeIrregular != 0 {
				return fmt.Errorf("输出路径 %s 包含不支持的文件类型（junction/reparse point），拒绝输出或清理", ancestor)
			}
			// Replacing or cleaning a link would discard a user-created link,
			// rather than update an ordinary generated file.
			if ancestor == target && info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("输出 %s 是符号链接，拒绝覆盖或清理", target)
			}
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("读取输出路径 %s: %w", ancestor, err)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return fmt.Errorf("输出路径 %s 没有可用父目录", target)
		}
		ancestor = parent
	}
	actual, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return fmt.Errorf("解析输出路径 %s: %w", ancestor, err)
	}
	// A supported directory symlink can itself resolve to a junction leaf.
	// Inspect the resolved path's ancestors as well, before trusting it.
	for path := actual; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("检查输出路径 %s: %w", path, err)
		}
		if info.Mode()&os.ModeIrregular != 0 {
			return fmt.Errorf("输出路径 %s 包含不支持的文件类型（junction/reparse point），拒绝输出或清理", path)
		}
		if filepath.Dir(path) == path {
			break
		}
	}
	inside, err := filepath.Rel(root, actual)
	if err != nil || (inside != "." && !filepath.IsLocal(inside)) {
		return fmt.Errorf("输出路径 %s 经符号链接解析后越出当前 Go module", target)
	}
	owner, err := findModuleRoot(actual)
	if err != nil {
		return err
	}
	if owner != root {
		return fmt.Errorf("输出路径 %s 属于其他 Go module（%s）", target, owner)
	}
	return nil
}

func validateManifestOutputPaths(moduleRoot string, m *manifest) error {
	for _, e := range m.Entries {
		if err := validateOutputBoundary(moduleRoot, filepath.Join(moduleRoot, filepath.FromSlash(e.Output))); err != nil {
			return fmt.Errorf("Manifest 输出路径 %q 不安全: %w；禁止生成或清理，请人工修复 Manifest", e.Output, err)
		}
	}
	return nil
}
