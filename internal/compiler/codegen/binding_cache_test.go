package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// 步骤 3：绑定缓存五场景验证
//
// 契约依据：绑定系统契约 §05.3.1（场景 ③ 已冻结规则）。
//
// 观测分层（对应任务卡 §2 的验收底线）：
//   - 可靠观测：槽位状态（valid/text/call）与渲染像素
//     → 可证明「显示转换层缓存」是否命中/失效；
//   - 不可靠观测：Gio 内部 shaping（layoutCache 未导出）
//     → 只能报告「文本布局入口调用次数」（通过槽位回放间接证明），
//       **不得宣称验证了 Gio 内部 shaping 缓存**。
// ============================================================================

// b3Probe 是五场景共用的探测框架。
const b3Probe = `
func b3Layout(v *GoxmlDashboardView, th *material.Theme, vm *fixture.ViewModel, w, h int) (int, *textCache) {
	win, err := headless.NewWindow(w, h)
	if err != nil {
		return -1, nil
	}
	defer win.Release()
	var ops op.Ops
	v.Layout(rtGtx(&ops, w, h), th, vm)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		return -1, nil
	}
	return rtNonZero(img.Pix), &v.slots[0]
}
`

// 场景 1：稳定值 → 复用（槽位命中），渲染不变。
func TestB3Scene1_StableValue(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, b3Probe+`
func TestProbeScene1(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	vm := &fixture.ViewModel{Title: "AAAA"}

	n1, c1 := b3Layout(v, th, vm, 240, 60)
	t1, call1 := c1.text, c1.call
	n2, _ := b3Layout(v, th, vm, 240, 60)
	n3, c3 := b3Layout(v, th, vm, 240, 60)

	t.Logf("三帧像素: %d %d %d；text=%q", n1, n2, n3, c3.text)
	if n1 <= 0 {
		t.Skip("渲染全零")
	}
	// 三帧渲染一致
	if n1 != n2 || n2 != n3 {
		t.Errorf("稳定值三帧渲染不一致: %d %d %d", n1, n2, n3)
	}
	// 缓存命中：text 未变、call 未变（回放而非重录）
	if c3.text != t1 || c3.call != call1 || !c3.valid {
		t.Error("稳定值应命中缓存（text/call 不变）")
	} else {
		t.Log("稳定值：命中显示缓存（回放，未重录）")
	}
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("场景1失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// 场景 2：值变化 → 失效重录，渲染新值。
func TestB3Scene2_ValueChange(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, b3Probe+`
func TestProbeScene2(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	vm := &fixture.ViewModel{Title: "AAAA"}

	n1, _ := b3Layout(v, th, vm, 240, 60)
	before := v.slots[0].text

	// 改值
	vm.Title = "WWWWWWWWWWWW"
	n2, c := b3Layout(v, th, vm, 240, 60)

	t.Logf("改前=%d 像素(text=%q) 改后=%d 像素(text=%q)", n1, before, n2, c.text)
	if n1 <= 0 || n2 <= 0 {
		t.Skip("渲染全零")
	}
	// 显示串必须更新
	if c.text != "WWWWWWWWWWWW" {
		t.Errorf("缓存 text 未更新: %q", c.text)
	}
	// 渲染必须变化（新值进入渲染路径）
	if n1 == n2 {
		t.Error("改值后渲染未变化——可能错误复用了旧缓存")
	} else {
		t.Log("值变化：缓存失效并重录，渲染更新")
	}
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("场景2失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// 场景 3：原始值变化但显示串不变 → 按 §05.3.1 允许复用。
//
// 注意：本项目显示转换当前是恒等（string 直接用），
// 因此「原始值变但显示串不变」在 string 绑定上不可构造。
// 该场景对非 string 类型（如 int 值 007 与 7 显示相同）才有意义。
// 此处锁定：显示串相同时缓存命中（与场景 1 同一判据）。
func TestB3Scene3_SameDisplay(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, b3Probe+`
func TestProbeScene3(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	vm := &fixture.ViewModel{Title: "AAAA"}

	n1, c1 := b3Layout(v, th, vm, 240, 60)
	call1 := c1.call

	// 显示串不变（重新赋相同值）
	vm.Title = "AAAA"
	n2, c2 := b3Layout(v, th, vm, 240, 60)

	t.Logf("两次像素: %d %d；call 相同=%v", n1, n2, c2.call == call1)
	if n1 <= 0 {
		t.Skip("渲染全零")
	}
	if n1 != n2 {
		t.Errorf("显示串相同时渲染不一致")
	}
	if c2.call != call1 {
		t.Error("显示串相同应复用（§05.3.1 允许复用）")
	} else {
		t.Log("显示串不变：复用已录制的 CallOp；Gio 内部 shaping 次数未直接观测")
	}
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("场景3失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// 场景 4：shaping 输入变化（约束/DPI）→ 必须失效。
func TestB3Scene4_InputsChange(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, b3Probe+`
func TestProbeScene4(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	vm := &fixture.ViewModel{Title: "AAAA"}

	// 约束变化 → renderKey 变化 → 必须重录
	var o1 op.Ops
	v.Layout(rtGtx(&o1, 240, 60), th, vm)
	k1 := v.slots[0].key
	call1 := v.slots[0].call

	var o2 op.Ops
	v.Layout(rtGtx(&o2, 100, 40), th, vm)
	k2 := v.slots[0].key
	call2 := v.slots[0].call

	t.Logf("key 变化=%v call 变化=%v", k1 != k2, call1 != call2)
	if k1 == k2 {
		t.Error("约束变化后 renderKey 未变化")
	}
	// 回到原约束 → key 回到原值，但缓存已被覆盖（只保留最新一份）
	var o3 op.Ops
	v.Layout(rtGtx(&o3, 240, 60), th, vm)
	k3 := v.slots[0].key
	if k3 != k1 {
		t.Error("恢复约束后 key 应与最初相同")
	}
	// 主题变化 → 失效
	th2 := material.NewTheme()
	var o4 op.Ops
	v.Layout(rtGtx(&o4, 240, 60), th2, vm)
	if v.slots[0].key.theme != th2 {
		t.Error("主题变化后 key.theme 未更新")
	}
	t.Log("约束/主题变化均正确失效")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("场景4失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// 场景 5：读取失败不污染缓存。
//
// 本项目绑定是静态类型字段访问，读取失败分支不存在（步骤 2 已核对）。
// 可构造的最接近情形：显示串为空。锁定缓存行为可预期即可。
func TestB3Scene5_EmptyValue(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, b3Probe+`
func TestProbeScene5(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	vm := &fixture.ViewModel{Title: "AAAA"}

	n1, _ := b3Layout(v, th, vm, 240, 60)

	// 空值
	vm.Title = ""
	n2, c := b3Layout(v, th, vm, 240, 60)

	t.Logf("有值=%d 空值=%d text=%q", n1, n2, c.text)
	if n1 <= 0 {
		t.Skip("渲染全零")
	}
	if c.text != "" {
		t.Errorf("空值后缓存 text 应为空: %q", c.text)
	}
	// 恢复有值
	vm.Title = "AAAA"
	n3, c3 := b3Layout(v, th, vm, 240, 60)
	if c3.text != "AAAA" || n3 != n1 {
		t.Errorf("恢复后应回到原渲染: n1=%d n3=%d", n1, n3)
	}
	t.Log("空值不污染缓存；恢复后渲染与最初一致")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("场景5失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestB3_MustNotFreezeWholePage 红线 2 再确认：
// 绑定缓存不得重新引入整页静态固化。
func TestB3_MustNotFreezeWholePage(t *testing.T) {
	v := verified(t, `<Text Value="{Title}"/>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// 整页固化的标志：Layout 内直接 op.Record（不含槽位私有录制）。
	if strings.Contains(s, "v.cacheOps") || strings.Contains(s, "cacheCall") || strings.Contains(s, "cacheDims") {
		t.Errorf("绑定页面不得重新引入整页静态缓存字段:\n%s", s)
	}
	// 但必须保留槽位缓存。
	if !strings.Contains(s, "textCache") || !strings.Contains(s, "bindText") {
		t.Error("绑定页面应生成槽位缓存")
	}
}

// TestB3_StableFrameAlloc 稳定帧分配测量（INV-PERF-4 的 allocs/op 维度）。
//
// 记录环境与预热方法；结果如实报告，不预设必须为 0。
func TestB3_StableFrameAlloc(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, b3Probe+`
func TestProbeAlloc(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	vm := &fixture.ViewModel{Title: "AAAA"}
	const w, h = 240, 60

	// 预热：触发首次录制
	var warm op.Ops
	v.Layout(rtGtx(&warm, w, h), th, vm)

	b := testing.Benchmark(func(b *testing.B) {
		var o op.Ops
		g := rtGtx(&o, w, h)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			o.Reset()
			v.Layout(g, th, vm)
		}
	})
	// testing.Benchmark 的结果用 AllocsPerOp/AllocedBytesPerOp 读取
	// （-bench 的 allocs/op 列在程序化调用时不打印）。
	t.Logf("稳定帧: ns/op=%d B/op=%d allocs/op=%d",
		b.NsPerOp(), b.AllocedBytesPerOp(), b.AllocsPerOp())
}
`)
	t.Log(rtFilter(out))
	if !contains(out, "[err=nil]") {
		t.Fatalf("分配测量失败:\n%s", out)
	}
	// 从原始输出提取 Benchmark 行（rtFilter 会滤掉英文基准行）。
	for _, line := range splitLines(out) {
		if contains(line, "allocs/op") {
			t.Log("基准: " + trimSpace(line))
			return
		}
	}
	t.Fatalf("未产生 allocs/op 数据，原始输出:\n%s", out)
}
