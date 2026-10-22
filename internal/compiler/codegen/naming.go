package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ============================================================================
// TASK-CODEGEN-NAMING-001 切片 C1：包级符号预检（纯读，不写任何文件）
//
// 职责边界（卡 §6）：
//   - 只回答「这批输入生成后，会不会和包内既有声明/彼此撞名」；
//   - **不改命名**（唯一化是 C2），**不写盘**（接线与零副作用是 C4）；
//   - 纯函数：给定 targets 与磁盘现状，返回冲突列表。
//
// 为什么必须有这一步：当前两个 .gxml 生成到同一个包会各自声明
// renderKey / textCache，CLI 却报「生成成功」，直到 go build 才炸。
// 预检把这类问题前移到写入之前。
// ============================================================================

// namingTarget 描述一个待生成的输入（C1 只读，不触碰磁盘）。
type namingTarget struct {
	// SourceAbs 是 .gxml 的绝对路径，仅用于诊断。
	SourceAbs string
	// SourcePath 是相对**目标包目录**的源文件名，等于产物保护头里的 `// Source:` 值。
	SourcePath string
	// TargetDir 是目标包目录的绝对路径。
	TargetDir string
	// OutputPath 是产物绝对路径，用于排除「自己上一版产物」。
	OutputPath string
}

// namingConflictKind 是冲突来源分类（诊断文案与后续诊断码要按它区分）。
type namingConflictKind string

const (
	// namingConflictBatch：本批另一个输入也要声明同一符号（含 pascal 塌缩）。
	namingConflictBatch namingConflictKind = "same-batch"
	// namingConflictExisting：目标包内既有声明占用该符号（用户代码或别的生成物）。
	namingConflictExisting namingConflictKind = "existing"
)

// namingConflict 是一条包级符号冲突。
type namingConflict struct {
	// Symbol 是被重复声明的包级标识符。
	Symbol string
	// Kind 是冲突来源分类。
	Kind namingConflictKind
	// Owner 是占用该符号的文件绝对路径（同类冲突时可能是本批另一个源）。
	Owner string
	// Source 是冲突方对应的 .gxml 绝对路径；用户代码冲突时为空。
	Source string
}

// generatedPackageSymbols 返回一个 .gxml 生成文件将声明的**包级**标识符。
//
// C2 之后类型名按源文件唯一，因此「两个不同源同包」不再撞名；
// 这里保留保守上界（缓存类型即使没有绑定槽位也计入），
// 影响仅限于：若同包两个源派生出**同一后缀**（如 a-b.gxml 与 a_b.gxml 的 pascal 塌缩），
// 会连同缓存类型一起被判冲突——这正是期望行为，不会误伤不同后缀的源。
//
// 保守上界的代价不止塌缩一类：包内既有同名 `goxmlTextCache<Source>` 用户符号时，
// 静态页/纯事件页（槽位为 0，产物不声明该类型）也会被报冲突，即**误报**。
// 该误报已被 `naming_textcache_boundary_test.go` 钉为可复现事实；
// 收窄判据是 `bindSlots > 0`（不是 `usesVM`），归 C4/C5，不在 C2 处理。
func generatedPackageSymbols(sourcePath string) ([]string, error) {
	view, err := viewSymbol(sourcePath)
	if err != nil {
		return nil, err
	}
	// 类型名必须与生成器共用同一派生（generatedTypeNames），
	// 否则预检报的符号与产物里的符号会对不上（C2 之后的专有风险）。
	keyType, cacheType, err := generatedTypeNames(sourcePath)
	if err != nil {
		return nil, err
	}
	return []string{view, "New" + view, keyType, cacheType}, nil
}

// detectNamingConflicts 执行预检：只读磁盘，不写任何文件。
//
// 冲突判定（卡 §5.1 第 2/3 条）：
//  1. 本批内部：两个输入声明同一符号；
//  2. 与包内既有声明：目标包里已有同名包级符号。
//
// 排除项：
//   - 自己的上一版产物（OutputPath 指向的文件，或保护头 `// Source:` 等于本次 SourcePath 的生成物）；
//   - `_test.go`（不参与构建）。
func detectNamingConflicts(targets []namingTarget) ([]namingConflict, error) {
	if len(targets) == 0 {
		return nil, nil
	}

	symbolsByTarget := make([][]string, len(targets))
	for i, t := range targets {
		syms, err := generatedPackageSymbols(t.SourcePath)
		if err != nil {
			return nil, fmt.Errorf("预检 %s: %w", t.SourcePath, err)
		}
		symbolsByTarget[i] = syms
	}

	var conflicts []namingConflict

	// ---- 1. 本批内部冲突 ----
	// 同一符号若被两个不同输入声明 → 冲突。pascal 塌缩（a-b / a_b）也走这条，
	// 因为塌缩后的 view 名相同，符号集合完全相同。
	firstOwner := make(map[string]int, len(targets))
	for i, t := range targets {
		for _, sym := range symbolsByTarget[i] {
			if j, seen := firstOwner[sym]; seen && j != i {
				conflicts = append(conflicts, namingConflict{
					Symbol: sym,
					Kind:   namingConflictBatch,
					Owner:  targets[j].SourceAbs,
					Source: t.SourceAbs,
				})
				continue
			}
			firstOwner[sym] = i
		}
	}

	// ---- 2. 与包内既有声明冲突 ----
	// 按目标包目录分组，避免同一目录被重复扫描。
	byDir := make(map[string][]namingTarget)
	for _, t := range targets {
		byDir[t.TargetDir] = append(byDir[t.TargetDir], t)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		existing, err := existingPackageSymbols(dir, byDir[dir])
		if err != nil {
			return nil, err
		}
		for _, t := range byDir[dir] {
			for _, sym := range symbolsByTarget[indexOfTarget(targets, t)] {
				owner, taken := existing[sym]
				if !taken {
					continue
				}
				conflicts = append(conflicts, namingConflict{
					Symbol: sym,
					Kind:   namingConflictExisting,
					Owner:  owner,
				})
			}
		}
	}

	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Symbol != conflicts[j].Symbol {
			return conflicts[i].Symbol < conflicts[j].Symbol
		}
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		return conflicts[i].Owner < conflicts[j].Owner
	})
	return conflicts, nil
}

// indexOfTarget 返回 target 在切片中的下标（值比较，targets 数量很小）。
func indexOfTarget(targets []namingTarget, t namingTarget) int {
	for i := range targets {
		if targets[i] == t {
			return i
		}
	}
	return -1
}

// existingPackageSymbols 收集目标包内已有的包级符号（符号 → 文件绝对路径）。
//
// 跳过：`_test.go`、本批各输入的上一版产物（含改名过的产物）。
func existingPackageSymbols(dir string, targets []namingTarget) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// --out-dir 指向的新目录还没有源码：空集合，不算错误（与 K3 指纹一致）。
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("预检读取包目录 %s: %w", dir, err)
	}

	out := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if isOwnArtifact(full, targets) {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("预检读取 %s: %w", full, err)
		}
		names, err := packageLevelNames(data)
		if err != nil {
			return nil, fmt.Errorf("预检解析 %s: %w", full, err)
		}
		for n := range names {
			if _, ok := out[n]; !ok {
				out[n] = full
			}
		}
	}
	return out, nil
}

// isOwnArtifact 判断某文件是否为本批某输入的上一版产物。
//
// 两条判据任一成立即算自己的：
//   - 路径等于该输入的 OutputPath；
//   - 带保护头，且第二行 `// Source:` 等于该输入的 SourcePath。
func isOwnArtifact(full string, targets []namingTarget) bool {
	for _, t := range targets {
		if t.OutputPath != "" && filepath.Clean(full) == filepath.Clean(t.OutputPath) {
			return true
		}
	}
	data, err := os.ReadFile(full)
	if err != nil || !HasProtectedHeader(data) {
		return false
	}
	src := generatedSourceOf(data)
	for _, t := range targets {
		if src != "" && src == t.SourcePath {
			return true
		}
	}
	return false
}

// generatedSourceOf 取保护头第二行的 `// Source:` 值（没有则返回空串）。
func generatedSourceOf(data []byte) string {
	lines := strings.SplitN(string(data), "\n", 3)
	if len(lines) < 2 {
		return ""
	}
	line := strings.TrimRight(lines[1], "\r")
	const prefix = "// Source: "
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix))
}

// packageLevelNames 解析 Go 源码，返回包级标识符集合（方法不计入）。
func packageLevelNames(src []byte) (map[string]bool, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, d := range f.Decls {
		switch dd := d.(type) {
		case *ast.FuncDecl:
			if dd.Recv == nil {
				names[dd.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range dd.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					names[sp.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						names[n.Name] = true
					}
				}
			}
		}
	}
	return names, nil
}
