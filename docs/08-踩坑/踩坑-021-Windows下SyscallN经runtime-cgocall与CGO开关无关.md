# PIT-021：Windows下syscall.SyscallN经runtime.cgocall——CGO_ENABLED=0的CPU profile也会大量出现"cgocall"

## 现象

M1 启动段 CPU profile 的 top1 是 `runtime.cgocall`（两个真实样本分别占 59.3%/45.7%），而诊断 exe 以 `CGO_ENABLED=0` 构建（exe 内嵌 buildinfo 双处实证）——表面矛盾：纯 Go 构建为何 cgo 热点？曾被误列为"符号化伪影 vs 真实 cgo"的二选一。

## 根因

Go 1.18+ 在 Windows 上，`syscall.SyscallN` 经由 **runtime 自身的 cgocall 栈切换机制**发起 stdcall（runtime/internal/syscall → `runtime.cgocall`）。该机制与 `CGO_ENABLED` 开关无关——开关只控制**用户** cgo。Gio 窗口后端每次 Win32 调用（d3d11 建备/DefWindowProc/SwapChain.Present/SetWindowPos/CreateWindowEx…）都会产生这一帧。

解码 runtime 自记的 profile 输入（protobuf：gzip + varint + packed repeated；叶子 = `location_id[0]`）后与 pprof -top **逐 tick 一致**（54 ticks 样本：cgocall 32、parsePoints 4、memmove 2…），证明 pprof 输出无伪影；cgocall 栈内可见真实来源：d3d11.CreateDevice 11 ticks、DefWindowProc 10、Present 2、text shaper 2 等。

## 处理与预防

1. 见到 `CGO_ENABLED=0` 构建下 "cgocall" 热点，**先解码 profile 输入再下结论**——不要凭 `-top` 文本在"伪影/真实 cgo"里二选一，还有第三种：runtime 自用的 syscall 机制。
2. M1 段 CPU 大头是窗口创建/首帧的 Win32 路径（GPU 上下文创建 + 消息泵 + Present），属 OS/图形栈固有成本；应用层唯一可探讨项是 init 期即建 D3D11 上下文是否可延迟。
3. 该解码方法（纯 Python 只读，无 pprof 运行）可复用于任何 Go profile 的独立仲裁。

## 数据来源

- 解码验证器与结论：`night-batch-2026-10-30/zcode/self-audit-r1/`（11/11）、`m1-facts-consolidated-r1/out/full-tick-accounting.json`（54 ticks 全量归因）、`m1-second-sample-r1/`（第二样本 46 ticks 互证）
- 输入 profile：`night-batch-2026-10-30/workbuddy/execute-r1/run/profiles/b00-u02…/cpu.pprof`（SHA `34e68f72…`）、`b00-u01…/cpu.pprof`（SHA `84713eb2…`）
