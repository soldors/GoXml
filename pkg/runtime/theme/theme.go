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
// 已实现：
//
//   - **A**：CJK 系统字体探测（候选表、超时、诊断、缓存）并在命中时把集合
//     装配进 `text.Shaper`；
//   - **B**：系统探测失败后回退到 `//go:embed` 的子集字体（GB2312 6,763 汉字，
//     不是完整 CJK）；两条链路的诊断都进 `ProbeReport`，并对 app 暴露
//     `Diagnostics()`。
//
// 尚未实现：按字形覆盖的 fallback 链（C）、懒加载与探测延迟优化（D）。
//
// # 宽容入口 vs 严格入口（不要混淆）
//
//   - `NewTheme() *material.Theme` 是**宽容包装**：签名固定，无法返回错误，
//     因此两条链路都失败时它仍返回 Gio 默认主题。诊断不会消失——用
//     `NewThemeWithReport()` 或 `Diagnostics()` 取。
//   - `pkg/runtime/app.Run` 走**严格路径**：默认主题构建失败时直接返回
//     包装了 `contracts.ErrFontNotFound` 的错误，不再静默降级
//     （规范 §1.2「不得静默降级到空白渲染」）。显式注入主题的
//     `RunWith` / `NewApp` 不受影响。
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
// 字体解析顺序（B 卡 §4）：系统探测命中 → 用系统字体；系统全部不可用 →
// 用嵌入子集字体。任一命中都会把集合装配进 `text.Shaper`（保留系统字体，
// 不传 `text.NoSystemFonts()`）。
//
// 两条链路都失败时**仍返回 Gio 默认主题**——本函数签名固定，没有返回错误的
// 位置。诊断不会消失：`NewThemeWithReport()` 返回的 `*ProbeReport` 与
// `Diagnostics()` 都能拿到完整原因。需要"失败即报错"的严格语义时，走
// `pkg/runtime/app.Run`（它返回错误）或直接调用 `NewThemeWithReport`。
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

// Diagnostics 返回最近一次**默认主题**字体解析的诊断报告（副本）。
//
// 这是 B6 要求的观测入口（A 卡约束 10 的另一半）：系统探测与嵌入兜底两条
// 链路的结果与原因都在里面。返回副本，调用方改动不会污染 Provider 内部状态。
//
// 尚未发生过解析时返回 nil——nil 与"解析成功"必须能区分开，因此不要用
// `Diagnostics() != nil` 来判断成功，要看 `rep.Err`。
func Diagnostics() *ProbeReport {
	rep := defaultProvider.LastReport()
	if rep == nil {
		return nil
	}
	cp := *rep
	cp.Candidates = append([]CandidateResult(nil), rep.Candidates...)
	if rep.Embedded != nil {
		e := *rep.Embedded
		cp.Embedded = &e
	}
	return &cp
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
