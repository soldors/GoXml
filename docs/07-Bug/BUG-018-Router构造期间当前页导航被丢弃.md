# BUG-018：Router构造期间当前页导航被丢弃

- 级别：P2，存量Router B1候选问题。
- 状态：隔离修复及独立复审通过，未合入用户原始未提交包；不追认B1验收或批准B2。

## 反例与范围

先挂载/a，UI线程开始构造/b；工厂调用期间另一goroutine合法Navigate(/a)返回nil。原实现依据旧current=/a把请求当no-op丢弃，工厂随后提交/b，下一帧仍/b、pending为空。这违背构造期间“最后合法请求保留到后续帧”的约定，根因不是要求工厂长时间阻塞。静止、没有在途构造时Navigate当前页的no-op仍需保留。

本轮只在自有headless模块复制原router.go/router_test.go，未复制或运行integration_test.go。原12测试绿，channel固定时序的原R01红；没有运行任何窗口或race。

## 隔离修复与审查

候选只增加私有navigating标记，并在消费时对已是current的pending去重。取出请求与设置在途标记处于同锁；工厂不持锁执行，期间的合法请求保留；成功/error/缺失工厂分支复位标记，原error回滚与旧ctx保持。

作者原R01红→绿、旧12测试、14个同步/后台顺序场景通过；原复核者独立再现红绿，补测无效请求不抹pending、16个并发请求之后的已知最后请求、成功/error两路、缺失工厂与空消费，未发现新增P0/P1/P2。候选router.go SHA `ad97d7aba03a653aa8f5d9c98f51ae924089b60c8c7eb3b1a5bf446c80362647`。

候选还包含临时go.mod/go.sum和故意保留其他红例的review_test，不能整目录合入。精确接纳建议是router.go、原router_test、hardening_test及抽取的R01回归；真实集成仍需解决原用户未提交文件的接纳边界，当前三原件SHA不变。

nil成功工厂、取消后队列处理/丢弃计数、无界drain各自政策与B2 API仍未处理；无race结果、无任意并发Layout安全承诺。

关联：[夜间计划](../05-开发/2026-10-30-夜间任务分工与续派计划.md)、[夜间复审记录](../06-测试/2026-10-30-夜间准备与独立复审记录.md)。
