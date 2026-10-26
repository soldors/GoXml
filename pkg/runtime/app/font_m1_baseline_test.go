package app

import (
	"testing"
	"time"

	"gioui.org/widget/material"
)

// TestFontM1BaselineNoFontLoad 采集 M1 的**无字体加载基线**（D-ACCEPT-01）。
//
// # 为什么必须有这一条
//
// `TestFontLazyLoadFirstFrameReport` 测的是**懒加载路径**的 M1，区间里包含
// "等待后台字体就绪"这段。要判断这段等待带来多少增量，必须有**同一探针、
// 同一计时方式**的基线；此前没有任何用例覆盖非懒加载 App 的 M1
// （现有 4 处 `FirstFrame()` 调用全在懒加载用例里）。
//
// # 探针与计时方式（与懒加载路径一致）
//
// 起点＝`NewApp` 构造入口（`a.createdAt`）；止点＝`frame()` 里首次记录的
// `firstFrameAt`；读数走 `App.FirstFrame()`，帧用 `driveFrames` 驱动。
// 差别只在"这一路不加载字体"——用 `material.NewTheme()`，不触发系统字体探测。
//
// # 为什么必须**批量**测（本机实测结论）
//
// 单次基线实测为 **0s**：无字体加载时"构造→首帧"远低于 `time.Now()` 在本机的
// 有效粒度（约 0.53 ms，见 dev-log 2026-10-23 §11.2）。单次读数因此是**空判据**，
// 不能当成"耗时为 0"，也不能据此断言"比懒加载路径快"。
// 按项目既有做法改为**累计 N 次总量**再取均值：总量远超一个粒度才可读。
//
// # 口径限制（必须随数字一起引用）
//
// 批量均值 = 「N 次循环总墙钟 ÷ N」，两头都不精确：
//
//   - **偏高**：含每次迭代的循环开销（读数、分配、`FirstFrame()` 调用）；
//   - **偏低**：循环复用同一个 `material.Theme`，shaper 在第 1 次后已**预热**，
//     因此**不含冷 shaping 成本**——而真实首帧是冷的。
//
// 两个偏差方向相反，因此该均值**只能作量级参考**，
// **不得**当作冷启动基线、性能基准或阈值依据；与懒加载路径的 131 ms 也**不得**做精确差值。
//
// 本用例**只记录数据、不设阈值**（D-ACCEPT-01：先测基线，阈值另行裁定）。
func TestFontM1BaselineNoFontLoad(t *testing.T) {
	const iterations = 2000

	// material.NewTheme() 不加载系统字体：这是"无字体加载"的关键。
	th := material.NewTheme()

	// ① 机制自检：未跑帧时必须报"未测量"，否则零值会被读成"首帧很快"。
	first, err := NewApp(&countingView{text: "x"}, th, Config{Title: "m1-baseline"})
	if err != nil {
		t.Fatal(err)
	}
	before := first.FirstFrame()
	if before.Measured() {
		t.Fatal("还没有任何帧却报告已测量：零值会被当成'首帧很快'，属虚假通过")
	}
	if !before.FirstFrameAt.IsZero() {
		t.Errorf("未跑帧时 FirstFrameAt = %v，期望零值", before.FirstFrameAt)
	}
	if before.CreatedAt.IsZero() {
		t.Error("CreatedAt 必须非零（构造入口时刻，M1 起点）")
	}
	if before.Elapsed != 0 {
		t.Errorf("未测量时 Elapsed = %v，期望 0（0 表示没测到，不是耗时为 0）",
			before.Elapsed)
	}

	// ② 单次：证明止点会被记录，且区间方向成立。
	driveFrames(first, 2)
	single := first.FirstFrame()
	if !single.Measured() {
		t.Fatal("跑过帧之后仍报未测量：基线 M1 没有被记录")
	}
	if single.FirstFrameAt.Before(single.CreatedAt) {
		t.Errorf("止点 %v 早于起点 %v：M1 区间不成立",
			single.FirstFrameAt, single.CreatedAt)
	}
	if single.Elapsed <= 0 {
		t.Logf("单次基线 M1 = %v：低于本机计时粒度（约 0.53 ms），"+
			"单次读数不可分辨 —— 这不是'耗时为 0'，也**不能**据此断言比懒加载快", single.Elapsed)
	} else {
		t.Logf("单次基线 M1 = %v", single.Elapsed)
	}

	// 只记第一次：后续帧不得覆盖（与懒加载路径同一契约）。
	pinned := single.FirstFrameAt
	driveFrames(first, 3)
	if got := first.FirstFrame(); got.FirstFrameAt != pinned {
		t.Errorf("首帧时刻被后续帧覆盖: %v → %v", pinned, got.FirstFrameAt)
	}

	// ③ 批量：累计 N 次总量，取到远超一个计时粒度的可读值。
	start := time.Now()
	for i := 0; i < iterations; i++ {
		a, err := NewApp(&countingView{text: "x"}, th, Config{Title: "m1-baseline"})
		if err != nil {
			t.Fatalf("第 %d 次构造失败: %v", i, err)
		}
		driveFrames(a, 1)
		if !a.FirstFrame().Measured() {
			t.Fatalf("第 %d 次未记录首帧：批量结果不可信", i)
		}
	}
	total := time.Since(start)
	if total <= 0 {
		t.Fatalf("批量总耗时 = %v：计时器读不出该量级，判据会在 0 上恒真——"+
			"必须记为未验证，不得当作通过", total)
	}

	t.Logf("M1 基线（无字体加载，批量）：%d 次 构造→首帧 总耗时 %v，均值 %v/次"+
		"（**仅量级参考**：偏高＝含循环开销，偏低＝shaper 已预热不含冷 shaping；"+
		"不得当冷启动基线或阈值依据）—— 只记录，阈值归 D-ACCEPT",
		iterations, total, total/iterations)
}
