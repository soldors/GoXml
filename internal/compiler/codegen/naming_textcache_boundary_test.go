package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// TASK-CODEGEN-NAMING-001 切片 C2 验收补丁：text cache 保守预检的边界
//
// 背景（用户静态审查提出的待处理项）：
//
//	generatedPackageSymbols 无条件把 goxmlTextCache<Source> 纳入预检符号集合，
//	而 bindCacheDecl **只在绑定槽位数 > 0 时**才发射该类型（binding.go 的
//	`if slots == 0 { return "" }`，不是 usesVM——纯事件页 usesVM 为真但槽位为 0，
//	同样不声明）。
//	→ 静态页与**纯事件页**的产物都不声明 goxmlTextCache<Source>，预检却把它算作占用，
//	  若包内恰好有同名用户符号，会被报冲突：这是**过度报告**（误报）。
//
// 本文件不修改该行为，只把"保守上界"的**现状与后果**钉成可执行判据：
//
//	① 不声明该类型的页面形态（静态页 / 纯事件页）与预检集合的差距被显式记录；
//	② 该差距的后果：预检报冲突，而真实 go build 仍通过 → 证明是误报不是真冲突；
//	③ 反向对照：含绑定页确实声明该类型，同名用户符号会让 go build 真失败
//	   → 证明保守上界对绑定页是**必要**的，不能整体删掉。
//
// 归属裁决（本轮结论，已同步任务卡与 dev-log）：
//
//	**不在 C2 收窄，留给 C4/C5。**
//	理由有三：
//	  a. 保守上界是 C1 的有意设计（见 naming.go 里 generatedPackageSymbols 的函数
//	     注释）：宁可误报，不可漏报；误报的表现是"生成前被拦下并给出明确诊断"，
//	     漏报的表现是"生成成功、build 才炸"，后者正是 C2 要消灭的失败模式。
//	  b. 要精确判定必须知道"该源是否含绑定槽位"（bindSlots > 0），而这是前端事实，
//	     预检拿不到——namingTarget 只带路径，预检只解析包内既有 .go，不跑 Parser/Analyzer；
//	     引入该判定等于改预检的契约输入，超出 C2 范围。
//	  c. C4（CLI 接线）才是预检的唯一调用点（当前 detectNamingConflicts 零接线），
//	     也是收窄的自然时机；收窄必须同步更新本文件，否则本测试会红——
//	     这是防回归的钩子，不是负担。
//
// 给 C4 的提醒：收窄的判据是 `bindSlots > 0`，不是 `usesVM`。按后者收窄会让
// 纯事件页继续被误报（见 TestPrecheckOverreportsForEventOnlyPage）。
// ============================================================================

// TestTextCacheIsDeclaredOnlyForBindingPages 钉住「谁声明 goxmlTextCache<Source>」。
//
// 用 generatedTypeNames 保证测试与产物共用同一派生，不硬编码字面量后验。
func TestTextCacheIsDeclaredOnlyForBindingPages(t *testing.T) {
	_, cacheStatic, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	staticSrc := string(mustGen(t, `<Text Value="Hello"/>`,
		Options{PackageName: "demo", SourcePath: "home.gxml"}))
	bindingSrc := string(mustGen(t, `<Column><Text Value="Static"/><Text Value="{Title}"/></Column>`,
		Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "home.gxml"}))

	// 静态页不声明该类型，其实是**两道闸门**的第一道：静态分支根本不走
	// bindCacheDecl（codegen.go 的 `if g.usesVM`），纯事件页才靠第二道
	// `slots == 0 → return ""`。
	if strings.Contains(staticSrc, "type "+cacheStatic+" struct") {
		t.Errorf("静态页产物意外声明了 %q:\n%s", cacheStatic, headOf(staticSrc, 40))
	}
	if !strings.Contains(bindingSrc, "type "+cacheStatic+" struct") {
		t.Errorf("含绑定页产物应声明 %q:\n%s", cacheStatic, headOf(bindingSrc, 60))
	}
}

// TestPrecheckIsConservativeForStaticTextCache 记录保守上界本身。
//
// 判据：预检宣称的符号集合 ⊋ 静态页产物实际声明的符号集合，多出的正是 textCache。
func TestPrecheckIsConservativeForStaticTextCache(t *testing.T) {
	declared, err := generatedPackageSymbols("home.gxml")
	if err != nil {
		t.Fatalf("generatedPackageSymbols: %v", err)
	}

	staticSrc := string(mustGen(t, `<Text Value="Hello"/>`,
		Options{PackageName: "demo", SourcePath: "home.gxml"}))

	_, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	if !containsSymbol(declared, cacheType) {
		t.Fatalf("预检应保守地包含 %q；实际=%v", cacheType, declared)
	}
	if strings.Contains(staticSrc, "type "+cacheType+" struct") {
		t.Fatalf("前提已变：静态页产物也声明了 %q，保守上界不再是上界，"+
			"本文件与 naming.go 的注释需同步更新:\n%s", cacheType, headOf(staticSrc, 40))
	}

	// 保守上界 = 预检多算了 textCache 这一项；其余三项产物都真声明。
	// 符号全部由生产函数派生（不硬编码字面量）：命名规则演进时这里会跟着走，
	// 硬编码则会在规则变更时假红，且需要人去发现"测试为什么红"。
	// 注意 generatedTypeNames 的返回顺序是 (keyType, cacheType)。
	keyType, _, _ := generatedTypeNames("home.gxml")
	view, err := viewSymbol("home.gxml")
	if err != nil {
		t.Fatalf("viewSymbol: %v", err)
	}
	for _, sym := range []string{view, "New" + view, keyType} {
		if !containsSymbol(declared, sym) {
			t.Errorf("预检应包含 %q；实际=%v", sym, declared)
		}
		if !strings.Contains(staticSrc, sym) {
			t.Errorf("静态页产物应真声明 %q:\n%s", sym, headOf(staticSrc, 40))
		}
	}
}

// TestPrecheckOverreportsForStaticPageButBuildStillPasses 把「过度报告」变成可复现事实。
//
// 两段判据缺一不可：
//   - 预检必须报冲突（否则下面的 build 通过就没有对照意义）；
//   - 真实 go build 必须通过（证明产物没有真声明该类型 → 该冲突是误报）。
func TestPrecheckOverreportsForStaticPageButBuildStillPasses(t *testing.T) {
	_, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.go")
	if err := os.WriteFile(userFile, []byte("package demo\n\ntype "+cacheType+" struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if !hasConflict(conflicts, cacheType, namingConflictExisting) {
		t.Fatalf("保守上界下应报 %q 与用户符号冲突（这正是待处理项）；实际=%v", cacheType, symbolsOf(conflicts))
	}

	// 同一组合放进真实包：静态页不声明 textCache，因此**不会**重复声明。
	staticSrc := mustGen(t, `<Text Value="Hello"/>`, Options{PackageName: "demo", SourcePath: "home.gxml"})
	err = buildFilesInRepoPackage(t, map[string]string{
		"user.go":     "package demo\n\ntype " + cacheType + " struct{}\n",
		"gen_home.go": string(staticSrc),
	})
	if err != nil {
		t.Fatalf("静态页与同名用户符号同包应当仍能编译（预检此处是误报）: %v", err)
	}
}

// TestPrecheckOverreportsForEventOnlyPage 纯事件页（usesVM 为真、绑定槽位为 0）同样被误报。
//
// 为什么单独测：`bindCacheDecl` 的发射条件是 `slots > 0`，不是 `usesVM`。
// 纯事件页走 vm 但槽位为 0，产物同样不声明 goxmlTextCache<Source>。
// 这条把「误报面不止静态页」钉死——C4 若按 usesVM 收窄预检，纯事件页仍会被误报。
func TestPrecheckOverreportsForEventOnlyPage(t *testing.T) {
	_, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	opts := Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "home.gxml"}
	eventSrc := string(mustGen(t, `<Button Text="Del" Click="OnWrongArity"/>`, opts))

	// ① 产物不声明该类型（纯事件页槽位为 0）。
	if strings.Contains(eventSrc, "type "+cacheType+" struct") {
		t.Errorf("纯事件页产物意外声明了 %q:\n%s", cacheType, headOf(eventSrc, 40))
	}
	// ② 但页面确实带 vm（否则这条与静态页用例重复，失去意义）。
	if !strings.Contains(eventSrc, "vm *") {
		t.Errorf("纯事件页产物应持有 vm 引用:\n%s", headOf(eventSrc, 60))
	}
	// ③ 预检仍报冲突，真实 build 却通过 → 误报。
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "user.go"),
		[]byte("package demo\n\ntype "+cacheType+" struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if !hasConflict(conflicts, cacheType, namingConflictExisting) {
		t.Fatalf("应报 %q 冲突；实际=%v", cacheType, symbolsOf(conflicts))
	}
	if err := buildFilesInRepoPackage(t, map[string]string{
		"user.go":     "package demo\n\ntype " + cacheType + " struct{}\n",
		"gen_home.go": eventSrc,
	}); err != nil {
		t.Fatalf("纯事件页与同名用户符号同包应当仍能编译（预检此处是误报）: %v", err)
	}
}

// TestPrecheckTextCacheConflictIsRealForBindingPage 反向对照。
//
// 同一个符号名、同一个用户文件，换成含绑定页 → 产物真声明该类型 → build 真失败。
// 这条证明：保守上界对绑定页不是"保守"，而是**必要**；因此不能整体删掉 textCache 预检。
func TestPrecheckTextCacheConflictIsRealForBindingPage(t *testing.T) {
	_, cacheType, err := generatedTypeNames("home.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "user.go"),
		[]byte("package demo\n\ntype "+cacheType+" struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflicts, err := detectNamingConflicts(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if !hasConflict(conflicts, cacheType, namingConflictExisting) {
		t.Fatalf("应报 %q 冲突；实际=%v", cacheType, symbolsOf(conflicts))
	}

	bindingSrc := mustGen(t, `<Column><Text Value="Static"/><Text Value="{Title}"/></Column>`,
		Options{PackageName: "demo", ModulePath: "github.com/goxml/goxml", SourcePath: "home.gxml"})
	buildErr := buildFilesInRepoPackage(t, map[string]string{
		"user.go":     "package demo\n\ntype " + cacheType + " struct{}\n",
		"gen_home.go": string(bindingSrc),
	})
	if buildErr == nil {
		t.Fatalf("含绑定页产物真声明 %q，与同名用户符号同包应当编译失败；实际通过了", cacheType)
	}
	if !strings.Contains(buildErr.Error(), "redeclared") {
		t.Fatalf("失败原因应含 redeclared，实际: %v", buildErr)
	}
}

// buildFilesInRepoPackage 把若干文件写进仓库内临时包并真实 go build，返回错误而非 t.Fatal。
//
// 建在仓库内（而非独立模块）的原因同 naming_samepkg_test.go：
// 含绑定页的 ViewModel 在 internal/... 下，外部模块被 internal 规则禁止 import。
// 目录建在 testdata/ 之下，避免被运行期 `./...` 枚举撞上。
func buildFilesInRepoPackage(t *testing.T, files map[string]string) error {
	t.Helper()

	repoRoot := repoRootOf(t)
	dir := filepath.Join(repoRoot, "internal", "compiler", "codegen",
		"testdata", "tmpcache"+randomSuffix()+t.Name())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		t.Fatal(err)
	}
	// 用**无通配**的显式路径：实测 `go build ./<空目录>/...` 会打印
	// "matched no packages" 并退出 0——那会让"期望成功"的用例静默空转。
	// 去掉 `...` 后，路径不存在或无包时退出码非 0。
	cmd := exec.Command("go", "build", "./"+filepath.ToSlash(rel))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &buildFailure{err: err, out: string(out)}
	}
	return nil
}

// buildFailure 携带子进程输出，便于断言失败原因。
type buildFailure struct {
	err error
	out string
}

func (b *buildFailure) Error() string { return b.err.Error() + "\n" + b.out }

// containsSymbol 判断符号集合是否含某元素。
//
// 不复用包内已有的 contains——它的签名是 (haystack, needle string)，用于子串匹配；
// 这里是集合成员判定，语义不同，混用会让人误读断言。
func containsSymbol(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
