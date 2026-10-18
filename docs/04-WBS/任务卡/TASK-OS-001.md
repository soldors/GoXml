# TASK-OS-001：实现剪贴板与异步原生文件弹窗

## 元信息
- 状态：Todo | 优先级：P1 | 预计：6h
- 分支：feature/TASK-OS-001
- 依赖：TASK-LIFE-001

## 1. 目标
提供剪贴板和原生文件弹窗接口；弹窗及系统调用不得阻塞 Gio 渲染线程，完成后通过回调和窗口失效通知交还结果。

## 1.1 规范依据
- 平台实现与 CGO 策略遵守 `docs/03-架构/平台与平台依赖矩阵.md`、`ADR-0007`。
- 线程和回调遵守 `docs/03-架构/运行时线程模型.md`。

## 1.2 CGO 策略（**已修订**）
- **Windows**：必须纯 Go（`x/sys/windows`），保持 `CGO_ENABLED=0`。
- **Linux / macOS**：允许使用 CGO 调用平台 API。**不再要求**全平台零 CGO——该要求已实测否决（ADR-0007）。
- 结论：剪贴板与弹窗的实现不再受"必须纯 Go"约束，但 Windows 侧仍必须纯 Go。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-OS-001-1 | 定义跨平台剪贴板 API 与各平台实现/降级行为 | go test -v ./pkg/platform/clipboard/... |
| TASK-OS-001-2 | 实现异步打开/保存文件弹窗及取消结果 | go test -v ./pkg/platform/dialog/... |
| TASK-OS-001-3 | 验证完成回调、错误传递与 UI Invalidate 时序 | CGO_ENABLED=1 go test -race ./pkg/platform/dialog/... |
| TASK-OS-001-4 | 验证 Windows 侧仍可在 `CGO_ENABLED=0` 下构建 | make build-windows |
