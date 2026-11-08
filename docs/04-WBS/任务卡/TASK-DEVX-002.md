# TASK-DEVX-002：实现独立静态 UI 预览器

## 元信息
- 状态：InProgress（R26 Windows 基础范围已验；整卡剩余范围未闭环） | 优先级：P1 | 预计：5h
- 分支：feature/TASK-DEVX-002
- 依赖：TASK-STYLE-001

## 1. 目标
提供独立的 GXML 预览命令，用于快速查看布局和主题效果，不启动目标应用的完整开发热重载进程。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-DEVX-002-1 | 实现 `preview <file.gxml>` 参数解析和输入校验 | go test -v ./internal/devx/preview |
| TASK-DEVX-002-2 | 将解析、宏展开与预览渲染串联 | go test -v -run ^TestDevPreview ./internal/cli |
| TASK-DEVX-002-3 | 覆盖无效文件、诊断展示及预览退出清理 | go test -v ./internal/devx/preview |

## 2026-11-08 R26 接线复核

基础范围已由负责人批准，公共接线和本机原生观察完成；六包覆盖率均达85%，组件Layout稳态零分配。完整Frame、整应用性能与整卡排除项分别登记；准确合入/推送身份由协调者交付与Git引用确认。
现行范围和复跑命令见 [R26 接线与验收](../../05-开发/2026-11-08-R26-Windows接线与验收.md)。

已修正原表中的历史目录，`cmd/goxml/preview` 不是当前实现目录。现行核心验收为 `go test -v ./internal/devx/preview`，公共入口为 `go test -v -run ^TestDevPreview ./internal/cli`。初轮自动化仅使用 build-only；后续获授权执行真实显示与标题栏关闭，修复专用宿主退出后 rc0 且目录清理。与原生错误透传的生成宿主测试分列，不把构建成功当视觉通过。
