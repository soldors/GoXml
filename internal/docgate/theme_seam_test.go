package docgate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ============================================================================
// 门禁：主题/字体「路线 B」的文档一致性（ADR-0012 决策 1）
//
// 背景：字体规范 §6.2 曾要求生成代码「只通过主题引用字体：`th.Fonts.Family`」，
// 而代码生成输出契约 §4.3.1/§4.3.2 把 `Layout` 的主题参数固定为 Gio 的
// `*material.Theme`——后者没有 `Fonts.Family` 字段，两处**同时成立是不可能的**。
// §4.3.1 当年只记录了冲突，没有裁决，于是两份冻结文档长期各说各话。
//
// 路线 B 的裁决是：字体选择属于**运行时**主题构建过程，生成代码只接受
// 一个已经配好字体的 `*material.Theme`，不引用任何字体字段。
// 裁决写在 ADR 里还不够——文档会漂移，必须有一条门禁把「一边说 A、
// 另一边说 B」变成红灯。
//
// 本测试只做**文档与生成代码之间的一致性**检查，不验证字体功能本身
// （那是 FONT-001-A..D 的事）。
// ============================================================================

// readDoc 读取仓库内的文档；读不到即失败（不静默跳过）。
func readDoc(t *testing.T, rel ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{repoRoot(t)}, rel...)...)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 %s 失败（拒绝以通过蒙混）: %v", filepath.Join(rel...), err)
	}
	return string(data)
}

// TestThemeRouteBDocumentationConsistent 两侧文档必须描述同一条边界。
func TestThemeRouteBDocumentationConsistent(t *testing.T) {
	fontSpec := readDoc(t, "docs", "03-架构", "字体与文本渲染策略.md")
	outContract := readDoc(t, "docs", "03-架构", "代码生成输出契约.md")

	t.Run("字体规范不得要求生成代码引用字体字段", func(t *testing.T) {
		// 被路线 B 删除的要求。若它复活，生成代码与输出契约必然冲突。
		// 历史引用一律留在 ADR-0012 与本文件 §9.1 的文字描述里，
		// 规范正文不复述该写法——免得被读成现行要求。
		for _, forbidden := range []string{"th.Fonts.", "Fonts.Family"} {
			if strings.Contains(fontSpec, forbidden) {
				t.Errorf("字体规范仍在要求生成代码引用 %s——与输出契约 §4.3.2 "+
					"（th 是 *material.Theme，没有该字段）冲突。路线 B 下字体选择由运行时完成", forbidden)
			}
		}
	})

	t.Run("字体规范必须说明字体解析由运行时完成", func(t *testing.T) {
		// 正向要求：不能只删掉旧的、不写新的，否则规范变成沉默。
		for _, want := range []string{"pkg/runtime/theme", "运行时"} {
			if !strings.Contains(fontSpec, want) {
				t.Errorf("字体规范未说明字体由运行时主题构建过程负责（缺少 %q）", want)
			}
		}
		if !strings.Contains(fontSpec, "FontProvider") {
			t.Errorf("字体规范未登记 FontProvider 契约（应在 §6.3）")
		}
	})

	t.Run("输出契约仍固定 th 为 *material.Theme", func(t *testing.T) {
		if !strings.Contains(outContract, "th *material.Theme") {
			t.Errorf("输出契约未固定 `th *material.Theme`；路线 B 的前提是生成代码 " +
				"只接受 Gio 主题，若此处改成项目主题，本门禁与字体规范必须同步修改")
		}
	})
}

// TestGeneratedLayoutSignatureMatchesRouteB 生成代码侧必须是同一个签名。
//
// 文档一致还不够：真正决定「生成代码能否引用 `th.Fonts.Family`」的是 Codegen
// 实际写出的签名。这里锁住它，避免文档改了、生成器没改（或反过来）。
func TestGeneratedLayoutSignatureMatchesRouteB(t *testing.T) {
	src := readDoc(t, "internal", "compiler", "codegen", "codegen.go")

	if !strings.Contains(src, "Layout(gtx layout.Context, th *material.Theme)") {
		t.Errorf("Codegen 生成的 Layout 签名不是契约 §4.3.1 的 " +
			"`Layout(gtx layout.Context, th *material.Theme)`")
	}
	if strings.Contains(src, "th *theme.Theme") {
		t.Errorf("Codegen 不得生成项目自有主题参数（路线 B：生成代码不依赖 pkg/runtime/theme）")
	}
}

// ============================================================================
// 门禁：包边界（THEME-SEAM-001-5）
//
// 文档一致 + 签名一致还不够：真正决定边界的是 `pkg/runtime/app` 到底从哪
// 拿主题。若它自己 `material.NewTheme()`，字体决策就没有落点（FONT-001
// 无论怎么实现都不会被调用）。这里锁死这条调用关系。
// ============================================================================

// themeImport 返回本模块的 theme 包导入路径（从 go.mod 推导，不硬编码）。
func themeImport(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败（拒绝以通过蒙混）: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")) + "/pkg/runtime/theme"
		}
	}
	t.Fatal("go.mod 中没有 module 行")
	return ""
}

// parseGoFile 解析 Go 源文件；解析失败即失败，不静默跳过。
func parseGoFile(t *testing.T, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败（拒绝以通过蒙混）: %v", path, err)
	}
	return f
}

// importsPath 判定文件是否导入了给定路径。
func importsPath(f *ast.File, path string) bool {
	for _, im := range f.Imports {
		if strings.Trim(im.Path.Value, `"`) == path {
			return true
		}
	}
	return false
}

// callsSelector 判定文件中是否存在 `pkg.Sel(...)` 形式的**调用**。
//
// 用 AST 而不是字符串包含：注释或文档里引用 `material.NewTheme()` 是合法的
// （本包自己的注释就这么写），字符串匹配会误报，进而逼人删掉有价值的注释。
func callsSelector(f *ast.File, pkg, sel string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		s, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := s.X.(*ast.Ident); ok && id.Name == pkg && s.Sel.Name == sel {
			found = true
		}
		return true
	})
	return found
}

func TestThemePackageBoundary(t *testing.T) {
	root := repoRoot(t)
	themePath := themeImport(t, root)
	appFile := parseGoFile(t, filepath.Join(root, "pkg", "runtime", "app", "app.go"))

	t.Run("app 的默认主题必须来自 theme 包", func(t *testing.T) {
		if !importsPath(appFile, themePath) {
			t.Errorf("pkg/runtime/app 未导入 %s：主题构建入口被绕过，FONT-001 将无处接线", themePath)
		}
		if callsSelector(appFile, "material", "NewTheme") {
			t.Error("pkg/runtime/app 仍在调用 material.NewTheme()——" +
				"主题构建必须只发生在 pkg/runtime/theme（ADR-0012 决策 1 路线 B）")
		}
	})

	t.Run("自检：检测器能识别真实调用", func(t *testing.T) {
		// 反向验证：一段确实调用 material.NewTheme() 的源码必须被判为存在。
		// 否则上一条断言恒真——等于没有门禁。
		src := "package x\n\nimport \"gioui.org/widget/material\"\n\nfunc f() { _ = material.NewTheme() }\n"
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !callsSelector(f, "material", "NewTheme") {
			t.Error("检测器漏掉了真实调用——上一条断言是空的")
		}
	})

	t.Run("theme 不得反向依赖 app 或窗口", func(t *testing.T) {
		files, err := filepath.Glob(filepath.Join(root, "pkg", "runtime", "theme", "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("pkg/runtime/theme 下没有 Go 文件（包不存在，FONT-001 验收必然空转）: %v", err)
		}
		appPkg := strings.TrimSuffix(themePath, "/pkg/runtime/theme") + "/pkg/runtime/app"
		for _, f := range files {
			af := parseGoFile(t, f)
			if importsPath(af, appPkg) {
				t.Errorf("%s 反向依赖 pkg/runtime/app：theme 只构建主题，"+
					"不参与启动与窗口生命周期", filepath.Base(f))
			}
			if importsPath(af, "gioui.org/app") {
				t.Errorf("%s 引用 gioui.org/app：theme 不得触碰窗口/事件循环", filepath.Base(f))
			}
		}
	})
}

// ============================================================================
// 门禁：验收命令不得空转（THEME-SEAM-001-6）
//
// 两类空转，都会显示 ok 却什么也没验证：
//   1. 包路径不存在——go test 对该模式匹配 0 个包，返回 0；
//   2. `-run` 匹配不到任何测试——Go 打印 "no tests to run"，仍返回 0。
//
// `TASK-FONT-001-A..D` 在 `pkg/runtime/theme` 建立前长期属于第 1 类。
// 本门禁把这两类都变成红灯；未实现的项必须以**显式 skip 的占位用例**登记
// （见 pkg/runtime/theme/font_pending_test.go）。
// ============================================================================

// accCmd 是一条解析后的验收命令。
type accCmd struct {
	raw      string
	runNames []string
	pkgs     []string
}

// accCmdRe 抓形如 `go test -run TestX ./pkg/...` 的行内片段（到换行或反引号为止）。
var accCmdRe = regexp.MustCompile("go test[^`\n]*")

// testFuncRe 抓测试函数名。
var testFuncRe = regexp.MustCompile(`func\s+(Test\w+)\s*\(`)

// parseAccCommands 解析文档里的 go test 命令。
func parseAccCommands(src string) []accCmd {
	var out []accCmd
	for _, raw := range accCmdRe.FindAllString(src, -1) {
		fields := strings.Fields(raw)
		c := accCmd{raw: raw}
		for i := 0; i < len(fields); i++ {
			f := fields[i]
			switch {
			case f == "-run" && i+1 < len(fields):
				i++
				// Markdown 表格里常见 `'A\|B'`（引号 + 转义竖线），
				// 去引号 → 去转义 → 按 | 切分。
				val := strings.Trim(fields[i], `'"`)
				for _, n := range strings.Split(strings.ReplaceAll(val, `\`, ""), "|") {
					if n != "" {
						c.runNames = append(c.runNames, n)
					}
				}
			case strings.HasPrefix(f, "-run="):
				c.runNames = append(c.runNames, strings.Trim(strings.TrimPrefix(f, "-run="), `'"`))
			case strings.HasPrefix(f, "./"):
				// "./pkg/x/..." 与 "./pkg/x/" 都归一到 "./pkg/x"。
				c.pkgs = append(c.pkgs, strings.TrimSuffix(strings.TrimRight(f, "/"), "/..."))
			}
		}
		out = append(out, c)
	}
	return out
}

// testFuncNames 收集目录下所有 _test.go 的测试函数名（递归）。
func testFuncNames(root, dir string) []string {
	var names []string
	_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range testFuncRe.FindAllStringSubmatch(string(data), -1) {
			names = append(names, m[1])
		}
		return nil
	})
	return names
}

// testFuncExists 判定 -run 的名字是否命中真实存在的测试（前缀匹配，
// 与 go test -run 的非锚定语义一致）。
func testFuncExists(root, dir, name string) bool {
	for _, n := range testFuncNames(root, dir) {
		if strings.HasPrefix(n, name) {
			return true
		}
	}
	return false
}

func TestFont001AcceptanceCommandsAreNotVacuous(t *testing.T) {
	root := repoRoot(t)
	card := readDoc(t, "docs", "04-WBS", "任务卡", "TASK-FONT-001.md")

	for _, id := range []string{"TASK-FONT-001-A", "TASK-FONT-001-B", "TASK-FONT-001-C", "TASK-FONT-001-D"} {
		if !strings.Contains(card, id) {
			t.Errorf("卡片缺少子任务 %s——少一行等于少一项验收", id)
		}
	}

	cmds := parseAccCommands(card)
	if len(cmds) < 4 {
		t.Fatalf("TASK-FONT-001 只解析出 %d 条 go test 验收命令（应至少 4 条，A..D）", len(cmds))
	}

	for _, c := range cmds {
		for _, pkg := range c.pkgs {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(pkg))); err != nil {
				t.Errorf("验收命令 `%s` 指向不存在的包 %s——go test 会匹配 0 个包并返回 0（空转报绿）",
					c.raw, pkg)
				continue
			}
			for _, name := range c.runNames {
				if !testFuncExists(root, pkg, name) {
					t.Errorf("验收命令 `%s` 的 -run %s 在 %s 下匹配不到任何测试："+
						"Go 会打印 no tests to run 并返回 0（空转报绿）。"+
						"未实现的项必须放显式 skip 的占位用例", c.raw, name, pkg)
				}
			}
		}
	}

	t.Run("自检：空转命令必须被判为失败", func(t *testing.T) {
		// 反向验证：一个指向真实包、但 -run 名字不存在的命令，
		// 必须被上面同一套判定识别为不存在。否则这套检查是空的。
		fake := parseAccCommands("go test -run TestNoSuchFontTest ./pkg/runtime/theme/...")
		if len(fake) != 1 {
			t.Fatalf("解析失败: %+v", fake)
		}
		if len(fake[0].runNames) != 1 || fake[0].runNames[0] != "TestNoSuchFontTest" {
			t.Fatalf("-run 名字解析错误: %+v", fake[0])
		}
		if testFuncExists(root, fake[0].pkgs[0], "TestNoSuchFontTest") {
			t.Error("检测器把不存在的测试名判为存在——门禁是空的")
		}
		if !testFuncExists(root, fake[0].pkgs[0], "TestFontProbe") {
			t.Error("检测器未能识别已存在的占位用例 TestFontProbe")
		}

		// 引号 + 转义竖线的组合形态（卡片 §8 就是这么写的）也必须能拆开。
		quoted := parseAccCommands("go test -run 'TestFontProbe\\|TestEmbeddedFallback' ./pkg/runtime/theme/...")
		if len(quoted) != 1 || len(quoted[0].runNames) != 2 ||
			quoted[0].runNames[0] != "TestFontProbe" || quoted[0].runNames[1] != "TestEmbeddedFallback" {
			t.Fatalf("带引号/转义的 -run 解析错误: %+v", quoted)
		}
		if quoted[0].pkgs[0] != "./pkg/runtime/theme" {
			t.Fatalf("包路径归一化错误: %q", quoted[0].pkgs[0])
		}
	})
}
