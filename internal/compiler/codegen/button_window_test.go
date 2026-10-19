package codegen

import (
	"testing"
)

// ============================================================================
// 真实窗口边界：带按钮的生成视图在真实 app.Window 中运行
//
// 诚实的范围声明：
//   - **能**验证：生成的 View（含 widget.Clickable 字段与轮询）在真实窗口
//     事件循环里可正常布局、不 panic、能干净退出；
//   - **不能**验证：向真实窗口注入 **OS 级**指针事件。Gio 的 app.Window
//     只提供 Perform(actions) 与 Invalidate，没有指针注入 API，
//     测试进程也无法伪造系统输入事件。
//
// 因此「点击 → 处理器执行」由 button_click_test.go 用 input.Router
// 驱动真实事件管线来证明；本文件只证明真实窗口接线不崩。
// 两者是**互补证据**，任一都不能单独代表另一项。
// ============================================================================

// TestButtonInRealWindowRuns 生成视图在真实窗口中运行并干净退出。
func TestButtonInRealWindowRuns(t *testing.T) {
	out := rtProbe(t, `<Column><Text Value="{Title}"/><Button Text="+1" Click="Increment"/></Column>`, `
func TestProbeRealWindow(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Title: "hello"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- app.RunWindow(v, th, app.Config{
			Title:  "button-in-window",
			Width:  200,
			Height: 120,
		}, stop)
	}()

	// 给事件循环一点时间；若有 panic 会在此暴露。
	time.Sleep(500 * time.Millisecond)
	close(stop)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("真实窗口返回错误: %v", err)
		}
		t.Log("带按钮的生成视图在真实窗口事件循环中运行并干净退出")
	case <-time.After(3 * time.Second):
		t.Fatal("真实窗口未在 3 秒内退出")
	}
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("真实窗口接线失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
