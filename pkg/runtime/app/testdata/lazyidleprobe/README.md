# 原生空闲字体唤醒观察工具

独立 opt-in 工具，协议 `lazy-idle-window-v1`。固定四个 case：

- `bound-first`：NewLazyApp 先绑定通知，再 Request。
- `early-no-notify`：提前 Request，首 Layout 仍 Loading，无旧 notify。
- `early-prior-notify`：提前 Request，保留一个不唤窗的旧 notify。
- `early-worker-failure`：提前 Request 后受控读源与嵌入链失败，后续 Layout 保持临时字体。

本工具只用于机制观察，不计算 M1 性能收益。后台读取用 channel 阻塞；首 Layout 后至少观察250ms静默才放行。观察期间不主动 Invalidate、不做动画、不手动 Take。所有快照由互斥保护，App.Run 留在主 goroutine，单 supervisor 决定输出/退出。成功出口优先保留已就绪 Run 错误；参数错误2、运行或输出错误1、超时3。

## 无窗口检查

```powershell
$env:CGO_ENABLED='0'
go test -count=1 ./pkg/runtime/app/testdata/lazyidleprobe
go vet ./pkg/runtime/app/testdata/lazyidleprobe
go build -o <独立输出目录>/lazyidleprobe.exe ./pkg/runtime/app/testdata/lazyidleprobe
```

上述检查不执行原生探针入口。真实运行必须有另行明确的窗口预算和新输出目录；本夜原生idle窗口预算为0，不挪用M1固定24诊断。示例参数形状为 `-case=early-no-notify -font-source=<固定CJK字体路径> -timeout=20s`，不作为本轮执行授权。

## 解释边界

成功只证明所观察的真实 Layout/最终字体或失败事实；250ms静默不能排除任意操作系统重绘，也不能独立计数 App 内部私有 wake。程序通过有界观察后强制进程退出，不验收优雅关闭、屏幕呈现或关闭后 StrictFontFailure。`nativeWindowVerified` 的实际窗口结论只能来自未来真实运行，机制单测和合成fixture不能当原生成功。

2026-10-30 R1.1 经作者红绿与独立限定复审，四源码工作树/candidate/manifest SHA一致。当前只有无窗口证据；详见仓库docs的夜间准备与独立复审记录。
