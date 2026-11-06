# R25-B 协调者最终技术补正（2026-11-06，待范围批准）

作者 candidate `737546817ad81547056bb3b9bc2e0fa84e1b03d1`、ready `1136e11`。
本篇登记协调者补正，不把作者冻结 SHA 当作已原样接纳。主库 A/C 仍为已发布
`76f0669`；Expanded 候选在批准之前不进主库，9 项文档 WIP 保留。

R3 的 Gap/Padding 边界、合法 Def/Slot、Exact 尺寸、逐帧约束与双实例缓存测试
独立通过。去除含 Expanded Row 的 Gap/Padding 后，x=2 的边界守卫行为红，恢复绿。

## 源码身份与探针补正

作者声称重建的 `r25r3_bench_probe_test.go` 实际未在冻结 ref 中，作者树显示为
未跟踪文件。协调者只读取其原字节另存 SHA 后，在自有候选树正式加入。
另发现原外层测试只检查输出中出现 benchmark 名字：内层 `b.Fatal`、rc1 时外层
仍 PASS。最小补正为必须出现 `[err=nil]`、不得有 FAIL，并匹配真实数字行；相同
内层失败变异现在外层 rc1，恢复 rc0。

原作者启用 input.Router.Source 却不在计时循环提交 Frame，累积事件过滤状态，
不能把非零 B/op 与四舍五入后的 0 allocs/op 当成稳态零分配证明。协调者新增
同模块、同 Page/Row/按钮/文字/主题/约束/轮询范围的对照，分两种口径：

- Layout-only：禁用 Source，循环只 Reset+Layout，构造与十帧预热不计时；
- Frame：启用 Source，每次都提交 Router.Frame，两端同样计入这一成本。

独立 30000 次 ×3 运行中，Layout-only 生成版 median 1387ns/op、手写版 1840ns/op，
两端均为 0 B/op / 0 allocs/op。Frame 口径 median 3688 / 4038ns/op，均 48B/op /
2allocs/op。另一个同协议早批次数据保留，不选取有利单次。只说明这个离屏样本，
不外推全局性能或把差值归因于单个包装；旧 2908/2423 及 R2 数字不作验收依据。

补充了两端几何相等断言。手写侧仅借用生成结构体中的 Clickable 存储，Layout
完全手工组装，不调用生成 Layout。额外的 Router 成本不冒充生成代码的分配。

## 边界补正

作者 Max.Y=100000 是大有限值，低于 `sizelayout.UnboundedThreshold=1<<20`，
不能代替实现中的无界边界。保留原大有限测试并改名，另在阈值前/阈值/阈值后
跑真实生成布局，明确主轴 Flex 行为与交叉轴 Fill 的无界回退不是同一规则。
双实例缓存守卫另核生成的 slots 文本和更新后的静态几何，不只检查 VM 原值。

本轮未改变任何生产生成规则；生产实现仍为 R25/R1/R2 既有候选。
技术门禁、race 与最终组合证据见外部 `product-r25/coordinator/review-repair-r3/`。
基础范围是否批准、输出修订及正式接纳由协调者与负责人最后裁决；Grid、权重、
自身装饰仍显式拒绝，未擅自赋予新语义。无原生窗口、安装或原生性能采样。
