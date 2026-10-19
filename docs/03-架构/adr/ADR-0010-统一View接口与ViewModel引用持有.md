# ADR-0010：统一 View 接口与 View 持有 ViewModel 引用

- 状态：接受
- 日期：2026-10-19
- 关联：`状态更新契约.md`（S1 冻结）、`状态更新机制现状核查.md`（S0）、`TASK-STATE-001`

---

## 1. 背景

绑定链路（Facts → 生成 → 编译 → 运行时读取 → 显示缓存）已闭环（`76dfedf`），
但绑定视图生成的签名是 `Layout(gtx, th, vm)`，与 App 的 `View` 接口
`Layout(gtx, th)` **不兼容**——绑定视图无法接入真实窗口。

S0 核查同时确认：

- `w.Invalidate()` 并发安全，语义正对状态变更场景；
- 事件循环是被动循环，没有「状态变化 → 重新布局」路径；
- ViewModel 所有权未定义；
- 多窗口/订阅机制当前不存在，现有 API 均单窗口。

## 2. 决策

1. **统一 View 接口不变**：`Layout(gtx, th)`。
2. **绑定视图持有 vm 引用**：构造时接收，`NewXxxView(vm *T)`；
   vm 由调用方创建与管理，View 只保存引用，不拥有不销毁；
   第一版不支持隐式替换。
3. **Runtime 提供窗口失效入口**，封装 `w.Invalidate()`：
   只请求重新布局，不修改状态。
4. **第一版整窗失效**，单窗口范围。
5. **nil vm 明确拒绝**：调用方配置错误，不静默降级。
6. **AOT Diff 不引入**：降为 S5 评估项。

## 3. 理由

### 3.1 为什么接口不变（否决 B/C）

绑定只是页面读取状态的一种**能力**，不是对框架接口的重定义。

- 方案 B（适配器）把生命周期复杂度转嫁给调用方；
- 方案 C（扩展接口）让所有静态页面跟随变化，还要解决不同页面 vm 类型不同
  的泛型难题——传播面大，收益小。

静态页面已经统一在 `Layout(gtx, th)` 上，App、示例、测试都依赖该形态。

### 3.2 为什么持有引用而非所有权

- ViewModel 常由应用层持有（如全局状态、数据库-backed 模型）；
- View 的生命周期（页面替换）与 vm 的生命周期**不同步**；
- 所有权归调用方，View 重建（页面切换）不破坏状态。

### 3.3 为什么第一版整窗失效

- S0 实测：`Invalidate` 机制成立，整窗失效**够用**且**最短**；
- 细粒度失效需要 op 级配合，复杂度大；
- 绑定缓存已按「显示串 + 上下文」判定复用，**命中时整窗重布局的成本
  已经很低**（稳定帧 140 ns/op + 回放）；
- 细粒度失效等有性能证据再评估（S5），避免过早引入复杂机制。

### 3.4 为什么 nil vm 明确拒绝

- 静默降级（空串/忽略）会掩盖调用方配置错误，产生难定位的深层 panic；
- 构造期拒绝把错误暴露在最早时机；
- 静态页面构造不受影响——不为 nil 校验复杂化大多数路径。

### 3.5 为什么暂不引入 AOT Diff

- 绑定缓存已按显示串判定复用，`0 allocs/op` 已达成；
- AOT Diff 解决的是「哪些节点变了」——在整窗失效 + 显示串缓存下，
  该问题不存在；
- 若 S5 实测整窗失效成为瓶颈，再评估，且须以测量为准。

## 4. 后果

### 正面

- 接口稳定：静态/绑定页面统一 `Layout(gtx, th)`；
- 最短路径：不改 Gio 交互，直接复用已验证的 Invalidate 机制；
- 状态生命周期清晰：调用方管理 vm，View 引用持有。

### 代价（接受）

- 绑定页面构造签名变化（`NewXxxView(vm)`）；
- 生成侧需把三参 `Layout` 改回两参（S2）；
- 整窗失效的效率依赖绑定缓存命中率——若未来实测不足，S5 再评估细粒度。

### 中立

- vm 数据竞争责任归调用方（契约明示，框架不自动同步）。

## 5. 验证状态

| 项 | 状态 |
|---|---|
| Invalidate 并发安全 | ✅ 源码证据（S0）+ 并发测试（8×100 无 panic，S3） |
| Event 唤醒 | ✅ 源码证据（S0）+ 真实窗口端到端（S2/S3） |
| 持有引用的构造形态 | ✅ `TestGeneratedConstructorRejectsNilVM` 对真实生成代码断言（S3，B0-F10 闭环） |
| `RunWindowWithInvalidator` 真实窗口刷新 | ✅ S2 受测场景通过 |
| **生产句柄形态 `NewApp`/`Run`/`Invalidate`** | ✅ S3 实现；`TestAppRunDrivesAndInvalidateRefreshes` 真实窗口验证 AAAA→WWWWWWWW；动态 demo（ticker 计数）冒烟存活 |
| **首帧前 Invalidate 语义** | ✅ 实测：安全静默跳过（mayInvalidate 零值 false + 真实窗口实测双重证据），无需就绪门 |
| 整窗失效性能充分性 | ⬜ S4 测量决定 |

## 6. S3 实现补充（2026-10-19）

生产 API 采纳**持久句柄形态**：`NewApp(view, th, cfg) (*App, error)` +
`(*App).Run() error` + `(*App).Invalidate()`。

选择理由：

- `app.Main()` 永不返回，任何「从返回值拿句柄」的方案不成立；
- 回调形态（`RunWith(..., onReady func(Invalidator))`）会迫使每个使用者
  为取句柄搭一套通道桥接；句柄形态在阻塞前就绪，后台 goroutine 直接使用；
- 与 Gio 自身「先 `new(app.Window)` 后驱动」的惯例同构。

`Run`/`RunWith` 保留为 `NewApp`+`Run` 的薄包装，一行式场景零破坏；
`RunWindow`/`RunWindowWithInvalidator`（测试集成形态）不变。

动态 demo（`examples/demo`）：.gxml 声明 `ViewModel="demoVM"`（同包形式）+
绑定 `{Title}`，管线真实产出绑定代码，后台 ticker 每秒更新并 Invalidate。
冒烟验证进程存活 8 秒（ticker 与主循环共存）。

**发现的接口语义澄清**：`SourceDir` 是同包 ViewModel 的解析目录。
demo 最初误设为仓库根导致 `GXML-ROOT-002`，修正为 demo 目录。
