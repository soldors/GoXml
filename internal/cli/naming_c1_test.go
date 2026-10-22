package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// C1 验收冻结清单的最小补齐（2026-10-22，卡 §10.10）
//
// 本文件只补**证据**，不改生产逻辑。三条对应冻结清单的 G2 / G5 / G6：
//
//	G2 绑定全在 <For> 内的页面（契约 §16.5.1 第 4 行）不得被 textCache 误报；
//	G5 命名冲突时不得**同时**吐出 Analyzer 的 GXML-ROOT-002（不重复诊断）；
//	G6 全部命中缓存时不得因"把命中输入纳入预检"而失败。
//
// 每条都自带正对照：没有对照，"不出现某诊断/不失败"可能只是永真断言。
// ============================================================================

// forOnlyPageGxml 是**绑定全在 <For> 内**的页面：有 VM、有绑定语法，
// 但 `<For>` 体内不分配槽位（列表契约 §6.2）→ 槽位数 0 → 不声明 TextCache。
//
// 它对夹具的要求比另外三页高：需要切片字段与元素字段，miniproj 的 VM 没有，
// 因此以下用例会覆写 vm.go（见 extendVMForList）。
const forOnlyPageGxml = `<Goxml Version="1" ViewModel="VM">
  <Page Route="/">
    <For Each="item" In="{Items}">
      <Text Value="{item.Name}"/>
    </For>
  </Page>
</Goxml>
`

// extendVMForList 覆写夹具 VM，补上切片字段与元素类型。
//
// 独立于 copyFixture 的 miniproj/vm.go：那份夹具保持最小（Title/Count），
// 不为了本用例把一个公共夹具改成"什么都有"。
func extendVMForList(t *testing.T, dir, pkg string) {
	t.Helper()
	src := `package ` + pkg + `

// Item 是列表元素（For 绑定用）。
type Item struct{ Name string }

// VM 是 ViewModel：在最小夹具基础上补 Items 字段。
type VM struct {
	Title string
	Count int
	Items []Item
}

// Increment 是零参事件处理器。
func (vm *VM) Increment() { vm.Count++ }
`
	if err := os.WriteFile(filepath.Join(dir, "vm.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNamingForOnlyPageTextCacheNotOverreported：G2 的 CLI 端。
//
// 判据落在**槽位数**上：这类页面既有 VM 也有绑定（不是"无绑定"的纯事件页），
// 若把收窄写成 `usesVM == false` 或"页面里出现过 `{...}` 就计入"，本用例必红。
func TestNamingForOnlyPageTextCacheNotOverreported(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)

	// ---- 先用绑定页产物拿到 text cache 类型名（从产物派生，不写死）----
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

	// ---- 换成"绑定全在 For 内"的页面 + 包内同名用户符号 ----
	if err := os.Remove(genPath); err != nil {
		t.Fatal(err)
	}
	writeGxml(t, tmp, "home.gxml", forOnlyPageGxml)
	extendVMForList(t, tmp, "miniproj")
	writeUserDecl(t, tmp, "miniproj", cacheType)

	out.Reset()
	errb.Reset()
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("For-only 页面不声明 %q，用户符号不构成冲突，应生成成功；退出码 = %d\nstderr:\n%s",
			cacheType, code, errb.String())
	}

	// ---- 前提复核：产物确实不声明该类型（否则"不该报冲突"没有依据）----
	after, err := os.ReadFile(genPath)
	if err != nil {
		t.Fatalf("产物不存在: %v", err)
	}
	if strings.Contains(string(after), "type "+cacheType+" struct") {
		t.Fatalf("For-only 页面产物声明了 %q，与「槽位为 0 不声明」矛盾:\n%s",
			cacheType, headOfStatic(string(after), 40))
	}
}

// TestNamingConflictDiagnosticsNotDuplicated：G5。
//
// 场景是 existing-symbol 冲突——**这恰好也是 Analyzer 会报 `GXML-ROOT-002`
// 的场景**（它加载目标包时看到 redeclared）。当前行为正确：预检在阶段一之前
// 返回，Analyzer 根本不会运行。但"正确"不等于"回归时测试会红"，所以：
//
//	判据 1：stderr 里命名诊断**恰好 1 条**（不重不漏）；
//	判据 2：不出现 GXML-ROOT-002；
//	正对照：同一工程里制造一次真正的包内重复声明（无命名冲突），
//	        证明 GXML-ROOT-002 这条路**确实会产生诊断**——否则判据 2 是永真。
func TestNamingConflictDiagnosticsNotDuplicated(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)

	// ---- 先正常生成一次，得到产物符号（不写死字面量）----
	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitOK {
		t.Fatalf("预置生成失败（退出码 %d）:\n%s", code, errb.String())
	}
	genData, err := os.ReadFile(filepath.Join(tmp, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := packageTypeNames(t, genData)
	if len(names) == 0 {
		t.Fatalf("产物里没有包级类型，无法构造冲突:\n%s", genData)
	}

	// ---- 制造 existing 冲突 ----
	writeUserDecl(t, tmp, "miniproj", names[0])

	out.Reset()
	errb.Reset()
	if code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb); code != ExitFailure {
		t.Fatalf("冲突应失败，退出码 = %d；stdout:\n%s", code, out.String())
	}
	if got := strings.Count(errb.String(), "CODEGEN-NAME-"); got != 1 {
		t.Errorf("命名诊断应恰好 1 条，实际 %d 条:\n%s", got, errb.String())
	}
	if !strings.Contains(errb.String(), ":0:0: CODEGEN-NAME-001:") {
		t.Errorf("应给出冻结格式的 CODEGEN-NAME-001:\n%s", errb.String())
	}
	// 判据 2（G5 的核心）：不得同时吐出 Analyzer 的 GXML-ROOT-002。
	// 预检在阶段一之前返回，Analyzer 根本不会运行；若将来把预检挪到生成之后，
	// 这里会红——正对照证明 ROOT-002 在这套工程里确实可产生，不是永真。
	if strings.Contains(errb.String(), "GXML-ROOT-002") {
		t.Errorf("命名冲突路径不应同时出现 GXML-ROOT-002（重复诊断）:\n%s", errb.String())
	}

	// ---- 正对照：包内真正的重复声明会走 Analyzer 的 GXML-ROOT-002 ----
	// 另起一个工程：源无命名冲突，但包内两个文件声明同一类型。
	ctrl := tempProjectInRepo(t)
	copyFixture(t, ctrl)
	dup := "package miniproj\n\ntype DupType struct{}\n"
	if err := os.WriteFile(filepath.Join(ctrl, "dup_a.go"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctrl, "dup_b.go"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	var out2, errb2 bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(ctrl, "home.gxml")}, &out2, &errb2); code == ExitOK {
		t.Fatalf("包内重复声明应导致失败（这是正对照的前提）:\nstdout:\n%s", out2.String())
	}
	if !strings.Contains(errb2.String(), "GXML-ROOT-002") {
		t.Fatalf("正对照不成立：包内重复声明没有产生 GXML-ROOT-002，判据 2 的'不出现'将是永真:\n%s",
			errb2.String())
	}
	// 正对照里也不该出现命名诊断：DupType 不是生成符号。
	if strings.Contains(errb2.String(), "CODEGEN-NAME-") {
		t.Errorf("无关的重复声明不应报命名冲突:\n%s", errb2.String())
	}
}

// TestNamingPrecheckSkippedOnAllHit：G6。
//
// 预检只对**未命中**输入（`precheckPackageSymbols` 的文档决定）。若把命中
// 输入也算进来，"磁盘上恰好有同名用户符号"就会让一个什么都不写的运行失败。
//
// 构造难点：新增一个用户 .go 文件会改变 K3 包指纹 → 变成未命中，就测不到
// 命中路径。因此这里复用**Manifest 已登记的产物路径**（K3 排除 Manifest 产物），
// 把它改写成"用户声明"（去掉保护头）——包内出现同名符号，而缓存键不变。
//
// 变红方式：把 `precheckPackageSymbols(missResolved, ...)` 改成 `resolved` → 本用例红。
func TestNamingPrecheckSkippedOnAllHit(t *testing.T) {
	tmp := tempProjectInRepo(t)
	copyFixture(t, tmp)                              // home.gxml（绑定页）+ vm.go
	writeGxml(t, tmp, "detail.gxml", staticPageGxml) // 第二个源，让首页批次有两个产物

	// ---- 首次：两个都未命中，正常生成并写入 Manifest + 缓存 ----
	var out, errb bytes.Buffer
	if code := Run([]string{"generate", tmp}, &out, &errb); code != ExitOK {
		t.Fatalf("首次生成失败（退出码 %d）:\n%s", code, errb.String())
	}
	homeGen, err := os.ReadFile(filepath.Join(tmp, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := packageTypeNames(t, homeGen)
	if len(names) == 0 {
		t.Fatalf("home 产物里没有包级类型:\n%s", homeGen)
	}

	// ---- 把 detail 的产物改写成用户声明（该路径在 Manifest 里 → 不进 K3）----
	userLike := "package miniproj\n\ntype " + names[0] + " struct{}\n"
	if err := os.WriteFile(filepath.Join(tmp, "detail_gen.go"), []byte(userLike), 0o644); err != nil {
		t.Fatal(err)
	}

	// ---- 只跑 home：全部命中 → 预检不参与 → 必须成功 ----
	out.Reset()
	errb.Reset()
	code := Run([]string{"generate", filepath.Join(tmp, "home.gxml")}, &out, &errb)
	if code != ExitOK {
		t.Fatalf("全部命中时不应失败（预检只该看未命中输入）；退出码 = %d\nstdout:\n%s\nstderr:\n%s",
			code, out.String(), errb.String())
	}
	// 前提复核：确实走了命中路径（否则本用例测的是别的东西）。
	if !strings.Contains(out.String(), "缓存命中") {
		t.Fatalf("前提不成立：本次没有走缓存命中路径，断言无意义\nstdout:\n%s", out.String())
	}
	if strings.Contains(errb.String(), "CODEGEN-NAME-") {
		t.Errorf("命中路径不该产出命名诊断:\n%s", errb.String())
	}

	// ---- 正对照：--no-cache 强制未命中 → 同一个符号必须被报出来 ----
	// 没有这条，"成功"可能只是因为 names[0] 压根不构成冲突。
	out.Reset()
	errb.Reset()
	code = Run([]string{"generate", filepath.Join(tmp, "home.gxml"), "--no-cache"}, &out, &errb)
	if code != 2 { // 契约冻结：命名冲突退出码 2（不引用常量，避免常量漂移时跟着绿）
		t.Fatalf("正对照不成立：--no-cache 下应因命名冲突失败（退出码 2），实际 %d\nstderr:\n%s",
			code, errb.String())
	}
	if !strings.Contains(errb.String(), ":0:0: CODEGEN-NAME-001:") ||
		!strings.Contains(errb.String(), names[0]) {
		t.Fatalf("正对照不成立：应报 CODEGEN-NAME-001 并点名 %q:\n%s", names[0], errb.String())
	}
}
