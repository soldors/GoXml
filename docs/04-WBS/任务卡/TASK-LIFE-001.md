# TASK-LIFE-001：实现组件生命周期与静态资产管线

## 元信息
- 状态：R28-C opt-in Owner/Router 页面生命周期已批准；整卡仍为部分完成（静态资源管线与原生验收待完成） | 优先级：P1 | 预计：4h
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

## 2.1 R28-C Lifecycle Owner/Router 验收边界

- 已批准范围为 opt-in Owner/Router 页面生命周期，不改变默认关闭策略。
- 通过组合验证的时序为：成功 publish 当前页后，先取消旧页，再对旧页执行一次 `OnUnmount`，随后在首个业务 `Layout` 前挂载待定新页。
- 失败、合并导航与 close-winning 路径不挂载页面；后台 `Router.Shutdown` 仅执行 `CancelOnly`。
- `Owner.CancelOnly()` 只取消 owner 根。若页面 Context 使用 independent parent，由 Router 的 `Shutdown`/`Finalize` 负责该页面的取消与卸载；`Finalize` 不隐式调用 `Shutdown`。
- TASK-LIFE-001-1/2 的生命周期与 Router 组合门禁已在 R28-C 基线验证；TASK-LIFE-001-3 静态资源管线仍为 Todo。
- 原生 Gio `DestroyEvent` 与 native race 尚未验收，不能将本卡标记为发布完成；发布级状态等待原生接受。
