package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// TASK-CODEGEN-NAMING-001 切片 C4：预检接线后的冲突与副作用验收
//
// 对应卡 §8.2（副作用）与 §8.1 里和 CLI 直接相关的几条（A2 用户符号、
// A4 pascal 塌缩、A3 不同目录同名、A5 静态+绑定同包）。
//
// 为什么必须在本包测：codegen 侧的用例只证明「预检能报出冲突」，
// 证明不了「报出之后 CLI 真的没写盘」——零副作用是**接线**的性质，
// 只有从 Run 入口跑一遍才算数。
//
// 变红方式（已实测，见 dev-log `2026-10-22.md` §5.3）：
//   - 把 runPipeline 里的 precheckPackageSymbols 调用删掉 → 冲突用例全红；
//   - 把预检挪到阶段一之后 → TestNamingConflictNoWrites 的 mtime/Manifest 断言红。
// ============================================================================

// ---- 夹具 ----

// staticPageGxml 是**静态页**：无 ViewModel、无绑定、无事件。
const staticPageGxml = `<Goxml Version="1">
  <Page Route="/">
    <Text Value="静态页"/>
  </Page>
</Goxml>
`

// bindingPageGxml 是**绑定页**：有 ViewModel、有 `{...}` 绑定。
const bindingPageGxml = `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
    <Text Value="{Title}"/>
    <Button Text="加一" Click="Increment"/>
  </Page>
</Goxml>
`

// writeGxml 写一个 .gxml 源到 dir。
func writeGxml(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeUserDecl 写一个**用户**源文件，声明若干包级类型（不带保护头）。
//
// 不带保护头是刻意的：它是"用户代码"，预检必须把它算作占用方，
// 而不是被 isOwnArtifact 排除掉。
func writeUserDecl(t *testing.T, dir, pkg string, symbols ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("package " + pkg + "\n\n")
	for _, s := range symbols {
		b.WriteString("type " + s + " struct{}\n")
	}
	p := filepath.Join(dir, "user_decl.go")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// packageTypeNames 解析 Go 源码，返回包级**类型**名（按出现顺序）。
func packageTypeNames(t *testing.T, src []byte) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("解析产物失败: %v", err)
	}
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range gd.Specs {
			if ts, ok := sp.(*ast.TypeSpec); ok {
				out = append(out, ts.Name.Name)
			}
		}
	}
	return out
}

// textCacheTypeName 从产物里挑出 text cache 类型名。
//
// 名字由**产物**派生，不写死字面量；但**不用 t.Skip 兜底**（复审意见）：
// 绑定页产物按卡 §10.7.4 的三类页面表**必须**声明 `goxmlTextCache<Source>`，
// 找不到就说明契约或实现变了，测试要**响亮失败**要求复核，而不是静默跳过。
func textCacheTypeName(t *testing.T, src []byte) string {
	t.Helper()
	for _, n := range packageTypeNames(t, src) {
		if strings.Contains(n, "TextCache") {
			return n
		}
	}
	t.Fatalf("绑定页产物里没有形如 *TextCache* 的类型名（卡 §10.7.4 要求它存在）：\n%s",
		headOfStatic(string(src), 40))
	return ""
}

// ---- 快照 ----

// snapshotDir 快照目录内所有文件内容（相对路径 → 内容）。
// 目录不存在返回空 map（"还没有缓存"与"缓存被改动"要区分开）。
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return out
		}
		t.Fatalf("快照目录 %s 失败: %v", dir, err)
	}
	return out
}

// diffSnapshots 比较两个快照，返回人类可读的差异（无差异返回空串）。
func diffSnapshots(before, after map[string]string) string {
	var b strings.Builder
	keys := make([]string, 0, len(before)+len(after))
	seen := make(map[string]bool)
	for k := range before {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range after {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		oldV, okOld := before[k]
		newV, okNew := after[k]
		switch {
		case !okNew:
			b.WriteString("消失: " + k + "\n")
		case !okOld:
			b.WriteString("新增: " + k + "\n")
		case oldV != newV:
			b.WriteString("内容变化: " + k + "\n")
		}
	}
	return b.String()
}

// ---- 用例 ----

// TestNamingConflictNoWrites：冲突时**什么都不许留下**（卡 §8.2 的 S1–S4）。
//
// 预置三者（产物 / Manifest / 缓存），再制造一个真实冲突（包内用户代码
// 声明了生成物要用的符号），然后断言三者逐字节不变、产物 mtime 不变、
// stdout 不出现「已生成」。
//
// 一个必须知道的事实（变异取证时实测）：这类"包内既有声明"冲突，
// Analyzer 也会报——它加载目标包时会看到 `redeclared`，报的是
// `GXML-ROOT-002`，但那要等阶段一跑完。预检的价值是**更早**（在阶段一
// 之前）给出 `CODEGEN-NAME-*`，并且在**批内冲突**场景不可替代：那两个
// 产物都还没写盘，磁盘上没有冲突可加载，Analyzer 不会报错。
// 因此本用例坚持断言 `CODEGEN-NAME-`：它同时是"预检在位"的判据。
func TestNamingConflictNoWrites(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)

	root, err := findModuleRoot(tmp)
	if err != nil {
		t.Fatalf("findModuleRoot: %v", err)
	}

	// ---- 预置：先成功生成一次 ----
	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("预置生成失败（退出码 %d）:\n%s", code, errb.String())
	}
	genPath := filepath.Join(tmp, "home_gen.go")
	genData, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("预置产物不存在: %v", err)
	}
	genStat, err := os.Stat(genPath)
	if err != nil {
		t.Fatal(err)
	}
	manBefore := snapshotDir(t, filepath.Dir(manifestPathOverride))
	cacheBefore := snapshotDir(t, cacheDir(root))
	if len(cacheBefore) == 0 {
		t.Fatalf("预置后缓存目录应有记录: %s", cacheDir(root))
	}

	// ---- 触发冲突：包内用户代码声明产物要用的符号 ----
	// 符号从产物派生：避免命名规则演进后本用例还在断言一个旧名字。
	names := packageTypeNames(t, genData)
	if len(names) == 0 {
		t.Fatalf("产物里没有包级类型，无法构造冲突:\n%s", genData)
	}
	writeUserDecl(t, tmp, "miniproj", names[0])

	// 让"写入"与"未写入"在 mtime 上可区分（本机时间粒度足够，但留一点余量）。
	time.Sleep(10 * time.Millisecond)

	out.Reset()
	errb.Reset()
	code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("冲突时应失败，退出码 = %d；stdout:\n%s", code, out.String())
	}
	if !strings.Contains(errb.String(), ":0:0: CODEGEN-NAME-001:") {
		t.Errorf("stderr 应输出冻结格式 `file:0:0: CODEGEN-NAME-001: message`:\n%s", errb.String())
	}
	if !strings.Contains(errb.String(), names[0]) {
		t.Errorf("诊断应点名冲突符号 %q:\n%s", names[0], errb.String())
	}
	if strings.Contains(out.String(), "已生成") {
		t.Errorf("失败时 stdout 不得出现「已生成」:\n%s", out.String())
	}

	// ---- S1：产物内容与 mtime 均未变 ----
	after, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("产物消失: %v", err)
	}
	if string(after) != string(genData) {
		t.Error("冲突失败后产物内容被改动")
	}
	afterStat, err := os.Stat(genPath)
	if err != nil {
		t.Fatal(err)
	}
	if !afterStat.ModTime().Equal(genStat.ModTime()) {
		t.Errorf("冲突失败后产物 mtime 被改动: %v → %v", genStat.ModTime(), afterStat.ModTime())
	}

	// ---- S2：Manifest 字节不变 ----
	if d := diffSnapshots(manBefore, snapshotDir(t, filepath.Dir(manifestPathOverride))); d != "" {
		t.Errorf("Manifest 目录被改动:\n%s", d)
	}

	// ---- S3：缓存目录文件集合与内容均不变 ----
	if d := diffSnapshots(cacheBefore, snapshotDir(t, cacheDir(root))); d != "" {
		t.Errorf("缓存目录被改动:\n%s", d)
	}
}

// TestNamingConflictSameBatchRejected：同包 pascal 塌缩（A4）在 CLI 层被拦下，
// 且目录内**不出现任何产物**（S5：无预置时也不留残骸）。
func TestNamingConflictSameBatchRejected(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)
	writeGxml(t, tmp, "a-b.gxml", bindingPageGxml)
	writeGxml(t, tmp, "a_b.gxml", bindingPageGxml)

	var out, errb bytes.Buffer
	code := Run([]string{"generate", tmp}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("同包塌缩应失败，退出码 = %d；stdout:\n%s", code, out.String())
	}
	// 塌缩是**批内冲突**，码必须是 002，且格式必须是 `file:0:0: CODE: message`。
	if !strings.Contains(errb.String(), ":0:0: CODEGEN-NAME-002:") {
		t.Errorf("stderr 应输出冻结格式 `file:0:0: CODEGEN-NAME-002: message`:\n%s", errb.String())
	}
	if strings.Contains(out.String(), "已生成") {
		t.Errorf("失败时 stdout 不得出现「已生成」:\n%s", out.String())
	}

	// S5：无预置时不得留下产物或 .tmp 残骸。
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_gen.go") || strings.Contains(e.Name(), ".tmp") {
			t.Errorf("失败后目录内不应出现生成物/残骸: %s", e.Name())
		}
	}
}

// TestNamingConflictDoesNotCreateOutDir：--out-dir 指向**尚不存在**的目录时，
// 冲突失败不得把该目录创建出来（S5 的另一半：副作用是"建目录"也要避免）。
func TestNamingConflictDoesNotCreateOutDir(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)
	writeGxml(t, tmp, "a-b.gxml", bindingPageGxml)
	writeGxml(t, tmp, "a_b.gxml", bindingPageGxml)
	outRoot := filepath.Join(tmp, "out-not-created")

	var out, errb bytes.Buffer
	code := Run([]string{"generate", tmp, "--out-dir", outRoot}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("冲突应失败，退出码 = %d；stdout:\n%s", code, out.String())
	}
	if _, err := os.Stat(outRoot); !os.IsNotExist(err) {
		t.Errorf("失败时不得创建输出目录 %s（err=%v）", outRoot, err)
	}
}

// TestNamingStaticPageTextCacheOverreport：**已知误报**，在此钉住接线后的
// 真实代价（卡 §10.4.2 的第 1 条限制；收窄归 C5，判据是 `bindSlots > 0`）。
//
// 静态页不声明 goxmlTextCache<Source>，但预检的符号集合是"只看路径"的
// 保守上界，把它也算进去了 → 包里只要有同名用户符号，生成就被拦下。
//
// 本用例不消除该行为，只把它变成可复现、可复核的事实，并自带反向对照：
// 去掉那个用户符号后，同一份静态页能正常生成，且产物里**没有**该类型
// ——两条合起来才说明"拦下它是没有必要的"。
func TestNamingStaticPageTextCacheOverreport(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)

	// ---- 先用绑定页拿到 text cache 类型名（从产物派生，不写死）----
	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("预置生成失败（退出码 %d）:\n%s", code, errb.String())
	}
	genPath := filepath.Join(tmp, "home_gen.go")
	genData, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("预置产物不存在: %v", err)
	}
	cacheType := textCacheTypeName(t, genData)

	// ---- 换成静态页：先确认它本身能生成，且产物不声明该类型 ----
	if err := os.Remove(genPath); err != nil {
		t.Fatal(err)
	}
	writeGxml(t, tmp, "home.gxml", staticPageGxml)

	out.Reset()
	errb.Reset()
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("静态页应能正常生成（退出码 %d）:\n%s", code, errb.String())
	}
	staticGen, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("静态页产物不存在: %v", err)
	}
	if strings.Contains(string(staticGen), "type "+cacheType) {
		t.Fatalf("静态页产物声明了 %q，与「保守上界」前提矛盾:\n%s", cacheType, headOfStatic(string(staticGen), 40))
	}

	// ---- 反向对照：包内出现同名用户符号 → 预检拦下（误报）----
	writeUserDecl(t, tmp, "miniproj", cacheType)

	out.Reset()
	errb.Reset()
	code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("当前实现下应被预检拦下（误报），退出码 = %d；stdout:\n%s", code, out.String())
	}
	if !strings.Contains(errb.String(), ":0:0: CODEGEN-NAME-001:") || !strings.Contains(errb.String(), cacheType) {
		t.Errorf("诊断应点名 %q 并按冻结格式给出 CODEGEN-NAME-001:\n%s", cacheType, errb.String())
	}
}

// headOfStatic 取前 n 行（失败信息里不要糊一大段源码）。
func headOfStatic(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		return strings.Join(lines[:n], "\n") + "\n…（截断）"
	}
	return s
}

// TestNamingMultiSourceSamePackageOK：同包两个**不同名**的源可以一起生成（A5 的正向对照）。
//
// 没有这条，预检的"宁可误报"取向没人拦着：任何过度收紧都会先把这条打红。
func TestNamingMultiSourceSamePackageOK(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)
	// home.gxml 是绑定页，detail.gxml 用静态页：两种形态同包共存（A5）。
	writeGxml(t, tmp, "detail.gxml", staticPageGxml)

	var out, errb bytes.Buffer
	code := Run([]string{"generate", tmp}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("同包多源应生成成功，退出码 = %d:\n%s", code, errb.String())
	}
	for _, name := range []string{"home_gen.go", "detail_gen.go"} {
		if _, err := os.Stat(filepath.Join(tmp, name)); err != nil {
			t.Errorf("应生成 %s: %v", name, err)
		}
	}
}

// TestNamingSameBaseDifferentDirsOK：不同目录的同名源各自成包（A3）。
//
// C4 接线时这条场景是第一波红灯的来源——当时批内冲突没有按目标包分组。
func TestNamingSameBaseDifferentDirsOK(t *testing.T) {
	tmp := tempProjectInRepo(t)
	for _, sub := range []string{"alpha", "beta"} {
		dir := filepath.Join(tmp, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// 包内必须有 .go 才能定包名（packageDirName）。
		if err := os.WriteFile(filepath.Join(dir, "vm.go"), []byte("package "+sub+"\n\ntype VM struct{ Title string }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeGxml(t, dir, "row.gxml", strings.Replace(bindingPageGxml, "Click=\"Increment\"", "", 1))
	}

	var out, errb bytes.Buffer
	code := Run([]string{"generate", tmp}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("不同目录的同名源应各自生成成功，退出码 = %d:\n%s", code, errb.String())
	}
	for _, p := range []string{filepath.Join(tmp, "alpha", "row_gen.go"), filepath.Join(tmp, "beta", "row_gen.go")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("应生成 %s: %v", p, err)
		}
	}
}
