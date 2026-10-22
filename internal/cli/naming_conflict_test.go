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

// eventOnlyPageGxml 是**纯事件页**：有 ViewModel、有事件，但没有任何 `{...}` 绑定。
//
// 它的存在是为了钉住收窄判据必须是 `bindSlots > 0` 而**不是** `usesVM`：
// 纯事件页 usesVM 为真，却不声明 goxmlTextCache<Source>。
const eventOnlyPageGxml = `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
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
	// 退出码字面量：契约冻结命名冲突为 2。这里**不**只断言 ExitFailure——
	// 常量本身若被改动，断言会跟着绿，就与"退出码 2"这条对外承诺脱钩了（C1 冻结清单 G3）。
	if code != 2 {
		t.Fatalf("冲突时应以退出码 2 失败，实际 = %d；stdout:\n%s", code, out.String())
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

// TestNamingStaticPageTextCacheNotOverreported：C5 收窄后，静态页**不再**被误报。
//
// 历史：C4 接线后本用例叫 TestNamingStaticPageTextCacheOverreport，钉住的是
// 「预检只看路径 → 静态页也被算上 textCache → 包内有同名用户符号就生成不了」
// 这个真实代价（卡 §10.4.2 第 1 条限制）。C5 按 `bindSlots > 0` 收窄后，
// 该行为必须消失——所以本用例的期望**整体反过来了**，并保留反向对照：
//
//   - 静态页（bindSlots=0，产物不声明该类型）+ 包内同名用户符号 → **生成成功**；
//   - 同一份用户符号 + **绑定页**（bindSlots>0，产物真声明它）→ **仍然拦下**
//     （CODEGEN-NAME-001）。没有这一条，收窄就可能退化成"根本不查 textCache"。
func TestNamingStaticPageTextCacheNotOverreported(t *testing.T) {
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

	// ---- 反向对照（同一份用户符号必须对绑定页仍然生效）----
	writeUserDecl(t, tmp, "miniproj", cacheType)
	out.Reset()
	errb.Reset()
	code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("绑定页声明 %q，与用户符号撞名时必须失败，退出码 = %d；stdout:\n%s",
			cacheType, code, out.String())
	}
	if !strings.Contains(errb.String(), ":0:0: CODEGEN-NAME-001:") || !strings.Contains(errb.String(), cacheType) {
		t.Errorf("诊断应点名 %q 并按冻结格式给出 CODEGEN-NAME-001:\n%s", cacheType, errb.String())
	}

	// ---- 换成静态页：同一份用户符号在包里，生成必须**成功** ----
	if err := os.Remove(genPath); err != nil {
		t.Fatal(err)
	}
	writeGxml(t, tmp, "home.gxml", staticPageGxml)

	out.Reset()
	errb.Reset()
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("静态页不声明 %q，用户符号不构成冲突，应生成成功；退出码 = %d\nstderr:\n%s",
			cacheType, code, errb.String())
	}
	staticGen, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("静态页产物不存在: %v", err)
	}
	// 前提复核：产物确实不声明该类型——否则"不该报冲突"的判断本身就没依据。
	if strings.Contains(string(staticGen), "type "+cacheType+" struct") {
		t.Fatalf("静态页产物声明了 %q，收窄判据的前提被破坏:\n%s",
			cacheType, headOfStatic(string(staticGen), 40))
	}
}

// TestNamingEventOnlyPageTextCacheNotOverreported：纯事件页（usesVM 真、槽位 0）
// 同样不该被 textCache 撞名误报。
//
// 这条用例是判据选择的反证：若收窄写成 `usesVM == false` 才排除，纯事件页
// 仍会被误报（它的 usesVM 为真）；只有用 `bindSlots > 0` 才是对的。
func TestNamingEventOnlyPageTextCacheNotOverreported(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)

	// 用绑定页产物拿到 text cache 类型名（不写死字面量）。
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

	// 换成纯事件页 + 包内同名用户符号。
	if err := os.Remove(genPath); err != nil {
		t.Fatal(err)
	}
	writeGxml(t, tmp, "home.gxml", eventOnlyPageGxml)
	writeUserDecl(t, tmp, "miniproj", cacheType)

	out.Reset()
	errb.Reset()
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("纯事件页不声明 %q，应生成成功；退出码 = %d\nstderr:\n%s",
			cacheType, code, errb.String())
	}
	eventGen, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("纯事件页产物不存在: %v", err)
	}
	// 前提复核两件：确实生成了（不能是靠失败后保留旧产物蒙过），且不含该类型。
	if strings.Contains(string(eventGen), "type "+cacheType+" struct") {
		t.Fatalf("纯事件页产物声明了 %q，与「槽位为 0 不声明」矛盾:\n%s",
			cacheType, headOfStatic(string(eventGen), 40))
	}
}

// TestNamingConflictStdoutSilentMultiFile 覆盖 S4 的 stdout 那一半，
// 并**自带正对照**。
//
// 为什么必须有正对照：`共 %d 个文件生成成功（按路径排序处理）` 只在
// len(sorted) > 1 时打印（pipeline.go）。在单文件用例里断言它「不得出现」
// 是**永真**的——那条断言会变成装饰品，永远绿，也永远证明不了任何事。
// 所以这里先用两个源成功生成一次，证明该行**确实会出现**，再触发冲突，
// 断言两行文案都不出现。
func TestNamingConflictStdoutSilentMultiFile(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp) // home.gxml（绑定页）+ vm.go
	// 第二个源：静态页，与 home 不同名 → 首次生成必然两个都未命中。
	writeGxml(t, tmp, "detail.gxml", staticPageGxml)

	// ---- 正对照：两个文件成功时，两行文案都必须出现 ----
	var out, errb bytes.Buffer
	if code := Run([]string{"generate", tmp}, &out, &errb); code != ExitOK {
		t.Fatalf("两文件应能生成（退出码 %d）:\n%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "已生成") {
		t.Fatalf("成功时 stdout 应有「已生成」:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "共 2 个文件生成成功") {
		t.Fatalf("成功时 stdout 应有「共 2 个文件生成成功」——正对照不成立，后面的断言就没有意义:\n%s",
			out.String())
	}

	// ---- 触发冲突：包内既有用户符号占用 detail 产物的一个类型名 ----
	detailGen, err := os.ReadFile(filepath.Join(tmp, "detail_gen.go"))
	if err != nil {
		t.Fatalf("detail 产物不存在: %v", err)
	}
	names := packageTypeNames(t, detailGen)
	if len(names) == 0 {
		t.Fatalf("detail 产物里没有包级类型，无法构造冲突:\n%s", detailGen)
	}
	writeUserDecl(t, tmp, "miniproj", names[0])

	// --no-cache：第一次已把缓存写满，否则第二次会全部命中而根本不进预检。
	out.Reset()
	errb.Reset()
	code := Run([]string{"generate", tmp, "--no-cache"}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("冲突时应失败，退出码 = %d；stdout:\n%s", code, out.String())
	}
	// S4：失败路径的 stdout 不得出现任何「生成成功」的语气。
	for _, forbidden := range []string{"已生成", "个文件生成成功"} {
		if strings.Contains(out.String(), forbidden) {
			t.Errorf("冲突失败时 stdout 不得出现 %q:\n%s", forbidden, out.String())
		}
	}
}

// TestNamingDoesNotMaskCompileErrors：前端未通过的源，报错必须是**编译错误**，
// 不能被命名预检伪装成符号冲突。
//
// 为什么需要这条：C5 收窄要判断 `bindSlots > 0`，而这个值只能由前端给出。
// 前端失败时预检会**回退为保守上界**并继续——这是刻意的，但如果实现成
// 「预检自己也跑前端，失败就把前端诊断当命名冲突报出来」，用户会看到
// 一个指错方向的 CODE（去改命名，实际是语法错）。本用例把正确归属钉住。
func TestNamingDoesNotMaskCompileErrors(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)
	// 故意未闭合的标签：Parser 必然失败。
	writeGxml(t, tmp, "broken.gxml", `<Goxml Version="1">
  <Page Route="/broken">
    <Text Value="x">
  </Page>
</Goxml>
`)

	var out, errb bytes.Buffer
	code := Run([]string{"generate", tmp}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("语法错误应失败，退出码 = %d；stdout:\n%s", code, out.String())
	}
	if !strings.Contains(errb.String(), "GXML-") {
		t.Errorf("stderr 应给出编译/解析诊断（含 GXML-* 码）:\n%s", errb.String())
	}
	if strings.Contains(errb.String(), "CODEGEN-NAME-") {
		t.Errorf("stderr 不得出现命名冲突码——那会把语法错误指向错误的修复方向:\n%s", errb.String())
	}
	// 失败路径仍应零产物。
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "_gen.go") {
			t.Errorf("失败后不应出现产物: %s", e.Name())
		}
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
