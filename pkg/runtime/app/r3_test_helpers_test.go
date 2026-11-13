//go:build !nativewindow

// R29-C R3 受控测试辅助：Open（注册）与 Run（启动）分离后，凡是要驱动
// 窗口事件源的用例必须显式启动宿主。r3StartHost 只替换 mainFunc 为
// 「立即返回」的桩，并隔离包级 Gio Main 所有权状态；不触碰生产路径、
// 不创建平台窗口。
package app

import "testing"

func r3StartHost(t *testing.T, h *WindowHost) {
	t.Helper()
	resetGioMainForTest()
	t.Cleanup(resetGioMainForTest)
	orig := mainFunc
	mainFunc = func() {}
	t.Cleanup(func() { mainFunc = orig })
	if err := h.Run(); err != nil {
		t.Fatalf("宿主启动失败: %v", err)
	}
}
