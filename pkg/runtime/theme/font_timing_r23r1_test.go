package theme

// R23-R1-B repair-r1：TempElapsed 测量来源的**双向确定性**守卫。
//
// 背景（F-B1/F-B3）：上一版写路径守卫只有 20ms 夹具单侧下界，把测量换成固定 1 小时
// 仍 PASS——那是"上界缺失 + 仅下界"的假守卫。本文件改用**包内私有确定性时钟接缝**
// （Lazy.now，生产恒 nil→time.Now）锁死测量来源：
//   - 非零守卫：注入固定步长 step，TempElapsed 必须**恰等于** step（既拒绝 0/未赋值，
//     也拒绝错误覆盖如固定 1h 或任何不是本轮的常量）。
//   - 零守卫：注入 step=0，TempElapsed 必须**恰为 0**——确定性验证"零是本次测量结果"，
//     而非"允许任意非负"；配合非零守卫，未赋值(0) 会被非零守卫抓住。
// 不用 time.Sleep / 强制最小时长 / 忙等 / 放大字体。生产计时语义不变（now 恒 nil）。

import (
	"testing"
	"time"
)

// fakeClock 每次调用前进固定 step，用于确定性驱动 l.clock()。
// 仅测试使用；通过同包直接赋 lz.now 注入（不改公开 API）。
type fakeClock struct {
	base time.Time
	step time.Duration
	n    int
}

func (c *fakeClock) Now() time.Time {
	t := c.base.Add(time.Duration(c.n) * c.step)
	c.n++
	return t
}

// newLazyWithClock 建系统命中 Lazy 并注入确定性时钟。
func newLazyWithClock(t *testing.T, step time.Duration) (*Lazy, *countingLoader) {
	lz, cl := newLazySystemHit(t)
	lz.now = (&fakeClock{base: time.Unix(1700000000, 0), step: step}).Now
	return lz, cl
}

// TestFontLazyLoadTempElapsedWrittenFromMeasurement 非零守卫：
// 注入 step=7ms，断言 TempElapsed **恰等于** 7ms；同时保留业务判据。
// 判别力：把测量替成 0（未赋值）、固定 1h、或任意非本轮常量 -> != 7ms -> FAIL。
func TestFontLazyLoadTempElapsedWrittenFromMeasurement(t *testing.T) {
	const step = 7 * time.Millisecond
	lz, cl := newLazyWithClock(t, step)
	lz.Request()
	waitState(t, lz, LoadReady, 10*time.Second)

	if cl.calls != 1 {
		t.Fatalf("嵌入加载次数 = %d，期望 1——前提不成立", cl.calls)
	}
	rep := lz.Report()
	if rep.TempSource != tempSourceEmbedded {
		t.Fatalf("TempSource = %q，期望 %q", rep.TempSource, tempSourceEmbedded)
	}
	// 双向精确约束：TempElapsed 必须来自本次测量，恰为注入步长。
	if rep.TempElapsed != step {
		t.Fatalf("TempElapsed = %v，期望恰为注入的测量步长 %v。"+
			"不等于说明测量来源被固定/未赋值/错误覆盖（如固定 1h、置 0、非本轮常量）",
			rep.TempElapsed, step)
	}
}

// TestFontLazyLoadTempElapsedZeroIsLegal 零值确定性守卫：
// 注入 step=0，断言 TempElapsed **恰为 0**——确定性证明"零是本次测量结果"，
// 不是"允许快路径任意非负"。业务判据仍需成立。
// 判别力：若测量被错误覆盖为 1h/任意非零 -> != 0 -> FAIL。
func TestFontLazyLoadTempElapsedZeroIsLegal(t *testing.T) {
	lz, cl := newLazyWithClock(t, 0)
	lz.Request()
	waitState(t, lz, LoadReady, 5*time.Second)

	if cl.calls != 1 {
		t.Fatalf("嵌入加载次数 = %d，期望 1", cl.calls)
	}
	rep := lz.Report()
	if rep.TempSource != tempSourceEmbedded {
		t.Fatalf("TempSource = %q，期望 %q", rep.TempSource, tempSourceEmbedded)
	}
	if rep.TempElapsed != 0 {
		t.Fatalf("TempElapsed = %v，期望恰为 0（注入 0 步长时钟下，零必须是本次测量结果；"+
			"非 0 说明错误覆盖）", rep.TempElapsed)
	}
}
