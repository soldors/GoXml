# 交接包：TASK-STATE-IMPLEMENT-001

## 1. 任务状态

- 状态：**进行中，未验收** | 优先级：P0
- 目标：完成 S2 最小状态通知机制，统一绑定页面 View 接口并为 S3 真实窗口验收准备入口
- 依赖：`TASK-STATE-001`、`状态更新契约.md`、`ADR-0010`
- 当前工作区：存在未提交的 Codegen、测试迁移和任务卡修改；不要把工作区状态当作已发布基线

## 2. 必读依据

1. [状态更新契约.md](../../03-架构/状态更新契约.md)
2. [代码生成输出契约.md](../../03-架构/代码生成输出契约.md) §4.3
3. [运行时入口契约.md](../../03-架构/运行时入口契约.md)
4. [ADR-0010](../../03-架构/adr/ADR-0010-统一View接口与ViewModel引用持有.md)
5. [TASK-STATE-IMPLEMENT-001.md](../../04-WBS/任务卡/TASK-STATE-IMPLEMENT-001.md)

## 3. 冻结目标

- 静态页面继续使用 `NewXxxView()`，实现 `Layout(gtx, th)`。
- 绑定页面使用 `NewXxxView(vm *T) (*XxxView, error)`，持有调用方的 ViewModel 引用。
- 绑定页面同样实现 `Layout(gtx, th)`，不再把 ViewModel 作为 Layout 参数。
- nil ViewModel 在构造期显式失败。
- Runtime 只封装 Gio `w.Invalidate()` 请求重布局，不修改 ViewModel、不引入事件总线。
- 第一版只承诺单窗口整窗失效；AOT Diff、Deps 和细粒度失效归 S5 评估。

## 4. 当前实现边界

已同步的文档：

- 输出契约 §4.3 已改为统一两参 Layout 和绑定构造持有 ViewModel；
- 运行时入口契约已改为引用统一接口；
- WBS、任务卡、验收用例、测试计划和文档索引已登记 S2。

尚未完成：

- 生成代码结构和导入必须包含并正确使用 ViewModel 字段；
- 现有绑定运行时与 B3 临时探测必须迁移到新构造 API；
- nil 构造失败测试；
- Runtime Invalidate 入口；
- 真实 `app.Window` 的状态更新、FrameEvent 和新值显示证据。

## 5. 当前失败证据

命令：

```text
go test -count=1 ./internal/compiler/codegen
```

当前失败集中在：

- 临时探测仍调用旧三参 `v.Layout(gtx, th, vm)`；
- 临时探测仍引用旧的 `vm` 局部变量；
- B3 显示缓存、绑定运行时读取和稳定帧分配测试因此不能作为 S2 证据；
- 真实窗口端到端测试尚未执行。

`go test -count=1 ./internal/docgate`、Markdown 链接扫描和 `git diff --check` 已通过。

## 6. 接手顺序

1. 完成 Codegen 生成结构的 `vm` 字段、`fmt` 导入和构造返回值；
2. 迁移 `binding_runtime_test.go` 与 `binding_cache_test.go` 的临时探测，先恢复编译；
3. 增加静态页面、绑定页面和 nil 构造的 API 断言；
4. 实现 Runtime 的 Invalidate 封装并补单元测试；
5. 在可用宿主环境执行真实 `app.Window` 集成测试；
6. 更新本交接包、TASK 卡和验收报告，只有全部退出条件满足后才将 S2 标为完成。

## 7. 不得作出的结论

- 不得把 `CallOp` 回放或 `allocs/op == 0` 当作状态自动刷新证据；
- 不得把 headless 布局测试当作真实窗口验收；
- 不得在 S2 未通过前关闭 `TASK-STATE-001`；
- 不得提前恢复 AOT Diff、Deps 或多窗口订阅范围。
