# TASK-LIFE-001：实现组件生命周期与静态资产管线

## 元信息
- 状态：Todo | 优先级：P1 | 预计：4h
- 分支：feature/TASK-LIFE-001
- 依赖：TASK-ROUTER-001

## 1. 目标
为页面和组件提供 Context 驱动的挂载/卸载生命周期，并将静态资源编译期嵌入生成应用。

## 1.1 规范依据
- 生命周期线程所有权和异步取消遵守 `docs/03-架构/运行时线程模型.md`。
- 状态提交和缓存边界遵守 `docs/03-架构/AOT状态Diff语义.md`。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-LIFE-001-1 | 实现挂载回调启动及 Context 取消传播 | go test -v -race ./pkg/runtime/lifecycle/... |
| TASK-LIFE-001-2 | 实现卸载清理并保证重复挂载/卸载行为确定 | go test -v -race ./pkg/runtime/lifecycle/... |
| TASK-LIFE-001-3 | 实现静态资源发现、嵌入声明和生成代码引用 | go test -v ./internal/compiler/codegen/... |
