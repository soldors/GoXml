# ADR-0018：Lifecycle Owner 与 Router 页面生命周期

## 状态

**已批准（Accepted）**

- 日期：2026-11-10
- 决策基线：`474dfb2a047ab19bc56a6093b13c32f7a0bbd204`
- 范围：`pkg/runtime/lifecycle`、`pkg/runtime/router/lifecycle.go` 及宿主已有的 `app.WithOnDestroy`
- 默认策略：opt-in；未调用 `Router.EnableLifecycle` 时保持既有路由行为

## 背景

页面路由已经具备成功提交、失败回退、合并导航和关闭路径。Lifecycle 需要在这些路径上提供可观察且可重复的挂载、取消和卸载语义，同时不能把生命周期回调、页面 Context 或宿主窗口关闭隐式混成同一个所有权边界。R28-C 对 Owner、Router 和 App 的组合接线做了限定验证，本 ADR 将该合同固定为公共文档。

## 决策

### 1. Owner 与页面 Context

`lifecycle.Owner` 是 UI 所有者串行点上的根生命周期管理器。`NewHandle` 只创建句柄，不执行挂载回调；句柄状态依次为 `Created -> Mounted -> Unmounted`，过期句柄不能再次挂载。

`Owner.MountHandle` 只有在页面已被 Router 成功 publish 后才可调用。挂载顺序固定为：

1. Router 成功提交新页面。
2. 取消旧页面 Context。
3. 对旧页面执行一次 `OnUnmount`（若旧页已挂载）。
4. 创建/保留待定新页面 Context。
5. 在新页面第一次业务 `Layout` 之前执行一次 `OnMount`。

`OnMount`、`OnUnmount` 在 owner/UI 串行点执行，且不在 Owner 锁内执行。回调不得承担阻塞 I/O、窗口销毁或等待另一个 UI 事件的职责。

### 2. CancelOnly 与 Finalize

- `Owner.CancelOnly()` 只取消 owner 根 Context 和 owner 自身的后台工作；它不调用 `OnUnmount`，也不替宿主卸载页面。
- 当页面 Context 由 independent parent 挂载时，该 Context 不属于 owner 根的取消范围。该页面由 Router 的 `Shutdown`/`Finalize` 负责取消和卸载；调用方不能仅依赖 `CancelOnly()` 推断页面已清理。
- `Owner.Finalize()` 执行一次性 owner 清理、当前页面卸载和待处理项清空；重复调用必须幂等。
- `Router.LifecycleFinalize()` 只负责 Router 的 lifecycle bridge/Owner finalize，**不隐式调用 `Router.Shutdown()`**。若宿主还要关闭 Router 根 Context，必须显式调用 `Shutdown`，并根据宿主关闭顺序保证只发生一次。

### 3. Router opt-in 组合

`Router.EnableLifecycle` 必须在第一次导航前调用，且只能成功启用一次。`LifecyclePage` 是页面 View 与 `OnMount`/`OnUnmount` 合同的适配器；工厂可以构造页面，但构造成功不等于页面已挂载。

Router 只在成功 publish 且进入实际渲染前安排待定挂载。`renderCurrent` 在调用页面首个业务 `Layout` 前完成挂载，因此业务 Layout 观察到的页面已经处于 Mounted 状态。失败工厂、合并导航、被 close-winning 导航取代的 pending 页面均不挂载，也不触发其 `OnUnmount`。

成功切页的组合顺序仍以本 ADR 第 1 节为准；Router 负责保存 pending handle/context，并在页面切换或关闭路径中清理它们。后台 `Router.Shutdown` 仅执行 `CancelOnly` 和 pending 清理，不触发 `OnUnmount`。

### 4. App DestroyEvent 最小接线

宿主可以把已有的 `app.WithOnDestroy` 用作关闭事件入口，将 `Router.LifecycleFinalize` 作为一次性 finalize 回调：

```go
wrapped, err := app.WithOnDestroy(router.View(), router.LifecycleFinalize)
if err != nil {
    return err
}
_ = wrapped
```

该接线的约束是：DestroyEvent 回调最多触发一次，`LifecycleFinalize` 本身保持幂等；它不隐式代替 `Router.Shutdown`。若宿主合同要求同时取消 Router 根 Context，应由宿主显式编排 `Shutdown` 与 `LifecycleFinalize`，不可依赖回调副作用猜测顺序。

R28-C 已完成受控组合验证及 Windows/amd64 单窗原生关窗验证。Codex 经授权在真实窗口点击标题栏 X。`natural` 场景的 Router 根在 Finalize 前后均活跃。`shutdown-first` 场景在关窗前已取消、尚未卸载，且 `ready-to-close` 已落盘。两场景都进入生产 `DestroyEvent`，页面 Context 先于卸载取消，卸载恰好一次，Observer 在清理后运行。两次进程均实测退出码 0，无 DATA RACE 或残留进程。

原生运行来自 `19c86a1486506e9453c9cae79de74075712a0c26`，race 二进制 SHA-256 为 `31b71e3e6f389d696c9edcbb58a4661c8c0fa403c458e1f39bc2b154b888a771`。run ID 分别为 `native-natural-20261112-151925-a155bf8f` 和 `native-shutdown-first-20261112-151402-9163db46`。截至代码候选 `35b61b5`，Lifecycle fixture 及其依赖源码和模块输入未变，可沿用这两次运行的证据。

## 公共 API 与兼容边界

| 区域 | 公开面 | 约束 |
|---|---|---|
| `pkg/runtime/lifecycle` | `Owner`、`Context`、`Handle`、`MountHandle`、`Switch`、`CancelOnly`、`Finalize` | UI owner 串行点；默认不自动启用；取消与卸载职责分离 |
| `pkg/runtime/router` | `EnableLifecycle`、`LifecyclePage`、`LifecycleOwner`、`LifecyclePost`、`LifecycleFinalize` | 仅 opt-in；首次导航前启用；失败/合并/关闭胜出路径不挂载 |
| `pkg/runtime/app` | 已有 `WithOnDestroy` | 仅提供宿主关闭回调入口；不新增隐式 Shutdown 行为 |
| `contracts` | `LifecycleMount`、`LifecycleUnmount` | `OnMount(context.Context)` 与 `OnUnmount()` 回调合同保持不变 |

未启用 Lifecycle 的现有 Router 调用方不需要迁移；不得为兼容旧调用方而默认创建 Owner、页面 Context 或生命周期 goroutine。

## 错误与关闭路径

- `EnableLifecycle` 在首次导航后或重复调用时返回既有错误，不改变已存在的 Router 状态。
- 工厂错误、导航合并、pending 页面被关闭路径取代时，旧的已提交页面保持可见且其生命周期不被错误清理；未成功 publish 的新页面不挂载。
- `DestroyEvent` 的 finalize 若重复到达，必须由 bridge 的 finalized 标记和 Owner 幂等清理吸收；不得重复 `OnUnmount`。
- 后台关闭只允许执行取消路径；需要 UI 卸载时，必须回到 Router/UI owner 的显式 Finalize/切页路径。

## 线程与宿主责任

后台调用方可以请求 Navigate、Dispatch、Invalidate 或 Shutdown；实际生命周期回调仍在 UI owner 串行点发生。宿主必须保证回调不阻塞主循环，并负责将 native 关闭事件转换为一次性的 finalize 调用。Lifecycle 不负责杀死 goroutine，也不把后台取消误报为页面卸载完成。

## 验证记录

决策基线 `474dfb2a047ab19bc56a6093b13c32f7a0bbd204` 已包含 R28-C Lifecycle/Router/App 组合收口。基线记录的门禁包括：lifecycle/router/app 测试套件 `230 PASS / 0 FAIL / 1 existing SKIP`、MinGW race、`CGO_ENABLED=0` Windows 构建、`go vet`、`gofmt`、docgate、diagdoc，以及 lifecycle/router/app 覆盖率约 `93.8% / 95% / 92%`。本次 ADR 仅修改文档，不改变生产代码或默认行为；提交前再次运行的命令与输出记录在对应 C closeout dev-log 中。

## 未完成项与发布判断

Owner/Router opt-in 合同为 **Accepted**。受控组合和上述 Windows 单窗两场景已验证。剩余验证包括：

1. Linux/macOS 原生关闭和窗口进程 race。
2. 多窗口关闭、DPI/IME 专项和整应用性能。
3. [TASK-LIFE-001-3](../../04-WBS/任务卡/TASK-LIFE-001.md) 中静态资产发现、嵌入声明与生成引用已由已发布的 Image v1 覆盖；通用资产 API、额外格式及跨包复制等扩展仍待裁决，不在本 ADR 的批准范围。

本 ADR 的 Windows 原生结论限于上述两条单窗关闭路径。发布状态以协调者的发布记录及 Git 引用为准。
