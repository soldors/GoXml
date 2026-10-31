# BUG-022：窗口先退出时stop等待者不收口

- 级别：P2，测试/集成窗口入口的goroutine资源泄漏。
- 状态：修复及作者外审通过，组合验证见[本批记录](../06-测试/2026-10-31-并行开发验证记录.md)。

## 复现与根因

RunWindowWithInvalidator创建的后台等待者只接收`stop`。若窗口先收到DestroyEvent返回，而stop未关闭或为nil，该goroutine永远等待。只检查RunWindow返回值看不到泄漏。

## 修复与证据

实际事件泵退出时关闭loopDone，等待者同时观察两个终止信号，已经退出时不再请求Close。内部窄接口用于无窗口驱动，公开API、Layout/Frame及Invalidate顺序保持。提取旧逻辑的红基线rc1；修复后生命周期回归重复20次rc0，独立外审确认接线。见[window_lifecycle_test.go](../../pkg/runtime/app/window_lifecycle_test.go)及[运行时日志](../05-开发/dev-log/2026-10-31-runtime.md)。不承诺取消已经进入Gio Perform的底层操作，未执行原生窗口/race验证。

## 协调者接纳

对应已审核修复已按范围接纳主仓：字体3357d8d、窗口d37cc00、编译器与CLI9e81534。组合源包、相关主仓回归及构建/静态检查通过；具体覆盖与未执行项见本批验证记录。
