# TASK-BENCH-001：性能基准测试与旗舰 Demo 验收

## 元信息
- 状态：Todo | 优先级：P1 | 预计：6h
- 分支：feature/TASK-BENCH-001
- 依赖：TASK-CONTRACT-001、TASK-PARSER-001、TASK-PARSER-002、TASK-ANALYZER-001、TASK-CODEGEN-001、TASK-STATE-001、TASK-STYLE-001、TASK-ROUTER-001、TASK-LIFE-001、TASK-DEVX-001、TASK-DEVX-002、TASK-OS-001、TASK-OS-002、TASK-WIDGET-001

## 1. 目标
构建覆盖核心功能的示例应用并建立可复现基准，量化生成代码相对手写 Gio 的渲染开销、内存分配和关键启动指标。

## 1.1 基线来源
单机初步基线（i7-12700H / Go 1.26.1 / Gio v0.10.3 / CGO_ENABLED=0）见 `docs/03-架构/性能预算与优化策略.md` §2。该数据用于确立量级与排序，**发布数值必须在固定 CI 环境复测**并写入 `docs/06-测试/测试报告.md` §7。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-BENCH-001-1 | 建立旗舰 Demo，覆盖绑定、列表、路由、主题和系统交互 | go test ./benchmarks/... |
| TASK-BENCH-001-2 | 建立生成版与手写 Gio 对照基准并记录环境和基线 | go test -bench=. -benchmem ./benchmarks/... |
| TASK-BENCH-001-3 | 汇总内存、启动和帧耗时结果并对照性能目标 | go test -bench=. -benchmem ./benchmarks/... |
| TASK-BENCH-001-4 | 渲染规模对照：全量 vs 虚拟滚动（10k / 100k 行） | go test -run '^$' -bench 'List' -benchmem ./benchmarks/... |
| TASK-BENCH-001-5 | 静态子树固化对照：闭包 vs 编译期展开 | go test -run '^$' -bench 'Static' -benchmem ./benchmarks/... |
| TASK-BENCH-001-6 | 文本 shaping 对照：稳定字符串 vs 每帧变化字符串 | go test -run '^$' -bench 'Text' -benchmem ./benchmarks/... |
| TASK-BENCH-001-7 | 实现 `make bench-assert` 性能不变量断言 | make bench-assert |
| TASK-BENCH-001-8 | 字体加载耗时与 CJK 文本基准（依赖 TASK-FONT-001） | go test -run '^$' -bench 'Font' ./benchmarks/... |
| TASK-BENCH-001-9 | 抽象开销对照：接口派发 vs 具体类型调用（ADR-0001 §3.2 的 5.9× 复现） | go test -run '^$' -bench 'Adt' -benchmem ./benchmarks/... |

## 3. 必须断言的性能不变量
依据 `docs/03-架构/性能预算与优化策略.md` §3：

| 断言 | 阈值 |
|---|---|
| 静态页面稳定帧 `allocs/op` | == 0 |
| 静态子树稳定帧宏重录次数 | == 0 |
| 10,000 行虚拟列表单帧 | ≤ 1 ms |
| 10,000 行 vs 100,000 行耗时比 | ≤ 1.5× |
| 生成源码含静态子树 `CallOp` 字段 | 是 |
| 生成源码 `Layout` 路径无 `fmt.Sprintf` | 是 |
