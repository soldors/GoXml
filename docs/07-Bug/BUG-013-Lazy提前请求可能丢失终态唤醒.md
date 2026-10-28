# BUG-013：Lazy提前请求可能丢失终态唤醒

- 状态：候选，源码级可达性已审查，尚未运行复现；初判P1。
- 基线：35dc6a1；相关异步接线来自9f3ab8e。
- 触发：先对未绑定notify的Lazy调用Request，再交给NewLazyApp；首帧仍Loading，后续窗口空闲。

## 证据与机制

`pkg/runtime/theme/lazy.go:335`附近捕获当时notify到worker；`pkg/runtime/app/app.go:287`附近SetNotify失败后仅在构造器补一次Invalidate。Gio v0.10.3 app/window.go:284附近在mayInvalidate=false时不投递，该时刻窗口驱动可能尚未创建。若worker之后没有有效回调、也无其它帧，最终主题可能无法显示。

实现符合当前任务卡的单次补偿条文；待审的是设计中“以后还会有帧”的假设，不能直接称实现违背冻结条文。正常先绑定再Request不受此路径影响。StrictFontFailure按现有契约在窗口关闭后返回，不能把“没立即返回严格错误”列为本缺陷证据。

## 复现与关闭条件

用channel控制先Request/首帧/加载完成的顺序，不用sleep猜时序；验证实际最终主题应用而非仅回调计数。另覆盖已有notify、已Ready、失败重试与正常路径。源码修复不得用永久轮询或放宽断言替代。旧实现红→候选绿、两包回归/vet及独立复核后才关闭。真实窗口未执行，不升级正式验收。

来源：[代码审查记录](../05-开发/2026-10-29-代码审查记录.md)。
