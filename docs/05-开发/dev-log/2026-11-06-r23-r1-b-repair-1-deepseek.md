# 2026-11-06 R23-R1-B repair-r1 dev-log（DeepSeek）

父 `3cea12af2c5b59a18b544b375299c9918dce2fa8`；分支 `codex/r23-r1-deepseek-repair-1`，树 `r23-r1-deepseek-repair1`。主库现 `fa0f9c5`（A 补证后，66 WIP 字节未变），不回滚。
记录提交：`3cea12a`（父）→ `0101bd9`（F-B1/F-B2/F-B3 修复+守卫+相邻）→ `c48ce84`（F-B4 方案）→ 本 dev-log。

## F-B1：固定 1 小时被放过 → 双向精确约束
上一版守卫只有 20ms 单侧下界，固定 1h 全 PASS（协调者 independent/raw 反例）。改为**包内私有确定性时钟**（`Lazy.now`，nil→time.Now）注入 fakeClock：
- 非零守卫：`TempElapsed == step(7ms)`（拒绝 0/未赋值、固定 1h、任意非本轮常量）；
- 零守卫：`TempElapsed == 0`（step=0，确定性证明零是本次测量）。
新源码重建协调者 1h 变异 → 非零/零守卫均 **FAIL**（`1h0m0s != 7ms` / `1h != 0`），并打红相邻 TempOnlyBuiltOnce。

## F-B2：去掉 sleep/强制延时
删除 `slowEmbeddedLoader`（time.Sleep(20ms)）。守卫无 sleep/忙等/放大字体，用确定性时钟。零/非零均可重复有界（×100 全 PASS）。

## F-B3：合法零确定性 + 相邻同源漏洞
零守卫注入 step=0 断言恰 0；未赋值由非零守卫翻红。相邻 `TestFontLazyLoadTempOnlyBuiltOnce` 的 `base<=0` 同源墙钟前提一并改为确定性时钟（base==5ms），保留“不重建/不改写”业务判据。**不报“flake 全消失”而留同源漏洞**——Temp 族 5 用例全部确定性化 + ×100 稳定。

## F-B4：方案锁与顺序
§5 预登记交替序列（S,L,S,L…，每样本独立单次 `-Runs 1`，禁止两整批冒充交替）；§8 用**真实公共锁** `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`，逐样本独占输出目录 + 非零退出保留不复跑。只准备，不开窗/不采样/不安装/不设阈值。

## 变异（新源码重建，健康 PASS→编译态 FAIL→恢复 PASS）
mut_fixed1h / mut_zero / mut_nomeasure / mut_rep1h 全部 rc1 FAIL（编译成功），恢复 PASS。原始见 `repair-r1/raw/mut/`。

## candidate concern
`Lazy.now` 为包内私有测试接缝（未导出、默认 time.Now、无公共 API、生产计时语义不变）。readyRef 含该接缝；若需单独候选，此处登记，合同依据见 closure-matrix。

## 门禁
theme 全包 ok、app 字体无窗口族 ok、vet rc0、gofmt 空、docgate（见 execute）、覆盖 theme 86.7%；`-race`（MinGW）theme×3 / Temp×5 / app 族 0 DATA RACE。`font_lazy_strict_test.go` 未改。

## 五态
实现 done / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。不宣布整体正式字体验收通过。
