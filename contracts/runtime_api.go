package contracts

import (
	"context"
	"errors"
	"image/color"
)

// ============================================================================
// 运行时契约（v1）
//
// 本文件是**框架层**对运行时的唯一接口集合。新增或修改接口必须经
// `make contract-lint`（go vet + go test ./contracts/...），
// 并同步 `docs/03-架构/` 下的对应冻结文档（见 AGENT.md §6.3）。
//
// 「契约先于实现」是有代价的：接口一旦落地就可能被误认为已接线。
// 因此每个接口都必须写明 **当前接线状态**与**接线条件**，
// 不允许留一个没有调用方也没有说明的抽象。
// ============================================================================

// LifecycleMount 组件挂载生命周期接口
type LifecycleMount interface {
	OnMount(ctx context.Context)
}

// LifecycleUnmount 组件卸载生命周期接口
type LifecycleUnmount interface {
	OnUnmount()
}

// ThemeProvider 强类型主题接口。
//
// **当前状态：未接线（0 个调用方）**，保留是有意为之，不是遗漏：
//
//   - 语言层已经能表达主题色：`$Theme.Field` 由 Parser/Expander 解析为
//     `NRGBAColor{IsThemeRef: true, ThemeField: field}`（见 ir_schema.go）；
//     也就是说**上游已经产出，只差运行时与 Codegen 的两端接线**。
//   - 接线条件 = `TASK-STYLE-001-1`：Codegen 按主题字段渲染 `Background`。
//     在那之前保留本接口，才能让「主题色引用」有契约层的落点；删掉它，
//     IR 里的 `IsThemeRef` 就成了一个没有出口的字段。
//
// 替换规则（避免并存两套颜色抽象）：若 STYLE-001-1 决定改用强类型/编译期字段
// 而非 string key，应**替换**本接口并同步 ir_schema.go 的 `ThemeField` 语义，
// 不得再新增第二种颜色获取方式。
type ThemeProvider interface {
	GetColor(field string) color.NRGBA
}

// ErrFontNotFound 表示逻辑字体族在当前环境下无法解析到任何可用字体集合
// （系统无该族、嵌入兜底也不可用）。
//
// 实现必须返回本错误或其包装（`errors.Is` 可判定），**不得**回退到一个
// 「看起来能用」的默认字体：那会让缺字形的文本静默渲染为空白或豆腐块，
// 而调用方完全无感知——正是本契约要禁止的失败模式
// （`字体与文本渲染策略.md` §5、ADR-0012 决策 1）。
var ErrFontNotFound = errors.New("goxml: font family not found")

// FontProvider 字体解析接口（ADR-0012 决策 1：路线 B）。
//
// # 职责边界
//
//   - 字体选择属于**运行时**主题构建过程（pkg/runtime/theme），**不属于生成代码**：
//     生成代码只接受一个已经配好字体的 `*material.Theme`
//     （`代码生成输出契约.md` §4.3.1 / §4.3.2），不得出现字体路径、字号常量、
//     `th.Fonts.Family` 之类的引用，也不得自行构建 `text.Shaper`。
//   - FontProvider 是这条边界的**唯一**跨层通道：它把环境相关的字体决策
//     （探测、fallback 链、懒加载）关在运行时一侧。
//
// # Resolve 语义
//
//   - 入参 family 是**逻辑字体族名**（如 "Body" / "CJK"），不是文件路径；
//   - 返回值是 pkg/runtime/theme 在探测阶段注册的**字体集合标识**；
//   - 无法解析时返回 ErrFontNotFound，**不返回空串 + nil**；
//   - 同一 family 的解析结果必须可缓存：不允许每次调用重新扫描磁盘。
//
// # 调用时机
//
// 主题构建期，或首次需要 shaping 时（懒加载）。**不得**每帧调用
// （性能预算 INV-PERF-2：稳定帧文本节点 allocs/op == 0），
// 且实现必须自带超时，不得阻塞首帧。
//
// **当前状态：已接线**。`pkg/runtime/theme.Provider` 实现本接口
// （`TASK-FONT-001-A` 系统探测；`TASK-FONT-001-B` 追加嵌入兜底与
// 两条链路诊断）。尚未实现的只有 C（按字形 fallback 链）与 D（懒加载）。
type FontProvider interface {
	Resolve(family string) (string, error)
}
