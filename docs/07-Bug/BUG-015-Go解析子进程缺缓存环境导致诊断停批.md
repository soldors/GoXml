# BUG-015：Go解析子进程缺缓存环境导致诊断停批

- 状态：真实失败已记录，待独立复核及工具修复；不属于已证明的字体生产故障。
- 时间：2026-10-29 00:34:11.918至00:34:14.755（北京时间）。
- 对象：隔离M1诊断宿主的Go profile解析子进程，非诊断exe本身。

## 实际结果

计划24次、执行2次、有效1次、22次未执行，batchValid=false，宿主rc=5。第二条CPU模式诊断exe退出0、stdout为JSON、stderr空，cpu.pprof保留；Go解析命令失败使该条无效，随后按规则停批。锁和claim已释放，run-meta记录postIdentityProblems为空。最终交付DONE在本记录撰写时尚未确认，不把在制报告当最终验收。

权威原始stderr：
```
build cache is required, but could not be located: GOCACHE is not defined and %LocalAppData% is not defined
```

本机证据：`D:/Codes/Goxml-agent-rounds/2026-10-27/face-selection/m1-diagnosis-execution-r1/workbuddy/run/samples/b00-u01-B-runtime-default-cpu-default-lazy/parse.stderr.bin`；批次事实在run/run-meta.json。原文件只读保留。

## 原因范围与处置

Go工具在实际最小化子进程环境中找不到所需缓存定位配置。尚需从真实argv/env与宿主launcher确认是哪一层环境被裁剪；不能在无核查时归咎Windows、Go安装或pprof文件损坏。

先修复并验证解析子进程的最小环境，覆盖缺省GOCACHE/LocalAppData、显式可用缓存位置、错误位置及真实解析路径；解析器环境修复不得改诊断两层GOMAXPROCS实验条件。无需安装组件。

失败原批次不可改valid、不可在原目录续跑或补齐。任何新真实诊断都要新的许可、哈希和目录；仅重解析旧文件也不能追改原批次为成功。两条单点M1不能用于p95/达标/根因声明。

关联：[实际执行测试记录](../06-测试/2026-10-29-M1诊断实际执行与停批记录.md)、[PIT-017](../08-踩坑/踩坑-017-真实子进程环境必须贯穿验证.md)。

## 无窗口修复候选补记

协调者通过真实launcher与原cpu.pprof补测：最小环境原错误rc1；仅补GOCACHE之后又出现Go临时目录落在C:/Windows、mkdir权限失败，首轮候选未通过。补显式可写GOTMPDIR后，最小环境与正常环境的Go解析均rc0，原profile哈希及输入环境保持。记录见本机night-resume/coordinator/env-repair-result.json（首轮失败）与env-v2/env-repair-result.json（后续通过）。

这只是辅助解析环境候选，不是冻住宿主真实run分支的新验收，未接入新许可、未新增窗口、未追改原valid或关闭原批次缺口。下一次真实执行仍需新快照与新许可。
