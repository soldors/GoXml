# R28-C Lifecycle 原生关窗 fixture（**只编译，默认不执行**）

## 0. 本轮状态（必须先读）

- 本目录在 `pkg/runtime/app/testdata/` 下，Go 的 `./...` 默认忽略；`main.go`
  还带 `//go:build nativewindow`，显式构建也必须给该标签。
- **本轮只编译了普通与 race 二进制，没有执行任何一个**。README 约束：
  「不操作桌面、不执行原生窗口程序」「编译原生 fixture 不等于执行」。
- 因此本文件下面的「自然关闭步骤」「证据字段」都是**给授权执行者的准备**，
  不是已有的原生验收结论。来自原生窗口的真实 `DestroyEvent`、真实 native
  race、跨平台运行仍是**验收结论：未验证**。

## 1. 它证明什么 / 不证明什么

| 层 | 结论 |
|---|---|
| 公开接线 | 只用公开 `app.WithOnDestroy` + `app.RunSingleWindowProcess` + `app.ProcessOptions{Observer}` + `app.ExitResult`，组合真实 `router.Router`（`EnableLifecycle` + `LifecyclePage`）。**未**新增公开 API，**未**调用 `NotifyOwnerDestroyed` |
| 真实关闭路径 | 生产 `RunSingleWindowProcess` 在 `opts.next == nil` 时取 `next = a.w.Event`（`process_entry.go`）；公开 `ProcessOptions` 没有事件/错误/退出注入字段。销毁事件只能来自 Gio 真实窗口事件泵。**fixture 不手工触发回调、不合成 `gioapp.DestroyEvent`** |
| 受控证据 | 现有 `pkg/runtime/app/controlled_window_test.go`（`!nativewindow`）、`hy4_combination_test.go`、`closeout_destroy_test.go` 的 **两条生产循环守卫**（`loopEvents` / `runWindowEvents`）原样保留，属受控事件回归，**不得**冒充原生证据 |
| 不证明 | 窗口真的显示、Gio 真的投递 `DestroyEvent`、回调线程归属、真实 native race、进程/句柄收口、跨平台运行 |

## 2. 自然标题栏关闭步骤（授权后由人工执行，本轮未执行）

1. 取得桌面控制/原生窗口的**明确新授权**（旧轮次授权不可沿用）；确认公共锁
   `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock` 空闲，在专用验收树操作。
2. 为本轮**独占新建**证据目录（已存在即拒绝，旧 PASS 不可复用）；记录
   普通/race 二进制的 SHA-256 与构建 argv/env/rc。
3. 以 `natural` 场景启动已编译二进制：`lifecyclenative <new-dir> natural`。
   记录父进程观察到的 `runner_observed_rc`。窗口标题含 PID，便于确认「关的是自己这一个窗口」。
4. 让窗口稳定渲染若干帧（可缩放/移动窗口触发真实 FrameEvent），**不做任何
   鼠标/键盘注入、不向别的进程发消息、不强制终止**。
5. 用**自然方式点击该窗口标题栏的 X** 关闭（不是代码内 `WM_CLOSE`、不是
   `system.ActionClose`、不是注入事件）。这是「真实关闭路径」的唯一来源。
6. 进程退出后读取 `<dir>/marker.txt` 与 `result.json`；确认 `runner_observed_rc`
   与 `process.code` 一致（正常关窗均为 0）；确认没有残留进程/窗口、
   `activeAfter=0`、临时目录归属清晰。
7. 再以 `shutdown-first` 场景重复 3–6：两帧后后台 `Router.Shutdown()` 应只
   取消（`shutdown_unmounts=0`、`shutdown_cleanup_done=false`），随后人工关窗
   时 UI owner 的 `LifecycleFinalize` 才产生**恰好一次** `OnUnmount`。
8. 概率性判据重复 N 次逐次留痕；授权范围内再跑真实 native `-race` 二进制。
   `-tags nativewindow` 集合与默认受控集合**分开记录**，不得互相冒充。

> 步骤 3–8 全程需 `scripts/record-command.py run --lock-mode acquire` 留痕；
> 原始 stdout/stderr 字节、argv、env、rc、timeout、进程 containment 都落盘。

## 3. 原始证据字段（`result.json` / `marker.txt`）

| 字段 | 由谁写 | 判据 |
|---|---|---|
| `steps` | fixture（UI owner 真实顺序） | `construct:home → mount:home → layout:home → callback → finalize → unmount:home`；Mount 必须早于首次业务 Layout |
| `mount_before_first_layout` | fixture | **首次业务 Layout 前 Mount**：第 1 次 Layout 时 `mounts==1` |
| `layouts` / `mounts` / `unmounts` | fixture | `OnUnmount` **恰一次**；`mounts==1` |
| `page_context_err_at_unmount` / `page_context_canceled_before_unmount` | fixture | **卸载前页面 Context 已取消**：`errors.Is(pageCtx.Err(), context.Canceled)` |
| `event_source` | fixture（静态事实） | 生产 `app.Window.Event`，无注入 |
| `real_close_path.{destroy_reached,destroy_err,window_destroy_error,callback_done,cleanup_done}` | fixture + `ExitResult` | **真实关闭路径**：正常人工关窗 `destroy_err=""`、`callback_done=true`、`cleanup_done=true` |
| `observer_after_cleanup` | `ExitResult.CleanupDone` | **cleanup 后 observer**：Observer 在事件循环 cleanup 之后、`os.Exit` 之前 |
| `process.{code,err,destroy_err,font_err,callback_done,cleanup_done}` | `ExitResult` | 正常关窗 `code=0`；窗口错误与字体错误经 `errors.Join` 分别可判 |
| `shutdown_observed` / `shutdown_unmounts` / `shutdown_cleanup_done` | 后台 goroutine | **后台 Shutdown 只取消**：`shutdown_unmounts=0`、`shutdown_cleanup_done=false` |
| `callback` → `observer` marker 顺序 | `marker.txt` | 显式回调先于 observer，observer 先于进程退出 |
| `runner_observed_rc` | **授权执行者** | 父进程实测 rc，与 `process.code` 一致 |
| `no_residual_process` | **授权执行者** | **无残留**：进程树、窗口句柄、临时目录收口；`activeAfter=0` |
| `natural_titlebar_close_steps_performed` | **授权执行者** | §2 步骤逐条真实完成，非代码内关闭 |
| `plain_binary_sha256` / `race_binary_sha256` | 本轮构建（见交付 `raw/fixture/binary-identity.json`） | 二进制身份可复核 |
| `native_race_result` | **授权执行者** | 真实 native `-race` 结果（本轮未跑） |
| `cross_platform_run_result` | **授权执行者** | Linux/macOS 运行（本轮未跑） |

`runner_fields_required` 在 `result.json` 里显式列出只能由执行者填写的字段，
fixture 编译成功**不填**这些字段，避免把「编译过」写成「跑过」。

## 4. 后台取消 vs UI owner 清理（两个场景的区别）

| 场景 | 后台 `Router.Shutdown()` | UI owner `LifecycleFinalize` |
|---|---|---|
| `natural` | 不调用 | 真实 `DestroyEvent` 的 `WithOnDestroy` 回调里调用一次 → `Owner.Finalize` → `unmountCurrent` 一次 `OnUnmount` |
| `shutdown-first` | 两帧后调用：`CancelOnly` 只取消 owner 根/拒绝排队，**不执行 `OnUnmount`**、`CleanupDone=false` | 随后人工关窗时调用一次，补上**恰好一次**清理 |

`Owner.CancelOnly` 的源码注释已按事实修正：函数只**直接**取消 owner 根，
不承诺取消独立 parent 的页面 Context（见 `pkg/runtime/lifecycle/lifecycle.go`）。
修正只改注释，**不改行为**。

## 5. 跨平台运行缺口

- 本轮只对 Windows/amd64 做了**编译**（普通 CGO=0 与 MinGW race CGO=1）。
  Linux/macOS 的交叉编译尝试（若执行）只证明编译，**不等于运行**；结果如实
  记在交付 `raw/fixture/` 台账里。
- AGENT.md / `平台与平台依赖矩阵.md`：Windows 可 `CGO_ENABLED=0`；
  Linux（X11/Wayland/EGL/Vulkan）与 macOS（Cocoa/Metal）**必须 CGO=1** 且需
  系统 `-dev`/运行库，Gio 使用 `#cgo` 不是 purego。不得据此声称三端零 CGO。
- 真实窗口销毁事件、DPI/IME、多窗口、原生退出与进程收口、native race、
  人工视觉验收、跨平台实机运行均**未验证**；旧轮次授权不自动扩展。

## 6. 构建（本轮已做，未执行）

```
CGO_ENABLED=0 go build -tags nativewindow -o <delivery>/raw/fixture/lifecyclenative.exe ./pkg/runtime/app/testdata/lifecyclenative
CGO_ENABLED=1 CC=<mingw64 gcc> go build -race -tags nativewindow -o <delivery>/raw/fixture/lifecyclenative-race.exe ./pkg/runtime/app/testdata/lifecyclenative
```

两个二进制的 SHA-256、大小、构建 argv/env/rc 见交付
`raw/fixture/binary-identity.json` 与 `commands.jsonl`。
