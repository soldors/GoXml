package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// S4：三缓存协同回归（静态缓存 / 绑定显示缓存 / 状态通知）
//
// S4 的核心问题不是「刷新是否生效」（S2/S3 在真实窗口已证），
// 而是「**Invalidate 触发重绘时，缓存是否被不必要地击穿**」：
//
//   - 值变化 → 绑定缓存必须失效重录（正确性，步骤 3 已证）；
//   - 值未变化（纯重绘）→ 绑定缓存必须**命中回放**——否则状态通知
//     机制会击穿 INV-PERF-4 的稳定帧零分配成果。
//
// 观测手段（吸取步骤 2 假通过教训）：**不用 call 指针判断重录**——
// ADR-0009/A5.1 实测同 Ops 重录相同内容会产生完全相同的 CallOp。
// 可靠信号：**bindText 的回放分支不写 text/key**（与 cacheKey 信号同一原理），
// 且真实生成代码在仓库内临时包中执行。
// ============================================================================

// TestS4_StableAcrossRedraws 值未变、布局被多次调用（模拟纯 Invalidate 重绘）：
// 绑定缓存必须持续命中——text/key 不变、不重录。
//
// 若失败，说明状态通知机制会击穿显示缓存：每秒 ticker 的重绘
// 会把 0 allocs/op 的稳定帧拖回每帧重录。
func TestS4_StableAcrossRedraws(t *testing.T) {
	probe := `
func TestProbeS4(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "AAAA"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	// 首帧：录制
	var o1 op.Ops
	v.Layout(rtGtx(&o1, 240, 60), th)
	if len(v.slots) != 1 || !v.slots[0].valid {
		t.Fatalf("首帧后槽位未录制: slots=%d", len(v.slots))
	}
	text1 := v.slots[0].text
	key1 := v.slots[0].key

	// 模拟状态通知触发的 5 次纯重绘（值未变）。
	for i := 0; i < 5; i++ {
		var o op.Ops
		v.Layout(rtGtx(&o, 240, 60), th)
	}

	// 回放分支不写 text/key：若被重录，虽然 text 相同（内容一致），
	// 但 renderKey 会被重写——用「key 指针位模式 + text」双重信号不可靠，
	// 因此这里用决定性证据：**bindText 回放路径不再执行 Body1 布局**。
	// 通过布局计数验证：见 TestS4_RedrawDoesNotRelayout。
	_ = text1
	_ = key1
	t.Log("5 次重绘完成；确定性观测见 TestS4_RedrawDoesNotRelayout")
}
`
	out := rtProbe(t, `<Text Value="{Title}"/>`, probe)
	if !contains(out, "[err=nil]") {
		t.Fatalf("S4 稳定重绘探测失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestS4_RedrawDoesNotRelayout 决定性证据：
//
// 纯重绘（值未变）时，文本布局入口**不得**再次执行。
// 探测里在 Body1 外无法挂钩，因此用**逐帧分配计数**：
// 命中回放的 Layout 每帧 0 分配（A4/步骤 3 实测 140ns/0B/0allocs）；
// 若每次重绘都重新布局文本，allocs/op 必然大于 0。
//
// 这与「避免重复 shaping 不能只凭分配数推断」的契约条款一致：
// 本测试证明的是**显示转换层与录制层未重复工作**（分配层面），
// Gio 内部 shaping 仍标注为未直接观测。
func TestS4_RedrawDoesNotRelayout(t *testing.T) {
	probe := `
func TestProbeS4Alloc(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "AAAA"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	// 预热：首帧录制
	var warm op.Ops
	v.Layout(rtGtx(&warm, 240, 60), th, )

	// 稳定重绘基准：值未变的连续布局（状态通知触发的重绘形态）
	b := testing.Benchmark(func(b *testing.B) {
		var o op.Ops
		g := rtGtx(&o, 240, 60)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			o.Reset()
			v.Layout(g, th)
		}
	})
	t.Logf("纯重绘稳定帧: ns/op=%d B/op=%d allocs/op=%d",
		b.NsPerOp(), b.AllocedBytesPerOp(), b.AllocsPerOp())

	if b.AllocsPerOp() != 0 {
		t.Errorf("值未变的重绘出现 %d allocs/op——显示缓存被击穿", b.AllocsPerOp())
	}
}
`
	out := rtProbe(t, `<Text Value="{Title}"/>`, probe)
	if !contains(out, "[err=nil]") {
		t.Fatalf("S4 分配观测失败:\n%s", out)
	}
	for _, line := range splitLines(out) {
		if contains(line, "allocs/op") {
			t.Log(trimSpace(line))
		}
	}
}

// TestS4_ChangeStillInvalidates 对照（正确性不因性能回退）：
// 值变化后必须重录——显示串更新、新帧渲染新值（步骤 3 场景 2 的复测）。
func TestS4_ChangeStillInvalidates(t *testing.T) {
	probe := `
func TestProbeS4Change(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "AAAA"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var o1 op.Ops
	v.Layout(rtGtx(&o1, 240, 60), th)
	if v.slots[0].text != "AAAA" {
		t.Fatalf("首帧 text = %q", v.slots[0].text)
	}

	// 改值（调用方同步后）→ 布局
	vm.Title = "WWWWWWWW"
	var o2 op.Ops
	v.Layout(rtGtx(&o2, 240, 60), th)
	if v.slots[0].text != "WWWWWWWW" {
		t.Fatalf("改值后 text = %q, 期望 WWWWWWWW——缓存未失效", v.slots[0].text)
	}
	t.Log("改值后缓存正确失效并重录")
}
`
	out := rtProbe(t, `<Text Value="{Title}"/>`, probe)
	if !contains(out, "[err=nil]") {
		t.Fatalf("S4 失效对照失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestS4_StaticPageCacheUnaffected 静态页面回归：
// 状态机制加入后，纯静态页面仍走整页静态缓存（op.Record/CallOp）。
func TestS4_StaticPageCacheUnaffected(t *testing.T) {
	out := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	s := string(out)
	for _, want := range []string{"op.Record", "op.CallOp", "cacheKey", "goxmlRenderKey"} {
		if !strings.Contains(s, want) {
			t.Errorf("静态页面应保留 %q", want)
		}
	}
	if strings.Contains(s, "bindText") || strings.Contains(s, "goxmlTextCache") {
		t.Error("静态页面不应生成绑定缓存")
	}
	t.Log("静态页面缓存形态未受状态机制影响")
}
