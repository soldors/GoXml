# PIT-003：Windows 下 Process.Kill() 无法清理子进程树
- 场景：goxml watch 重启子进程时，.exe 仍被旧进程占用导致编译失败。
- 规避：通知子进程优雅退出；超时强制执行 taskkill /F /T /PID 清理整个进程树后再启动 go build。
