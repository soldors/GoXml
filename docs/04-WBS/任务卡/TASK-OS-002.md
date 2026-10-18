# TASK-OS-002：实现系统托盘与系统菜单栏

## 元信息
- 状态：Todo | 优先级：P1 | 预计：8h
- 分支：feature/TASK-OS-002
- 依赖：TASK-OS-001

## 1. 目标
为 Windows、Linux、macOS 提供统一的托盘和系统菜单能力，按平台采用对应系统接口实现，并在能力不可用时明确降级。

## 1.1 规范依据
- 平台支持边界、CGO 开关与降级行为遵守 `docs/03-架构/平台与平台依赖矩阵.md`、`ADR-0007`。

## 1.2 CGO 策略（**已修订**）
- **Windows**：纯 Go 实现（Shell API），保持 `CGO_ENABLED=0`。
- **Linux**：D-Bus StatusNotifier 等，优先纯 Go（`godbus/dbus`）；如引入 CGO 需登记到平台矩阵。
- **macOS**：允许 CGO / purego 绑定 NSStatusItem。
- **原验收命令 `make test-cross-compile` 已废弃**，改为分平台构建。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-OS-002-1 | 定义平台无关的托盘、菜单与事件接口 | go test -v ./pkg/platform/... |
| TASK-OS-002-2 | 实现平台后端、资源释放及不支持场景处理 | go test -v ./pkg/platform/... |
| TASK-OS-002-3 | 验证各平台构建（Windows 零 CGO，Linux/macOS 开 CGO） | make build-windows && make build-linux |
| TASK-OS-002-4 | 新增系统依赖时更新平台矩阵并加依赖断言测试 | go test -v -run TestPlatformDeps ./pkg/platform/... |
