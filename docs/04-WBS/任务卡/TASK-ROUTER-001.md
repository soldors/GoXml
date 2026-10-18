# TASK-ROUTER-001：实现单窗口路由与转场动画

## 元信息
- 状态：Todo | 优先级：P1 | 预计：6h
- 分支：feature/TASK-ROUTER-001
- 依赖：TASK-STATE-001

## 1. 目标
在单个 Gio 窗口中按路由状态选择页面；转场时录制并复用旧页面快照，提供独立 OSWindow 逃生接口。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-ROUTER-001-1 | 实现路由注册、当前路径与页面分发 | go test -v ./pkg/runtime/router/... |
| TASK-ROUTER-001-2 | 实现旧页面 MacroOp 快照及转场生命周期 | go test -v -run TestTransition ./pkg/runtime/router/... |
| TASK-ROUTER-001-3 | 实现 OSWindow 逃生接口并覆盖切换、取消场景 | go test -v ./pkg/runtime/router/... |
