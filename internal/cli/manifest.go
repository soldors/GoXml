package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// ============================================================================
// 1-6：Manifest 与 --clean（TASK-GENERATE-CLI-001-6，契约 §10）
//
// 三把安全锁（评审确认）：
//   1. 双重所有权：--clean 只删「Manifest 登记 ∧ 带保护头」的文件；
//      保护头被改动 = 用户接管所有权，禁止删除；
//   2. Manifest 自身原子更新（临时文件 + os.Rename，防半截 JSON）；
//   3. 损坏即自锁：Manifest 无法反序列化 → 报错终止，禁止任何删除。
// ============================================================================

// manifestVersion 是 Manifest 格式版本（契约 §10 示例）。
const manifestVersion = 1

// manifestName 是 Manifest 文件名（契约 §10：项目根目录维护）。
const manifestName = "generate-manifest.json"

// manifestDir 是 Manifest 所在目录（契约 §10：.goxml/，已在 .gitignore）。
const manifestDir = ".goxml"

// entry 是 Manifest 的一条登记。
type entry struct {
	// Source 是 .gxml 的项目相对路径（/ 分隔）。
	Source string `json:"source"`
	// Output 是生成文件的项目相对路径（/ 分隔）。
	Output string `json:"output"`
	// InputHash 是 .gxml 内容的 SHA-256（hex）。
	InputHash string `json:"inputHash"`
	// CompilerVersion 是语言版本（G3：与保护头同一来源，显式单一）。
	CompilerVersion string `json:"compilerVersion"`
}

// manifest 是 Manifest 文档。
type manifest struct {
	Version int     `json:"version"`
	Entries []entry `json:"entries"`
}

// manifestPathOverride 供测试把 Manifest 重定向到隔离目录。
//
// 生产代码不得设置；测试用它避免多个用例共享同一个真实项目根的
// Manifest 而互相污染（1-6 开发时实测：损坏自锁会让后续用例连环失败）。
var manifestPathOverride string

// manifestPath 返回 Manifest 的绝对路径。
func manifestPath(moduleRoot string) string {
	if manifestPathOverride != "" {
		return manifestPathOverride
	}
	return filepath.Join(moduleRoot, manifestDir, manifestName)
}

// loadManifest 读 Manifest。
//
// 返回 (nil, nil) 表示文件不存在——首次生成是合法状态。
// 文件存在但损坏 → 明确错误（损坏即自锁，禁止任何删除）。
func loadManifest(path string) (*manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &manifest{Version: manifestVersion}, nil
		}
		return nil, fmt.Errorf("读取 Manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("Manifest 损坏（%s）: %w；禁止任何清理操作，请人工修复或删除该文件", path, err)
	}
	if m.Version != manifestVersion {
		return nil, fmt.Errorf("Manifest 版本 %d 不受支持（期望 %d）；禁止任何清理操作", m.Version, manifestVersion)
	}
	// Valid JSON is not sufficient: these paths control fingerprint exclusion
	// and deletion. Validate the entire document before any generation/cleanup.
	for i, e := range m.Entries {
		for _, field := range []struct{ label, value string }{{"source", e.Source}, {"output", e.Output}} {
			if err := validateProjectFilePath(field.label, field.value); err != nil {
				return nil, fmt.Errorf("Manifest 损坏（%s，第 %d 条）: %w；禁止生成或清理，请人工修复 Manifest", path, i+1, err)
			}
		}
	}
	return &m, nil
}

// saveManifest 原子保存：临时文件 + os.Rename（防半截 JSON）。
func saveManifest(path string, m *manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建 Manifest 目录: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 Manifest: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".goxml-manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 Manifest 临时文件: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后该路径已不存在，忽略错误
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写 Manifest 临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭 Manifest 临时文件: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换 Manifest: %w", err)
	}
	return nil
}

// inputHashOf 计算 .gxml 内容哈希（G3：与保护头同一版本语义）。
func inputHashOf(src []byte) string {
	sum := sha256.Sum256(src)
	return hex.EncodeToString(sum[:])
}

// updateManifestAfterGenerate 在生成成功后更新 Manifest 登记并原子保存。
func updateManifestAfterGenerate(moduleRoot string, es []entry) error {
	path := manifestPath(moduleRoot)
	m, err := loadManifest(path)
	if err != nil {
		return err
	}
	// 用本次的 entries 覆盖同 source 的旧登记；其余保留。
	bySrc := make(map[string]entry, len(es))
	for _, e := range es {
		bySrc[e.Source] = e
	}
	kept := m.Entries[:0]
	for _, old := range m.Entries {
		if _, touched := bySrc[old.Source]; !touched {
			kept = append(kept, old)
		}
	}
	kept = append(kept, es...)
	m.Entries = kept
	return saveManifest(path, m)
}

// cleanOrphans 执行 --clean：只删除「Manifest 登记 ∧ 仍带保护头 ∧ 存在」的文件。
//
// 返回 (删除列表, 跳过列表, 错误)。
// 跳过原因：文件不存在 / 保护头不符（用户接管）/ 未登记（不归本工具管）。
func cleanOrphans(moduleRoot string, keepSources map[string]bool) ([]string, []string, error) {
	path := manifestPath(moduleRoot)
	m, err := loadManifest(path)
	if err != nil {
		return nil, nil, err // 损坏即自锁
	}
	if err := validateManifestOutputPaths(moduleRoot, m); err != nil {
		return nil, nil, err
	}

	var deleted, skipped []string
	for _, e := range m.Entries {
		if keepSources[e.Source] {
			continue // 本轮生成中还在用的不删
		}
		// An input omitted by a single-file/subdirectory invocation is not an
		// orphan. Only a source that actually disappeared permits cleanup.
		if _, err := os.Stat(filepath.Join(moduleRoot, filepath.FromSlash(e.Source))); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("检查源文件 %s: %w", e.Source, err)
		}
		target := filepath.Join(moduleRoot, filepath.FromSlash(e.Output))
		data, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				skipped = append(skipped, e.Output+" (不存在)")
				continue
			}
			return nil, nil, fmt.Errorf("读取 %s: %w", target, err)
		}
		if !codegen.HasProtectedHeader(data) {
			// 保护头不符 → 用户接管所有权，绝不删除（安全锁 1）。
			skipped = append(skipped, e.Output+" (保护头不符，视为用户文件)")
			continue
		}
		if err := os.Remove(target); err != nil {
			return nil, nil, fmt.Errorf("删除 %s: %w", target, err)
		}
		deleted = append(deleted, e.Output)
	}

	// 清理后保存 Manifest（删除的条目移除）。
	if len(deleted) > 0 {
		remaining := m.Entries[:0]
		deletedSet := make(map[string]bool, len(deleted))
		for _, d := range deleted {
			deletedSet[d] = true
		}
		for _, e := range m.Entries {
			if !deletedSet[e.Output] {
				remaining = append(remaining, e)
			}
		}
		m.Entries = remaining
		if err := saveManifest(path, m); err != nil {
			return deleted, skipped, fmt.Errorf("更新 Manifest: %w", err)
		}
	}
	return deleted, skipped, nil
}
