package docgate

import (
	"os"
	"path/filepath"
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
