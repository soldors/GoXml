# ADR-0007：Zero-CGO 降级为 Windows 侧约束，Linux/macOS 接受 CGO

- 状态：已接受（取代 ADR-0005 的全局 Zero-CGO 要求）
- 决策日期：2026-10-18
- 关联 ADR：ADR-0001、ADR-0005（被本 ADR 部分取代）
- 关联文档：[平台与平台依赖矩阵](../平台与平台依赖矩阵.md)、[环境搭建](../../05-开发/环境搭建.md)、[测试与质量门禁规范](../../06-测试/测试与质量门禁规范.md)
- 决策依据：真机实测（见 §3）

## 1. 背景

ADR-0005 规定「严禁 CGO，三端均在 `CGO_ENABLED=0` 下编译，保全 Go 一键交叉编译」。该规定在文档阶段成立，但从未在真机验证。

本次在 Windows 与真实 Linux（WSL2 Debian 13）上做了完整实测，结论是：**该规定对 Gio 依赖链而言，在 Linux 和 macOS 上物理不可实现。** 因此必须修订。

## 2. 决策

**Zero-CGO 从「全平台强制红线」降级为「Windows 侧保持」：**

| 平台 | CGO | 依据 | 产物形态 |
|---|---|---|---|
| **Windows** | **`CGO_ENABLED=0`（保持 Zero-CGO）** | Gio 的 Windows 后端是纯 Go（`x/sys/windows` + 手写 D3D11 绑定） | 单文件静态二进制 |
| **Linux** | **接受 `CGO_ENABLED=1`** | Gio 的 X11/Wayland/EGL/Vulkan 后端依赖 CGO | 动态链接，需 10 个系统库 |
| **macOS** | **接受 `CGO_ENABLED=1`** | Gio 的 Cocoa/Metal 后端依赖 CGO | 动态链接系统框架，需在 macOS 构建 |

同时明确两条工程后果：

1. **放弃「一键三端交叉编译」**，改为各平台原生构建（CI 使用对应 Runner）。
2. **Windows 侧的 Zero-CGO 价值保留**：仍可获得单文件静态产物与免安装依赖。

## 3. 实测证据

环境：Windows 11（i7-12700H）+ WSL2 Debian GNU/Linux 13 (trixie)，Go 1.24.6，Gio v0.10.3。

### 3.1 Linux `CGO_ENABLED=0` 穷举失败

| build tag 组合 | 结果 |
|---|---|
| （无） | ❌ |
| `nox11` | ❌ |
| `nowayland` | ❌ |
| `noopengl` | ❌ |
| `nox11,nowayland` | ❌ |
| `nox11,nowayland,noopengl` | ❌ |

错误恒定：

```text
imports gioui.org/internal/vk: build constraints exclude all Go files
```

根因：`internal/vk/vulkan.go` 为 `//go:build linux || freebsd` 且 `import "C"`；`internal/gl/gl_unix.go`、`internal/egl/egl_unix.go`、`app/os_x11.go`、`app/os_wayland.go` 同样依赖 CGO 与系统头文件。

### 3.2 Linux `CGO_ENABLED=1` 成功

安装 `build-essential` 与 X11/EGL/xkb 开发库后：

```text
CGO_ENABLED=1 go build → exit 0，产出 11MB 二进制
```

### 3.3 产物依赖（`readelf -d`）

CGO 产物为动态链接，`NEEDED` 列表（X11 与 Wayland 两套并存）：

```text
libEGL.so.1            libwayland-egl.so.1     libwayland-client.so.0
libwayland-cursor.so.0 libxkbcommon-x11.so.0   libxkbcommon.so.0
libX11-xcb.so.1        libX11.so.6             libXcursor.so.1
libXfixes.so.3
INTERP: /lib64/ld-linux-x86-64.so.2
```

**这是放弃 Linux Zero-CGO 的直接代价：终端用户必须预装上述系统库，否则启动即报 `cannot open shared object file`。**

### 3.4 Windows 保持 Zero-CGO 的依据

- `app/os_windows.go` 不含 `import "C"`，依赖 `golang.org/x/sys/windows`。
- Windows 目标下实际编译的 Gio 包（`app/internal/windows`、`internal/d3d11` 等）均可 `CGO_ENABLED=0` 构建。
- 实测 `CGO_ENABLED=0 GOOS=windows` 构建成功。

### 3.5 跨平台交叉编译能力实测（纯 Go 依赖时）

在同一台 Windows 机器上，无 gcc/clang：

```text
windows/amd64  ✅  windows/arm64 ✅  linux/amd64 ✅
linux/arm64    ✅  darwin/amd64  ✅  darwin/arm64 ✅
6 个平台总耗时 19.9 秒
```

该能力在纯 Go 依赖下成立，但**对 Gio 的 Linux/macOS 后端不成立**——这正是本 ADR 要澄清的边界。

## 4. 替代方案与否决理由

| 方案 | 否决理由 |
|---|---|
| 维持三端 `CGO_ENABLED=0` | 实测物理不可实现（§3.1），该红线无法被满足，只会导致文档与现实脱节 |
| 自建纯 Go 平台后端（X11/Wayland/Cocoa + EGL/Metal 绑定） | 需重写约 6,400 行以上平台相关代码且需 macOS 真机调试；成本 3–6 人月，超出 v1 范围。可作为远期选项，但不能阻塞 v1 |
| 锁定 Windows 单平台 | 放弃跨平台，与产品定位冲突；且实测 Linux CGO 路径完全可用，没有必要放弃 |
| 换用其他 GUI 底座（Avalonia/Fyne 等） | 推翻 ADR-0001；且 Avalonia 同样采用「每平台原生后端 + 随包分发原生库」模式，结论一致，无收益 |

**行业印证**：Avalonia 采用同一模式——C# 业务层 + 每平台原生窗口后端 + 随包分发的 `libSkiaSharp`/`libAvaloniaNative` 等原生库，并明确要求 Linux 安装 `libX11` 等系统包。「一套代码 + 每平台原生后端」是跨平台 GUI 框架的主流做法。

## 5. 影响范围

| 文档 | 影响 |
|---|---|
| `AGENT.md` 红线 1 | 由「绝对 Zero-CGO 铁律」改为「Windows 侧 Zero-CGO；Linux/macOS 允许平台工具链」 |
| `AGENT.md` 技术栈 | 删除纯 Go `purego` 桥接的表述（与 Gio 实际依赖不符） |
| `Zero-CGO平台兼容性矩阵.md` | 重写为 `平台与平台依赖矩阵.md`，按平台区分 CGO 与系统依赖 |
| `Makefile` | `test-cross-compile` 拆分为 `build-windows`（零 CGO）与各平台原生构建 |
| `测试与质量门禁规范.md` | 编译矩阵按平台区分 CGO 开关；不再要求三端 `CGO_ENABLED=0` |
| `环境搭建.md` | 补充 Linux 13 个系统包与 macOS 前置条件 |
| `发布单.md` | 产物表增加「原生依赖清单」列 |
| `TASK-OS-001` / `TASK-OS-002` | 平台实现不再强制纯 Go；Windows 侧仍要求 |

## 6. 不变量（仍然强制）

降级不等于放任。以下约束继续有效：

1. **Windows 侧必须保持 `CGO_ENABLED=0`**，且不得引入任何需要 CGO 的第三方库。
2. **禁止引入会破坏当前平台构建的额外 GUI 框架**（如以 WebView 为主的壳）。
3. `pkg/runtime` 与 `pkg/platform` 仍不得反向依赖 `internal/compiler`。
4. 平台能力不可用时必须返回明确错误，不得假成功、不得阻塞渲染线程。
5. 任何新增 CGO 依赖必须在本 ADR 的平台矩阵中登记，并说明其系统依赖。

## 7. 版本演进边界

以下变更必须新增 ADR：

- 恢复某平台的 Zero-CGO 要求；
- 引入新的原生依赖（新增 `.so`/`.dylib`/`.dll` 或系统包）；
- 改变各平台的 CGO 开关；
- 启动自建纯 Go 平台后端的工作。
