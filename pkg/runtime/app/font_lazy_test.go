package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ============================================================================
// TASK-FONT-001-D-IMPL-2：帧内切换与布局一致性
//
// 本文件覆盖任务卡 §4 的七个用例。三条贯穿全文件的纪律：
//
//  1. **措辞**：只写"提交"，不写"显示/呈现"。`FrameSubmittedAt` 的语义上界
//     止于 `e.Frame` 已返回，它不是屏幕呈现时刻（§1.4）。
//  2. **不靠崩溃**：跨 goroutine 共用 shaper 是 Go 运行时 fatal error
//     （recover 无效），"这次没崩"证明不了任何事。因此"只在帧内替换"被做成
//     `App.swapPending` 里可判定的条件，而不是靠运气。
//  3. **不数帧**：唤醒/替换类断言一律用状态与序号，不得写成"第 N 帧必须…"
//     （D 卡 §4.3.5）。`TestFontLazyLoadNoFrameCounting` 专门守这条。
//
// # 帧从哪来
//
// 无图形会话时真实窗口可能永不产生 FrameEvent，而纯逻辑用例**不得 Skip**。
// 好在 `app.FrameEvent` 的 `Frame` 是一个**字段**（`func(*op.Ops)`），
// 可以合成一个不依赖窗口的帧事件，直接驱动 `(*App).frame`。
// 这样可以逐帧、可数、可重复地验证 §1.2 冻结的六步顺序。
// 真实窗口链路仍由 TestRealWindow* 覆盖（那些允许 Skip）。
// ============================================================================

// lazySampleText 是布局一致性度量的样本：单行 CJK，无换行、无混排。
//
// 选 CJK 而不是拉丁，是因为 D6-6 关心的正是"临时字体与真字体的汉字度量差"。
const lazySampleText = "中文字体懒加载布局一致性度量"

// lazyProbeView 记录每帧看到的 Shaper 指针、帧内标记与排版耗时。
//
// 三个证据维度对应三条规则：
//   - shapers：替换次数（SwapOncePerGeneration）
//   - inFrames：替换是否发生在帧内（SwapOnlyInFrame，R2）
//   - durations：是否出现第二次 shaping 冷启动（NoRepeatShaping，D6-3）
type lazyProbeView struct {
	owner     *App // 可为 nil：不需要读帧内标记的用例不设
	shapers   []*text.Shaper
	inFrames  []bool
	durations []time.Duration
}

func (v *lazyProbeView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	start := time.Now()
	if v.owner != nil {
		v.inFrames = append(v.inFrames, v.owner.inFrame.Load())
	}
	v.shapers = append(v.shapers, th.Shaper)
	dims := material.Body1(th, lazySampleText).Layout(gtx)
	v.durations = append(v.durations, time.Since(start))
	return dims
}

// shaperChanges 统计序列中相邻两项之间 Shaper 指针发生变化的次数。
//
// 用指针身份而不是"值是否相等"：我们关心的是"有没有被换成另一个对象"，
// 而不是"对象内容有没有变"。
func shaperChanges(s []*text.Shaper) int {
	n := 0
	for i := 1; i < len(s); i++ {
		if s[i] != s[i-1] {
			n++
		}
	}
	return n
}

// shaperSeq 把"切换前抓到的 Shaper"接到逐帧记录的前面。
//
// 不这么做就数不出第一次变化：若第一帧就已经完成替换，逐帧记录的第一项
// 就是真字体的 shaper，变化次数会算成 0——把"换了一次"误判成"没换"。
func shaperSeq(temp *text.Shaper, frames []*text.Shaper) []*text.Shaper {
	return append([]*text.Shaper{temp}, frames...)
}

// frameRecorder 记录合成帧的提交次数与最后一次 `e.Frame` 的调用时刻。
//
// `lastAt` 是 §1.2 步骤 ⑤ 的时间戳——步骤 ⑥（`MarkFrameSubmitted`）必须
// 发生在它之后。没有它，"⑥ 在 ⑤ 之后"这条冻结顺序就只是注释里的承诺。
type frameRecorder struct {
	count  atomic.Int32
	lastAt atomic.Int64 // UnixNano
}

// submittedAt 返回最后一次 e.Frame 被调用的时刻（未提交过则为零值）。
func (r *frameRecorder) submittedAt() time.Time {
	ns := r.lastAt.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// synthFrameEvent 合成一个不依赖窗口的 FrameEvent。
//
// `Frame` 字段只做计数与记时：它代表"这一帧已交给 Gio 的呈现路径"，
// 是 `MarkFrameSubmitted` 在时间上的前置事件（§1.2 步骤 ⑤→⑥）。
func synthFrameEvent(r *frameRecorder) app.FrameEvent {
	return app.FrameEvent{
		Now:    time.Now(),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Size:   image.Pt(800, 600),
		Frame: func(*op.Ops) {
			r.lastAt.Store(time.Now().UnixNano())
			r.count.Add(1)
		},
	}
}

// driveFrames 连续驱动 n 帧，返回提交记录。
func driveFrames(a *App, n int) *frameRecorder {
	var rec frameRecorder
	var ops op.Ops
	for i := 0; i < n; i++ {
		a.frame(&ops, synthFrameEvent(&rec))
	}
	return &rec
}

// newTestLazy 构造一个**尚未 Request** 的 Lazy。
//
// 必须未 Request：`NewLazyApp` 内部要"先 SetNotify 再 Request"，而
// `SetNotify` 只接受 Request 之前的首次绑定（IMPL-1 §2）。
func newTestLazy(t *testing.T) *theme.Lazy {
	t.Helper()
	lz, err := theme.NewLazy(theme.LazyOptions{})
	if err != nil {
		t.Fatalf("构造 Lazy 失败: %v", err)
	}
	return lz
}

// waitState 轮询等待 Lazy 进入目标状态。
func waitState(t *testing.T, lz *theme.Lazy, want theme.LoadState, d time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if lz.State() == want {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return lz.State() == want
}

// requireReady 等待 Ready；等不到就用报告里的诊断说明原因，不静默跳过。
func requireReady(t *testing.T, lz *theme.Lazy) {
	t.Helper()
	if waitState(t, lz, theme.LoadReady, 15*time.Second) {
		return
	}
	rep := lz.Report()
	t.Fatalf("懒加载未进入 Ready：state=%s attempts=%d err=%v\n诊断: %+v",
		rep.State, rep.Attempts, rep.Err, rep.Probe)
}

// measureWidth 在离屏上下文里量一段文本的行宽。
//
// 用 `Max` 给一个远大于文本宽度的上界，保证不换行——换行会让"宽度"
// 变成约束宽度的函数，D6-6 的度量差就不可归因了。
func measureWidth(t *testing.T, th *material.Theme, s string) int {
	t.Helper()
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Pt(1<<20, 1000)},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	return material.Body1(th, s).Layout(gtx).Size.X
}

// ---------------------------------------------------------------------------
// 用例 1：替换只在帧内（R2）
// ---------------------------------------------------------------------------

// TestFontLazyLoadSwapOnlyInFrame `Take()` 只在帧内被调用；帧外调用即失败。
func TestFontLazyLoadSwapOnlyInFrame(t *testing.T) {
	t.Run("帧外调用不替换、也不消耗 pending", func(t *testing.T) {
		lz := newTestLazy(t)
		v := &lazyProbeView{}
		a, err := NewLazyApp(v, lz, Config{Title: "swap-outside"})
		if err != nil {
			t.Fatal(err)
		}
		v.owner = a
		requireReady(t, lz)

		if a.inFrame.Load() {
			t.Fatal("构造后不应处于帧内：inFrame 只能由 frame 置真")
		}

		// 帧外直接调用替换入口：必须拒绝。
		swapped, when := a.swapPending()
		if swapped {
			t.Fatal("帧外发生了替换——这会让 shaper 在渲染线程之外被换掉（R2）")
		}
		if !when.IsZero() {
			t.Errorf("未替换时 layoutStart 必须是零值, 实际 %v", when)
		}

		// 反向证明：pending 没被上面那次调用吃掉——帧内仍能取到并替换。
		sub := driveFrames(a, 1)
		if sub.count.Load() != 1 {
			t.Fatalf("提交次数 %d, 期望 1", sub.count.Load())
		}
		if !lz.Report().FrameSubmittedAt.IsZero() {
			t.Log("帧外调用未消耗 pending：随后的帧内调用仍然取到了真主题")
		} else {
			t.Fatal("帧内未发生替换：说明帧外那次调用已经把 pending 吃掉了")
		}
	})

	t.Run("替换发生在帧内标记区间内", func(t *testing.T) {
		lz := newTestLazy(t)
		v := &lazyProbeView{}
		a, err := NewLazyApp(v, lz, Config{Title: "swap-inside"})
		if err != nil {
			t.Fatal(err)
		}
		v.owner = a
		requireReady(t, lz)

		driveFrames(a, 3)

		if len(v.inFrames) != 3 {
			t.Fatalf("排版次数 %d, 期望 3", len(v.inFrames))
		}
		for i, ok := range v.inFrames {
			if !ok {
				t.Errorf("第 %d 次排版时 inFrame 为 false——"+
					"替换（发生在排版之前）就不在帧内了", i+1)
			}
		}
		if a.inFrame.Load() {
			t.Error("frame 返回后 inFrame 必须复位为 false")
		}
	})

	t.Run("Take 调用点在 pkg/runtime/app 内唯一", func(t *testing.T) {
		// 结构判据：光有上面两条运行时用例，挡不住"有人另开一个调用点"。
		// 用 AST 而不是字符串匹配：注释里写 Take 是合法的。
		files, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatal(err)
		}
		hits := 0
		for _, f := range files {
			if len(f) > 8 && f[len(f)-8:] == "_test.go" {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
			if err != nil {
				t.Fatalf("解析 %s 失败: %v", f, err)
			}
			ast.Inspect(parsed, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if ok && sel.Sel != nil && sel.Sel.Name == "Take" {
					hits++
				}
				return true
			})
		}
		if hits != 1 {
			t.Errorf("pkg/runtime/app 生产代码里 `.Take()` 命中 %d 处, 期望 1 处"+
				"（完成判据 §7-2：切换路径唯一）", hits)
		}

		t.Run("自检：检测器能识别多出来的调用点", func(t *testing.T) {
			src := "package x\n\ntype L struct{}\n\nfunc (l *L) Take() int { return 0 }\n" +
				"func f(l *L) { _ = l.Take(); _ = l.Take() }\n"
			p, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			ast.Inspect(p, func(node ast.Node) bool {
				if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel != nil && sel.Sel.Name == "Take" {
					n++
				}
				return true
			})
			if n != 2 {
				t.Errorf("检测器漏判：期望 2 处, 实际 %d 处——上一条断言会恒真", n)
			}
		})
	})
}

// ---------------------------------------------------------------------------
// 用例 2：同一次 Ready 只触发一次替换
// ---------------------------------------------------------------------------

// TestFontLazyLoadSwapOncePerGeneration 一次 Ready 只换一次 Shaper，后续帧不再换。
func TestFontLazyLoadSwapOncePerGeneration(t *testing.T) {
	lz := newTestLazy(t)
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "swap-once"})
	if err != nil {
		t.Fatal(err)
	}
	v.owner = a

	// 切换**前**的 shaper：不抓它，首帧就替换的用例会数不出第一次变化。
	tempShaper := a.th.Shaper
	requireReady(t, lz)

	sub := driveFrames(a, 6)
	if sub.count.Load() != 6 {
		t.Fatalf("提交次数 %d, 期望 6", sub.count.Load())
	}
	seq := shaperSeq(tempShaper, v.shapers)
	if got := shaperChanges(seq); got != 1 {
		t.Fatalf("Shaper 变化 %d 次, 期望 1 次（同一次 Ready 只换一次）: %v", got, seq)
	}
	// Take 的一次性在这里直接探一次：
	// 本用例没有任何渲染线程在用 shaper（帧已经跑完），这里的 Take 只是读状态，
	// 不会与渲染并发——结构判据（Take 调用点唯一）只统计生产代码，不受影响。
	if next := lz.Take(); next != nil {
		t.Error("一次 Ready 之后 Take() 仍返回非 nil——重复替换的风险")
	}
	t.Logf("6 帧内 Shaper 变化 %d 次（首帧替换，其后 5 帧复用）", shaperChanges(seq))
}

// ---------------------------------------------------------------------------
// 用例 3：提交时刻晚于加载完成（R1）
// ---------------------------------------------------------------------------

// TestFontLazyLoadFrameSubmittedAfterLoaded R1 的两条可观察后果：
//
//  1. `FrameSubmittedAt >= LoadedAt`，且用例中二者**明显不等**——
//     证明不是把同一个时刻抄进两个字段；
//  2. `FrameSubmittedAt` 记在 `e.Frame` **返回之后**——§1.2 步骤 ⑥ 在 ⑤ 之后。
func TestFontLazyLoadFrameSubmittedAfterLoaded(t *testing.T) {
	t.Run("提交晚于产出、且两个时刻明显不等", func(t *testing.T) {
		lz := newTestLazy(t)
		v := &lazyProbeView{}
		a, err := NewLazyApp(v, lz, Config{Title: "r1-order"})
		if err != nil {
			t.Fatal(err)
		}
		v.owner = a
		requireReady(t, lz)

		before := lz.Report()
		if before.LoadedAt.IsZero() {
			t.Fatal("Ready 之后 LoadedAt 仍为零值——后台产出时刻没有被记录")
		}

		// 人为把"产出"与"提交"拉开：这段是 Invalidate 调度 + 排队，
		// R1 明确禁止把它算进任何耗时指标，这里只用来证明两个字段不是同一个值。
		const gap = 80 * time.Millisecond
		time.Sleep(gap)

		driveFrames(a, 1)

		after := lz.Report()
		if after.FrameSubmittedAt.IsZero() {
			t.Fatal("FrameSubmittedAt 未被写入——MarkFrameSubmitted 没有真的被调用")
		}
		if after.FrameSubmittedAt.Before(after.LoadedAt) {
			t.Errorf("FrameSubmittedAt(%v) 早于 LoadedAt(%v)："+
				"提交不可能发生在产出之前", after.FrameSubmittedAt, after.LoadedAt)
		}
		// 阈值取注入延迟的一半：足以证明"两个字段是两次独立取时刻"，
		// 又不会因调度抖动误判（sleep 的下界由运行时保证，不是估计值）。
		const wantGap = gap / 2
		if d := after.FrameSubmittedAt.Sub(after.LoadedAt); d < wantGap {
			t.Errorf("LoadedAt 与 FrameSubmittedAt 差距仅 %v（注入延迟 %v，期望 >= %v）："+
				"两个字段不能是同一个时刻的两次抄写", d, gap, wantGap)
		} else {
			t.Logf("LoadedAt=%v FrameSubmittedAt=%v 差 %v（注入延迟 %v）",
				after.LoadedAt, after.FrameSubmittedAt, d, gap)
		}
	})

	t.Run("提交时刻记在 e.Frame 返回之后", func(t *testing.T) {
		// 本机计时粒度约 0.53 ms，而"提交"与"记录"之间只差微秒——
		// 用普通帧分不出先后。因此这一帧的 Frame 回调故意睡 5 ms，
		// 把 ⑤ 拉长到远超一个粒度，⑥ 的先后才**可观测**。
		// 这是合成帧带来的好处：真实窗口无法人为拉长呈现路径。
		lz := newTestLazy(t)
		v := &lazyProbeView{}
		a, err := NewLazyApp(v, lz, Config{Title: "r1-order-slow"})
		if err != nil {
			t.Fatal(err)
		}
		v.owner = a
		requireReady(t, lz)

		const submitCost = 5 * time.Millisecond
		var rec frameRecorder
		var ops op.Ops
		a.frame(&ops, app.FrameEvent{
			Now:    time.Now(),
			Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Size:   image.Pt(800, 600),
			Frame: func(*op.Ops) {
				time.Sleep(submitCost)
				rec.lastAt.Store(time.Now().UnixNano())
				rec.count.Add(1)
			},
		})

		rep := lz.Report()
		at := rec.submittedAt()
		if at.IsZero() {
			t.Fatal("e.Frame 未被调用")
		}
		if rep.FrameSubmittedAt.Before(at) {
			t.Errorf("FrameSubmittedAt(%v) 早于 e.Frame 结束时刻(%v)："+
				"步骤 ⑥ 必须在步骤 ⑤ 之后——把 MarkFrameSubmitted 挪到 e.Frame 之前，这条会变红",
				rep.FrameSubmittedAt, at)
		}
		t.Logf("e.Frame 结束于 %v, FrameSubmittedAt = %v（提交成本注入 %v）",
			at, rep.FrameSubmittedAt, submitCost)
	})
}

// ---------------------------------------------------------------------------
// 用例 4：首次排版耗时被记录（评审点名项）
// ---------------------------------------------------------------------------

// TestFontLazyLoadFirstLayoutRecorded 切换后首帧记录了排版耗时，且只记一次。
func TestFontLazyLoadFirstLayoutRecorded(t *testing.T) {
	lz := newTestLazy(t)
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "first-layout"})
	if err != nil {
		t.Fatal(err)
	}
	v.owner = a
	requireReady(t, lz)

	driveFrames(a, 1)

	first := lz.Report()
	if first.FirstLayoutAfterSwap <= 0 {
		t.Fatalf("FirstLayoutAfterSwap = %v, 期望 > 0："+
			"MarkFirstLayout 必须真的把耗时写回 Report", first.FirstLayoutAfterSwap)
	}
	t.Logf("切换后首次排版耗时 = %v", first.FirstLayoutAfterSwap)

	// 后续帧不得覆盖。
	driveFrames(a, 4)
	second := lz.Report()
	if second.FirstLayoutAfterSwap != first.FirstLayoutAfterSwap {
		t.Errorf("首次排版耗时被后续帧覆盖了: %v → %v",
			first.FirstLayoutAfterSwap, second.FirstLayoutAfterSwap)
	}
	if second.FrameSubmittedAt != first.FrameSubmittedAt {
		t.Errorf("FrameSubmittedAt 被后续帧覆盖了: %v → %v",
			first.FrameSubmittedAt, second.FrameSubmittedAt)
	}

	// 幂等性由类型保证（不靠调用点自觉）：直接再写一次也不得改变。
	lz.MarkFirstLayout(first.FirstLayoutAfterSwap + 7*time.Second)
	lz.MarkFrameSubmitted(time.Now().Add(7 * time.Second))
	third := lz.Report()
	if third.FirstLayoutAfterSwap != first.FirstLayoutAfterSwap {
		t.Errorf("MarkFirstLayout 非幂等: %v → %v",
			first.FirstLayoutAfterSwap, third.FirstLayoutAfterSwap)
	}
	if third.FrameSubmittedAt != first.FrameSubmittedAt {
		t.Errorf("MarkFrameSubmitted 非幂等: %v → %v",
			first.FrameSubmittedAt, third.FrameSubmittedAt)
	}
}

// ---------------------------------------------------------------------------
// 用例 5：布局度量差（D6-6）
// ---------------------------------------------------------------------------

// TestFontLazyLoadLayoutDelta 同一段文本在临时字体与真字体下的行宽相对差。
//
// 判据（§3 冻结）：delta = |real - temp| / max(temp, 1)，单行 CJK 阈值 2%。
// 阈值是**判断不是事实**，理由写在下面；换字体/换平台后看本用例的打印值复核。
func TestFontLazyLoadLayoutDelta(t *testing.T) {
	lz := newTestLazy(t)
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "layout-delta"})
	if err != nil {
		t.Fatal(err)
	}
	v.owner = a

	// 切换**前**抓一份主题快照：只换 Shaper 的冻结语义保证了
	// "temp 与 real 的差别 = Shaper 的差别"，没有 Palette / 字号混入。
	tempSnap := *a.th
	requireReady(t, lz)
	driveFrames(a, 1)
	realSnap := *a.th

	// 断言 A（反空判据）：两个快照的 Shaper 必须是**不同对象**。
	//
	// 它验证的是"这次比较确实在比较两个东西"——若替换没发生，temp 与 real
	// 就是同一个 shaper，delta 必然为 0，下面的阈值判据会变成装饰。
	//
	// A 与 B 验证的是**两个不同的性质**，互不替代：
	//   A = 比较有效（不是自己跟自己比）
	//   B = 度量差可接受（D6-6 的实质结论）
	// A 过不代表 B 过（两个不同字体也可能差 30%），
	// B 过也不代表 A 过（同一个 shaper 的 delta 恒为 0）。两条都必要。
	if tempSnap.Shaper == realSnap.Shaper {
		t.Fatal("切换前后是同一个 Shaper：本次度量差无效（可能替换没有发生）")
	}

	wTemp := measureWidth(t, &tempSnap, lazySampleText)
	wReal := measureWidth(t, &realSnap, lazySampleText)
	if wTemp <= 0 || wReal <= 0 {
		t.Fatalf("行宽测量无效: temp=%d real=%d（文本 %q 未被排版）",
			wTemp, wReal, lazySampleText)
	}
	delta := math.Abs(float64(wReal-wTemp)) / math.Max(float64(wTemp), 1)
	t.Logf("样本 %q：temp 宽 %d px, real 宽 %d px, delta = %.4f%%（阈值 2%%）",
		lazySampleText, wTemp, wReal, delta*100)
	t.Logf("临时主题来源 = %s", lz.Report().TempSource)

	// 断言 B（D6-6 的实质结论）：相对差在阈值内。
	//
	// 2% 的理由：CJK 是等宽方块字，同一字号下汉字 advance 由 em 决定，
	// 两个都覆盖样本字符的字体，其 em 宽度差异通常 < 1%；2% 留了一倍余量，
	// 又远小于"换成了完全不同度量（缺字 fallback 到拉丁字体）"时的量级
	// （那种情况差值通常在 30% 以上，一眼可辨）。
	const threshold = 0.02
	if delta > threshold {
		t.Errorf("布局相对差 %.4f%% 超过阈值 %.2f%%："+
			"必须给出修复或说明，不得只记录数字。temp=%d real=%d",
			delta*100, threshold*100, wTemp, wReal)
	}
}

// ---------------------------------------------------------------------------
// 用例 6：切换后不再产生 shaping 冷启动（D6-3）
// ---------------------------------------------------------------------------

// TestFontLazyLoadNoRepeatShaping 切换后连续多帧不再产生 shaping 冷启动。
//
// # 为什么判据是"批量总量"而不是"逐帧尖峰"
//
// 本机实测（排查记录见 dev-log）：`time.Now()` 的粒度约 **0.53 ms**
// ——2000 次空区间测量全部返回 0 ns；忙等到计时器翻动，翻动值稳定在
// 523–548 µs。而 warm shaping 的单帧真值经批量反推约为 2 µs。
// 也就是说：**单帧耗时在本机根本读不出来**（小于一个计时粒度）。
//
// 若按"逐帧 max/min"写判据，min=max=0 会让不等式恒真——那是空判据，
// 比没有断言更危险（它看起来是绿的）。因此改成测 N 帧的**总耗时**：
// N 取到让总量远超一个粒度（2000 帧 ≈ 4 ms ≈ 8 个粒度），读数才可信。
//
// # 判据
//
//	切换帧（含 1 次冷启动）= d_swap
//	其后 N 帧总耗时          = T
//	断言 T <= 10 * d_swap
//
// 理由：冷启动是"解析字体集合 + 建字形缓存"，一次性且昂贵（本机 40–100 ms）；
// 缓存命中的 shaping 是微秒级。若冷启动在切换后又发生，N 帧的成本会是
// N 次冷启动，比 10 次高出两个数量级，判据不可能误判。
// 它抓不住细微回归——那是 D-ACCEPT 定阈值的事（§8 明令本卡不定阈值）。
//
// # 两个"测不到就报未验证"的护栏
//
// `d_swap <= 0` 或 `T <= 0` 都说明本机计时粒度撑不起这个判据。此时必须
// 失败并写明"未验证"，**不得**让不等式在 0 上恒真而假装通过。
//
// # 提前跳出
//
// 万一真的每帧都在冷启动，跑满 2000 帧要几十秒。因此每 50 帧看一次累计
// 耗时，超预算立刻跳出并判失败——失败路径也有界。
//
// # 三条"不要把本用例读成它不是的东西"
//
//  1. `T / N`（打印出来的"单帧均值"）**不是**逐帧性能承诺：它是总量除以帧数
//     得到的算术平均，分辨率受本机 0.53 ms 粒度限制，且不含置信区间。
//     它只用于人工复核，**不得**被引为"Goxml 单帧排版耗时 ≤ X"这类结论。
//  2. 本用例**不是**性能基准测试：没有 warmup 隔离、没有多轮取最小值、
//     没有屏蔽频率缩放，也不与其它机器比较。定阈值是 D-ACCEPT 的事。
//  3. `d_swap` 与 `T` 都是**本机单次**观测值。换机器、换字体、换 Gio 版本
//     都要重新看打印值，不得把本轮数字当成跨环境的既有事实。
func TestFontLazyLoadNoRepeatShaping(t *testing.T) {
	lz := newTestLazy(t)
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "no-repeat-shaping"})
	if err != nil {
		t.Fatal(err)
	}
	v.owner = a
	requireReady(t, lz)

	// 第 1 帧：切换帧，含真字体的 shaping 冷启动。
	driveFrames(a, 1)
	if len(v.durations) != 1 {
		t.Fatalf("排版次数 %d, 期望 1", len(v.durations))
	}
	swapCost := v.durations[0]
	if swapCost <= 0 {
		t.Fatalf("切换帧耗时 = %v：本机计时粒度约 0.53 ms，测不出这个量级。"+
			"D6-3 的耗时判据在本机**无法成立**——记为未验证，不得当作通过",
			swapCost)
	}

	const (
		afterFrames = 2000 // ≈ 4 ms，约 8 个计时粒度
		checkEvery  = 50   // 失败路径的耗时上界：50 帧 × 冷启动
	)
	budget := 10 * swapCost

	var ops op.Ops
	var rec frameRecorder
	start := time.Now()
	frames := 0
	for i := 1; i <= afterFrames; i++ {
		a.frame(&ops, synthFrameEvent(&rec))
		frames = i
		// 跳出条件与下面的失败条件**同一个**（total 单调不减），
		// 因此这里不需要额外的"已跳出"标记——多一个标记就是多一处可能失配的状态。
		if i%checkEvery == 0 && time.Since(start) > budget {
			break
		}
	}
	total := time.Since(start)
	if int(rec.count.Load()) != frames {
		t.Fatalf("提交次数 %d, 期望 %d", rec.count.Load(), frames)
	}
	if total <= 0 {
		t.Fatalf("连续 %d 帧总耗时 = %v：计时器读不出该量级，判据会在 0 上恒真——"+
			"必须记为未验证", frames, total)
	}

	t.Logf("切换帧（含 1 次冷启动）= %v；其后 %d 帧总耗时 = %v；预算 = %v",
		swapCost, frames, total, budget)
	t.Logf("总量/帧数 = %v（仅供人工复核，非逐帧性能承诺，见本用例注释）",
		total/time.Duration(frames))
	t.Logf("逐帧存档值（本机恒为 0，仅备查，不作判据）= %v", v.durations[1:6])

	if total > budget {
		t.Errorf("切换后仅 %d 帧就耗掉 %v，超过 10 倍冷启动成本（%v）："+
			"shaping 冷启动在切换后又反复发生了", frames, total, budget)
	}
}

// ---------------------------------------------------------------------------
// 用例 7：唤醒/替换断言不得数帧
// ---------------------------------------------------------------------------

// TestFontLazyLoadNoFrameCounting 替换由**状态**驱动，不由帧序号驱动。
//
// 两个到达时机相反的子用例：若实现（或断言）写死"第 N 帧替换"，
// 至少有一个子用例会失败——这正是本用例存在的理由。
func TestFontLazyLoadNoFrameCounting(t *testing.T) {
	t.Run("早到：Ready 后的第一帧就替换", func(t *testing.T) {
		lz := newTestLazy(t)
		v := &lazyProbeView{}
		a, err := NewLazyApp(v, lz, Config{Title: "arrive-early"})
		if err != nil {
			t.Fatal(err)
		}
		v.owner = a
		tempShaper := a.th.Shaper
		requireReady(t, lz)

		driveFrames(a, 1)

		// 断言用状态，不用帧号。
		if lz.Report().FrameSubmittedAt.IsZero() {
			t.Error("Ready 之后的第一帧没有替换：实现若按固定帧号（>1）替换，这里会失败")
		}
		if got := shaperChanges(shaperSeq(tempShaper, v.shapers)); got != 1 {
			t.Errorf("Shaper 变化 %d 次, 期望 1 次", got)
		}
	})

	t.Run("晚到：Ready 之前的每一帧都不得替换", func(t *testing.T) {
		lz := newTestLazy(t)
		v := &lazyProbeView{}
		a, err := NewLazyApp(v, lz, Config{Title: "arrive-late"})
		if err != nil {
			t.Fatal(err)
		}
		v.owner = a
		// 切换**前**的 shaper：这里可能在到达前跑 0 帧（加载极快），
		// 逐帧记录的第一项就已经是真字体，变化次数会算成 0。
		tempShaper := a.th.Shaper

		// 一路跑帧，直到状态变成 Ready。期间跑了几帧**不作断言**（随机器而变，
		// 本轮实测 1–235 帧都出现过），这就是"断言不得写帧号"的由来。
		//
		// ⚠ 竞态修正（本用例曾因此偶发失败）：后台加载可能在**某一帧运行期间**
		// 完成。那一帧开始时状态还不是 Ready、结束时已经是，它究竟算"到达前"
		// 还是"到达后"无法判定——把这种帧当成"到达前的帧"去断言"不得替换"
		// 就是拿一个不可判定的事实下结论，会在全量跑（时序不同）时偶发失败。
		//
		// 解法：按**帧的起止状态是否一致**给每一帧分类——
		//   - 起止一致（都未 Ready）：确定处于到达前，断言不得替换；
		//   - 起止不一致：跨越到达时刻，**单列并跳过**判定，不强行下结论。
		var ops op.Ops
		var rec frameRecorder
		before, straddle := 0, 0
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if lz.State() == theme.LoadReady {
				break
			}
			pre := lz.State()
			a.frame(&ops, synthFrameEvent(&rec))
			post := lz.State()
			before++
			if pre != post {
				// 跨越到达时刻：无法判定先后，只计数。
				straddle++
			} else if !lz.Report().FrameSubmittedAt.IsZero() {
				t.Fatalf("确定处于到达前的第 %d 帧发生了替换："+
					"替换不是由状态驱动的", before)
			}
			if before > 500 {
				break
			}
		}
		if lz.State() != theme.LoadReady {
			t.Fatalf("15 秒内未进入 Ready（state=%s）", lz.State())
		}

		// 若跨越帧没有替到（多数情况），Ready 之后的第一帧必须替换。
		if lz.Report().FrameSubmittedAt.IsZero() {
			a.frame(&ops, synthFrameEvent(&rec))
			if lz.Report().FrameSubmittedAt.IsZero() {
				t.Error("Ready 之后的第一帧没有替换")
			}
		}
		if got := shaperChanges(shaperSeq(tempShaper, v.shapers)); got != 1 {
			t.Errorf("Shaper 变化 %d 次, 期望恰好 1 次", got)
		}
		t.Logf("Ready 之前跑了 %d 帧（其中 %d 帧跨越到达时刻，不作判定），"+
			"替换发生在到达后的第一帧——替换帧号随到达时机变化，"+
			"因此断言只能是状态，不能是帧号", before, straddle)
	})
}

// ---------------------------------------------------------------------------
// 入口与参数：NewLazyApp 本身的契约
// ---------------------------------------------------------------------------

// TestNewLazyAppValidatesParams 参数校验与接线顺序的可观察后果。
//
// 名称不以 TestFontLazyLoad 开头（§4 只约束那七个用例），但同属本卡交付：
// 没有它，"nil lazy 会怎样""SetNotify 是否先于 Request"就没有可执行判据。
func TestNewLazyAppValidatesParams(t *testing.T) {
	lz := newTestLazy(t)
	if _, err := NewLazyApp(nil, lz, Config{}); err == nil {
		t.Error("nil view 应被拒绝")
	}
	if _, err := NewLazyApp(&lazyProbeView{}, nil, Config{}); err == nil {
		t.Error("nil lazy 应被拒绝")
	}

	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "lazy-params"})
	if err != nil {
		t.Fatalf("合法参数创建失败: %v", err)
	}
	if a.th == nil {
		t.Fatal("NewLazyApp 未持有主题：Request 之后临时主题必须可用")
	}
	if a.th.Shaper == nil {
		t.Error("临时主题缺少 Shaper：首帧将无法画出汉字")
	}
	if a.lz != lz {
		t.Error("NewLazyApp 未持有传入的 Lazy 句柄")
	}
	if lz.State() == theme.LoadUnloaded {
		t.Error("NewLazyApp 之后状态仍为 unloaded：Request 没有被调用")
	}
	// SetNotify 必须**先于** Request：反了就会晚绑定。
	if lz.Report().NotifyLateBound {
		t.Error("正常路径下 NotifyLateBound 为真：SetNotify 与 Request 的顺序错了")
	}
	t.Logf("NewLazyApp：state=%s tempSource=%s", lz.State(), lz.Report().TempSource)
}

// TestLazyAppDoesNotTouchNotifyLateBound 完成判据：本包对该字段零命中。
//
// 第 3 版把 `NotifyLateBound` 的写入责任收回 IMPL-1（`Lazy.SetNotify` 内部）。
// 这是 IMPL-1 §9-4 的验收项之一，用 grep 固化而不是靠自觉。
func TestLazyAppDoesNotTouchNotifyLateBound(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if len(f) > 8 && f[len(f)-8:] == "_test.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if containsIdentifier(string(b), "NotifyLateBound") {
			t.Errorf("%s 出现了 NotifyLateBound：该字段的写入责任在 "+
				"theme.Lazy.SetNotify，pkg/runtime/app 必须零命中", f)
		}
	}
}

// containsIdentifier 判定源码里是否出现该标识符（含注释中的提及）。
//
// 用**词法**扫描而不是 strings.Contains：注释里解释这个字段是必要的，
// 但注释里**写字段**与代码里**写字段**必须区分开——本判据只认代码。
func containsIdentifier(src, name string) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		return true // 解析不了就判为"有"，fail-closed
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}
