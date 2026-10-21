package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ============================================================================
// TASK-CODEGEN-NAMING-001 切片 C1：包级符号预检
//
// 红灯证据（本轮已实测）：改实现之前，两个源文件的生成片段都声明
// renderKey / textCache —— 同包多 .gxml 必然无法编译。该证据被保留在
// TestNamingDetectsSameBatchConflict 的断言里（先证明缺陷存在，再证明预检能报出它）。
//
// 变红方式（已实测）：
//   - detectNamingConflicts 直接返回 nil（不检测）→ 本文件全部冲突用例红；
//   - 去掉 isOwnArtifact 的排除逻辑 → TestNamingIgnoresOwnArtifact 红；
//   - 把 existingPackageSymbols 的 `_test.go` 过滤去掉 → TestNamingIgnoresTestFiles 红。
// ============================================================================

// declaredPackageNames 解析一段生成片段，返回其中的**包级**标识符。
//
// 方法（FuncDecl 带接收者）不算包级符号——它们挂在类型上，只有类型参与包级冲突。
func declaredPackageNames(t *testing.T, snippet string) map[string]bool {
	t.Helper()
	src := "package p\n\n" + snippet
	f, err := parser.ParseFile(token.NewFileSet(), "snippet.go", src, 0)
	if err != nil {
		t.Fatalf("生成片段不是合法 Go：%v\n片段:\n%s", err, snippet)
	}
	return declNamesOf(f)
}

func declNamesOf(f *ast.File) map[string]bool {
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
	return names
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// namingTargets 为一批源文件构造 targets（源文件不必真实存在，C1 不读 .gxml）。
func namingTargets(dir string, sources ...string) []namingTarget {
	out := make([]namingTarget, 0, len(sources))
	for _, s := range sources {
		base := strings.TrimSuffix(filepath.Base(s), ".gxml")
		out = append(out, namingTarget{
			SourceAbs:  filepath.Join(dir, s),
			SourcePath: s,
			TargetDir:  dir,
			OutputPath: filepath.Join(dir, base+"_gen.go"),
		})
	}
	return out
}

// symbolsOf 抽出冲突结果里的符号名（便于断言）。
func symbolsOf(cs []namingConflict) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Symbol)
	}
	sort.Strings(out)
	return out
}

func hasConflict(cs []namingConflict, symbol string, kind namingConflictKind) bool {
	for _, c := range cs {
		if c.Symbol == symbol && c.Kind == kind {
			return true
		}
	}
	return false
}

// TestNamingDetectsSameBatchConflict：本批内部符号撞车必须被报出。
//
// C2 唯一化之后，两个**不同**的源已不再撞名；仍会撞名的是 pascal 塌缩
// （a-b.gxml 与 a_b.gxml 派生同一后缀），这里用它验证同批检测仍然有效。
func TestNamingDetectsSameBatchConflict(t *testing.T) {
	va, err := viewSymbol("a-b.gxml")
	if err != nil {
		t.Fatalf("viewSymbol(a-b.gxml): %v", err)
	}
	vb, err := viewSymbol("a_b.gxml")
	if err != nil {
		t.Fatalf("viewSymbol(a_b.gxml): %v", err)
	}
	if va != vb {
		t.Skipf("当前实现未塌缩（%q vs %q）", va, vb)
	}
	ka, ca, err := generatedTypeNames("a-b.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames(a-b.gxml): %v", err)
	}
	kb, cb, err := generatedTypeNames("a_b.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames(a_b.gxml): %v", err)
	}
	if ka != kb || ca != cb {
		t.Fatalf("测试构造错误：塌缩源未派生出相同类型名（%q/%q vs %q/%q）", ka, ca, kb, cb)
	}

	// 片段层面确实重复（缺陷证据）
	namesA := declaredPackageNames(t, renderKeyDecl(va, ka, ca)+bindCacheDecl(va, 2, ka, ca))
	namesB := declaredPackageNames(t, renderKeyDecl(vb, kb, cb)+bindCacheDecl(vb, 2, kb, cb))
	if !namesA[ka] || !namesB[kb] {
		t.Fatal("测试构造错误：片段里找不到派生类型名")
	}

	dir := t.TempDir()
	conflicts, err := detectNamingConflicts(namingTargets(dir, "a-b.gxml", "a_b.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if !hasConflict(conflicts, va, namingConflictBatch) {
		t.Fatalf("应报 view 冲突 %q；实际=%v", va, symbolsOf(conflicts))
	}
	if !hasConflict(conflicts, ka, namingConflictBatch) {
		t.Fatalf("应报类型冲突 %q；实际=%v", ka, symbolsOf(conflicts))
	}
}

// TestNamingNoConflictBetweenDistinctSources：C2 的直接产物——
// 两个不同源（home / second）声明互不相交的包级符号，预检不得误报。
func TestNamingNoConflictBetweenDistinctSources(t *testing.T) {
	dir := t.TempDir()
	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml", "second.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("不同源不应冲突；实际=%v", symbolsOf(conflicts))
	}
}

func TestNamingDetectsExistingUserSymbol(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.go")
	if err := os.WriteFile(userFile, []byte("package demo\n\ntype goxmlRenderKeyHome struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if !hasConflict(conflicts, "goxmlRenderKeyHome", namingConflictExisting) {
		t.Fatalf("应报 goxmlRenderKeyHome 与用户符号冲突；实际=%v", symbolsOf(conflicts))
	}
	for _, c := range conflicts {
		if c.Symbol == "goxmlRenderKeyHome" && c.Kind == namingConflictExisting && c.Owner != userFile {
			t.Errorf("冲突来源应为 %s，实际 %s", userFile, c.Owner)
		}
	}
}

// TestNamingIgnoresOwnArtifact：自己的上一版产物（保护头 + Source 匹配）不算冲突。
func TestNamingIgnoresOwnArtifact(t *testing.T) {
	dir := t.TempDir()
	prev := filepath.Join(dir, "home_gen.go")
	content := "// Code generated by goxml. DO NOT EDIT.\n// Source: home.gxml\n// Goxml-Version: 1\n\npackage demo\n\ntype renderKey struct{}\n"
	if err := os.WriteFile(prev, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("自己的旧产物不应算冲突；实际=%v", symbolsOf(conflicts))
	}
}

// TestNamingIgnoresOwnRenamedArtifact：产物改过名，但保护头 Source 指向本输入，也不算冲突。
func TestNamingIgnoresOwnRenamedArtifact(t *testing.T) {
	dir := t.TempDir()
	prev := filepath.Join(dir, "old_name_gen.go")
	content := "// Code generated by goxml. DO NOT EDIT.\n// Source: home.gxml\n// Goxml-Version: 1\n\npackage demo\n\ntype renderKey struct{}\n"
	if err := os.WriteFile(prev, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("改名后的自家产物不应算冲突；实际=%v", symbolsOf(conflicts))
	}
}

// TestNamingDetectsPascalCollapse：a-b.gxml 与 a_b.gxml 的 view 名塌缩相同 → 必须报冲突。
func TestNamingDetectsPascalCollapse(t *testing.T) {
	va, err := viewSymbol("a-b.gxml")
	if err != nil {
		t.Fatalf("viewSymbol(a-b.gxml): %v", err)
	}
	vb, err := viewSymbol("a_b.gxml")
	if err != nil {
		t.Fatalf("viewSymbol(a_b.gxml): %v", err)
	}
	if va != vb {
		t.Skipf("当前实现未塌缩（%q vs %q）；本条留待实现变更后复核", va, vb)
	}

	dir := t.TempDir()
	conflicts, err := detectNamingConflicts(namingTargets(dir, "a-b.gxml", "a_b.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if !hasConflict(conflicts, va, namingConflictBatch) {
		t.Fatalf("pascal 塌缩应报 %q 冲突；实际=%v", va, symbolsOf(conflicts))
	}
}

// TestNamingNoConflictSingleFile：单文件空包 → 无冲突。
func TestNamingNoConflictSingleFile(t *testing.T) {
	dir := t.TempDir()
	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("不应有冲突；实际=%v", symbolsOf(conflicts))
	}
}

// TestNamingIgnoresTestFiles：`_test.go` 里的同名声明不算冲突（不参与构建）。
func TestNamingIgnoresTestFiles(t *testing.T) {
	dir := t.TempDir()
	tf := filepath.Join(dir, "user_test.go")
	if err := os.WriteFile(tf, []byte("package demo\n\ntype goxmlRenderKeyHome struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("_test.go 不应触发冲突；实际=%v", symbolsOf(conflicts))
	}
}

// TestNamingMissingTargetDirIsNotError：--out-dir 指向的新目录尚不存在，不算错误。
func TestNamingMissingTargetDirIsNotError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created-yet")
	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("目标目录不存在不应报错: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("不应有冲突；实际=%v", symbolsOf(conflicts))
	}
}

// TestNamingUniqueSymbolsAcrossSources 是 C2 的契约断言：
// 不同源文件的生成片段必须声明**互不相交**的包级符号，且形态符合裁决 D-3
// （小写 goxml 前缀 + 源派生后缀，例如 goxmlRenderKeyHome）。
func TestNamingUniqueSymbolsAcrossSources(t *testing.T) {
	viewA, err := viewSymbol("home.gxml")
	if err != nil {
		t.Fatalf("viewSymbol: %v", err)
	}
	viewB, err := viewSymbol("second.gxml")
	if err != nil {
		t.Fatalf("viewSymbol: %v", err)
	}

	keyA, cacheA, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}
	keyB, cacheB, err := generatedTypeNames("second.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	// 形态：goxml + 类型 + 源后缀
	if want := "goxmlRenderKeyHome"; keyA != want {
		t.Errorf("key 类型名 = %q, 期望 %q", keyA, want)
	}
	if want := "goxmlTextCacheHome"; cacheA != want {
		t.Errorf("cache 类型名 = %q, 期望 %q", cacheA, want)
	}
	if keyA == keyB || cacheA == cacheB {
		t.Fatalf("不同源派生出相同类型名：%q/%q vs %q/%q", keyA, cacheA, keyB, cacheB)
	}

	// 片段层面必须互不相交（C1 红灯证据的正面断言）
	namesA := declaredPackageNames(t, renderKeyDecl(viewA, keyA, cacheA)+bindCacheDecl(viewA, 2, keyA, cacheA))
	namesB := declaredPackageNames(t, renderKeyDecl(viewB, keyB, cacheB)+bindCacheDecl(viewB, 2, keyB, cacheB))
	var dup []string
	for n := range namesA {
		if namesB[n] {
			dup = append(dup, n)
		}
	}
	sort.Strings(dup)
	if len(dup) > 0 {
		t.Fatalf("两个源的片段仍声明相同包级符号 %v", dup)
	}
}

// TestNamingSourceSymbolSuffixRules 直接断言 C2 的派生规则本身（验收第 1、5 项）：
// 去扩展名 → 按 / \ . 切段 → 逐段 PascalCase。
//
// 只测「两个源派生出不同类型名」不足以约束规则：拼错分隔符或漏掉 PascalCase
// 一样能让两个源互不相同。因此这里把规则逐条钉住。
func TestNamingSourceSymbolSuffixRules(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"home.gxml", "Home"},
		{"sub/home.gxml", "SubHome"},
		{`sub\home.gxml`, "SubHome"},
		{"sub.home.gxml", "SubHome"},
		{"./home.gxml", "Home"},
		{"dashboard_v2.gxml", "Dashboard_V2"},
		// 两种写法都会塌缩成 A_B —— 这正是必须由 C1 预检拦下的碰撞（见下一条测试）
		{"a-b.gxml", "A_B"},
		{"a_b.gxml", "A_B"},
	}
	for _, c := range cases {
		got, err := sourceSymbolSuffix(c.in)
		if err != nil {
			t.Fatalf("sourceSymbolSuffix(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("sourceSymbolSuffix(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}

	// 空后缀必须报错，而不是静默生成一个无意义的类型名。
	for _, bad := range []string{"", ".gxml", "/", "...", `\\`} {
		if got, err := sourceSymbolSuffix(bad); err == nil {
			t.Errorf("sourceSymbolSuffix(%q) = %q, 期望报错（空后缀不得静默生成符号）", bad, got)
		}
	}
}

// 注：pascal 塌缩（a-b.gxml 与 a_b.gxml 派生同一后缀 A_B）的冲突由
// TestNamingDetectsPascalCollapse 与 TestNamingDetectsSameBatchConflict 覆盖，
// 不在此重复。

// TestNamingViewAndTypesShareSuffix 覆盖 View / render key / text cache 三种形态
// （验收第 2 项），并确认同一 SourcePath 重复派生逐字符一致（验收第 3 项）。
func TestNamingViewAndTypesShareSuffix(t *testing.T) {
	const src = "dashboard.gxml"
	view, err := viewSymbol(src)
	if err != nil {
		t.Fatalf("viewSymbol: %v", err)
	}
	if want := "GoxmlDashboardView"; view != want {
		t.Errorf("View 名 = %q, 期望 %q", view, want)
	}
	key, cache, err := generatedTypeNames(src)
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}
	if want := "goxmlRenderKeyDashboard"; key != want {
		t.Errorf("key 类型名 = %q, 期望 %q", key, want)
	}
	if want := "goxmlTextCacheDashboard"; cache != want {
		t.Errorf("cache 类型名 = %q, 期望 %q", cache, want)
	}
	if !strings.HasSuffix(view, "DashboardView") ||
		!strings.Contains(key, "Dashboard") || !strings.Contains(cache, "Dashboard") {
		t.Fatalf("三种形态应共享同一后缀：view=%q key=%q cache=%q", view, key, cache)
	}

	// 稳定性：重复派生必须完全一致（禁止随机串/时间戳/递增编号）。
	for i := 0; i < 3; i++ {
		v2, err := viewSymbol(src)
		if err != nil {
			t.Fatalf("viewSymbol(第 %d 次): %v", i+1, err)
		}
		k2, c2, err := generatedTypeNames(src)
		if err != nil {
			t.Fatalf("generatedTypeNames(第 %d 次): %v", i+1, err)
		}
		if v2 != view || k2 != key || c2 != cache {
			t.Fatalf("第 %d 次派生结果漂移：%q/%q/%q vs %q/%q/%q",
				i+1, v2, k2, c2, view, key, cache)
		}
	}
}
