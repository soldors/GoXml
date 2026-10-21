package theme

import (
	giofont "gioui.org/font"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// ============================================================================
// 主题构造（TASK-FONT-001-D-IMPL-1 §4 的落点）
//
// # 为什么单独一个文件
//
// 异步加载路径 `lazy.go` **不得**出现任何 `.Shaper` 选择子——那是
// `TestAsyncPathNoRenderShaper` 这条结构门禁的判据。而构造一个主题**必须**
// 给它的 `Shaper` 赋值，两者直接冲突。
//
// 解法是把两种职责分到两个文件：
//   - 本文件：**构造新主题**（可以有自己的 `Shaper`，与渲染无关）；
//   - `lazy.go`：**调度**（探测、解析、状态机、报告），一行都不碰 `Shaper`。
//
// 这样一来，"后台线程绝不触碰渲染线程正在用的那个 shaper"不是靠自觉，
// 而是靠"它根本拿不到 shaper 这个词"来保证。
// ============================================================================

// 临时主题的来源标记（进 LoadReport.TempSource）。
const (
	// tempSourceEmbedded 方案 c：NoSystemFonts + 嵌入子集（D 卡 §4.2 冻结）。
	tempSourceEmbedded = "embedded-subset"
	// tempSourceCustom 调用方显式给了 LazyOptions.Temp。
	tempSourceCustom = "custom"
	// tempSourceGioDefault 嵌入字体也不可用时的降级：Gio 默认主题（含系统字体）。
	//
	// 这条分支只在构建时关闭了字体嵌入（nofontembed）时才会走到。此时首帧
	// **没有 CJK 保证**，属于如实登记的降级，不是静默降级：TempSource 会
	// 显式标成这个值，诊断里一眼能看出来。
	tempSourceGioDefault = "gio-default"
)

// newTempTheme 构造首帧可用的临时主题（D 卡 §4.2 方案 c）。
//
// # 必然解析一次嵌入字体
//
// 临时主题必须**不依赖系统字体**也有汉字：首帧要在真主题还没到的时候就把
// 中文画出来。因此这里走 `NoSystemFonts()` + 嵌入子集，代价是必然解析一次
// 嵌入字体（实测约 1.4–1.7 ms，见 `LoadReport.TempElapsed`）。
//
// # 不污染最终链的诊断
//
// `Provider.loadEmbedded` 返回的是**新的** `EmbeddedResult`，本函数直接丢弃它，
// 不写进任何 `ProbeReport`。这是 B4 断言的保护：系统命中时
// `ProbeReport.Embedded` 必须仍是 nil / Attempted=false，否则
// "系统命中 ⇒ 不解析嵌入"这条既有承诺会被临时主题的解析记录打穿。
//
// # 为什么返回 source 而不是 error
//
// 嵌入不可用时**仍然返回可用主题**（Gio 默认 + 系统字体），只是把来源标成
// `gio-default`。降级事实完全由 `source` 表达，不需要再挂一个错误——
// 把"临时主题降级"混进 `LoadReport.Err`（终态错误）会让 IMPL-3 的
// `errors.Is(Err, ErrFontNotFound)` 出现假阳性。
func newTempTheme(p *Provider) (*material.Theme, string) {
	th := material.NewTheme()
	if p == nil {
		return th, tempSourceGioDefault
	}
	_, faces, res := p.loadEmbedded()
	if res.Err != nil || len(faces) == 0 {
		return th, tempSourceGioDefault
	}
	// 只给嵌入子集、不要系统字体：首帧的度量与最终字体可能不同，
	// 但**保证有汉字**。D 卡 §4.2 已冻结这个取舍（不能用纯拉丁字体换稳定）。
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces))
	return th, tempSourceEmbedded
}

// newThemeWithFaces 用给定的字体集合构造主题（真主题）。
//
// 与 `NewThemeWithReport` 的装配规则一致：**保留**系统字体（不传
// `NoSystemFonts`），CJK 集合作为补充/主选。两条路径的装配口径必须相同，
// 否则"切换到真主题"会顺带改变拉丁字体的度量，D6-6 的布局差就不可归因了。
func newThemeWithFaces(faces []giofont.FontFace) *material.Theme {
	th := material.NewTheme()
	if len(faces) == 0 {
		return th
	}
	th.Shaper = text.NewShaper(text.WithCollection(faces))
	return th
}
