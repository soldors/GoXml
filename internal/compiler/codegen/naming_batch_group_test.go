package codegen

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// ============================================================================
// 批内冲突的分组判据（卡 §8.1 的 A3；C4 接线时暴露的误报）
//
// 背景：C4 把预检接到 CLI 之后，第一批红灯就是
// 「alpha/row.gxml 与 beta/row.gxml 冲突」——两个**不同目录**（不同 Go 包）
// 的同名源，本来就该各自拥有 GoxmlRowView（A3），却被判成批内冲突。
// 根因：detectNamingConflicts 的第 1 段用**全局** firstOwner，没有按
// 目标包目录分组。C1/C2 阶段预检无人调用，缺陷不显形。
//
// 变红方式（已实测，见 dev-log `2026-10-22.md` §5.2）：
//   - 把第 1 段的分组去掉、退回全局 firstOwner → 前两个用例红。
// ============================================================================

// twoPackagesSameBase 构造两个**不同目录**下的同名源（A3 的形态）。
//
// OutputPath 故意留空，SourceAbs 也不落盘：本组用例只验判定，不读磁盘。
func twoPackagesSameBase(root string) []NamingTarget {
	return []NamingTarget{
		{
			SourceAbs:  filepath.Join(root, "alpha", "row.gxml"),
			SourcePath: "row.gxml",
			TargetDir:  filepath.Join(root, "alpha"),
			OutputPath: filepath.Join(root, "alpha", "row_gen.go"),
		},
		{
			SourceAbs:  filepath.Join(root, "beta", "row.gxml"),
			SourcePath: "row.gxml",
			TargetDir:  filepath.Join(root, "beta"),
			OutputPath: filepath.Join(root, "beta", "row_gen.go"),
		},
	}
}

// TestNamingSameBaseDifferentDirsNoConflict：不同目录的同名源各自成包，
// 派生出的符号名相同也**不算**冲突（A3）。
func TestNamingSameBaseDifferentDirsNoConflict(t *testing.T) {
	root := t.TempDir()
	// 两个目录都建出来：预检会去扫包内既有 .go，目录不存在虽不算错误，
	// 但建出来更接近真实（目录里可以有各自的用户源码）。
	for _, sub := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	conflicts, err := detectNamingConflicts(twoPackagesSameBase(root))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("不同目录的同名源不应互相冲突；实际=%v", symbolsOf(conflicts))
	}
}

// TestNamingSameBaseDifferentDirsNoDiagnostics：同上，走 C4 的导出入口
// 也不得产出诊断（CLI 判的就是这条）。
func TestNamingSameBaseDifferentDirsNoDiagnostics(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"alpha", "beta"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	diags, err := CheckNamingConflicts(twoPackagesSameBase(root))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("不同目录的同名源不应产出诊断；实际 %d 条", len(diags))
	}
}

// TestNamingSameDirCollapseStillConflicts：反向对照——**同一目录**里
// 派生同符号的两个源（pascal 塌缩）必须照报，分组不得把真冲突也放过去。
//
// 这里**不放 Skip**（复审 R3）：D-4 裁决要求"塌缩必须检测并报错"，
// 这条能力一旦丢失就应当**失败**。将来若真要改塌缩规则，应先改契约与卡，
// 再有意识地改本用例——不能由测试自行跳过。
func TestNamingSameDirCollapseStillConflicts(t *testing.T) {
	dir := t.TempDir()
	targets := []NamingTarget{
		{SourceAbs: filepath.Join(dir, "a-b.gxml"), SourcePath: "a-b.gxml", TargetDir: dir},
		{SourceAbs: filepath.Join(dir, "a_b.gxml"), SourcePath: "a_b.gxml", TargetDir: dir},
	}
	conflicts, err := detectNamingConflicts(targets)
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	sawBatch := false
	for _, c := range conflicts {
		if c.Kind == namingConflictBatch {
			sawBatch = true
		}
	}
	if !sawBatch {
		t.Fatalf("同目录塌缩必须报 same-batch 冲突（D-4 裁决）；实际=%v", symbolsOf(conflicts))
	}
}

// TestNamingConflictOrderIsFullyDetermined：三源塌缩时排序键必须**完备**（复审 R4）。
//
// 背景：`detectNamingConflicts` 原先按 Symbol/Kind/Owner 三级排序，漏了 Source。
// 三个以上输入撞同一符号时会产生「三级键全同、只有 Source 不同」的元素，
// 而 `sort.Slice` **不稳定** —— 这些等价元素的相对顺序无从规定，诊断序列
// 就不再是"由输入决定"的确定结果。
//
// 构造：`-` / `_` / `+` 都被 pascal 归一成 `_`，因此 a-b / a_b / a+b 派生同一后缀。
// 声明顺序**刻意与 Source 字典序相反**（`a+b` < `a-b` < `a_b`），
// 这样"没按 Source 排"会直接暴露成逆序，而不是靠运气。
func TestNamingConflictOrderIsFullyDetermined(t *testing.T) {
	dir := t.TempDir()
	base := viewMust(t, "a-b.gxml")
	for _, name := range []string{"a_b.gxml", "a+b.gxml"} {
		if other := viewMust(t, name); other != base {
			t.Fatalf("前提不成立：%q 派生 %q，与 %q 不同（本用例依赖三者塌缩）", name, other, base)
		}
	}

	targets := []NamingTarget{
		{SourceAbs: filepath.Join(dir, "a_b.gxml"), SourcePath: "a_b.gxml", TargetDir: dir},
		{SourceAbs: filepath.Join(dir, "a-b.gxml"), SourcePath: "a-b.gxml", TargetDir: dir},
		{SourceAbs: filepath.Join(dir, "a+b.gxml"), SourcePath: "a+b.gxml", TargetDir: dir},
	}

	first, err := detectNamingConflicts(targets)
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	// 3 个源、4 个符号（View / New+View / renderKey / textCache）：后到两个源各报一条。
	if want := 8; len(first) != want {
		t.Fatalf("冲突条数 = %d，期望 %d；实际=%v", len(first), want, symbolsOf(first))
	}

	// 判据 1：整体按 Symbol 升序。
	for i := 1; i < len(first); i++ {
		if first[i].Symbol < first[i-1].Symbol {
			t.Errorf("未按 Symbol 升序: %q 排在 %q 之后", first[i].Symbol, first[i-1].Symbol)
		}
	}
	// 判据 2（R4 核心）：同一 (Symbol, Kind, Owner) 组内，Source 必须升序。
	// 只按前三级排序时，该组会保留追加顺序（= Source 降序），此处必红。
	for i := 1; i < len(first); i++ {
		prev, cur := first[i-1], first[i]
		if prev.Symbol != cur.Symbol || prev.Kind != cur.Kind || prev.Owner != cur.Owner {
			continue
		}
		if cur.Source < prev.Source {
			t.Errorf("同组内未按 Source 升序: %q 排在 %q 之后（Symbol=%q, Owner=%q）",
				cur.Source, prev.Source, cur.Symbol, prev.Owner)
		}
	}

	// 判据 3：重复运行结果完全一致（确定性回归）。
	second, err := detectNamingConflicts(targets)
	if err != nil {
		t.Fatalf("第二次预检报错: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("两次预检结果不一致：\n第一次=%v\n第二次=%v", first, second)
	}
}

// viewMust 派生 view 符号，失败即终止（测试辅助）。
func viewMust(t *testing.T, sourcePath string) string {
	t.Helper()
	v, err := viewSymbol(sourcePath)
	if err != nil {
		t.Fatalf("viewSymbol(%q): %v", sourcePath, err)
	}
	return v
}
