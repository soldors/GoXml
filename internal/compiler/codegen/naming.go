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

	"github.com/goxml/goxml/contracts"
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

// NamingTarget 描述一个待生成的输入（预检只读，不触碰磁盘）。
//
// C4 起导出：CLI 在**写盘之前**用它构造预检批次（卡 §6.1 两阶段流程）。
// 字段语义与产物保护头一一对应，改动会影响 F-06 那类比对（见 isOwnArtifact）。
type NamingTarget struct {
	// SourceAbs 是 .gxml 的绝对路径，仅用于诊断。
	SourceAbs string
	// SourcePath 是相对**目标包目录**的源文件名，等于产物保护头里的 `// Source:` 值。
	SourcePath string
	// TargetDir 是目标包目录的绝对路径。
	TargetDir string
	// OutputPath 是产物绝对路径，用于排除「自己上一版产物」。
	OutputPath string
	// BindSlots 是该源页面会分配的显示缓存槽位数（C5 收窄判据）。
	//
	// 只有 BindSlotsKnown 为真时有意义。为 0 表示产物**不会声明**
	// `goxmlTextCache<Source>`（静态页 / 纯事件页 / 绑定全在 <For> 内），
	// 此时把该类型计入符号集合就是误报。
	BindSlots int
	// BindSlotsKnown 表示 BindSlots 已由编译前端确定。
	//
	// 为假时必须回退为**保守上界**（把缓存类型计入）：前端未通过
	// （语法/绑定错误）的源，我们无从知道产物会不会声明它，而那种情况的
	// 正确报错点是阶段一的编译诊断，不是这里的命名冲突。
	//
	// 这个字段存在的原因：预检发生在任何解析之前（阶段零 C），
	// 它自己拿不到这个事实；而"保守"在接线之后等于"合法工程生成不了"。
	BindSlotsKnown bool
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
	// Source 是触发方对应的 .gxml 绝对路径。
	//
	// C3 之前只有批内冲突填它，与包内既有声明的冲突（用户代码）为空——
	// 这使得 CODEGEN-NAME-001 的消息缺首参。自 C3 起两类冲突都填。
	Source string
}

// generatedPackageSymbols 返回一个 .gxml 生成文件将声明的**包级**标识符。
//
// C2 之后类型名按源文件唯一，因此「两个不同源同包」不再撞名。
//
// 缓存类型是否计入由调用方给出的 bindSlots 决定（C5 起收窄）：
//   - known 且 bindSlots == 0 → **不计入**：产物根本不声明它
//     （静态页 / 纯事件页 / 绑定全在 <For> 内，见卡 §10.7.4 三类页面表）；
//   - known 且 bindSlots > 0 → 计入（真声明，漏了就是漏报）；
//   - 未知 → 计入（保守上界，见 NamingTarget.BindSlotsKnown 的说明）。
//
// 塌缩仍然照旧被拦：`a-b.gxml` 与 `a_b.gxml` 的 view / key 类型同名，
// 即使两者的缓存类型都不计入，冲突依旧成立（卡 §8.1 A4）。
func generatedPackageSymbols(sourcePath string, bindSlots int, slotsKnown bool) ([]string, error) {
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
	syms := []string{view, "New" + view, keyType}
	// 未知槽位数时保守纳入；已知为 0 时排除。
	if !slotsKnown || bindSlots > 0 {
		syms = append(syms, cacheType)
	}
	return syms, nil
}

// detectNamingConflicts 执行预检：只读磁盘，不写任何文件。
//
// 冲突判定（卡 §5.1 第 2/3 条）：
//  1. 本批内部：两个输入声明同一符号；
//  2. 与包内既有声明：目标包里已有同名包级符号。
//
// 排除项：
//   - 自己的上一版产物（OutputPath 指向的文件，或保护头 `// Source:` 归一化后等于本次 SourcePath 的生成物）；
//   - `_test.go`（不参与构建）。
func detectNamingConflicts(targets []NamingTarget) ([]namingConflict, error) {
	if len(targets) == 0 {
		return nil, nil
	}

	symbolsByTarget := make([][]string, len(targets))
	for i, t := range targets {
		syms, err := generatedPackageSymbols(t.SourcePath, t.BindSlots, t.BindSlotsKnown)
		if err != nil {
			return nil, fmt.Errorf("预检 %s: %w", t.SourcePath, err)
		}
		symbolsByTarget[i] = syms
	}

	var conflicts []namingConflict

	// ---- 1. 本批内部冲突 ----
	// 只在**同一目标包目录**内比较：不同目录是不同的 Go 包，各自可以有
	// 自己的 GoxmlRowView（卡 §8.1 的 A3）。同一符号若被同包两个输入
	// 声明 → 冲突；pascal 塌缩（a-b / a_b）也走这条，因为塌缩后的
	// view 名相同，符号集合完全相同。
	//
	// 这里**必须**分组：C4 接线前这段是全局比较，而预检无人调用，缺陷
	// 不显形；接线后第一次跑 CLI 测试就把「alpha/row.gxml 与 beta/row.gxml」
	// 判成了冲突（批次全部放弃）——正是 A3 要防的误报。
	// 目录用 Clean 归一，避免 `dir` 与 `dir/` 被当成两个包而漏报。
	groups := make(map[string][]int, len(targets))
	groupDirs := make([]string, 0, len(targets))
	for i, t := range targets {
		key := filepath.Clean(t.TargetDir)
		if _, ok := groups[key]; !ok {
			groupDirs = append(groupDirs, key)
		}
		groups[key] = append(groups[key], i)
	}
	sort.Strings(groupDirs)

	for _, key := range groupDirs {
		firstOwner := make(map[string]int, len(groups[key]))
		for _, i := range groups[key] {
			for _, sym := range symbolsByTarget[i] {
				if j, seen := firstOwner[sym]; seen && j != i {
					conflicts = append(conflicts, namingConflict{
						Symbol: sym,
						Kind:   namingConflictBatch,
						Owner:  targets[j].SourceAbs,
						Source: targets[i].SourceAbs,
					})
					continue
				}
				firstOwner[sym] = i
			}
		}
	}

	// ---- 2. 与包内既有声明冲突 ----
	// 按目标包目录分组，避免同一目录被重复扫描。
	// key 与批内那段的 Clean 口径保持一致（`dir` 与 `dir/` 指同一个包）。
	byDir := make(map[string][]NamingTarget)
	for _, t := range targets {
		byDir[filepath.Clean(t.TargetDir)] = append(byDir[filepath.Clean(t.TargetDir)], t)
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
					// Source 补上触发方：C3 的诊断消息需要它作为第一个实参。
					// 此前该字段为空（只有批内冲突才填），补信息不改变判定结果。
					Source: t.SourceAbs,
				})
			}
		}
	}

	// 排序键必须**完整**：Symbol / Kind / Owner / Source 四级全比。
	//
	// 漏掉 Source 时，三个以上输入撞同一符号会产生「Symbol+Kind+Owner 全同、
	// 只有 Source 不同」的元素；sort.Slice 是**不稳定**排序，这些等价元素的
	// 相对顺序无从规定，诊断输出就不再是"由输入决定"的确定序列。
	// 与其依赖当前 pdqsort 的实现巧合，不如把键写全（复审 R4）。
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Symbol != conflicts[j].Symbol {
			return conflicts[i].Symbol < conflicts[j].Symbol
		}
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		if conflicts[i].Owner != conflicts[j].Owner {
			return conflicts[i].Owner < conflicts[j].Owner
		}
		return conflicts[i].Source < conflicts[j].Source
	})
	return conflicts, nil
}

// CheckNamingConflicts 跑预检并把冲突转成带诊断码的 CompilerError。
//
// 这是 C4 的**唯一 CLI 入口**：CLI 在写盘之前调用它，有诊断就整批放弃
// （卡 §6.1 阶段 1 → 阶段 2 的门；§6.2 零副作用）。C3 只定义/发射码，
// 不接调用方；C4 完成接线后两码才进入生产链路可达状态。
//
// 调用方的两条责任（本函数不代劳）：
//   - 传入的一批 targets 必须是**本次将要写入**的输入——把缓存命中的
//     输入也算进来会让「全部命中」的批次因磁盘既有状态而失败；
//   - 返回诊断后必须**不写任何文件**，否则零副作用约束失效。
//
// 位置信息为零值：命名冲突是**包级**事实，不落在 .gxml 的某个行列，
// `CompilerError.Error()` 会输出 `file:0:0: CODE: message`。
func CheckNamingConflicts(targets []NamingTarget) ([]contracts.CompilerError, error) {
	conflicts, err := detectNamingConflicts(targets)
	if err != nil {
		return nil, err
	}
	out := make([]contracts.CompilerError, 0, len(conflicts))
	for _, c := range conflicts {
		out = append(out, conflictDiagnostic(c))
	}
	return out, nil
}

// conflictDiagnostic 把一条冲突转成诊断（语义冻结于卡 §10.5.2）。
//
// 消息文案与实参顺序是**对外承诺**：日志、文档与测试都按它匹配；
// 改文案必须同步改卡 §10.5.2 与目录 §2.8，并更新锁定它的测试。
func conflictDiagnostic(c namingConflict) contracts.CompilerError {
	if c.Kind == namingConflictExisting {
		return contracts.NewError(c.Source, contracts.SourceSpan{}, CodeNamingExisting,
			"generated symbol %q (from %s) is already declared in package: %s",
			c.Symbol, c.Source, c.Owner)
	}
	// 批内冲突：Owner 是先声明该符号的那个源，Source 是后到的源。
	return contracts.NewError(c.Source, contracts.SourceSpan{}, CodeNamingSameBatch,
		"sources %q and %q both generate symbol %q",
		c.Owner, c.Source, c.Symbol)
}

// indexOfTarget 返回 target 在切片中的下标（值比较，targets 数量很小）。
func indexOfTarget(targets []NamingTarget, t NamingTarget) int {
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
func existingPackageSymbols(dir string, targets []NamingTarget) (map[string]string, error) {
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
//
// 第二条的两侧必须**同形态**比对：保护头由 headerFor 写出，走的是
// normalizeSourcePath（统一 `/`），而调用方给的 SourcePath 可能是平台
// 原生分隔符（Windows 的 `\`）。直接比对会让自己的上一版产物被当成
// 他人声明 → 误报冲突（F-06，C4-PRE-1）。
// 实现上**两侧都过** normalizeSourcePath：不假设任何一侧"已经是目标形态"，
// 这样判据不依赖保护头的当前写法（旧产物、手改头、未来换分隔符都不影响）。
func isOwnArtifact(full string, targets []NamingTarget) bool {
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
		// F-06：**两侧都归一化**后再比。
		//
		// 只说"保护头是归一化形态"是不够的：那是一个未在比较处表达的隐含前提
		// （保护头也可能来自旧版本产物或被手改）。把两侧都过一遍
		// normalizeSourcePath，判据就只依赖"要不要在同一形态下比较"这一条，
		// 不依赖任何一侧的当前实现细节。
		if src != "" && normalizeSourcePath(src) == normalizeSourcePath(t.SourcePath) {
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
