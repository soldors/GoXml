package codegen

import (
	"testing"
)

// ============================================================================
// B0-F10：生成代码必须直接拒绝 nil ViewModel（S2 遗留缺口闭环）
//
// S2 已冻结语义（状态更新契约 §4）：
//   绑定页面构造期 nil vm 明确拒绝，不静默降级。
//   失败形式 = 返回 error（仓库惯例：NewTypeEnv 先例；panic 仅用于内部不变式破坏）。
//
// 之前的测试只验证了「手写等价物」（app 包的 newBindingView），
// 本文件补上**对真实生成代码**的直接断言——B0-F10 指出的正是这个缺口。
// ============================================================================

// TestGeneratedConstructorRejectsNilVM 对生成代码直接调用 NewXxxView(nil)。
//
// 在仓库内临时包中执行真实生成代码：构造函数必须返回非 nil error，
// 且不得产出可用视图（防止静默降级成空文本页面）。
func TestGeneratedConstructorRejectsNilVM(t *testing.T) {
	probe := `
func TestProbeNilVM(t *testing.T) {
	// ① nil 必须返回 error
	v, err := NewGoxmlDashboardView(nil)
	if err == nil {
		t.Fatal("nil ViewModel 必须返回 error（状态更新契约 §4）")
	}
	if v != nil {
		t.Error("nil ViewModel 时不得返回可用视图")
	}
	// ② 合法 vm 正常构造
	v2, err2 := NewGoxmlDashboardView(&fixture.ViewModel{Title: "ok"})
	if err2 != nil {
		t.Fatalf("合法 vm 构造失败: %v", err2)
	}
	if v2 == nil {
		t.Fatal("合法 vm 构造出 nil 视图")
	}

	// ③ 拒绝后缓存状态未被污染：合法构造的视图初始槽位应为空
	if len(v2.slots) != 0 {
		t.Errorf("新构造视图的槽位应惰性初始化，实际 %d", len(v2.slots))
	}
	t.Log("生成代码构造期 nil 拒绝生效；合法构造正常")
}
`
	out := rtProbe(t, `<Text Value="{Title}"/>`, probe)
	if !contains(out, "[err=nil]") {
		t.Fatalf("B0-F10 探测失败:\n%s", out)
	}
	t.Log(rtFilter(out))
	t.Log("B0-F10 闭环：生成代码构造期 nil 拒绝有直接测试证据")
}
