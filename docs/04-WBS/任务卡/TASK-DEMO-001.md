# TASK-DEMO-001：最小可运行窗口

## 元信息
- 状态：**Done**（2026-10-19）| 优先级：P0 | 预计：2h
- 依赖：TASK-ORCH-001、TASK-CODEGEN-001
- 契约：`docs/03-架构/运行时入口契约.md`

## 1. 目标

打通从 `.gxml` 到真实窗口的完整链路：管线生成源码 → 运行入口渲染 → 窗口显示。

## 2. 为什么单独立项

编排层（`TASK-ORCH-001`）只解决控制流，不碰窗口。`goui.App` 之类的运行入口
在仓库中**从未存在**，属于新架构决策，需要独立契约与验收。

## 3. 交付物

| 交付物 | 内容 |
|---|---|
| `docs/03-架构/运行时入口契约.md` | `View` 接口、`Config`、`Run`/`RunWith`/`RunWindow` 的职责与边界 |
| `pkg/runtime/app` | 窗口创建、事件循环、退出处理 |
| `examples/demo` | 走完整管线并打开真实窗口 |

## 4. 关键设计决定

### 4.1 为什么拆出 `RunWindow`

Gio 的 `app.Main()` 接管主线程且**没有退出机制**——只能靠关闭窗口结束。
若只有一种形态，自动化测试只能强杀进程，无法区分「正常运行」与「卡死」。

`RunWindow` 接受 `stop` channel，收到信号后关闭窗口并返回，使
「启动 N 毫秒不崩溃」成为可验证事实。**生产代码不得使用它**
（不调用 `app.Main()`，窗口可能不显示）。

### 4.2 `app.Main` 的可注入点

`RunWith` 内部的 `app.Main()` 会把控制权完全接管，测试无法调用。
因此抽出包级变量 `mainFunc = app.Main`，测试替换为立即返回的空函数，
从而覆盖其余逻辑。

这**不**验证主线程接管行为——那是 Gio 的职责，无法在单元测试中观察。

### 4.3 一次实现缺陷

`RunWindow` 初版在循环里 `select` 检查 `stop`，但 `w.Event()` 会阻塞到下一个
事件到来——无事件时循环永远回不到 `select`，导致无法退出（测试立刻暴露）。

修复：由独立 goroutine 监听 `stop`，收到后调用 `w.Perform(system.ActionClose)`
并 `Invalidate()`，让阻塞中的 `Event()` 返回。

## 5. 验收

```text
$ go test ./pkg/runtime/app/
ok  coverage: 93.8%   （6 个测试）

$ go run ./examples/demo
--- 生成 demo_gen.go ---
（打印完整生成源码，随后打开标题为 "Goxml Demo" 的窗口）
```

### 诚实的验证边界

| 能自动验证 | 不能自动验证 |
|---|---|
| 生成代码编译通过 | 窗口是否真的弹出 |
| `RunWindow` 启动不 panic、能干净退出 | 文字是否正确渲染 |
| 布局被实际调用（计数 View） | 布局结果是否符合预期 |

**物理窗口与渲染效果由人工本地确认**，本任务不声称已自动验证。

## 6. 平台范围

仅 **Windows amd64** 实测。Linux/macOS 需 CGO 与系统 `-dev` 包，未验证。

## 7. 遗留

- Codegen 仍只支持 `<Page>` + 单个静态 `<Text>`，demo 的界面因此极简。
- 未做 CLI：demo 内嵌 `.gxml` 源码，不读取磁盘文件、不写盘。
