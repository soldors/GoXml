package app

import (
	"fmt"
	"time"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ============================================================================
// TASK-FONT-001-D-IMPL-3：诊断入口、严格失败语义、M1 首帧时刻的可测化
//
// 契约见 docs/04-WBS/任务卡/TASK-FONT-001-D-IMPL-3.md（第 3 版）。
//
// # 本文件只做三件事
//
//  1. 把懒加载句柄交出去（诊断跟着句柄走，**不做全局单例**——
//     D 卡 §4.1 要求每个顶层窗口有独立的 Theme/Shaper）；
//  2. 把"加载失败"在严格模式下变成 `Run()` 的返回值；
//  3. 让 M1（首帧预算）**可测**——只记录与暴露，不判合格。
//
// # 明确不做
//
//   - 不写入 `LoadReport.Err`：那是 IMPL-1 的职责（`Lazy.load` 的终态写入），
//     本卡只**验证**它的可观测性与 `errors.Is(Err, ErrFontNotFound)` 语义；
//   - 不定任何性能阈值（M1/M4 的阈值归 `TASK-FONT-001-D-ACCEPT`，
//     D 卡 §9-1 禁止先拍数字）；
//   - 不实现通用性能监控框架——M1 只有两个时刻与一个差值。
// ============================================================================

// Lazy 返回构造时传入的 *theme.Lazy，供调用方读诊断。
//
// 返回 nil 表示本 App 走的是**同步路径**（`NewApp`）——那条路径没有
// 懒加载状态可读，字体语义由 `Run` 的默认主题构建保证。
//
// 交出的是句柄本身，**不是**内部可变状态的拷贝：调用方应当只用它读
// （`Report()` / `State()` / `Theme()`），不得用它驱动加载或替换主题——
// 那是渲染线程的事（IMPL-2 的 `swapPending`）。
func (a *App) Lazy() *theme.Lazy {
	return a.lz
}

// FirstFrameReport 是 M1（首帧预算）的最小读数。
//
// # 起止事件（卡 §4.1 只写了"进程启动→首帧处理开始"，没有可执行的起止点，
// 这里依据实际生命周期给出最小定义）
//
//	起点 CreatedAt    = NewApp / NewLazyApp **入口**的时刻
//	止点 FirstFrameAt = 第一个 FrameEvent 开始处理、`view.Layout` 之前的时刻
//
// 两个"必须写清楚"的偏差：
//
//  1. 起点**不是**真正的进程启动：Go 没有可移植的进程起始时间 API，库能观测
//     到的最早时刻就是运行句柄的构造入口。`CreatedAt` 与真实进程启动之间的
//     差属于宿主代码（import、flag 解析、业务初始化），**不在本库的可测
//     范围内**；不得用 `CreatedAt` 冒充"进程启动"。
//  2. 区间**包含**窗口创建与临时主题构造（`TempElapsed`）——卡 §4.1 明确这段
//     是有界同步成本，不是零；**不包含**等待字体（懒加载路径根本不等待）。
//
// # 时钟来源
//
// 与 `theme.LoadReport` 的其它时刻字段同源：`time.Now()`。
// 本机实测其粒度约 0.53 ms（见 dev-log 2026-10-23 §11.2）——M1 的量级远大于
// 该粒度，因此单次测量可读；但换机器仍须重新确认粒度。
//
// # 零值语义（"没测到" ≠ "耗时为 0"）
//
// `FirstFrameAt` 为零值 = 还没有过任何一帧。此时 `Elapsed` 也是零值，
// 那是**没测到**，不是"首帧很快"。调用方必须先判 `Measured()`，
// 未测到时报告「未验证」，不得把零值当通过。
type FirstFrameReport struct {
	// CreatedAt 是运行句柄构造的入口时刻（M1 起点）。
	CreatedAt time.Time
	// FirstFrameAt 是首帧开始处理的时刻（M1 止点）；零值 = 尚未有过帧。
	FirstFrameAt time.Time
	// Elapsed = FirstFrameAt - CreatedAt；未测到时为 0。
	Elapsed time.Duration
}

// Measured 报告首帧时刻是否已经测到。
//
// 这是"计时数据不可用时报告未验证"的判据入口：调用方与测试都必须先过这一关，
// 再看 Elapsed 的数值。
func (r FirstFrameReport) Measured() bool {
	return !r.FirstFrameAt.IsZero()
}

// FirstFrame 返回 M1 的读数（纯读取，无副作用）。
//
// 读取时机：至少跑过一帧之后才有意义。在此之前调用会得到
// `Measured() == false`——那是合法的"未验证"状态，不是错误。
func (a *App) FirstFrame() FirstFrameReport {
	rep := FirstFrameReport{CreatedAt: a.createdAt}
	if ns := a.firstFrameAt.Load(); ns != 0 {
		rep.FirstFrameAt = time.Unix(0, ns)
		rep.Elapsed = rep.FirstFrameAt.Sub(a.createdAt)
	}
	return rep
}

// fontFailure 决定 `Run()` 是否要把字体加载失败作为错误交出去。
//
// 三条边界（卡 §1）：
//
//  1. 同步路径（`lz == nil`）恒返回 nil——`StrictFontFailure` 不得影响它；
//  2. 非严格模式恒返回 nil——失败不终止窗口，原因从 `Report().Err` 读；
//  3. 严格模式下**只**在加载进入失败终态时返回错误；加载成功或仍在途都返回 nil。
//
// 这里**不** panic、**不**强制关窗：把"可诊断的失败"变成"崩溃"比同步失败更糟
// （D 卡 §6）。错误包装 `Report().Err`，而后者由 IMPL-1 保证包了
// `contracts.ErrFontNotFound`，因此 `errors.Is(err, ErrFontNotFound)` 成立。
func (a *App) fontFailure() error {
	if a.lz == nil || !a.strict {
		return nil
	}
	rep := a.lz.Report()
	if rep.Err == nil {
		return nil
	}
	return fmt.Errorf("app: 懒加载字体失败（StrictFontFailure=true）: %w", rep.Err)
}
