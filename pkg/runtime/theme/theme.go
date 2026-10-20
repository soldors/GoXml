// Package theme 构建运行时主题（ADR-0012 决策 1：路线 B）。
//
// # 职责边界
//
//   - 本包是**唯一**的主题构建入口。字体探测、fallback 链、懒加载与
//     `text.Shaper` 配置将来全部落在这里；
//   - `pkg/runtime/app` 只负责启动与窗口生命周期，默认主题**必须**来自本包，
//     不得自行调用 `material.NewTheme()`（由 `internal/docgate` 门禁锁住）；
//   - 生成代码只接受本包产出的 `*material.Theme`，不引用任何字体字段
//     （输出契约 §4.3.1 / §4.3.2）。
//
// # 当前状态（重要）
//
// `NewTheme()` 当前做两件事：构建 Gio 默认主题，并在探测成功时把 CJK 字体集合
// 装配进 `text.Shaper`（TASK-FONT-001-A）。**尚未实现**：嵌入兜底（B）、
// 按字形的 fallback 链（C）、懒加载与加载失败强制诊断（D）。
//
// ⚠️ **A 阶段的已知缺口**：探测失败时 `NewTheme()` 仍返回 Gio 默认主题
// （Shaper 零值 → 隐式加载系统字体），只保证「诊断可见」。
// 这是有意的暂缓：A 阶段没有兜底可回退，此时强制失败会让无 CJK 字体的环境
// 直接不可用。**退出条件 = B 落地后必须收紧**为「探测失败即诊断失败」，
// 否则长期违反规范 §1.2「不得静默降级到空白渲染」。
// 失败信息由 `NewThemeWithReport` 返回的 `*ProbeReport` 暴露。
package theme

import (
	"fmt"

	"gioui.org/text"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
)

// defaultProvider 是进程内共享的字体 Provider。
//
// 构造不做任何 IO（规范 §3.3 禁止 init 同步加载字体），探测发生在首次 Resolve。
var defaultProvider = NewProvider(ProviderOptions{})

// NewTheme 构建默认主题。
//
// 探测成功时把命中的 CJK 字体集合装配进 `text.Shaper`（系统字体仍保留，
// 不传 `text.NoSystemFonts()`）；探测失败时保持 Gio 默认行为，
// 诊断通过 NewThemeWithReport 可见（A 阶段已知缺口，见包注释）。
//
// # 稳定性承诺
//
// 签名不因字体能力的接线而改变。若将来需要参数（字体策略、嵌入开关等），
// 应新增 `NewThemeWith*` 并保留本函数——`pkg/runtime/app` 不得因此改为
// 自行构建主题，否则路线 B 的边界（字体决策只在运行时一侧）会被绕过。
//
// # 实例语义
//
// 每次调用返回**独立**实例。主题是可变结构（调用方可能改 `Palette`、
// 换 `Shaper`），返回共享单例会让两个窗口互相污染。
func NewTheme() *material.Theme {
	th, _ := NewThemeWithReport()
	return th
}

// NewThemeWithReport 与 NewTheme 相同，另外返回字体探测的诊断报告。
//
// 这是「A 阶段降级」的观测入口：调用方与测试通过报告判断探测是否成功、
// 失败原因是什么（规范 §2.3 要求探测结果可观测）。
func NewThemeWithReport() (*material.Theme, *ProbeReport) {
	th := material.NewTheme()

	id, rep, err := defaultProvider.ResolveWithReport(FamilyCJK)
	if rep == nil {
		rep = &ProbeReport{Family: FamilyCJK, Err: err}
	}
	if err != nil {
		return th, rep
	}
	faces, ok := defaultProvider.Faces(id)
	if !ok || len(faces) == 0 {
		rep.Err = fmt.Errorf("%w: 标识 %q 无法反查到字体集合", contracts.ErrFontNotFound, id)
		return th, rep
	}
	// 装配：保留系统字体（不传 text.NoSystemFonts），CJK 集合作为补充。
	th.Shaper = text.NewShaper(text.WithCollection(faces))
	return th, rep
}
