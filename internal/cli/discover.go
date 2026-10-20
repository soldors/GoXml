package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ============================================================================
// 1-3A：输入发现（TASK-GENERATE-CLI-001-3）
//
// 冻结规则（评审确认）：
//   - 输入形式：单个 .gxml 文件或目录；
//   - 目录递归发现 .gxml；
//   - **不跟随目录符号链接**（默认约定，避免环与越界）；
//   - 发现后按**规范化的相对路径**排序——顺序由输入本身决定，
//     不依赖文件系统返回顺序（契约 §6 确定性）；
//   - 每个 .gxml 的 SourceDir = 其实际所在目录（G2 已冻结）；
//   - 输入路径与输出路径分开计算（G2 修订：两者不互相推导）。
// ============================================================================

// discovered 是一个待生成输入。
type discovered struct {
	// abs 是 .gxml 的绝对路径。
	abs string
	// rel 是相对发现根（CLI 参数）的路径，`/` 分隔，用于排序与输出映射。
	rel string
	// sourceDir 是该文件的解析上下文（= 其所在目录）。
	sourceDir string
}

// discoverInputs 发现待生成的 .gxml 输入。
//
// 输入可以是单个文件或目录。目录递归发现，结果按 rel 排序。
func discoverInputs(root string) ([]discovered, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析输入绝对路径: %w", err)
	}
	fi, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("输入路径不可用: %w", err)
	}

	// 单文件：直接成项。
	if !fi.IsDir() {
		if !isGxmlFile(absRoot) {
			return nil, fmt.Errorf("%q 不是 .gxml 文件", root)
		}
		rel := filepath.Base(absRoot)
		return []discovered{{
			abs:       absRoot,
			rel:       rel,
			sourceDir: filepath.Dir(absRoot),
		}}, nil
	}

	// 目录：递归发现。
	var found []discovered
	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("遍历 %s: %w", path, err)
		}
		// 不跟随目录符号链接。
		if d.IsDir() && d.Type()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if !isGxmlFile(path) {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return fmt.Errorf("计算相对路径: %w", err)
		}
		found = append(found, discovered{
			abs:       path,
			rel:       filepath.ToSlash(rel),
			sourceDir: filepath.Dir(path),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 确定性：按 rel 排序（契约 §6；G5）。
	sort.Slice(found, func(i, j int) bool { return found[i].rel < found[j].rel })

	// 空目录：明确结果，不静默成功。
	if len(found) == 0 {
		return nil, fmt.Errorf("目录 %s 中没有 .gxml 文件", root)
	}
	return found, nil
}

// isGxmlFile 报告路径是否为 .gxml 文件（扩展名小写匹配）。
func isGxmlFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".gxml")
}
