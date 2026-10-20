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
// `NewTheme()` **只构建 Gio 默认主题**，等价于 `material.NewTheme()`。
// 字体相关能力属 `TASK-FONT-001-A..D`，**尚未实现**——不实现也有意先建这个
// 入口，理由是：`FONT-001-A..D` 的验收命令原本指向一个不存在的包，
// 属于「空转报绿」；入口存在后命令才有真实落点。
//
// 未实现的四项在 `font_pending_test.go` 中以**显式 skip**登记，
// 运行时表现为 `--- SKIP`，不会被误读为通过。
package theme

import (
	"gioui.org/widget/material"
)

// NewTheme 构建默认主题。
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
	return material.NewTheme()
}
