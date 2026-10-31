# 便携示例验证工具最终复核修正

基于 WorkBuddy 冻结 1cd0e5cef0a387f487d83b9e7f544d23d09438bc，独立复核完整净工具链。

修正输出根接管空目录/junction 与 Test-Path→New-Item -Force 创建竞争，改为拒绝已有/重解析路径且不带 Force 独占创建；checkout 在写入前按目录分隔符核对边界。修正 finally 日志失败跳过解锁并吞原码，保留已有错误码，成功后的日志失败返回 99。修正 Windows CRT 引号前/结尾多重反斜杠；锁助手 Win32 句柄 ctypes 签名、活 PID 的创建 FILETIME 比对，以及 JSON 输出使用 ASCII 转义以适配 PS5.1 代码页。无框架/CLI 协议改动。

独立证据位于 parallel-product/coordinator/portable-review：冻结态与修后相同无 Go 边界测试、真实 Python argv/env 子进程、共享 proclock 双向协议、错误 token 字节不变、两宿主 tasks 真实烟测。禁止自动删除他人锁，所有 Go 负载显式公共 heavy.lock。未运行窗口、宽矩阵或全仓测试。
