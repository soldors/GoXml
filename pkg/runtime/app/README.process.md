# 显式单窗进程入口

R27 Windows 基础范围已批准，无窗口/race与本机原生限定验收通过，准确发布身份见 Git 与协调者凭据。

```go
app.RunSingleWindowProcess(view, app.Config{Title: "My app"}, app.ProcessOptions{})
```

仅由拥有整个进程的单窗 `main` 显式调用。主线程运行 Gio Main；窗口销毁后先调用显式 `WithOnDestroy` 回调、完成清理，再交给 Observer，最后终止进程。正常关闭退出 0，参数、窗口原因或严格字体错误退出 1。Observer 必须返回，不能更改退出码。

公开 `ProcessOptions` 只有 `Theme`、`Lazy`、`Observer`。Theme 与 Lazy 同时给定在开窗前拒绝；二者均空时使用同步默认字体来源；仅给 Lazy 时保留临时主题与异步加载。公开 `ExitResult` 提供 Code、Err、DestroyErr、FontErr、CallbackDone、CleanupDone；窗口原因和字体原因并存时 `errors.Join`，两条均可通过 `errors.Is` 判断。

事件源、错误注入、退出函数和步骤字符串均为包内测试接缝。真实 console 测试由包内测试二进制子进程执行，仍验证四个场景的真实退出码、marker 和内部顺序。没有为外部调用方暴露自检开关。

旧 Run、RunWith、App.Run、RunWindow 等入口不调用这个新入口，不隐式终止调用者进程。Windows Gio Main 不返回的边界仍在；多窗口、跨平台退出和原生初始化失败不由该单窗范围自动保证。BUG-028 的显式单窗替代路径已验，原卡保留其未解决的旧入口/跨平台边界。
