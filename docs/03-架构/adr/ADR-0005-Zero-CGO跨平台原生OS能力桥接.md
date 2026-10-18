# ADR-0005：坚持 Zero-CGO 实现跨平台原生 OS 能力桥接

- 状态：**已被 ADR-0007 部分取代**
- 原冻结日期：2026-10-18
- 取代者：[ADR-0007：Zero-CGO 降级为 Windows 侧约束](ADR-0007-Zero-CGO降级为Windows侧约束.md)
- 原平台矩阵：~~docs/03-架构/Zero-CGO平台兼容性矩阵.md~~ → 现为 [平台与平台依赖矩阵](../平台与平台依赖矩阵.md)
- 线程约束：[运行时线程模型](../运行时线程模型.md)

## 原决策

严禁 CGO；Windows 走 Syscall，Linux 走 D-Bus Socket，macOS 走 Purego，保全 Go 一键交叉编译。平台能力不可用时必须明确降级，不得阻塞渲染线程。

## 为何被取代

该决策在文档阶段成立，但从未真机验证。2026-10-18 在 Windows 与 WSL2 Debian 13 上实测后确认：

1. **Linux 上 `CGO_ENABLED=0` 物理不可实现**：穷举 `nox11`、`nowayland`、`noopengl` 及其全部组合共 5 种 tag 组合，均失败于 `gioui.org/internal/vk: build constraints exclude all Go files`。
2. **macOS 同理**：`app/os_macos.go`、`app/metal_macos.go` 依赖 `#cgo` + Cocoa/Metal。
3. **"macOS 走 Purego" 与事实不符**：Gio 的 `go.mod` 中不含 `purego`，macOS 后端使用 `#cgo`。
4. **Linux `CGO_ENABLED=1` 一次构建成功**（11MB 产物），证明该路径可用。

因此"全平台 Zero-CGO"这条红线无法被遵守，继续保留只会让规范与实现脱节。

## 保留的部分

- **Windows 侧仍强制 `CGO_ENABLED=0`**（Gio 的 Windows 后端是纯 Go，已验证）。
- **平台能力不可用时必须明确降级、不得阻塞渲染线程**——该条完整保留。

## 历史说明

本 ADR 保留以记录决策演变，不应再作为实现依据。现行依据为 ADR-0007 与 [平台与平台依赖矩阵](../平台与平台依赖矩阵.md)。
