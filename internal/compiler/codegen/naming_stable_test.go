package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// ============================================================================
// C5 / 验收矩阵 A7：单文件产物「除改名涉及的符号外，其余字节不变」
//
// 卡 §8.1 A7 的原话是「防止顺手改坏」。它要防的不是命名规则本身，而是
// 「为了解决命名问题而顺带改了生成器的别处」——那种改动会让所有页面的产物
// 都悄悄变形，而 A1–A6 全都只关心符号与编译，看不出来。
//
// 没有 golden 基线的前提下，用两条互补的判据：
//
//  1. **包级声明集合精确等于预期**：三个类型 + 一个构造函数，不多不少。
//     多一个说明顺手加了东西，少一个说明丢了东西。
//  2. **归一化后字节相等**：同一份页面内容、两个不同的文件名，各自生成产物后
//     把「由源名派生的符号」与保护头里的源文件名替换成占位符 → 两份产物必须
//     **逐字节相同**。这直接表达「改名只影响那几个符号」。
//
// ⚠️ **判据 2 的已知局限（必须写明，否则会被高估）**：它比较的是「同一份代码
// 生成的产物」两两之间的一致性，因此**抓不到全局形状变化**——如果生成器被整体
// 改坏（例如所有页面都多出一行、字段顺序全变），两侧会一起变，归一化后仍然
// 相等。要抓那类问题需要**签入的 golden 文件**（契约 §6 的要求），本仓目前
// 没有 golden 机制；本判据只承担"改名派生一致性"，不承担"全局形状冻结"。
// 这也是 A7 只能部分覆盖「防止顺手改坏」的原因，已记入 dev-log §6.8 的遗留项。
//
// 变红方式（已实测，见 dev-log `2026-10-22.md` §6.7）：
//   - 把 renderKeyDecl 的 keyType 换成固定名 → 判据 2 红（两侧占位符不一致）；
//   - 删掉 NewXxxView 的生成 → 判据 1 红（包级函数数不符）。
// ============================================================================

// packageDecls 解析产物，返回包级类型名与函数名（各按出现顺序）。
func packageDecls(t *testing.T, src []byte) (types, funcs []string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("解析产物失败: %v", err)
	}
	for _, d := range f.Decls {
		switch dd := d.(type) {
		case *ast.GenDecl:
			if dd.Tok != token.TYPE {
				continue
			}
			for _, sp := range dd.Specs {
				if ts, ok := sp.(*ast.TypeSpec); ok {
					types = append(types, ts.Name.Name)
				}
			}
		case *ast.FuncDecl:
			if dd.Recv == nil {
				funcs = append(funcs, dd.Name.Name)
			}
		}
	}
	return types, funcs
}

// normalizeDerived 把「由源路径派生的符号」替换成占位符，留下其余字节。
//
// 替换顺序必须**先长后短**：New<View> 含有 <View>，先替换短的会把长的切碎，
// 导致两侧占位符形态不一致而假红。
func normalizeDerived(t *testing.T, src []byte, sourcePath string) string {
	t.Helper()
	view, err := viewSymbol(sourcePath)
	if err != nil {
		t.Fatalf("viewSymbol: %v", err)
	}
	keyType, cacheType, err := generatedTypeNames(sourcePath)
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}
	s := string(src)
	s = strings.ReplaceAll(s, "New"+view, "<<NEW>>")
	s = strings.ReplaceAll(s, view, "<<VIEW>>")
	s = strings.ReplaceAll(s, cacheType, "<<CACHE>>")
	s = strings.ReplaceAll(s, keyType, "<<KEY>>")
	// 保护头里的源文件名也随源而变，属于「派生」，一并归一化。
	s = strings.ReplaceAll(s, baseName(sourcePath), "<<SRC>>")
	return s
}

func TestNamingSingleFileStable(t *testing.T) {
	const body = `<Column><Text Value="静态"/><Text Value="{Title}"/><Button Text="加一" Click="Increment"/></Column>`
	opts := func(name string) Options {
		return Options{
			PackageName: "demo",
			ModulePath:  "github.com/goxml/goxml",
			SourcePath:  name,
		}
	}

	alphaSrc := mustGen(t, body, opts("alpha.gxml"))
	betaSrc := mustGen(t, body, opts("beta.gxml"))

	// ---- 判据 1：包级声明集合精确等于预期（三个类型 + 一个构造函数）----
	view, err := viewSymbol("alpha.gxml")
	if err != nil {
		t.Fatalf("viewSymbol: %v", err)
	}
	keyType, cacheType, err := generatedTypeNames("alpha.gxml")
	if err != nil {
		t.Fatalf("generatedTypeNames: %v", err)
	}
	types, funcs := packageDecls(t, alphaSrc)
	wantTypes := map[string]bool{view: true, keyType: true, cacheType: true}
	if len(types) != len(wantTypes) {
		t.Errorf("包级类型数 = %d，期望 %d：%v", len(types), len(wantTypes), types)
	}
	for _, got := range types {
		if !wantTypes[got] {
			t.Errorf("出现了预期外的包级类型 %q（顺手改坏的典型信号）：%v", got, types)
		}
		delete(wantTypes, got)
	}
	for missing := range wantTypes {
		t.Errorf("缺少包级类型 %q：%v", missing, types)
	}
	if len(funcs) != 1 || funcs[0] != "New"+view {
		t.Errorf("包级函数 = %v，期望只有 %q", funcs, "New"+view)
	}

	// ---- 判据 2：归一化后两份产物逐字节相同 ----
	a := normalizeDerived(t, alphaSrc, "alpha.gxml")
	b := normalizeDerived(t, betaSrc, "beta.gxml")
	if a != b {
		t.Errorf("同一页面内容、不同源名，归一化后产物应逐字节相同，实际不同。\n--- alpha ---\n%s\n--- beta ---\n%s",
			headOf(a, 60), headOf(b, 60))
	}
}
