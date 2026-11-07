package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goxml/goxml/internal/compiler/codegen"
)

// on-disk Manifest 与产物保护头的**文档化格式**（复用 internal/cli 口径，不重造身份）。
const (
	manifestDirName  = ".goxml"
	manifestFileName = "generate-manifest.json"
)

// manifestDoc 是 Manifest JSON 的最小读取结构（只取本包需要的字段）。
type manifestDoc struct {
	Version int `json:"version"`
	Entries []struct {
		Source string `json:"source"`
		Output string `json:"output"`
	} `json:"entries"`
}

// ownedIndex 是从 Manifest 得到的 owned 产物集合（项目相对路径，/ 分隔）。
type ownedIndex struct {
	// byOutput: 产物相对路径 -> 期望的保护头 Source 基名。
	byOutput map[string]string
}

// readOwnedIndex 读取 ProjectDir/.goxml/generate-manifest.json，构建 owned 产物索引。
// 文件不存在 => 空索引（首次生成合法状态）。损坏 => 明确错误（不静默当作空：损坏即自锁风险）。
func readOwnedIndex(projectDir string) (*ownedIndex, error) {
	idx := &ownedIndex{byOutput: map[string]string{}}
	p := filepath.Join(projectDir, manifestDirName, manifestFileName)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return idx, nil
		}
		return nil, fmt.Errorf("读取 Manifest 失败: %w", err)
	}
	var doc manifestDoc
	if jerr := json.Unmarshal(data, &doc); jerr != nil {
		return nil, fmt.Errorf("Manifest 损坏（禁止当作空索引）: %w", jerr)
	}
	for _, e := range doc.Entries {
		out := toSlash(e.Output)
		if out == "" {
			continue
		}
		idx.byOutput[out] = toSlash(e.Source)
	}
	return idx, nil
}

func toSlash(p string) string { return strings.ReplaceAll(p, string(filepath.Separator), "/") }

// classify 依据“Manifest 登记 ∧ 产物保护头 Source 与登记一致”判定 owned；否则用户文件。
// 三条同时成立才 owned：Manifest 登记 ∧ 真实保护头 ∧ 第2行 Source 与登记源精确一致（与 cli 口径一致）。
func (idx *ownedIndex) classify(projectDir, rel string, content []byte) FileClass {
	wantBase, registered := idx.byOutput[rel]
	if !registered {
		return ClassUser
	}
	if guardHeaderOwned(content, wantBase) {
		return ClassOwned
	}
	// 保护头被改/不符 => 用户接管所有权，视为用户文件（其变化应触发重建）。
	return ClassUser
}

// guardHeaderOwned 检查产物内容是否带**本工具真实保护头**（复用 codegen.HasProtectedHeader 三行规范），
// 且第 2 行 `// Source: <X>` 与 Manifest 登记的源相对路径**精确一致**（与 cli 的 headerSourceMatches
// 及 --clean/target/cache 所有权口径相同：登记 Source 是项目相对路径，逐字比对，不比对 basename 的宽松前缀）。
// 这样「另一页产物（Source 不符）」不会被误当本页 owned，用户对其改动能被监听。
func guardHeaderOwned(content []byte, wantSourceRel string) bool {
	if !codegen.HasProtectedHeader(content) {
		return false
	}
	lines := strings.SplitN(string(content), "\n", 3)
	if len(lines) < 2 {
		return false
	}
	// 保护头 `// Source:` 是**相对目标包目录**的路径（= 源文件 basename）；与 Manifest 登记的
	// 源相对路径的 basename 精确比对（coordinator：basename 精确一致）。
	wantBase := wantSourceRel
	if i := strings.LastIndex(wantSourceRel, "/"); i >= 0 {
		wantBase = wantSourceRel[i+1:]
	}
	return strings.TrimRight(lines[1], "\r") == "// Source: "+wantBase
}

// Scan 对 ProjectDir 做一次确定性快照，返回 Snapshot 与 owned 索引。
//
// 覆盖：递归遍历 ProjectDir 下普通文件（.gxml、.go、go.mod/go.sum、资产、其它构建输入）。
// 排除：.git、Manifest 目录 .goxml、以及 Config.ExcludeDirs 命中的目录（自有构建输出/缓存）。
// 可移植性：结果按相对路径排序，SHA-256 内容哈希；无时间/顺序依赖，便于确定性断言。
func Scan(cfg Config) (Snapshot, error) {
	if cfg.ProjectDir == "" {
		return nil, fmt.Errorf("ProjectDir 必填")
	}
	idx, err := readOwnedIndex(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{".git": true, manifestDirName: true}
	for _, d := range cfg.ExcludeDirs {
		excluded[toSlash(filepath.Clean(d))] = true
	}
	snap := Snapshot{}
	root := cfg.ProjectDir
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		relSlash := toSlash(rel)
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == manifestDirName {
				return filepath.SkipDir
			}
			if excluded[relSlash] || excluded[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // 跳过 symlink/设备/目录入口，只快照普通文件
		}
		data, ferr := os.ReadFile(path)
		if ferr != nil {
			return ferr
		}
		sum := sha256.Sum256(data)
		snap[relSlash] = hex.EncodeToString(sum[:])
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	_ = idx // owned 分类在 Diff 时按需进行（需要内容判定保护头）
	return snap, nil
}

// Diff 计算两次快照之间变化的项目相对路径（稳定排序）。
// added/modified/deleted 均计入。返回集合可确定性断言。
func Diff(prev, next Snapshot) []string {
	var changed []string
	for k, v := range next {
		if pv, ok := prev[k]; !ok || pv != v {
			changed = append(changed, k)
		}
	}
	for k := range prev {
		if _, ok := next[k]; !ok {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

// ChangeSet 是 Diff 后按 owned/用户分类的变更，用于决定是否触发重建。
type ChangeSet struct {
	// Trigger 是需要触发重建的用户变更（稳定排序）。
	Trigger []string
	// Owned 是被判为工具 owned 的变更（不触发，仅登记；用于防自激循环断言）。
	Owned []string
	// Ignored 是被排除的路径（当前 Scan 已排除，此处保留用于诊断）。
	Ignored []string
}

// ClassifyChanges 在该快照的 next 内容下对 changed 路径分类（owned vs 用户）。
// 需要读取 next 内容判定保护头（仅对 Manifest 登记过的输出做内容检查）。
func ClassifyChanges(projectDir string, next Snapshot, changed []string) (ChangeSet, error) {
	idx, err := readOwnedIndex(projectDir)
	if err != nil {
		return ChangeSet{}, err
	}
	var cs ChangeSet
	for _, rel := range changed {
		// 只对 Manifest 登记过的输出才需要读盘判保护头；其余一律用户文件。
		if _, registered := idx.byOutput[rel]; !registered {
			cs.Trigger = append(cs.Trigger, rel)
			continue
		}
		abs := filepath.Join(projectDir, filepath.FromSlash(rel))
		content, rerr := os.ReadFile(abs)
		if rerr != nil {
			// 删除的 owned 输出：不再是 owned 产物；视为用户侧触发（保守：重建一次以便清理/反映）。
			cs.Trigger = append(cs.Trigger, rel)
			continue
		}
		if idx.classify(projectDir, rel, content) == ClassOwned {
			cs.Owned = append(cs.Owned, rel)
		} else {
			cs.Trigger = append(cs.Trigger, rel)
		}
	}
	sort.Strings(cs.Trigger)
	sort.Strings(cs.Owned)
	return cs, nil
}
