package theme

// R23-R1-B：TempElapsed 测量写入路径守卫（判别力测试，替代原并发用例里
// 依赖时钟分辨率的 `TempElapsed > 0` 断言）。
//
// 背景（B1 定性，见 dev-log）：TestFontLazyLoadTempBuiltOnceConcurrent 原断言
// `TempElapsed > 0` 在 32 goroutine 争用下会因墙钟测量低于本机时钟分辨率低
// （~0.5ms）读成 0，基线 100 次实测 96 PASS / 4–9 FAIL、失败值恒为 0s。
// 这是**测试前提（计时精度）不成立**，不是生产并发缺陷；业务判据是\"临时主题只
// 构造一次\"(cl.calls==1)、Attempts==1、TempSource 正确——这些已在并发用例保留。
//
// 本文件分离出两类语义：
//   ① 合法零值：构建快于分辨率时 TempElapsed 就是 0，不据 0 判失败（并发用例已放开）。
//   ② 真实写入路径：用**可控慢加载器**让临时主题构建实测 > 分辨率，据此证明
//      `l.tempElapsed = time.Since(start)` 与 `l.rep.TempElapsed = l.tempElapsed`
//      两处写入真实发生。删除任一写入 -> TempElapsed 恒 0 -> 本守卫 FAIL。
//
// 这不是放宽判据：原并发用例的\"构造一次\"业务断言原样保留；这里新增的是对
// \"测量被真正写入\"的、比删断言更有辨识力的守卫（用可控 fixture 制造可测时长）。

import (
	"testing"
	"time"
)

// slowEmbeddedLoader 在 Load 时阻塞到测试放行，并给一个明确的真实等待下限，
// 使临时主题构建的墙钟耗时稳定大于本机时钟分辨率（可测），从而让 TempElapsed
// 的写入可被判别。等待有墙钟上限（由测试固定的小常量），避免挂死。
type slowEmbeddedLoader struct {
	data  []byte
	delay time.Duration
	calls int // 由 embMu 串行化保护（同一 Provider 内）
}

func (l *slowEmbeddedLoader) Load() ([]byte, string, error) {
	l.calls++
	// 受控真实等待：确保 newTempTheme 的测量区间 > 时钟分辨率。
	// 这是**测试夹具**用于制造可测时长，不是生产路径的强制延时，也不用于掩盖断言。
	time.Sleep(l.delay)
	return l.data, "SlowEmbedded", nil
}

// TestFontLazyLoadTempElapsedWrittenFromMeasurement 真实写入路径守卫：
// 慢加载器下 TempElapsed 必须实测 > 时钟分辨率（此处取保守下限 delay/2）。
func TestFontLazyLoadTempElapsedWrittenFromMeasurement(t *testing.T) {
	font := hanziFontBytes(t)
	dir := t.TempDir()
	good := writeFont(t, dir, "good.otf", font)

	const delay = 20 * time.Millisecond // 远大于 ~0.5ms 分辨率
	sl := &slowEmbeddedLoader{data: font, delay: delay}
	p := NewProvider(ProviderOptions{
		Candidates: []string{good},
		Embedded:   sl,
	})
	lz, err := NewLazy(LazyOptions{Provider: p})
	if err != nil {
		t.Fatalf("NewLazy 失败: %v", err)
	}
	lz.Request()
	waitState(t, lz, LoadReady, 10*time.Second)
	rep := lz.Report()
	// 前提：临时主题确实构造并解析了嵌入（否则测的是空路径）。
	if sl.calls != 1 {
		t.Fatalf("嵌入加载次数 = %d，期望 1——前提不成立", sl.calls)
	}
	if rep.TempSource != tempSourceEmbedded {
		t.Fatalf("TempSource = %q，期望 %q", rep.TempSource, tempSourceEmbedded)
	}
	// 写入路径判别：慢加载器保证测量区间 ≥ delay，TempElapsed 必须反映它。
	// 保守下限 delay/2，容忍调度抖动；远高于时钟分辨率，故不是 flake。
	if rep.TempElapsed < delay/2 {
		t.Fatalf("TempElapsed = %v，期望 ≥ %v（慢加载器 %v 下必须实测到写入；"+
			"若为 0 说明 l.tempElapsed / l.rep.TempElapsed 写入路径缺失或未赋值）",
			rep.TempElapsed, delay/2, delay)
	}
}

// TestFontLazyLoadTempElapsedZeroIsLegal 合法零值语义：
// 快加载（无延时）下 TempElapsed 允许为 0（低于分辨率），**不得**据此判失败；
// 但\"构造一次\"与来源仍必须成立。与上一条一起，把\"合法零\"与\"真实写入\"分开。
func TestFontLazyLoadTempElapsedZeroIsLegal(t *testing.T) {
	lz, cl := newLazySystemHit(t)
	lz.Request()
	waitState(t, lz, LoadReady, 5*time.Second)
	rep := lz.Report()
	if cl.calls != 1 {
		t.Fatalf("嵌入加载次数 = %d，期望 1", cl.calls)
	}
	if rep.TempSource != tempSourceEmbedded {
		t.Fatalf("TempSource = %q，期望 %q", rep.TempSource, tempSourceEmbedded)
	}
	// 不断言 >0：0 是合法测量值（快于分辨率）。只断言非负（时间戳语义不出现负值）。
	if rep.TempElapsed < 0 {
		t.Fatalf("TempElapsed = %v，期望非负", rep.TempElapsed)
	}
	t.Logf("快加载 TempElapsed = %v（0 合法，不作失败判据）", rep.TempElapsed)
}
